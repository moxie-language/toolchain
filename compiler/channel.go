package compiler

// This file lowers channel operations (make/send/recv/close) to runtime calls
// or pseudo-operations that are lowered during goroutine lowering.

import (
	"fmt"
	"go/token"
	"math"

	"moxie/compiler/llvmutil"
	"moxie/mxssa"
	"moxie/typecheck"
	"tinygo.org/x/go-llvm"
)


// createChanRecv emits a pseudo chan receive operation. It is lowered to the
// actual channel receive operation during goroutine lowering.

// createChanClose closes the given channel.
func (b *builder) createChanClose(ch llvm.Value) {
	b.createRuntimeCall("chanClose", []llvm.Value{ch}, "")
}

// createSelect emits all IR necessary for a select statements. That's a
// non-trivial amount of code because select is very complex to implement.

// getChanSelectResult returns the special values from a *ssa.Extract expression
// when extracting a value from a select statement (*ssa.Select). Because
// *ssa.Select cannot load all values in advance, it does this later in the
// *ssa.Extract expression.

func (b *builder) createMakeChanTC(expr *mxssa.MakeChan) llvm.Value {
	elementSize := b.targetData.TypeAllocSize(b.getLLVMTypeTC(expr.Type().Underlying().(*typecheck.Chan).Elem()))
	elementSizeValue := llvm.ConstInt(b.uintptrType, elementSize, false)
	bufSize := b.getValueTC(expr.Size, getPosTC(expr))
	b.createChanBoundsCheckTC(elementSize, bufSize, expr.Size.Type().Underlying().(*typecheck.Basic), expr.Pos())
	if bufSize.Type().IntTypeWidth() < b.uintptrType.IntTypeWidth() {
		bufSize = b.CreateZExt(bufSize, b.uintptrType, "")
	} else if bufSize.Type().IntTypeWidth() > b.uintptrType.IntTypeWidth() {
		bufSize = b.CreateTrunc(bufSize, b.uintptrType, "")
	}
	return b.createRuntimeCall("chanMake", []llvm.Value{elementSizeValue, bufSize}, "")
}

func (b *builder) createChanSendTC(instr *mxssa.Send) {
	pos := instr.Pos()
	ch := b.getValueTC(instr.Chan, pos)
	chanValue := b.getValueTC(instr.X, pos)

	valueType := b.getLLVMTypeTC(instr.X.Type())
	isZeroSize := b.targetData.TypeAllocSize(valueType) == 0
	var valueAlloca, valueAllocaSize llvm.Value
	if isZeroSize {
		valueAlloca = llvm.ConstNull(b.dataPtrType)
	} else {
		valueAlloca, valueAllocaSize = b.createTemporaryAlloca(valueType, "chan.value")
		b.CreateStore(chanValue, valueAlloca)
	}

	channelOp := b.getLLVMRuntimeType("channelOp")
	channelOpAlloca, channelOpAllocaSize := b.createTemporaryAlloca(channelOp, "chan.op")

	b.createRuntimeCall("chanSend", []llvm.Value{ch, valueAlloca, channelOpAlloca}, "")

	b.emitLifetimeEnd(channelOpAlloca, channelOpAllocaSize)
	if !isZeroSize {
		b.emitLifetimeEnd(valueAlloca, valueAllocaSize)
	}
}

func (b *builder) createChanRecvTC(unop *mxssa.UnOp) llvm.Value {
	valueType := b.getLLVMTypeTC(unop.X.Type().Underlying().(*typecheck.Chan).Elem())
	ch := b.getValueTC(unop.X, getPosTC(unop))

	isZeroSize := b.targetData.TypeAllocSize(valueType) == 0
	var valueAlloca, valueAllocaSize llvm.Value
	if isZeroSize {
		valueAlloca = llvm.ConstNull(b.dataPtrType)
	} else {
		valueAlloca, valueAllocaSize = b.createTemporaryAlloca(valueType, "chan.value")
	}

	channelOp := b.getLLVMRuntimeType("channelOp")
	channelOpAlloca, channelOpAllocaSize := b.createTemporaryAlloca(channelOp, "chan.op")

	commaOk := b.createRuntimeCall("chanRecv", []llvm.Value{ch, valueAlloca, channelOpAlloca}, "")
	var received llvm.Value
	if isZeroSize {
		received = llvm.ConstNull(valueType)
	} else {
		received = b.CreateLoad(valueType, valueAlloca, "chan.received")
		b.emitLifetimeEnd(valueAlloca, valueAllocaSize)
	}
	b.emitLifetimeEnd(channelOpAlloca, channelOpAllocaSize)

	if unop.CommaOk {
		tuple := llvm.Undef(b.ctx.StructType([]llvm.Type{valueType, b.ctx.Int1Type()}, false))
		tuple = b.CreateInsertValue(tuple, received, 0, "")
		tuple = b.CreateInsertValue(tuple, commaOk, 1, "")
		return tuple
	}
	return received
}

func (b *builder) createSelectTC(expr *mxssa.Select) llvm.Value {
	if len(expr.States) == 0 {
		llvmType := b.getLLVMTypeTC(expr.Type())
		if expr.Blocking {
			b.createRuntimeCall("deadlock", nil, "")
			return llvm.Undef(llvmType)
		}
		retval := llvm.Undef(llvmType)
		retval = b.CreateInsertValue(retval, llvm.ConstInt(b.intType, 0xffffffffffffffff, true), 0, "")
		return retval
	}

	const maxSelectStates = math.MaxUint32 >> 2
	if len(expr.States) > maxSelectStates {
		b.addError(expr.Pos(), fmt.Sprintf("too many select states: got %d but the maximum supported number is %d", len(expr.States), maxSelectStates))
	}

	recvbufSize := uint64(0)
	recvbufAlign := 0
	var selectStates []llvm.Value
	chanSelectStateType := b.getLLVMRuntimeType("chanSelectState")
	for _, state := range expr.States {
		ch := b.getValueTC(state.Chan, state.Pos)
		selectState := llvm.ConstNull(chanSelectStateType)
		selectState = b.CreateInsertValue(selectState, ch, 0, "")
		switch state.Dir {
		case token.ARROW: // recv
			llvmType := b.getLLVMTypeTC(state.Chan.Type().Underlying().(*typecheck.Chan).Elem())
			if size := b.targetData.TypeAllocSize(llvmType); size > recvbufSize {
				recvbufSize = size
			}
			if align := b.targetData.ABITypeAlignment(llvmType); align > recvbufAlign {
				recvbufAlign = align
			}
		case token.CHAN: // send
			sendValue := b.getValueTC(state.Send, state.Pos)
			alloca := llvmutil.CreateEntryBlockAlloca(b.Builder, sendValue.Type(), "select.send.value")
			b.CreateStore(sendValue, alloca)
			selectState = b.CreateInsertValue(selectState, alloca, 1, "")
		default:
			panic("unreachable")
		}
		selectStates = append(selectStates, selectState)
	}

	recvbuf := llvm.Undef(b.dataPtrType)
	if recvbufSize != 0 {
		allocaType := llvm.ArrayType(b.ctx.Int8Type(), int(recvbufSize))
		recvbufAlloca, _ := b.createTemporaryAlloca(allocaType, "select.recvbuf.alloca")
		recvbufAlloca.SetAlignment(recvbufAlign)
		recvbuf = b.CreateGEP(allocaType, recvbufAlloca, []llvm.Value{
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
		}, "select.recvbuf")
	}

	statesAllocaType := llvm.ArrayType(chanSelectStateType, len(selectStates))
	statesAlloca, statesSize := b.createTemporaryAlloca(statesAllocaType, "select.states.alloca")
	for i, state := range selectStates {
		gep := b.CreateGEP(statesAllocaType, statesAlloca, []llvm.Value{
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
			llvm.ConstInt(b.ctx.Int32Type(), uint64(i), false),
		}, "")
		b.CreateStore(state, gep)
	}
	statesPtr := b.CreateGEP(statesAllocaType, statesAlloca, []llvm.Value{
		llvm.ConstInt(b.ctx.Int32Type(), 0, false),
		llvm.ConstInt(b.ctx.Int32Type(), 0, false),
	}, "select.states")
	statesLen := llvm.ConstInt(b.uintptrType, uint64(len(selectStates)), false)

	var results llvm.Value
	if expr.Blocking {
		opsAllocaType := llvm.ArrayType(b.getLLVMRuntimeType("channelOp"), len(selectStates))
		opsAlloca, opsSize := b.createTemporaryAlloca(opsAllocaType, "select.block.alloca")
		opsLen := llvm.ConstInt(b.uintptrType, uint64(len(selectStates)), false)
		opsPtr := b.CreateGEP(opsAllocaType, opsAlloca, []llvm.Value{
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
		}, "select.block")
		results = b.createRuntimeCall("chanSelect", []llvm.Value{
			recvbuf,
			statesPtr, statesLen, statesLen,
			opsPtr, opsLen, opsLen,
		}, "select.result")
		b.emitLifetimeEnd(opsAlloca, opsSize)
	} else {
		opsPtr := llvm.ConstNull(b.dataPtrType)
		opsLen := llvm.ConstInt(b.uintptrType, 0, false)
		results = b.createRuntimeCall("chanSelect", []llvm.Value{
			recvbuf,
			statesPtr, statesLen, statesLen,
			opsPtr, opsLen, opsLen,
		}, "select.result")
	}

	b.emitLifetimeEnd(statesAlloca, statesSize)

	if b.selectRecvBufTC == nil {
		b.selectRecvBufTC = make(map[*mxssa.Select]llvm.Value)
	}
	b.selectRecvBufTC[expr] = recvbuf

	return results
}

func (b *builder) getChanSelectResultTC(expr *mxssa.Extract) llvm.Value {
	if expr.Index == 0 {
		value := b.getValueTC(expr.Tuple, getPosTC(expr))
		index := b.CreateExtractValue(value, expr.Index, "")
		if index.Type().IntTypeWidth() < b.intType.IntTypeWidth() {
			index = b.CreateSExt(index, b.intType, "")
		}
		return index
	} else if expr.Index == 1 {
		value := b.getValueTC(expr.Tuple, getPosTC(expr))
		return b.CreateExtractValue(value, expr.Index, "")
	} else {
		recvbuf := b.selectRecvBufTC[expr.Tuple.(*mxssa.Select)]
		typ := b.getLLVMTypeTC(expr.Type())
		return b.CreateLoad(typ, recvbuf, "")
	}
}
