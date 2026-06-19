# Moxie Architecture

## Core Principle

The server is dumb. The client is smart. Each domain is sovereign.

Moxie enforces isolation at the language level. Domains are OS processes. There is no shared memory. Communication is serialized over IPC. Each domain runs a single thread with channels and select for event dispatch.

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
- **Allocation**: bump-pointer arena per domain. `alloc()` bumps within the current arena. No GC, no free lists, no individual free.
- **Per-function ownership**: compiler emits mark/restore around each function. Return values deep-copied to caller's region before restore reclaims the function's allocations.
- **Domain exit**: `munmap` releases all arena pages in one operation.

## Build Tags

Moxie uses `moxie` and `moxie.unicore` build tags:

```
//go:build moxie.unicore    → cooperative single-core (always true)
//go:build moxie            → moxie compiler (always true)
//go:build gc.dealloc       → arena allocator, no GC (always true)
//go:build scheduler.tasks  → cooperative scheduler (always true)
```

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
