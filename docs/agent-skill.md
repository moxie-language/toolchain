---
name: moxie
description: Use when writing, reviewing or debugging Moxie (.mx) code, or working on the Moxie compiler, runtime or stdlib. Moxie is a compiled systems language: a tree of domains (threads in one process), one arena stack per domain, no heap, no GC, no goroutines, no reflection and no empty interface, explicit-width integers, string=[]byte, `|` for text concatenation, channels and `select` for event dispatch.
---

# Working in Moxie

The normative documents are in `docs/`. This sheet is the condensed form;
[decisions.md](decisions.md) says which designs are real. When this sheet and
the code disagree, the code is the record and this sheet is the bug.

## What Moxie is not

Moxie is not Go. These are **compile errors**, not warnings:

| written | why | instead |
|---|---|---|
| `int`, `uint` | undefined names | `int32`, `int64`, `uint32`, `uint64` |
| `s + t` on text | no `+` on slices | `s \| t` |
| `append` | removed | `push(s, v)` - it grows |
| `make(T, ...)` | removed | `[]T{:n}`, `[]T{:n:c}`, `chan T{}`, `chan T{:N}`, `map[K]V{}` |
| `new(T)` | removed | `&T{}` |
| `any`, `interface{}` | removed | an interface with at least one method |
| `reflect` | removed | nothing needs it: every builtin has `String()`, every boundary has a codec |
| `recover` | removed | panic is death; deferred calls do not run |
| `go f()` | no goroutines | `spawn` starts a domain thread |
| `fallthrough` | removed | `case A, B:` or a shared function |
| `import "strings"` | removed | `bytes` (string is []byte) |
| `import "sync"` | removed | one thread per domain |
| unnamed result | rejected | `func f() (n int32)` |
| value receiver on a struct | rejected | pointer receiver (a struct value is a copy) |
| `var x = expr` at package scope | rejected | `var x T`, assign in `init()` |
| `int8(1000)` | constant overflow | the value must be representable |

## Types and values

- `string` and `[]byte` are one **mutable** type, identical 3-word layout.
- `len()` and `cap()` return `int32`; indexes are `int32`.
- Every builtin has `String() string`; `fmt` takes `...Stringer`.
- `[]T` compares with `==`/`!=` when `T` is comparable.
- A string literal is one object per occurrence, not interned by content.
- **A subslice is a copy.** `s[a:b]` allocates. A write through it does not reach
  the parent, and neither does a write through a subslice passed as an argument.
- **No aliasing.** Never pass a parameter back as a return value.

## Memory, in one paragraph

No heap, no GC, no `free`. Every allocation goes to `fnStack[fnTop]`: a
per-function arena released at return, a manual phase arena released at
`ManualArenaExit`, a sovereign arena owned by a self-mutating receiver, or the
root arena of the domain. Return values are encoded in the callee's frame,
`FnArenaPop` releases it, and the caller decodes into its own arena. Parameters
are owned by the caller; anything in the frame not attached to a named result is
gone at return. `push` grows by itself - for one element, several, or a spread -
so there is no capacity guard to write and no growth helper to call; a helper
that *returns* a pointer-bearing slice is deep-copied by the return protocol and
is quadratic when used in a loop.

## Concurrency

- A domain is a **thread in one process** with its own arena stack, TLS and event
  loop. No scheduler, no preemption, no locks.
- `chan T{}` is **one slot**. There is no unbuffered channel; `:0` is an error.
  `chan T{:N}` is N outstanding messages; `chan T{:N:bytes}` declares the frame
  budget.
- `spawn(fn, args...)` copies values and shares channel handles. A spawn target's
  single result MUST be named `closed` and be of type `chan struct{}`.
- A closure does not capture: a function value is a code pointer and nothing else.
- Managers own the event loop and the channels; workers serve one inbox and
  never spawn on demand. Termination is cooperative - the breaker arm goes at
  every wait.

## Style that is not mechanical

- Presize with `[]T{:n}` and write through a cursor when the count is known.
- One function, one arena, one buffer, one pass.
- Long loops belong in free functions, so their intermediates die at return.
- A mutating method's direct allocations persist in the receiver's arena: push
  heavy scratch into a free helper.
- If a size is not known and cannot be bounded by a declaration, the shape is
  wrong before the code is.

## Before claiming something is done

Run it. A claim backed by recollection is not backed by anything; a claim backed
by a build and a run is. Every compiler change lands in `legacy/` first and
`_mxc_stage4/` second (wood law), unless it changes no emitted code - and say so
explicitly when that is the reason.
