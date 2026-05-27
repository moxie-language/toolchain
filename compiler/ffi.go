package compiler

import (
	"moxie/loader"
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// isFFIFunction returns true if fn is an mxh FFI function backed by dlopen/dlsym.
func (c *compilerContext) isFFIFunction(fn *ssa.Function) bool {
	if fn.Package() == nil || fn.Package().Pkg == nil {
		return false
	}
	return loader.MXHLibrary(fn.Package().Pkg.Path()) != ""
}

// createFFIFunction generates a function body that resolves the symbol
// via dlopen/dlsym at first call, then dispatches through the function pointer.
func (c *compilerContext) createFFIFunction(fn *ssa.Function, llvmFn llvm.Value) {
	pkgPath := fn.Package().Pkg.Path()
	libPath := loader.MXHLibrary(pkgPath)
	symName := loader.MXHSymbol(pkgPath, fn.Name())
	if symName == "" {
		symName = fn.Name()
	}

	// Create globals for the library handle and function pointer.
	globalPrefix := "_mxh_" + fn.Name()
	libHandleGlobal := c.getOrCreateFFILibHandle(pkgPath, libPath)
	fptrGlobal := llvm.AddGlobal(c.mod, c.dataPtrType, globalPrefix+"_fptr")
	fptrGlobal.SetInitializer(llvm.ConstNull(c.dataPtrType))
	fptrGlobal.SetLinkage(llvm.InternalLinkage)

	fnType := llvmFn.GlobalValueType()
	llvmFn.SetLinkage(llvm.LinkOnceODRLinkage)
	llvmFn.SetUnnamedAddr(true)

	// Build the function body.
	entry := c.ctx.AddBasicBlock(llvmFn, "entry")
	resolveBlock := c.ctx.AddBasicBlock(llvmFn, "resolve")
	callBlock := c.ctx.AddBasicBlock(llvmFn, "call")

	irbuilder := c.ctx.NewBuilder()
	defer irbuilder.Dispose()

	// entry: check if function pointer is resolved
	irbuilder.SetInsertPointAtEnd(entry)
	fptr := irbuilder.CreateLoad(c.dataPtrType, fptrGlobal, "fptr")
	isNull := irbuilder.CreateICmp(llvm.IntEQ, fptr, llvm.ConstNull(c.dataPtrType), "isnull")
	irbuilder.CreateCondBr(isNull, resolveBlock, callBlock)

	// resolve: call dlopen + dlsym, store function pointer
	irbuilder.SetInsertPointAtEnd(resolveBlock)
	libHandle := irbuilder.CreateLoad(c.dataPtrType, libHandleGlobal, "libh")
	libIsNull := irbuilder.CreateICmp(llvm.IntEQ, libHandle, llvm.ConstNull(c.dataPtrType), "libnull")

	resolveLibBlock := c.ctx.AddBasicBlock(llvmFn, "resolve.lib")
	resolveSym := c.ctx.AddBasicBlock(llvmFn, "resolve.sym")
	irbuilder.CreateCondBr(libIsNull, resolveLibBlock, resolveSym)

	// resolve.lib: dlopen the library
	irbuilder.SetInsertPointAtEnd(resolveLibBlock)
	dlopenFn := c.getOrDeclareDlopen()
	libPathStr := c.createCString(libPath)
	rtldNow := llvm.ConstInt(c.ctx.Int32Type(), 2, false) // RTLD_NOW
	newHandle := irbuilder.CreateCall(dlopenFn.GlobalValueType(), dlopenFn, []llvm.Value{libPathStr, rtldNow}, "newlib")
	irbuilder.CreateStore(newHandle, libHandleGlobal)
	irbuilder.CreateBr(resolveSym)

	// resolve.sym: dlsym the function
	irbuilder.SetInsertPointAtEnd(resolveSym)
	libHandlePhi := irbuilder.CreatePHI(c.dataPtrType, "lib.phi")
	libHandlePhi.AddIncoming([]llvm.Value{libHandle, newHandle}, []llvm.BasicBlock{resolveBlock, resolveLibBlock})

	dlsymFn := c.getOrDeclareDlsym()
	symStr := c.createCString(symName)
	resolvedFptr := irbuilder.CreateCall(dlsymFn.GlobalValueType(), dlsymFn, []llvm.Value{libHandlePhi, symStr}, "sym")
	irbuilder.CreateStore(resolvedFptr, fptrGlobal)
	irbuilder.CreateBr(callBlock)

	// call: invoke through function pointer
	irbuilder.SetInsertPointAtEnd(callBlock)
	fptrPhi := irbuilder.CreatePHI(c.dataPtrType, "fn.phi")
	fptrPhi.AddIncoming([]llvm.Value{fptr, resolvedFptr}, []llvm.BasicBlock{entry, resolveSym})

	// Collect parameters (skip the Go context param if present).
	var params []llvm.Value
	paramCount := llvmFn.ParamsCount()
	for i := 0; i < paramCount; i++ {
		params = append(params, llvmFn.Param(i))
	}

	// Build the C function type (no context param).
	var cParamTypes []llvm.Type
	for _, p := range params {
		cParamTypes = append(cParamTypes, p.Type())
	}
	cFnType := llvm.FunctionType(fnType.ReturnType(), cParamTypes, false)

	result := irbuilder.CreateCall(cFnType, fptrPhi, params, "")
	if fnType.ReturnType().TypeKind() == llvm.VoidTypeKind {
		irbuilder.CreateRetVoid()
	} else {
		irbuilder.CreateRet(result)
	}
}

func (c *compilerContext) getOrCreateFFILibHandle(pkgPath, libPath string) llvm.Value {
	name := "_mxh_lib_" + pkgPath
	g := c.mod.NamedGlobal(name)
	if !g.IsNil() {
		return g
	}
	g = llvm.AddGlobal(c.mod, c.dataPtrType, name)
	g.SetInitializer(llvm.ConstNull(c.dataPtrType))
	g.SetLinkage(llvm.InternalLinkage)
	return g
}

func (c *compilerContext) getOrDeclareDlopen() llvm.Value {
	fn := c.mod.NamedFunction("dlopen")
	if !fn.IsNil() {
		return fn
	}
	fnType := llvm.FunctionType(c.dataPtrType, []llvm.Type{c.dataPtrType, c.ctx.Int32Type()}, false)
	return llvm.AddFunction(c.mod, "dlopen", fnType)
}

func (c *compilerContext) getOrDeclareDlsym() llvm.Value {
	fn := c.mod.NamedFunction("dlsym")
	if !fn.IsNil() {
		return fn
	}
	fnType := llvm.FunctionType(c.dataPtrType, []llvm.Type{c.dataPtrType, c.dataPtrType}, false)
	return llvm.AddFunction(c.mod, "dlsym", fnType)
}

func (c *compilerContext) createCString(s string) llvm.Value {
	data := s + "\x00"
	globalType := llvm.ArrayType(c.ctx.Int8Type(), len(data))
	global := llvm.AddGlobal(c.mod, globalType, ".ffi.str")
	global.SetInitializer(c.ctx.ConstString(data, false))
	global.SetLinkage(llvm.InternalLinkage)
	global.SetGlobalConstant(true)
	global.SetUnnamedAddr(true)
	global.SetAlignment(1)
	zero := llvm.ConstInt(c.ctx.Int32Type(), 0, false)
	return llvm.ConstInBoundsGEP(globalType, global, []llvm.Value{zero, zero})
}

