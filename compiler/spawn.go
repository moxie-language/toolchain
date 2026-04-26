package compiler

// This file implements the 'spawn' mechanism for creating isolated domains.
// spawn creates a new OS process (native) or Service Worker (JS) with its
// own cooperative scheduler. Arguments are serialized across the boundary.
// Channels become IPC channels.

import (
	"fmt"
	"go/constant"

	"golang.org/x/tools/go/ssa"
	"moxie/mxssa"
	"moxie/typecheck"
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
// validateSpawnArgTC checks that a type is safe to pass across a spawn boundary.
// Channels are allowed as bare top-level parameters (they become IPC channels);
// their element types must implement moxie.Codec. Non-channel arguments must
// implement moxie.Codec directly. No interface types anywhere.
func validateSpawnArgTC(t typecheck.Type, argIndex int, codec *typecheck.Interface) error {
	if ch, ok := t.Underlying().(*typecheck.Chan); ok {
		return validateSpawnElemTC(ch.Elem(), argIndex, codec)
	}
	return validateSpawnElemTC(t, argIndex, codec)
}

// validateSpawnElemTC rejects types that can't be serialized across a spawn
// boundary. Named types that implement moxie.Codec are accepted immediately.
// Channels are rejected here — they're only allowed at the top level.
func validateSpawnElemTC(t typecheck.Type, argIndex int, codec *typecheck.Interface) error {
	if implementsCodecTC(t, codec) {
		return nil
	}
	switch ut := t.Underlying().(type) {
	case *typecheck.Pointer:
		return fmt.Errorf("spawn: argument %d is a pointer type %s (not allowed across spawn boundary)", argIndex+1, ut)
	case *typecheck.Chan:
		return fmt.Errorf("spawn: argument %d contains a nested channel (channels must be top-level parameters)", argIndex+1)
	case *typecheck.Slice:
		return validateSpawnElemTC(ut.Elem(), argIndex, codec)
	case *typecheck.Map:
		if err := validateSpawnElemTC(ut.Key(), argIndex, codec); err != nil {
			return err
		}
		return validateSpawnElemTC(ut.Elem(), argIndex, codec)
	case *typecheck.Signature:
		return fmt.Errorf("spawn: argument %d is a function type (not allowed across spawn boundary)", argIndex+1)
	case *typecheck.Interface:
		return fmt.Errorf("spawn: argument %d is an interface type (not allowed across spawn boundary)", argIndex+1)
	case *typecheck.Struct:
		for i := 0; i < ut.NumFields(); i++ {
			if err := validateSpawnElemTC(ut.Field(i).Type(), argIndex, codec); err != nil {
				return err
			}
		}
		return nil
	case *typecheck.Array:
		return validateSpawnElemTC(ut.Elem(), argIndex, codec)
	}
	return fmt.Errorf("spawn: argument %d type %s does not implement moxie.Codec (use moxie.Int32, moxie.Uint64, etc.)", argIndex+1, t)
}

// implementsCodecTC checks whether t or *t satisfies the Codec interface.
func implementsCodecTC(t typecheck.Type, codec *typecheck.Interface) bool {
	return typecheck.Implements(t, codec) || typecheck.Implements(typecheck.NewPointer(t), codec)
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

func (b *builder) createSpawnTC(instr *mxssa.CallCommon) (llvm.Value, error) {
	if len(instr.Args) < 1 {
		b.addError(instr.Pos(), "spawn requires at least a function argument")
		return llvm.Value{}, nil
	}

	transport := "local"
	argStart := 0
	if transportStr, ok := extractTransportStringTC(instr.Args[0]); ok {
		transport = transportStr
		argStart = 1
	}

	switch transport {
	case "local", "pipe":
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

	fnArg := instr.Args[argStart]
	var targetFn *mxssa.Function
	switch v := fnArg.(type) {
	case *mxssa.Function:
		targetFn = v
	case *mxssa.MakeClosure:
		if fn, ok := v.Fn.(*mxssa.Function); ok {
			targetFn = fn
		}
	}

	if targetFn == nil {
		b.addError(instr.Pos(), "spawn: first argument must be a static function (dynamic function values not yet supported)")
		return llvm.Value{}, nil
	}

	concreteArgs := instr.Args[argStart+1:]

	// Look up moxie.Codec via the typecheck importer (cached, populates on
	// demand). Going through ssa.Program would also work but mxssa.Program
	// only registers packages eagerly translated by ssabridge.
	moxieTC, _ := b.mxTranslator.Importer().Import("moxie")
	if moxieTC == nil {
		b.addError(instr.Pos(), "spawn: internal error — \"moxie\" package not in program (runtime wiring broken)")
		return llvm.Value{}, nil
	}
	codecObj := moxieTC.Scope().Lookup("Codec")
	if codecObj == nil {
		b.addError(instr.Pos(), "spawn: internal error — moxie.Codec symbol missing from moxie package")
		return llvm.Value{}, nil
	}
	codecTC, ok := codecObj.Type().Underlying().(*typecheck.Interface)
	if !ok {
		b.addError(instr.Pos(), "spawn: internal error — moxie.Codec is not an interface type")
		return llvm.Value{}, nil
	}

	sig := targetFn.Signature
	for i := 0; i < sig.Params().Len(); i++ {
		pt := sig.Params().At(i).Type()
		if err := validateSpawnArgTC(pt, i, codecTC); err != nil {
			b.addError(instr.Pos(), err.Error())
			return llvm.Value{}, nil
		}
	}

	if len(concreteArgs) != sig.Params().Len() {
		b.addError(instr.Pos(), fmt.Sprintf("spawn: %s expects %d arguments, got %d",
			targetFn.Name(), sig.Params().Len(), len(concreteArgs)))
		return llvm.Value{}, nil
	}

	for i, arg := range concreteArgs {
		paramType := sig.Params().At(i).Type()
		argType := arg.Type()
		if !typecheck.Identical(argType.Underlying(), paramType.Underlying()) {
			b.addError(instr.Pos(), fmt.Sprintf("spawn: argument %d has type %s, %s expects %s",
				i+1, argType, targetFn.Name(), paramType))
			return llvm.Value{}, nil
		}
	}

	for i, arg := range concreteArgs {
		if _, isChan := arg.Type().Underlying().(*typecheck.Chan); !isChan {
			continue
		}
		if mc := traceToMakeChanTC(arg); mc != nil {
			if c, ok := mc.Size.(*mxssa.Const); ok {
				if c.Value() != nil && constant.Sign(c.Value()) > 0 {
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

	var params []llvm.Value
	for _, arg := range concreteArgs {
		params = append(params, b.expandFormalParam(b.getValueTC(arg, instr.Pos()))...)
	}

	paramBundle := b.emitPointerPack(params)

	goFn := b.mxTranslator.ReverseFunction(targetFn)
	if goFn == nil {
		return llvm.Value{}, b.makeError(instr.Pos(), "cannot reverse-bridge function: "+targetFn.Name())
	}
	funcType, funcPtr := b.getFunction(goFn)

	wrapper := b.createGoroutineStartWrapper(funcType, funcPtr, "spawn", false, false, instr.Pos())

	spawnFnType, spawnFn := b.getFunction(b.program.ImportedPackage("runtime").Members["spawnDomain"].(*ssa.Function))
	b.createCall(spawnFnType, spawnFn, []llvm.Value{wrapper, paramBundle, llvm.Undef(b.dataPtrType)}, "")

	chanType := b.getLLVMRuntimeType("channel")
	return llvm.ConstPointerNull(llvm.PointerType(chanType, 0)), nil
}

func extractTransportStringTC(v mxssa.Value) (string, bool) {
	if mi, ok := v.(*mxssa.MakeInterface); ok {
		v = mi.X
	}
	c, ok := v.(*mxssa.Const)
	if !ok {
		return "", false
	}
	if c.Value() == nil || c.Value().Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(c.Value()), true
}

func traceToMakeChanTC(v mxssa.Value) *mxssa.MakeChan {
	switch v := v.(type) {
	case *mxssa.MakeChan:
		return v
	case *mxssa.MakeInterface:
		return traceToMakeChanTC(v.X)
	case *mxssa.Phi:
		for _, edge := range v.Edges {
			if mc := traceToMakeChanTC(edge); mc != nil {
				return mc
			}
		}
	}
	return nil
}
