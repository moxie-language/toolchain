![moxie sphinx  cat](./docs/mascot.png)

# Moxie

> a programming language without fluff

A compiler for domain-isolated cooperative concurrency. Fork of TinyGo, now its own language with .mx source files, moxie.mod modules, a restricted subset of Go syntax, single-threaded cooperative scheduling per domain, and `spawn` for creating isolated OS-process domains with IPC channels.

## What Moxie Is

Each Moxie program is a tree of **domains**. Each domain is an OS process with its own cooperative scheduler running goroutines on a single thread. Domains communicate through channels that become IPC over Unix socketpairs. No shared memory. No data races. No mutexes.

```moxie
package main

import "moxie"

func worker() {
    println("hello from child domain")
}

func main() {
    moxie.Spawn(worker)
    println("parent continues")
}
```

`Spawn` forks the process. The child gets a fresh scheduler, runs `worker`, exits. The parent continues immediately.

## Architecture

```
┌─────────────────────────────────────┐
│              Program                │
│  ┌─────────┐     ┌─────────┐       │
│  │ Domain 0 │────│ Domain 1 │       │
│  │ (parent) │ IPC│ (child)  │       │
│  │          │sock│          │       │
│  │ goroutine│pair│ goroutine│       │
│  │ goroutine│    │ goroutine│       │
│  │ goroutine│    │          │       │
│  └──────────┘    └──────────┘       │
│  cooperative      cooperative       │
│  scheduler        scheduler         │
└─────────────────────────────────────┘
```

- **Within a domain**: goroutines, channels, cooperative scheduling (no preemption, no threads)
- **Between domains**: `spawn` + IPC channels over socketpairs (deep copy, no shared state)
- **Native targets**: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
- **JS target**: each domain is a Service Worker with its own runtime copy

## Type Unification

Moxie unifies types that Go keeps separate:

| Go | Moxie | Effect |
|----|-------|--------|
| `string` (immutable) / `[]byte` (mutable) | `[]byte` only | Text is `[]byte`. String literals auto-wrap. `\|` for concatenation. |
| `int` (platform-sized) / `int32` | `int32` only | 32-bit on all targets. `len()`/`cap()` return `int32`. |

The `string` and `int` keywords still compile — the type checker treats them as their unified equivalents. Moxie convention uses `[]byte` and `int32` explicitly.

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
| `new(T)` | `&T{}` or `var x T; p := &x` |
| `+` on text | `\|` pipe operator for `[]byte` concatenation |
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

Arguments to `moxie.Spawn` must be safe to serialize across process boundaries:

| Allowed | Treatment |
|---------|-----------|
| Value types (int, float, bool, struct, array) | Deep copy |
| Channels | Become IPC channels |
| Slices, maps | Deep copy |

| Rejected (compile error) | Reason |
|--------------------------|--------|
| Pointers (`*T`) | Can't share memory across processes |
| Functions | Can't serialize code |
| Non-empty interfaces | Can't serialize vtables |

Non-constant variables passed to `spawn` cannot be used after the call — ownership moves to the child domain.

## Scheduler

Moxie uses cooperative scheduling on all platforms. There are no threads, no preemption, no OS-level synchronization primitives within a domain. Goroutines yield at channel operations and explicit yield points.

This is intentional: each domain is single-threaded, so there are no data races within a domain. The OS provides isolation between domains.

## JS Target

On the JS target, each domain maps to a Service Worker with its own copy of the cooperative runtime. IPC uses `BroadcastChannel` (SW-SW) or `MessagePort` (Worker-Worker).

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
│   ├── moxie/               # Shared constants + Spawn declaration
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
