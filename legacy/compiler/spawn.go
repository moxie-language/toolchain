package compiler

// This file implements the 'spawn' mechanism for creating isolated domains.
// spawn creates a new OS process (native) or Service Worker (JS) with its
// own cooperative scheduler. Arguments are serialized across the boundary.
// Channels become IPC channels.

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"

	"moxie/loader"
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// createSpawn handles spawn(fn, args...) builtin calls. The compiler emits
// a call to runtime.spawnDomain with the function pointer and serialized
// arguments.
//
// As a builtin, the SSA passes args directly — no MakeInterface wrapping,
// no variadic slice packing.
//
// If the first argument is a string constant, it specifies the transport:
//   spawn("local", fn, args...)  — fork+socketpair (default)
//   spawn("pipe", fn, args...)   — alias for local
//   spawn(fn, args...)           — implicit local
func (b *builder) createSpawn(instr *ssa.CallCommon) (llvm.Value, error) {
	if len(instr.Args) < 1 {
		b.addError(instr.Pos(), "spawn requires at least a function argument")
		return llvm.Value{}, nil
	}

	// Wasm spawn is refused, not emitted. The parent threads channel handles,
	// but the target's `closed` breaker result is never bound in the child, so
	// its select lowers to a receive through a null channel - invalid IR, and
	// undefined behaviour even if it linked. A named compile error is the
	// honest state until the breaker handle is threaded through the wasm entry.
	if b.isWasmTarget() {
		b.addError(instr.Pos(), "spawn: spawn is not supported on the wasm target yet - a spawned domain's breaker and channel handles are not threaded, so the child would receive through a null channel (native only)")
		return llvm.Value{}, nil
	}
	// Check if the first argument is a transport string constant.
	transport := "local"
	argStart := 0 // index of the function argument
	if transportStr, ok := extractTransportString(instr.Args[0]); ok {
		transport = transportStr
		argStart = 1
	}

	// Validate transport.
	switch transport {
	case "local", "pipe":
		// OK — use fork+socketpair.
	default:
		if len(transport) > 0 && (hasScheme(transport, "tcp") || hasScheme(transport, "ws") || hasScheme(transport, "wss")) {
			b.addError(instr.Pos(), fmt.Sprintf("spawn: network transport %q not yet implemented", transport))
			return llvm.Value{}, nil
		}
		b.addError(instr.Pos(), fmt.Sprintf("spawn: unknown transport %q (expected \"local\", \"pipe\", or a network URL)", transport))
		return llvm.Value{}, nil
	}

	if argStart >= len(instr.Args) {
		b.addError(instr.Pos(), "spawn requires a function argument after transport string")
		return llvm.Value{}, nil
	}

	// Extract the function argument.
	fnArg := instr.Args[argStart]
	var targetFn *ssa.Function
	switch v := fnArg.(type) {
	case *ssa.Function:
		targetFn = v
	case *ssa.MakeClosure:
		if fn, ok := v.Fn.(*ssa.Function); ok {
			targetFn = fn
		}
	}

	if targetFn == nil {
		b.addError(instr.Pos(), "spawn: first argument must be a static function (dynamic function values not yet supported)")
		return llvm.Value{}, nil
	}

	// Data args are everything after the function.
	concreteArgs := instr.Args[argStart+1:]

	// Look up the moxie.Codec interface for spawn-boundary type checking.
	// runtime transitively imports moxie, so this should always succeed;
	// the explicit errors turn a formerly-silent permissive path into a
	// clear compiler failure if the stdlib wiring is ever broken.
	moxiePkg := b.program.ImportedPackage("moxie")
	if moxiePkg == nil {
		b.addError(instr.Pos(), "spawn: internal error — \"moxie\" package not in program (runtime wiring broken)")
		return llvm.Value{}, nil
	}
	codecObj := moxiePkg.Pkg.Scope().Lookup("Codec")
	if codecObj == nil {
		b.addError(instr.Pos(), "spawn: internal error — moxie.Codec symbol missing from moxie package")
		return llvm.Value{}, nil
	}
	codecIface, ok := codecObj.Type().Underlying().(*types.Interface)
	if !ok {
		b.addError(instr.Pos(), "spawn: internal error — moxie.Codec is not an interface type")
		return llvm.Value{}, nil
	}

	// Spawn ABI: a parameter is either a channel (the domain's only route to
	// another domain) or a static startup value, serialized at the spawn call
	// and frozen in the child from that moment. It must not return a value: the
	// breaker that closes when the domain terminates is the channel spawn
	// returns.
	sig := targetFn.Signature
	if !isSpawnTargetSig(sig) {
		b.addError(instr.Pos(), fmt.Sprintf(
			"spawn: %s must be declared with exactly one result, (closed chan struct{}), which is the domain's breaker; a function without it cannot be spawned",
			targetFn.Name()))
		return llvm.Value{}, nil
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if err := validateSpawnArg(sig.Params().At(i).Type(), i, codecIface); err != nil {
			b.addError(instr.Pos(), err.Error())
			return llvm.Value{}, nil
		}
	}
	if globalName, found := b.spawnTargetReadsChannelGlobal(targetFn); found {
		b.addError(instr.Pos(), fmt.Sprintf(
			"spawn: %s reads channel global %s; a spawned domain's channels must be its parameters, so pass it through the spawn call",
			targetFn.Name(), globalName))
		return llvm.Value{}, nil
	}
	if !closedSelectPresent(targetFn) {
		b.addError(instr.Pos(), fmt.Sprintf(
			"spawn: %s never selects on its closed breaker; a spawned domain must handle termination, so its loop selects on <-closed and returns the breaker",
			targetFn.Name()))
		return llvm.Value{}, nil
	}

	if len(concreteArgs) != sig.Params().Len() {
		b.addError(instr.Pos(), fmt.Sprintf("spawn: %s expects %d arguments, got %d",
			targetFn.Name(), sig.Params().Len(), len(concreteArgs)))
		return llvm.Value{}, nil
	}

	// Verify argument types match the target function's parameter types.
	for i, arg := range concreteArgs {
		paramType := sig.Params().At(i).Type()
		argType := arg.Type()
		if !types.Identical(argType.Underlying(), paramType.Underlying()) {
			b.addError(instr.Pos(), fmt.Sprintf("spawn: argument %d has type %s, %s expects %s",
				i+1, argType, targetFn.Name(), paramType))
			return llvm.Value{}, nil
		}
	}

	// Buffered channels are the mechanism, not a special case: a spawn channel's
	// declaration states how many messages may be outstanding (`chan T{:N}`,
	// one slot when unsized), and the runtime reads that depth from the handle
	// at send time. Nothing about the buffer needs to be known here.

	// Collect channel args (in declaration order) for post-spawn marking.
	var chanArgs []spawnChanArg
	for _, arg := range concreteArgs {
		if _, isChan := arg.Type().Underlying().(*types.Chan); isChan {
			chanArgs = append(chanArgs, spawnChanArg{
				ssa:  arg,
				llvm: b.getValue(arg, instr.Pos()),
			})
		}
	}

	var params []llvm.Value
	for _, arg := range concreteArgs {
		v := b.getValue(arg, instr.Pos())
		// Channels are shared; values are copied. A value's own bytes are
		// copied by the bundle, but its slices and pointers would still look
		// into this frame, so the runtime copies the whole graph at the
		// boundary (and refuses to if it does not fit its declared budget).
		if _, isChan := arg.Type().Underlying().(*types.Chan); isChan {
			params = append(params, b.expandFormalParam(v)...)
			continue
		}
		params = append(params, b.spawnArgCopy(arg.Type(), v, instr.Pos())...)
	}

	paramBundle := b.emitPointerPack(params)

	// Get the LLVM function.
	funcType, funcPtr := b.getFunction(targetFn)

	// Create a goroutine-style wrapper that unpacks args and calls the function.
	wrapper := b.createGoroutineStartWrapper(funcType, funcPtr, "spawn", false, false, instr.Pos())

	// Note every channel's ring slot before the domain exists: spawnDomain
	// sizes each ring pair from that channel's declared depth and its
	// compiler-computed frame budget, both of which travel on the handle.
	target := b.nativeSpawnTargetFor(targetFn)
	if target != nil && len(chanArgs) > 0 {
		note := b.program.ImportedPackage("runtime").Members["NoteSpawnChan"].(*ssa.Function)
		noteType, noteFn := b.getFunction(note)
		i32 := b.ctx.Int32Type()
		for i, ca := range chanArgs {
			b.CreateCall(noteType, noteFn, []llvm.Value{
				llvm.ConstInt(i32, uint64(target.chanIDs[i]-1), false),
				ca.llvm,
				llvm.Undef(b.dataPtrType),
			}, "")
		}
	}

	// Call runtime.spawnDomain(wrapper, paramBundle, nChans).
	nChans := llvm.ConstInt(b.ctx.Int32Type(), uint64(len(chanArgs)), false)
	spawnFnType, spawnFn := b.getFunction(b.program.ImportedPackage("runtime").Members["spawnDomain"].(*ssa.Function))
	b.createCall(spawnFnType, spawnFn, []llvm.Value{wrapper, paramBundle, nChans, llvm.Undef(b.dataPtrType)}, "")

	// Mark every channel arg pipe-bound on the parent side. The child's
	// setupNativeSpawnTargetChannels does the symmetric mark with ChildPipeFd.
	if target != nil {
		b.emitNativeSpawnChannelMark(target, chanArgs)
	}

	// Block until the child signals ready.
	waitFnType, waitFn := b.getFunction(b.program.ImportedPackage("runtime").Members["PipeWaitChildReady"].(*ssa.Function))
	b.createCall(waitFnType, waitFn, []llvm.Value{llvm.Undef(b.dataPtrType)}, "child.ready")

	// Return the breaker channel (chan struct{}) that closes when the child
	// exits.
	//
	// The compiler does NOT close it at return. A frame returning is not the
	// owner deciding a domain is finished: the function may be handing the
	// breaker to its caller, which is exactly what a worker-pool constructor
	// does, and closing it there killed the worker microseconds after its
	// thread started. The owner ends a domain with close(breaker); the process
	// ending reclaims every domain there is.
	lcFnType, lcFn := b.getFunction(b.program.ImportedPackage("runtime").Members["SpawnLifecycleChannel"].(*ssa.Function))
	doneCh := b.createCall(lcFnType, lcFn, []llvm.Value{llvm.Undef(b.dataPtrType)}, "done.ch")
	return doneCh, nil
}

// terminationConsumed reports whether fn consumes a termination signal from
// chanVal (or, when wantSigint is set, from the sigint builtin's channel).
//
// A select arm is the intended shape, but a SINGLE-ARM select is lowered by the
// SSA builder into a plain receive - `select { case <-closed: ...; default: }`
// becomes a non-blocking receive, and `select { case <-closed: ... }` a blocking
// one - so requiring a Select instruction would refuse correct code that this
// compiler itself produced. The rule is therefore about the signal being
// consumed, and either shape satisfies it.
func terminationConsumed(fn *ssa.Function, chanVal ssa.Value, wantSigint bool) bool {
	if fn == nil || len(fn.Blocks) == 0 {
		return true
	}
	match := func(ch ssa.Value) bool {
		if wantSigint {
			call, ok := ch.(*ssa.Call)
			if !ok {
				return false
			}
			bi, ok := call.Call.Value.(*ssa.Builtin)
			return ok && bi.Name() == "__sigint"
		}
		if ch == chanVal {
			return true
		}
		if u, ok := ch.(*ssa.UnOp); ok && u.X == chanVal {
			return true
		}
		// A target that only reads its closed result gets the nil constant
		// here (no result alloca; see chanOperandValue, which resolves that
		// operand to the domain's breaker at emission). The consumption is
		// real, so the check counts it.
		if c, ok := ch.(*ssa.Const); ok && c.IsNil() {
			if tc, ok2 := ch.Type().Underlying().(*types.Chan); ok2 && breakerElem(tc.Elem()) {
				return true
			}
		}
		return false
	}
	for _, blk := range fn.Blocks {
		for _, instr := range blk.Instrs {
			switch in := instr.(type) {
			case *ssa.Select:
				for _, st := range in.States {
					if match(st.Chan) {
						return true
					}
				}
			case *ssa.UnOp:
				if in.Op == token.ARROW && match(in.X) {
					return true
				}
			}
		}
	}
	return false
}

// closedSelectPresent reports whether fn consumes its own closed breaker's
// termination signal. The prologue binds the result alloca (Comment "closed"),
// so the operand is that alloca or a load of it.
func closedSelectPresent(fn *ssa.Function) bool {
	if fn == nil || len(fn.Blocks) == 0 {
		return true
	}
	var closedAlloc *ssa.Alloc
	for _, blk := range fn.Blocks {
		for _, instr := range blk.Instrs {
			if a, ok := instr.(*ssa.Alloc); ok && a.Comment == "closed" {
				closedAlloc = a
			}
		}
	}
	if closedAlloc == nil {
		// The builder did not materialize the result variable: the target's
		// body reads the nil constant instead (round 363). chanOperandValue
		// resolves that operand to the domain's breaker when the body is
		// emitted, so the consumption is still checkable - against the nil
		// operand itself, which terminationConsumed accepts for this shape.
		return terminationConsumed(fn, nil, false) && closedConsumedAsNil(fn)
	}
	return terminationConsumed(fn, closedAlloc, false)
}

// closedConsumedAsNil reports whether fn consumes the breaker through the nil
// operand this compiler produces for an unbound result: a select arm or receive
// whose channel is a nil chan-of-text value. It is the shape
// chanOperandValue resolves at emission, so the check and the lowering agree.
func closedConsumedAsNil(fn *ssa.Function) bool {
	for _, blk := range fn.Blocks {
		for _, instr := range blk.Instrs {
			switch in := instr.(type) {
			case *ssa.Select:
				for _, st := range in.States {
					if c, ok := st.Chan.(*ssa.Const); ok && c.IsNil() {
						if tc, ok2 := st.Chan.Type().Underlying().(*types.Chan); ok2 && breakerElem(tc.Elem()) {
							return true
						}
					}
				}
			case *ssa.UnOp:
				if in.Op != token.ARROW {
					continue
				}
				if c, ok := in.X.(*ssa.Const); ok && c.IsNil() {
					if tc, ok2 := in.X.Type().Underlying().(*types.Chan); ok2 && breakerElem(tc.Elem()) {
						return true
					}
				}
			}
		}
	}
	return false
}

// sigintSelectPresent reports whether fn consumes the termination channel.
func sigintSelectPresent(fn *ssa.Function) bool {
	return terminationConsumed(fn, nil, true)
}

// checkEntrySelect enforces the termination rule at the program entry: a
// program that cannot be told to stop has to be killed, and a domain's whole
// point is that it stops when asked. In this compiler a `main` that survives to
// codegen IS the entry (a library package's main is renamed to init by the
// loader), so the name is the test.
func (b *builder) checkEntrySelect() {
	// The process entry (`runtime.main`, called by C) is also named main; the
	// PROGRAM entry is the main function of the package named main.
	if b.fn == nil || b.fn.Name() != "main" || len(b.fn.Blocks) == 0 {
		return
	}
	if b.fn.Pkg == nil || b.fn.Pkg.Pkg == nil || b.fn.Pkg.Pkg.Name() != "main" {
		return
	}
	if sigintSelectPresent(b.fn) {
		return
	}
	b.addError(b.fn.Pos(), "main must select on sigint: add a `case <-sigint:` arm to its select (or a non-blocking one at entry for a batch program), because a program that cannot be told to stop has to be killed")
}

// breakerElem reports whether T is the element of a breaker channel: the empty
// struct, because a breaker carries no message. A spawn target's `closed`
// result and the channel `spawn` returns are both typed with it.
func breakerElem(T types.Type) bool {
	st, ok := T.Underlying().(*types.Struct)
	return ok && st.NumFields() == 0
}

// isSpawnTargetSig reports whether sig is the declared shape of a spawned
// function: exactly one result, of type chan []byte. That result is the
// domain's breaker - the same channel spawn returns - and its presence in the
// signature is what marks the function as a spawn target.
func isSpawnTargetSig(sig *types.Signature) bool {
	if sig == nil || sig.Results().Len() != 1 {
		return false
	}
	r := sig.Results().At(0)
	// The result must be NAMED `closed`, and it must be the only one. That name
	// is the entry point's whole contract: the language predeclares `closed` as
	// the word a spawn target selects on, and the runtime binds the domain's
	// breaker into it. Matching the shape alone made every ordinary function
	// that happens to return a `chan []byte` a spawn target, so calling it
	// directly died in its prologue with "spawn target called directly".
	if r.Name() != "closed" {
		return false
	}
	ch, ok := r.Type().Underlying().(*types.Chan)
	if !ok || ch.Dir() != types.SendRecv {
		return false
	}
	// The breaker is a signal, so its element is the empty struct: nothing is
	// ever sent on it, and a close is the whole message.
	st, okSt := ch.Elem().Underlying().(*types.Struct)
	return okSt && st != nil && st.NumFields() == 0
}

// spawnArgCopy hands one value parameter across the spawn boundary as a frozen
// copy. The runtime encodes the value against the budget the compiler's cap-tag
// walk computes - the same walk that sizes a spawn channel's ring - panics if it
// does not fit, and decodes it into the root arena, so the child's slices and
// pointers do not point into this frame. The value is then loaded out of the
// copy, which is what the bundle carries and the child receives.
func (b *builder) spawnArgCopy(t types.Type, v llvm.Value, pos token.Pos) []llvm.Value {
	llvmType := b.getLLVMType(t)
	alloca, size := b.createTemporaryAlloca(llvmType, "spawn.arg")
	b.CreateStore(v, alloca)
	budget := b.frameBudget(t, nil, 0, pos)
	note := b.frameNote(t, "", "")
	notePtr, noteLen := b.budgetNoteOperand(note)
	desc := b.getTypeCode(t)
	i32 := b.ctx.Int32Type()
	copied := b.createRuntimeCall("SpawnArgCopy", []llvm.Value{
		alloca,
		desc,
		llvm.ConstInt(i32, uint64(budget), false),
		notePtr,
		noteLen,
	}, "spawn.argcopy")
	val := b.CreateLoad(llvmType, copied, "spawn.argval")
	b.emitLifetimeEnd(alloca, size)
	return b.expandFormalParam(val)
}

// validateSpawnArg checks one parameter of a spawned function. A channel
// parameter becomes IPC; the element type must be spawn-safe. A non-channel
// parameter is a static startup value: it is serialized at the spawn call and
// frozen in the child from that moment, so it must implement moxie.Codec.
func validateSpawnArg(t types.Type, argIndex int, codec *types.Interface) error {
	if ch, ok := t.Underlying().(*types.Chan); ok {
		return validateSpawnElem(ch.Elem(), argIndex, codec)
	}
	return validateSpawnElem(t, argIndex, codec)
}

// spawnTargetReadsChannelGlobal reports the first channel-typed package global
// the target's own body references. A spawned domain may only use channels it
// received as parameters: a global channel is shared memory nobody negotiated,
// and the child's copy of its state is a fork-time snapshot.
func (b *builder) spawnTargetReadsChannelGlobal(fn *ssa.Function) (name string, found bool) {
	for _, blk := range fn.Blocks {
		for _, instr := range blk.Instrs {
			switch in := instr.(type) {
			case *ssa.UnOp:
				if g, ok := in.X.(*ssa.Global); ok {
					if _, isChan := g.Type().Underlying().(*types.Chan); isChan {
						return g.Name(), true
					}
				}
			case *ssa.Store:
				if g, ok := in.Addr.(*ssa.Global); ok {
					if _, isChan := g.Type().Underlying().(*types.Chan); isChan {
						return g.Name(), true
					}
				}
			}
		}
	}
	return "", false
}

// validateSpawnElem rejects types that can't be serialized across a spawn
// boundary. Named types that implement moxie.Codec are accepted immediately.
// Channels are rejected here - they're only allowed at the top level.
// codec is non-nil (createSpawn enforces moxie.Codec availability before call).
func validateSpawnElem(t types.Type, argIndex int, codec *types.Interface) error {
	if loader.ImplementsCodecIface(t, codec) {
		return nil
	}

	switch ut := t.Underlying().(type) {
	case *types.Basic:
		if isSpawnSafeBasic(ut) {
			return nil
		}
		return fmt.Errorf("spawn: argument %d type %s is not spawn-safe", argIndex+1, t)
	case *types.Pointer:
		if loader.ImplementsCodecIface(t, codec) {
			return nil
		}
		return fmt.Errorf("spawn: argument %d is a pointer type %s without Codec; raw pointers cannot cross spawn boundaries", argIndex+1, ut)
	case *types.Chan:
		return fmt.Errorf("spawn: argument %d contains a nested channel (channels must be top-level parameters)", argIndex+1)
	case *types.Slice:
		return validateSpawnElem(ut.Elem(), argIndex, codec)
	case *types.Map:
		if err := validateSpawnElem(ut.Key(), argIndex, codec); err != nil {
			return err
		}
		return validateSpawnElem(ut.Elem(), argIndex, codec)
	case *types.Signature:
		return fmt.Errorf("spawn: argument %d is a function type (not allowed across spawn boundary)", argIndex+1)
	case *types.Interface:
		return fmt.Errorf("spawn: argument %d is an interface type (not allowed across spawn boundary)", argIndex+1)
	case *types.Struct:
		for i := 0; i < ut.NumFields(); i++ {
			if err := validateSpawnElem(ut.Field(i).Type(), argIndex, codec); err != nil {
				return err
			}
		}
		return nil
	case *types.Array:
		return validateSpawnElem(ut.Elem(), argIndex, codec)
	}

	return fmt.Errorf("spawn: argument %d type %s does not implement moxie.Codec (use moxie.Int32, moxie.Uint64, etc.)", argIndex+1, t)
}

// isSpawnSafeBasic returns true for basic types that are safe to copy across
// a fork-based spawn boundary (same binary, same architecture). All concrete
// scalar types are safe; untyped types and unsafe.Pointer are not.
func isSpawnSafeBasic(b *types.Basic) bool {
	switch b.Kind() {
	case types.Bool, types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr,
		types.Float32, types.Float64, types.Complex64, types.Complex128,
		types.String:
		return true
	}
	return false
}

// extractTransportString checks if an SSA value is a string constant
// (possibly wrapped in MakeInterface) and returns it.
func extractTransportString(v ssa.Value) (string, bool) {
	if mi, ok := v.(*ssa.MakeInterface); ok {
		v = mi.X
	}
	c, ok := v.(*ssa.Const)
	if !ok {
		return "", false
	}
	if c.Value == nil || c.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(c.Value), true
}

// traceToMakeChan walks an SSA value back to its MakeChan definition.
// Returns nil if the channel origin can't be determined (parameter, load, etc).
func traceToMakeChan(v ssa.Value) *ssa.MakeChan {
	switch v := v.(type) {
	case *ssa.MakeChan:
		return v
	case *ssa.MakeInterface:
		return traceToMakeChan(v.X)
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if mc := traceToMakeChan(edge); mc != nil {
				return mc
			}
		}
	}
	return nil
}

// hasScheme checks if s starts with scheme://.
func hasScheme(s, scheme string) bool {
	if len(s) < len(scheme)+3 {
		return false
	}
	for i := 0; i < len(scheme); i++ {
		if s[i] != scheme[i] {
			return false
		}
	}
	return s[len(scheme)] == ':' && s[len(scheme)+1] == '/' && s[len(scheme)+2] == '/'
}
