# Moxie Language Restrictions

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

## Active Restrictions (compile errors)

### Type Unification

| Go Type | Moxie Equivalent | Notes |
|---------|-----------------|-------|
| `string` | `string = []byte` | Same type. Identical 3-word layout, mutually assignable. `string` is preferred for readability. |
| `int` | `int32` | 32-bit on all targets. `len()`/`cap()` return `int32`. |
| `uint` | `uint32` | 32-bit on all targets. |

`string` and `[]byte` are the same type in Moxie — same runtime representation, same operations, fully interchangeable. Use `string` in signatures and declarations for readability; use `[]byte` when the byte-level nature matters (e.g., binary protocols).

### Removed Builtins

| Builtin | Alternative |
|---------|------------|
| `append(s, v...)` | `push(s, v...)` - explicit allocation semantics, geometric growth on overflow |
| `make(T, ...)` | Literal syntax: `[]T{:n}`, `chan T{}`, `map[K]V{}` |
| `new(T)` | `&T{}` or `var x T; p := &x` |
| `complex(r, i)` | Not supported. Use separate float fields. |
| `real(z)` | Not supported. |
| `imag(z)` | Not supported. |

### Added Builtins (Moxie-specific)

| Builtin | Signature | Semantics |
|---------|-----------|-----------|
| `push` | `push(s []T, v1, v2, ...T)` | Appends elements to slice. Panics if len+n > cap. Lazy-inits nil slices (cap 32 min). Store-back to `s` is implicit. |
| `pop` | `pop(s []T) T` | Removes and returns last element. Panics if len == 0. Store-back implicit. |
| `resize` | `resize(s []T, n int32)` | Sets len to n. Panics if n > cap or n < 0 (unsigned check). Store-back implicit. |
| `spawn` | `spawn(fn, args...) chan struct{}` | Creates new domain. See spawn boundary rules. |

#### Why append was removed

`append` conflates allocation and insertion into one call with unpredictable copy
semantics. It sometimes returns the same backing array, sometimes a new one - the
caller cannot know which without comparing pointers. Code that holds an alias into
a slice prefix and then appends to the original may or may not corrupt the alias,
depending on whether capacity was sufficient. This makes reasoning about ownership
impossible in an arena system where dead backing arrays are never freed individually.

`push` separates concerns: declare capacity explicitly with `[]T{:0:cap}`. Push
into existing capacity or panic. No hidden reallocation, no silent copying. If
you hit the cap, the allocation site is wrong - fix it there, not at the push.

### Removed Types

| Type | Alternative |
|------|------------|
| `complex64` | Not supported. |
| `complex128` | Not supported. |
| `uintptr` | Use explicit pointer types. Allowed in packages that import `unsafe`. |

### Removed Statements

| Statement | Alternative |
|-----------|------------|
| `go f()` | **Compile error.** There are no goroutines. Use channels + `select` for event dispatch, `spawn` for domain-level parallelism. |
| `fallthrough` | Comma-separated case expressions, or call a shared function. |

### Slice Concatenation (`|` operator)

| Go | Moxie |
|----|-------|
| `a + b` (strings) | `a \| b` (pipe operator) |
| `s += "suffix"` | `s \|= "suffix"` |
| two slices concatenated | `a \| b` (always allocates fresh backing) |

The `+` operator on text types is a compile error. Use `|` instead.

The `|` operator works on **all matching slice types**, not just `[]byte`:

```go
a := []int32{1, 2}
b := []int32{3, 4}
c := a | b   // []int32{1, 2, 3, 4} — always a fresh allocation
```

For `[]byte`, the loader rewrites `|` to an internal concat call before type checking. For all other slice types, the type checker and compiler handle `|` natively.

### Literal Syntax Extensions

| Syntax | Equivalent | Notes |
|--------|-----------|-------|
| `[]T{:n}` | `make([]T, n)` | Slice with length `n` |
| `[]T{:n:c}` | `make([]T, n, c)` | Slice with length `n`, capacity `c` |
| `chan T{}` | `make(chan T)` | Unbuffered channel |
| `chan T{n}` | `make(chan T, n)` | Buffered channel with capacity `n` |

The leading `:` in slice literals distinguishes size allocation from composite literals. `make()` is a compile error in user code — use literal syntax only.

### Spawn Boundary Restrictions

All data arguments and channel element types must implement `moxie.Codec` (see `src/moxie/codec.go`). The `moxie` package provides built-in codec types: `Bool`, `Int8`, `Uint8`, `Int16`, `Uint16`, `Int32`, `Uint32`, `Int64`, `Uint64`, `Float32`, `Float64`, `Bytes`. Big-endian aliases: `BigInt16`, `BigUint16`, `BigInt32`, `BigUint32`, `BigInt64`, `BigUint64`, `BigFloat32`, `BigFloat64`.

| Allowed | Treatment |
|---------|-----------|
| Types implementing `moxie.Codec` | Serialized via EncodeTo/DecodeFrom |
| Channels (element type must implement Codec) | Become IPC channels |
| Structs with Codec methods | User-defined serialization |

| Rejected (compile error) | Reason |
|--------------------------|--------|
| Raw built-in types (`int32`, `bool`, etc.) | Use `moxie.Int32`, `moxie.Bool`, etc. |
| `*T` (pointer) | Cannot share memory across processes |
| `func(...)` | Cannot serialize code |
| Any `interface` type (including `interface{}`) | No interface crosses the spawn boundary |

Non-constant variables passed to `spawn` cannot be used after the call (move semantics).

### Slice Equality (Extension)

Moxie extends Go's comparison rules: any `[]T` where `T` is comparable supports `==` and `!=`. Slices can be used as map keys. Go restricts this to `string` only — Moxie treats all comparable-element slices uniformly.

### Import Restrictions

| Import | Error |
|--------|-------|
| `"strings"` | `use "bytes" instead of "strings" (string=[]byte)` |

## Exemptions

Permanently exempt from restrictions:

| Package | Reason |
|---------|--------|
| `runtime/*` | Language primitives |
| `internal/*` | Internal implementation |
| `unsafe` | Language primitive |
| `reflect` | Must handle all Go types |
| `os/*` | File descriptors, syscall wrappers |
| `syscall/*` | Syscall interfaces |

All other packages (user code and converted stdlib) are subject to full restrictions.
