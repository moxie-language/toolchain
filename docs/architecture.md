# Moxie Architecture

## Core Principle

The server is dumb. The client is smart. Each domain is sovereign.

Moxie enforces isolation at the language level. Domains are OS processes. There is no shared memory. Communication is serialized across each domain boundary. Each domain runs a single thread with channels and select for event dispatch.

Everything in this document is the mechanical realization of the two axioms in [axioms.md](axioms.md), with the lemma ids given where a claim is a consequence of one. Where this document and the axioms disagree, the axioms are correct.

## Compilation Pipeline

```
Moxie source (.mx files)
    │
    ▼
loader (package loading, MOXIEROOT overlays)
    │
    ▼
cgo (libclang processing for C interop)
    │
    ▼
SSA construction (via golang.org/x/tools/go/ssa)
    │
    ▼
compiler (Moxie SSA → LLVM IR)
    ├── spawn builtin → runtime.spawnDomain (Codec validation)
    ├── restriction checking: reject new, complex, etc.
    ├── channel/select lowering
    └── syscall intrinsics: inline assembly
    │
    ▼
transform (LLVM IR optimization passes)
    │
    ▼
interp (compile-time partial evaluation)
    │
    ▼
builder (orchestration)
    ├── compile musl, compiler-rt
    ├── compile extra .c files (signal.c, spawn_unix.c)
    └── link with ld.lld
    │
    ▼
Static ELF/Mach-O binary
```


## Execution Model: one thread, no scheduler

Each domain runs a single thread with no scheduler and no task switching. `hasScheduler` and `hasParallelism` are both false (`scheduler_none.mx`), and the task abstraction carries no stack-switching state (`internal/task/task_none.mx`).

- A blocking channel operation parks the entire domain thread. `task.Pause()` is a deadlock error, not a yield: `deadlock: Pause called in single-threaded domain`.
- Timers are the only thing that makes progress while parked. `sleepTicks` sleeps the thread, `fireTimers` runs due timer callbacks, and `Gosched()` is exactly `fireTimers()`.
- No preemption, no timer interrupts, no signal-based stack inspection, no run queue, no context-switch assembly.
- Mutexes and atomics are no-ops in user code because nothing within a domain is shared (lemma L11). Cross-process ordering exists only inside the ring implementation (`atomics_critical.mx`, `spawn_unix.c`), which is the confinement required by lemma L9.

## Spawn: Domain Creation

`spawn` is a language builtin (like `copy`, `push`). The type checker validates
argument count and types. The SSA validator enforces `moxie.Codec` on all data
arguments and channel element types, and rejects pointers, interfaces, and
nested channels.

```
spawn(fn, args...)
    │
    ▼ (compile time)
spawn handling in the compiler
    │ validate all args implement moxie.Codec
    │ reject pointers, interfaces, functions, channels
    │ emit: runtime.spawnDomain(fn, packedArgs, nChans)
    │
    ▼ (runtime, src/runtime/spawn.mx)
    ├── ringCreate(32MB) x2 per channel   (mmap MAP_SHARED, parent→child and child→parent)
    ├── spawnCtlCreate()                  (shared ready flag for the spawn rendezvous)
    ├── pipe()                            (lockdown pipe: guard-page fault notification only)
    ├── fork()
    │
    ├── child (pid == 0):
    │   ├── childSpawnTarget = true, childParentPid = getppid()
    │   ├── route fault notifications to the lockdown write end
    │   ├── spawnCall(wrapper, buf): unpack args, mark child-side ring halves,
    │   │   signal ready, call the target (no scheduler, no run queue)
    │   └── exit(0)
    │
    └── parent:
        ├── lastSpawnPid = pid
        ├── domainRegister(pid, lockdownReadFd)
        ├── MarkParentPipeChannel per channel arg (binds its ring halves)
        ├── PipeWaitChildReady()              (bounded spin on the shared ready flag)
        └── return the lifecycle channel
```

The child reverses the ring mapping: what the parent calls the send ring is the child's recv ring, and the reverse. Death detection is asymmetric on purpose - the parent reaps with `waitpid(WNOHANG)`, and the child detects its own orphaning when `getppid()` stops returning the pid it captured at spawn (lemma L22).

## Memory Model

- **Within a domain**: single-threaded, no races possible. No mutexes needed. Channels are the synchronization primitive.
- **Between domains**: complete isolation. No shared pointers. Channel data is encoded into the shared rings; nothing addressable crosses (lemma L15).
- **Allocation**: bump-pointer arena per domain. `alloc()` bumps within the current arena. No GC, no free lists, no individual free. See [OWNERSHIP.md](OWNERSHIP.md) for the ownership model and lemma L19 for why this is the only allocation model the axioms admit.
- **Per-function ownership**: the compiler emits FnArenaPush/Pop around each function. Return values are relocated into the caller's arena before the function's arena is released.
- **Domain exit**: `munmap` releases all arena pages in one operation.

## Build Tags

Per-file build constraints in Moxie source use the `//:build` directive, and they
are platform constraints only:

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

The `moxie.*`, `gc.dealloc`, `scheduler.tasks`, `scheduler.cores` and
`scheduler.asyncify` pseudo-tags inherited from TinyGo appear as constraints in
no source file. They are inert and select nothing. The single-thread, no-scheduler
model they used to gate is now the only model (see the execution model above).

## Target Configuration

Moxie supports exactly 4 native targets plus JS:

| GOOS/GOARCH | Scheduler | Memory | Libc | Linker |
|-------------|-----------|--------|------|--------|
| linux/amd64 | tasks | arena | musl | ld.lld |
| linux/arm64 | tasks | arena | musl | ld.lld |
| darwin/amd64 | tasks | arena | darwin-libSystem | ld.lld |
| darwin/arm64 | tasks | arena | darwin-libSystem | ld.lld |
| js | tasks | bump | - | - |

No embedded targets. No WASI. No Windows.
