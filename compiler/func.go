package compiler

// This file implements function values and closures.

import (
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

	_, fn := b.getFunction(f)
	return b.createFuncValue(fn, context), nil
}
