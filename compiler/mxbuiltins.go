package compiler

// Moxie builtin interception.
//
// The source rewrite phase converts Moxie-specific syntax into calls to
// stub functions (__moxie_concat for | pipe concatenation). This file
// intercepts those calls at the SSA level and emits the correct LLVM IR.
//
// Channel literals (chan T{}) are rewritten to make(chan T) at text level
// and go through the standard MakeChan SSA path.

import (
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// createMoxieConcat handles __moxie_concat(a, b []byte) []byte.
// Emits a call to runtime.bytesConcat.
func (b *builder) createMoxieConcat(instr *ssa.CallCommon) llvm.Value {
	a := b.getValue(instr.Args[0], instr.Pos())
	bv := b.getValue(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("bytesConcat", []llvm.Value{a, bv}, "")
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
