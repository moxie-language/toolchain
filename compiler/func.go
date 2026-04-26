package compiler

// This file implements function values and closures.

import (
	"go/types"

	"moxie/mxssa"
	"tinygo.org/x/go-llvm"
)

// createFuncValue creates a function value from a raw function pointer with no
// context. The closure layout is {context, function pointer}.
func (b *builder) createFuncValue(funcPtr, context llvm.Value) llvm.Value {
	funcValueType := b.funcValueType()
	funcValue := llvm.Undef(funcValueType)
	funcValue = b.CreateInsertValue(funcValue, context, 0, "")
	funcValue = b.CreateInsertValue(funcValue, funcPtr, 1, "")
	return funcValue
}

// funcValueType returns the LLVM struct type used for all func values:
// {context, function pointer}. The Go signature has no impact on this layout.
func (c *compilerContext) funcValueType() llvm.Type {
	return c.ctx.StructType([]llvm.Type{c.dataPtrType, c.funcPtrType}, false)
}

// extractFuncScalar returns some scalar that can be used in comparisons. It is
// a cheap operation.
func (b *builder) extractFuncScalar(funcValue llvm.Value) llvm.Value {
	return b.CreateExtractValue(funcValue, 1, "")
}

// extractFuncContext extracts the context pointer from this function value. It
// is a cheap operation.
func (b *builder) extractFuncContext(funcValue llvm.Value) llvm.Value {
	return b.CreateExtractValue(funcValue, 0, "")
}

// decodeFuncValue extracts the context and the function pointer from this func
// value.
func (b *builder) decodeFuncValue(funcValue llvm.Value) (funcPtr, context llvm.Value) {
	context = b.CreateExtractValue(funcValue, 0, "")
	funcPtr = b.CreateExtractValue(funcValue, 1, "")
	return
}

// getLLVMFunctionType returns a LLVM function type for a given signature.
func (c *compilerContext) getLLVMFunctionType(typ *types.Signature) llvm.Type {
	// Get the return type.
	var returnType llvm.Type
	switch typ.Results().Len() {
	case 0:
		// No return values.
		returnType = c.ctx.VoidType()
	case 1:
		// Just one return value.
		returnType = c.getLLVMType(typ.Results().At(0).Type())
	default:
		// Multiple return values. Put them together in a struct.
		// This appears to be the common way to handle multiple return values in
		// LLVM.
		members := make([]llvm.Type, typ.Results().Len())
		for i := 0; i < typ.Results().Len(); i++ {
			members[i] = c.getLLVMType(typ.Results().At(i).Type())
		}
		returnType = c.ctx.StructType(members, false)
	}

	// Get the parameter types.
	var paramTypes []llvm.Type
	if typ.Recv() != nil {
		recv := c.getLLVMType(typ.Recv().Type())
		if recv.StructName() == "runtime._interface" {
			// This is a call on an interface, not a concrete type.
			// The receiver is not an interface, but a i8* type.
			recv = c.dataPtrType
		}
		for _, info := range c.expandFormalParamTypeTC(recv, "", nil) {
			paramTypes = append(paramTypes, info.llvmType)
		}
	}
	for i := 0; i < typ.Params().Len(); i++ {
		subType := c.getLLVMType(typ.Params().At(i).Type())
		for _, info := range c.expandFormalParamTypeTC(subType, "", nil) {
			paramTypes = append(paramTypes, info.llvmType)
		}
	}
	// All functions take these parameters at the end.
	paramTypes = append(paramTypes, c.dataPtrType) // context

	// Make a func type out of the signature.
	return llvm.FunctionType(returnType, paramTypes, false)
}

// parseMakeClosure makes a function value (with context) from the given
// closure expression.

func (b *builder) parseMakeClosureTC(expr *mxssa.MakeClosure) (llvm.Value, error) {
	if len(expr.Bindings) == 0 {
		panic("unexpected: MakeClosure without bound variables")
	}
	f := expr.Fn.(*mxssa.Function)

	boundVars := make([]llvm.Value, len(expr.Bindings))
	for i, binding := range expr.Bindings {
		llvmBoundVar := b.getValueTC(binding, expr.Pos())
		boundVars[i] = llvmBoundVar
	}

	context := b.emitPointerPack(boundVars)

	goFn := b.mxTranslator.ReverseFunction(f)
	if goFn == nil {
		panic("parseMakeClosureTC: cannot reverse-bridge function: " + f.Name())
	}
	_, fn := b.getFunction(goFn)
	return b.createFuncValue(fn, context), nil
}
