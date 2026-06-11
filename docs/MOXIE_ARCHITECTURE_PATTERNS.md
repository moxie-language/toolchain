# Architecture Patterns for CSP/Actor/DDD Systems in Moxie

*Domain-isolated, event-driven program design: principles, patterns, and worked examples.*

---

## 1. The Model

A Moxie program is a **tree of domains**. Each domain is an isolated OS process with a single thread of execution. Domains communicate exclusively through serialized IPC channels created by `spawn`. There is no shared memory anywhere in the system. There are no goroutines, no scheduler, no locks, and no atomics - these are not omissions, they are the design.

Three traditions converge here:

- **CSP** (Hoare, 1978): processes communicate through channels; `select` is guarded choice; the channel is the synchronization primitive.
- **Actor model** (Hewitt, 1973): each actor owns its state exclusively, processes one message at a time, and the only way to affect an actor is to send it a message.
- **Domain-Driven Design** (Evans, 2003): software decomposes along business-meaningful boundaries (bounded contexts); each context has its own model and its own language; contexts integrate through explicit translation layers.

In most languages these are disciplines you impose on yourself. In Moxie they are the only things the language can express. A domain *is* a bounded context *is* an actor: one process, one thread, one owner for every piece of state, one mailbox (the `select` loop), and a serialization boundary (the `Codec` interface) acting as the anti-corruption layer at every edge.

### 1.1 The entrypoint is an orchestrator

`main` does not do work. `main` is the root of the domain tree. Its job is:

1. Parse configuration.
2. Spawn the top-level domains.
3. Run the root event loop: route messages between children, watch for child death, decide on restart or shutdown.
4. Propagate shutdown downward when the program ends.

```
package main

import "moxie"

func main() {
    // 1. configuration
    cfg := loadConfig()

    // 2. spawn top-level domains, each with its duplex channel pair
    ingestTx, ingestRx := makeDuplex()
    storeTx, storeRx := makeDuplex()
    serveTx, serveRx := makeDuplex()

    spawn(ingestDomain, ingestRx, cfg.IngestConf)
    spawn(storeDomain, storeRx, cfg.StoreConf)
    spawn(serveDomain, serveRx, cfg.ServeConf)

    // 3. root event loop: routing and supervision only
    for {
        select {
        case ev := <-ingestTx.out:
            storeTx.in <- ev          // pipeline: ingest -> store
        case q := <-serveTx.queries:
            storeTx.queries <- q      // pipeline: serve -> store
        case r := <-storeTx.results:
            serveTx.results <- r      // pipeline: store -> serve
        case sig := <-signals:
            shutdownAll(ingestTx, storeTx, serveTx)
            return
        }
    }
}
```

The root never touches event data beyond moving it between channels. It holds no business state. If you find business logic in `main`, a domain is missing.

### 1.2 Fractal decomposition

Every domain has the same internal shape as the whole program. A domain that needs concurrency spawns its own children and becomes an orchestrator for them. The pattern recurses until a domain is simple enough to be a pure leaf - a single `select` loop over its own state with no children.

```
main (root orchestrator)
├── ingest (orchestrator)
│   ├── listener (leaf: accepts connections)
│   └── decoder pool (orchestrator)
│       ├── decoder[0] (leaf)
│       ├── decoder[1] (leaf)
│       └── decoder[2] (leaf)
├── store (leaf: owns the database handle)
└── serve (orchestrator)
    ├── session[...] (leaves, spawned per connection)
    └── responder (leaf)
```

Rules of the fractal:

- **Orchestrators route; leaves compute.** A domain is one or the other. Mixing the roles is the smell that precedes every tangled event loop.
- **Parents own children.** A child's lifecycle belongs to its parent: the parent spawns it, the parent detects its death, the parent decides what death means. A child never knows about its siblings except through channels the parent wired up.
- **Depth is cheap, width is deliberate.** Adding a level of orchestration costs one process and some pipe buffers. Adding width (pool size) is a capacity decision and should be a config number, not an architectural one.

---

## 2. The Five Laws

Each law maps to a Wu Xing element and governs one aspect of inter-domain design. They are checks to run against any proposed channel topology.

### Earth - state ownership

State lives with the longer-lived party. Short-lived workers receive **copies** (which the spawn boundary enforces - all arguments are serialized through `Codec`, ownership of non-constant values transfers). A worker that dies takes nothing with it that anyone else needed. Death is **reported** to the parent on the control channel (chanID 0) - never hidden, never auto-recovered behind the parent's back. The parent decides whether death means respawn, degrade, or propagate.

Anti-pattern: a "registry" domain that holds state belonging to many short-lived sessions. When the registry restarts, every session's state evaporates. The state should live in the sessions (each owning its own) or in a domain whose lifetime genuinely spans them (a store).

### Metal - precise interfaces

Every peer-to-peer interaction gets **independent bidirectional channels**. Give the stranger a key, not the house: a child receives exactly the channels it needs, typed to exactly the messages it handles, and nothing else. Moxie enforces the strong version of this - there is no way to pass a pointer, closure, or interface through `spawn`, so a child physically cannot reach anything it wasn't given.

The duplex requirement is not stylistic. A simplex channel plus a lock-like turn-taking convention is strictly more complex and introduces deadlock: if both sides must take turns, each can end up waiting for the other's turn. The spawn socketpair is full-duplex and both sides are symmetric concurrent peers; design your logical channels the same way.

### Water - backpressure by buffer

Contested flow resolves through buffer state, not through freezing the sender. Moxie's spawn channels implement this at the runtime level: sends and receives are non-blocking with retry-and-yield, and backpressure is expressed by pipe buffer fullness. Neither side can freeze the other in a syscall.

At the application level, honor the same principle. A producer that outruns a consumer should see a full buffer and make a *policy decision* - drop, coalesce, spill, or slow its own intake - rather than wedging. The policy belongs to the producer because the producer has the context to decide what staleness means for its data.

```
select {
case out <- ev:
    // delivered
default:
    // buffer full: policy decision, made explicitly
    dropped++
    if dropped&1023 == 0 {
        report <- DropStats{Count: moxie.Int32(dropped)}
    }
}
```

### Wood - bilateral incremental growth

Trust between domains scales through small synchronous exchanges, not large upfront commitments. Practically: prefer request/response handshakes for anything stateful, and grow protocol complexity incrementally. A child confirms readiness before the parent floods it. A new protocol version is negotiated per-message-kind, not by big-bang migration. Both sides of a protocol change ship simultaneously or not at all - a channel where the sender speaks v2 and the receiver expects v1 is two claimants on one contract, which means zero owners.

### Fire - measurement before pricing

You cannot tune what you have not measured, and you cannot measure what is not sovereign. Each domain owns its own counters (messages in, messages out, drops, queue high-water marks) and reports them upward on request through a stats query - one more message kind on the existing channel. No shared metrics registry, no global sampling. The orchestrator aggregates by asking. Capacity decisions (pool width, buffer depth) are made from these numbers, never from intuition.

---

## 3. Intra-Domain Patterns

Within a single domain there is one thread. Channels and `select` are the event dispatch system - an unbuffered send jumps execution directly to the waiting `select` case; a buffered send queues for the next iteration. This means intra-domain "actors" are really event handlers sharing one event loop, and the patterns below are about organizing that loop.

### 3.1 The actor loop

The canonical shape. All state in the domain is reachable only from this function's locals.

```
func storeDomain(ctl chan moxie.Bytes, reqs chan Request, results chan Result) {
    db := openDB()        // state: owned here, visible nowhere else
    cache := newCache()

    for {
        select {
        case req := <-reqs:
            r := db.lookup(req)
            cache.note(req, r)
            results <- r
        case <-ctl:
            db.close()
            return
        }
    }
}
```

Properties worth stating because they are guarantees, not habits:

- One message at a time. No interleaving, ever.
- `db` and `cache` cannot race because nothing else can name them.
- Shutdown is a message like any other, handled at a message boundary - the domain is never killed mid-operation by its own control flow.

### 3.2 Typed operation channels

One channel per operation, not one channel with a type switch. The dispatch is the `select` statement, resolved structurally rather than by inspecting a discriminant field.

```
type Store struct {
    get    chan GetReq      // Func: request -> response
    put    chan PutReq      // Proc: fire-and-forget mutation w/ ack
    stats  chan chan Stats  // Query: no argument, returns snapshot
    flush  chan chan bool   // Signal: no argument, ack only
}
```

Four shapes cover nearly everything:

| Shape | Carries | Returns | Use for |
|---|---|---|---|
| **Func** | request payload | response payload | reads and computed mutations |
| **Proc** | request payload | ack | mutations where the caller needs visibility, not data |
| **Query** | nothing | snapshot | introspection, stats |
| **Signal** | nothing | ack | lifecycle nudges (flush, rotate, reload) |

A fifth, **Inbox**, is a buffered fire-and-forget channel for high-frequency events where the sender must never wait and occasional loss under pressure is acceptable (activity timestamps, metrics ticks). Use it sparingly and consciously: every Inbox is a place where a caller can observe stale state, because the send returns before the handler runs.

### 3.3 The synchronous-mutation rule

If a caller will read state that a mutation affects, the mutation must be synchronous from the caller's perspective. The reply channel inside the request message is the mechanism:

```
type PutReq struct {
    Key  moxie.Bytes
    Val  moxie.Bytes
    done chan bool      // buffered(1): handler writes, never blocks
}

func (s *Store) Put(key, val moxie.Bytes) {
    req := PutReq{Key: key, Val: val, done: make(chan bool, 1)}
    s.put <- req
    <-req.done          // mutation visible after this returns
}
```

The reply channel is buffered(1) so the handler's write never blocks even if the caller has abandoned (e.g. timed out). The handler writes to a channel nobody reads; no leak, no wedge.

The corollary: every fire-and-forget buffered send followed by a synchronous read of dependent state is a race by construction. The message may still be in the buffer when the read executes. This is the most common bug class in channel refactors, and it is mechanical to spot: grep for buffered sends whose effects a test or caller immediately inspects.

### 3.4 Construction is spawning

An actor-shaped type without its event loop running is a corpse. Direct struct construction - the shared-memory habit of "make the struct, call methods" - produces nil channels and deadlocks on first use. The constructor must initialize every channel and start the loop, and there must be no other way to obtain the value:

```
func NewStore(path moxie.Bytes) *Store {
    s := &Store{
        get:   make(chan GetReq),
        put:   make(chan PutReq),
        stats: make(chan chan Stats),
        flush: make(chan chan bool),
    }
    go-equivalent: in Moxie, the loop IS the domain - spawn it,
    or for intra-domain actors, register its channels in the
    parent select loop.
    return s
}
```

(In Moxie proper, an intra-domain "actor" is a set of channels handled by the domain's single select loop; a true concurrent actor is a spawned child domain. Choose by whether the work needs parallelism or just isolation - see §4.1.)

### 3.5 Post-mortem queries are undefined

Calling into a stopped actor is dereferencing a freed pointer. The old-code habit of `select { case ch <- req: ... case <-stopped: return zero }` returns a zero value by accident, not by contract. Do not preserve it. The contract is: after Stop returns, the actor answers nothing, and any caller still holding its channels has a lifecycle bug upstream. Fix the caller, not the actor.

### 3.6 Context-aware calls

When a caller holds a deadline or cancellation and must bail if the actor is slow, the call needs two escape points - one on the send, one on the receive:

```
func (s *Store) GetCtx(ctx Ctx, req GetReq) (Result, error) {
    msg := getMsg{req: req, resp: make(chan Result, 1)}
    select {
    case s.get <- msg:
    case <-ctx.Done():
        return Result{}, ctx.Err()
    }
    select {
    case r := <-msg.resp:
        return r, nil
    case <-ctx.Done():
        return Result{}, ctx.Err()
    }
}
```

The (Resp, error) split is load-bearing: the error position carries *transport* failure (cancelled, actor gone), the Resp carries the *operation* outcome, which may itself contain an operation-level error. `(error, error)` returns are ugly and exactly right - two failure planes, two values, unambiguous at every call site.

---

## 4. Inter-Domain Patterns

### 4.1 When to spawn

Spawn a child domain when any of these hold:

1. **Parallelism.** The work needs another core. Intra-domain channels give you concurrency of *structure* (interleaved event handling) but never of *execution* - one domain, one thread.
2. **Fault isolation.** The work can crash and the parent must survive it (parsers on untrusted input, plugins, anything touching the network).
3. **Lifecycle mismatch.** The work's lifetime differs from the parent's (per-connection sessions, batch jobs).
4. **Bounded-context boundary.** The work belongs to a different domain model in the DDD sense. Even with no performance motive, the serialization boundary keeps the models from bleeding into each other.

Do not spawn for organization alone. Intra-domain actors organized around one select loop are free; processes are cheap but not free. The fractal grows on demand.

### 4.2 The spawn boundary as anti-corruption layer

Everything crossing `spawn` implements `Codec`. No pointers, no functions, no interfaces cross. This is DDD's anti-corruption layer made physical: a domain cannot depend on another domain's internal representation because the only thing that travels is an explicitly defined wire format.

Design messages as the *published language* of the domain, not as serialized internals:

```
// published language of the store domain: what it promises, not what it is
type PutEvent struct {
    Key       moxie.Bytes
    Val       moxie.Bytes
    Timestamp moxie.Int64
}

func (e *PutEvent) EncodeTo(w io.Writer) error { ... }
func (e *PutEvent) DecodeFrom(r io.Reader) error { ... }
```

When the store's internals change, the codec stays stable and the rest of the tree does not recompile its mental model. When the codec must change, both sides change simultaneously (wood) or a version field gates the transition.

### 4.3 Worker pools

A pool is a sub-orchestrator with per-worker channels. The parent dispatches explicitly - round-robin or least-loaded - onto each worker's own input channel, and collects from each worker's own result channel in rotation. Every channel in the system stays single-producer single-consumer.

```
func decoderPool(ctl chan moxie.Bytes, in chan RawMsg, out chan Event, width moxie.Int32) {
    workers := make([]duplex, width)
    for i := range workers {
        workers[i] = makeDuplex()
        spawn(decoder, workers[i].rx)
    }

    next := moxie.Int32(0)
    for {
        select {
        case raw := <-in:
            workers[next].in <- raw            // explicit dispatch
            next = (next + 1) % width
        case ev := <-workers[0].out:           // collect in rotation
            out <- ev
        case ev := <-workers[1].out:
            out <- ev
        case ev := <-workers[2].out:
            out <- ev
        case <-ctl:
            closeAll(workers)
            return
        }
    }
}
```

Why not N workers reading one shared channel? Shared-channel work-stealing is implicit coordination - it works in Go's runtime but it is a structure the orchestrator does not control and cannot observe. Explicit dispatch keeps the routing decision where the routing knowledge is, makes per-worker load measurable (fire), and maps directly onto the spawn model where each child has its own pipe anyway.

Buffered per-worker result channels eliminate head-of-line blocking: a slow worker just presents an empty buffer when the collector rotates past; fast workers never wait on it. Workers block only when their own buffer fills - backpressure, not deadlock (water).

### 4.4 Pipelines

The default macro-structure. Each stage is a domain, each owns its state, data flows one direction, and the orchestrator wires stage N's output to stage N+1's input. Backpressure propagates upstream naturally through buffer fullness; the slowest stage sets the pace without any explicit rate-limiting machinery.

```
listener -> verifier pool -> dispatcher -> store
                                  └-----> subscribers
```

A stage with fan-out (dispatcher feeding both store and subscribers) makes the same drop-or-wait policy decision per output (water). A stage with fan-in does not exist: fan-in is the orchestrator's rotation over per-source channels (§4.3), never multiple producers on one channel.

### 4.5 Supervision

Child death arrives as a close on the control channel. The parent's options, in order of preference:

1. **Respawn** - for stateless workers (pool members). The pool orchestrator respawns and reports the incident upward as a counter, not an emergency.
2. **Degrade** - for optional capabilities. Mark the capability down, keep serving everything else, retry on a timer.
3. **Propagate** - for children whose loss makes the parent meaningless. Close own resources, report death upward, exit. The decision repeats at each level; this is how a fatal error in a leaf unwinds the exact subtree that depended on it and nothing more.

Never auto-recover *state*. Respawning a worker recreates a process, not its memory. If the lost state mattered, its loss must be visible to whoever owned the data flowing through it - usually as a retransmit request upstream or an error downstream. Hiding state loss converts a crash into silent corruption.

### 4.6 Shutdown

Shutdown propagates root-to-leaf as control messages; completion propagates leaf-to-root as channel closes. The root closes control to its children; each orchestrator forwards to its children, waits for their closes, finishes its own teardown, then closes toward its parent. Inversion (leaves outliving parents) is impossible by construction because the pipe close is itself the death notification.

Drain semantics are a per-stage decision and must be explicit: on shutdown, does a stage finish its queued input (drain-then-stop) or abandon it (stop-now)? Stages with durable side effects (store) drain; stages with idempotent or refetchable input (decoders) stop. Write it in the message contract.

---

## 5. DDD Mapping Summary

| DDD concept | Moxie realization |
|---|---|
| Bounded context | Domain (spawned process) |
| Aggregate root | The domain's select loop - sole entry to its state |
| Aggregate invariant | Holds trivially: one message at a time, no observer mid-mutation |
| Repository | A store domain; access is messaging, never a handle to internals |
| Domain event | A codec message on a pipeline channel |
| Published language | The Codec types on a domain's channels |
| Anti-corruption layer | The spawn serialization boundary itself |
| Ubiquitous language | The message type names - they ARE the domain vocabulary |
| Context map | The channel wiring in each orchestrator - readable in one place |

The deepest alignment: DDD's central struggle is keeping models from bleeding across context boundaries, and every mainstream language fights this with discipline because a reference can always leak. Moxie cannot express the leak. The context map is not a diagram that drifts from the code; it is the code, in the orchestrator, in one screenful.

---

## 6. Checklist

Run against any design before building:

- [ ] Does `main` contain only spawn, route, supervise? (§1.1)
- [ ] Is every domain either an orchestrator or a leaf, never both? (§1.2)
- [ ] Does every piece of state have exactly one owning domain, with the longer lifetime? (earth)
- [ ] Does every peer pair have independent duplex channels typed to exactly their messages? (metal)
- [ ] Is every full-buffer outcome an explicit policy (drop/coalesce/spill/slow), never a wedge? (water)
- [ ] Do both sides of every protocol change ship together? (wood)
- [ ] Can every domain report its own counters on request? (fire)
- [ ] Is every mutation a caller depends on synchronous via reply channel? (§3.3)
- [ ] Is every actor obtainable only through its constructor? (§3.4)
- [ ] Are all channels SPSC - pools via per-worker channels and rotation? (§4.3)
- [ ] Is every child death handled by an explicit respawn/degrade/propagate decision? (§4.5)
- [ ] Is drain-vs-stop written down for every stage? (§4.6)

---

*Coherence dies at boundaries. Both sides simultaneously or not at all. Pass handles, not objects. Cost is physical, not social. Ownership unambiguous - two claimants means zero owners.*
