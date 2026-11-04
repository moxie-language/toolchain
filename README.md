![moxie sphinx  cat](./docs/mascot.png)

# Moxie

*a programming language without fluff*

> "*Perfection is achieved, not when there is nothing more to add, but when there is nothing left to take away.*"
> 
> - *Antoine de Saint-Exupery*

*Saint-Exupery was an aircraft engineer. Software is like aircraft - what fails in the air is usually needless complexity.*

-----

A compiled systems language for domain-isolated event-driven programs. Each program is a tree of **domains** - isolated OS processes communicating through serialized IPC channels. No goroutines, no shared memory, no data races.

Moxie descends from [TinyGo](https://tinygo.org/). All embedded targets and threading infrastructure have been stripped. What remains is a compilation pipeline for Linux, Darwin, and browser JS, rewired for single-threaded domains with process-level isolation via `spawn`.

## Axioms

Everything in Moxie derives from three axioms. They are stated, justified, and expanded into thirty lemmas in **[docs/axioms.md](docs/axioms.md)**, which is the normative document: where any other document appears to conflict with it, the axioms win.

**A1 - just the facts.** Every property that governs execution is declared and statically checkable. Nothing that determines behaviour is inferred, negotiated, or discovered at runtime.

**A2 - total isolation.** No component can address another component's state, and every fact has exactly one authoritative owner. State has one owner; interfaces have two.

**A3 - closure.** Every capability and every input is declared, and the declaration is exhaustive: anything not declared is unreachable. The default is deny. No ambient filesystem, network, clock, environment, signal, or global registry; every perturbation is a declared message or a declared value.

The three are load-bearing together: A1 says what must be written down, A2 says what cannot be reached in memory, and A3 says what cannot be reached even though it is not another component's memory. Each closes what the previous one leaves open.

The operational test: a proposed feature must name the axiom that forces it. If no axiom forces it, it is an opinion, and it is deleted.

Every rule below is a consequence of those three. Where a section states a rule, it is a lemma from docs/axioms.md.

## Address

`moxielang.dev` is the toolchain's canonical address and import root. The
toolchain's own packages import as `moxielang.dev/...`, and the domain answers
three audiences at one address:

- **Go tooling** (`go get`, `go list`) asks with `?go-get=1` and receives the
  `go-import` discovery document, which names this repository.
- **A browser** gets a real 301 to the repository page.
- **git** asks for `/info/refs` and is redirected to the repository's git
  endpoint, so `git clone https://moxielang.dev` works.
- **`moxie get`** reads the same document: `moxie get moxielang.dev/pkg/mxutil`
  resolves the module root and the repository URL from it, with no knowledge of
  the repository host.

The site block, the page and a Cloudflare Worker variant live in
[`site/vanity/`](site/vanity/), beside the verification commands.

## Quick Start

### Prerequisites

**LLVM 22** is pinned across the stack: clang, ld.lld, llvm-link, opt and
llvm-extract must all be major version 22, and the legacy bootstrap compiler
links libLLVM-22. **Go 1.25.3 exactly** is needed only to build that bootstrap
compiler - it patches `go/types` by exact source text, so a newer Go is a
different source tree and the patch refuses to apply.

On a bare Ubuntu 24.04 host, `scripts/provision-build-host.sh` does the whole
job - pinned Go, LLVM 22 under the unversioned names the driver asks for,
compiler-rt builtins, the llvm-glue library, and the bootstrap through
`s2 == s3`:

```sh
sudo bash scripts/provision-build-host.sh --check      # report, change nothing
sudo bash scripts/provision-build-host.sh --bootstrap  # provision + fixpoint
```

Elsewhere, most distributions ship LLVM 22 as the unversioned toolchain:

```sh
# Arch
sudo pacman -S llvm clang lld

# Fedora / RHEL
sudo dnf install llvm clang lld

# Debian / Ubuntu (use apt.llvm.org if your release predates LLVM 22)
sudo apt install llvm-22 clang-22 lld-22
```

`build.sh` verifies the major version of every tool before building, so a
mis-provisioned machine fails immediately with a list of what is wrong.

See [docs/building.md](docs/building.md) for the full dependency list,
per-distribution instructions, and the bootstrap protocol.

```sh
# Build the compiler
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

The tree of domains is the consequence of A2 (total isolation) and A1 (just the facts): `spawn` is the only construct that creates a peer, so the topology is a declared and statically checkable artifact (lemma L20).

| Construct | Behavior |
|-----------|----------|
| Unbuffered channel send | Execution jumps to the waiting `select` case |
| Buffered channel send | Message queued for the next `select` iteration |
| `select` | Event handler - blocks until a channel, I/O event, or timer fires |
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

## Spawn Channel Model: Shared Rings, Non-blocking Duplex

A spawn-bound channel is a pair of shared single-producer single-consumer ring buffers, one per direction, created with `mmap(MAP_SHARED)` before `fork` so both processes map the same physical memory. There is no socketpair and no pipe in the channel transport. Each direction has exactly one writer and one reader (lemma L12), which is what makes the ring sound without locks.

The ring is a 32MB data area per direction, lazily faulted, so the virtual reservation is cheap and no frame within `ringMaxMsg` can silently truncate.

Both directions always exist. The two sides are symmetric concurrent peers, and neither can freeze the other. Backpressure is expressed by ring occupancy, never by a sender blocking in a syscall (lemma L21: the index word is both the state and the notification).

Control rides the same shared memory rather than a separate channel:

- **channel close** - the ring header's closed flag (`ringClose`)
- **child readiness** - a shared control block's ready atomic, which the parent spins on for the synchronous spawn rendezvous
- **child death** - `waitpid` on the parent side, `getppid` on the child side (an orphaned child observes the change and exits)

### ChanID convention

chanID 0 = control (close signals), chanID 1 = first spawn channel arg, chanID 2 = second, etc.

### Channel operations on spawn channels

| Operation | Behavior |
|-----------|----------|
| `ch <- v` | Encode, `ringSend`, retry-and-yield on backpressure. Returns false on close or peer death. |
| `v := <-ch` | `ringRecv`, retry-and-yield while empty. Returns `ok = false` on close or peer death. |
| `select { case v := <-ch: }` | Non-blocking poll via `tryPipeRecv`/`tryPipeSend`, which must never block. |
| `close(ch)` | `ringClose` on both rings; the peer observes the closed flag on recv and on send. |

## Targets

| Target | Output | Memory |
|--------|--------|--------|
| linux/amd64 | Static ELF | Arena allocator |
| linux/arm64 | Static ELF | Arena allocator |
| darwin/amd64 | Mach-O | Arena allocator |
| darwin/arm64 | Mach-O | Arena allocator |
| js/wasm | JavaScript + runtime | Bump allocator |

All native binaries are fully statically linked.

## AI-Assisted Development

[EXAMPLE_CLAUDE.md](EXAMPLE_CLAUDE.md) is a CLAUDE.md template that teaches Claude (or other AI coding assistants) how to write correct Moxie code. Drop it into your project's CLAUDE.md to get accurate code generation that respects Moxie's restrictions and patterns.

## Documentation

- **[docs/axioms.md](docs/axioms.md)** - The three axioms, their justification, and the derived lemmas (normative)
- **[REFERENCE.md](REFERENCE.md)** - Complete language specification
- **[TUTORIAL.md](TUTORIAL.md)** - Learn Moxie from scratch
- **[EXAMPLE_CLAUDE.md](EXAMPLE_CLAUDE.md)** - AI coding assistant configuration
- **[Architecture Patterns](docs/MOXIE_ARCHITECTURE_PATTERNS.md)** - CSP/Actor/DDD design patterns and worked examples
- **[Ownership](docs/OWNERSHIP.md)** - Memory ownership model and design principles
- [docs/spawn.md](docs/spawn.md) - Spawn quick reference
- [docs/architecture.md](docs/architecture.md) - Compiler pipeline and runtime internals
- [docs/restrictions.md](docs/restrictions.md) - Active restrictions and exemptions
- [docs/PORTING.md](docs/PORTING.md) - Migrating existing code to Moxie
- [docs/building.md](docs/building.md) - Build instructions and flags

## Actor Libraries

The architecture patterns document describes a general CSP/Actor model
that applies beyond Moxie. Three libraries implement the intra-domain
actor patterns (sections 3.1-3.6) for other languages:

- **[actor](https://git.smesh.lol/actor)** (Go) - `go get git.smesh.lol/actor`
- **[pickle](https://git.smesh.lol/pickle)** (Rust) - `pickle = { git = "https://git.smesh.lol/pickle" }`
- **[anneal](https://git.smesh.lol/anneal)** (TypeScript) - `npm install anneal`

## Project Structure

```
moxie/
├── _mxc_stage4/             # Self-hosting compiler (Moxie source)
├── compile/                 # Compiler library (forward-ported from stage4)
├── build.sh                 # Bootstrap build script
├── release.sh               # Release tarball builder
├── install.sh               # Remote install script
├── jsruntime/               # JS host shims for WASM target
├── sysroot/                 # Precompiled runtime bitcode, musl, compiler-rt
├── src/                     # Target-side sources (compiled INTO programs)
│   ├── runtime/             # Runtime: scheduler, channels, arena, spawn, stringers
│   ├── internal/task/       # Cooperative task system, context switching
│   ├── moxie/               # Codec interface, built-in codec types
│   └── ...                  # Stdlib (.mx files)
└── docs/                    # Language reference, porting guide, architecture
```

## License

The moxie toolchain (compiler, linker, tools) is licensed under [GPL-3.0-or-later](LICENSE).

The runtime and standard library (`src/`, `jsruntime/`) are MIT, so that programs you build with moxie are your own; see `src/LICENSE` and `jsruntime/LICENSE`. Vendored third-party code (`lib/musl`, `lib/bdwgc`) keeps its own license.

Commercial licensing available on request.
