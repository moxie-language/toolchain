package loader

// Moxie type unification.
//
// On WASM (Moxie's target), int is already 32 bits at runtime. This file
// patches Go's type checker to agree: int becomes type-identical to int32,
// and uint becomes type-identical to uint32. This eliminates the need for
// int32(len(...)) casts throughout all Moxie code.
//
// The patch works by overwriting the kind field of types.Typ[Int] and
// types.Typ[Uint] via unsafe. The Basic struct layout is:
//   {kind BasicKind, info BasicInfo, name string}
// with kind at offset 0. Type identity in go/types uses kind comparison
// (x.kind == y.kind), so after patching, Identical(int, int32) returns true.
//
// Only applied when the target has 32-bit ints (e.g. WASM32). On 64-bit
// targets, int and int32 remain distinct because they have different sizes
// and different LLVM IR representations (i32 vs i64).

import (
	"go/types"
	"unsafe"
)

// patchIntTypes makes int≡int32 and uint≡uint32 in the type checker.
// Only applies when the target's int size is 4 bytes (32-bit targets).
// Must be called before any type checking begins.
func patchIntTypes(sizes types.Sizes) {
	if sizes == nil {
		return
	}
	// Check target int size BEFORE patching. After the patch, int.Kind()
	// becomes Int32, which would always report 4 bytes via basicSizes
	// regardless of the actual target.
	if sizes.Sizeof(types.Typ[types.Int]) != 4 {
		return
	}

	// Patch int.kind from Int(2) to Int32(5).
	kindPtr := (*types.BasicKind)(unsafe.Pointer(types.Typ[types.Int]))
	*kindPtr = types.Int32

	// Patch uint.kind from Uint(7) to Uint32(10).
	kindPtr = (*types.BasicKind)(unsafe.Pointer(types.Typ[types.Uint]))
	*kindPtr = types.Uint32
}
