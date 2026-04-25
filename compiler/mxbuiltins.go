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
	"moxie/mxssa"
	"tinygo.org/x/go-llvm"
)

func (b *builder) createMoxieConcatTC(instr *mxssa.CallCommon) llvm.Value {
	a := b.getValueTC(instr.Args[0], instr.Pos())
	bv := b.getValueTC(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("bytesConcat", []llvm.Value{a, bv}, "")
}

func (b *builder) createMoxieEqTC(instr *mxssa.CallCommon) llvm.Value {
	a := b.getValueTC(instr.Args[0], instr.Pos())
	bv := b.getValueTC(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("stringEqual", []llvm.Value{a, bv}, "")
}

func (b *builder) createMoxieLtTC(instr *mxssa.CallCommon) llvm.Value {
	a := b.getValueTC(instr.Args[0], instr.Pos())
	bv := b.getValueTC(instr.Args[1], instr.Pos())
	return b.createRuntimeCall("stringLess", []llvm.Value{a, bv}, "")
}

func (b *builder) createMoxieSecallocTC(instr *mxssa.CallCommon) llvm.Value {
	n := b.getValueTC(instr.Args[0], instr.Pos())
	return b.createRuntimeCall("SecureAlloc", []llvm.Value{n}, "")
}
