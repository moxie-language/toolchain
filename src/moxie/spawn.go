package moxie

// spawn is a true builtin (like make, append) — declared in go/types universe
// scope via patch-gotypes.go. No function stub needed here.
//
// Usage:
//   spawn(fn, args...)                 — local fork+socketpair
//   spawn("local", fn, args...)        — explicit local transport
//
// The compiler enforces at compile time:
//   - fn must be a static function
//   - data arguments must implement Codec
//   - channel element types must implement Codec
//   - no interface{} values cross the boundary
