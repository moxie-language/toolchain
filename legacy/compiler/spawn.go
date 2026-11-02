package compiler

// This file implements the 'spawn' mechanism for creating isolated domains.
// spawn creates a new OS process (native) or Service Worker (JS) with its
// own cooperative scheduler. Arguments are serialized across the boundary.
// Channels become IPC channels.

import (
	"fmt"
	"go/constant"
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

	// Wasm target uses a completely different spawn mechanism (Workers + SAB channels).
	if b.isWasmTarget() {
		return b.createWasmSpawn(instr)
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

	// Validate that the function signature's parameters are spawn-safe
	// and implement moxie.Codec.
	sig := targetFn.Signature
	for i := 0; i < sig.Params().Len(); i++ {
		if err := validateSpawnArg(sig.Params().At(i).Type(), i, codecIface); err != nil {
			b.addError(instr.Pos(), err.Error())
			return llvm.Value{}, nil
		}
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
		params = append(params, b.expandFormalParam(b.getValue(arg, instr.Pos()))...)
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

	// Return a lifecycle channel (chan struct{}) that closes when the child exits.
	lcFnType, lcFn := b.getFunction(b.program.ImportedPackage("runtime").Members["SpawnLifecycleChannel"].(*ssa.Function))
	doneCh := b.createCall(lcFnType, lcFn, []llvm.Value{llvm.Undef(b.dataPtrType)}, "done.ch")
	return doneCh, nil
}

// validateSpawnArg checks that a type is safe to pass across a spawn boundary.
// Channels are allowed as bare top-level parameters (they become IPC channels);
// their element types must implement moxie.Codec. Non-channel arguments must
// implement moxie.Codec directly. No interface types anywhere.
func validateSpawnArg(t types.Type, argIndex int, codec *types.Interface) error {
	if ch, ok := t.Underlying().(*types.Chan); ok {
		// Top-level channel: element type must be spawn-safe and implement Codec.
		return validateSpawnElem(ch.Elem(), argIndex, codec)
	}
	return validateSpawnElem(t, argIndex, codec)
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
