# Moxie Execution Model

## Domains

A Moxie program is a tree of **domains**. Each domain is:
- **Native**: a thread in the same process (its own TLS, root arena and event loop)
- **WASM**: a Web Worker

Each domain has exactly one thread. No data races possible. No `sync` package - mutexes and atomics are meaningless in a single-threaded domain.

## Program Startup

1. Packages initialized in dependency order. `var X T` declarations establish static storage (`.bss`), then `init()` runs.
2. `main()` called. Event dispatch (`select`, `spawn`) available from here.

## Concurrency Within a Domain

Three constructs:
- **Unbuffered channel send**: execution transfers to waiting `select` case (like a function call via channel).
- **Buffered channel send**: message queued for next `select` iteration.
- **`select`**: event handler - blocks until channel message, I/O event, or timer fires.

Idiomatic pattern:
```moxie
for {
    select {
    case req := <-requests:
        handle(req)
    case <-quit:
        return
    }
}
```

Cases checked **top-to-bottom**. First ready wins. Deterministic - no random selection.

## spawn

```moxie
done := spawn(fn, arg1, arg2, ...)
```

Creates a new domain. Returns `chan struct{}` (lifecycle channel, closes when child exits).

- First arg must be a **static function name** (not closure, not variable).
- Must not appear inside `init()`.
- Non-constant values are **moved** to child. Use-after-spawn = compile error.
- Constants and channels are exempt from move semantics.

### Codec Requirement

All data arguments and channel element types must implement `moxie.Codec`:

```moxie
type Codec interface {
    EncodeTo(w io.Writer) (err error)
    DecodeFrom(r io.Reader) (err error)
}
```

### Built-in Codec Types (moxie package)

| Default (LE) | Big-endian (BE) | Size |
|-------------|-----------------|------|
| `Bool`, `Int8`, `Uint8` | - | 1 byte |
| `Int16`, `Uint16` | `BigInt16`, `BigUint16` | 2 bytes |
| `Int32`, `Uint32` | `BigInt32`, `BigUint32` | 4 bytes |
| `Int64`, `Uint64` | `BigInt64`, `BigUint64` | 8 bytes |
| `Float32` | `BigFloat32` | 4 bytes |
| `Float64` | `BigFloat64` | 8 bytes |
| `Bytes` | - | 4-byte LE length prefix + data |

### Allowed at Spawn Boundary

| Type | Treatment |
|------|-----------|
| Types implementing `moxie.Codec` | Serialized |
| `*T` where `T` implements `Codec` | Copy semantics (pointer dissolves) |
| Channels (element type implements Codec) | IPC channels |

### Rejected (compile error)

| Type | Reason |
|------|--------|
| Raw builtins (`int32`, `bool`) | Use `moxie.Int32`, `moxie.Bool` |
| `func(...)` | Cannot serialize code |
| Interface types | Cannot cross spawn boundary |
| `*T` where T is non-Codec | Cannot share memory |

### Channels at the Boundary

Channels passed to `spawn` are the same channel objects in both domains; their
single-producer single-consumer rings are in shared memory. Each direction has
one writer and one reader, so no lock is needed. The ring is sized from the
declaration and the frame budget (`chan T{:N}` gives N outstanding messages; one
unstated dimension is one page).

A send returns once its frame is in the ring and parks on the ring's read index
(futex wake on the receiver's advance) when the declared depth is outstanding.
Close is a flag on the ring; a dead peer is observed through the ring state and
the domain's breaker, not `waitpid`.

### Native: threads + rings

1. The channel object and its rings already exist in the shared address space.
2. `pthread_create` starts the domain thread (64 MB stack) with a fresh
   `DomainRoot`/TLS and root arena.
3. The child marks the child-side ring halves for each channel argument, runs
   the target, and closes its breaker when it returns.
4. The parent registers the handles and returns the lifecycle channel.
5. The target's result is named `closed chan struct{}`; the parent observes the
   breaker.

### WASM: Worker

1. New Worker with an isolated microtask context
2. Values are copied across the worker boundary through the host shims
3. Channels are host message ports; close and worker death use the same breaker

## Fan-Out Pattern

```moxie
import "moxie"

func worker(id moxie.Int32, results chan moxie.Int32) {
    results <- moxie.Int32(int32(id) * int32(id))
}

func main() {
    results := chan moxie.Int32{}
    for i := int32(0); i < 4; i++ {
        spawn(worker, moxie.Int32(i), results)
    }
    for i := int32(0); i < 4; i++ {
        println(<-results)
    }
}
```

## I/O Model

All I/O is non-blocking and buffered. `epoll` (Linux) / `kqueue` (Darwin) for readiness notification. All I/O dispatched through event loop via `select`.

## Runtime Guarantees

1. Single-threaded per domain. No preemption.
2. No data races. Structurally impossible.
3. Complete domain isolation. OS-level memory isolation.
4. Deterministic execution given same inputs.
5. Static binaries. No dynamic dependencies.
