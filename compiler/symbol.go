package compiler

// This file manages symbols, that is, functions and globals. It reads their
// pragmas, determines the link name, etc.

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"moxie/compiler/llvmutil"
	"moxie/loader"
	"moxie/mxssa"
	"moxie/typecheck"
	"tinygo.org/x/go-llvm"
)

// functionInfo contains some information about a function or method. In
// particular, it contains information obtained from pragmas.
//
// The linkName value contains a valid link name, even if //go:linkname is not
// present.
type functionInfo struct {
	wasmModule    string     // go:wasm-module
	wasmName      string     // wasm-export-name or wasm-import-name in the IR
	wasmExport    string     // go:wasmexport is defined (export is unset, this adds an exported wrapper)
	wasmExportPos token.Pos  // position of //go:wasmexport comment
	linkName      string     // go:linkname, go:export - the IR function name
	section       string     // go:section - object file section name
	exported      bool       // go:export, CGo
	interrupt     bool       // go:interrupt
	nobounds      bool       // go:nobounds
	noescape      bool       // go:noescape
	variadic      bool       // go:variadic (CGo only)
	inline        inlineType // go:inline
}

type inlineType int

// How much to inline.
const (
	// Default behavior. The compiler decides for itself whether any given
	// function will be inlined. Whether any function is inlined depends on the
	// optimization level.
	inlineDefault inlineType = iota

	// Inline hint, just like the C inline keyword (signalled using
	// //go:inline). The compiler will be more likely to inline this function,
	// but it is not a guarantee.
	inlineHint

	// Don't inline, just like the GCC noinline attribute. Signalled using
	// //go:noinline.
	inlineNone
)

// Values for the allockind attribute. Source:
// https://github.com/llvm/llvm-project/blob/release/16.x/llvm/include/llvm/IR/Attributes.h#L49
const (
	allocKindAlloc = 1 << iota
	allocKindRealloc
	allocKindFree
	allocKindUninitialized
	allocKindZeroed
	allocKindAligned
)

// getFunction returns the LLVM function for the given *mxssa.Function, creating
// it if needed. It can later be filled with compilerContext.createFunction().
func (c *compilerContext) getFunction(fn *mxssa.Function) (llvm.Type, llvm.Value) {
	info := c.getFunctionInfo(fn)
	llvmFn := c.mod.NamedFunction(info.linkName)
	if !llvmFn.IsNil() {
		return llvmFn.GlobalValueType(), llvmFn
	}

	var retType llvm.Type
	var results *typecheck.Tuple
	if fn.Signature != nil {
		results = fn.Signature.Results()
	}
	if results == nil || results.Len() == 0 {
		retType = c.ctx.VoidType()
	} else if results.Len() == 1 {
		retType = c.getLLVMTypeTC(results.At(0).Type())
	} else {
		rs := make([]llvm.Type, 0, results.Len())
		for i := 0; i < results.Len(); i++ {
			rs = append(rs, c.getLLVMTypeTC(results.At(i).Type()))
		}
		retType = c.ctx.StructType(rs, false)
	}

	var paramInfos []paramInfo
	for _, param := range getParamsTC(fn.Signature) {
		paramType := c.getLLVMTypeTC(param.Type())
		paramFragmentInfos := c.expandFormalParamTypeTC(paramType, param.Name(), param.Type())
		paramInfos = append(paramInfos, paramFragmentInfos...)
	}

	// Add an extra parameter as the function context. This context is used in
	// closures and bound methods, but should be optimized away when not used.
	if !info.exported && !strings.HasPrefix(info.linkName, "llvm.") {
		paramInfos = append(paramInfos, paramInfo{llvmType: c.dataPtrType, name: "context", elemSize: 0})
	}

	var paramTypes []llvm.Type
	for _, info := range paramInfos {
		paramTypes = append(paramTypes, info.llvmType)
	}

	fnType := llvm.FunctionType(retType, paramTypes, info.variadic)
	llvmFn = llvm.AddFunction(c.mod, info.linkName, fnType)
	if strings.HasPrefix(c.Triple, "wasm") {
		// C functions without prototypes like this:
		//   void foo();
		// are actually variadic functions. However, it appears that it has been
		// decided in WebAssembly that such prototype-less functions are not
		// allowed in WebAssembly.
		// In C, this can only happen when there are zero parameters, hence this
		// check here. For more information:
		// https://reviews.llvm.org/D48443
		// https://github.com/WebAssembly/tool-conventions/issues/16
		if info.variadic && len(fn.Params) == 0 {
			attr := c.ctx.CreateStringAttribute("no-prototype", "")
			llvmFn.AddFunctionAttr(attr)
		}
	}
	c.addStandardDeclaredAttributes(llvmFn)

	dereferenceableOrNullKind := llvm.AttributeKindID("dereferenceable_or_null")
	for i, paramInfo := range paramInfos {
		if paramInfo.elemSize != 0 {
			dereferenceableOrNull := c.ctx.CreateEnumAttribute(dereferenceableOrNullKind, paramInfo.elemSize)
			llvmFn.AddAttributeAtIndex(i+1, dereferenceableOrNull)
		}
		if info.noescape && paramInfo.flags&paramIsGoParam != 0 && paramInfo.llvmType.TypeKind() == llvm.PointerTypeKind {
			// Parameters to functions with a //go:noescape parameter should get
			// the nocapture attribute. However, the context parameter should
			// not.
			// (It may be safe to add the nocapture parameter to the context
			// parameter, but I'd like to stay on the safe side here).
			nocapture := c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0)
			llvmFn.AddAttributeAtIndex(i+1, nocapture)
		}
		if paramInfo.flags&paramIsReadonly != 0 && paramInfo.llvmType.TypeKind() == llvm.PointerTypeKind {
			// Readonly pointer parameters (like strings) benefit from being marked as readonly.
			readonly := c.ctx.CreateEnumAttribute(llvm.AttributeKindID("readonly"), 0)
			llvmFn.AddAttributeAtIndex(i+1, readonly)
		}
	}

	// Set a number of function or parameter attributes, depending on the
	// function. These functions are runtime functions that are known to have
	// certain attributes that might not be inferred by the compiler.
	switch info.linkName {
	case "abort":
		// On *nix systems, the "abort" functuion in libc is used to handle fatal panics.
		// Mark it as noreturn so LLVM can optimize away code.
		llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("noreturn"), 0))
	case "internal/abi.NoEscape":
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
	case "machine.keepAliveNoEscape", "machine.unsafeNoEscape":
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
	case "runtime.alloc":
		// Tell the optimizer that runtime.alloc is an allocator, meaning that it
		// returns values that are never null and never alias to an existing value.
		for _, attrName := range []string{"noalias", "nonnull"} {
			llvmFn.AddAttributeAtIndex(0, c.ctx.CreateEnumAttribute(llvm.AttributeKindID(attrName), 0))
		}
		// Add attributes to signal to LLVM that this is an allocator function.
		// This enables a number of optimizations.
		llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("allockind"), allocKindAlloc|allocKindZeroed))
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("alloc-family", "runtime.alloc"))
		// Use a special value to indicate the first parameter:
		// > allocsize has two integer arguments, but because they're both 32 bits, we can
		// > pack them into one 64-bit value, at the cost of making said value
		// > nonsensical.
		// >
		// > In order to do this, we need to reserve one value of the second (optional)
		// > allocsize argument to signify "not present."
		llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("allocsize"), 0x0000_0000_ffff_ffff))
	case "runtime.sliceAppend":
		// Appending a slice will only read the to-be-appended slice, it won't
		// be modified.
		llvmFn.AddAttributeAtIndex(2, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
		llvmFn.AddAttributeAtIndex(2, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("readonly"), 0))
	case "runtime.sliceCopy":
		// Copying a slice won't capture any of the parameters.
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("writeonly"), 0))
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
		llvmFn.AddAttributeAtIndex(2, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("readonly"), 0))
		llvmFn.AddAttributeAtIndex(2, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
	case "runtime.stringFromRunes":
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("readonly"), 0))
	case "runtime.trackPointer":
		// This function is necessary for tracking pointers on the stack in a
		// portable way (see gc_stack_portable.go). Indicate to the optimizer
		// that the only thing we'll do is read the pointer.
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nocapture"), 0))
		llvmFn.AddAttributeAtIndex(1, c.ctx.CreateEnumAttribute(llvm.AttributeKindID("readonly"), 0))
	case "__mulsi3", "__divmodsi4", "__udivmodsi4":
		if strings.Split(c.Triple, "-")[0] == "avr" {
			// These functions are compiler-rt/libgcc functions that are
			// currently implemented in Go. Assembly versions should appear in
			// LLVM 17 hopefully. Until then, they need to be made available to
			// the linker and the best way to do that is llvm.compiler.used.
			// I considered adding a pragma for this, but the LLVM language
			// reference explicitly says that this feature should not be exposed
			// to source languages:
			// > This is a rare construct that should only be used in rare
			// > circumstances, and should not be exposed to source languages.
			llvmutil.AppendToGlobal(c.mod, "llvm.compiler.used", llvmFn)
		}
	case "GetModuleHandleExA", "GetProcAddress", "GetSystemInfo", "GetSystemTimeAsFileTime", "LoadLibraryExW", "QueryPerformanceCounter", "QueryPerformanceFrequency", "QueryUnbiasedInterruptTime", "SetEnvironmentVariableA", "Sleep", "SystemFunction036", "VirtualAlloc":
		// On Windows we need to use a special calling convention for some
		// external calls.
		if c.GOOS == "windows" && c.GOARCH == "386" {
			llvmFn.SetFunctionCallConv(llvm.X86StdcallCallConv)
		}
	}

	// External/exported functions may not retain pointer values.
	// https://golang.org/cmd/cgo/#hdr-Passing_pointers
	if info.exported {
		if c.archFamily() == "wasm32" && len(fn.Blocks) == 0 {
			// We need to add the wasm-import-module and the wasm-import-name
			// attributes.
			if info.wasmModule != "" {
				llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("wasm-import-module", info.wasmModule))
			}

			llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("wasm-import-name", info.wasmName))
		}
		nocaptureKind := llvm.AttributeKindID("nocapture")
		nocapture := c.ctx.CreateEnumAttribute(nocaptureKind, 0)
		for i, typ := range paramTypes {
			if typ.TypeKind() == llvm.PointerTypeKind {
				llvmFn.AddAttributeAtIndex(i+1, nocapture)
			}
		}
	}

	// Build the function if needed.
	c.maybeCreateSyntheticFunction(fn, llvmFn)

	return fnType, llvmFn
}

// If this is a synthetic function (such as a generic function or a wrapper),
// create it now.
func (c *compilerContext) maybeCreateSyntheticFunction(fn *mxssa.Function, llvmFn llvm.Value) {
	// Synthetic functions are functions that do not appear in the source code,
	// they are artificially constructed. Usually they are wrapper functions
	// that are not referenced anywhere except in a SSA call instruction so
	// should be created right away.
	// The exception is the package initializer, which does appear in the
	// *ssa.Package members and so shouldn't be created here.
	if fn.Synthetic == "" || fn.Synthetic == "package initializer" || fn.Synthetic == "generic function" || fn.Synthetic == "range-over-func yield" {
		return
	}
	// newBuilder still requires *ssa.Function; obtain via translator.
	goFn := c.mxTranslator.ReverseFunction(fn)
	if goFn != nil {
		if origin := goFn.Origin(); origin != nil && origin.RelString(nil) == "internal/abi.Escape" {
			// This is a special implementation or internal/abi.Escape, which
			// can only really be implemented in the compiler.
			// For simplicity we'll only implement pointer parameters for now.
			if len(fn.Params) > 0 {
				if _, ok := fn.Params[0].Type().Underlying().(*typecheck.Pointer); ok {
					irbuilder := c.ctx.NewBuilder()
					defer irbuilder.Dispose()
					b := newBuilder(c, irbuilder, goFn)
					b.createAbiEscapeImpl()
					llvmFn.SetLinkage(llvm.LinkOnceODRLinkage)
					llvmFn.SetUnnamedAddr(true)
				}
			}
			// If the parameter is not of a pointer type, it will be left
			// unimplemented. This will result in a linker error if the function
			// is really called, making it clear it needs to be implemented.
			return
		}
		// Ensure the mxssa body is filled — synthetic instances/wrappers may
		// have been registered as shells via cross-package translation.
		c.mxTranslator.EnsureFunction(goFn)
	}
	if goFn == nil || len(goFn.Blocks) == 0 {
		c.addError(fn.Pos(), "missing function body")
		return
	}
	irbuilder := c.ctx.NewBuilder()
	b := newBuilder(c, irbuilder, goFn)
	b.createFunctionDispatch()
	irbuilder.Dispose()
	llvmFn.SetLinkage(llvm.LinkOnceODRLinkage)
	llvmFn.SetUnnamedAddr(true)
}

// getFunctionInfo returns information about a function that is not directly
// present in *mxssa.Function, such as the link name and whether it should be
// exported.
func (c *compilerContext) getFunctionInfo(f *mxssa.Function) functionInfo {
	if info, ok := c.functionInfos[f]; ok {
		return info
	}
	// Use ssa.Function.RelString(nil) for canonical linkName since
	// mxssa.Function.RelString doesn't include the receiver type for methods.
	var linkName string
	if goFn := c.mxTranslator.ReverseFunction(f); goFn != nil {
		linkName = goFn.RelString(nil)
	} else {
		linkName = f.RelString(nil)
	}
	info := functionInfo{
		linkName: linkName,
	}

	// Check for a few runtime functions that are treated specially.
	if info.linkName == "runtime.wasmEntryReactor" && c.BuildMode == "c-shared" {
		info.linkName = "_initialize"
		info.wasmName = "_initialize"
		info.exported = true
	}
	if info.linkName == "runtime.wasmEntryCommand" && c.BuildMode == "default" {
		info.linkName = "_start"
		info.wasmName = "_start"
		info.exported = true
	}
	if info.linkName == "runtime.wasmEntryLegacy" && c.BuildMode == "wasi-legacy" {
		info.linkName = "_start"
		info.wasmName = "_start"
		info.exported = true
	}

	// Check for //go: pragmas, which may change the link name (among others).
	c.parsePragmasTC(&info, f)

	c.functionInfos[f] = info
	return info
}

// parsePragmas is used by getFunctionInfo to parse function pragmas such as
// //export or //go:noinline.

// parsePragmasTC is the mxssa variant of parsePragmas. Walks the AST via
// fn.Syntax() and uses TC helpers for unsafe-import and wasm checks.
//
// Note: Origin (generic instantiation) handling is not supported by mxssa;
// the syntax falls back to fn.Syntax() unconditionally. In practice Moxie
// makes minimal use of generics and pragmas don't apply to instantiations.
func (c *compilerContext) parsePragmasTC(info *functionInfo, fn *mxssa.Function) {
	syntax := fn.Syntax()
	if syntax == nil {
		return
	}

	var pragmas []*ast.Comment
	hasWasmExport := false
	if decl, ok := syntax.(*ast.FuncDecl); ok && decl.Doc != nil {
		for _, comment := range decl.Doc.List {
			text := comment.Text
			if strings.HasPrefix(text, "//go:") || strings.HasPrefix(text, "//export ") {
				pragmas = append(pragmas, comment)
				if strings.HasPrefix(comment.Text, "//go:wasmexport ") {
					hasWasmExport = true
				}
			}
		}
	}

	for _, comment := range pragmas {
		parts := strings.Fields(comment.Text)
		switch parts[0] {
		case "//export", "//go:export":
			if len(parts) != 2 {
				continue
			}
			if hasWasmExport {
				continue
			}
			info.linkName = parts[1]
			info.wasmName = info.linkName
			info.exported = true
		case "//go:interrupt":
			if hasUnsafeImportTC(fn.Pkg.Pkg) {
				info.interrupt = true
			}
		case "//go:wasm-module":
			if len(parts) != 2 {
				continue
			}
			info.wasmModule = parts[1]
		case "//go:wasmimport":
			if len(parts) != 3 {
				continue
			}
			if len(fn.Blocks) > 0 {
				c.addError(fn.Pos(), "can only use //go:wasmimport on declarations")
				continue
			}
			c.checkWasmImportExportTC(fn, comment.Text)
			info.exported = true
			info.wasmModule = parts[1]
			info.wasmName = parts[2]
		case "//go:wasmexport":
			if len(fn.Blocks) == 0 {
				c.addError(fn.Pos(), "can only use //go:wasmexport on definitions")
				continue
			}
			if len(parts) != 2 {
				c.addError(fn.Pos(), fmt.Sprintf("expected one parameter to //go:wasmexport, not %d", len(parts)-1))
				continue
			}
			name := parts[1]
			if name == "_start" || name == "_initialize" {
				c.addError(fn.Pos(), fmt.Sprintf("//go:wasmexport does not allow %#v", name))
				continue
			}
			if c.BuildMode != "c-shared" && fn.RelString(nil) == "main.main" {
				c.addError(fn.Pos(), fmt.Sprintf("//go:wasmexport does not allow main.main to be exported with -buildmode=%s", c.BuildMode))
				continue
			}
			if c.archFamily() != "wasm32" {
				c.addError(fn.Pos(), "//go:wasmexport is only supported on wasm")
			}
			c.checkWasmImportExportTC(fn, comment.Text)
			info.wasmExport = name
			info.wasmExportPos = comment.Slash
		case "//go:inline":
			info.inline = inlineHint
		case "//go:noinline":
			info.inline = inlineNone
		case "//go:linkname":
			if len(parts) != 3 || parts[1] != fn.Name() {
				continue
			}
			if hasUnsafeImportTC(fn.Pkg.Pkg) {
				info.linkName = parts[2]
			}
		case "//go:section":
			if len(parts) == 2 && hasUnsafeImportTC(fn.Pkg.Pkg) {
				info.section = parts[1]
				info.inline = inlineNone
			}
		case "//go:nobounds":
			if hasUnsafeImportTC(fn.Pkg.Pkg) {
				info.nobounds = true
			}
		case "//go:noescape":
			if len(fn.Blocks) == 0 {
				info.noescape = true
			}
		case "//go:variadic":
			if strings.HasPrefix(fn.Name(), "_Cgo_") {
				info.variadic = true
			}
		}
	}

	if c.Nobounds {
		info.nobounds = true
	}
}

// Check whether this function can be used in //go:wasmimport or
// //go:wasmexport. It will add an error if this is not the case.
//
// The list of allowed types is based on this proposal:
// https://github.com/golang/go/issues/59149

// Check whether the type maps directly to a WebAssembly type.
//
// This reflects the relaxed type restrictions proposed here (except for structs.HostLayout):
// https://github.com/golang/go/issues/66984
//
// This previously reflected the additional restrictions documented here:
// https://github.com/golang/go/issues/59149

type wasmSite int

const (
	siteParam wasmSite = iota
	siteResult
	siteIndirect // pointer or field
)

// checkWasmImportExportTC is the mxssa variant of checkWasmImportExport. Note
// that typecheck.Var doesn't expose a typed Pos(), so result-type errors use
// the function's position as a fallback. Parameter errors use the parameter's
// own position via mxssa.Parameter.Pos().
func (c *compilerContext) checkWasmImportExportTC(fn *mxssa.Function, pragma string) {
	if c.mxPkg.Path() == "runtime" || c.mxPkg.Path() == "syscall/js" || c.mxPkg.Path() == "syscall" || c.mxPkg.Path() == "crypto/internal/sysrand" {
		return
	}
	sig := fn.Signature
	if sig == nil {
		return
	}
	if results := sig.Results(); results != nil {
		if results.Len() > 1 {
			c.addError(fn.Pos(), fmt.Sprintf("%s: too many return values", pragma))
		} else if results.Len() == 1 {
			result := results.At(0)
			if !c.isValidWasmTypeTC(result.Type(), siteResult) {
				c.addError(fn.Pos(), fmt.Sprintf("%s: unsupported result type %s", pragma, result.Type().String()))
			}
		}
	}
	for _, param := range fn.Params {
		if !c.isValidWasmTypeTC(param.Type(), siteParam) {
			c.addError(param.Pos(), fmt.Sprintf("%s: unsupported parameter type %s", pragma, param.Type().String()))
		}
	}
}

// isValidWasmTypeTC is the typecheck variant of isValidWasmType. Mirrors the
// SSA logic with typecheck.Type. Note: Uintptr collapses to Uint64 in tcbridge,
// so the Uintptr case is handled by the Uint64 branch.
func (c *compilerContext) isValidWasmTypeTC(typ typecheck.Type, site wasmSite) bool {
	switch typ := typ.Underlying().(type) {
	case *typecheck.Basic:
		switch typ.Kind() {
		case typecheck.Bool:
			return true
		case typecheck.Int8, typecheck.Uint8, typecheck.Int16, typecheck.Uint16:
			return site == siteIndirect
		case typecheck.Int32, typecheck.Uint32, typecheck.Int64, typecheck.Uint64:
			return true
		case typecheck.Float32, typecheck.Float64:
			return true
		case typecheck.UnsafePointer:
			return true
		case typecheck.String:
			return site == siteParam || site == siteIndirect
		}
	case *typecheck.Array:
		return site == siteIndirect && c.isValidWasmTypeTC(typ.Elem(), siteIndirect)
	case *typecheck.Struct:
		if site != siteIndirect {
			return false
		}
		if typ.NumFields() == 0 {
			return true
		}
		// HostLayout detection requires field name matching by string;
		// typecheck.Struct doesn't expose go-version metadata, so default
		// to true (accept) for fields that aren't HostLayout.
		hasHostLayout := true
		for i := 0; i < typ.NumFields(); i++ {
			ftyp := typ.Field(i).Type()
			if ftyp.String() == "structs.HostLayout" {
				hasHostLayout = true
				continue
			}
			if !c.isValidWasmTypeTC(ftyp, siteIndirect) {
				return false
			}
		}
		return hasHostLayout
	case *typecheck.Pointer:
		return c.isValidWasmTypeTC(typ.Elem(), siteIndirect)
	}
	return false
}

// getParamsTC returns the function parameters, including the receiver at
// the start. Tolerates nil Signature
// fields since the bridge may produce signatures with nil params/results.
func getParamsTC(sig *typecheck.Signature) []*typecheck.Var {
	params := []*typecheck.Var{}
	if sig == nil {
		return params
	}
	if sig.Recv() != nil {
		params = append(params, sig.Recv())
	}
	if p := sig.Params(); p != nil {
		for i := 0; i < p.Len(); i++ {
			params = append(params, p.At(i))
		}
	}
	return params
}

// addStandardDeclaredAttributes adds attributes that are set for any function,
// whether declared or defined.
func (c *compilerContext) addStandardDeclaredAttributes(llvmFn llvm.Value) {
	if c.SizeLevel >= 1 {
		// Set the "optsize" attribute to make slightly smaller binaries at the
		// cost of minimal performance loss (-Os in Clang).
		kind := llvm.AttributeKindID("optsize")
		attr := c.ctx.CreateEnumAttribute(kind, 0)
		llvmFn.AddFunctionAttr(attr)
	}
	if c.SizeLevel >= 2 {
		// Set the "minsize" attribute to reduce code size even further,
		// regardless of performance loss (-Oz in Clang).
		kind := llvm.AttributeKindID("minsize")
		attr := c.ctx.CreateEnumAttribute(kind, 0)
		llvmFn.AddFunctionAttr(attr)
	}
	if c.CPU != "" {
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("target-cpu", c.CPU))
	}
	if c.Features != "" {
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("target-features", c.Features))
	}
}

// addStandardDefinedAttributes adds the set of attributes that are added to
// every function defined by Moxie (even thunks/wrappers), possibly depending
// on the architecture. It does not set attributes only set for declared
// functions, use addStandardDeclaredAttributes for this.
func (c *compilerContext) addStandardDefinedAttributes(llvmFn llvm.Value) {
	// Moxie does not currently raise exceptions, so set the 'nounwind' flag.
	// This behavior matches Clang when compiling C source files.
	// It reduces binary size on Linux a little bit on non-x86_64 targets by
	// eliminating exception tables for these functions.
	llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("nounwind"), 0))
	if strings.Split(c.Triple, "-")[0] == "x86_64" {
		// Required by the ABI.
		// The uwtable has two possible values: sync (1) or async (2). We use
		// sync because we currently don't use async unwind tables.
		// For details, see: https://llvm.org/docs/LangRef.html#function-attributes
		llvmFn.AddFunctionAttr(c.ctx.CreateEnumAttribute(llvm.AttributeKindID("uwtable"), 1))
	}
}

// addStandardAttributes adds all attributes added to defined functions.
func (c *compilerContext) addStandardAttributes(llvmFn llvm.Value) {
	c.addStandardDeclaredAttributes(llvmFn)
	c.addStandardDefinedAttributes(llvmFn)
}

// globalInfo contains some information about a specific global. By default,
// linkName is equal to .RelString(nil) on a global and extern is false, but for
// some symbols this is different (due to //go:extern for example).
type globalInfo struct {
	linkName string // go:extern, go:linkname
	extern   bool   // go:extern
	align    int    // go:align
	section  string // go:section
}

// loadASTComments loads comments on globals from the AST, for use later in the
// program. In particular, they are required for //go:extern pragmas on globals.
func (c *compilerContext) loadASTComments(pkg *loader.Package) {
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				switch decl.Tok {
				case token.VAR:
					if len(decl.Specs) != 1 {
						continue
					}
					for _, spec := range decl.Specs {
						switch spec := spec.(type) {
						case *ast.ValueSpec: // decl.Tok == token.VAR
							for _, name := range spec.Names {
								id := pkg.Pkg.Path() + "." + name.Name
								c.astComments[id] = decl.Doc
							}
						}
					}
				}
			}
		}
	}
}

// getGlobal returns a LLVM IR global value for a native SSA global. It is
// added to the LLVM IR if it has not been added already.
func (c *compilerContext) getGlobal(g *mxssa.Global) llvm.Value {
	info := c.getGlobalInfo(g)
	llvmGlobal := c.mod.NamedGlobal(info.linkName)
	if llvmGlobal.IsNil() {
		// mxssa.Global.Type() returns the pointer type (matches go/ssa).
		var elemType typecheck.Type
		if ptrTyp, ok := g.Type().(*typecheck.Pointer); ok {
			elemType = ptrTyp.Elem()
		} else {
			elemType = g.Type()
		}
		llvmType := c.getLLVMTypeTC(elemType)
		llvmGlobal = llvm.AddGlobal(c.mod, llvmType, info.linkName)

		// Set alignment from the //go:align comment.
		alignment := c.targetData.ABITypeAlignment(llvmType)
		if info.align > alignment {
			alignment = info.align
		}
		if alignment <= 0 || alignment&(alignment-1) != 0 {
			c.addError(g.Pos(), "global variable alignment must be a positive power of two")
		} else {
			llvmGlobal.SetAlignment(alignment)
		}

		if c.Debug && !info.extern {
			// Debug info still uses go/types to drive getDIType, since the
			// createDIType chain remains keyed on go/types.Type. Migrating
			// that is its own commit (§3). Pull from getDITypeForGlobal
			// (helper in compiler.go) so symbol.go stays types-import-free.
			pos := c.mxProgram.Fset.Position(g.Pos())
			diType := c.getDITypeForGlobal(g)
			diglobal := c.dibuilder.CreateGlobalVariableExpression(c.difiles[pos.Filename], llvm.DIGlobalVariableExpression{
				Name:        g.String(),
				LinkageName: info.linkName,
				File:        c.getDIFile(pos.Filename),
				Line:        pos.Line,
				Type:        diType,
				LocalToUnit: false,
				Expr:        c.dibuilder.CreateExpression(nil),
				AlignInBits: uint32(alignment) * 8,
			})
			llvmGlobal.AddMetadata(0, diglobal)
		}
	}
	return llvmGlobal
}

// getGlobalInfo returns some information about a specific global.
func (c *compilerContext) getGlobalInfo(g *mxssa.Global) globalInfo {
	info := globalInfo{
		linkName: g.String(),
	}
	doc := c.astComments[info.linkName]
	if doc != nil {
		info.parsePragmasTC(doc, c, g)
	}
	return info
}

// Parse //go: pragma comments from the source. In particular, it parses the
// //go:extern and //go:linkname pragmas on globals.

// parsePragmasTC is the mxssa variant of (*globalInfo).parsePragmas.
func (info *globalInfo) parsePragmasTC(doc *ast.CommentGroup, c *compilerContext, g *mxssa.Global) {
	for _, comment := range doc.List {
		if !strings.HasPrefix(comment.Text, "//go:") {
			continue
		}
		parts := strings.Fields(comment.Text)
		switch parts[0] {
		case "//go:extern":
			info.extern = true
			if len(parts) == 2 {
				info.linkName = parts[1]
			}
		case "//go:align":
			align, err := strconv.Atoi(parts[1])
			if err == nil {
				info.align = align
			}
		case "//go:section":
			if len(parts) == 2 {
				info.section = parts[1]
			}
		case "//go:linkname":
			if len(parts) != 3 || parts[1] != g.Name() {
				continue
			}
			if hasUnsafeImportTC(g.Package().Pkg) {
				info.linkName = parts[2]
			}
		}
	}
}

// hasUnsafeImportTC is the typecheck-side mirror of hasUnsafeImport.
// Requires ssabridge.TranslatePackage / ensurePackage to have populated
// pkg.Imports() — done unconditionally in current ssabridge.
func hasUnsafeImportTC(pkg *typecheck.Package) bool {
	if pkg == nil {
		return false
	}
	for _, imp := range pkg.Imports() {
		if imp != nil && imp.Path() == "unsafe" {
			return true
		}
	}
	return false
}
