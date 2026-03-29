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

// createSpawn handles moxie.Spawn(fn, args...) calls. The compiler intercepts
// these and emits a call to runtime.spawnDomain with the function pointer and
// serialized arguments.
//
// Because Spawn takes interface{} params, the SSA wraps arguments in
// MakeInterface. We look through this wrapping to find the actual function.
func (b *builder) createSpawn(instr *ssa.CallCommon) (llvm.Value, error) {
	if len(instr.Args) < 1 {
		b.addError(instr.Pos(), "moxie.Spawn requires at least a function argument")
		return llvm.Value{}, nil
	}

	// Unwrap the first argument to find the actual function.
	// In SSA: moxie.Spawn(worker) becomes moxie.Spawn(MakeInterface(worker))
	fnArg := instr.Args[0]
	var targetFn *ssa.Function

	// Look through MakeInterface to find the function value.
	if mi, ok := fnArg.(*ssa.MakeInterface); ok {
		fnArg = mi.X
	}
	switch v := fnArg.(type) {
	case *ssa.Function:
		targetFn = v
	case *ssa.MakeClosure:
		if fn, ok := v.Fn.(*ssa.Function); ok {
			targetFn = fn
		}
	}

	if targetFn == nil {
		b.addError(instr.Pos(), "moxie.Spawn: first argument must be a static function (dynamic function values not yet supported)")
		return llvm.Value{}, nil
	}

	// Validate that the function signature's parameters are spawn-safe.
	sig := targetFn.Signature
	for i := 0; i < sig.Params().Len(); i++ {
		if err := validateSpawnArg(sig.Params().At(i).Type(), i); err != nil {
			b.addError(instr.Pos(), err.Error())
			return llvm.Value{}, nil
		}
	}

	// Extract concrete argument values from the variadic []interface{} slice.
	// SSA packs each arg into MakeInterface, stores them in a local array,
	// then slices it. We walk the SSA graph to recover the actual typed values.
	concreteArgs := extractSpawnSSAArgs(instr)
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

	// Return a lifecycle channel (chan struct{}).
	// TODO: wire this to the socketpair so close propagates.
	// For now, return a nil channel.
	chanType := b.getLLVMRuntimeType("channel")
	return llvm.ConstPointerNull(llvm.PointerType(chanType, 0)), nil
}

// extractSpawnArgs recovers the concrete typed values from the variadic
// []interface{} argument of a spawn call.
//
// The SSA builder transforms spawn(worker, int32(42)) into:
//
//	t0 = local [1]interface{} (varargs)
//	t1 = &t0[0:int]
//	*t1 = MakeInterface(int32(42))
//	t2 = slice t0[:]
//	spawn(MakeInterface(worker), t2)
//
// We walk: Slice → Alloc → referrers(IndexAddr) → referrers(Store) → unwrap MakeInterface
func extractSpawnSSAArgs(instr *ssa.CallCommon) []ssa.Value {
	if len(instr.Args) < 2 {
		return nil
	}

	variadicArg := instr.Args[1]

	slice, ok := variadicArg.(*ssa.Slice)
	if !ok {
		return nil
	}
	alloc, ok := slice.X.(*ssa.Alloc)
	if !ok {
		return nil
	}

	refs := alloc.Referrers()
	if refs == nil {
		return nil
	}

	type indexedValue struct {
		index int64
		value ssa.Value
	}
	var indexed []indexedValue

	for _, ref := range *refs {
		ia, ok := ref.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		iaRefs := ia.Referrers()
		if iaRefs == nil {
			continue
		}
		for _, iaRef := range *iaRefs {
			store, ok := iaRef.(*ssa.Store)
			if !ok || store.Addr != ia {
				continue
			}
			idx := int64(0)
			if constIdx, ok := ia.Index.(*ssa.Const); ok {
				if v, ok := constant.Int64Val(constIdx.Value); ok {
					idx = v
				}
			}
			val := store.Val
			if mi, ok := val.(*ssa.MakeInterface); ok {
				val = mi.X
			}
			indexed = append(indexed, indexedValue{idx, val})
		}
	}

	// Sort by index (insertion sort — N is always small).
	for i := 1; i < len(indexed); i++ {
		for j := i; j > 0 && indexed[j].index < indexed[j-1].index; j-- {
			indexed[j], indexed[j-1] = indexed[j-1], indexed[j]
		}
	}

	result := make([]ssa.Value, len(indexed))
	for i, iv := range indexed {
		result[i] = iv.value
	}
	return result
}

// validateSpawnArg checks that a type is safe to pass across a spawn boundary.
// Channels are allowed as bare top-level parameters (they become IPC channels).
// No interface types anywhere — they erase concrete types and make serialization
// ambiguous. Slices/maps are allowed if their element types are spawn-safe.
func validateSpawnArg(t types.Type, argIndex int) error {
	if ch, ok := t.Underlying().(*types.Chan); ok {
		// Top-level channel: validate element type has no pointers/channels.
		return validateSpawnElem(ch.Elem(), argIndex)
	}
	return validateSpawnElem(t, argIndex)
}

// validateSpawnElem rejects types that can't be serialized across a spawn
// boundary. Channels are rejected here — they're only allowed at the top level
// (checked by validateSpawnArg).
func validateSpawnElem(t types.Type, argIndex int) error {
	switch ut := t.Underlying().(type) {
	case *types.Pointer:
		return fmt.Errorf("spawn: argument %d is a pointer type %s (not allowed across spawn boundary)", argIndex+1, ut)
	case *types.Chan:
		return fmt.Errorf("spawn: argument %d contains a nested channel (channels must be top-level parameters)", argIndex+1)
	case *types.Slice:
		return validateSpawnElem(ut.Elem(), argIndex)
	case *types.Map:
		if err := validateSpawnElem(ut.Key(), argIndex); err != nil {
			return err
		}
		return validateSpawnElem(ut.Elem(), argIndex)
	case *types.Signature:
		return fmt.Errorf("spawn: argument %d is a function type (not allowed across spawn boundary)", argIndex+1)
	case *types.Interface:
		return fmt.Errorf("spawn: argument %d is an interface type (not allowed across spawn boundary)", argIndex+1)
	case *types.Struct:
		for i := 0; i < ut.NumFields(); i++ {
			if err := validateSpawnElem(ut.Field(i).Type(), argIndex); err != nil {
				return err
			}
		}
	case *types.Array:
		return validateSpawnElem(ut.Elem(), argIndex)
	}
	return nil
}
