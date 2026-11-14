# Concurrency

Status: **implemented**. A domain is a thread; `spawn` starts one; channels and
`select` connect them. There is no scheduler, no goroutine and no lock.

See [language.md](language.md) for the complete language reference.

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
    case <-shutdown:
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

Each domain is a dedicated Worker driving a WebAssembly instance, with spawn channels carried on SharedArrayBuffer-backed rings. `shutdown` never fires in a Worker; a browser module is stopped by the host tearing down the Worker. A panic posts its message to the page as stderr, and the page stops every domain it spawned - the browser counterpart of one process aborting.

## Depth is a message count

A channel is a mailbox with the depth its declaration gives it. `chan T{}` is one
slot; `chan T{:N}` is N outstanding messages; `chan T{:N:bytes}` states the frame
budget as well. The depth is **not** a byte threshold: the ring header carries
`sentFrames` and `recvFrames`, each written by one side only, so
`sentFrames - recvFrames` is the outstanding count whatever the messages weigh.
`cap(ch)` reports the declared N at both ends; `len(ch)` reports the local
in-runtime queue, which the boundary path never uses.

There is no unbuffered channel and no rendezvous. A domain has exactly one
thread, so a rendezvous send could never complete; `chan T{:0}` is a compile
error naming that reason, and a capacity computed at run time is rejected by
`chanMake` before any send can hang on it.

**Sizing: one slot unless the other side's cost varies.** One slot is right for a
latency-sensitive handoff that is promptly consumed. Add slots only where
per-message processing time on the far side varies and the producer's tempo does
not match it. A buffer is slack, not a queue: queue discipline belongs to the
receiver - a sovereign self-mutating type that drains the mailbox into its own
queue - never to the channel. A sender that has to park is telling you the
receiving loop is not returning to its select promptly enough, which is a fault
worth seeing.

## A select arm carries the frame format

`chanSelectState` is `{ch, value, codec, td}` and the compiler fills `codec`/`td`
per arm from the element type, exactly as the direct send/recv path chooses: the
element's own codec when its type has one, the structural codec's descriptor when
it has not, both null when the element is self-contained. The runtime encodes at
the moment the arm FIRES, so a losing arm never pays a caller-written
`EncodeTo`. Before this, a select arm moved raw element bytes while a direct send
encoded them: a select receive of a struct carrying a `[]byte` arrived zeroed and
a select send of one corrupted the ring. There is one encode/decode pair
(`codecFrameEncode`/`codecFrameDecode` in `src/runtime/chan.mx`) and every
channel entry point on both targets routes through it.

## Two entry channels: `shutdown` and `closed`

`main` selects on `shutdown` (SIGINT from the shell, SIGTERM from a supervisor);
a spawn target selects on `closed` (the parent's close, which also happens when
the parent's process ends, because a domain thread cannot outlive it).

`shutdown` is a `chan struct{}` the runtime owns, one per domain, created on
first use in the root arena, so it outlives every frame that selects on it. Both
are signals: nothing is ever sent, and the close is the whole event. The signal
handler does ONE thing - raise a flag - because it may interrupt an allocation, a
channel operation or a held lock; every point where the domain waits polls the
flag and closes `shutdown` there, in normal context.

## Managers and workers

Spawn is heavyweight (pthread + 64 MB stack + ring pairs + a fresh domain init),
so goroutine conventions do not apply. Every domain takes exactly ONE role.

- **Manager**: owns an event loop, owns the channels, multiplexes signals. The
  loop is tight: poll, identify the arm that fired, dispatch.
- **Worker**: a long-lived server. Spawned once, then serves messages over its
  inbox and watches its breaker channels. It never spawns on demand.

A domain is never both at once. The spawn tree therefore has distinct element
types: manager nodes and worker nodes.

**Channel handles are fixed at spawn time.** Everything the child will ever
receive must be among the channels passed in the spawn call; post-spawn traffic
is messages over those handles, never new ones. That is why a timer service is a
worker spawned once with the caller's signal channels, and a timeout is a
message, never a per-wait spawn.

**Workers are servers; per-job spawns are invalid.** A worker replies over the
pipe-bound ring pair correlated by id. Queries and commands are the only inbound
traffic.

## Terminating a domain

Termination is cooperative. A domain can only observe a closed breaker where it
runs Moxie code, so the arm goes at every wait - the top-level select, and inside
each work loop, poll iteration and batch. A worker parked on a receive and never
returning to its select cannot be told to stop; worse, a manager that replaces
such a worker without closing its breaker loses the only handle to it, which is
how one relay accumulated 2,600+ live domains.

The runtime makes the close reach the waits: a spawn target's channel operations
recheck its own `DomainClosed`, and a parent's spawn channels remember the
breaker of the domain they were handed to, so a parent parked on a channel whose
domain ended wakes and reports the close. A domain that never returns to a check
is still reclaimed - not by a signal, since a thread has none, but by the
process: when `main` returns every domain goes with it.

## wasm: the page is the hub

On the browser target a domain is a Worker, and a Worker started by a Worker does
not begin to run while its creator is parked in `Atomics.wait`. The page is the
only realm that is never parked, so the page starts every domain and carries the
control and log planes while the data plane stays direct between domains. The
protocol, the id scheme, the reaping rules and the divergence from native are in
[wasm-hub.md](wasm-hub.md).
