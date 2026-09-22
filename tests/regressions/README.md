// Regression test cases for bugs fixed in the moxie self-hosted bootstrap.
//
// Each file documents a specific crash or incorrect-IR bug and its fix.
// The runner compiles each test with the self-hosted compiler and verifies
// that the binary runs successfully.
//
// Tests:
//
// funcvar_call.mx
//   Bug: extractvalue {ptr,ptr} on ptr function pointer.
//   When a function was stored in a variable and called, the emitter
//   assumed all indirect call targets were {ptr,ptr} closures. The loaded
//   value was a plain ptr.
//
// closure_zero_freevars.mx
//   Bug: zero-binding closure not wrapped in SSAMakeClosure.
//   SSA builder returned bare *SSAFunction for func literals with zero
//   captured variables. The emitter then tried extractvalue on the ptr.
//
// string_compare.mx
//   Bug: string comparison passed i32-typed argument to runtime.stringEqual.
//   The SSAType for one argument resolved to int32 instead of string.
//   clang rejected: expected {ptr,i64,i64} but got i32.
//
// ptr_recv_array_elem.mx
//   Bug: a pointer-receiver method called on an array element reached through
//   a pointer (p.arr[i].M()) passed the element by value. The call signature
//   then disagreed with the method definition - a struct by value where
//   wasm32 expects an i32 pointer - which the wasm validator rejected, and
//   receiver writes went to a copy. The receiver form is now chosen from the
//   type checker's selection: a pointer receiver takes the operand's address.
//
// type_assert_on_iface.mx
//   Bug: SSATypeAssert generated on concrete type value, not interface.
//   The emitter extracted {i64,ptr} from an i32 value. clang rejected.
//
// range_over_imported_slice/
//   Bug: "unresolved range type" when iterating over imported []T.
//   The type checker couldn't resolve element types of cross-package
//   slices, producing nil types that crashed the emitter.
//
// map_value_return_reloc/
//   Bug: map values that own memory were raw-copied across a return from a
//   non-main package. codecEncodeMap wrote the value slot verbatim unless the
//   value type was bytes/pointer/interface, so a map[string][]string value
//   kept its data pointer into the callee's fn arena; the arena was released
//   at return, and the next callee's codecSeenInit overwrote the caller's live
//   value on its second call. Slice, struct, map and array values are now
//   delegated to codecEncodeValue/codecDecodeValue (codecMapValDeep in
//   src/runtime/codec.mx).
//
// switch_default_first.mx
//   Bug: a default clause written before the cases was emitted where it stood,
//   as an unconditional jump, so every case after it was dead. crypto/aes
//   NewCipher validates its key with 'switch k { default: ...; case 16, 24,
//   32: break }' and rejected a valid 32-byte AES-256 key, trapping the smesh
//   signer worker on addIdentity. buildSwitch now chains the case tests first
//   and enters the default body only when none matched, as the legacy
//   compiler does.
