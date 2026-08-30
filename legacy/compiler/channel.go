package compiler

// This file lowers channel operations (make/send/recv/close) to runtime calls
// or pseudo-operations that are lowered during goroutine lowering.

import (
	"fmt"
	"go/types"
	"math"

	"moxie/compiler/llvmutil"
	"moxie/loader"
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

func (b *builder) createMakeChan(expr *ssa.MakeChan) llvm.Value {
	elementSize := b.targetData.TypeAllocSize(b.getLLVMType(expr.Type().Underlying().(*types.Chan).Elem()))
	elementSizeValue := llvm.ConstInt(b.uintptrType, elementSize, false)
	bufSize := b.getValue(expr.Size, getPos(expr))
	b.createChanBoundsCheck(elementSize, bufSize, expr.Size.Type().Underlying().(*types.Basic), expr.Pos())
	if bufSize.Type().IntTypeWidth() < b.uintptrType.IntTypeWidth() {
		bufSize = b.CreateZExt(bufSize, b.uintptrType, "")
	} else if bufSize.Type().IntTypeWidth() > b.uintptrType.IntTypeWidth() {
		bufSize = b.CreateTrunc(bufSize, b.uintptrType, "")
	}
	ch := b.createRuntimeCall("chanMake", []llvm.Value{elementSizeValue, bufSize}, "")
	if b.PrintAllocs != nil {
		b.emitLogAlloc(expr.Pos())
	}
	return ch
}

// createChanSend emits a pseudo chan send operation. It is lowered to the
// actual channel send operation during goroutine lowering.
func (b *builder) createChanSend(instr *ssa.Send) {
	// Wasm spawn-boundary SAB channel: bypass runtime, call bridge directly.
	if b.isWasmTarget() {
		if handle, ok := b.sabChannels[instr.Chan]; ok {
			b.createWasmSABChanSend(instr, handle)
			return
		}
	}
	// Native: any chan whose element type implements moxie.Codec routes
	// through ChanSendDual at runtime, which dispatches on ch.pipeBoundFd.
	// This preserves correctness through array / slice / struct-field
	// indirection where the compiler's per-SSA-value tracking can't see
	// that the chan was originally passed to spawn().
	if b.elemTypeImplementsCodec(instr.X.Type()) {
		b.createCodecChanSend(instr)
		return
	}

	ch := b.getValue(instr.Chan, getPos(instr))
	chanValue := b.getValue(instr.X, getPos(instr))

	// store value-to-send
	valueType := b.getLLVMType(instr.X.Type())
	isZeroSize := b.targetData.TypeAllocSize(valueType) == 0
	var valueAlloca, valueAllocaSize llvm.Value
	if isZeroSize {
		valueAlloca = llvm.ConstNull(b.dataPtrType)
	} else {
		valueAlloca, valueAllocaSize = b.createTemporaryAlloca(valueType, "chan.value")
		b.CreateStore(chanValue, valueAlloca)
	}

	// Allocate buffer for the channel operation.
	channelOp := b.getLLVMRuntimeType("channelOp")
	channelOpAlloca, channelOpAllocaSize := b.createTemporaryAlloca(channelOp, "chan.op")

	// Do the send.
	b.createRuntimeCall("chanSend", []llvm.Value{ch, valueAlloca, channelOpAlloca}, "")

	// End the lifetime of the allocas.
	// This also works around a bug in CoroSplit, at least in LLVM 8:
	// https://bugs.llvm.org/show_bug.cgi?id=41742
	b.emitLifetimeEnd(channelOpAlloca, channelOpAllocaSize)
	if !isZeroSize {
		b.emitLifetimeEnd(valueAlloca, valueAllocaSize)
	}
}

// createChanRecv emits a pseudo chan receive operation. It is lowered to the
// actual channel receive operation during goroutine lowering.
func (b *builder) createChanRecv(unop *ssa.UnOp) llvm.Value {
	// Wasm spawn-boundary SAB channel.
	if b.isWasmTarget() {
		if handle, ok := b.sabChannels[unop.X]; ok {
			return b.createWasmSABChanRecv(unop, handle)
		}
	}
	// Native: dual-path runtime dispatch for Codec-implementing types.
	chanType := unop.X.Type().Underlying().(*types.Chan)
	if b.elemTypeImplementsCodec(chanType.Elem()) {
		return b.createCodecChanRecv(unop)
	}

	valueType := b.getLLVMType(chanType.Elem())
	ch := b.getValue(unop.X, getPos(unop))

	// Allocate memory to receive into.
	isZeroSize := b.targetData.TypeAllocSize(valueType) == 0
	var valueAlloca, valueAllocaSize llvm.Value
	if isZeroSize {
		valueAlloca = llvm.ConstNull(b.dataPtrType)
	} else {
		valueAlloca, valueAllocaSize = b.createTemporaryAlloca(valueType, "chan.value")
	}

	// Allocate buffer for the channel operation.
	channelOp := b.getLLVMRuntimeType("channelOp")
	channelOpAlloca, channelOpAllocaSize := b.createTemporaryAlloca(channelOp, "chan.op")

	// Do the receive.
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
	} else {
		return received
	}
}

// createChanClose closes the given channel.
func (b *builder) createChanClose(ch llvm.Value) {
	b.createRuntimeCall("chanClose", []llvm.Value{ch}, "")
}

// createWasmSABChanSend emits a bridge.channel_send call for spawn-boundary channels.
func (b *builder) createWasmSABChanSend(instr *ssa.Send, handle llvm.Value) {
	chanValue := b.getValue(instr.X, getPos(instr))
	valueType := b.getLLVMType(instr.X.Type())
	sz := b.targetData.TypeAllocSize(valueType)
	i32 := b.ctx.Int32Type()

	if sz == 0 {
		b.emitBridgeCall("channel_send", []llvm.Value{
			handle,
			llvm.ConstNull(b.dataPtrType),
			llvm.ConstInt(i32, 0, false),
		}, b.ctx.VoidType())
		return
	}
	alloca, allocaSize := b.createTemporaryAlloca(valueType, "sab.send.val")
	b.CreateStore(chanValue, alloca)
	b.emitBridgeCall("channel_send", []llvm.Value{
		handle,
		alloca,
		llvm.ConstInt(i32, sz, false),
	}, b.ctx.VoidType())
	b.emitLifetimeEnd(alloca, allocaSize)
}

// createWasmSABChanRecv emits a bridge.channel_recv call for spawn-boundary channels.
func (b *builder) createWasmSABChanRecv(unop *ssa.UnOp, handle llvm.Value) llvm.Value {
	valueType := b.getLLVMType(unop.X.Type().Underlying().(*types.Chan).Elem())
	sz := b.targetData.TypeAllocSize(valueType)
	i32 := b.ctx.Int32Type()

	alloca, allocaSize := b.createTemporaryAlloca(valueType, "sab.recv.val")
	result := b.emitBridgeCall("channel_recv", []llvm.Value{
		handle,
		alloca,
		llvm.ConstInt(i32, sz, false),
	}, i32)

	var received llvm.Value
	if sz == 0 {
		received = llvm.ConstNull(valueType)
	} else {
		received = b.CreateLoad(valueType, alloca, "sab.received")
		b.emitLifetimeEnd(alloca, allocaSize)
	}

	if unop.CommaOk {
		// result == -1 (all bits set as int32) means closed.
		isOpen := b.CreateICmp(llvm.IntNE, result, llvm.ConstInt(i32, ^uint64(0), true), "sab.ok")
		tuple := llvm.Undef(b.ctx.StructType([]llvm.Type{valueType, b.ctx.Int1Type()}, false))
		tuple = b.CreateInsertValue(tuple, received, 0, "")
		tuple = b.CreateInsertValue(tuple, isOpen, 1, "")
		return tuple
	}
	return received
}

// createSelect emits all IR necessary for a select statements. That's a
// non-trivial amount of code because select is very complex to implement.
func (b *builder) createSelect(expr *ssa.Select) llvm.Value {
	if len(expr.States) == 0 {
		// Shortcuts for some simple selects.
		llvmType := b.getLLVMType(expr.Type())
		if expr.Blocking {
			// Blocks forever:
			//     select {}
			b.createRuntimeCall("deadlock", nil, "")
			return llvm.Undef(llvmType)
		} else {
			// No-op:
			//     select {
			//     default:
			//     }
			retval := llvm.Undef(llvmType)
			retval = b.CreateInsertValue(retval, llvm.ConstInt(b.intType, 0xffffffffffffffff, true), 0, "")
			return retval // {-1, false}
		}
	}

	const maxSelectStates = math.MaxUint32 >> 2
	if len(expr.States) > maxSelectStates {
		// The runtime code assumes that the number of state must fit in 30 bits
		// (so the select index can be stored in a uint32 with two bits reserved
		// for other purposes). It seems unlikely that a real program would have
		// that many states, but we check for this case anyway to be sure.
		// We use a uint32 (and not a uintptr or uint64) to avoid 64-bit atomic
		// operations which aren't available everywhere.
		b.addError(expr.Pos(), fmt.Sprintf("too many select states: got %d but the maximum supported number is %d", len(expr.States), maxSelectStates))

		// Continue as usual (we'll generate broken code but the error will
		// prevent the compilation to complete).
	}

	// This code create a (stack-allocated) slice containing all the select
	// cases and then calls runtime.chanSelect to perform the actual select
	// statement.
	// Simple selects (blocking and with just one case) are already transformed
	// into regular chan operations during SSA construction so we don't have to
	// optimize such small selects.

	// Go through all the cases. Create the selectStates slice and and
	// determine the receive buffer size and alignment.
	recvbufSize := uint64(0)
	recvbufAlign := 0
	var selectStates []llvm.Value
	chanSelectStateType := b.getLLVMRuntimeType("chanSelectState")
	for _, state := range expr.States {
		ch := b.getValue(state.Chan, state.Pos)
		selectState := llvm.ConstNull(chanSelectStateType)
		selectState = b.CreateInsertValue(selectState, ch, 0, "")
		switch state.Dir {
		case types.RecvOnly:
			// Make sure the receive buffer is big enough and has the correct alignment.
			llvmType := b.getLLVMType(state.Chan.Type().Underlying().(*types.Chan).Elem())
			if size := b.targetData.TypeAllocSize(llvmType); size > recvbufSize {
				recvbufSize = size
			}
			if align := b.targetData.ABITypeAlignment(llvmType); align > recvbufAlign {
				recvbufAlign = align
			}
		case types.SendOnly:
			// Store this value in an alloca and put a pointer to this alloca
			// in the send state.
			sendValue := b.getValue(state.Send, state.Pos)
			alloca := llvmutil.CreateEntryBlockAlloca(b.Builder, sendValue.Type(), "select.send.value")
			b.CreateStore(sendValue, alloca)
			selectState = b.CreateInsertValue(selectState, alloca, 1, "")
		default:
			panic("unreachable")
		}
		selectStates = append(selectStates, selectState)
	}

	// Create a receive buffer, where the received value will be stored.
	// Always allocate at least one byte: the runtime memzeros the buffer on
	// a closed-channel receive (memzero(value, elementSize)), and an undef
	// pointer would write through garbage into whatever memory it aliases.
	// Zero-size elements (struct{}) yield a nonsense alignment from
	// ABITypeAlignment; clamp to a byte for the forced buffer.
	recvbuf := llvm.Undef(b.dataPtrType)
	if recvbufSize == 0 {
		recvbufSize = 1
		recvbufAlign = 1
	}
	{
		allocaType := llvm.ArrayType(b.ctx.Int8Type(), int(recvbufSize))
		recvbufAlloca, _ := b.createTemporaryAlloca(allocaType, "select.recvbuf.alloca")
		recvbufAlloca.SetAlignment(recvbufAlign)
		recvbuf = b.CreateGEP(allocaType, recvbufAlloca, []llvm.Value{
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
		}, "select.recvbuf")
	}

	// Create the states slice (allocated on the stack).
	statesAllocaType := llvm.ArrayType(chanSelectStateType, len(selectStates))
	statesAlloca, statesSize := b.createTemporaryAlloca(statesAllocaType, "select.states.alloca")
	for i, state := range selectStates {
		// Set each slice element to the appropriate channel.
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

	// Do the select in the runtime.
	var results llvm.Value
	if expr.Blocking {
		// Stack-allocate operation structures.
		// If these were simply created as a slice, they would heap-allocate.
		opsAllocaType := llvm.ArrayType(b.getLLVMRuntimeType("channelOp"), len(selectStates))
		opsAlloca, opsSize := b.createTemporaryAlloca(opsAllocaType, "select.block.alloca")
		opsLen := llvm.ConstInt(b.uintptrType, uint64(len(selectStates)), false)
		opsPtr := b.CreateGEP(opsAllocaType, opsAlloca, []llvm.Value{
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
			llvm.ConstInt(b.ctx.Int32Type(), 0, false),
		}, "select.block")

		results = b.createRuntimeCall("chanSelect", []llvm.Value{
			recvbuf,
			statesPtr, statesLen, statesLen, // []chanSelectState
			opsPtr, opsLen, opsLen, // []channelOp
		}, "select.result")

		// Terminate the lifetime of the operation structures.
		b.emitLifetimeEnd(opsAlloca, opsSize)
	} else {
		opsPtr := llvm.ConstNull(b.dataPtrType)
		opsLen := llvm.ConstInt(b.uintptrType, 0, false)
		results = b.createRuntimeCall("chanSelect", []llvm.Value{
			recvbuf,
			statesPtr, statesLen, statesLen, // []chanSelectState
			opsPtr, opsLen, opsLen, // []channelOp (nil slice)
		}, "select.result")
	}

	// Terminate the lifetime of the states alloca.
	b.emitLifetimeEnd(statesAlloca, statesSize)

	// The result value does not include all the possible received values,
	// because we can't load them in advance. Instead, the *ssa.Extract
	// instruction will treat a *ssa.Select specially and load it there inline.
	// Store the receive alloca in a sidetable until we hit this extract
	// instruction.
	if b.selectRecvBuf == nil {
		b.selectRecvBuf = make(map[*ssa.Select]llvm.Value)
	}
	b.selectRecvBuf[expr] = recvbuf

	return results
}

// getChanSelectResult returns the special values from a *ssa.Extract expression
// when extracting a value from a select statement (*ssa.Select). Because
// *ssa.Select cannot load all values in advance, it does this later in the
// *ssa.Extract expression.
func (b *builder) getChanSelectResult(expr *ssa.Extract) llvm.Value {
	if expr.Index == 0 {
		// index
		value := b.getValue(expr.Tuple, getPos(expr))
		index := b.CreateExtractValue(value, expr.Index, "")
		if index.Type().IntTypeWidth() < b.intType.IntTypeWidth() {
			index = b.CreateSExt(index, b.intType, "")
		}
		return index
	} else if expr.Index == 1 {
		// comma-ok
		value := b.getValue(expr.Tuple, getPos(expr))
		return b.CreateExtractValue(value, expr.Index, "")
	} else {
		// Select statements are (index, ok, ...) where ... is a number of
		// received values, depending on how many receive statements there
		// are. They are all combined into one alloca (because only one
		// receive can proceed at a time) so we'll get that alloca, bitcast
		// it to the correct type, and dereference it.
		recvbuf := b.selectRecvBuf[expr.Tuple.(*ssa.Select)]
		typ := b.getLLVMType(expr.Type())
		return b.CreateLoad(typ, recvbuf, "")
	}
}

// createNativePipeChanSend emits the native spawn-boundary chan send
// path. Wraps the value as a moxie.Codec interface (with *T as the
// underlying value so both value- and pointer-receiver methods are
// dispatchable) and calls runtime.PipeChanSendCodec, which invokes
// the user-defined EncodeTo to serialize correctly across the fork.
//
// Codec replaces raw-memcpy: types containing slices, maps, or other
// indirect data now serialize their byte payload, not just their
// pointer headers. The compiler's stack allocas stay T-sized (no
// quadratic codegen blowup from huge inline allocas) because EncodeTo
// emits bytes incrementally.
// createCodecChanSend emits the dual-path chan send for any chan whose
// element type implements moxie.Codec. The runtime helper ChanSendDual
// branches on ch.pipeBoundFd: if pipe-bound, EncodeTo then PipeSend; if
// local, the existing chanSend in-runtime queue path.
//
// Compile-time emission shape:
//   alloca v: T            // store the value-to-send
//   alloca op: channelOp   // for local-queue path
//   codec := iface{*T, &v}
//   ChanSendDual(ch, codec, &v, &op)
func (b *builder) createCodecChanSend(instr *ssa.Send) {
	ch := b.getValue(instr.Chan, getPos(instr))
	chanValue := b.getValue(instr.X, getPos(instr))
	elemType := instr.X.Type()
	elemLLVM := b.getLLVMType(elemType)

	valAlloca, valAllocaSize := b.createTemporaryAlloca(elemLLVM, "chan.value")
	b.CreateStore(chanValue, valAlloca)
	codecIface := b.createPipeChanCodecIface(elemType, valAlloca)

	channelOpType := b.getLLVMRuntimeType("channelOp")
	opAlloca, opAllocaSize := b.createTemporaryAlloca(channelOpType, "chan.op")

	runtimePkg := b.program.ImportedPackage("runtime")
	sendFn := runtimePkg.Members["ChanSendDual"].(*ssa.Function)
	sendType, sendLLVM := b.getFunction(sendFn)

	args := []llvm.Value{ch}
	args = append(args, b.expandFormalParam(codecIface)...)
	args = append(args, valAlloca, opAlloca)
	args = append(args, llvm.Undef(b.dataPtrType))
	b.createCall(sendType, sendLLVM, args, "")

	b.emitLifetimeEnd(opAlloca, opAllocaSize)
	b.emitLifetimeEnd(valAlloca, valAllocaSize)
}

// createCodecChanRecv emits the dual-path chan recv counterpart of
// createCodecChanSend.
func (b *builder) createCodecChanRecv(unop *ssa.UnOp) llvm.Value {
	elemType := unop.X.Type().Underlying().(*types.Chan).Elem()
	elemLLVM := b.getLLVMType(elemType)
	ch := b.getValue(unop.X, getPos(unop))

	valAlloca, valAllocaSize := b.createTemporaryAlloca(elemLLVM, "chan.value")
	b.CreateStore(llvm.ConstNull(elemLLVM), valAlloca)
	codecIface := b.createPipeChanCodecIface(elemType, valAlloca)

	channelOpType := b.getLLVMRuntimeType("channelOp")
	opAlloca, opAllocaSize := b.createTemporaryAlloca(channelOpType, "chan.op")

	runtimePkg := b.program.ImportedPackage("runtime")
	recvFn := runtimePkg.Members["ChanRecvDual"].(*ssa.Function)
	recvType, recvLLVM := b.getFunction(recvFn)

	args := []llvm.Value{ch}
	args = append(args, b.expandFormalParam(codecIface)...)
	args = append(args, valAlloca, opAlloca)
	args = append(args, llvm.Undef(b.dataPtrType))
	okBool := b.createCall(recvType, recvLLVM, args, "chan.recv.ok")

	received := b.CreateLoad(elemLLVM, valAlloca, "chan.received")
	b.emitLifetimeEnd(opAlloca, opAllocaSize)
	b.emitLifetimeEnd(valAlloca, valAllocaSize)

	if unop.CommaOk {
		tuple := llvm.Undef(b.ctx.StructType([]llvm.Type{elemLLVM, b.ctx.Int1Type()}, false))
		tuple = b.CreateInsertValue(tuple, received, 0, "")
		tuple = b.CreateInsertValue(tuple, okBool, 1, "")
		return tuple
	}
	return received
}

// elemTypeImplementsCodec reports whether the given chan element type
// satisfies the moxie.Codec interface (or *T does — covers value- and
// pointer-receiver methods). Used to decide whether to emit the dual-
// path chan op or the local-only path.
func (b *builder) elemTypeImplementsCodec(t types.Type) bool {
	if ch, ok := t.Underlying().(*types.Chan); ok {
		t = ch.Elem()
	}
	moxiePkg := b.program.ImportedPackage("moxie")
	if moxiePkg == nil {
		return false
	}
	codecObj := moxiePkg.Pkg.Scope().Lookup("Codec")
	if codecObj == nil {
		return false
	}
	codecIface, ok := codecObj.Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return loader.ImplementsCodecIface(t, codecIface)
}

// createPipeChanCodecIface builds a moxie.Codec interface value whose
// typecode is *T (so value-receiver and pointer-receiver methods on T
// both dispatch correctly) and whose valuePtr points at the supplied
// alloca holding (or to be filled with) a T value.
//
// Avoids createMakeInterface — that would heap-allocate the value via
// emitPointerPack for non-pointer-typed inputs. Here valPtr is already
// a pointer; we splice it directly into the _interface struct.
func (b *builder) createPipeChanCodecIface(elemType types.Type, valPtr llvm.Value) llvm.Value {
	ptrType := types.NewPointer(elemType)
	typecode := b.getTypeCode(ptrType)
	itf := llvm.Undef(b.getLLVMRuntimeType("_interface"))
	itf = b.CreateInsertValue(itf, typecode, 0, "")
	itf = b.CreateInsertValue(itf, valPtr, 1, "")
	return itf
}
