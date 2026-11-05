package compiler

// This file lowers channel operations (make/send/recv/close) to runtime calls
// or pseudo-operations that are lowered during goroutine lowering.

import (
	"fmt"
	"go/token"
	"go/types"
	"math"
	"strconv"
	"strings"

	"moxie/compiler/llvmutil"
	"moxie/loader"
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

func (b *builder) createMakeChan(expr *ssa.MakeChan) llvm.Value {
	// A channel is a mailbox with at least one slot. A Moxie domain has one
	// thread, so an unbuffered channel would have no possible listener: the
	// first send could never complete. `chan T{}` is lowered to capacity 1, so
	// the only way to arrive here with a constant below one is to write a
	// capacity that cannot work.
	if sz, ok := expr.Size.(*ssa.Const); ok && !sz.IsNil() && sz.Int64() < 1 {
		b.addError(expr.Pos(), "channel capacity must be at least 1: a Moxie domain has one thread, so an unbuffered channel has no possible listener")
	}
	elemType := expr.Type().Underlying().(*types.Chan).Elem()
	elementSize := b.targetData.TypeAllocSize(b.getLLVMType(elemType))
	elementSizeValue := llvm.ConstInt(b.uintptrType, elementSize, false)
	budget := b.frameBudget(elemType, nil, 0, expr.Pos())
	var budgetVal llvm.Value
	if expr.Budget != nil {
		// `chan T{depth, bytes}` states the frame budget in the declaration, and
		// it may be a run-time expression so relay policy read from config can
		// size the ring. A constant is still checked here; a computed one is
		// checked by spawnRingDataSize when the ring is created.
		if c, ok := expr.Budget.(*ssa.Const); ok && !c.IsNil() && c.Int64() > frameBudgetMax {
			b.addError(expr.Pos(), fmt.Sprintf(
				"spawn: channel frame budget %d is over the %d-byte maximum", c.Int64(), frameBudgetMax))
		}
		budgetVal = b.extendInteger(b.getValue(expr.Budget, getPos(expr)),
			expr.Budget.Type().Underlying().(*types.Basic), b.uintptrType)
	} else {
		if budget > frameBudgetMax {
			b.addError(expr.Pos(), fmt.Sprintf(
				"spawn: %s has a %d-byte frame budget, over the %d-byte maximum; reduce its cap dimensions", elemType, budget, frameBudgetMax))
			budget = frameBudgetMax
		}
		budgetVal = llvm.ConstInt(b.uintptrType, uint64(budget), false)
	}
	notePtr, noteLen := b.budgetNoteOperand(b.frameNote(elemType, "", ""))
	bufSize := b.getValue(expr.Size, getPos(expr))
	b.createChanBoundsCheck(elementSize, bufSize, expr.Size.Type().Underlying().(*types.Basic), expr.Pos())
	if bufSize.Type().IntTypeWidth() < b.uintptrType.IntTypeWidth() {
		bufSize = b.CreateZExt(bufSize, b.uintptrType, "")
	} else if bufSize.Type().IntTypeWidth() > b.uintptrType.IntTypeWidth() {
		bufSize = b.CreateTrunc(bufSize, b.uintptrType, "")
	}
	ch := b.createRuntimeCall("chanMake", []llvm.Value{
		elementSizeValue,
		bufSize,
		budgetVal,
		notePtr, noteLen,
	}, "")
	if b.PrintAllocs != nil {
		// The channel's storage belongs to the runtime allocator, so the byte
		// size is not known here; the site is still counted.
		b.emitLogAlloc(expr.Pos(), llvm.ConstInt(b.uintptrType, 0, false))
	}
	return ch
}

// frameBudgetDefault is the byte budget for a variable-length dimension whose
// struct field states no cap tag: one memory page.
const frameBudgetDefault = 4096

// frameBudgetMax bounds one frame. A spawn ring holds the declared depth plus
// one in-flight frame, so the budget has to leave the pair inside ringMaxMsg.
const frameBudgetMax = 16 << 20

// capTagCounts parses a cap:"..." struct tag into its per-dimension element
// counts, outermost dimension first. A malformed or missing number is treated
// as unstated (the caller then takes the page default).
func capTagCounts(tag string) (dims []int64) {
	const key = `cap:"`
	i := strings.Index(tag, key)
	if i < 0 {
		return nil
	}
	rest := tag[i+len(key):]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		rest = rest[:j]
	}
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		dims = append(dims, n)
	}
	return dims
}

// frameBudget is an upper bound, in bytes, on the structural encoding of one
// value of type t as written by codecEncodeValue: a tag byte per non-basic
// value, a varint count (at most 5 bytes for a uint32), then the payload. A
// slice, string or map dimension takes its count from the enclosing field's
// cap tag, which states ELEMENTS of that dimension (bytes for a byte/string
// leaf); an unstated dimension is one memory page. The value sizes the shared
// ring a spawn channel carries, so a dimension that cannot be bounded at all
// (channel, interface, function, type parameter) is an error, never a guess.
func (b *builder) frameBudget(t types.Type, dims []int64, di int, pos token.Pos) (n int64) {
	n = b.frameBudgetDepth(t, dims, di, pos, 0)
	// The ring must hold whichever representation the element uses: the
	// compact structural encoding, or the offset frame (a memory image plus a
	// 4-byte magic). The offset sizer is the codec's own; a new representation
	// adds a sizer here rather than editing this formula twice.
	if om := b.offsetFrameMax(t, dims, pos); om > n {
		n = om
	}
	return n
}

// offsetEligibleType reports whether a value of t can cross as an offset frame:
// scalars and string/[]byte, or a struct of only those. Mirrors stage4.
// isBytesField reports whether t is Moxie text: go/types spells []byte as a
// slice of byte (string and []byte are one type in Moxie), so the legacy model
// needs this where stage4 sees TCString.
func isBytesField(t types.Type) bool {
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}
	if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 {
		return true
	}
	sl, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	eb, ok := sl.Elem().Underlying().(*types.Basic)
	return ok && eb.Kind() == types.Byte
}

func offsetEligibleType(t types.Type) bool {
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}
	switch ut := t.Underlying().(type) {
	case *types.Basic:
		return ut.Info()&types.IsString != 0 || ut.Info()&types.IsInteger != 0 || ut.Info()&types.IsFloat != 0 || ut.Kind() == types.Bool
	case *types.Struct:
		for i := 0; i < ut.NumFields(); i++ {
			fu := ut.Field(i).Type()
			if named, ok := fu.(*types.Named); ok {
				fu = named.Underlying()
			}
			if isBytesField(fu) {
				continue
			}
			fb, ok := fu.Underlying().(*types.Basic)
			if !ok {
				return false
			}
			if fb.Info()&types.IsInteger == 0 && fb.Info()&types.IsFloat == 0 && fb.Kind() != types.Bool {
				return false
			}
		}
		return true
	}
	return false
}

// offsetFrameMax is the largest offset frame for one value of t: the 4-byte
// magic, the value's in-memory extent, and every string/[]byte field's data at
// its cap (4096 for an unstated dimension). Mirrors stage4.
func (b *builder) offsetFrameMax(t types.Type, dims []int64, pos token.Pos) (n int64) {
	if !offsetEligibleType(t) {
		return 0
	}
	n = 4 + int64(b.targetData.TypeAllocSize(b.getLLVMType(t)))
	u := t
	if named, ok := t.(*types.Named); ok {
		u = named.Underlying()
	}
	if fb, ok := u.Underlying().(*types.Basic); ok {
		if fb.Info()&types.IsString != 0 {
			c := dimCount(dims, 0)
			if c == 0 {
				c = frameBudgetDefault
			}
			n += c
		}
		return n
	}
	st, ok := u.Underlying().(*types.Struct)
	if !ok {
		return 0
	}
	for i := 0; i < st.NumFields(); i++ {
		fu := st.Field(i).Type()
		if named, ok := fu.(*types.Named); ok {
			fu = named.Underlying()
		}
		if !isBytesField(fu) {
			continue
		}
		c := dimCount(capTagCounts(st.Tag(i)), 0)
		if c == 0 {
			c = frameBudgetDefault
		}
		n += c
	}
	return n
}

// frameBudgetDepth bounds the walk: a type graph with a cycle (a struct
// pointing at itself) would otherwise recurse until the stack dies. Past the
// bound one page is counted, which can only over-estimate the frame.
func (b *builder) frameBudgetDepth(t types.Type, dims []int64, di int, pos token.Pos, depth int) (n int64) {
	if depth > 48 {
		return frameBudgetDefault
	}
	if t == nil {
		return frameBudgetDefault
	}
	if named, ok := t.(*types.Named); ok {
		t = named.Underlying()
	}
	switch ut := t.Underlying().(type) {
	case *types.Basic:
		if ut.Info()&types.IsString != 0 {
			// tag + varint length + varint capacity + bytes
			c := dimCount(dims, di)
			if c == 0 {
				c = frameBudgetDefault
			}
			return 1 + 10 + c
		}
		return int64(b.targetData.TypeAllocSize(b.getLLVMType(t)))
	case *types.Slice:
		if c := dimCount(dims, di); c > 0 {
			return 1 + 10 + c*b.frameBudgetDepth(ut.Elem(), dims, di+1, pos, depth+1)
		}
		return 1 + 10 + frameBudgetDefault
	case *types.Array:
		return 1 + 10 + int64(ut.Len())*b.frameBudgetDepth(ut.Elem(), dims, di+1, pos, depth+1)
	case *types.Pointer:
		return 1 + b.frameBudgetDepth(ut.Elem(), dims, di, pos, depth+1)
	case *types.Map:
		if c := dimCount(dims, di); c > 0 {
			return 1 + 5 + c*(b.frameBudgetDepth(ut.Key(), nil, 0, pos, depth+1)+b.frameBudgetDepth(ut.Elem(), nil, 0, pos, depth+1))
		}
		return 1 + 5 + frameBudgetDefault
	case *types.Struct:
		n = 1 + 5
		for i := 0; i < ut.NumFields(); i++ {
			n += b.frameBudgetDepth(ut.Field(i).Type(), capTagCounts(ut.Tag(i)), 0, pos, depth+1)
		}
		return n
	case *types.Chan, *types.Interface, *types.Signature, *types.TypeParam:
		// Not sizeable, and not legal in a spawn frame either: the spawn
		// validator refuses those types by name, so the budget is not the gate.
		// Counting a page keeps the number an over-estimate rather than making
		// every make(chan) in the tree report a diagnostic.
	}
	return frameBudgetDefault
}

// dimCount is the stated count for the dimension at di, or 0 when unstated.
func dimCount(dims []int64, di int) (n int64) {
	if di >= 0 && di < len(dims) {
		return dims[di]
	}
	return 0
}

// budgetNoteOperand materializes the frame-budget note as a (pointer, length)
// pair for the runtime: a private, writable global holding the bytes. The note
// is the compiler's contribution to the overflow panic, so it names the fields
// whose cap tags set the budget.
func (b *builder) budgetNoteOperand(note string) (ptr llvm.Value, n llvm.Value) {
	if note == "" {
		return llvm.ConstNull(b.dataPtrType), llvm.ConstInt(b.uintptrType, 0, false)
	}
	globalType := llvm.ArrayType(b.ctx.Int8Type(), len(note))
	global := llvm.AddGlobal(b.mod, globalType, "$spawn.frameNote")
	global.SetInitializer(b.ctx.ConstString(note, false))
	global.SetLinkage(llvm.InternalLinkage)
	// Moxie strings are mutable; keep the note out of .rodata like every other
	// string object the compiler emits.
	global.SetGlobalConstant(false)
	global.SetUnnamedAddr(true)
	global.SetAlignment(1)
	zero := llvm.ConstInt(b.ctx.Int32Type(), 0, false)
	ptr = llvm.ConstInBoundsGEP(globalType, global, []llvm.Value{zero, zero})
	return ptr, llvm.ConstInt(b.uintptrType, uint64(len(note)), false)
}

// frameNote lists every variable-length field in t's frame path with the cap
// tag that sizes it, or "unstated (4096)" when the tag is absent. This text is
// carried on the channel and appears in the panic that fires when an encoding
// overruns its budget, so it must name the declaration a programmer changes.
func (b *builder) frameNote(t types.Type, path, tag string) (s string) {
	return b.frameNoteDepth(t, path, tag, 0)
}

func (b *builder) frameNoteDepth(t types.Type, path, tag string, depth int) (s string) {
	if depth > 48 {
		return ""
	}
	if t == nil {
		return ""
	}
	if named, ok := t.(*types.Named); ok {
		if path == "" {
			path = types.TypeString(named, nil)
		}
		t = named.Underlying()
	}
	switch ut := t.Underlying().(type) {
	case *types.Basic:
		if ut.Info()&types.IsString != 0 {
			return capNoteEntry(path, tag)
		}
	case *types.Slice:
		if strings.Contains(tag, `cap:"`) || elemIsByte(ut.Elem()) {
			return capNoteEntry(path, tag)
		}
		if sub := b.frameNoteDepth(ut.Elem(), path+"[]", "", depth+1); sub != "" {
			return capNoteEntry(path, tag) + "; " + sub
		}
		return capNoteEntry(path, tag)
	case *types.Array:
		return b.frameNoteDepth(ut.Elem(), path, tag, depth+1)
	case *types.Pointer:
		return b.frameNoteDepth(ut.Elem(), path, tag, depth+1)
	case *types.Map:
		return capNoteEntry(path, tag)
	case *types.Struct:
		if path == "" {
			path = t.String()
		}
		var parts []string
		for i := 0; i < ut.NumFields(); i++ {
			if sub := b.frameNoteDepth(ut.Field(i).Type(), path+"."+ut.Field(i).Name(), ut.Tag(i), depth+1); sub != "" {
				parts = append(parts, sub)
			}
		}
		return strings.Join(parts, "; ")
	}
	return ""
}

func capNoteEntry(path, tag string) string {
	const key = `cap:"`
	if i := strings.Index(tag, key); i >= 0 {
		rest := tag[i+len(key):]
		if j := strings.IndexByte(rest, '"'); j >= 0 {
			return path + ` cap:"` + rest[:j] + `"`
		}
	}
	return path + " unstated (4096)"
}

func elemIsByte(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// createChanSend emits a pseudo chan send operation. It is lowered to the
// actual channel send operation during goroutine lowering.
func (b *builder) createChanSend(instr *ssa.Send) {
	// A send's X is the VALUE being sent, so its type IS the element type -
	// unwrapping it as a chan panics on every send whose value is not itself a
	// channel (types.Basic for a []byte). The recv hook below reads
	// unop.X.Type() as the chan because a receive's operand IS the channel.
	//
	// A self-contained element crosses as its own bytes: the extent check comes
	// first, so a pointer-free type pays a memcpy and not its Codec round trip.
	if !typeNeedsReloc(instr.X.Type()) {
		b.createExtentChanSend(instr)
		return
	}
	// Native: a chan whose element holds references routes through its Codec
	// when it has one, ChanSendDual at runtime, which dispatches on
	// ch.spawnBound. This preserves correctness through array / slice /
	// struct-field indirection where the compiler's per-SSA-value tracking
	// can't see that the chan was originally passed to spawn().
	if b.elemTypeImplementsCodec(instr.X.Type()) {
		b.createCodecChanSend(instr)
		return
	}
	b.createStructuralChanSend(instr)
}

// createExtentChanSend sends a self-contained value: the store into the alloca
// is the message, and runtime.chanSend routes a spawn-bound channel straight to
// PipeChanSendRaw, the memcpy of the extent.
func (b *builder) createExtentChanSend(instr *ssa.Send) {
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

// chanOperandValue resolves the channel a receive or select arm names. A spawn
// target's single result is the domain's breaker, but this compiler's SSA leaves
// that result unbound for a target that only reads it - the dumped SSA shows the
// operand as the nil constant, with no result alloca to bind (round 363). A
// receive on a nil channel blocks forever, so the arm would be dead and the
// domain could never be told to stop. A nil chan-of-text operand inside a spawn
// target is therefore that breaker, and it resolves to the one the domain
// actually got.
func (b *builder) chanOperandValue(v ssa.Value) llvm.Value {
	val := b.getValue(v, getPos(v))
	if b.fn == nil || !isSpawnTargetSig(b.fn.Signature) {
		return val
	}
	if !val.IsConstant() || !val.IsNull() {
		return val
	}
	ch, ok := v.Type().Underlying().(*types.Chan)
	if !ok || !breakerElem(ch.Elem()) {
		return val
	}
	brk := b.createRuntimeCall("ClosedBreakerChan", nil, "closed")
	return b.createRuntimeCall("AssertBreaker", []llvm.Value{brk}, "closed")
}

// createChanRecv emits a pseudo chan receive operation. It is lowered to the
// actual channel receive operation during goroutine lowering.
func (b *builder) createChanRecv(unop *ssa.UnOp) llvm.Value {
	chanType := unop.X.Type().Underlying().(*types.Chan)
	// A self-contained element is copied straight into the receiver's slot;
	// an element that holds references is decoded, which is its relocation.
	if !typeNeedsReloc(chanType.Elem()) {
		return b.createExtentChanRecv(unop)
	}
	// Native: dual-path runtime dispatch for Codec-implementing types.
	if b.elemTypeImplementsCodec(chanType.Elem()) {
		return b.createCodecChanRecv(unop)
	}
	return b.createStructuralChanRecv(unop)
}

// createExtentChanRecv is the recv half of the extent path:
// runtime.chanRecv routes a spawn-bound channel to PipeChanRecvRaw, which copies
// the frame into the receiver's own slot.
func (b *builder) createExtentChanRecv(unop *ssa.UnOp) llvm.Value {
	chanType := unop.X.Type().Underlying().(*types.Chan)

	valueType := b.getLLVMType(chanType.Elem())
	ch := b.chanOperandValue(unop.X)

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

	// Size the receive buffer first: a receive arm's state carries a codec whose
	// value pointer IS that buffer, so the buffer has to exist before the states
	// are built.
	recvbufSize := uint64(0)
	recvbufAlign := 0
	for _, state := range expr.States {
		if state.Dir != types.RecvOnly {
			continue
		}
		// Make sure the receive buffer is big enough and has the correct alignment.
		llvmType := b.getLLVMType(state.Chan.Type().Underlying().(*types.Chan).Elem())
		if size := b.targetData.TypeAllocSize(llvmType); size > recvbufSize {
			recvbufSize = size
		}
		if align := b.targetData.ABITypeAlignment(llvmType); align > recvbufAlign {
			recvbufAlign = align
		}
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

	// Go through all the cases. Create the selectStates slice.
	var selectStates []llvm.Value
	chanSelectStateType := b.getLLVMRuntimeType("chanSelectState")
	for _, state := range expr.States {
		ch := b.chanOperandValue(state.Chan)
		selectState := llvm.ConstNull(chanSelectStateType)
		selectState = b.CreateInsertValue(selectState, ch, 0, "")
		switch state.Dir {
		case types.RecvOnly:
			// value stays null: that is how the runtime recognises a receive.
			selectState = b.storeSelectFrame(selectState, state.Chan, recvbuf)
		case types.SendOnly:
			// Store this value in an alloca and put a pointer to this alloca
			// in the send state.
			sendValue := b.getValue(state.Send, state.Pos)
			alloca := llvmutil.CreateEntryBlockAlloca(b.Builder, sendValue.Type(), "select.send.value")
			b.CreateStore(sendValue, alloca)
			selectState = b.CreateInsertValue(selectState, alloca, 1, "")
			selectState = b.storeSelectFrame(selectState, state.Chan, alloca)
		default:
			panic("unreachable")
		}
		selectStates = append(selectStates, selectState)
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

// storeSelectFrame fills the frame-format words of a select state (see the
// runtime's chanSelectState): the element's own codec when its type has one, the
// structural codec's type descriptor when it does not, and nothing at all for a
// self-contained element, whose extent IS the message. dest is the slot the frame
// encodes from (send) or decodes into (receive).
func (b *builder) storeSelectFrame(state llvm.Value, ch ssa.Value, dest llvm.Value) llvm.Value {
	chanType := ch.Type().Underlying().(*types.Chan)
	elemType := chanType.Elem()
	if !typeNeedsReloc(elemType) {
		return state
	}
	if b.elemTypeImplementsCodec(chanType) {
		codec := b.createPipeChanCodecIface(elemType, dest)
		return b.CreateInsertValue(state, codec, 2, "")
	}
	return b.CreateInsertValue(state, b.getTypeCode(elemType), 3, "")
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
	ch := b.chanOperandValue(unop.X)

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

// typeNeedsReloc reports whether a channel element's bytes are NOT the message:
// the element holds a reference into an arena (a pointer, slice, string, map or
// interface, or a struct/array containing one), so a raw copy would hand the
// receiver an address whose frame dies when the sender returns. Measured on a
// runtime-built []byte before the codec path existed: the receiver got the right
// length and the bytes [4, 0, 0, ...] - the push allocator's first-batch marker,
// i.e. allocator metadata, not the payload.
//
// Such an element is encoded - by its own Codec when it has one, by the
// structural codec otherwise (runtime.ChanSendStructural / ChanRecvStructural) -
// and the decode is the relocation that gives the receiver its own copy.
//
// The complement is the extent path: a self-contained element crosses as its own
// bytes. Both ends of a spawn channel are threads in one address space running
// the same binary, so a pointer-free value needs no encoding at all.
//
// Channels and funcs count as self-contained: a channel is one object both ends
// hold (a capability, not a payload) and a func value is a code pointer. Both
// are handles, and a handle's bytes ARE the message.
func typeNeedsReloc(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.UnsafePointer || u.Kind() == types.String || u.Kind() == types.UntypedString
	case *types.Pointer:
		return true
	case *types.Slice:
		return true
	case *types.Map:
		return true
	case *types.Interface:
		return true
	case *types.Array:
		return typeNeedsReloc(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if typeNeedsReloc(u.Field(i).Type()) {
				return true
			}
		}
		return false
	case *types.Signature:
		return false
	case *types.Chan:
		return false
	}
	return true
}

// createStructuralChanSend emits the dual-path chan send for an element type
// that holds pointers: runtime.ChanSendStructural(ch, td, &v, &op) encodes the
// value with the structural codec when the channel is spawn-bound and takes the
// in-runtime queue path otherwise.
func (b *builder) createStructuralChanSend(instr *ssa.Send) {
	ch := b.getValue(instr.Chan, getPos(instr))
	chanValue := b.getValue(instr.X, getPos(instr))
	elemType := instr.X.Type()
	elemLLVM := b.getLLVMType(elemType)

	valAlloca, valAllocaSize := b.createTemporaryAlloca(elemLLVM, "chan.value")
	b.CreateStore(chanValue, valAlloca)
	typeDesc := b.getTypeCode(elemType)

	channelOpType := b.getLLVMRuntimeType("channelOp")
	opAlloca, opAllocaSize := b.createTemporaryAlloca(channelOpType, "chan.op")

	runtimePkg := b.program.ImportedPackage("runtime")
	sendFn := runtimePkg.Members["ChanSendStructural"].(*ssa.Function)
	sendType, sendLLVM := b.getFunction(sendFn)

	args := []llvm.Value{ch, typeDesc, valAlloca, opAlloca, llvm.Undef(b.dataPtrType)}
	b.createCall(sendType, sendLLVM, args, "")

	b.emitLifetimeEnd(opAlloca, opAllocaSize)
	b.emitLifetimeEnd(valAlloca, valAllocaSize)
}

// createStructuralChanRecv is the recv counterpart: the runtime decodes the
// frame into the receiver's own slot, so a slice arrives with a fresh backing in
// the receiver's arena.
func (b *builder) createStructuralChanRecv(unop *ssa.UnOp) llvm.Value {
	elemType := unop.X.Type().Underlying().(*types.Chan).Elem()
	elemLLVM := b.getLLVMType(elemType)
	ch := b.chanOperandValue(unop.X)

	valAlloca, valAllocaSize := b.createTemporaryAlloca(elemLLVM, "chan.value")
	b.CreateStore(llvm.ConstNull(elemLLVM), valAlloca)
	typeDesc := b.getTypeCode(elemType)

	channelOpType := b.getLLVMRuntimeType("channelOp")
	opAlloca, opAllocaSize := b.createTemporaryAlloca(channelOpType, "chan.op")

	runtimePkg := b.program.ImportedPackage("runtime")
	recvFn := runtimePkg.Members["ChanRecvStructural"].(*ssa.Function)
	recvType, recvLLVM := b.getFunction(recvFn)

	args := []llvm.Value{ch, typeDesc, valAlloca, opAlloca, llvm.Undef(b.dataPtrType)}
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
