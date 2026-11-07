# Moxie Architecture

## Core Principle

The server is dumb. The client is smart. Each domain is sovereign.

Moxie enforces isolation at the language level. A domain is a thread in one process with its own TLS, root arena and event loop; domains share an address space but not access - the only handles between them are channels, and there is no API that reaches another domain's memory. Each domain runs a single thread with channels and select for event dispatch.

Everything in this document is the mechanical realization of the three axioms in [axioms.md](axioms.md), with the lemma ids given where a claim is a consequence of one. Where this document and the axioms disagree, the axioms are correct.

## Compilation Pipeline

The stage4 compiler is written in Moxie (`_mxc_stage4`). Its pipeline:

```
Moxie source (.mx files)
    │
    ▼
resolveAll: discover packages, read sources, build the package graph
    │
    ▼
prepareDispatchIndex: export facts (.mxh), dispatch index, type database
    │
    ▼
whole-link typecheck: every package typed into the registry, conversion
    sites recorded, export data written
    │
    ▼
per-package compile (dependency order): typecheck/SSA, then IR emit
    ├── function arenas, relocation, receiver store-back
    ├── channel/select lowering and frame budgets (cap tags)
    ├── interface dispatch arms (type-ID switch)
    └── restriction checking
    │
    ▼
clang/llc: .ll -> .bc -> .o
    │
    ▼
ld.lld: static ELF/Mach-O (musl + compiler-rt)
```

The legacy Go compiler (`legacy/`) is the bootstrap mirror and the reference
implementation. It performs the same language semantics with go/types and an
SSA/interpreter path for the runtime; it is not the primary pipeline. Both
compilers must agree on emitted behaviour (the wood law in claude.md).

## Execution Model: one thread, no scheduler

Each domain runs a single thread with no scheduler and no task switching. `hasScheduler` and `hasParallelism` are both false (`scheduler_none.mx`), and the task abstraction carries no stack-switching state (`internal/task/task_none.mx`).

- A blocking channel operation parks the entire domain thread. `task.Pause()` is a deadlock error, not a yield: `deadlock: Pause called in single-threaded domain`.
- Timers are the only thing that makes progress while parked. `sleepTicks` sleeps the thread, `fireTimers` runs due timer callbacks, and `Gosched()` is exactly `fireTimers()`.
- No preemption, no timer interrupts, no signal-based stack inspection, no run queue, no context-switch assembly.
- Mutexes and atomics are no-ops in user code because nothing within a domain is shared (lemma L11). Cross-process ordering exists only inside the ring implementation (`atomics_critical.mx`, `spawn_unix.c`), which is the confinement required by lemma L9.

## Spawn: Domain Creation

`spawn` is a language builtin. The target must be a static function whose single
result is named `closed` and typed `chan struct{}`; the runtime binds the
domain's breaker into it. Value arguments are copied and frozen at the call;
channel arguments are shared handles.

```
spawn(fn, args...)
    │
    ▼ (compile time)
    │ verify the target shape (named closed chan struct{})
    │ reject closure captures
    │ size each channel ring from its declaration and frame budget (cap tags)
    │ emit: runtime.spawnDomain(fn, packedArgs, nChans)
    │
    ▼ (runtime, src/runtime/spawn.mx)
    ├── the channel object already exists in the address space; its rings are
    │   mapped and shared by both domains
    ├── pthread_create(domain thread, 64 MB stack)
    │
    ├── child thread:
    │   ├── new DomainRoot/TLS, root arena
    │   ├── mark the child-side ring halves for each channel argument
    │   ├── run the target (no scheduler, no run queue)
    │   └── close its breaker; the thread ends
    │
    └── parent:
        ├── register the channel handles with the child domain
        ├── return the lifecycle channel
        └── a dead peer is observed through the ring state and the breaker
```

There is no `fork`, no socketpair and no pipe in the spawn path. The child is a
thread in the same process, so ring frames are shared memory; what is isolated is
the domain's capability set, not an MMU mapping. The parent detects a dead peer
through the domain breaker and the ring's liveness bits, not `waitpid`.

## Memory Model

- **Within a domain**: single-threaded, no races possible. No mutexes needed. Channels are the synchronization primitive.
- **Between domains**: contractual isolation in one address space. No pointer is handed across; values are copied (or codec-relocated) through the shared rings, and a channel handle is the only shared capability (lemma L15).
- **Allocation**: bump-pointer arena per domain. `alloc()` bumps within the current arena. No GC, no free lists, no individual free. See [ownership.md](ownership.md) for the ownership model and lemma L19 for why this is the only allocation model the axioms admit.
- **Per-function ownership**: the compiler emits FnArenaPush/Pop around each function. Return values are relocated into the caller's arena before the function's arena is released.
- **Domain exit**: the domain's arenas are released when its thread ends; the process address space is not shared with another process to unmap.

## Compiler Type Database

The compiler keeps a per-build type-resolution database (`docs/type-db.md`).
Facts are keyed nominally (package + declaration name); the structural hash is
a secondary index only. Six indexes answer the questions the build needs: type
to methods (sorted), package to interface contributions, (package, name),
hash, package to types, and alias/underlying to named types. There is
deliberately no method-to-types index: conversion sites are recorded at the
three `MakeInterface` sites (assignment/coercion, return, argument), so the
actual implementer set is traffic, not the whole closure. On the nominal
fixture the cold build records 201 sites and collapses them to 48 distinct
(interface, type) edges.

## Compiler Build Memory

- The dispatch fingerprint measurement runs in its own arena, freed per
  package; its arm lists used to accumulate on the build arena.
- A package's type graph lives in a per-package arena and is released at its
  last-need index; the whole-link graphs are not root-lifetime.
- `MOXIE_ARENA_CHECK=1` fails loudly if one package leaves more than 32 MB on
  the build arena, because the build arena is build-lifetime, not per-package.
- Measured cold serial `http_header_api`: ~2.4 GB peak (was ~7.8 GB), root
  high-water 1662 MB; the compiler self-build peaks at 964 MB.

## Build Tags

Per-file build constraints in Moxie source use the `//:build` directive. The
tags select source files for the target; the compiler also accepts a small set
of compatibility tags (`moxie`, `moxie.stringer`, `moxie.linux`, `moxie.unicore`,
`purego`, `osusergo`, `math_big_pure_go`, `go1.x`). `//:build ignore` excludes a
file from every build.

```
//:build linux
//:build unix
//:build js && wasm
//:build wasm
//:build amd64
//:build arm64
//:build ignore        (excluded from every build)
```

A file whose name carries a platform part that does not match the target is
skipped, so `foo_arm64.mx` is compiled only for arm64.

The TinyGo scheduler pseudo-tags (`gc.dealloc`, `scheduler.tasks`,
`scheduler.cores`, `scheduler.asyncify`) select no file in this tree: the
single-thread, no-scheduler model they used to gate is now the only model (see
the execution model above). `moxie.stringer` is not inert - it selects the real
builtin Stringer bodies on every target.

## Target Configuration

Moxie supports exactly 4 native targets plus JS:

| GOOS/GOARCH | Scheduler | Memory | Libc | Linker |
|-------------|-----------|--------|------|--------|
| linux/amd64 | none | arena | musl | ld.lld |
| linux/arm64 | none | arena | musl | ld.lld |
| darwin/amd64 | none | arena | darwin-libSystem | ld.lld |
| darwin/arm64 | none | arena | darwin-libSystem | ld.lld |
| js | none | arena | - | - |

No embedded targets. No WASI. No Windows.
