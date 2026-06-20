package loader

// Moxie type unification.
//
// Moxie defines int as always 32 bits on all targets. This file patches
// Go's type checker to agree: int becomes type-identical to int32, and
// uint becomes type-identical to uint32. This eliminates the need for
// int32(len(...)) casts throughout all Moxie code.
//
// The patch works by overwriting the kind field of types.Typ[Int] and
// types.Typ[Uint] via unsafe. The Basic struct layout is:
//   {kind BasicKind, info BasicInfo, name string}
// with kind at offset 0. Type identity in go/types uses kind comparison
// (x.kind == y.kind), so after patching, Identical(int, int32) returns true.
//
// On 64-bit targets, slice headers still use uintptr-sized (i64) len/cap
// internally. The compiler inserts trunc/ext at the int boundary (len/cap
// builtins, make, indexing). Max addressable length is 2^31-1.

import (
	"go/token"
	"go/types"
	"unsafe"
)

// patchIntTypes makes int≡int32 and uint≡uint32 in the type checker.
// Applied unconditionally on all targets. Must be called before any type
// checking begins. Idempotent - safe to call multiple times.
//
// Also registers push/pop/resize as universe-scope functions so go/types
// accepts them in all packages including the runtime.
func patchIntTypes() {
	// Already patched - idempotent guard.
	if types.Typ[types.Int].Kind() == types.Int32 {
		return
	}

	// Patch int.kind from Int(2) to Int32(5).
	kindPtr := (*types.BasicKind)(unsafe.Pointer(types.Typ[types.Int]))
	*kindPtr = types.Int32

	// Patch uint.kind from Uint(7) to Uint32(10).
	kindPtr = (*types.BasicKind)(unsafe.Pointer(types.Typ[types.Uint]))
	*kindPtr = types.Uint32

	// Register push/pop/resize in go/types universe scope.
	// push(s, v...) - variadic, mutates slice length, no return.
	// pop(s) - returns element, mutates slice length.
	// resize(s, n) - mutates slice length, no return.
	// Typed as func(...any) any so go/types accepts any argument types.
	// The real type checking happens in the Moxie type checker.
	anySlice := types.NewSlice(types.Universe.Lookup("any").Type())
	pushSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "args", anySlice)),
		nil, true)
	types.Universe.Insert(types.NewFunc(token.NoPos, nil, "push", pushSig))

	popSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "s", anySlice)),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Universe.Lookup("any").Type())),
		false)
	types.Universe.Insert(types.NewFunc(token.NoPos, nil, "pop", popSig))

	resizeSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(
			types.NewVar(token.NoPos, nil, "s", anySlice),
			types.NewVar(token.NoPos, nil, "n", types.Typ[types.Int32]),
		),
		nil, false)
	types.Universe.Insert(types.NewFunc(token.NoPos, nil, "resize", resizeSig))
}
