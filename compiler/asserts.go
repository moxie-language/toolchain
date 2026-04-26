package compiler

// This file implements functions that do certain safety checks that are
// required by the Go programming language.

import (
	"fmt"
	"go/token"

	"moxie/typecheck"

	"tinygo.org/x/go-llvm"
)

// createLookupBoundsCheck emits a bounds check before doing a lookup into a
// slice. This is required by the Go language spec: an index out of bounds must
// cause a panic.
// The caller should make sure that index is at least as big as arrayLen.
func (b *builder) createLookupBoundsCheck(arrayLen, index llvm.Value) {
	if b.info.nobounds {
		// The //go:nobounds pragma was added to the function to avoid bounds
		// checking.
		return
	}

	// Extend arrayLen if it's too small.
	if index.Type().IntTypeWidth() > arrayLen.Type().IntTypeWidth() {
		// The index is bigger than the array length type, so extend it.
		arrayLen = b.CreateZExt(arrayLen, index.Type(), "")
	}

	// Now do the bounds check: index >= arrayLen
	outOfBounds := b.CreateICmp(llvm.IntUGE, index, arrayLen, "")
	b.createRuntimeAssert(outOfBounds, "lookup", "lookupPanic")
}

// createSliceBoundsCheck emits a bounds check before a slicing operation to make
// sure it is within bounds.
//
// This function is both used for slicing a slice (low and high have their
// normal meaning) and for creating a new slice, where 'capacity' means the
// biggest possible slice capacity, 'low' means len and 'high' means cap. The
// logic is the same in both cases.

// createSliceToArrayPointerCheck adds a check for slice-to-array pointer
// conversions. This conversion was added in Go 1.17. For details, see:
// https://tip.golang.org/ref/spec#Conversions_from_slice_to_array_pointer
func (b *builder) createSliceToArrayPointerCheck(sliceLen llvm.Value, arrayLen int64) {
	// From the spec:
	// > If the length of the slice is less than the length of the array, a
	// > run-time panic occurs.
	arrayLenValue := llvm.ConstInt(b.uintptrType, uint64(arrayLen), false)
	isLess := b.CreateICmp(llvm.IntULT, sliceLen, arrayLenValue, "")
	b.createRuntimeAssert(isLess, "slicetoarray", "sliceToArrayPointerPanic")
}

// createUnsafeSliceStringCheck inserts a runtime check used for unsafe.Slice
// and unsafe.String. This function must panic if the ptr/len parameters are
// invalid.

// createChanBoundsCheck creates a bounds check before creating a new channel to
// check that the value is not too big for runtime.chanMake.

// createNilCheck checks whether the given pointer is nil, and panics if it is.
// It has no effect in well-behaved programs, but makes sure no uncaught nil
// pointer dereferences exist in valid Go code.

// createNegativeShiftCheck creates an assertion that panics if the given shift value is negative.
// This function assumes that the shift value is signed.
func (b *builder) createNegativeShiftCheck(shift llvm.Value) {
	if b.info.nobounds {
		// Function disabled bounds checking - skip shift check.
		return
	}

	// isNegative = shift < 0
	isNegative := b.CreateICmp(llvm.IntSLT, shift, llvm.ConstInt(shift.Type(), 0, false), "")
	b.createRuntimeAssert(isNegative, "shift", "negativeShiftPanic")
}

// createDivideByZeroCheck asserts that y is not zero. If it is, a runtime panic
// will be emitted. This follows the Go specification which says that a divide
// by zero must cause a run time panic.
func (b *builder) createDivideByZeroCheck(y llvm.Value) {
	if b.info.nobounds {
		return
	}

	// isZero = y == 0
	isZero := b.CreateICmp(llvm.IntEQ, y, llvm.ConstInt(y.Type(), 0, false), "")
	b.createRuntimeAssert(isZero, "divbyzero", "divideByZeroPanic")
}

// createRuntimeAssert is a common function to create a new branch on an assert
// bool, calling an assert func if the assert value is true (1).
func (b *builder) createRuntimeAssert(assert llvm.Value, blockPrefix, assertFunc string) {
	// Check whether we can resolve this check at compile time.
	if !assert.IsAConstantInt().IsNil() {
		val := assert.ZExtValue()
		if val == 0 {
			// Everything is constant so the check does not have to be emitted
			// in IR. This avoids emitting some redundant IR.
			return
		}
	}

	// Put the fault block at the end of the function and the next block at the
	// current insert position.
	faultBlock := b.ctx.AddBasicBlock(b.llvmFn, blockPrefix+".throw")
	nextBlock := b.insertBasicBlock(blockPrefix + ".next")
	b.currentBlockInfo.exit = nextBlock // adjust outgoing block for phi nodes

	// Now branch to the out-of-bounds or the regular block.
	b.CreateCondBr(assert, faultBlock, nextBlock)

	// Fail: the assert triggered so panic.
	b.SetInsertPointAtEnd(faultBlock)
	b.createRuntimeCall(assertFunc, nil, "")
	b.CreateUnreachable()

	// Ok: assert didn't trigger so continue normally.
	b.SetInsertPointAtEnd(nextBlock)
}


func (b *builder) extendIntegerTC(value llvm.Value, valueType typecheck.Type, targetType llvm.Type) llvm.Value {
	if value.Type().IntTypeWidth() < targetType.IntTypeWidth() {
		if valueType.Underlying().(*typecheck.Basic).Info()&typecheck.IsUnsigned != 0 {
			value = b.CreateZExt(value, targetType, "")
		} else {
			value = b.CreateSExt(value, targetType, "")
		}
	}
	return value
}

func (b *builder) createSliceBoundsCheckTC(capacity, low, high, max llvm.Value, lowType, highType, maxType *typecheck.Basic) {
	if b.info.nobounds {
		return
	}
	capacityType := capacity.Type()
	if low.Type().IntTypeWidth() > capacityType.IntTypeWidth() {
		capacityType = low.Type()
	}
	if high.Type().IntTypeWidth() > capacityType.IntTypeWidth() {
		capacityType = high.Type()
	}
	if max.Type().IntTypeWidth() > capacityType.IntTypeWidth() {
		capacityType = max.Type()
	}
	if capacityType != capacity.Type() {
		capacity = b.CreateZExt(capacity, capacityType, "")
	}
	low = b.extendIntegerTC(low, lowType, capacityType)
	high = b.extendIntegerTC(high, highType, capacityType)
	max = b.extendIntegerTC(max, maxType, capacityType)
	outOfBounds1 := b.CreateICmp(llvm.IntUGT, low, high, "slice.lowhigh")
	outOfBounds2 := b.CreateICmp(llvm.IntUGT, high, max, "slice.highmax")
	outOfBounds3 := b.CreateICmp(llvm.IntUGT, max, capacity, "slice.maxcap")
	outOfBounds := b.CreateOr(outOfBounds1, outOfBounds2, "slice.lowmax")
	outOfBounds = b.CreateOr(outOfBounds, outOfBounds3, "slice.lowcap")
	b.createRuntimeAssert(outOfBounds, "slice", "slicePanic")
}

func (b *builder) createUnsafeSliceStringCheckTC(name string, ptr, length llvm.Value, elementType llvm.Type, lenType *typecheck.Basic) {
	length = b.extendIntegerTC(length, lenType, b.uintptrType)
	maxSize := b.maxSliceSize(elementType)
	maxSizeValue := llvm.ConstInt(length.Type(), maxSize, false)
	zero := llvm.ConstInt(length.Type(), 0, false)
	lenOutOfBounds := b.CreateICmp(llvm.IntUGT, length, maxSizeValue, "")
	ptrIsNil := b.CreateICmp(llvm.IntEQ, ptr, llvm.ConstNull(ptr.Type()), "")
	lenIsNotZero := b.CreateICmp(llvm.IntNE, length, zero, "")
	assert := b.CreateAnd(ptrIsNil, lenIsNotZero, "")
	assert = b.CreateOr(assert, lenOutOfBounds, "")
	b.createRuntimeAssert(assert, name, "unsafeSlicePanic")
}

func (b *builder) createChanBoundsCheckTC(elementSize uint64, bufSize llvm.Value, bufSizeType *typecheck.Basic, pos token.Pos) {
	if b.info.nobounds {
		return
	}
	bufSize = b.extendIntegerTC(bufSize, bufSizeType, b.uintptrType)
	maxBufSize := b.CreateLShr(llvm.ConstNot(llvm.ConstInt(b.uintptrType, 0, false)), llvm.ConstInt(b.uintptrType, 1, false), "")
	if elementSize > maxBufSize.ZExtValue() {
		b.addError(pos, fmt.Sprintf("channel element type is too big (%v bytes)", elementSize))
		return
	}
	if elementSize == 0 {
		elementSize = 1
	}
	maxBufSize = b.CreateUDiv(maxBufSize, llvm.ConstInt(b.uintptrType, elementSize, false), "")
	if maxBufSize.Type() != bufSize.Type() {
		maxBufSize = b.CreateZExt(maxBufSize, bufSize.Type(), "")
	}
	bufSizeTooBig := b.CreateICmp(llvm.IntUGE, bufSize, maxBufSize, "")
	b.createRuntimeAssert(bufSizeTooBig, "chan", "chanMakePanic")
}
