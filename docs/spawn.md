# spawn

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

`spawn` is a language builtin (like `push`, `copy`). No import required. Registered in the type universe alongside other Moxie builtins.

```go
done := spawn(funcName, arg1, arg2, ...)
```

Creates a new domain: an isolated OS process (native) or Worker (JS), running `funcName(arg1, arg2, ...)` with its own arena. Within a domain there is no scheduler and no task switching (lemma L1). Returns `chan struct{}` (the lifecycle channel).

## Codec Requirement

All data arguments and channel element types must implement `moxie.Codec`:

```go
import "moxie"

type Codec interface {
    EncodeTo(w io.Writer) error
    DecodeFrom(r io.Reader) error
}
```

Built-in codec types encode little-endian by default. Big-endian aliases exist for protocols like Bitcoin:

| Default (LE) | Big-endian (BE) | Size |
|-------------|-----------------|------|
| `moxie.Bool`, `moxie.Int8`, `moxie.Uint8` | - | 1 byte |
| `moxie.Int16`, `moxie.Uint16` | `moxie.BigInt16`, `moxie.BigUint16` | 2 bytes |
| `moxie.Int32`, `moxie.Uint32` | `moxie.BigInt32`, `moxie.BigUint32` | 4 bytes |
| `moxie.Int64`, `moxie.Uint64` | `moxie.BigInt64`, `moxie.BigUint64` | 8 bytes |
| `moxie.Float32` | `moxie.BigFloat32` | 4 bytes |
| `moxie.Float64` | `moxie.BigFloat64` | 8 bytes |
| `moxie.Bytes` | - | 4-byte LE length prefix + data |

User-defined structs implement Codec by defining EncodeTo/DecodeFrom methods that encode each field.

## Example

```go
import "moxie"

func worker(n moxie.Int32, scale moxie.Float64) {
    println(float64(n) * float64(scale))
}

func main() {
    spawn(worker, moxie.Int32(42), moxie.Float64(2.5))
}
```

## Type Checking

The compiler validates:
- The first argument must be a function
- Result type is `chan struct{}`, the lifecycle channel
- All data argument types implement `moxie.Codec`
- All channel element types implement `moxie.Codec`
- No `interface{}` values cross the boundary
- No pointers, functions, or nested channels

Channel arguments are tagged as spawn-bound. The parent binds its halves with `MarkParentPipeChannel` per channel argument; the child binds the reversed halves before running the target.

## Move Semantics

Non-constant, non-channel values passed to `spawn` cannot be used afterward:

```go
x := compute()
spawn(worker, x)
println(x)        // compile error: ownership moved to child
```

## Channel Behavior

A spawn-bound channel is a pair of shared SPSC ring buffers, one per direction, mapped `MAP_SHARED` before `fork` (32MB data area per direction, lazily faulted). Each direction has exactly one writer and one reader, so no lock is needed (lemmas L12, L13).

Blocking operations retry with yield: `ringSend` and `ringRecv` are attempted, and on backpressure or emptiness the domain sleeps for 1ms and retries until the operation succeeds, the ring closes, or the peer dies. `select` uses the non-blocking `tryPipeSend`/`tryPipeRecv` variants, which must never block. Both sides are symmetric concurrent peers, and there is no parent/child behavioural distinction. Backpressure is expressed by ring occupancy, not by a sender blocking in a syscall (lemma L21).

Control rides the same shared memory: channel close is the ring header's closed flag, child readiness is a shared atomic the parent spins on, and peer death is `waitpid` on the parent side and `getppid` on the child side. Inversion cannot happen: an orphaned child observes the `getppid` change and exits.

## Native Implementation

`fork()` plus the shared ring pairs and a shared control block, all created before the fork. The child gets a COW copy of parent memory, sets `childSpawnTarget`, routes guard-page fault notifications to a lockdown pipe (observability only), then runs the wrapper directly: unpack arguments, mark the child-side ring halves, signal ready, call the target. There is no scheduler and no run queue (lemma L1).

Each domain has its own bump-pointer arena. There is no GC; the arena is released by `munmap` on domain exit, and a function's arena is released at return after its return values are relocated into the caller's arena (lemma L19).

## JS Implementation

`$rt.domain.spawn(async () => fn(args))`. Slices spread-copied, maps `Object.assign`'d. Isolated microtask scheduler per domain.
