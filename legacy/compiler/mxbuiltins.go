package compiler

// Moxie builtin interception.
//
// The source rewrite phase converts Moxie-specific syntax into calls to
// stub functions (__moxie_concat for | pipe concatenation). This file
// intercepts those calls at the SSA level and emits the correct LLVM IR.
//
// Pipe semantics:
//   x = x | y / x |= y  → bytesConcatMove (extend in place, free old on grow)
//   z := x | y           → bytesConcat     (alloc new, copy both, no free)
//
// The distinction is compile-time: if the Call result is stored back to the
// same address the LHS was loaded from, it's a move. Otherwise it's a borrow.

import (
	"go/token"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// createMoxieConcat handles __moxie_concat(a, b []byte) []byte.
// Detects move vs borrow at the SSA level and emits the appropriate
// runtime call.
func (b *builder) createMoxieConcat(instr *ssa.CallCommon, call *ssa.Call) llvm.Value {
	a := b.getValue(instr.Args[0], instr.Pos())
	bv := b.getValue(instr.Args[1], instr.Pos())
	if call != nil && isConcatMove(instr, call) {
		return b.createRuntimeCall("bytesConcatMove", []llvm.Value{a, bv}, "")
	}
	return b.createRuntimeCall("bytesConcat", []llvm.Value{a, bv}, "")
}

// isConcatMove returns true if the concat result is stored back to the same
// address the first argument was loaded from. This means the LHS is consumed
// (move semantics) and the old buffer can be freed on growth.
func isConcatMove(instr *ssa.CallCommon, call *ssa.Call) bool {
	// Find the source address of the first argument (LHS of pipe).
	lhs := instr.Args[0]
	var srcAddr ssa.Value
	if load, ok := lhs.(*ssa.UnOp); ok && load.Op == token.MUL {
		srcAddr = load.X
	}
	if srcAddr == nil {
		return false
	}
	// Check if the Call result is stored back to the same address.
	refs := call.Referrers()
	if refs == nil {
		return false
	}
	for _, ref := range *refs {
		if store, ok := ref.(*ssa.Store); ok {
			if sameAddr(store.Addr, srcAddr) {
				return true
			}
		}
	}
	return false
}

// sameAddr returns true if two SSA values refer to the same memory address.
// Handles FieldAddr (same base + field), IndexAddr, and direct identity.
func sameAddr(a, b ssa.Value) bool {
	if a == b {
		return true
	}
	fa, aOk := a.(*ssa.FieldAddr)
	fb, bOk := b.(*ssa.FieldAddr)
	if aOk && bOk && fa.Field == fb.Field {
		return sameAddr(fa.X, fb.X)
	}
	ia, aOk := a.(*ssa.IndexAddr)
	ib, bOk := b.(*ssa.IndexAddr)
	if aOk && bOk && ia.Index == ib.Index {
		return sameAddr(ia.X, ib.X)
	}
	return false
}

// createMoxieConcatMove handles __moxie_concat_move(a, b []byte) []byte.
// Move semantics: extends a in place if capacity allows, otherwise grows
// via sliceGrow (alloc new + copy + free old). Old a buffer is consumed.
func (b *builder) createMoxieConcatMove(instr *ssa.CallCommon) llvm.Value {
	a := b.getValue(instr.Args[0], instr.Pos())
	bv := b.getValue(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("bytesConcatMove", []llvm.Value{a, bv}, "")
}

// createMoxieEq handles __moxie_eq(a, b []byte) bool.
// Emits a call to runtime.stringEqual (same layout as []byte).
func (b *builder) createMoxieEq(instr *ssa.CallCommon) llvm.Value {
	a := b.getValue(instr.Args[0], instr.Pos())
	bv := b.getValue(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("stringEqual", []llvm.Value{a, bv}, "")
}

// createMoxieLt handles __moxie_lt(a, b []byte) bool.
// Emits a call to runtime.stringLess (same layout as []byte).
func (b *builder) createMoxieLt(instr *ssa.CallCommon) llvm.Value {
	a := b.getValue(instr.Args[0], instr.Pos())
	bv := b.getValue(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("stringLess", []llvm.Value{a, bv}, "")
}

// createMoxieSecalloc handles __moxie_secalloc(n int32) []byte. The text
// rewriter emits this for the `[]byte{:n, secure}` literal form. Emits a
// call to runtime.SecureAlloc which mmap's a guarded arena and returns a
// slice into it.
func (b *builder) createMoxieSecalloc(instr *ssa.CallCommon) llvm.Value {
	n := b.getValue(instr.Args[0], instr.Pos())
	return b.createRuntimeCall("SecureAlloc", []llvm.Value{n}, "")
}
