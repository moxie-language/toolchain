# Moxie Language Restrictions

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

## Active Restrictions (compile errors)

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
| `uintptr` | Use explicit pointer types. |

### Removed Statements

| Statement | Alternative |
|-----------|------------|
| `fallthrough` | Comma-separated case expressions, or call a shared function. |

### Spawn Boundary Restrictions

| Type | Rule |
|------|------|
| `*T` (pointer) | Cannot cross spawn boundary. |
| `func(...)` | Cannot cross spawn boundary. |
| Any `interface` type | Cannot cross spawn boundary (including `interface{}`). |
| Nested `chan` | Channels must be top-level spawn parameters, not inside structs/arrays/slices/maps. |

Non-constant variables passed to `spawn` cannot be used after the call (move semantics).

## Planned Restrictions

| Feature | Alternative | Status |
|---------|------------|--------|
| `int`, `uint` | Explicit sizing (`int32`, `int64`, etc.) | Awaiting implementation. |
| `string` | `[]byte` as default text type | Awaiting compiler transform. |
| `make` | Typed constructors | Awaiting alternative syntax. |
| `append` | Concatenation operator | Awaiting alternative syntax. |

## Exemptions

Runtime, internal, and standard library packages are exempt from all restrictions. They use the full Go language to implement the primitives that user code accesses through the restricted subset.
