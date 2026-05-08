![moxie sphinx  cat](./docs/mascot.png)

# Moxie

> a programming language without fluff

A compiled systems language for domain-isolated event-driven programs. Each program is a tree of **domains** — isolated OS processes communicating through serialized IPC channels. No goroutines, no shared memory, no data races.

Moxie descends from [TinyGo](https://tinygo.org/). All embedded targets and threading infrastructure have been stripped. What remains is a compilation pipeline for Linux, Darwin, and browser JS, rewired for single-threaded domains with process-level isolation via `spawn`.

## Quick Start

```sh
# Build the compiler (requires Go 1.25+ and LLVM 19)
./build.sh

# Build a program
export MOXIEROOT=/path/to/moxie
moxie build -o hello .
./hello
```

```moxie
package main

import "moxie"

func worker(n moxie.Int32) {
    println("child domain:", n)
}

func main() {
    spawn(worker, moxie.Int32(42))
    println("parent continues")
}
```

## The Model

Each domain runs a single thread. No goroutines, no scheduler, no task switching.

| Construct | Behavior |
|-----------|----------|
| Unbuffered channel send | Execution jumps to the waiting `select` case |
| Buffered channel send | Message queued for the next `select` iteration |
| `select` | Event handler — blocks until a channel, I/O event, or timer fires |
| `spawn` | Creates a child domain (OS process) with IPC channels |

Within a domain, channels and `select` are the event dispatch system. Between domains, `spawn` and IPC channels provide concurrent execution with complete memory isolation.

## Type Unification

| Traditional | Moxie | Effect |
|-------------|-------|--------|
| `string` (immutable) / `[]byte` (mutable) | `string = []byte` | Same type, mutually assignable. `\|` for concatenation. |
| `int` (platform-sized) | `int32` always | 32-bit on all targets. |

## Spawn Boundary

`spawn` is a language builtin. All data arguments must implement `moxie.Codec` for serialization. No pointers, functions, or interfaces cross the boundary. Non-constant values are moved (ownership transfer).

```moxie
import "moxie"

type Codec interface {
    EncodeTo(w io.Writer) error
    DecodeFrom(r io.Reader) error
}
```

Built-in codec types: `Bool`, `Int8`, `Uint8`, `Int16`, `Uint16`, `Int32`, `Uint32`, `Int64`, `Uint64`, `Float32`, `Float64`, `Bytes`. Little-endian default. Big-endian aliases (`BigInt32`, `BigUint64`, etc.) for network protocols.

## Removed Features

| Removed | Use Instead |
|---------|------------|
| `go f()` | Channels + `select`, or `spawn` |
| `new(T)` | `&T{}` |
| `+` on text | `\|` pipe operator |
| `fallthrough` | `case A, B:` |
| `complex64/128` | Not supported |
| `uintptr` | Explicit pointer types |
| `import "strings"` | `import "bytes"` |

## Targets

| Target | Output | GC |
|--------|--------|-----|
| linux/amd64 | Static ELF | Boehm |
| linux/arm64 | Static ELF | Boehm |
| darwin/amd64 | Mach-O | Boehm |
| darwin/arm64 | Mach-O | Boehm |
| js/wasm | JavaScript + runtime | Browser GC |

All native binaries are fully statically linked.

## AI-Assisted Development

[EXAMPLE_CLAUDE.md](EXAMPLE_CLAUDE.md) is a CLAUDE.md template that teaches Claude (or other AI coding assistants) how to write correct Moxie code. Drop it into your project's CLAUDE.md to get accurate code generation that respects Moxie's restrictions and patterns.

## Documentation

- **[REFERENCE.md](REFERENCE.md)** — Complete language specification
- **[TUTORIAL.md](TUTORIAL.md)** — Learn Moxie from scratch
- **[EXAMPLE_CLAUDE.md](EXAMPLE_CLAUDE.md)** — AI coding assistant configuration
- [docs/spawn.md](docs/spawn.md) — Spawn quick reference
- [docs/architecture.md](docs/architecture.md) — Compiler pipeline and runtime internals
- [docs/restrictions.md](docs/restrictions.md) — Active restrictions and exemptions
- [docs/PORTING.md](docs/PORTING.md) — Migrating existing code to Moxie
- [docs/building.md](docs/building.md) — Build instructions and flags

## Project Structure

```
moxie/
├── main.go                  # CLI entry point
├── build.sh                 # Compiler build (patched GOROOT + LLVM linking)
├── build/                   # Build-time tools (go/types patcher)
├── compiler/                # Moxie SSA → LLVM IR (+ spawn detection, restrictions)
├── builder/                 # Build orchestration, library compilation
├── loader/                  # Package loading, .mx rewriting, pipe/concat transforms
├── compileopts/             # Target configuration
├── cgo/                     # CGo processing via libclang
├── goenv/                   # Environment and version detection
├── transform/               # LLVM IR optimization passes
├── interp/                  # Compile-time partial evaluation
├── diagnostics/             # Error reporting
├── jsruntime/               # JS host shims for WASM target (channels, domains, browser bridges)
├── src/                     # Target-side sources (compiled INTO programs)
│   ├── runtime/             # Runtime: scheduler, channels, GC, spawn
│   ├── internal/task/       # Cooperative task system, context switching
│   ├── moxie/               # Codec interface, built-in codec types
│   └── ...                  # Stdlib overlays (.mx files)
├── lib/                     # C libraries (musl, bdwgc, compiler-rt)
└── docs/                    # Language reference, porting guide, architecture
```
