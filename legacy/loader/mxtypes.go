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
	"go/types"
	"unsafe"
)

// patchIntTypes makes int≡int32 and uint≡uint32 in the type checker,
// and adds Moxie builtins (free, spawn) to the universe scope.
// Applied unconditionally on all targets. Must be called before any type
// checking begins. Idempotent - safe to call multiple times.
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

	// Add Moxie builtins to the universe scope.
	// free(x) frees heap memory. Accepts any type in Moxie.
	anyIface := types.NewInterfaceType(nil, nil)
	anyIface.Complete()
	freeSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewParam(0, nil, "ptr", anyIface)),
		nil, false)
	freeObj := types.NewFunc(0, nil, "free", freeSig)
	types.Universe.Insert(freeObj)
}
