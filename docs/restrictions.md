# Moxie Language Restrictions

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

## Active Restrictions (compile errors)

### Type Unification

| Go Type | Moxie Equivalent | Notes |
|---------|-----------------|-------|
| `string` | `[]byte` | Identical 3-word layout. String literals produce `[]byte`. |
| `int` | `int32` | 32-bit on all targets. `len()`/`cap()` return `int32`. |
| `uint` | `uint32` | 32-bit on all targets. |

The `string` keyword still compiles — the type checker treats `string` and `[]byte` as mutually assignable. But Moxie convention is `[]byte` for text.

### Removed Builtins

| Builtin | Alternative |
|---------|------------|
| `new(T)` | `&T{}` or `var x T; p := &x` |
| `complex(r, i)` | Not supported. Use separate float fields. |
| `real(z)` | Not supported. |
| `imag(z)` | Not supported. |

### Removed Types

| Type | Alternative |
|------|------------|
| `complex64` | Not supported. |
| `complex128` | Not supported. |
| `uintptr` | Use explicit pointer types. Allowed in packages that import `unsafe`. |

### Removed Statements

| Statement | Alternative |
|-----------|------------|
| `fallthrough` | Comma-separated case expressions, or call a shared function. |

### Text Concatenation

| Go | Moxie |
|----|-------|
| `a + b` (strings) | `a \| b` (pipe operator) |
| `s += "suffix"` | `s = s \| "suffix"` |

The `+` operator on text types is a compile error. The pipe operator `|` is rewritten by the compiler to `__moxie_concat()`.

### Spawn Boundary Restrictions

| Type | Rule |
|------|------|
| `*T` (pointer) | Cannot cross spawn boundary. |
| `func(...)` | Cannot cross spawn boundary. |
| Any `interface` type | Cannot cross spawn boundary (including `interface{}`). |
| Nested `chan` | Channels must be top-level spawn parameters, not inside structs/arrays/slices/maps. |

Non-constant variables passed to `spawn` cannot be used after the call (move semantics).

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
