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

## Building Moxie

### Prerequisites

- Go 1.25+
- LLVM 19 (clang-19, lld-19)
- On Arch Linux: `pacman -S llvm19 clang19 lld19`

### Build the compiler

```sh
export CGO_CPPFLAGS="-I/usr/lib/llvm19/include"
export CGO_LDFLAGS="-L/usr/lib/llvm19/lib $(/usr/lib/llvm19/bin/llvm-config --ldflags --libs all) -lclang -lstdc++ -lz -lzstd"
GOWORK=off go build -tags llvm19 -o moxie ./main.go
```

### Build a program

```sh
export MOXIEROOT=/path/to/moxie
export PATH="/usr/lib/llvm19/bin:$PATH"
./moxie build -o hello hello.mx
./hello
```

## Language Restrictions

Moxie is a restricted subset of Go syntax. Source files use the `.mx` extension. The following are compile-time errors in user code (runtime internals are exempt):

| Rejected | Reason |
|----------|--------|
| `new(T)` | Use `var x T` or `&T{...}` |
| `make` | Use literal syntax: `chan T{}`, `map[K]V{}`, `[]T{}` |
| `string` type | Use `[]byte`; string literals auto-wrap to `[]byte` |
| `+` on text | Use `\|` for byte slice concatenation |
| `complex()`, `real()`, `imag()` | Complex numbers not supported |
| `complex64`, `complex128` types | Complex numbers not supported |
| `uintptr` | Use explicit pointer types |
| `int`, `uint` | Use explicitly sized types (`int32`, `int64`, etc.) |
| `fallthrough` | Each case must be self-contained |

Future restrictions (not yet enforced — alternatives pending):

| Will reject | Alternative |
|-------------|------------|
| `append` | `\|` concatenation operator (already works for `[]byte`) |

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

## Scheduler

Moxie uses `scheduler = "tasks"` (cooperative) on all platforms. There are no threads, no preemption, no OS-level synchronization primitives within a domain. Goroutines yield at channel operations and explicit yield points.

This is intentional: each domain is single-threaded, so there are no data races within a domain. The OS provides isolation between domains.

## JS Target

On the JS target, each domain maps to a Service Worker with its own copy of the cooperative runtime. IPC uses `BroadcastChannel` (SW↔SW) or `MessagePort` (Worker↔Worker).

The jsruntime provides:
- `goroutine.mjs` — per-domain cooperative scheduler via `queueMicrotask`
- `channel.mjs` — intra-domain channels with Promise-based blocking
- `domain.mjs` — spawn manager, BroadcastChannel isolation
- `sw.mjs` — Service Worker lifecycle and event management

## Project Structure

```
moxie/
├── main.mx                  # CLI entry point
├── compiler/                # Moxie SSA → LLVM IR (+ spawn detection, restrictions)
├── builder/                 # Build orchestration, library compilation
├── loader/                  # Go package loading with MOXIEROOT overlays
├── compileopts/             # Target configuration (scheduler=tasks, GC=boehm)
├── cgo/                     # CGo processing via libclang
├── goenv/                   # Environment detection
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
│   └── ...                  # Stdlib overlays
└── lib/                     # C libraries (musl, bdwgc, compiler-rt)
```

## Lineage

Moxie is a fork of [TinyGo](https://tinygo.org/). All embedded/MCU targets, flash tools, and threading infrastructure have been stripped. What remains is the compilation pipeline for linux, darwin, and JS, rewired for cooperative single-threaded domains with process-level isolation via `spawn`. Moxie source files use `.mx` extension and `moxie.mod` for modules.
