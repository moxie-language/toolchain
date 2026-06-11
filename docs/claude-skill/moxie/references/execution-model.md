# Moxie Execution Model

## Domains

A Moxie program is a tree of **domains**. Each domain is:
- **Native**: an OS process (created via `fork()`)
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

### IPC Channels

Channels passed to `spawn` become IPC channels multiplexed over a single socketpair. Operations are non-blocking with retry-and-yield (1ms sleep, indefinite retry until success or pipe close). Backpressure expressed by pipe buffer fullness, not blocking.

### Native: socketpair + fork

1. `socketpair(AF_UNIX, SOCK_STREAM)`
2. `fork()` - child gets COW copy
3. Child closes parent's fd, runs function, exits on completion
4. Parent closes child's fd, registers domain, continues

### WASM: Worker

1. New Worker with isolated microtask context
2. Slices element-copied, maps shallow-copied
3. IPC via `BroadcastChannel`

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
