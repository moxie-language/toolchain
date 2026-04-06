![moxie sphinx  cat](./docs/mascot.png)

# Moxie

> a programming language without fluff

A compiler for domain-isolated event-driven programs. Fork of TinyGo, now its own language with .mx source files, moxie.mod modules, a restricted subset of Go syntax, single-threaded execution per domain, and `spawn` for creating isolated OS-process domains with IPC channels.

## What Moxie Is

Each Moxie program is a tree of **domains**. Each domain is an OS process running a single thread. There are no goroutines. Execution flow is controlled by three constructs:

1. **Unbuffered channels** are dispatchers — sending on an unbuffered channel transfers execution to the `select` statement listening for that channel, like a goto without the baggage.
2. **Buffered channels** are message queues — sending enqueues a value for the next time the receiving `select` runs.
3. **`select`** is the event handler — it waits for channel messages, I/O events, and timer expirations. All concurrency reduces to sitting in a `select` loop.

Domains communicate through channels that become IPC over Unix socketpairs via `spawn`. Messages pass between processes transparently — the receiving domain picks them up when its event loop reaches the relevant `select`.

```moxie
package main

func worker() {
    println("hello from child domain")
}

func main() {
    spawn(worker)
    println("parent continues")
}
```

`spawn` is a language builtin — no import needed. It forks the process. The child runs `worker` to completion, exits. The parent continues immediately.

## Execution Model

```
┌─────────────────────────────────────┐
│              Program                │
│  ┌──────────┐     ┌──────────┐     │
│  │ Domain 0  │────│ Domain 1  │     │
│  │ (parent)  │ IPC│ (child)   │     │
│  │           │sock│           │     │
│  │  select   │pair│  select   │     │
│  │  ├ ch1    │    │  ├ ch3    │     │
│  │  ├ ch2    │    │  └ timer  │     │
│  │  └ I/O    │    │           │     │
│  └───────────┘    └───────────┘     │
│  single thread    single thread     │
└─────────────────────────────────────┘
```

- **Within a domain**: channels + `select` as event dispatch. No goroutines, no scheduler, no task switching.
- **Between domains**: `spawn` + IPC channels over socketpairs (deep copy, no shared state).
- **The `go` keyword is a compile error.** There is no way to create concurrent execution within a domain.
- **Native targets**: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- **JS target**: each domain is a Service Worker

### Why No Goroutines

The `go` keyword hides execution paths. When you write `go f()`, you create an invisible thread of control that runs at unpredictable times relative to the rest of your code. Reasoning about program behavior requires tracking every goroutine's state simultaneously.

Moxie reduces the execution model to three incontrovertible rules:

| Construct | Behavior |
|-----------|----------|
| Unbuffered channel send | Execution jumps to the waiting `select` case |
| Buffered channel send | Message queued, processed on next `select` iteration |
| `select` | Event handler — blocks until a channel, I/O event, or timer fires |

With these three rules, you can trace any Moxie program's execution by following the channel sends to their corresponding `select` cases. The cost of reasoning about control flow drops to near zero.

## Type Unification

Moxie unifies types that Go keeps separate:

| Go | Moxie | Effect |
|----|-------|--------|
| `string` (immutable) / `[]byte` (mutable) | `string = []byte` | Same runtime layout, mutually assignable. No immutable strings. `string` is preferred for readability. `\|` for concatenation (works on all slice types). |
| `int` (platform-sized) / `int32` | `int32` only | 32-bit on all targets. `len()`/`cap()` return `int32`. |

The `string` keyword is retained and preferred — it reads better than `[]byte` in function signatures and type declarations. They are the same type: same layout, same operations, same runtime representation. Use whichever reads better in context.

### Slice Equality

Any slice of a comparable element type supports `==` and `!=`. Two slices are equal if they have the same length and all corresponding elements are equal.

```moxie
a := []int32{1, 2, 3}
b := []int32{1, 2, 3}
println(a == b) // true

m := map[[]byte]int32{}  // slices can be map keys
m[[]byte("key")] = 42
```

Go restricts `==` to strings and refuses it for `[]byte` — an artificial distinction. Moxie treats all slices the same: if the element type is comparable, the slice is comparable.

## Building Moxie

### Prerequisites

- Go 1.25+
- LLVM 19 (clang-19, lld-19)
- On Arch Linux: `pacman -S llvm19 clang19 lld19`

### Build the compiler

```sh
./build.sh
```

This creates a patched GOROOT (for string=[]byte type unification), then builds the compiler with LLVM 19 linking. The result is a `./moxie` binary.

### Build a program

```sh
export MOXIEROOT=/path/to/moxie
export PATH="/usr/lib/llvm19/bin:$MOXIEROOT:$PATH"
moxie build -o hello hello.mx
./hello
```

## Language Restrictions

Moxie is a restricted subset of Go syntax. Source files use the `.mx` extension. The following are compile-time errors in user code (runtime internals are exempt):

| Rejected | Alternative |
|----------|------------|
| `go f()` | Channels + `select` for event dispatch |
| `new(T)` | `&T{}` or `var x T; p := &x` |
| `+` on text | `\|` pipe operator (concatenates any matching slice types) |
| `complex()`, `real()`, `imag()` | Not supported |
| `complex64`, `complex128` | Not supported |
| `uintptr` | Explicit pointer types (allowed with `import "unsafe"`) |
| `fallthrough` | Comma-separated cases: `case A, B:` |

Moxie also provides alternative literal syntax (optional — `make()` still works):

| Moxie syntax | Equivalent |
|--------------|-----------|
| `chan T{}` | `make(chan T)` |
| `chan T{n}` | `make(chan T, n)` |
| `[]T{:n}` | `make([]T, n)` |
| `[]T{:n:c}` | `make([]T, n, c)` |

See [REFERENCE.md](REFERENCE.md) for the full language specification and [docs/PORTING.md](docs/PORTING.md) for a step-by-step migration guide.

## Spawn Boundary Rules

`spawn` is a true language builtin (like `make`, `append`) — patched into Go's type universe, no import or function stub. The type checker validates args directly; the SSA validator enforces `moxie.Codec` on data types and channel element types.

All data arguments and channel element types must implement `moxie.Codec`:

```go
type Codec interface {
    EncodeTo(w io.Writer) error
    DecodeFrom(r io.Reader) error
}
```

Built-in codec types (in the `moxie` package): `Bool`, `Int8`, `Uint8`, `Int16`, `Uint16`, `Int32`, `Uint32`, `Int64`, `Uint64`, `Float32`, `Float64`, `Bytes`. All encode little-endian by default (matching target hardware). Big-endian aliases for protocols like Bitcoin: `BigInt16`, `BigUint16`, `BigInt32`, `BigUint32`, `BigInt64`, `BigUint64`, `BigFloat32`, `BigFloat64`.

| Allowed | Treatment |
|---------|-----------|
| Types implementing `moxie.Codec` | Serialized via EncodeTo/DecodeFrom |
| Channels (element type must implement Codec) | Become IPC channels |
| Structs with Codec methods | User-defined serialization |

| Rejected (compile error) | Reason |
|--------------------------|--------|
| Raw built-in types (`int32`, `bool`, etc.) | Use `moxie.Int32`, `moxie.Bool`, etc. |
| Pointers (`*T`) | Can't share memory across processes |
| Functions | Can't serialize code |
| All interface types | No `interface{}` crosses the spawn boundary |

Non-constant variables passed to `spawn` cannot be used after the call — ownership moves to the child domain.

## Runtime

Each domain runs on a single thread with no scheduler. I/O is handled via epoll (Linux) or kqueue (macOS), blocking the thread until events arrive. Timers fire as interrupts during I/O waits and sleep. Channels are synchronous within a domain — unbuffered sends dispatch directly to the waiting receiver.

There are no goroutines, no task switching, no run queue. The only concurrency primitive is `select`, which multiplexes channel messages, I/O readiness, and timer expirations into a single event loop.

## JS Target

On the JS target, each domain maps to a Service Worker. IPC uses `BroadcastChannel` (SW-SW) or `MessagePort` (Worker-Worker).

## Project Structure

```
moxie/
├── main.go                  # CLI entry point
├── build.sh                 # Compiler build (patched GOROOT + LLVM linking)
├── build/                   # Build-time tools (go/types patcher)
├── compiler/                # Moxie SSA → LLVM IR (+ spawn detection, restrictions)
├── builder/                 # Build orchestration, library compilation
├── loader/                  # Package loading, .mx rewriting, pipe/concat transforms
├── compileopts/             # Target configuration (scheduler=tasks, GC=boehm)
├── cgo/                     # CGo processing via libclang
├── goenv/                   # Environment and version detection
├── transform/               # LLVM IR optimization passes
├── interp/                  # Compile-time partial evaluation
├── stacksize/               # Goroutine stack analysis
├── diagnostics/             # Error reporting
├── jsbackend/               # Go SSA → JavaScript compiler
├── jsruntime/               # JS runtime (goroutines, channels, domains, SW)
├── src/                     # Target-side sources (compiled INTO programs)
│   ├── runtime/             # Runtime: scheduler, channels, GC, spawn
│   ├── internal/task/       # Cooperative task system, context switching
│   ├── moxie/               # Codec interface, built-in codec types, constants
│   └── ...                  # Stdlib overlays (.mx files)
├── lib/                     # C libraries (musl, bdwgc, compiler-rt)
└── docs/                    # Language reference, porting guide, architecture
```

## Documentation

- [REFERENCE.md](REFERENCE.md) — Complete language specification
- [docs/PORTING.md](docs/PORTING.md) — Step-by-step Go → Moxie migration guide
- [docs/restrictions.md](docs/restrictions.md) — Active restrictions and exemptions
- [docs/architecture.md](docs/architecture.md) — Compiler pipeline and runtime internals
- [docs/building.md](docs/building.md) — Build instructions and flags
- [docs/spawn.md](docs/spawn.md) — Spawn quick reference

## Lineage

Moxie is a fork of [TinyGo](https://tinygo.org/). All embedded/MCU targets, flash tools, and threading infrastructure have been stripped. What remains is the compilation pipeline for linux, darwin, and JS, rewired for cooperative single-threaded domains with process-level isolation via `spawn`.
