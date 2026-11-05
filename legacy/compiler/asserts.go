package compiler

// This file implements functions that do certain safety checks that are
// required by the Go programming language.

import (
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// addressIsElementAccess reports whether an address is used to load or store
// an element, directly or through a field/element chain (out[i].f = v). Only
// then is an index bounds check required: forming an address is pointer
// arithmetic and is deliberately unchecked.
func addressIsElementAccess(v ssa.Value) bool {
	for _, ref := range *v.Referrers() {
		switch ref := ref.(type) {
		case *ssa.UnOp:
			// A load is a dereference (token.MUL) of the address.
			if ref.Op == token.MUL {
				return true
			}
		case *ssa.Store:
			return true
		case *ssa.FieldAddr:
			if addressIsElementAccess(ref) {
				return true
			}
		case *ssa.IndexAddr:
			if addressIsElementAccess(ref) {
				return true
			}
		}
	}
	return false
}

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

	// Both operands are unsigned: the caller has already sign-extended a
	// signed index, so a negative index compares as a very large unsigned
	// value and is caught by the same test.
	if arrayLen.Type().IntTypeWidth() < index.Type().IntTypeWidth() {
		arrayLen = b.CreateZExt(arrayLen, index.Type(), "")
	}

	// Moxie: an index store into slice capacity past len used to write
	// silently and leave len unchanged, so the value vanished on the next
	// push. A silent wrong answer is worse than a trap: fail loud.
	outOfBounds := b.CreateICmp(llvm.IntUGE, index, arrayLen, "")
	b.createRuntimeAssert(outOfBounds, "lookup", "lookupPanic")
}

// clampSliceLow folds the low bound into [0, length] and never above high.
// Moxie emits no slice bounds check - the programmer owns the indices - but an
// exhausted cursor (`s[1:]` on an empty slice) must not produce a slice with a
// negative length: the tree's `for ; len(r) > 0; r = r[1:]` loops leave `r`
// empty and the `if len(r) == 0` guards that follow have to fire. A valid
// slice (0 <= low <= high <= length) is untouched.
func (b *builder) clampSliceLow(low, high, length llvm.Value) llvm.Value {
	cmp := b.CreateICmp(llvm.IntULT, low, length, "")
	low = b.CreateSelect(cmp, low, length, "")
	cmp = b.CreateICmp(llvm.IntULT, low, high, "")
	return b.CreateSelect(cmp, low, high, "")
}

// createSliceBoundsCheck emits a bounds check before a slicing operation to make
// sure it is within bounds.
//
// This function is both used for slicing a slice (low and high have their
// normal meaning) and for creating a new slice, where 'capacity' means the
// biggest possible slice capacity, 'low' means len and 'high' means cap. The
// logic is the same in both cases.
func (b *builder) createSliceBoundsCheck(capacity, low, high, max llvm.Value, lowType, highType, maxType *types.Basic) {
	// Moxie: no slice bounds checks. The programmer is responsible for
	// ensuring slice indices are within bounds. This function is kept as
	// a no-op to avoid changing all the call sites.
}

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
func (b *builder) createUnsafeSliceStringCheck(name string, ptr, len llvm.Value, elementType llvm.Type, lenType *types.Basic) {
	// From the documentation of unsafe.Slice and unsafe.String:
	//   > At run time, if len is negative, or if ptr is nil and len is not
	//   > zero, a run-time panic occurs.
	// However, in practice, it is also necessary to check that the length is
	// not too big that a GEP wouldn't be possible without wrapping the pointer.
	// These two checks (non-negative and not too big) can be merged into one
	// using an unsigned greater than.

	// Make sure the len value is at least as big as a uintptr.
	len = b.extendInteger(len, lenType, b.uintptrType)

	// Determine the maximum slice size, and therefore the maximum value of the
	// len parameter.
	maxSize := b.maxSliceSize(elementType)
	maxSizeValue := llvm.ConstInt(len.Type(), maxSize, false)

	// Do the check. By using unsigned greater than for the length check, signed
	// negative values are also checked (which are very large numbers when
	// interpreted as signed values).
	zero := llvm.ConstInt(len.Type(), 0, false)
	lenOutOfBounds := b.CreateICmp(llvm.IntUGT, len, maxSizeValue, "")
	ptrIsNil := b.CreateICmp(llvm.IntEQ, ptr, llvm.ConstNull(ptr.Type()), "")
	lenIsNotZero := b.CreateICmp(llvm.IntNE, len, zero, "")
	assert := b.CreateAnd(ptrIsNil, lenIsNotZero, "")
	assert = b.CreateOr(assert, lenOutOfBounds, "")
	b.createRuntimeAssert(assert, name, "unsafeSlicePanic")
}

// createChanBoundsCheck creates a bounds check before creating a new channel to
// check that the value is not too big for runtime.chanMake.
func (b *builder) createChanBoundsCheck(elementSize uint64, bufSize llvm.Value, bufSizeType *types.Basic, pos token.Pos) {
	if b.info.nobounds {
		// The //go:nobounds pragma was added to the function to avoid bounds
		// checking.
		return
	}

	// Make sure bufSize is at least as big as maxBufSize (an uintptr).
	bufSize = b.extendInteger(bufSize, bufSizeType, b.uintptrType)

	// Calculate (^uintptr(0)) >> 1, which is the max value that fits in an
	// uintptr if uintptrs were signed.
	maxBufSize := b.CreateLShr(llvm.ConstNot(llvm.ConstInt(b.uintptrType, 0, false)), llvm.ConstInt(b.uintptrType, 1, false), "")
	if elementSize > maxBufSize.ZExtValue() {
		b.addError(pos, fmt.Sprintf("channel element type is too big (%v bytes)", elementSize))
		return
	}
	// Avoid divide-by-zero.
	if elementSize == 0 {
		elementSize = 1
	}
	// Make the maxBufSize actually the maximum allowed value (in number of
	// elements in the channel buffer).
	maxBufSize = b.CreateUDiv(maxBufSize, llvm.ConstInt(b.uintptrType, elementSize, false), "")

	// Make sure maxBufSize has the same type as bufSize.
	if maxBufSize.Type() != bufSize.Type() {
		maxBufSize = b.CreateZExt(maxBufSize, bufSize.Type(), "")
	}

	// Do the check for a too large (or negative) buffer size.
	bufSizeTooBig := b.CreateICmp(llvm.IntUGE, bufSize, maxBufSize, "")
	b.createRuntimeAssert(bufSizeTooBig, "chan", "chanMakePanic")
}

// createNilCheck checks whether the given pointer is nil, and panics if it is.
// It has no effect in well-behaved programs, but makes sure no uncaught nil
// pointer dereferences exist in valid Go code.
func (b *builder) createNilCheck(inst ssa.Value, ptr llvm.Value, blockPrefix string) {
	// Check whether we need to emit this check at all.
	if !ptr.IsAGlobalValue().IsNil() {
		return
	}

	switch inst := inst.(type) {
	case *ssa.Alloc:
		// An alloc is never nil.
		return
	case *ssa.FreeVar:
		// A free variable is allocated in a parent function and is thus never
		// nil.
		return
	case *ssa.IndexAddr:
		// This pointer is the result of an index operation into a slice or
		// array. Such slices/arrays are already bounds checked so the pointer
		// must be a valid (non-nil) pointer. No nil checking is necessary.
		return
	case *ssa.Convert:
		// This is a pointer that comes from a conversion from unsafe.Pointer.
		// Don't do nil checking because this is unsafe code and the code should
		// know what it is doing.
		// Note: all *ssa.Convert instructions that result in a pointer must
		// come from unsafe.Pointer. Testing here for unsafe.Pointer to be sure.
		if inst.X.Type() == types.Typ[types.UnsafePointer] {
			return
		}
	}

	// Compare against nil.
	// We previously used a hack to make sure this wouldn't break escape
	// analysis, but this is not necessary anymore since
	// https://reviews.llvm.org/D60047 has been merged.
	nilptr := llvm.ConstPointerNull(ptr.Type())
	isnil := b.CreateICmp(llvm.IntEQ, ptr, nilptr, "")

	// Emit the nil check in IR.
	b.createRuntimeAssert(isnil, blockPrefix, "nilPanic")
}

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

// extendInteger extends the value to at least targetType using a zero or sign
// extend. The resulting value is not truncated: it may still be bigger than
// targetType.
func (b *builder) extendInteger(value llvm.Value, valueType types.Type, targetType llvm.Type) llvm.Value {
	w := value.Type().IntTypeWidth()
	tw := targetType.IntTypeWidth()
	if w < tw {
		if valueType.Underlying().(*types.Basic).Info()&types.IsUnsigned != 0 {
			// Unsigned, so zero-extend to the target type.
			value = b.CreateZExt(value, targetType, "")
		} else {
			// Signed, so sign-extend to the target type.
			value = b.CreateSExt(value, targetType, "")
		}
	} else if w > tw {
		// A 64-bit index on a 32-bit target. Truncating here, where the value
		// is made the target's index width, is what keeps every later use
		// (slice clamping in particular) in one type: a uint64 high bound used
		// against a 32-bit length produced `icmp ult i32 ..., i64 ...` and a
		// select with mismatched arms, which the LLVM verifier rejects - every
		// wasm build that sliced with a uint64 index.
		value = b.CreateTrunc(value, targetType, "")
	}
	return value
}
