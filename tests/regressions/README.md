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
// type_assert_on_iface.mx
//   Bug: SSATypeAssert generated on concrete type value, not interface.
//   The emitter extracted {i64,ptr} from an i32 value. clang rejected.
//
// range_over_imported_slice/
//   Bug: "unresolved range type" when iterating over imported []T.
//   The type checker couldn't resolve element types of cross-package
//   slices, producing nil types that crashed the emitter.
