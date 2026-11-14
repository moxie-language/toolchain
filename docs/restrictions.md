# Language restrictions

Status: **implemented and enforced**. Every rule here is a compile error in both
compilers. The language reference is [language.md](language.md); the rules that
are conventions rather than errors are in [style.md](style.md) and
[memory.md](memory.md).

The gate is A1: a property that governs execution must be declared and statically
checkable, so anything whose behaviour would be discovered at run time is
removed rather than policed. Removing `reflect` and the empty interface also
removes the seam where that machinery would be needed (A2's single authority
holds for a value whose type is known). Removed constructs are *undefined*, not
restricted - `int` is not a keyword with a
warning, it is a name that resolves to nothing, so a program may declare its own
`int` type.

---

## 1. Types

| Go | Moxie | note |
|---|---|---|
| `string` | `string = []byte` | one type, one 3-word layout, mutable |
| `[]byte` | same type | the spelling chosen by intent |
| `int` | `int32` | `len()`/`cap()` return `int32` on every target |
| `uint` | `uint32` | |
| `complex64`, `complex128` | removed | |
| `any`, `interface{}` | removed | an interface must name at least one method |
| arrays `[N]T` | removed | use a slice; an array type is leftover Go slice-bypass |
| `uintptr` | kept | the pointer-sized integer for `unsafe.Pointer` arithmetic |

## 2. Removed builtins and statements

| removed | replacement |
|---|---|
| `append(s, v...)` | `push(s, v...)` |
| `make(T, ...)` | literal syntax: `[]T{:n}`, `[]T{:n:c}`, `chan T{}`, `chan T{:N}`, `map[K]V{}` |
| `new(T)` | `&T{}`, or `var x T; p := &x` |
| `complex`, `real`, `imag` | not available |
| `go f()` | a domain is a thread: `spawn` starts one, channels connect them |
| `fallthrough` | comma-separated case expressions, or a shared function |
| `recover` | not available - a panic is death and deferred calls do not run |
| `s + t` on text | `s \| t` |
| `import "strings"` | use `bytes` (string=[]byte) |
| `import "reflect"` | removed with the empty interface |
| `import "sync"` | removed; a domain is one thread |

`push`, `pop` and `resize` are **statements**, not expressions. The read form is
`s[len(s)-1]`; `x := pop(s)` does not parse.

## 3. Slices

- `push(s, ...)` grows: lazy-init to `max(n,4)` on a nil slice, doubling past
  capacity. It does not panic on overflow. There is no capacity guard to write
  and no growth helper to call (`mxutil.Ensure`/`Reserve` are legacy and being
  deleted).
- Presize with `[]T{:n}` and write through a cursor when the size is known.
- `s[a:b]` **copies**. There is no aliasing, so `copy(x[a:b], y)` and
  `x[a:b][i] = v` are compile errors, and a callee that writes through a
  subslice argument writes to a copy.
- Slices with comparable elements compare with `==`/`!=`.
- A `//:linkdata` slice has foreign capacity: push/pop/resize panic rather than
  write past the external symbol.

## 4. Sparsity and closure

- No ambient clock, filesystem, network, environment, entropy and no global
  registry: authority is a parameter (A3, L31-L39). `//:linkdata` and the hosted
  runtime are the declared boundary.
- Package-scoped variables are immutable outside `init()`.
- A closure does not capture. A function value carries a code pointer and
  nothing else, so a nested function must receive what it needs as parameters
  or as a sovereign receiver.

## 5. Channels and spawn

- `chan T{}` is **one slot**, not an unbuffered channel. There is no unbuffered
  channel: one thread could never rendezvous with itself. `chan T{:0}` is a
  compile error naming that reason.
- `chan T{:N}` is N outstanding messages; `chan T{:N:bytes}` states the frame
  budget, which overrides the `cap:"..."` walk.
- A spawn target's single result must be named `closed` and be of type
  `chan struct{}`. That name and type together are what make a function a
  target.
- Spawn arguments are **copied**, frozen at the call; channels are shared
  handles. There is no move semantics.
- The codec route is chosen from the element type: pointer-free elements travel
  as their own bytes, a type with a custom codec uses it, anything else uses the
  structural codec. A raw `int32` or `bool` element is legal - `moxie.Codec`
  wrappers are a convenience, not a requirement (see
  `src/net/http/chanpool.mx`, whose spawn targets take `chan int32`).

## 6. Exemptions

`runtime/*`, `internal/*` and `unsafe` are exempt from the restrictions above:
they implement the primitives. Everything else, user code and converted stdlib
alike, is subject to the full set. `reflect` is **not** an exemption - it is
removed from the language; `src/internal/reflectlite` is internal runtime
support, not a user-facing package.
