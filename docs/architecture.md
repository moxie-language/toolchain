# Moxie Architecture

## Core Principle

The server is dumb. The client is smart. Each domain is sovereign.

Moxie enforces isolation at the language level. Domains are OS processes. There is no shared memory. Communication is serialized over IPC. Within each domain, goroutines cooperate on a single thread.

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
    ├── goroutine lowering: go → task.start
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
    ├── compile musl, bdwgc, compiler-rt
    ├── compile extra .c files (signal.c, spawn_unix.c)
    └── link with ld.lld
    │
    ▼
Static ELF/Mach-O binary
```


## Cooperative Scheduler

Each domain runs `scheduler = "tasks"` - a cooperative FIFO scheduler.

```
                ┌──────────┐
                │ Run Queue │ (FIFO)
                └─────┬────┘
                      │
          ┌───────────┼───────────┐
          ▼           ▼           ▼
     ┌────────┐  ┌────────┐  ┌────────┐
     │ Task 0 │  │ Task 1 │  │ Task 2 │
     │ stack  │  │ stack  │  │ stack  │
     └────────┘  └────────┘  └────────┘
```

- Tasks yield at channel operations (`chan.go`) and `Gosched()`
- Context switch via assembly (`task_stack_amd64.S`, `task_stack_arm64.S`)
- `task.Pause()` suspends the current task, `task.Resume()` re-queues it
- No preemption, no timer interrupts, no signal-based stack inspection
- Mutexes and atomics are no-ops (`mutex-cooperative.go`, `atomic-cooperative.go`) - single thread

## Spawn: Domain Creation

`spawn` is a language builtin (like `copy`, `append`). The type checker validates
argument count and types. The SSA validator enforces `moxie.Codec` on all data
arguments and channel element types.

```
spawn(fn, args...)
    │
    ▼ (compile time)
compiler/spawn.go
    │ validate all args implement moxie.Codec
    │ reject pointers, interfaces, functions
    │ emit: runtime.spawnDomain(wrapper, packedArgs)
    │
    ▼ (runtime)
src/runtime/spawn.go
    │
    ├── socketpair(AF_UNIX, SOCK_STREAM) → fds[2]
    ├── fork()
    │
    ├── child (pid == 0):
    │   ├── close(fds[0])
    │   ├── drain parent run queue
    │   ├── task.start(wrapper, args, 8KB)
    │   ├── scheduler(returnAtDeadlock=true)
    │   └── exit(0)
    │
    └── parent:
        ├── close(fds[1])
        └── continue execution
```

## Memory Model

- **Within a domain**: single-threaded, no races possible. No mutexes needed. Channels are the synchronization primitive.
- **Between domains**: complete isolation. No shared pointers. Channel data is serialized over IPC.
- **GC**: Boehm conservative GC per domain. Each forked process has its own heap.

## Build Tags

Moxie uses `moxie` and `moxie.unicore` build tags:

```
//go:build moxie.unicore    → cooperative single-core (always true)
//go:build moxie            → moxie compiler (always true)
//go:build gc.boehm         → Boehm GC (default on linux/darwin)
//go:build scheduler.tasks  → cooperative scheduler (always true)
```

## Target Configuration

Moxie supports exactly 4 native targets plus JS:

| GOOS/GOARCH | Scheduler | GC | Libc | Linker |
|-------------|-----------|-----|------|--------|
| linux/amd64 | tasks | boehm | musl | ld.lld |
| linux/arm64 | tasks | boehm | musl | ld.lld |
| darwin/amd64 | tasks | boehm | darwin-libSystem | ld.lld |
| darwin/arm64 | tasks | boehm | darwin-libSystem | ld.lld |
| js | tasks | - | - | - |

No embedded targets. No WASI. No Windows.
