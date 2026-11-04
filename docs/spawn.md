# spawn

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

`spawn` is a language builtin (like `push`, `copy`). No import required. Registered in the type universe alongside other Moxie builtins.

```go
done := spawn(funcName, arg1, arg2, ...)
```

Creates a new domain: a THREAD in the same process (native) or a Worker (JS), running `funcName(arg1, arg2, ...)` with its own arena. Within a domain there is no scheduler and no task switching (lemma L1). Returns `chan struct{}`, the lifecycle channel (the domain's breaker).

## Target Signature

The target's one result must be named `closed` and be of type `chan struct{}`. That name and type together are what make a function a spawn target; the runtime binds the domain's breaker into it.

```go
func worker(in chan Request, out chan Response) (closed chan struct{}) {
    for {
        select {
        case <-closed:
            return closed
        case req := <-in:
            out <- handle(req)
        }
    }
}
```

The breaker is a signal, not a message: nothing is ever sent on it, and `close(breaker)` is the whole event. A parent stops its worker with `close(done)`, and a worker's own death closes its breaker.

## Codec Requirement

A value crosses a spawn channel by one of three routes, decided by the element type:

- no references in the element: the extent is the message (a raw copy into the ring);
- references and a custom codec (`moxie.Codec`: `EncodeTo`/`DecodeFrom`): the codec is the relocation;
- references and no custom codec: the builtin structural codec, with back-references.

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

func worker(n moxie.Int32, scale moxie.Float64) (closed chan struct{}) {
    println(float64(n) * float64(scale))
    return closed
}

func main() {
    select {
    case <-sigint:
        return
    default:
    }
    spawn(worker, moxie.Int32(42), moxie.Float64(2.5))
}
```

## Type Checking

The compiler validates:
- The first argument must be a function
- The target's sole result is `(closed chan struct{})`
- Data and channel element types are encodable: every variable-length
  dimension has a `cap` tag, and a channel, interface, function value or type
  parameter in the frame path is a compile error
- No `interface{}` values cross the boundary
- The target selects on `closed` and returns it
- The target does not reach a channel through a package global

Channel arguments are tagged as spawn-bound. The parent binds its halves with `MarkParentPipeChannel` per channel argument; the child binds the reversed halves before running the target.

## Values and Channels

Values are copied and frozen at the spawn call; channels are shared handles. A spawn boundary is a sharing boundary, not an isolation boundary: the child holds the same rings the parent holds. A response arrives over a channel the caller passed in, never through mutable package state - package variables are thread-local, and a new domain starts with zeroed TLS.

## Channel Behavior

A spawn-bound channel is a pair of shared SPSC ring buffers, one per direction. The data area is `pow2((N + 1) * align8(4 + frameBudget(T)))`: `N` is the declared depth and `frameBudget(T)` is the compiler's upper bound on one encoded message, from the `cap` tags. Both ends are threads in one address space, so the rings are ordinary shared memory, one writer and one reader per direction, and no lock is needed (lemmas L12, L13).

A send returns as soon as its frame is in the ring; it parks on the ring's read index (`ringParkRead`, with 20ms bounds, then a recheck of ring state and peer liveness) only when the declared number of messages is already outstanding, and the receiver advancing that index is the wake. `select` uses the non-blocking `tryPipeSend`/`tryPipeRecv` variants, which must never block. There is no yield-and-retry loop and no give-up.

Control rides the same shared memory: channel close is the ring header's closed flag, child readiness is a shared atomic the parent spins on, and a domain's death is the close of its breaker - the same `chan struct{}` the parent got from `spawn` and the child calls `closed`.

## Native Implementation

A domain is a `pthread` (64 MB stack), created by `spawnDomain` with its own TLS: domain root, arena stack and package globals. The ring pairs and the control block are created before the thread starts, and the child receives them through a handoff record in the root arena. The child sets `childSpawnTarget`, binds its side of the rings, signals ready and calls the target. There is no scheduler and no run queue (lemma L1).

Each domain has its own arena stack. There is no GC; a domain's arenas are released when its thread exits, and a function's arena is released at return after its return values are relocated into the caller's arena (lemma L19).

## JS Implementation

Each domain is a dedicated Worker driving a WebAssembly instance, with spawn channels carried on SharedArrayBuffer-backed rings. `sigint` never fires in a Worker; a browser module is stopped by the host tearing down the Worker. A panic posts its message to the page as stderr, and the page stops every domain it spawned - the browser counterpart of one process aborting.
