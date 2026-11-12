# The Segment Heap: Unified Memory Model

Status: design proposal. Supersedes the ad-hoc arena backing store described in
`ownership.md`. The ownership semantics in that document do not change. What
changes is what sits underneath them.

This design is not novel. It is an assembly of five established systems, and
knowing which part came from where tells you where to look when something
breaks.

---

## 0. Instructions to the implementer

Read `ownership.md`, `spawn.md`, `pointer-containers.md` and `capabilities.md`
before writing any code. This document assumes all four.

Rules:

- Do not change the language semantics. `FnArenaPush`, `FnArenaPop`,
  return-as-transfer, immutable parameter borrows and codec-based channel
  relocation all stay exactly as specified.
- Do not introduce a write barrier. If a design step requires instrumenting every
  pointer store, the design is wrong for this project.
- Do not introduce handles into the intra-arena hot path. Handles exist only at
  arena boundaries.
- Do not put the identity table in the dereference path. It is not a liveness
  oracle and it must never be consulted to decide whether an object is alive.
- Every phase in section 16 is independently shippable and independently
  testable. Do not begin a phase until the previous phase's acceptance test
  passes.
- When something is genuinely undecided, stop and ask. Section 15 records every
  decision that has been made; do not reopen one without new evidence.

---

## 1. Prior art: what this is assembled from

Five lineages, one mechanism taken from each.

| Mechanism | Source | What transfers | What does not |
|---|---|---|---|
| Segment plus cleaner | Sprite LFS (Rosenblum and Ousterhout 1991), NILFS2, F2FS | append-only writing, victim selection by dead ratio, evacuation of survivors | on-disk durability, flash erase constraints, crash consistency |
| Per-owner heap with copy at the boundary | BEAM (Erlang/OTP) | domain partition, independent collection, codec as the boundary | BEAM's scheduler and process model |
| Scope-bound bulk-free arena | APR pools, C++ `std::pmr`, game frame allocators | push/pop discipline, hierarchical release, one-operation destroy | APR's manual cleanup registration, pool leakage |
| Compacted segmented DRAM heap | RAMCloud log-structured memory (Rumble et al., FAST 2014) | the cost model, cleaning overhead, space overhead | distributed recovery |
| Key-value separation | WiscKey (Lu et al., FAST 2016), BadgerDB | sparse side index, on-demand identity, separating metadata from payload | the indirection itself, index-based liveness |

### The five claims being borrowed

**From Sprite LFS: the cleaning algorithm.** Segments, append-only allocation, and
a cleaner that picks victims by dead ratio and copies survivors into fresh
segments. This is section 9, almost line for line. Production on Linux as NILFS2
and F2FS for two decades. The failure modes are known.

**From the BEAM: the partition.** Every process owns its heap and collects it
alone. Cross-process references do not exist because messages are copied. That is
the domain boundary and the codec, and telecom has run on it since 1986.

**From APR and the frame allocators: the function tier.** A pool that dies whole,
with children dying before parents. httpd has run on this for twenty-five years.
Game engines run the same pattern at frame granularity with tighter budgets and
have therefore tuned it harder.

**From RAMCloud: the calibration.** Log-structured memory in DRAM works, and the
paper reports what it costs. Section 13 summarises the parts that constrain this
design.

**From WiscKey and Badger: the encoding discipline.** Keep the index small and
sparse, keep the payload free of fields that only some objects need, and never
let the index become the authority on liveness. Section 6 applies this to the
object header and removes 16 of the 24 bytes.

### What this means for debugging

When the whole thing is on fire, ask which lineage the bug belongs to. A stale
pointer after a clean is LFS territory. A missing copy is BEAM territory. A pool
that never dies is APR territory. A cleaner that eats the frame budget is RAMCloud
territory. A header field that is stale after a move is WiscKey territory. Sort
the bug before you instrument.

---

## 2. Why the current model fails

Function-scoped arenas solve short-lived allocation well. Every allocation made
inside a function dies with the function, and release is one operation. That part
of the model is sound and is being kept.

The failure is the domain-lifetime arena, the root, and the pool beneath it.

1. **Reclaim exists but is in the wrong place.** Sovereign data arenas already
   copy-compact. `ArenaAllocTD`'s own comment says that when a new region would be
   needed and the arena holds fragmented dead space, the group is compacted first
   and the tail reused, with an allocation map whose referrers walk object
   interiors. That is the right algorithm with the wrong trigger: it fires on
   region exhaustion rather than on dead ratio, it is entangled with the sovereign
   group machinery, and it does not apply to the plain root arena at all. So the
   root grows for the life of the domain, objects in it die at different times, and
   nothing reuses their space. That is the shape of the relay stress failure and
   the MLS marmot growth.

2. **mmap per function.** A syscall per allocating call, and thousands of small
   mappings. The pool returns them, but reuse is not deterministic.

3. **The pool is the only reuse mechanism.** Because arenas are whole mmaps, the
   smallest unit of reuse is a whole arena. A function allocating 4 KiB consumes a
   64 KiB mapping. Waste is structural.

4. **Nothing is verifiable.** No assertion that a function's arena is released, no
   accounting tying an allocation to an owner, no way to distinguish a leak from
   fragmentation. The sovereign-loop declaration problems are a symptom of a
   discipline that is not mechanically enforced.

5. **Metadata dominates small allocations.** A 24-byte inline header plus a
   16-byte index entry is 40 bytes of overhead per object. At 32 bytes of payload
   that is 125% overhead. Section 6 fixes this.

APR pools have the fourth failure too and it is worth naming: a long-lived pool
accumulates because nothing forces an accounting, and Apache modules leaked for
years exactly this way. The fix there was per-request pools. The equivalent here
is per-domain heaps with a compaction trigger.

---

## 3. The theorem that constrains the design

You cannot move an object without knowing every slot that points to it. Raw
pointers patched in place require complete pointer discovery. There are exactly
two mechanisms: scan the slots, which requires layout and stack maps, or trap
every store, which requires a write barrier.

There is no third. Handles are a third and they are the double indirection being
removed.

**Decision: precise scanning, compiler-emitted.** Moxie owns the compiler, so
layout descriptors and stack maps cost nothing at runtime. A write barrier costs
something on every store, forever. The BEAM avoids maps because it has no
cross-heap pointers; we cannot take that option because function arenas nest
inside domains.

**Consequence: the referrer index proposed earlier is unnecessary.** It exists
only if we relocate function arenas. We do not, because of section 4.

---

## 4. The partition

Two kinds of memory, two mechanisms. This is the whole design.

### Function arena

Backing: a bump region carved from a segment. `FnArenaPush` takes a slice;
`FnArenaPop` returns the bump pointer.

Pointers inside a function arena are **offsets from the arena base**, not
addresses. The arena never moves. It is allocated on push, released on pop, and
its interior is never compacted.

Consequence: **no fixup ever for function-arena pointers.** The base lives in a
register or frame slot, offsets are stable for the arena's life. This is the
linker's position-independent-code trick and WebAssembly's linear memory applied
to a scope.

### Root heap

Domain-lifetime. Segmented, append-only, address-based pointers, **compacted**.
The only memory that moves, and therefore the only memory whose pointers must be
discoverable.

### Channel ring buffers

A fourth tier, and the only one with split ownership.

A ring buffer is a fixed region with two indices. The writer owns the tail and may
advance only the tail. The reader owns the head and may advance only the head.
Neither writes the other's index, and neither region of authority overlaps the
other's. That is disjoint authority rather than shared ownership, which is why it
does not violate the axiom in `ownership.md`: every byte still has exactly one
owner at every instant, and the owner of a byte changes exactly once, when it is
consumed.

Ring buffers hold **bytes, never pointers.** Everything crossing a channel is
either a pointer-free extent copied raw, or a codec-encoded form produced by the
sender and decoded by the receiver into its own arena. Nothing in a ring buffer
resolves to an address inside either side's heap.

Consequence: ring buffers need no layout descriptor, no stack map, no scanning and
no relocation. They are outside the object model entirely. They are pinned by
construction and the cleaner never sees them.

```
Ring region: a dedicated mapping, NOT a pool segment
    writeIdx        u32   producer-written
    readIdx         u32   consumer-written
    size            u32   data area, power of two, fixed at creation
    closed          u32
    recvSleeper     u32
    sendSleeper     u32
    sentFrames      u32   producer-written
    recvFrames      u32   consumer-written
    refcount        u32   one per end
    data            size bytes, lazily faulted

Frames: [len u32][flags u32][payload][pad to 8]
Flags: CONTINUATION | FINAL
```

A ring is not drawn from the 1 MiB segment pool and must not be. The current
runtime sizes the data area at 32 MiB and relies on lazy faulting, so a ring is
its own mapping with its own lifetime. `kind == CHANNEL` therefore describes a
region class, not a pool segment, and the pool accounting in section 5 excludes
it. This is a deliberate exception to fixed segment size, made once, for the one
tier that is variable-sized by nature.

`head` and `tail` (here `writeIdx` and `readIdx`) must sit on separate cache
lines. Without that the two sides invalidate each other's line on every operation
and the channel costs more than the work it carries. This is not an optimization,
it is a requirement, and it is the most common way ring buffers are built wrong.

**The current `ringbuf.mx` has exactly this bug.** Its header is 32 bytes with
`writeIdx` at [0:4] and `readIdx` at [4:8], which is the same 64-byte line. Every
publish by the producer and every consume by the consumer ping-pongs that line
between cores. Fixing it is a matter of relocating `readIdx` to offset 64 and
adjusting the header constants, and it should be done before the ring is folded
into the new model rather than after, because it changes the header layout and
with it every offset in the file.

The single-producer single-consumer ring is Lamport's bounded queue. The LMAX
Disruptor is the production version with the padding and the batching. Read
Lamport for the proof and the Disruptor for the layout.

### Channel unification

There are currently three files doing one job. `chan.mx` implements a bounded
queue with sender and receiver task lists. `ringbuf.mx` implements the SPSC ring.
`pipe_channel.mx` is the dispatch layer that routes a channel operation to one or
the other depending on `channelIsPipeBound`, and its own header states the case
plainly: a domain is a thread, both ends share the address space, so the ring is
the one true transport.

The task lists in `chan.mx` are vestigial in the native build. `scheduler_none.mx`
sets `hasScheduler = false` and `hasParallelism = false`, a domain is a single
task, and so a sender list and a receiver list each hold at most one entry. Every
park and every ready in that file is already guarded by `if !hasScheduler`.

The apparatus for a scheduler that can switch tasks is dead weight in the only
configuration moxie runs. Strip it away and `chan.mx` is a bounded queue, at most
one blocked sender, at most one blocked receiver. That is a ring with two waiters.
The two implementations are the same object.

**The unification: one ring, one concurrency class field.**

```
Concurrency class
    LOCAL      one task. No atomics, no wait. Full or empty is an error.
    SHARED     atomics on the indices. Futex or spin to wait.
```

Two classes. There is no multi-writer class, because a channel send end belongs
to exactly one domain and two claimants on it is the state `ownership.md` exists
to make inexpressible. See D5.

The structure is the ring header above plus a class field. `pipe_channel.mx`
already branches on `channelIsPipeBound`; extend that branch to read the class.
One struct, one framing, one close protocol, two concurrency disciplines.

- An intra-domain channel is `LOCAL`. There is no signal because there is nobody
to signal. This is the observation that motivated the unification and it is
correct.
- A cross-domain channel with one sender is `SHARED`.

The memory ordering follows from the class. In `LOCAL` there is one thread, so no
ordering is required at all. In `SHARED` the producer's data writes precede its
index store (release) and the consumer's index store follows its data reads
(acquire), which is what `ringbuf.mx` already does.

**Four things that must be settled before this lands**

1. *Unbuffered channels.* A rendezvous needs the sender to block until a receiver
takes the value. In `LOCAL` with a single task that is an immediate deadlock.
Recommendation: reject capacity 0 at compile time in `LOCAL`, and implement it in
`SHARED` as a capacity-1 ring with an ack frame. Do not leave it unhandled.

2. *select.* Waiting on N channels needs either a task that can park on many
queues, which we do not have, or a multi-wait primitive. On Linux 5.16 and later
`futex_waitv` waits on up to 128 futexes and covers select directly. Confirm the
oldest target kernel before committing, and note that wasm needs its own path.

3. *Direct handoff.* `chan.mx` hands a value straight to a waiting receiver,
bypassing the queue. The ring always goes through the queue. The difference is one
copy, and the copy is cheaper than the second mechanism.

4. *Multi-producer is not a decision.* Ownership forbids it, so the transport
assumes one writer and the compiler must enforce the assumption. The `chan.mx`
sender list exists because it was ported from a runtime with a scheduler, can hold
at most one entry here, and after unification does not exist at all.

**The 32 MiB ring is a symptom of the wrong coupling**

`ringMaxMsg` is 32 MiB and the ring is sized to carry it, so every channel
direction reserves 32 MiB of address space because one message might be that
large. Address space is cheap and lazy faulting means it is not resident, but the
size is a function of the worst case rather than of the queue depth, which is the
wrong variable.

Cap frames at something defensible, 1 MiB, and require anything larger to use a
streaming codec over several frames. The ring then sizes to the depth it actually
needs, typically a few pages. This is a change to the protocol contract, so it
goes with the unification rather than after it.

### Channel ownership and the sender/receiver pair

The geometry intended by `spawn` is one reader on one side and one writer on the
other, never both on either side. A `chan T` is **one ring with two ends**: a sender
and a receiver. A bidirectional relationship between two domains is therefore two
`chan T` instances, one per direction. The runtime already organises its storage
that way, one ring per direction, even though today both hang off a single channel
object.

**Who allocates it.** The declaration allocates the control block and the ring in the
root arena, never a function arena. They cannot be in a frame, because the child
outlives the frame that made the declaration. This is already how the runtime
behaves: `NoteSpawnChan` hands back a root-arena copy of the handle specifically so
the child never reads the parent's frame struct.

**The declaration.** The fate of a channel is a property that governs execution, so
A1 requires it be declared rather than inferred from wherever the handle later
travels. Two forms, one per scope:

```
s, r          := chan T{64}                 // one ring, one domain. LOCAL.
parent, child := edge[In, Out]{64}          // two rings, two domains.
```

The depth in braces is the ring's frame capacity, declared with the channel because
it governs behaviour.

`chan` is the local primitive: one one-way ring and its two ends, both held by the
same domain. There is no bidirectional value anywhere, so nothing is split, nothing
is narrowed, and there is no instant at which a channel's fate is undecided.

`edge` is the spawn-facing bundle. It allocates two rings, one per direction, and
returns two complementary endpoints:

```
type Edge[In, Out] struct {
    Recv Receiver[In]
    Send Sender[Out]
}
```

`parent` is an `Edge[Response, Request]`; `child` is an `Edge[Request, Response]`.
The same generic type with the parameters swapped, which is what makes one
declaration produce two values that are complementary by construction rather than
by convention.

Field names are identical on both sides. `Send` is where you write and `Recv` is
where you read, for the whole life of the value, whichever side holds it. That is
what removes the nominal parent-end/child-end distinction: with roles fixed, the
same end never means different things in different domains.

`Edge` must be a builtin generic. A user-declared struct of the same shape would
compile happily with two senders and produce a ring with no reader, and nothing
would catch it.

**Why not derive the mirror.** `spawn(worker, parent)`, with the compiler handing
the child the complement, is one argument and needs no new type. A1 forbids it: the
child's capabilities would be derived from an argument rather than declared, and A1
says nothing governing execution is inferred. It also collapses the moment a worker
takes two edges.

**Usage.**

```
parent, child := edge[Request, Response]{depth: 64}
done         := spawn(worker, child)

worker := func(ep Edge[Request, Response]) {
    for req := <-ep.Recv {
        ep.Send <- handle(req)
    }
}
```

Four ends, four holders, two in each `Edge`, no overlap. The parent never holds
anything the child holds.

**The transfer.** `spawn` accepts an `Edge[In, Out]`, or a bare `Sender T` or
`Receiver T` for a one-way edge. The child parameter's type must match what is
passed. There is no direction to check for inversion, only agreement, and
transposition is impossible because the parameters are named rather than positional.

The ends are affine: moved once, never copied, and an `Edge` is affine by
composition. Two writers on one ring is therefore unrepresentable rather than
diagnosed. Passing an end to a function moves it, and the callee must return it if
the caller still needs it, which is uniform with the rest of the model where writing
through a parameter is already a compile error.

**Why the child does not allocate it.** Creation has to happen before the child
exists, and the parent has to be able to send the first message. So one party
creates the object and the authority is divided at handover. The storage outlives
both frames, is refcounted two, and is released by whichever side closes last.
Nothing about that requires the child to allocate; it requires the child to receive
its own end rather than a handle shared with its parent.

**Why this satisfies A2.** The ring is a declared transfer region, which A2's
mechanism clause permits. Authority over each direction is a single fact with a
single holder, which is A2's property clause. Both clauses must be stated, because
the mechanism alone permits the shared handle we have today and the property alone
cannot be checked without the type system.

**The types.** `Sender T` writes, `Receiver T` reads, `Edge[In, Out]` is the
generic pair of both. All three are affine: moved once, never copied. Two writers on
one ring is unrepresentable rather than diagnosed, and so is a reader that writes.
An intra-domain queue is the `chan` form with both ends held by one domain, which is
the `LOCAL` class and needs no separate syntax.

### Secret pages

`secalloc.mx` already implements a fifth class: page-guarded arenas holding
secrets, laid out as `[guard PROT_NONE][data PROT_RW, mlock'd][guard PROT_NONE]`,
with a signal handler that wipes on fault.

Secret pages must never enter the compacting heap. If a clean copied a secret into
an ordinary segment, it would defeat the guard pages, lose the mlock, and leave a
copy in memory that nothing wipes. So they are a separate class with three
invariants:

- `PINNED` and never selected as a victim, checked by the same `kind` assertion in
  section 9 that protects channel rings.
- Never scanned. A secret page has no layout descriptor, because nothing inside it
  is reachable from ordinary pointers.
- Zeroed on release, unconditionally, before the page returns to the OS.

An object holding secret material must be allocated entirely inside a secret page,
never split between a secret page and the root heap, because the root heap half
would be copied by the cleaner and the guarantee would be gone. The type system
should eventually enforce this. Until it does, it is a review item.

### Boundaries

| From | To | Representation |
|---|---|---|
| fn arena | same fn arena | offset from arena base |
| fn arena | caller's fn arena | offset, after return-transfer copy |
| fn arena | root heap | handle |
| root heap | root heap | raw address |
| root heap | any fn arena | handle |
| any arena | ring buffer | encoded bytes only, never a pointer |
| any arena | other domain | codec relocation, unchanged |

A handle is `(arena_tag, object_id)` resolved through the arena's id table. This
is the Unix file descriptor design, or Mach ports, applied at an arena boundary.
Handles are the only indirection, and in a well-decomposed moxie program they are
rare, which is the premise of the language.

---

## 5. Data structures

```
Segment
    size            u32     fixed 1 MiB, power of two
    base            u64     mapping address
    kind            u8      FN | ROOT | LARGE | CHANNEL | FREE
    state           u8      LIVE | EVACUATING | FREE
    bump            u32     FN and ROOT
    live_bytes      u32     ROOT only
    owner           u32     domain id. Exactly one per segment, except CHANNEL,
                            which is owned by the channel. See section 4.
    refcount        u32     CHANNEL only. One per end, released by the last.

CHANNEL is listed for bookkeeping but is not allocated from the pool: a ring is a
dedicated mapping of its own size (section 4). The pool holds FN, ROOT and LARGE
segments only.
    generation      u32     incremented on every compaction
    next_in_log     u32     ROOT only

Object header, 8 bytes, prefix of every heap object
    size            u16     up to 65535; larger uses a LARGE segment
    kind            u16     layout table index
    flags           u8      PINNED | EXTERNAL | HAS_ID
    rsv             u8      must be zero

No id field. No owner field. No forward field. Section 6 says where each went.

Layout table     per type: count, then pointer-bearing offsets, then size
Stack maps       per safepoint: bitmap of live pointer slots per frame
Root set         globals, thread-locals, spilled registers, per safepoint
Id table         address -> id. Sparse. Only objects with HAS_ID set. ROOT only.
Id index         id -> address. The same table read backwards, or a second map.
                 Diagnostics and handle resolution. Never a liveness oracle.
```

8 bytes and not 12: `size` is u16 because anything larger takes the LARGE path,
`kind` is u16 because 65535 layout kinds is ample, and the remaining two bytes are
flags and reserved. Add a field only by re-running the size test in section 17.

---

## 6. Object encoding: the WiscKey import

### 6.1 The problem being solved

The previous draft carried a 24-byte header and a 16-byte index entry per object:
40 bytes of metadata per allocation. Measured against payload:

| payload | metadata | total | overhead |
|---|---|---|---|
| 16 | 40 | 56 | 250% |
| 32 | 40 | 72 | 125% |
| 64 | 40 | 104 | 62% |
| 256 | 40 | 296 | 16% |
| 4096 | 40 | 4136 | 1% |

For a language whose heap is mostly strings, nodes and map entries, this is not
acceptable. Small objects are the common case and they are the case the metadata
destroys.

### 6.2 What WiscKey and Badger actually teach

WiscKey separates keys from values in an LSM tree. The LSM stores key plus a
pointer into an append-only value log, and compaction therefore moves only keys,
which are small, instead of values, which are most of the bytes. BadgerDB is the
Go implementation and is easier to read than the paper.

The structural insight is not "add an indirection". It is:

> **The index and the payload have different densities and different lifetimes.
> Store them separately, size the index for the index, and keep the payload free
> of anything that only some objects need.**

Everything below follows from that sentence.

### 6.3 What transfers, and what does not

Transfers:

- **Sparse identity.** Only objects that a handle names need an id. Assign on
  demand, set HAS_ID, store in a side table. The overwhelming majority of objects
  are never named by a handle and should pay nothing for the possibility.
- **Separate storage for metadata.** Identity lives in a side table keyed by
  address, not in the object. Same reason Badger keeps value pointers in the LSM
  rather than in the log.
- **Different lifetimes get different homes.** `owner` is a property of the
  container, not of the object, so it belongs on the segment.

Does not transfer:

- **The indirection.** WiscKey adds a pointer chase because a disk read is
  already expensive, so the chase is free in relative terms. In DRAM the chase is
  a cache miss of about a hundred cycles, and avoiding exactly that is the point
  of raw addresses. The identity table must never appear in the dereference path.
- **Index-based liveness.** WiscKey's value-log GC asks the LSM "is this value
  still live", so liveness is a lookup. Moxie marks from roots, which is precise
  and needs one source of truth. Using the id index as the liveness oracle would
  require the heap and the index to be perfectly synchronised at all times, and
  that is a bug class this design does not otherwise have. **Prohibited.**
- **Write amplification.** There are no erase blocks and no levelled compaction in
  DRAM. The analogous cost here is bytes copied, which is already the cleaner's
  cost model.

### 6.4 The three removals

**Owner moves to the segment.** A segment holds objects from exactly one domain,
because cross-domain references are copies rather than pointers (section 11). So
the domain is a property of the segment. This removes 4 bytes per object and
costs nothing, because single-domain segments were already an invariant.

**Identity becomes sparse.** An object gets an id when something first takes a
handle to it, not at allocation. The id table is `address -> id`, and its inverse
is built lazily or maintained alongside for handle resolution. Objects with
HAS_ID pay 8 bytes in a side table; all others pay one bit.

Consequence worth stating: `owner`-based accounting queries ("what does spawn 41
own") now work at segment granularity rather than object granularity. That is the
query anyone actually asks, and it is O(segments) instead of O(objects).

**Forwarding becomes an overwrite.** During evacuation, after copying an object,
write the destination's segment-relative offset into the object's first four
bytes. The source is dead at that point and its `kind` is already recorded at the
destination, so nothing needs the source header again. This removes 8 bytes.

Requires a strict ordering: **all victims fully evacuated before any fixup
begins**, and victim segments are in state EVACUATING for the whole window. The
invariant in section 9 guarantees it. Section 10 explains why incremental mode
must therefore step at segment granularity.

### 6.5 The resulting header

| payload | metadata | total | overhead |
|---|---|---|---|
| 16 | 8 | 24 | 50% |
| 32 | 8 | 40 | 25% |
| 64 | 8 | 72 | 12.5% |
| 256 | 8 | 264 | 3.1% |
| 4096 | 8 | 4104 | 0.2% |

### 6.6 Deferred: slab-side metadata

If density on sub-32-byte objects ever becomes the bottleneck, the next step is
the jemalloc and mimalloc layout: segregate segments by size class, divide into
equal slots, and move `kind` and `flags` into per-slab byte arrays with a used
bitmap. That reaches about 2.1 bytes of metadata per object.

Cost: two object layouts in the marker, more partial segments, and a size-class
polymorphism that has to be tested twice.

**Do not do this in the first implementation.** It is documented here so the
decision is made once and deliberately rather than discovered in a profile six
months from now. The 8-byte header is the design of record; the slab layout is a
recorded option with a trigger condition (sub-32-byte objects exceeding some
fraction of allocations) to be decided by measurement.

---

## 7. Allocation

```
alloc_fn(size):
    if fn_arena.remaining < 8 + size:
        if 8 + size > LARGE_THRESHOLD:          # 65535, set by u16 size
            return alloc_large(size, FN)
        fn_arena = new_fn_arena()
    p = fn_arena.bump
    write_header(p, size, kind, 0)
    fn_arena.bump += align(8 + size)
    return fn_arena.base + p                    # offset, materialised as address

alloc_root(size, kind, domain):
    if current_root_segment.remaining < 8 + size:
        current_root_segment = next_root_segment()
    o = current_root_segment.bump
    write_header(o, size, kind, 0)
    current_root_segment.live_bytes += 8 + size
    return address(o)

take_handle(o):                                 # first handle to o
    if not flags(o) & HAS_ID:
        flags(o) |= HAS_ID
        id_table.insert(address(o), next_id())
    return (arena_tag(o), id_table.get(address(o)))
```

No free list, no coalescing, no external fragmentation by construction. The only
fragmentation is dead bytes inside root segments, and that is the cleaner's job.

`alloc_large` maps a dedicated segment, marks it LARGE and PINNED, frees it whole.
Large objects never move.

`next_id()` is a monotonic counter under the domain's own lock, not a global
atomic, because ids are only unique per domain. See section 11.

---

## 8. Return as transfer

`ownership.md` says return values are relocated into the caller's arena before
release, and that the relocation is the proof that ownership moved. That stays.

Only the implementation changes. Both source and destination are offset addressed,
so the copy is `memcpy` plus offset arithmetic with one differing base register.
There is no fixup pass. If the current `ArenaRelease` on return walks the object
adjusting pointers, that walk is removed.

Resolved: the return path is a structural deep copy. `ir_deepcopy.mx` emits the
walk, saves the codec buffer's data pointer across `FnArenaPop`, and restores its
position afterwards. So this ships alone as phase 1 and is a win independent of
everything else. Measure it on the compiler's own bootstrap, which is the heaviest
return-copy workload in the tree.

---

## 9. Compaction: the Sprite LFS cleaner

Runs on one domain's root heap. Never touches function arenas.

```
compact(domain):
    victims = root segments of domain where kind == ROOT
                  and dead_bytes / size > THRESHOLD (0.5)
              ordered by dead ratio descending
    if victims empty: return
    if sum(live of victims) > free_nonvictim_space:
        drop victims until it fits              # never overcommit

    for v in victims: v.state = EVACUATING

    # phase 1: mark from roots, precisely
    mark_stack = root_set(domain)
    marked = empty
    while mark_stack:
        o = pop
        if o in marked: continue
        marked.insert(o)
        for off in layout(kind(o)).pointer_offsets:
            if *(o+off): mark_stack.push(*(o+off))

    # phase 2: evacuate into non-victim segments only
    for o in victims where marked:
        dst = alloc_silent_into_nonvictim(size(o), segment(o).owner, kind(o))
        memcpy(dst, o, 8 + size(o))
        if flags(o) & HAS_ID:
            id_table.move(address(o), dst)      # hash probe only when flagged
        write_u32(o, offset_within_segment(dst))  # overwrite, forwarding
    # every live object in every victim is now forwarded

    # phase 3: fixup
    for slot in root_set(domain):       *slot = resolve(*slot)
    for o in all_live_objects(domain):
        for off in layout(kind(o)).pointer_offsets:
            *(o+off) = resolve(*(o+off))
    for h in handle_table(domain):      h.target = resolve(h.target)

    # phase 4: reclaim
    for v in victims:
        v.generation += 1
        release(v)

resolve(p):
    if p null: return p
    s = segment_of(p)
    if s.state != EVACUATING: return p
    off = read_u32(p)
    dst = s.base + off
    assert dst is inside a non-victim segment, "corrupt forward pointer"
    return dst
```

### The invariants that prevent every known bug

**Victims are evacuated only into non-victim segments.** Never victim to victim.
Victim-to-victim moves create forwarding chains, chains require path compression,
and path compression is where every implementation of this has produced a bug.

**Only ROOT segments are ever victims.** FN segments are released on pop, LARGE
segments are pinned and freed whole, CHANNEL segments are refcounted and released
by their owners. Assert `kind == ROOT` in victim selection rather than relying on
the dead-ratio threshold, which a nearly-empty FN or CHANNEL segment would
otherwise satisfy.

**Every live object in a victim segment is forwarded before phase 3 begins.**
This is what makes `resolve` a single comparison instead of a search. If phase 2
can be interrupted before completion, the state is inconsistent and a pointer into
a half-evacuated victim cannot be resolved. Section 10 resolves this by making the
step unit a whole segment.

**All live objects in a victim are marked before any evacuation.** Marking reads
`kind`, and evacuation destroys the source header. Order is not optional.

### Victim selection, following LFS

Two bands, as Sprite LFS uses. Segments above 75% live are cold and left alone.
Segments below 25% live are cleaned first. The middle band is cleaned when the
free pool falls below a watermark. Cleaning a nearly-full segment costs more than
it recovers, which is why a threshold exists rather than "always clean the worst".

### Determinism

Victims ordered by (dead ratio descending, segment id ascending); objects within a
victim by offset ascending; the mark work list is FIFO seeded from `root_set` in a
defined order.

---

## 10. Quiescence: BEAM-style self-collection

Lemma L1 in `spawn.md` states there is no scheduler and no task switching within a
domain. That is why this is cheap, and the implementation must rest on it.

With no preemption inside a domain, **compaction is self-compaction at a
safepoint**. No thread ever stops another. This is the BEAM property: each process
collects its own heap and no global pause exists.

```
request_compaction(domain):
    domain.compact_requested = 1

safepoint(domain):                  # function entry, channel op, explicit
    if domain.compact_requested:
        domain.compact_requested = 0
        compact(domain)             # this thread, this domain
```

Safepoints are the points that already publish stack maps. The check is a load and
a branch. Do not add polling at loop backedges: loop bodies do not allocate across
iterations in this model.

The channel-operation safepoint is at **entry**, before any bytes are encoded or
copied. A clean must never run between encoding a message and committing it to the
ring, because a custom codec is free to read the sender's heap while producing
the encoded form, and compacting underneath it would move the data mid-encode.

### Incremental mode steps at segment granularity

This is the interaction that section 6.4 forces. Because forwarding is stored by
overwriting the source, a partially evacuated victim is unreadable: some objects
are forwarded and some are not, and `resolve` cannot distinguish them.

The resolution is that the pause unit is **one segment**, not a byte budget. Take
one victim, mark only what is reachable from that victim's live objects plus the
roots, evacuate the whole segment, fix up, release, yield. Repeat.

This is also how NILFS2's cleaner behaves and for the same reason. It makes the
pause bounded by 1 MiB of live data, which is a real bound rather than a hoped-for
one, and it keeps the invariant exact.

Do this in phase 7, not phase 6. Whole-heap compaction first.

### Triggers

- `dead_ratio(root) > 0.6` checked at safepoint against a running counter.
- Explicit `runtime.Compact()`, for tests and pause-sensitive code.
- Domain idle: blocked in `select` with no ready case, compact before parking.

---

## 11. Ownership and identity

### Ownership is per segment

A segment has exactly one owner. Objects inherit it. Cross-domain references do
not exist as pointers, because the codec copies at the channel boundary, so an
object never belongs to a domain other than the one that allocated it.

The one exception is the channel ring buffer, which has two owners: writer and
reader, each owning a disjoint index and a disjoint half-open region. Section 4
sets out why that is disjoint authority rather than shared ownership, and why it
does not weaken the axiom.

A CHANNEL segment is therefore not released by domain death. It is refcounted, one
per end, and released by whichever side closes last. If a domain dies while a
channel it shares is still open at the other end, the channel survives, which is
correct, because the other end still owns its half and its bytes are still
readable.

Debug assertion: if any code path produces an object whose domain differs from its
segment's owner, that is a bug, and it should trap rather than create a shared
segment. Do not build shared-segment machinery for a case the language makes
impossible. If the language ever permits it, this section changes first.

Domain death releases every segment the domain owns, without tracing. That is the
arena win extended to the root heap, and it is the reason per-segment ownership is
worth the constraint.

### Identity is per domain and sparse

```
id = (epoch << 40) | counter
```

`counter` is monotonic per domain under the domain's own lock, not a global atomic.
`epoch` increments on process start and is written alongside any persisted id, so
ids from a previous run cannot collide with ids from this one.

Ids are assigned on first handle, not at allocation. An object that is never
referenced across a boundary never has one, and the id table stays proportional to
the number of boundary-crossing objects rather than the number of objects.

The id table is keyed by address because that is what a handle needs to resolve to
and what the cleaner needs to update. It is a good structure for that job and a
bad structure for anything else. **It is not a liveness oracle.** Liveness comes
from marking roots. If the cleaner ever consults the id table to decide whether an
object is alive, the design has acquired a second source of truth and a
synchronisation requirement it does not need.

---

## 12. The alternative considered and rejected: copy-on-write

LMDB and ZFS solve the same problem without moving anything. Update never
overwrites; it writes a new page and updates the parent pointer, so a page becomes
free the moment nothing references it. Reclamation is by generation rather than
relocation, and no pointers are ever rewritten.

Attractive because it deletes section 9 entirely.

Rejected because it is a poor fit for mutable hot data. Every write allocates, so
write amplification equals write volume, and a workload mutating a small structure
repeatedly churns pages proportional to the mutation rate rather than the live set.
Log-structured cleaning costs proportional to live data moved, which is bounded by
the heap. Copy-on-write costs proportional to writes, which is unbounded.

There is a case for a third tier later: an immutable snapshot heap for data never
mutated in place, reusing COW and sharing section 9's segment structure. Not now.

---

## 13. Cost calibration from RAMCloud

Rumble and colleagues built this and measured it. The parts that constrain us:

**Cleaning costs CPU proportional to live bytes moved, not to bytes freed.** Victim
selection therefore matters more than cleaning frequency. Picking high-dead-ratio
segments is the difference between a cleaner at a few percent and one that eats the
machine.

**Space overhead is roughly one segment per log plus destination space during
cleaning.** Budget free segments at about 20% of the root heap. Below that the
cleaner cannot find a destination and compaction stalls while the heap keeps
growing.

**The cost curve turns sharply as utilisation rises.** At low utilisation cleaning
is nearly free, at 90% it is brutal. That is where the 0.6 trigger and the
free-segment watermark come from.

**Throughput is fine.** Their conclusion is that log-structured memory in DRAM is
competitive with a conventional allocator once the cleaning policy is tuned. The
concern is tuning, not feasibility.

---

## 14. Compiler work required

Cannot be done in the runtime alone.

1. **Layout descriptors.** For every type, the array of pointer-bearing offsets,
   plus size and alignment, in the module type section. Derivable from the type
   database already used by `ir_descriptors`.

2. **Stack maps.** Per safepoint, a bitmap of pointer slots in stack frames and
   spilled registers. Safepoints are function entry, call sites, channel operations,
   explicit yields.

3. **Root set enumeration.** Globals and thread-locals per domain, addressable by the
   runtime.

4. **`raw` field attribute.** Escape hatch for byte buffers, hash outputs and cipher
   state that must never be scanned.

5. **Offset addressing for function arenas.** Pointer into a function arena is
   `arena_base + offset`, with the base in a dedicated register or frame slot. This
   is the change that removes the fixup pass.

6. **Compact `kind` encoding.** Layout indices must fit u16. If the type database
   emits more than 65535 distinct layouts, partition by module or widen the field
   and re-run the size test.

Resolved: `%fn_arena` already exists as an SSA value produced by `FnArenaPush`,
and `arenaAllocRaw` bumps `a.regions[active].off` inside `a.regions[active].base`.
The arena handle therefore exists and the base-within-region does not. The work is
to expose `regions[active].base` as a second SSA value, `%fn_base`, set at push and
reloaded whenever a region is chained. That is the entirety of the codegen change
for offset addressing; everything else in the function tier is unchanged.

---

## 15. Decisions of record

Every item below was open in an earlier draft. All are now decided, with the
reasoning attached, so the implementer does not relitigate them.

**D1. Segment sizes.** Function chunks are 64 KiB, matching the existing
`FnArenaPush(..., 65536)` call in `ir_deepcopy.mx`. Root segments are 1 MiB. Two
pools, both power of two, both fixed. Function chunks are transient and small; root
segments want cleaning granularity, and 1 MiB gives a pause bound comfortably under
a millisecond.

**D2. Cache-line layout.** 128-byte stride, which is safe on both 64-byte and
128-byte line architectures. Producer line holds `writeIdx` and `sentFrames`.
Consumer line holds `readIdx` and `recvFrames`. A third line holds `size`, `closed`,
`recvSleeper`, `sendSleeper`, `refcount`. Header is 384 bytes, data starts at 384.
Nothing that both sides write in a hot path shares a line. The current
`ringbuf.mx` layout, with both indices in the first eight bytes, fails this.

**D3. Frames are chained, the ring stays small.** No fixed frame cap. A message
larger than the contiguous free space in the ring is written as a chain of chunks
and reassembled by the receiver into its own arena. Maximum message size is
bounded by receiver memory, not by ring size, and `ringMaxMsg` is deleted.

This is what removes the 32 MiB reservation. It is also mandatory on wasm, where
`memory.grow` never returns memory, so a 32 MiB ring per direction is permanent
address space that can never be reclaimed. The cost is a two-word frame header and
a reassembly buffer on the receiving side, which the codec path needs anyway.

**D4. Ring size.** 128 KiB natively, 64 KiB on wasm. Independent of message size,
because frames chain. Address space stops being a function of the worst case, and
on wasm it stops being permanent.

**D5. One writer, by construction.** A channel's send end belongs to exactly one
domain, so there is exactly one writer to `writeIdx` and nothing beyond the release
store that publishes each frame is needed. Two claimants on one send end is the
failure `ownership.md` exists to make inexpressible. Multi-producer is not a
feature being declined; it is a state the language cannot represent.

This holds only because of D19. The compiler does not currently enforce it, and the
tree says so in a comment. D19 is the prerequisite, not a follow-up.

The load-bearing requirement falls on the compiler, not the runtime: transferring
the same send end to two `spawn` calls must be a compile error, because that is the
only path to two writers. This is a conformance test, not a runtime check.

Because there is one writer, no COMMITTED flag is needed. Each chunk becomes
visible when the index advances past it, and chunks are reassembled by following
CONTINUATION until FINAL. The index advance is the commit.

**D6. Capacity zero.** Compile error in `LOCAL`, because a rendezvous with a single
task is a guaranteed deadlock. Legal in `SHARED`, implemented as capacity 1 plus an
ack frame.

**D7. select.** `futex_waitv` where the kernel provides it, Linux 5.16 and later.
Below that, polling with exponential backoff capped at 1 ms. Both correct, one
faster, both documented and both tested. The wasm backend keeps its existing path.

**D8. Large object threshold.** 65535 bytes, the maximum of the u16 size field.
Larger objects take a LARGE segment, pinned and freed whole. Revisit only if the
phase 0 histogram shows a meaningful population just above 64 KiB.

**D9. Slab-side metadata.** Deferred. Trigger: more than 40% of allocations at or
below 32 bytes. Measure in phase 0 and decide once.

**D10. Cleaning policy.** Clean a segment when its dead ratio reaches 0.5. Start a
cycle when the root heap's overall dead ratio reaches 0.6, or when free segments
fall below 20% of the pool. The cold band above 0.75 live is never cleaned. All
four numbers live in one constants block and nowhere else.

**D11. Identity width.** `id = (epoch << 40) | counter`. 24 bits of epoch gives
16.7 million process restarts before wrap. 40 bits of counter gives a trillion
objects per run. Per domain, not global.

**D12. Return path.** Flat copy with offsets. No structural walk. Phase 1.

**D13. `-no-main-arena`.** No function arena; allocation goes to the ROOT heap. The
bootstrap becomes the first consumer of the new path rather than a special case
bolted onto it.

**D14. Function arena base.** `%fn_arena` already exists as an Arena pointer. Add
`%fn_base` as a second SSA value, set at push and reloaded on region chaining. That
is the whole codegen change for offset addressing.

**D15. The arena inventory.** Phase 0a. Every function in `alloc.mx` is classified
as subsumed by the function tier, subsumed by the root heap, or genuinely needed
and missing from this document. Anything found in the third category is a gap in
this document, not a feature to preserve.

**D16. The compactor is extracted, not written.** `relocate.mx` is already a
three-pass mark-compact arena relocator: mark from roots, forward by allocating
destinations in the caller's arena, then compact and fix up pointer fields, with
region locality determined by scanning the arena's flat region table. `ArenaAllocTD`
additionally copy-compacts sovereign data arenas on region exhaustion, with an
allocation map whose referrers walk object interiors. Between the two, the
algorithm in section 9 already exists in the tree. The work is to point it at the
ROOT heap, replace the trigger with D10, and delete the sovereign coupling. Do not
write a second compactor, and do not merge the two existing ones until one of them
is provably unused.

**D17. wasm is in scope for phase 2.** Phase 2 ships both backends together and
there is no native-only intermediate. `mem_wasm.mx` supplies
`sysMap` and `sysMapShared` from `memory.grow`, which is contiguous, zeroed and
page-aligned but never returned to the host. Consequences: the segment pool is the
*only* reclamation on wasm, so compaction is not an optimization there but the sole
mechanism for reuse; segments must be smaller, 64 KiB rather than 1 MiB, because
waste is permanent; and `sysUnmap` returns to the pool rather than to the host.

This is not theoretical. The runtime documents a failure where the NIST P-521
generator table chained a 64 MB region per overflowing frame and `memory.grow`
failed at 4 GiB inside `nistec.buildP521GeneratorTable` before the program had
produced a byte. A recyclable pool with a compacted root heap is the fix, and it is
the strongest available argument for this design on that platform.

Cross-thread channels on wasm use a `SharedArrayBuffer` with `Atomics.wait`, not
futex. The `LOCAL` class has no atomics there either, for the same reason it has
none natively.

**D18. Secret pages are their own class.** See section 4. Never compacted, never
scanned, zeroed unconditionally on release.

**D19. Channel ends are linear, and the compiler does not yet enforce it.** Grep
result, recorded here because it is the one finding that contradicts A2 in the
tree as it stands.

`ir_call.mx` at `emitSpawnCall` carries the comment **"Channels are shared: the
child gets the same ring"** and passes the caller's handle through unchanged. There
is no move tracking anywhere in the compiler: a search for move, transfer,
consume, escaped or twice diagnostics returns nothing but unrelated matches.
Directional channel types do exist (`TCSendOnly`, `TCRecvOnly`, with `Dir` on
`TCChan` and parsing at `ir_typecheck.mx:2491`), but narrowing a bidirectional
channel to a direction does not consume the source.

Therefore a program can pass one send end to two `spawn` calls today and obtain two
writers on one ring. Under A2 that is not a bug in the ring, it is a hole in the
language, and it predates this document.

The enforcement is a type feature, not a pass bolted on afterwards, because the
machinery exists: `TCChan` already carries a `Dir`, and `TCSendOnly` and `TCRecvOnly`
are already parsed.

1. `s, r := chan T{d}` binds one `Sender T` and one `Receiver T` over a single
   one-way ring. `parent, child := edge[In, Out]{d}` binds two complementary
   `Edge` values over two rings. There is no bidirectional value, so there is nothing
   to split and no second-split case.
2. `Edge` is a builtin generic whose two fields are the complementary ends. A
   user-defined struct of the same shape does not satisfy `spawn`.
3. All three types are affine. Copying any of them is a compile error, which is what
   makes two writers unrepresentable rather than diagnosed.
4. `spawn` accepts an `Edge[In, Out]`, a `Sender T` or a `Receiver T`, and the child
   parameter's type must match. There is no inversion to check, only agreement.
5. Writing to a `Receiver T` or reading from a `Sender T` is a compile error, whether
   reached directly or through a field. The check is on the operation.
6. The storage is allocated in the root arena at the declaration, so the pair is a
   division of authority over existing memory, never an allocation the child
   performs.
6. The storage is allocated in the root arena at the declaration, so the pair is a
division of authority over existing memory, never an allocation the child performs.

Rules 1 and 3 together are what make the ring single-writer by construction rather
than by convention, and they are the axiom-required form: authority over a
direction is a fact with exactly one holder, and the compiler is what enforces it.
Rule 5 is the same statement seen from the reader's side. Rule 2 is what keeps the
pairing a fact the compiler can check rather than a convention the programmer keeps.

**D20. The type is called `Edge`.** A1's own corollary settles it: "a capability that
crosses a boundary the topology does not declare is an undeclared edge; a declared
edge with no capability is a dead channel." The doctrine already identifies an edge
with a channel, so naming the bundle `Edge` makes the type the structured form of
the axiom's word rather than a metaphor for it.

The collision is real and should be recorded. `_mxc_stage4` already uses `edge` as a
loop variable for type-dependency graph edges in `ir_scope.mx`, `ssa_builder.mx` and
`ir_emit.mx`, and `type_db.mx` has `typeDBRemoveEdge`. Those are a different graph.
There is no `Edge` type today, so the identifier is free, and capitalisation
distinguishes them at the point of use. Anyone reading a function that ranges over
`x.Edges` and also handles an `Edge[In, Out]` should know they are unrelated.

If that proves confusing in practice, `Link` is the fallback and means the same
thing. Do not pick a third word; two candidates are enough and the doctrine prefers
the first.

**Decided out of scope.** Persistent root heap: `epoch` exists so it can be added
without id collision, and WiscKey becomes relevant when it lands, because
page-granular writes restore write amplification. Sub-segment incremental cleaning:
ruled out by the overwrite-forwarding scheme in 6.4. Concurrent cleaning: one
domain, one thread, one cleaner, always.

---

## 16. Work breakdown

Ordered, with the dependency that forces each position. Every phase ships alone.

**Phase 0a. Arena inventory.** This is the first thing run, and it changes no code.
Classify every function in `alloc.mx` per D15: subsumed by the function tier,
subsumed by the root heap, or genuinely needed and missing from this document.
Exit criterion: nothing unclassified, and every gap found is folded back into this
document before any code changes. Phase 2 is blocked on this, because until the seven
overlapping mechanisms are named, "replace the allocator" has no defined boundary.

**Phase 0b. Measurement.** No dependencies; runs alongside 0a.
Counters per function and per domain; push/pop balance assertion; leak checker at
domain exit; allocation size histogram; peak function-arena usage per function;
channel count and total ring address space; reproduce the relay and marmot growth
curves.
Exit criterion: the histogram exists. D8, D9 and the phase 4 header size are all
read from it, so phase 4 cannot fix the encoding without it.

**Phase 0c. Channel linearity.** Needs nothing; this is a live soundness hole and
it goes first.
Implement D19: `chan` and `edge` declaration forms, `Edge` as a builtin generic,
affineness on all three types, use-after-move diagnosed, `spawn` accepting `Edge` or
a bare end, and operation-level role checking.
Exit criterion: a program that passes one `Edge` to two `spawn` calls fails to
compile with a file:line; a transposed `Edge` fails to compile; the relay and marmot
trees are migrated to `edge` and narrowed ends; both still pass their suites. No
runtime change at all, which is the point, because the ring is already single-writer
once the language stops permitting otherwise.

**Phase 1. Flat return copy.** Independent.
Replace the structural walk with a flat copy per D12.
Exit criterion: bootstrap time improves measurably and the full suite passes.

**Phase 2. Segment allocator.** Needs 0a and 0b.
Function chunks of 64 KiB, root segments of 1 MiB natively and 64 KiB on wasm, two
pools, arenas become bump slices. Both `sysMap` backends land together, because D17
puts wasm in scope here. No compaction. Header unchanged at this point.
Exit criterion: zero mmap per allocating call in steady state on native, and a
wasm build of the self-hosted compiler completes where it currently fails.

**Phase 3. Layout descriptors and stack maps.** Needs 2.
Emit them and use them for nothing. Verifier walks the ROOT heap asserting every
pointer offset holds null or a valid header.
Exit criterion: verifier passes on the suite and under relay load.
This is the gate. Nothing downstream starts until it is green.

**Phase 4. Compact encoding.** Needs 3, and needs the phase 0b histogram.
8-byte header, owner to segment, sparse identity, `%fn_base` offsets per D14. The
distribution of allocation sizes decides whether the 8-byte header is sufficient or
whether D9's slab layout fires, so the encoding is not fixed until the histogram
exists. Everything else in phase 4 can proceed without it; the size decision cannot.
Exit criterion: `sizeof(header) == 8`; id table empty on a handle-free workload;
verifier still green; metadata ratio reported against the 24-byte baseline; and a
recorded decision on D9 with the histogram that justified it.

**Phase 5. Extract the compactor.** Needs 4.
Lift the existing sovereign-data-arena compaction onto the ROOT heap per D16,
retrigger on D10, delete the sovereign coupling. Explicit `runtime.Compact()` only.
Exit criterion: shadow model agrees over 1000 randomised sequences, and the
victim-to-victim and forward-pointer assertions never fire.

**Phase 6. Channel unification.** Needs 2; independent of 3 through 5.
Ring relayout per D2; frame chaining per D3; ring size per D4; `LOCAL` and
`SHARED` classes; single writer enforced by the compiler per D5; `chan.mx`'s queue replaced by the ring; task lists deleted; dispatch on
class rather than on pipe-bound; capacity zero per D6; select per D7.
Exit criterion: `LOCAL` makes no syscalls under strace; capacity zero is a compile
error in `LOCAL`; `SHARED` passes the contention test; select works on 5.16 and
degrades correctly below it.

**Phase 7. Automatic and incremental cleaning.** Needs 5.
Dead-ratio trigger at safepoint; one victim per step, whole-segment evacuation.
Exit criterion: 24-hour soak, flat RSS, maximum pause under 2 ms.

**Phase 8. Handles.** Needs 5.
Raw cross-arena pointers replaced by handles.
Exit criterion: verifier proves no raw pointer crosses an arena boundary.

Critical path is 0a, 2, 3, 4, 5, 7. Phases 0b and 0c run in parallel and should
start immediately: 0c because it closes a live soundness hole, and 0b because phase
4 cannot fix the encoding without the histogram. Phase 6 is independent of 3 through
5 and should start alongside 2, since it has the most immediate user-visible effect.

---

## 17. Acceptance tests

1. **Header size.** `sizeof(header) == 8`, asserted at compile time.
2. **Metadata ratio.** Report bytes of metadata per payload byte on the reference
   workload; it must beat the 24-byte-header baseline by a factor recorded at phase 4.
3. **Sparse identity.** A workload taking no handles leaves the id table empty.
4. **Identity survives compaction.** An object with HAS_ID resolves correctly through
   its handle after the owning segment is cleaned.
5. **Overwrite safety.** No object in a live segment has a corrupted first four bytes
   after a compaction; verified by the shadow model.
6. **Shadow model equivalence.** Random sequences, deep graph compare after each
   compaction.
7. **No fixup for function arenas.** A returned structure's offsets are identical
   before and after the caller compacts.
8. **Victim isolation.** No object in a non-victim segment changed address.
9. **Forwarding terminality.** Debug assertions that `forward` never points into an
   EVACUATING segment and that no live object in a victim is unforwarded at fixup.
10. **Leak balance.** Every segment is LIVE, or FREE and pooled, at domain exit.
11. **Determinism.** Same seed, same segment ids and offsets.
12. **Pause bound.** Maximum pause reported; incremental mode respects the bound.
13. **Long soak.** 24 hours of relay, flat RSS.
14. **Hostile maps.** A deliberately wrong layout descriptor is caught by the verifier
    rather than by a segfault. This tests the test.
15. **No liveness from the index.** A static check or code review gate that the
    cleaner never reads the id table to decide liveness. Record it as a review item
    because it cannot be tested.
16. **Ring isolation.** Victim selection asserts `kind == ROOT`; a deliberately
    emptied CHANNEL segment must not be chosen even though its dead ratio is 1.0.
17. **Index authority.** Debug assertions in the SPSC path that the writer never
    writes `head` and the reader never writes `tail`.
18. **Drain on close.** Closing one end while the other is mid-read releases the
    segment only when the second end closes; refcount reaches zero exactly once and
    the release runs once.
19. **Ring cache lines.** Compile-time assertions that `&head` and `&tail` differ
    by at least one cache line and that neither shares a line with `data`.
20. **No pointers in rings.** Encode a message whose payload is pointer-free and
    assert the ring bytes contain no value that resolves inside any heap segment.
21. **LOCAL class issues no syscalls.** An intra-domain channel under strace makes
    no `futex`, `poll` or `read` calls.
22. **LOCAL full is an error.** Sending past capacity in `LOCAL` traps rather than
    blocking, because blocking is a guaranteed deadlock.
23. **SHARED under contention.** One sender and one receiver on separate threads,
    10^6 frames, every frame received exactly once and in order.
24. **select across three channels** wakes exactly once, on the channel that became
    ready, with no spurious frame consumed.
25. **Capacity 0.** Rejected at compile time in `LOCAL`; in `SHARED` the sender does
    not complete until a receiver takes the frame.
26. **Single-writer conformance.** A program that transfers one send end to two
    `spawn` calls fails to compile. This is the whole of D5 and it is a compiler
    test, not a runtime one.
27. **Chained frames reassemble.** A message several times the ring size arrives
    byte-identical, and so does one that straddles the wrap point.
28. **Affine ends.** Copying a `Sender T` or `Receiver T`, in a call, an assignment
    or a struct field, is a compile error.
29. **No transposition.** `spawn` given an `Edge[Request, Response]` where the target
takes `Edge[Response, Request]` is a compile error, and this is the case two
positional channel arguments would have accepted silently.
30. **Edge is builtin.** A locally declared struct with `Send` and `Recv` fields does
    not satisfy a `spawn` parameter of type `Edge[In, Out]`.
31. **Role is enforced on the operation.** Reading from a `Sender T` and writing to
    a `Receiver T` are both compile errors, including through an `Edge` field.
31. **Local cannot cross.** A `chan T` captured by a spawned closure or reachable
    from a `spawn` argument is a compile error.
32. **Storage outlives the frame.** A channel declared in a function that returns
    before the child is signalled still works, because the control block is in the
    root arena. This is the `MarkChildPipeChannel` start-up crash from the relay
    turned into a test.

---

## 18. What this does not give you

- **No protection against hostile code.** A program cannot attack itself here.
- **No cross-domain compaction.** Per domain, on the domain's own thread, always.
- **No persistent heap.** `epoch` exists so persistence can be added without id
  collision, not because it is in scope.
- **No CHERI dependency.** CHERI would turn use-after-free from silent corruption
  into a trap, which is real and orthogonal. Do not depend on it.

---

## 19. Risks, in order

1. **Stack maps wrong somewhere.** Mitigated by the phase 3 verifier and the
   one-phase gap. Never collapse that gap.
2. **Function-arena base.** Resolved by D14: `%fn_arena` exists, `%fn_base` is the
   addition. Not a risk any more, retained here so the decision is traceable.
3. **Encoding change after the cleaner exists.** Mitigated by staging the encoding
   at 4, before the cleaner at 5. Do not reorder these two.
4. **Incremental cleaning breaking the forwarding invariant.** Mitigated by making
   the step unit a whole segment. If anyone proposes byte-budget stepping, the
   forwarding scheme must move to a side table first, at the cost of the 8 bytes.
5. **Intrusive structures.** Linked lists where the pointer is the identity cannot
   use handles cleanly. Enumerate before phase 8; each is a design decision.
6. **`kind` overflow past 65535 layouts.** Widen and re-run the size test, or
   partition by module. Cheap to detect, embarrassing to discover late.
7. **External pointers.** Anything handed to C, the kernel or a device must be
   PINNED. Add `pin()` and make it explicit at every FFI boundary.
8. **The `-no-main-arena` build tag.** Resolved: it gives package main no function
   arena and no return encode, for bootstrap. Under the new model that mode
   allocates straight into the ROOT heap, which is what the tag already implies and
   which makes the bootstrap the first consumer of the new path rather than a
   special case bolted onto it.
9. **Binary size.** Stack maps at every call site are not free. Measure at phase 3;
   if unacceptable, emit per function region at the cost of coarser roots.
10. **Cleaner stalls.** Guarded by the free-segment watermark. If the watermark is
    ever violated the heap grows without bound and the failure looks like a leak.
    Assert it.
11. **Identity table used as a liveness oracle.** The single prohibition in this
    document. It would introduce a synchronisation requirement between heap and
    index that no other part of the design has, and the resulting bugs would be
    intermittent and expensive.
12. **False sharing on the ring indices.** `writeIdx` and `readIdx` must be on
    separate cache lines and neither may share a line with the data. The current
    `ringbuf.mx` fails this: both indices are in the first eight bytes. Verify with
    size and offset assertions at compile time, not with benchmark intuition.
13. **Multiple producers on one channel.** Impossible by ownership, and the compiler
    is what makes it so. The in-process `chan.mx` appears to support many senders
    through its task lists, but those lists cannot hold more than one entry here,
    and after unification they are gone. Do not add a multi-writer path to solve a
    problem the ownership model removes. If two senders ever appear on one ring at
    runtime, that is a compiler soundness bug, not a feature request.
14. **Ring release racing domain death.** A domain shutting down with a shared
    channel still open at the other end must decrement, never free. The refcount is
    the only thing standing between an orderly shutdown and a use-after-free in the
    surviving domain, and it is the failure that will only appear in the tests that
    exercise shutdown under load.
15. **Unification changing channel semantics silently.** The ring is strictly FIFO
    and `chan.mx` is FIFO only when its queue is empty. Any program relying on
    direct handoff order will see a difference. Audit `select` usage before the
    transport is unified.
16. **`futex_waitv` availability.** select over N channels depends on it. If the
    oldest supported kernel predates 5.16, select needs a different mechanism and
    the unification cannot land before that is decided.
17. **The compiler does not reject a double transfer, and this is confirmed, not
    suspected.** `ir_call.mx` shares channel handles into the child by design and
    there is no move tracking. Programs with two writers on one ring may exist in
    the tree today. D19 and phase 0c close it before any of the memory work
    depends on the SPSC assumption. Do not let phase 6 land first.
18. **Bootstrapping the runtime through itself.** The runtime is written in moxie
    and compiled by the compiler, which is itself compiled. During the transition
    the *old* binary emits calls to `runtime.FnArenaPush` and friends while the
    *new* source implements them, so the extern ABI must not change in the same
    phase as the implementation. Keep the signatures identical through phases 2 to
    5 and change them, if at all, only after a two-stage build passes. `-no-main-arena`
    exists because this problem is already known; do not discover it again.

---

## 20. Phase 0a: the arena inventory, recorded

Every function in `src/runtime/alloc.mx` (150 of them, the file's complete
function set) is classified below. `arena_scope.mx`, `alloc_trace.mx`,
`alloc_trace_off.mx` and `idle_trim_linux.mx` are the measurement layer and are
covered with the function tier because they read the same region table.

### 20.1 Subsumed by the function tier

`alloc`, `FnArenaPush`, `FnArenaPop`, `FnArenaPopFree`, `CurrentArena`,
`SetCurrentArena`, `InitArenaRestore`, `SovereignMarkScratch`,
`ArenaAlloc`, `ArenaAllocTD`, `arenaAllocRaw`, `ArenaNew`, `ArenaAcquire`,
`ArenaRelease`, `ArenaFree`, `ArenaReset`, `arenaHeaderSize`,
`arenaDataOffset`, `assertLen`, `rawSliceArena`, `rawSliceU64`,
`rawSliceUptr`, `rawSliceRegion`, `ArenaUsed`, `scanstack`.

`arenaTrimTail` (in `idle_trim_linux.mx`) belongs here too: it trims the tail of
the top region of a function or root arena and its semantics are unchanged when
the region is a segment.

### 20.2 Subsumed by the root heap

`initDomain`, `domainHeaderSize`, `trueRootArena`, `RootArena`, `rootArenaAddr`,
`sovRootAlloc`, `ArenaRootUsage`, `RootArenaStats`, `ReadMemStats`.

`rootArenaAddr` exists for one reason: the codec's stability test asks "is this
address in the root arena" on every pointer that could cross a boundary, and
that test must not pay the return protocol. It survives phase 2 unchanged; it is
deleted when handles remove the question (phase 8).

### 20.3 Sovereign machinery

Retained (ownership semantics, section 0): `codecSovereignArena`,
`codecSovereignEnter`, `codecSovereignEndCall`, `codecSovereignAlloc`,
`codecSovereignFree`, `SovereignSetArena`, `SovereignRestoreArena`,
`SovereignRegStable`, `SovereignRegRestore`, `sovDataArena`, `codecRegStable`,
`sovLookup`, `sovFindContainingArena`, `sovAddrInArena`, `sovInArena`,
`sovArenaBorrowed`, `sovTrackArenaGroup`, `sovGroupInDyingArena`,
`sovInvalidateArena`, `arenaReleaseGroups`, `sovRangeRegister`,
`sovRangeUnregister`, `sovInSovereignDataArena`, `sovRangeOwner`,
`sovCtxLive`, `sovBorrowedReceiverIn`, `sovGroupHeld`, `sovRekeyRelocated`.

Subsumed by the root heap's compactor (phase 5): `sovGroupTake`,
`sovRecycleGroup`, `sovParkRecs`, `sovGrowRecs`, `sovGrowSlots`, `sovAddRec`,
`sovFind`, `sovSortRecs`, `sovMergeSortRecs`, `sovMarkBlock`,
`sovRangeHasPtr`, `sovNameExtField`, `sovExtNote`, `sovRootLiveRef`,
`sovExternalRef`, `sovMarkStack`, `sovMarkExternalGroups`, `sovNodescHasPtr`,
`sovInReadableArena`, `sovReadable`, `sovWalkValue`, `sovWalkValue1`,
`sovMarkMap`, `sovFullMark`, `sovMarkFrameRoots`, `sovRebaseFrameRoots`,
`sovMarkReceiverRoots`, `sovMarkBorrowedReceivers`, `sovRebaseRoots`,
`sovCycleAbort`, `sovCycleOrder`, `codecSovereignCompact`, `sovCompactStep`,
`sovFinishCompact`, `sovRebasePtr`, `sovRebaseValue`, `sovRebaseValue1`,
`sovRebaseMap`, `sovQueueCompact`, `sovDequeuePending`, `sovRotatePending`,
`sovUnqueueCompact`, `sovTrimTail`, `sovZeroRuns`, `sovRangeOverlaps`,
`sovRegionOf`, `sovRegionOfIdx`, `liveCover`, `sovReleaseDeadGroup`,
`sovReleaseDead`, `sovNewArena`, `sovReleaseArena`, `sovPoolSlots`.

The reason the second list is subsumed and not merely deleted is D16: the
algorithm is the root heap's compactor. These functions become the same code
pointed at ROOT segments, with the receiver-keyed group as the root set. The
borrow/stable half above is what remains of the sovereign arena as an
ownership concept.

Subsumed by precise stack maps (phase 3): `sovEscapeDrop`, `sovEscapeDrop`'s
counters, `SovEscapeLease`, `SovUnescape`, `SovResetStable`,
`SovereignStableDepth`, `SovereignStableSlot`. They exist to guess at holders
the runtime cannot enumerate. Once the root set and stack maps enumerate them,
there is nothing to lease and nothing to rotate. Keep them until phase 3's
verifier is green; then delete them with the sovereign coupling, not before.

### 20.4 The reclaim API that the cleaner replaces (phase 5, then deleted)

`SovReleaseDead`, `SovTrimAll`, `SovReleaseGroup`, `SovRootScopeEnter`,
`RootScopeEnter`, `SovRootScopeExit`, `RootScopeExit`, `SovSetCopyBudget`,
`SovDrainCompactions`, `SovScanCeil`, `SovUnescape`. These are explicit
"the request is over, reclaim now" calls from `net/http`'s worker loops and the
wasm host boundary. They are not part of the memory model; they are the
workaround for a root heap that cannot reclaim by itself. Each one is a
migration site for phase 7, and each has a test that must move with it.

### 20.5 Measurement layer (phase 0b, retained and extended)

`fnPushCount`/`fnReleaseCount`, `ReadMemStats`, `arena_scope.mx` (`SetArenaScope`,
`ArenaScope`, `RecordRegionAdd`, `RecordRegionFree`, `RecordSysMap`,
`RecordSysUnmap`, `RecordSovCreate`, `RecordSovRelease`, `RecordSovUntracked`,
`RecordSovReleaseMiss`, `RecordSovReleaseRefused`, `RecordSovMeta`,
`RecordSovRegionAdd`, `RecordSovRegionFree`, `arenaScopeAlloc`,
`arenaScopeFreed`, `SetArenaTrace`, `RecordRootSize`, `arenaTraceWanted`,
`arenaScopeArena`, `ResetArenaScopeStats`, `ArenaScopeReport`, `emitLiveArena`,
`liveArenaCapBytes`, `appendScopeStr`, `appendScopeUint`), `alloc_trace.mx`,
`alloc_trace_off.mx`, `SovCompactions`, `SovRefusals`, `SovCancels`,
`SovPendingCompactions`, `SovTrimmed`, `SovGroupRecycleCount`,
`SovPendingArena`, `SovPendingHeld`, `SovSampleLoop`, `SovSampleGet`,
`SovDomainRetained`, `SovRootHits`, `SovTopGroup`, `SovDomainReport`,
`SovPrintStackWords`, `SovereignDepth`, `SovereignSlotArena`,
`SovereignSlotBorrowed`, `SovGroupTotals`, `SovGroupCount`, `SovGroupAt`.

### 20.6 Genuinely needed and missing: gaps folded back into this document

**G1. Explicit phase scopes.** `ManualArenaEnter`/`ManualArenaExit` plus
`ArenaNew` in `_mxc_stage4` (`ir_typecheck.mx`, `ir_emit.mx`, `main.mx`) hold a
whole compiler phase in one arena that is not on the call stack, and
`ArenaAcquire`/`ArenaRelease` do the same for a `net/http` request. The two-tier
model in section 4 has no name for this. It is the function tier with the push
and pop driven by the phase rather than by a call: **a phase scope is a function
arena whose push and pop are explicit**. No new mechanism, no root lifetime; the
arena is released at `ManualArenaExit` exactly as a frame is at return. It stays
in the function tier and is sized by its declaration (4-64 MiB today).

**G2. Sovereign data arenas.** A sovereign group's data arena is long-lived and
variable-sized, so it is neither a function chunk nor a ROOT segment. Until
phase 5 folds it into the root heap it is a chain of FN-kind segments owned by
the group, pooled by `sovPoolSlots` as today. Section 5's segment kinds therefore
carry `FN` for it; it is never a victim and never a destination.

**G3. The domain root is itself a segment.** `initDomain` maps one region that
holds `DomainRoot`, the carved tables and the root arena's first data. It is a
`ROOT` segment owned by the domain: pinned, never a victim, released only at
domain death. Section 5 lists ROOT without saying the domain root is one; it is.

**G4. LARGE is a real path, not an accident.** Today an allocation larger than
the active region makes `arenaAllocRaw` map a region of exactly that size and
the arena treats it like any other region. Section 5 calls this `alloc_large`
and phase 2 must implement it as a distinguished `LARGE` segment that is PINNED
and freed whole, while the arena's small-allocation cursor stays in its current
`FN` or `ROOT` segment. Without the distinction a single 2 MiB buffer would
become the arena's active region and swallow every subsequent small allocation
in its tail.

**G5. Owner-arena switching is a root-tier borrow.** `SetCurrentArena` at
`fnTop == 0`, used by hashmap growth, temporarily replaces `fnStack[0]`, which is
why `trueRootArena` exists. This is a borrow of the root slot, not an arena
kind. It survives phase 2 unchanged and is deleted with the hashmap's owner
discipline (phase 8 at the earliest).

**G6. Phase 1 cannot precede phase 4.** Section 8 and D12 describe the flat
return copy as "memcpy plus offset arithmetic with one differing base register",
which presumes offset-addressed function arenas — the change section 14/D14
schedules at phase 4. Measured in the tree: the return path today is
`codecEncodeValue` into a fn-arena buffer, `FnArenaPop`, then a structural decode
into the caller (`ir_deepcopy.mx`, `legacy/compiler/compiler.go`); there is no
`Reloc*` call from either compiler and `RelocInit` has no caller. A flat copy
without layout descriptors cannot know which words to rebase. The honest order is
**2 (segments) → 4 (`%fn_base` offsets) → 1 (flat copy)**, and phase 1's
acceptance criterion (bootstrap time) is measured after 4. The phase list in
section 16 keeps its numbering; this gap records that 1's code change lands after
4, and that claiming phase 1 before then is the kind of narrative section
"Reporting Completion" forbids.

**G7. Stability queries read the region table.** `codecIsStableAddr` and the
sovereign range table answer "is this address in an arena that outlives the
frame" by scanning `ArenaRegion` entries. Phase 2 changes what a region is
(a segment), not this query; the region table must keep answering containment
and allocation-top for every arena, including root segments and LARGE segments.
Any phase that moves the table into a side structure must carry this query with
it or the codec will start deep-copying root data on every return.

Exit criterion for 0a: the four lists above contain all 150 functions with no
duplicate and no omission; the seven gaps are recorded here; no code changed.

---

## 21. Implementation record

This section records what has landed, what the evidence is, and what is still
open. It is append-only: a later phase appends, and a claim that is not backed
by a named fixture is not a claim.

### 21.1 Phase 0a — done

Section 20 holds the classification of all 150 functions in `alloc.mx` and the
seven gaps folded back into the model. No code changed for it.

### 21.2 Phase 0b — measurement landed

- `FnArenaPush`/`ArenaRelease` keep an unconditional `pushed`/`released` ledger;
  `runtime.ArenaLedger()` returns it with the live frame depth and pool size.
  The balance holds at quiescence (measured: 55043 == 55043 in
  `tests/regressions/should_compile/segment_heap.mx`).
- `ArenaDomainExitCheck` runs at the end of a spawned domain and reports an
  unbalanced ledger under the trace gate.
- `runtime.ReadSegmentStats` reports live segments and bytes per kind, and
  `runtime.SysMapStats` the process-level mapping ledger. The segment ledger is
  updated by every mapping and release, including the sovereign release paths.
- The allocation-size histogram is carved from the DOMAIN segment (64 bins,
  floor(log2(size)), count and bytes) and is always on; `AllocHistogram`
  exposes it. Removed after use: the temporary per-phase `tc-arena` prints that
  located the compiler's own allocation appetite.
- Not landed: per-function peak usage (needs compiler instrumentation at the
  frame boundary) and the relay/marmot growth curves (those trees are not in
  this checkout).

### 21.3 Phase 2 — segment allocator landed

Implemented:

- **Segment classes.** `FN` chunks are 64 KiB, `ROOT` segments 1 MiB natively and
  64 KiB on wasm, the `DOMAIN` segment 1 MiB natively and 256 KiB on wasm,
  `LARGE` a dedicated mapping of exactly the request, `CHANNEL` the ring
  (separate mapping, section 4). `arenaRootSegCap`, `arenaDomainSegCap`,
  `arenaPhaseChainCap` live in the platform files.
- **The region table is the segment table.** `ArenaRegion` gained `kind`,
  `state`, `owner` and `gen`, so there is no second descriptor to synchronise.
  Region indices are the segment indices for the arena's purposes.
- **`Arena.active` is the small-allocation cursor.** A `LARGE` segment never
  becomes the cursor (G4), so a multi-megabyte buffer cannot swallow the small
  allocations behind it. `arenaPickActive` restores the cursor after the
  sovereign region-table compaction.
- **Two growth policies.** A frame chunk doubles to 4 MiB; a phase scope or a
  sovereign data arena grows to a 64 MiB class natively (4 MiB on wasm). The
  root arena appends fixed `ROOT` segments; a request above one sixteenth of a
  segment takes a `LARGE` segment instead, bounding tail waste.
- **The root heap no longer reserves its worst case.** `initDomain` maps the
  DOMAIN segment (1 MiB) and the root arena appends 1 MiB ROOT segments, so a
  174 MB root heap is 174 segments instead of one 256 MB reservation plus 64 MB
  chains. The root region table is 1024 entries, carved in the domain segment.
- **Pooled function arenas are per-region data-offset aware.** The region table
  is 32 entries for a frame chunk and the data offset is recorded per arena.

Bugs found and fixed while landing it:

1. `ArenaReset` reset region 0's bump to `arenaDataOffset()`, which excludes the
   `Arena` header. A pooled arena's next allocation therefore began inside the
   tail of its own region table (128 entries made the overlap 200 bytes; the
   32-entry table made it larger). Fixed by storing `dataOff` per arena.
2. `sovReleaseDeadGroup` rebuilt the region table without copying the new
   descriptor fields and left `active` pointing at a dropped region. Fixed by
   copying the descriptor and re-picking the cursor.
3. `sovTrimTail` tested an appended region for emptiness with
   `off == arenaDataOffset()`, but an appended region is empty at `off == 0`, so
   overflow regions were never trimmed. Fixed.
4. `chanSelect` had lost the compaction drain that CLAUDE.md documents
   ("compaction is drained at every select"). The fixture
   `sov_compaction` was red on the pristine runtime for exactly this reason
   (verified by building it with a compiler built from the unmodified sources);
   the drain is restored immediately after `interrupt.Disable()`, before the
   arms are examined, and the fixture now passes.

Evidence:

- `tests/regressions/should_compile/segment_heap.mx` passes: a warm
  2000-iteration deep-call loop makes zero mapping syscalls (`SysMapStats`
  unchanged at 30 maps), the ledger balances, a 2 MB object takes a `LARGE`
  segment, the domain segment is one `DOMAIN` segment, and the histogram has
  counted 169k allocations.
- `tests/regressions/should_compile/ring_chain.mx` passes (section 21.4).
- The fixture set `arena_deep_nest`, `chan_buffered_slice`, `rootmap`,
  `sovereign_arena`, `spawn_frame_budget`, `spawn_orphan`,
  `spawn_parked_intermediate`, `spawn_root_parked_parent` and
  `spawn_worker_print_fd` passes natively with a compiler built from these
  sources.
- A wasm build of the self-hosted compiler completes:
  `mxc build -target wasm ./_mxc_stage4` produces a module.
- The stdlib test suite (`run_tests.sh`, every package with tests) passes 11 of
  11 with a compiler built by the checked-in generation and the new runtime.
  The first run, with a compiler whose own runtime predated the root-table
  growth, lost `crypto/x509` to a root-table overflow at 1024 regions; that is
  the failure the growth above was written for, and it is in the record because
  the fix is only trustworthy when the test that caught it is named.

Not yet demonstrated, and why:

- **The bootstrap fixpoint.** A compiler generation built from these sources
  (`mxc-r2`) bakes the runtime and builds the next generation (`mxc-r3`), but
  `mxc-r3` fails building the generation after it. The same failure happens
  with the unmodified runtime: a compiler built from pristine sources by the
  same path (`p3`) dies at the same package and with the same symptom class.
  This is the pre-existing compiler instability recorded in `issues.md`
  ("stage4 builds, the binary it produces does not run yet"), not a regression
  from this phase. The fixpoint gate stays red until that compiler bug is
  found; phase 2's other exit criterion (zero mmap per allocating call in
  steady state) is met and pinned by `segment_heap`.

  Re-measured while landing phase 0c, with a narrower symptom: the compiler
  built by the checked-in binary (`./mxc-stage4`, call it C0) works and is
  REPRODUCIBLE (two C0 builds of the same sources are byte-identical, sha256
  `57c55732…`). Any compiler built BY a stage4 compiler - C1 from C0, and C2
  from C1 - builds sources correctly but then panics at start-up in
  `runtime.codecDecodeValue` with "codec bad pointer tag", before it compiles a
  single file. A compiler built by legacy (`legacy/moxie build -a ./_mxc_stage4`)
  shows the same panic, so this is not a stage4-vs-legacy divergence in the
  compiler's own output: it is a miscompile of the compiler (or of the runtime it
  embeds) by every builder except the checked-in C0. Bisected to the same
  symptom at 8335db88, 50fccafe and the bare-end commit, so it predates the
  ownership work; a clean package cache does not change it. The build PATH that
  works, and that every fixture in this record was verified with, is
  `./mxc-stage4 build -a` (C0). The fixpoint needs the codec panic found first.

  The panic now has a precise shape: instrumenting both `codec bad pointer tag`
  sites showed the decoder reading tag **2 (slice)** where a pointer tag was
  expected, at byte 4451 of a 6086-byte frame, while decoding a value whose
  layout kind is 21. That is a desynchronisation between the encoder's and the
  decoder's idea of one field: the two sides disagree about a slice-versus-
  pointer field, which is the family the string/[]byte unification creates when
  the two spellings of the same type are given different descriptors. It is the
  next thing to chase, and it is why every compiler-derived binary dies before
  it compiles anything while the checked-in one does not.

  A debugger backtrace turned that into a call chain: the decoder was walking
  `map[bytes]TypeName` -> `TypeName.object` (interface) -> `typeapi.Type`
  (interface) -> `*Named` -> `Named` -> an interface whose 8-byte typecode had
  become **3**, the value of `codecTagIface` - so the stream was already
  desynchronised one field earlier and the "typecode" was the next tag. A
  position trace showed the failing struct is 224 bytes with eight fields whose
  first two are `Scope`, `Typ`, and that NO encode of that struct ran in the
  process: the encoder and decoder disagree about the layout of one compiler
  type, so the encoder's descriptor says "small" and the decoder's says 224.
  It reproduces with `-j 1` (no spawn) and with a cold package cache, so it is
  neither the parallel compiler nor stale export data.

  The decisive comparison: BOTH sides print the same descriptor for the failing
  value - 224 bytes, eight fields, `Scope:0 Typ:8 Error:176 Uintptr:184
  Registry:192 DirectImports:200 ByName:208 byNameSeq:216`. So the descriptor is
  not duplicated or stale; the encoder writes something for field `Scope` (a
  `*Scope`, kind 21 Pointer) that begins with tag **2**, the slice tag, and the
  decoder then reads that as a pointer tag. The fault is therefore in the
  encoder's choice of tag for one pointer field of a compiler type during a
  return encode - the next diagnostic is a per-field tag trace on the encode
  side (print the tag written for each field of the 224-byte struct), which
  names the branch that writes 2.

  A per-field trace on the ENCODE side settles where the fault is NOT: field 0
  (`Scope`, kind 21 Pointer, 8 bytes) is encoded with tag **0**, `codecTagPtr`,
  exactly as the decoder expects, and the fields are the same eight in the same
  order. The decoder nevertheless reads 2 for that position, and every encode
  position read as 0 because the encoder appends through `push` rather than
  advancing `cb.pos`. Conclusion: what the decoder is handed is not the buffer
  the encoder filled, or not the same window of it - the defect is in the RETURN
  buffer handoff (`ir_deepcopy` saves the data pointer and length after the
  encode loop, restores the caller arena, then rebuilds a CodecBuf from them),
  most likely a stale slice when the encode GREW the buffer. That is exactly
  why a small graph passes (`codecEnsure` never grows) and the universe graph
  fails, and it is the same region phase 1 (flat return copy) rewrites.

  The next experiment is a byte-window comparison: print the first eight bytes
  and the length at the end of the encode, and again at the entry of the failing
  decode. Equal bytes mean the decoder starts at the wrong offset; different
  bytes mean the saved pointer is stale.

  **Round 10: the stalling declaration is a GENERIC FUNCTION.** Counting the
  declarations of `internal/bytealg` through the scope-population loop prints
  0..19 and then stops inside declaration 20, which in that file is the first
  generic function (`HashStr[T []byte]`; `HashStrRev`, `IndexRabinKarp` and
  `LastIndexRabinKarp` follow). The old binary typechecks the same file in
  under a second, so the regression is in how the current sources handle a
  generic function DECLARATION, not in parsing (the scanner's own no-progress
  detector never fired) and not in the alias pass. A standalone reproduction is
  not available through the normal build path - even a non-main package pulls
  the runtime closure, so bytealg is always typechecked first - and a git
  worktree at an older commit cannot simply reuse the current `legacy/moxie`
  binary (it is linked against its own checkout's runtime libs), so the next
  bisect point has to build legacy inside the worktree.

  **Round 9: located in the typecheck pass, between the builtin-String
  injection and the alias pass.** Instrumenting `typeCheckPkg` with stage
  markers showed `internal/bytealg` printing `parsed` and `inj` and then never
  reaching `pre-scan` or the first `pass` marker, while the packages before it
  (`array`, `internal/task`) print every marker and finish in seconds. The
  region between those two markers is the per-declaration loop that records a
  generic type declaration's parameters on its `Named` before the body resolves
  - which is where the next instrumented build should count iterations, because
  bytealg has no generic type declarations of its own, so a loop that spends
  minutes there is iterating something it should not.

  **Round 9: the next wall is a performance cliff, not a hang.** After the codec
  fix a compiler built from these sources starts and compiles, but it needs more
  than ten minutes on `internal/bytealg`'s typecheck where the six-day-old
  working binary needs 21-28 seconds for the whole closure (measured with a cold
  package cache via `MOXIEPATH`, so the cache is not the difference). Two
  hypotheses were eliminated by rebuilding the compiler and timing it: the
  `Identical` type-argument comparison, and the phase 0b allocation
  instrumentation (`arenaScopeAlloc`/`recordAllocSize` on every bump). Attaching
  a debugger to the running compiler is not possible in this environment (no
  frames from `gdb -p`), so the next step is a source bisect: build the compiler
  with `legacy/moxie` at successive commits and time `internal/bytealg`
  (~7 minutes per candidate, cold cache).

  **CLEARED in round 8.** `installChannelOwnershipTypes` now runs from
  `SetUniverseGlobals`, after the universe has been handed out, instead of
  inside `buildUniverse`. A compiler built by `legacy/moxie` from these sources
  starts and compiles; it no longer dies in `codecDecodeValue`. The next
  blocker is the parse runaway this record already names: the new compiler
  spends 22 minutes at 89% CPU on `internal/bytealg` before the build times out,
  which is the `syntax.ParseBytesArena` blow-up described above, not a codec
  fault.

  **Root cause found (round 8).** The trigger is the hand-built ownership-type graph
  installed by `installChannelOwnershipTypes`. With that call disabled the
  compiler built by `legacy/moxie` starts and compiles (it reached
  `internal/bytealg` instead of dying at start-up); with only `Edge` left out of
  the universe it also runs. The `Sender`/`Receiver` objects alone are harmless;
  `Edge` - a `Named` whose `Under` is a hand-built three-field `TCStruct` whose
  field types are hand-made `Named` instances carrying `TArgs` - is what
  desynchronises the encode. Two repairs were tried and both still desync:
  giving the field instances their own instantiated `TCChan` underlying, and
  leaving the generic bodies in place. The next repair to try is to keep the
  ownership objects OUT of the universe graph that `InitUniverse` returns (build
  them on first lookup of the name, after the return encode) or to build the
  `Edge` instance through the same path user generics take, so the descriptor
  graph never contains a hand-assembled generic.

  Growth alone is NOT the trigger: a 64-item bundle of pointer+slice+string
  elements returned from a function (a frame far past the codec's first
  allocation, so `push` moves the buffer) round-trips intact on the current
  compiler, printing `grow return ok 189` (with the known arena-escape note for
  an unknown-extent pointee). So the suspect is narrower than "returns that
  grow": it is this graph - one with interfaces whose dynamic types are
  package-level named types, which is what the universe is made of.

  A second fact that constrains the repair: the older working binaries in
  `/tmp` (10-11 November) cannot compile the present sources at all - they
  enforce "':=' declaration inside a loop in a self-mutating method" as an
  ERROR, a rule the newer compiler relaxed to a note - so the compiler can only
  be rebuilt through a binary that is itself built from sources at or after that
  relaxation. Preserve one such binary (`/tmp/mm-seg/mxc-r46`) before replacing
  it, or the tree becomes unbuildable, which is exactly what happened in this
  round.

  **Operational trap: `./mxc-stage4` is a BUILD ARTIFACT and can be stale.**
  The copy in the tree was from 6 November and predated the child channel handle
  copy and the whole ownership surface, so fixtures compiled with it exercised
  none of that: `ring_release` appeared to regress (the child shared the parent's
  handle and no end was released) purely because the wrapper that binary emitted
  had no `ChildChannelCopy`. The rule that follows: after any compiler change,
  rebuild `./mxc-stage4` from the current sources with the previous one, and use
  THAT for fixtures. The refreshed binary (`r46`) works and is reproducible, and
  the sources at this commit also build it through `legacy/moxie`.

### 21.4 Phase 6 — the ring half landed

Implemented:

- **D2 cache-line layout.** The header is 384 bytes: producer line at 0
  (`writeIdx`, `sentFrames`), consumer line at 128 (`readIdx`, `recvFrames`),
  shared line at 256 (`size`, `closed`, `recvSleeper`, `sendSleeper`,
  `refcount`), data at 384. `ringLayoutCheck` asserts every offset and the total
  size at every creation, because the runtime has no const-assert form.
- **D3 chained frames.** A chunk is `[len u32][flags u32][payload][pad 8]`;
  `ringFlagFinal` marks the last chunk. A message is split into chunks of at
  most half the data area, so a chunk always fits when the ring is otherwise
  empty and the reader always has room to consume the chunk that is blocking
  the writer. `sentFrames` is counted once per message BEFORE the first chunk's
  commit store (counting after it lets a single-chunk message complete on the
  far side first and underflow the depth); `recvFrames` is counted once, on
  FINAL. The index advance is the commit.
- **D4 fixed ring size.** `ringDefaultDataSize()` is 128 KiB; `spawnRingDataSize`
  and `ringMaxMsg` are deleted. A spawn's rings are no longer a function of the
  declared depth or frame budget; the depth is enforced by the message counters
  and the budget by the send's own check.
- **The concurrency class (section 4).** A channel carries `class`:
  `chanClassLocal` (the zero value, so every channel that is not bound to a
  spawn is local) or `chanClassShared`. Dispatch in `chan.mx`, `pipe_channel.mx`
  and `spawn_wasm.mx` reads the class rather than a spawn-bound boolean, so the
  one branch the design describes exists and has two named sides. The LOCAL body
  is still the in-runtime queue: deleting it is the remaining half of phase 6,
  not a rounding error, because the queue's park loop is the only place a
  local send can make progress while I/O callbacks run in the same thread.
- **Reassembly.** `PipeChanSend`/`PipeChanRecv` stream chunks and reassemble.
  A select receive keeps its partial message in `channel.rxPartial`, backed by
  the root arena (`rootPushBytes`), because the selecting frame dies between
  attempts. A select SEND cannot park, so it refuses a frame larger than one
  chunk (`tryPipeSend` returns false and the arm loses); a blocking send on the
  same channel carries the chained form. This is a recorded limitation, not a
  silent one, and it is the one place phase 6 deviates from "every send is the
  same protocol".

Evidence:

- `tests/regressions/should_compile/ring_chain.mx` passes: 300 KB messages
  cross a 128 KiB ring byte-identical and in order through a select receiver
  (`ring chain ok 5 530304` — five mappings totalling 530 KB for a spawn, where
  the old reservation was 32 MiB per direction).
- `futexpark` (200 parked ping-pong rounds and a dead-peer unpark) passes with
  the new header and chaining.

**The ring release protocol (section 11, acceptance test 18).** A ring is
created with two references, one per end. A handle drops its end's references
when the parent closes the channel and when the child's domain ends (its exit
path, before the breaker close, so "the child has ended" means everything it
owed is returned). A channel handed to a second child retains the pair first, so
a manager passing a worker's channel on cannot let the new child's exit drop the
manager's reference. The holder that decrements to zero unmaps, and only that
holder: a domain that ends while the other end is open decrements and never
frees (risk 14). `tests/regressions/should_compile/ring_release.mx` forces the
parent's drop to be last and asserts three rings → one.

Landing it deleted the dead retirement machinery: `releaseLastSpawn`,
`recordLastSpawn` and the `lastSpawn*` record had no caller in the tree, and
with a refcount the "retire the previous spawn's rings" notion is gone. The
control ring is the exception and the remaining cost: it is the breaker's ring,
it has no close hook (the child closes it, the parent only observes it), and its
parent-side reference lives until the process ends — 4 KiB per spawn, against
the 32 MiB per direction the old sizing reserved. The `spawnCtl` block (16
bytes per spawn) goes with the process for the same reason.

Still open in phase 6: replacing `chan.mx`'s bounded queue with the ring and
deleting its sender/receiver task lists, the capacity-0 rule, `futex_waitv` for
select, and the `Edge`/`Sender`/`Receiver` two-ring binding that phase 0c
declares.

### 21.5 Phase 0c — role checks landed; the Edge half is open

Landed: **D19 rule 5, the operation-level role check**, in stage4. A send on a
`<-chan T`, a receive from a `chan<- T`, and both shapes inside a select clause
are compile errors naming the file and line:

    main.mx:13: cannot send to receive-only channel
    main.mx:20: cannot receive from send-only channel

This was a real bilateral divergence, not a missing nicety: the legacy compiler
gets the check from go/types and always rejected these programs, while stage4
accepted them and emitted `PipeChanSendRaw` on the receive ring. The two halves
of the stack disagreed about a program that type-checks at all. The fix is in
`ssa_builder.mx` (`checkSendChan`/`checkRecvChan` at the send statement, the
select send clause, both select receive forms and the unary receive), and it is
pinned by `tests/restrictions/reject_chan_send_role.mx` and
`reject_chan_recv_role.mx`.

**Prerequisite fixed in the same round.** The phase-0c probe surfaced a second
bilateral divergence, this one in the type machinery Edge stands on: every
element-type query used `SafeUnderlying` without binding a generic named
container's instantiation, so `Q[int32]` (where `type Q[T cmp.Ordered] chan T`)
looked like `chan T`. A receive through it printed an empty value in stage4
while legacy printed the number, a named generic slice indexed the same way,
and the emitter's channel-element string helper fell back to `i8`. The four
queries (`ssaChanElemType`, `ssaElemType`, `ssaIndexElemType` and the emitter's
two channel helpers) now bind the instantiation, and the emitter's helpers
route through the one SSA definition instead of four copies. Pinned by
`tests/regressions/should_compile/chan_named_elem.mx` (channel, slice, map and
the directional forms) on both compilers, and the stdlib test suite passes 11 of
11 with the change.

**Builtin ownership types (22.3 step 2) landed.** `Sender[T]`, `Receiver[T]`
and `Edge[In, Out]` are now PREDECLARED generic types in both compilers, with
no import and no user constraint:

    Sender[T]     chan<- T
    Receiver[T]   <-chan T
    Edge[In, Out] struct { Recv Receiver[In]; Send Sender[Out] }

stage4 constructs them in `pkg/types`' `buildUniverse`; legacy installs them in
the patched `go/types` universe from `legacy/build/patch-gotypes.go`. Because
the underlying types are ordinary directional channels and a two-field struct,
the operation-level role check applies through `e.Send`/`e.Recv` unchanged, and
an instantiated field carries its instantiation (pinned by a send through
`e.Send` arriving at `e.Recv` on both compilers). The stdlib test suite passes
11 of 11 with the universe change, and the sampled regression set (channels,
spawn, sovereign compaction, segment heap, rootmap) is green.

Two things learned while installing them, recorded for the next reader:

- `go/types`' `def()` routes an EXPORTED name to the unsafe package scope and
  asserts the object is black. These names are exported and must be
  predeclared, so the legacy patch sets the object colour explicitly
  (`NewTypeName` with a nil type is white) and inserts into `Universe`
  directly. Leaving the placeholder type non-nil instead (to get a black
  object) silently kept the object's type at `invalid`, which type-checked as
  a generic type but panicked the SSA builder with "no types.Object for
  ast.Ident".
- Package-level declarations named `Edge`/`Sender` are legal in both compilers
  (the shadowing rule governs inner scopes), so `internal/profile.Edge` and
  `crypto/hpke.Sender` keep working and no exemption was needed. Inside those
  packages the spelling means their own type, which is exactly why the
  compiler keys the builtin identity on the universe object rather than on
  the spelling.

**The declaration form (22.3 step 3) landed.** `parent, child := edge[In, Out]{depth}`
is lowered by each compiler's literal pass (Go scanner tokens in legacy,
`pkg/mxsrc` text scanning in stage4) into

    var parent Edge[In, Out]; var child Edge[Out, In]; __edgealloc(&parent, &child, Edge[In, Out]{}, depth)

`__edgealloc` is an internal builtin in both universes, and both compilers lower
it to `runtime.EdgeMake`, which allocates two SPSC rings and four
fixed-direction handles in the root arena. Three details are load-bearing:

- **The child is the complement.** With one type on both sides the child's
  Receive end would be typed for In while the parent sends Out, and the pair
  would not type-check. The declaration names In first (the parent's receive
  type) and Out second.
- **Direction is a property of the handle.** A fixed-direction handle
  (`dirFixed`) resolves `chSendRing`/`chRecvRing` from itself, not from the
  domain role: a Sender handle has only a send ring, a Receiver handle only a
  receive ring.
- **Element sizes follow the HANDLE, not the ring convention.** The two
  directions carry different elements, and a receive handle shares its element
  with the peer sender. Swapping the child's sizes was caught at run time by the
  frame-size check ("received 8 bytes for a 4-byte element"), which is why the
  factory's four handles are written out explicitly rather than derived.

`tests/regressions/should_compile/edge_decl.mx` passes on both compilers: both
directions of an int32/int64 pair and a codec-carrying struct pair.

**spawn acceptance (step 3b) landed.** A declared pair crosses the spawn
boundary as a value: the child receives the complement it was handed, the
argument's type must match the target's parameter BY NAME (a same-shaped struct
does not pass), and the child's breaker is bound on all four handles through the
pair record. The record is the Edge value's hidden third word, typed `uintptr`
so the argument codec copies it verbatim instead of following it.

Two more findings, both fixed:

- An `Edge` value contains channel fields, and legacy's spawn validator rejected
  any argument with a nested channel. A declared pair is a capability (its
  channel fields are the pair's ends), so ownership types are exempt from that
  rule; values that hide a capability inside copied bytes are still rejected.
- stage4's `emitMakeEdge` still required the Edge struct to have TWO fields when
  computing the two element sizes, so with the hidden word added the sizes fell
  back to i8/i8. The pair then carried 1-byte elements: a 300-byte response was
  truncated to a byte (44) while short ones looked correct, which is what the
  multi-size fixture caught.

Fixtures: `edge_spawn.mx` (both directions, 10/300/1000-byte requests, passes on
both compilers) and `reject_edge_transposed.mx` (the parent handed to a target
taking the child type; stage4 rejects it in the SSA builder, legacy in its spawn
argument check, both with the same sentence).

**A diagnostics defect behind that check.** stage4's `Named.String()` rendered
only the origin, so `Edge[int32, Req]` and `Edge[Req, int32]` both printed as
`Edge` - the transposed pair looked identical in every message, and an
agreement check that compared rendered names could not see the difference. It
now renders the instance's arguments, matching go/types. Related and still
open: stage4's `types.Identical` compares two instantiations of a generic by
ORIGIN alone, so assignment between transposed pairs is accepted where legacy
rejects it; the spawn check therefore compares rendered types, and the Identical
defect is recorded in the open decisions rather than papered over.

**Affineness (step 4) landed.** An Edge has one owner and moves once: into a
spawn, or out of the function as a return value. Reading its fields
(`e.Send`, `e.Recv`) is a read. Everything else - `e2 := e1`, a non-spawn call,
a struct literal, a send, a slice element - is a copy and is rejected; a second
spawn of one pair is rejected too. Bare `Sender`/`Receiver` values are not yet
tracked this way.

The two compilers check it in different places for the same reason: legacy has
go/types information over the go/ast, so it walks parent links after the final
type-check; stage4's SSA builder carries NO type information (it resolves through
scopes), so its check runs in the typecheck pass, where every expression has a
TypeAndValue, and the verdict goes through the deferred-error channel that const
underflow already uses (the typecheck pass drops its own `compileErrors`, because
their slices live in the pass arena). stage4 identifies a pair-holding variable
syntactically - the lowered declaration is `var name Edge[In, Out]` - which also
sidesteps shadowing.

A verification trap worth recording: a build can be served from cached package
objects even with `-a` when the source path was built before, so a compiler-side
check added moments ago appears not to fire. Every one of these fixtures was
confirmed in a FRESH directory; several negative results in this round were
cache artifacts, not logic errors.

**A bare end crosses spawn too.** The ends of a pair need not travel together:
the parent can keep its send end and hand the child's receive end to a worker.
A `Sender`/`Receiver` is a handle, so the word IS the message - no codec, no
ring. Three things had to line up:

- Both compilers exclude ownership ends from the spawn's channel-argument list,
  so `spawnDomain` does not size a ring pair for a handle that already has one.
- The child-side chan-ID assignment must skip them too. stage4 assigns IDs from
  the CALL SITE's channel indices, which already exclude them; legacy assigns
  them from the TARGET SIGNATURE, so a bare end shifted every later channel by
  one and the child bound a fresh ring pair over a handle that already had one -
  the child then parked forever on a ring nobody wrote.
- The child's breaker is bound through the HANDLE's pair link: a fixed-direction
  handle carries `edgePair`, so `runtime.EdgeBindHandle(handle, breaker)` reaches
  all four handles, including the parent's end - the one that has to wake.

Pinned by `edge_bare_end.mx` (parent keeps `Edge.Send`, child gets
`child.Recv` as a `Receiver[Req]`, requests 5/300/1024 bytes, replies on a plain
spawn channel), on both compilers. The two-value `chan` form remains open.

The over-budget panic from an Edge frame also prints an empty variable-length
field list, because the budget note travels on the channel and an Edge handle
has none; the message names size and budget only. Until those land, the tree still
passes the same channel handle to a child and to the parent, and nothing rejects
two transfers of one send end: that is a live soundness hole, exactly as D19
states. The role check above is rule 5 of D19 and does not close rule 3.

### 21.6 Verification notes for a later reader

- The compiler generation built BY a working stage4 is currently unstable in
  this checkout (it runs away in typecheck and aborts). A generation built by
  the checked-in `./mxc-stage4` works and is what the fixtures above ran with.
  Do not conclude from a red bootstrap that a memory-model change broke the
  compiler: build the generation with `./mxc-stage4` and compare against the
  same chain on unmodified runtime sources, which is how the pre-existing
  failure is separated from a regression.
- Narrowed on 2026-11-12 with `tclen` prints in `typeCheckPkg`: for
  `internal/bytealg` the rewrites leave 9,285 bytes of source (const 9,272,
  shutdown 9,272, chan 9,272, slice 9,285, duppkg 9,285) and the runaway starts
  in `syntax.ParseBytesArena`, which consumes hundreds of megabytes for that
  source in the broken generation. The rewrites are not implicated, the parser
  (or what it calls) is. Recorded so the next attempt starts at the parser
  rather than at the arena.
- `sysroot/runtime_go.bc` is gitignored and must be re-baked
  (`<compiler> build-runtime`) after any runtime change, or applications link
  the previous runtime and fail with undefined symbols.
- The wasm target keeps its own transport (`spawn_wasm.mx`, SharedArrayBuffer);
  the ring changes in this record are native-only by construction. The class
  field is shared, so a wasm build must still compile: `channelDropRings` has a
  wasm no-op there (the SAB is owned by the JS bridge, not by this address
  space).
- `5/spawn_orphan` fails on wasm with interleaved output from two domains
  writing to the host's stdout one character at a time. It is an output-path
  failure, not a memory-model one, and it is red with the unmodified runtime as
  well; it is recorded here so a later reader does not attribute it to the
  transport.

---

## 22. Phase 0c: the channel ownership types — plan and prerequisites

Status: operation-level role checks landed (21.5). `Edge[In, Out]`,
`Sender T`, `Receiver T`, the declaration forms, affineness and spawn
agreement are open. This section records what the reconnaissance established,
the decisions taken, the order, and the one discovered prerequisite, so the
next round starts at the first unfinished line rather than at the argument.

### 22.1 What was verified

- **Unconstrained type parameters are not a thing in this language.** Both
  compilers reject `type Sender[T] chan<- T` with `syntax error: missing type
  constraint`, and claude.md requires a concrete non-empty constraint. So
  `Edge[In, Out]`, whose parameters are deliberately unconstrained, cannot be
  an ordinary generic declared in a package: it must be a **builtin (universe)
  generic**, exactly as D19 rule 2 says. This is a parse/semantics fact, not a
  style choice.
- **The type forms themselves exist.** `type Sender[T cmp.Ordered] chan<- T`
  compiles, converts from `chan T`, and enforces the role on the operation. So
  the directional-channel machinery the runtime and the role checks need is
  already in the type system once the parameters are builtin rather than
  user-constrained.
- **Prerequisite bug, found with the probe and FIXED (21.5).** A channel whose
  type is a generic instantiation lost its instantiation at every element-type
  query: with `type Q[T cmp.Ordered] chan T`, `var q Q[int32]` was treated as
  `chan T`, `<-q` yielded an empty value in stage4 while legacy yielded the
  number, and the emitter's string fallback turned it into an `i8` store once
  the SSA side was bound. The fix was to bind `instantiatedUnder` in the four
  element-type definitions and delete the parallel copies in the emitter; the
  fixture `chan_named_elem.mx` covers channel, slice, map and directional
  forms on both compilers. The first attempt (binding only at the receive call
  site) moved the failure into the IR and was reverted; it is recorded because
  "fix the query, not the call site" is the lesson. Edge now starts on
  resolved types.
- The universe is built from `src/builtin/builtin.mx` plus the `Typ` table
  (`ir_util.mx`/`ir_typecheck.mx`/`ir_iface.mx`). A builtin generic type is a
  universe entry with `TParams` and a marker that exempts it from the
  constraint check; that is new machinery and it belongs to the universe, not
  to a package.

### 22.2 Decisions

**D22.1 — Edge is a builtin generic in the universe.** A package-level generic
is impossible (22.1) and a shape-matched user struct must not satisfy `spawn`
(D19 rule 2). The three types are universe entries: `Sender[T]`,
`Receiver[T]`, `Edge[In, Out]`.

**D22.2 — Representation.** `Sender[T]`/`Receiver[T]` are channel types with a
direction (`TCChan` with `Dir`), so the existing role check applies unchanged
and `spawn` can verify agreement by type identity. `Edge[In, Out]` is a struct
of `Recv Receiver[In]` and `Send Sender[Out]` plus one compiler-known hidden
word: a pointer to the pair record. The hidden word is what lets `spawn(child)`
reach the four handles to bind the child's breaker without deriving the mirror
from an argument (A1 forbids the derivation, not the linkage). It is invisible
to user code and is not part of the type's field set.

**D22.3 — Runtime handles get a fixed direction.** A channel handle gains
`dirFixed`; when set, `chSendRing` returns `ringA` and `chRecvRing` returns
`ringB` unconditionally, so a handle's role is a property of the handle rather
than of `domainID`. The edge factory creates two rings (one per direction) and
four handles: parent Send, parent Recv, child Send, child Recv. Each handle
drops exactly the ring it references on close, and the ring's two references
are the two ends, so the release protocol from 21.4 is unchanged.

**D22.4 — Declaration order: `edge` first.** `parent, child := edge[In, Out]{d}`
is new syntax with no ambiguity: it allocates the pair and two rings in the
root arena at the declaration and returns the two complementary values. The
two-value `s, r := chan T{d}` form is the same machinery, but `chan T{:N}`
already means one bidirectional value, so making the two-value form mean
something different is a separate decision (recorded open in 22.5). Do not
introduce it in the same step.

**D22.5 — Affineness, concretely.**
- A *use* of a held end is a send, a receive, or a select arm on `e.Send`/
  `e.Recv`; it does not move the value and may happen any number of times.
- A *move* is a whole-value transfer: a `spawn` argument, assignment to another
  linear variable, a return, or a store into a field/slice/map.
- Anything else that needs the value's bytes (comparison, printing, a
  non-spawn call argument, a conversion) is a **copy** and is an error.
- After a move, any use of that variable is an error. Tracking is per function
  and sequential in the SSA builder (legacy mirrors in its own builder); a move
  in one branch of an if is treated as a move on the joined path, which is
  conservative and sound for a one-thread domain.

**D22.6 — spawn agreement.** `spawn` accepts `Edge[In, Out]`, `Sender T` or
`Receiver T`; the child parameter's type must be identical. Transposition is a
type mismatch because the parameters are named, not positional. The child's
handles are the same structures the parent holds (they are in the root arena),
so the child receives capabilities, not a copy of the ring.

### 22.3 Order of work (each step independently testable)

1. **Fix the instantiated-channel-type prerequisite** — DONE (21.5), pinned by
   `chan_named_elem.mx`.
2. **Universe builtin generics** — DONE (21.5): `Sender`, `Receiver` and `Edge`
   are predeclared in both compilers; `Sender[int32]`, `Edge[Request, Response]`
   and the field roles type-check and run, pinned by `edge_types.mx` and
   `reject_edge_field_role.mx`.
3. **`edge[In, Out]{d}`** — DONE (21.5): the declaration, the runtime factory
   (two rings, four fixed-direction handles, pair record, root arena), spawn
   acceptance, name-based agreement, breaker binding through the pair, and the
   general spawn argument/parameter agreement in stage4 that legacy already had
   (a user struct no longer satisfies an Edge parameter there). Pinned by
   `edge_decl.mx`, `edge_spawn.mx`, `edge_roundtrip.mx`,
   `reject_edge_transposed.mx` and `reject_edge_user_struct.mx`. Bare
   `Sender`/`Receiver` spawn arguments and the two-value `chan` form remain.
4. **Affineness checks** — DONE (21.5), in BOTH compilers: legacy checks the
   lowered AST after the final type-check; stage4 checks in the typecheck pass
   (its SSA pass has no type information) and fails the package through the
   same deferred-error channel const underflow uses. Pinned by
   `reject_edge_copy.mx` and `reject_edge_two_spawns.mx`.
5. **Acceptance fixtures**: role through an Edge field; transposed Edge;
   one Edge to two spawns; a copied end; a user struct with the same field
   shape rejected; channel storage outliving the declaring frame.

### 22.4 Acceptance fixtures to add when the step lands

- `reject_edge_transposed.mx` — `spawn` given `Edge[Request, Response]` where
  the target takes `Edge[Response, Request]`.
- `reject_edge_two_spawns.mx` — one child End passed to two `spawn` calls.
- `reject_edge_copy.mx` — `e2 := e1` or `f(e1)` where `f` is not a spawn.
- `reject_edge_user_struct.mx` — a local struct with `Send`/`Recv` fields does
  not satisfy a spawn parameter of type `Edge[In, Out]`.
- `edge_roundtrip.mx` — request and response cross in both directions, the
  child outlives the declaring function, and the rings are released by the last
  end (the 21.4 protocol, now over four handles).

### 22.4a Step 3's implementation path (recorded before the code)

The parser accepts `edge[In, Out]{d}` on both sides without change: it is a
composite literal whose type is an index expression, which is valid Go syntax
(and stage4's parser mirrors it). A builtin generic *function* `edge(...)` is
NOT the cheap path: `Signature` in `pkg/types` carries no type parameters
(stage4 monomorphizes generic functions textually), and legacy would need a
universe Func with a generic signature.

The cheap path is the one `[]T{:n}` already takes: `mxsrc.RewriteChanMakeLiterals`
turns the literal into `__slicealloc(T, n)`, where the FIRST argument is a TYPE
expression and the compiler special-cases the builtin (`ssa_types.mx`'s
`__slicealloc` case). So:

1. `pkg/mxsrc` rewrites `edge[In, Out]{d}` to `__edgealloc(Edge[In, Out], d)`
   before the parser runs, next to the chan/slice literal rewrites.
2. Both compilers special-case the `__edgealloc` call: the first argument is the
   instantiated Edge type (supplying element sizes, codec descriptors and frame
   budgets from the same walk the channel element uses), the second the depth.
   The call has TWO results (parent, child), so the assignment stays a normal
   two-value `:=`.
3. The runtime `edgeMake` allocates the pair record and the two rings in the
   root arena and returns the four fixed-direction handles; the compiler builds
   the parent and child `Edge` structs from them.
4. `spawn(childEdge)` binds the breaker through the pair record (D22.2), and
   affineness (D22.5) then keeps a second transfer from being expressible.

Nothing in this path needs a parser change, which is why it is the recorded
one. The legacy half needs the same rewriter hook (legacy's pipeline has its own
rewrites) and the same builtin case in its SSA builder.

### 22.4c The spawn agreement check regressed `chan string` vs `chan []byte`

The agreement check added in step 3b compares spawn arguments to the target's
parameters, and for non-ownership types it compared the RENDERED underlying
types. Moxie treats `string` and `[]byte` as one type, so a spawn whose target
takes `chan string` while the caller passes `chan []byte` (the pre-existing
fixture `spawn_closed_crosspkg`) is rejected by stage4 although it is legal, and
legacy's go/types check accepts it because go/types unifies the two spellings
where Moxie's checker does.

The fix is one line: compare with `Identical` (which unifies string/[]byte, and
now also compares generic instance arguments) instead of comparing rendered
strings. It is NOT landed because it cannot currently be compiled into a working
compiler: see 21.6 - every compiler built from the present sources panics at
start-up in `runtime.codecDecodeValue`, by both paths (stage4-built and
legacy-built), so the tree stays byte-identical to the sources of the working
`./mxc-stage4` binary until that bug is found. Recorded here so the next round
starts from the defect rather than rediscovering it.

### 22.4d The 8-byte object header needs phase 4's offset addressing

The per-object header (section 5) was implemented in `ArenaAlloc` during this
round and reverted: with `sizeof(header) == 8` and a 16-byte span (the payload
must stay 16-byte aligned or the compiler dies on its first `movaps`), the
verifier walked the root heap correctly (`root objects ok 623 624`), but SIX
regression units broke: `asn1_builder_der` produced wrong DER for a 128-bit
integer, `generic_aggregate_subst` substituted the wrong field, `http_header_api`,
`tls_handshake` and `spawn_closed_crosspkg` failed. The cause is the same one
phase 4 exists for: a header shifts every payload by 16 bytes, and code that
derives an address from a region base plus a recorded offset (hashmap buckets,
codec buffers, offset frames) no longer agrees with it. The header therefore
lands WITH the `%fn_base` offset addressing, not before it; its alignment rule
(span 16, size 8) and the verifier's walk shape are settled and recorded here.

### 22.4b Open defect found while landing step 3

stage4's `types.Identical` returned true for any two instantiations of one
generic whose origin matched, ignoring the type arguments: `Box[int32]` and
`Box[uint32]` - and a transposed Edge pair - compared equal, so assignments
legacy (go/types) rejects were accepted. FIXED: the Named case now compares the
origin, rejects a typed-nil `*Named`, and compares each type argument
recursively; `reject_generic_assign_mismatch.mx` pins it (both compilers say
"cannot use"). The earlier crash while attempting this was the typed-nil
dereference, not the comparison itself: with the guard in place the compiler
builds its own next generation (r50 → r51 completed).

### 22.5 Open decisions, recorded rather than guessed

- **The two-value `chan` form.** Whether `s, r := chan T{d}` replaces the
  single-value meaning or coexists with it (and how the compiler distinguishes
  them) is not decided. `edge` does not depend on it.
- **Hidden word in a builtin type's layout.** D22.2 needs one compiler-known
  word in `Edge`; whether it is part of the user-visible struct layout (and so
  `unsafe.Sizeof`) or kept in a side table is decided when the layout is
  emitted. The constraint is that it must not be reachable from user code as a
  field.
- **Constraints on builtin generic parameters.** They are unconstrained by
  definition; the universe entry must bypass the "concrete non-empty
  constraint" rule that applies to user generics, and the bypass must be a
  property of the builtin type, not a special case at every use site.
