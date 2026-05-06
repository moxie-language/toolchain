package compiler

// This file implements the 'spawn' mechanism for creating isolated domains.
// spawn creates a new OS process (native) or Service Worker (JS) with its
// own cooperative scheduler. Arguments are serialized across the boundary.
// Channels become IPC channels.

import (
	"fmt"
	"go/constant"
	"go/types"

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

	// Reject buffered channels across spawn boundaries.
	for i, arg := range concreteArgs {
		if _, isChan := arg.Type().Underlying().(*types.Chan); !isChan {
			continue
		}
		if mc := traceToMakeChan(arg); mc != nil {
			if c, ok := mc.Size.(*ssa.Const); ok {
				if c.Value != nil && constant.Sign(c.Value) > 0 {
					b.addError(instr.Pos(), fmt.Sprintf(
						"spawn: argument %d is a buffered channel — only unbuffered channels allowed across spawn boundaries",
						i+1))
					return llvm.Value{}, nil
				}
			} else {
				b.addError(instr.Pos(), fmt.Sprintf(
					"spawn: argument %d has dynamic channel buffer size — only unbuffered channels (make(chan T)) allowed across spawn boundaries",
					i+1))
				return llvm.Value{}, nil
			}
		}
	}

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

	// Call runtime.spawnDomain(wrapper, paramBundle).
	spawnFnType, spawnFn := b.getFunction(b.program.ImportedPackage("runtime").Members["spawnDomain"].(*ssa.Function))
	b.createCall(spawnFnType, spawnFn, []llvm.Value{wrapper, paramBundle, llvm.Undef(b.dataPtrType)}, "")

	// Mark every channel arg pipe-bound on the parent side. The child's
	// setupNativeSpawnTargetChannels does the symmetric mark with ChildPipeFd.
	if target := b.nativeSpawnTargetFor(targetFn); target != nil {
		b.emitNativeSpawnChannelMark(target, chanArgs)
	}

	// Return a lifecycle channel (chan struct{}).
	// TODO: wire this to the socketpair so close propagates.
	chanType := b.getLLVMRuntimeType("channel")
	return llvm.ConstPointerNull(llvm.PointerType(chanType, 0)), nil
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
// Channels are rejected here — they're only allowed at the top level.
// codec is non-nil (createSpawn enforces moxie.Codec availability before call).
func validateSpawnElem(t types.Type, argIndex int, codec *types.Interface) error {
	// Named type that implements Codec — accept directly.
	// This handles moxie.Int32, moxie.Bytes, user structs with Codec methods, etc.
	if implementsCodec(t, codec) {
		return nil
	}

	switch ut := t.Underlying().(type) {
	case *types.Pointer:
		// A pointer that itself implements Codec is acceptable: the codec
		// handles the contents, the receiver gets a fresh allocation. This
		// is copy-not-alias semantics — distinct from sharing memory across
		// the boundary, which is impossible by construction (no shared address
		// space across fork or workers).
		if implementsCodec(t, codec) {
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

	// Leaf type that doesn't implement Codec.
	return fmt.Errorf("spawn: argument %d type %s does not implement moxie.Codec (use moxie.Int32, moxie.Uint64, etc.)", argIndex+1, t)
}

// implementsCodec checks whether t or *t satisfies the Codec interface.
func implementsCodec(t types.Type, codec *types.Interface) bool {
	return types.Implements(t, codec) || types.Implements(types.NewPointer(t), codec)
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
