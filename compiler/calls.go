package compiler

import (
	"go/types"
	"strconv"

	"moxie/typecheck"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// For a description of the calling convention in prose, see:
// https://moxie.dev/compiler-internals/calling-convention/

// The maximum number of arguments that can be expanded from a single struct. If
// a struct contains more fields, it is passed as a struct without expanding.
const maxFieldsPerParam = 3

// paramInfo contains some information collected about a function parameter,
// useful while declaring or defining a function.
type paramInfo struct {
	llvmType llvm.Type
	name     string     // name, possibly with suffixes for e.g. struct fields
	elemSize uint64     // size of pointer element type, or 0 if this isn't a pointer
	flags    paramFlags // extra flags for this parameter
}

// paramFlags identifies parameter attributes for flags. Most importantly, it
// determines which parameters are dereferenceable_or_null and which aren't.
type paramFlags uint8

const (
	// Whether this is a full or partial Go parameter (int, slice, etc).
	// The extra context parameter is not a Go parameter.
	paramIsGoParam = 1 << iota

	// Whether this is a readonly parameter (for example, a string pointer).
	paramIsReadonly
)

// getRuntimeFunction looks up runtime.<fnName> and returns its LLVM type and
// value. Hides the go/ssa lookup so callers in TC files don't need ssa.Function.
func (b *builder) getRuntimeFunction(fnName string) (llvm.Type, llvm.Value) {
	member := b.program.ImportedPackage("runtime").Members[fnName]
	if member == nil {
		panic("unknown runtime function: " + fnName)
	}
	return b.getFunction(member.(*ssa.Function))
}

// createRuntimeCallCommon creates a runtime call. Use createRuntimeCall or
// createRuntimeInvoke instead.
func (b *builder) createRuntimeCallCommon(fnName string, args []llvm.Value, name string, isInvoke bool) llvm.Value {
	fnType, llvmFn := b.getRuntimeFunction(fnName)
	if llvmFn.IsNil() {
		panic("trying to call non-existent function: runtime." + fnName)
	}
	args = append(args, llvm.Undef(b.dataPtrType)) // unused context parameter
	if isInvoke {
		return b.createInvoke(fnType, llvmFn, args, name)
	}
	return b.createCall(fnType, llvmFn, args, name)
}

// createRuntimeCall creates a new call to runtime.<fnName> with the given
// arguments.
func (b *builder) createRuntimeCall(fnName string, args []llvm.Value, name string) llvm.Value {
	return b.createRuntimeCallCommon(fnName, args, name, false)
}

// createRuntimeInvoke creates a new call to runtime.<fnName> with the given
// arguments. If the runtime call panics, control flow is diverted to the
// landing pad block.
// Note that "invoke" here is meant in the LLVM sense (a call that can
// panic/throw), not in the Go sense (an interface method call).
func (b *builder) createRuntimeInvoke(fnName string, args []llvm.Value, name string) llvm.Value {
	return b.createRuntimeCallCommon(fnName, args, name, true)
}

// createCall creates a call to the given function with the arguments possibly
// expanded.
func (b *builder) createCall(fnType llvm.Type, fn llvm.Value, args []llvm.Value, name string) llvm.Value {
	expanded := make([]llvm.Value, 0, len(args))
	for _, arg := range args {
		fragments := b.expandFormalParam(arg)
		expanded = append(expanded, fragments...)
	}
	// Coerce named↔anonymous struct mismatches (TC path may produce
	// anonymous struct types where the callee expects named ones).
	if paramTypes := fnType.ParamTypes(); len(paramTypes) == len(expanded) {
		for i, pt := range paramTypes {
			if expanded[i].Type() != pt &&
				pt.TypeKind() == llvm.StructTypeKind &&
				expanded[i].Type().TypeKind() == llvm.StructTypeKind {
				expanded[i] = b.coerceStructType(expanded[i], pt)
			}
		}
	}
	call := b.CreateCall(fnType, fn, expanded, name)
	if !fn.IsAFunction().IsNil() {
		if cc := fn.FunctionCallConv(); cc != llvm.CCallConv {
			// Set a different calling convention if needed.
			// This is needed for GetModuleHandleExA on Windows, for example.
			call.SetInstructionCallConv(cc)
		}
	}
	return call
}

// createInvoke is like createCall but continues execution at the landing pad if
// the call resulted in a panic.
func (b *builder) createInvoke(fnType llvm.Type, fn llvm.Value, args []llvm.Value, name string) llvm.Value {
	if b.hasDeferFrame() {
		b.createInvokeCheckpoint()
	}
	return b.createCall(fnType, fn, args, name)
}

// Expand an argument type to a list that can be used in a function call
// parameter list.
func (c *compilerContext) expandFormalParamType(t llvm.Type, name string, goType types.Type) []paramInfo {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fieldInfos := c.flattenAggregateType(t, name, goType)
		if len(fieldInfos) <= maxFieldsPerParam {
			// managed to expand this parameter
			return fieldInfos
		}
		// failed to expand this parameter: too many fields
	}
	// TODO: split small arrays
	return []paramInfo{c.getParamInfo(t, name, goType)}
}

// expandFormalParamOffsets returns a list of offsets from the start of an
// object of type t after it would have been split up by expandFormalParam. This
// is useful for debug information, where it is necessary to know the offset
// from the start of the combined object.
func (b *builder) expandFormalParamOffsets(t llvm.Type) []uint64 {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fields := b.flattenAggregateTypeOffsets(t)
		if len(fields) <= maxFieldsPerParam {
			return fields
		} else {
			// failed to lower
			return []uint64{0}
		}
	default:
		// TODO: split small arrays
		return []uint64{0}
	}
}

// expandFormalParam splits a formal param value into pieces, so it can be
// passed directly as part of a function call. For example, it splits up small
// structs into individual fields. It is the equivalent of expandFormalParamType
// for parameter values.
func (b *builder) expandFormalParam(v llvm.Value) []llvm.Value {
	switch v.Type().TypeKind() {
	case llvm.StructTypeKind:
		fieldInfos := b.flattenAggregateType(v.Type(), "", nil)
		if len(fieldInfos) <= maxFieldsPerParam {
			fields := b.flattenAggregate(v)
			if len(fields) != len(fieldInfos) {
				panic("type and value param lowering don't match")
			}
			return fields
		} else {
			// failed to lower
			return []llvm.Value{v}
		}
	default:
		// TODO: split small arrays
		return []llvm.Value{v}
	}
}

// Try to flatten a struct type to a list of types. Returns a 1-element slice
// with the passed in type if this is not possible.
func (c *compilerContext) flattenAggregateType(t llvm.Type, name string, goType types.Type) []paramInfo {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		var paramInfos []paramInfo
		for i, subfield := range t.StructElementTypes() {
			if c.targetData.TypeAllocSize(subfield) == 0 {
				continue
			}
			suffix := strconv.Itoa(i)
			isString := false
			if goType != nil {
				// Try to come up with a good suffix for this struct field,
				// depending on which Go type it's based on.
				switch goType := goType.Underlying().(type) {
				case *types.Interface:
					suffix = []string{"typecode", "value"}[i]
				case *types.Slice:
					suffix = []string{"data", "len", "cap"}[i]
				case *types.Struct:
					suffix = goType.Field(i).Name()
				case *types.Basic:
					switch goType.Kind() {
					case types.Complex64, types.Complex128:
						suffix = []string{"r", "i"}[i]
					case types.String:
						suffix = []string{"data", "len", "cap"}[i]
						// Moxie: don't mark string data as readonly (string=[]byte, mutable).
					}
				case *types.Signature:
					suffix = []string{"context", "funcptr"}[i]
				}
			}
			subInfos := c.flattenAggregateType(subfield, name+"."+suffix, extractSubfield(goType, i))
			if isString {
				subInfos[0].flags |= paramIsReadonly
			}
			paramInfos = append(paramInfos, subInfos...)
		}
		return paramInfos
	default:
		return []paramInfo{c.getParamInfo(t, name, goType)}
	}
}

// getParamInfo collects information about a parameter. For example, if this
// parameter is pointer-like, it will also store the element type for the
// dereferenceable_or_null attribute.
func (c *compilerContext) getParamInfo(t llvm.Type, name string, goType types.Type) paramInfo {
	info := paramInfo{
		llvmType: t,
		name:     name,
		flags:    paramIsGoParam,
	}
	if goType != nil {
		switch underlying := goType.Underlying().(type) {
		case *types.Pointer:
			// Pointers in Go must either point to an object or be nil.
			info.elemSize = c.targetData.TypeAllocSize(c.getLLVMType(underlying.Elem()))
		case *types.Chan:
			// Channels are implemented simply as a *runtime.channel.
			info.elemSize = c.targetData.TypeAllocSize(c.getLLVMRuntimeType("channel"))
		case *types.Map:
			// Maps are similar to channels: they are implemented as a
			// *runtime.hashmap.
			info.elemSize = c.targetData.TypeAllocSize(c.getLLVMRuntimeType("hashmap"))
		}
	}
	return info
}

// extractSubfield extracts a field from a struct, or returns null if this is
// not a struct and thus no subfield can be obtained.
func extractSubfield(t types.Type, field int) types.Type {
	if t == nil {
		return nil
	}
	switch t := t.Underlying().(type) {
	case *types.Struct:
		return t.Field(field).Type()
	case *types.Interface, *types.Slice, *types.Basic, *types.Signature:
		// These Go types are (sometimes) implemented as LLVM structs but can't
		// really be split further up in Go (with the possible exception of
		// complex numbers).
		return nil
	default:
		// This should be unreachable.
		panic("cannot split subfield: " + t.String())
	}
}

// flattenAggregateTypeOffsets returns the offsets from the start of an object of
// type t if this object were flattened like in flattenAggregate. Used together
// with flattenAggregate to know the start indices of each value in the
// non-flattened object.
//
// Note: this is an implementation detail, use expandFormalParamOffsets instead.
func (c *compilerContext) flattenAggregateTypeOffsets(t llvm.Type) []uint64 {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		var fields []uint64
		for fieldIndex, field := range t.StructElementTypes() {
			if c.targetData.TypeAllocSize(field) == 0 {
				continue
			}
			suboffsets := c.flattenAggregateTypeOffsets(field)
			offset := c.targetData.ElementOffset(t, fieldIndex)
			for i := range suboffsets {
				suboffsets[i] += offset
			}
			fields = append(fields, suboffsets...)
		}
		return fields
	default:
		return []uint64{0}
	}
}

// flattenAggregate breaks down a struct into its elementary values for argument
// passing. It is the value equivalent of flattenAggregateType
func (b *builder) flattenAggregate(v llvm.Value) []llvm.Value {
	switch v.Type().TypeKind() {
	case llvm.StructTypeKind:
		var fields []llvm.Value
		for i, field := range v.Type().StructElementTypes() {
			if b.targetData.TypeAllocSize(field) == 0 {
				continue
			}
			subfield := b.CreateExtractValue(v, i, "")
			subfields := b.flattenAggregate(subfield)
			fields = append(fields, subfields...)
		}
		return fields
	default:
		return []llvm.Value{v}
	}
}

// collapseFormalParam combines an aggregate object back into the original
// value. This is used to join multiple LLVM parameters into a single Go value
// in the function entry block.
func (b *builder) collapseFormalParam(t llvm.Type, fields []llvm.Value) llvm.Value {
	param, remaining := b.collapseFormalParamInternal(t, fields)
	if len(remaining) != 0 {
		panic("failed to expand back all fields")
	}
	return param
}

// collapseFormalParamInternal is an implementation detail of
// collapseFormalParam: it works by recursing until there are no fields left.
func (b *builder) collapseFormalParamInternal(t llvm.Type, fields []llvm.Value) (llvm.Value, []llvm.Value) {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		flattened := b.flattenAggregateType(t, "", nil)
		if len(flattened) <= maxFieldsPerParam {
			value := llvm.ConstNull(t)
			for i, subtyp := range t.StructElementTypes() {
				if b.targetData.TypeAllocSize(subtyp) == 0 {
					continue
				}
				structField, remaining := b.collapseFormalParamInternal(subtyp, fields)
				fields = remaining
				value = b.CreateInsertValue(value, structField, i, "")
			}
			return value, fields
		} else {
			// this struct was not flattened
			return fields[0], fields[1:]
		}
	default:
		return fields[0], fields[1:]
	}
}

// ─── TC variants (use typecheck.Type instead of go/types.Type) ───────────────

// expandFormalParamTypeTC is the typecheck variant of expandFormalParamType.
// Mirrors the SSA structure: try to flatten small structs, fall back to a
// single paramInfo for everything else.
func (c *compilerContext) expandFormalParamTypeTC(t llvm.Type, name string, mxType typecheck.Type) []paramInfo {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fieldInfos := c.flattenAggregateTypeTC(t, name, mxType)
		if len(fieldInfos) <= maxFieldsPerParam {
			return fieldInfos
		}
	}
	return []paramInfo{c.getParamInfoTC(t, name, mxType)}
}

// flattenAggregateTypeTC is the typecheck variant of flattenAggregateType.
// Names struct subfields based on the typecheck type when available
// (matching SSA's behavior for interface/slice/struct/string/signature).
func (c *compilerContext) flattenAggregateTypeTC(t llvm.Type, name string, mxType typecheck.Type) []paramInfo {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		var paramInfos []paramInfo
		for i, subfield := range t.StructElementTypes() {
			if c.targetData.TypeAllocSize(subfield) == 0 {
				continue
			}
			suffix := strconv.Itoa(i)
			if mxType != nil {
				switch mxt := mxType.Underlying().(type) {
				case *typecheck.Interface:
					suffix = []string{"typecode", "value"}[i]
				case *typecheck.Slice:
					suffix = []string{"data", "len", "cap"}[i]
				case *typecheck.Struct:
					suffix = mxt.Field(i).Name()
				case *typecheck.Basic:
					if mxt.Info()&typecheck.IsString != 0 {
						suffix = []string{"data", "len", "cap"}[i]
					}
				case *typecheck.Signature:
					suffix = []string{"context", "funcptr"}[i]
				}
			}
			subInfos := c.flattenAggregateTypeTC(subfield, name+"."+suffix, extractSubfieldTC(mxType, i))
			paramInfos = append(paramInfos, subInfos...)
		}
		return paramInfos
	default:
		return []paramInfo{c.getParamInfoTC(t, name, mxType)}
	}
}

// getParamInfoTC is the typecheck variant of getParamInfo. Sets elemSize for
// pointer-like types (pointer/chan/map) so LLVM can apply the
// dereferenceable_or_null attribute.
func (c *compilerContext) getParamInfoTC(t llvm.Type, name string, mxType typecheck.Type) paramInfo {
	info := paramInfo{
		llvmType: t,
		name:     name,
		flags:    paramIsGoParam,
	}
	if mxType != nil {
		switch underlying := mxType.Underlying().(type) {
		case *typecheck.Pointer:
			info.elemSize = c.targetData.TypeAllocSize(c.getLLVMTypeTC(underlying.Elem()))
		case *typecheck.Chan:
			info.elemSize = c.targetData.TypeAllocSize(c.getLLVMRuntimeType("channel"))
		case *typecheck.Map:
			info.elemSize = c.targetData.TypeAllocSize(c.getLLVMRuntimeType("hashmap"))
		}
	}
	return info
}

// extractSubfieldTC is the typecheck variant of extractSubfield. Returns the
// element type of a struct field, or nil for types that can't be split.
func extractSubfieldTC(t typecheck.Type, field int) typecheck.Type {
	if t == nil {
		return nil
	}
	switch t := t.Underlying().(type) {
	case *typecheck.Struct:
		return t.Field(field).Type()
	case *typecheck.Interface, *typecheck.Slice, *typecheck.Basic, *typecheck.Signature:
		return nil
	default:
		return nil
	}
}
