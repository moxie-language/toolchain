# Moxie Memory: Layout Reference

Companion to `memory-model.md`, which carries the rationale, the decisions of
record and the migration plan. This document carries the bits: field widths,
offsets, alignment, and the differences between the native 64-bit and wasm32
backends.

Status: specification, revision 3. Sections marked IMPLEMENTED match the tree today;
sections marked TARGET are the design of record and land per the phases in
`memory-model.md` section 16.

**Revision 3 vocabulary.** There is no root heap. `MAIN` is the process region
(mapped at start-up, never reclaimed, never moved). `SHELF` is a declared store —
a value you construct and hold — and it is the only region that is compacted. The
compiler declares no shelf (memory-model.md 23), so nothing in the layout of a
compiler binary depends on one.

---

## 1. Pointer classes

Four ways to name memory. Which one you have determines what the collector can do
to it, so this table is the whole design in one place.

```
  class        width          base            moves?   fixup on move
  -----------  -------------  --------------  -------  ---------------------------
  offset       u32            fn arena base   no       never
  address      uintptr (8/4)  none            depends  only inside a shelf
  handle       (tag,id)       id table        yes      resolved, not rewritten
  extent       -              -               no       copied whole

  offset   inside a function arena. base lives in %fn_base. Never relocated.
  address  inside MAIN (never moves, never rewritten) or inside a shelf
           (relocated by the sweep, rewritten in phase 3). The two are
           distinguished by the segment kind, not by the pointer.
  handle   across a store boundary. Resolved through the id table. Never stored
           as a raw address in the hot path.
  extent   a pointer-free byte range crossing a channel. Copied, not shared.
```

The object header contains no pointers, so its width is identical on both backends.
That is deliberate: the encoding does not change with the platform.

---

## 2. Tier map

Six regions with different lifetimes and different reclamation.

```
  +----------------------------------------------------------------+
  |  MAIN             process lifetime                             |
  |                   one mapping at start-up, NEVER reclaimed      |
  |                   never moved, never scanned, never swept       |
  +----------------------------------------------------------------+
  |  SHELF            the store's lifetime, declared by its owner   |
  |                   1 MiB native / 64 KiB wasm32                  |
  |                   append-only, raw addresses, COMPACTED         |
  +----------------------------------------------------------------+
  |  FUNCTION ARENAS  function lifetime                             |
  |                   64 KiB chunks, bump allocator, IMMOBILE      |
  +----------------------------------------------------------------+
  |  LARGE            arbitrary                                    |
  |                   one mapping per object, PINNED, freed whole  |
  +----------------------------------------------------------------+
  |  RING REGIONS     channel lifetime                             |
  |                   dedicated mapping, MAP_SHARED, refcounted    |
  +----------------------------------------------------------------+
  |  SECRET PAGES     arbitrary                                    |
  |                   guard|data|guard, mlock, wiped on release    |
  +----------------------------------------------------------------+
```

Region selection is by `kind`, and the sweep asserts `kind == SHELF` before
selecting any victim, so a nearly-empty FN or RING region cannot be chosen on dead
ratio alone. A binary with no `shelf` declaration has no SHELF region at all.

---

## 3. Function arena

### Descriptor

```
  offset  field       type      notes
  ------  ----------  --------  ------------------------------------------
  0       base        uintptr   base of the active chunk
  8/4     bump        u32       bytes used in the active chunk
  12/8    limit       u32       capacity of the active chunk
  16/12   prev        *Arena    enclosing arena, for pop
  24/16   chunk_kind  u8        CHUNK_FN | CHUNK_LARGE
  25/17   pad[7]      u8

  native: 32 bytes. wasm32: 24 bytes. Payload is 8-byte aligned on both.
```

### Stack

Arena push and pop is a stack, not a tree.

```
  fnStack[0]  main region           <- process lifetime, never popped
  fnStack[1]  main()                <- 64 KiB chunk
  fnStack[2]  f()                   <- 64 KiB chunk
  fnStack[3]  g()                   <- fnTop

  chunk layout:

  +--------+--------+--------+--------+ - - - +--------+
  | 64 KiB | 64 KiB | 64 KiB |        |       | 64 KiB |
  +--------+--------+--------+--------+ - - - +--------+
  ^base            ^bump                      ^limit

  on pop: bump is discarded wholesale. no per-object work at all.
  on grow: a new 64 KiB chunk is chained; prev keeps the old one.
```

### Why offsets

```
  object inside function arena:

      %fn_base = arena.base          (register or frame slot)
      obj      = %fn_base + 40       (u32 offset)

  moving the arena would cost one register write, not a walk. The cleaner never
  moves it, so it costs nothing at all. No pointer inside a function arena is
  ever rewritten, at return or at any other time.
```

### Return as transfer

```
  callee arena               caller arena
  +-----------+   memcpy    +-----------+
  | result    | ----------> | result    |
  +-----------+  (offsets)  +-----------+
        |
        v  pop, whole chunk returned to the pool

  Both sides are offset-addressed, so the transfer is a flat copy with one
  differing base. There is no fixup pass, which is what removes the structural
  walk in ir_deepcopy.mx.
```

---

## 4. Shelf and main

### Segment descriptor

```
  offset  field         type      notes
  ------  ------------  --------  ----------------------------------------
  0       base          uintptr   mapping base
  8/4     size          u32       fixed: 1 MiB native, 64 KiB wasm32
  12/8    bump          u32       next free byte
  16/12   live_bytes    u32       excludes headers? no: includes them
  20/16   owner         u32       domain id, or the shelf handle id
  24/20   generation    u32       incremented on each compaction
  28/24   next_in_log   u32       append order, for deterministic iteration
  32/28   kind          u8        MAIN | FN | SHELF | LARGE | RING | FREE
  33/29   state         u8        LIVE | EVACUATING | FREE
  34/30   pad[2]        u8

  native: 40 bytes. wasm32: 32 bytes.
```

Segments are held in a flat array, not a linked list. Victim selection scans the
array; `next_in_log` gives a stable iteration order so compaction is deterministic.

### Object header, exact

Every object in MAIN, in a shelf, in a function arena and in a LARGE mapping
begins with the same 8 bytes. There are no pointers in it, so both backends agree.

```
  offset  width  field   notes
  ------  -----  ------  ---------------------------------------------
  0       2      size    payload bytes, u16. 65535 max; larger -> LARGE
  2       2      kind    index into the layout table, u16
  4       1      flags   bit 0 PINNED, bit 1 EXTERNAL, bit 2 HAS_ID
  5       1      rsv     must be zero; assert on read in debug
  6       2      pad     zero, keeps payload 8-byte aligned
  ------  -----  ------
  total   8

  payload starts at +8, aligned to 8.
  owner is NOT here: it is a property of the segment.
  id  is NOT here: it lives in the side table, only when HAS_ID is set.
  forward is NOT here: see section 7.
```

### Packing

```
  segment (1 MiB native)
  +--------+-------------+--------+--------+-----+-------------+
  | hdr    | payload     | hdr    | payload| ... |   free      |
  | 8      | n           | 8      | m      |     |             |
  +--------+-------------+--------+--------+-----+-------------+
  ^base    ^+8                       ^bump                    ^+size

  append only. no free list, no coalescing, no external fragmentation.
  the only waste is dead objects between live ones, which is the cleaner's job.
```

### Size classes

```
  payload  header  total  overhead
  -------  ------  -----  --------
  16       8       24     50.0%
  32       8       40     25.0%
  64       8       72     12.5%
  256      8       264    3.1%
  4096     8       4104   0.2%

  If more than 40% of allocations are <= 32 bytes, the slab layout in
  memory-model.md 6.6 fires: kind and flags move to per-slab byte arrays and the
  header drops to zero. That is a measured decision, not a default.
```

---

## 5. Identity table

Sparse. Only objects that have been handed out across an arena boundary get an id.

```
  id = (epoch << 40) | counter          u64

  epoch    24 bits   increments per process start, prevents collision across runs
  counter  40 bits   monotonic per domain, ~1.1e12 objects per run

  two maps, both keyed on their own field:

  address -> id     used by the cleaner during evacuation:
                    if flags & HAS_ID, move the entry to the new address
  id -> address     used by handle resolution

  Neither map is ever consulted to decide liveness. Liveness is marking from
  roots. This is the one prohibition in the design.
```

---

## 6. Ring region

### Header, 128-byte stride

Cache-line separated so the two sides never share a line in a hot path. 128 rather
than 64 so it is correct on architectures with 128-byte lines.

```
  offset  width  field         writer        reader
  ------  -----  ------------  ------------  ------------
  0       4      writeIdx      producer      reads
  4       4      sentFrames    producer      reads
  8       120    pad           -             -
  ------  -----  ------------  ------------  ------------
  128     4      readIdx       reads         consumer
  132     4      recvFrames    reads         consumer
  136     120    pad           -             -
  ------  -----  ------------  ------------  ------------
  256     4      size          set at creation
  260     4      closed        either
  264     4      recvSleeper   reader hint
  268     4      sendSleeper   writer hint
  272     4      refcount      one per end, atomic
  276     4      codec         layout id, 0 = raw extent
  280     104    pad           -             -
  ------  -----  ------------  ------------  ------------
  384     size   data          -
```

```
  +================+================+================+----------------+
  | producer line  | consumer line  |  shared line   |  data area     |
  | writeIdx       | readIdx        | size, closed   |  2^N bytes     |
  | sentFrames     | recvFrames     | sleepers, rc   |  lazily faulted|
  +================+================+================+----------------+
  0                128              256              384
```

Sizing: 128 KiB native, 64 KiB wasm32. Independent of message size because frames
chain. `sentFrames - recvFrames` is the depth and is checked against the declared
capacity from `chan T{depth}`.

### Frame

```
  offset  width   field     notes
  ------  ------  --------  ----------------------------------------
  0       4       len       payload bytes
  4       4       flags     bit 0 CONTINUATION, bit 1 FINAL
  8       len     payload
  ...     pad     align 8

  A message larger than the contiguous free space is split across frames, each
  marked CONTINUATION, the last marked FINAL. The writer advances writeIdx once
  per frame, and that store is the only synchronisation needed, because there is
  exactly one writer.
```

### Layout note on the current implementation

`ringbuf.mx` today is a 32-byte header with `writeIdx` at [0:4] and `readIdx` at
[4:8], which is one cache line. That is a false-sharing bug and the relayout above
fixes it. It also sizes the data area at 32 MiB to fit `ringMaxMsg`, which
chaining removes.

---

## 7. Secret pages

```
  +-------------+---------------------+-------------+
  | guard page  | data page(s)        | guard page  |
  | PROT_NONE   | PROT_RW, mlock'd    | PROT_NONE   |
  +-------------+---------------------+-------------+

  native only. wasm32 has no mprotect, no mlock and no signal handler, so the
  secret tier does not exist there; a build that needs it fails at compile time
  rather than silently degrading.
```

Invariants: never a compaction victim, never scanned, zeroed unconditionally
before the page returns to the OS. An object with secret material is allocated
entirely inside the secret page, never split across tiers.

---

## 8. Compaction

Runs on one shelf, on its owner's thread, at the owner's safepoint. Never touches
function arenas and never touches MAIN.

### Phases

```
  phase 0  select
           victims = SHELF segments where dead/size > 0.5
           sorted by dead ratio desc, then segment id asc
           trim until live_bytes fits in free non-victim space
           mark all victims EVACUATING

  phase 1  mark
           worklist := root set
           while worklist:
               o = pop; if marked: continue
               mark(o)
               for off in layout(kind(o)).ptr_offsets:
                   if *(o+off): worklist.push(*(o+off))

  phase 2  evacuate
           for o in victims where marked:
               dst = alloc_silent(non_victim, size(o), kind(o))
               memcpy(dst, o, 8 + size(o))
               if flags(o) & HAS_ID: id_table.move(address(o), dst)
               store_u32(o, dst - segment(o).base)      <- forwarding
           # every live object in every victim is now forwarded

  phase 3  fixup
           for slot in root set:            *slot = resolve(*slot)
           for o in all live objects:
               for off in layout(kind(o)).ptr_offsets:
                   *(o+off) = resolve(*(o+off))
           for h in handles:                h.target = resolve(h.target)

  phase 4  reclaim
           for v in victims: v.generation++; v.state = FREE
```

### Forwarding, and why it overwrites

```
  before phase 2:            after phase 2:
  +--------+--------+        +--------+--------+
  | hdr    | payload|        | dst_off| ~~~~~  |
  +--------+--------+        +--------+--------+
   ^source                    ^source, now a forward pointer

  resolve(p):
      s = segment_of(p)
      if s.state != EVACUATING: return p
      return s.base + load_u32(p)

  One comparison. This works only because phase 2 completes entirely before
  phase 3 begins, so a pointer into an EVACUATING segment is always forwarded.
  Partial evacuation would break it, which is why incremental mode steps one
  whole segment at a time.
```

### Invariants

```
  I1  victims are evacuated only into non-victim segments
  I2  every live object in a victim is forwarded before phase 3
  I3  all live objects are marked before any is moved
  I4  only kind == SHELF is ever a victim

  I1 keeps forwarding terminal: no chains, no path compression.
  I2 makes resolve a single load.
  I3 is required because marking reads kind and evacuation destroys the source.
  I4 is asserted rather than inferred from dead ratio.
```

---

## 9. Platform matrix

```
  property            native 64-bit              wasm32
  ------------------  ---------------------------  ----------------------------
  pointer             8 bytes                      4 bytes
  page                4 KiB (16 KiB on some arm64) 64 KiB (wasm page)
  map                 mmap                          memory.grow
  unmap               munmap, returned to OS        never returned; pool only
  shelf segment       1 MiB                        64 KiB
  fn chunk            64 KiB                       64 KiB
  ring data           128 KiB                      64 KiB
  atomics             lock-free u32                Atomics on SharedArrayBuffer
  wait                futex / futex_waitv          Atomics.wait
  secret pages        mprotect + mlock + sigaction not available
  object header       8 bytes                      8 bytes
  forwarding          u32 segment offset           u32 segment offset
  segment descriptor  40 bytes                     32 bytes
  arena descriptor    32 bytes                     24 bytes
```

The two rows that matter most: **unmap never returns memory on wasm**, so the
segment pool is the only reclamation there and compaction is load-bearing rather
than an optimisation; and the **object header is platform-invariant**, so the
encoding, the layout table and the marker have one implementation.

The wasm failure this addresses is documented in `mem_wasm.mx`: chained 64 MB
regions per overflowing frame exhausted 4 GiB inside the NIST P-521 generator
table before the program produced output.

---

## 10. Budget arithmetic

Worked for the native backend, 64 KiB function chunks and 1 MiB shelf segments.

```
  function arena
    chunk                     65536 B
    usable per chunk          65536 - 0                      (bump, no header)
    objects per chunk at 64 B (8 hdr + 64 payload)
                              65536 / 72  = 910

  shelf, 512 KiB live in 1 MiB of segments (50% utilisation)
    dead ratio                0.5                -> sweepable
    free segments required    20% of capacity for the sweep to find a destination
    sweep CPU                 proportional to live bytes moved, not freed
    ceiling                   the declared capacity, enforced; there is no growth
                              path, because growth is what a shelf must answer for
```

```
  metadata per object at various payloads (shelf)
    32 B payload  ->  8 B header + ~2 B id-table amortised = 31%
    64 B payload  ->  8 B header + ~1 B                  = 14%
    1 KiB payload ->  8 B header                        = 0.8%
```

Ring memory, with chaining in place:

```
  per channel direction      128 KiB VA, ~a few pages resident
  per message                bounded by receiver memory, not by the ring
```

---

## 11. Checklist for an implementer

```
  [ ] object header is exactly 8 bytes, asserted at compile time
  [ ] payload is 8-byte aligned on both backends
  [ ] function arena pointers are offsets, never addresses
  [ ] a shelf is CONSTRUCTED in scope, never found, never defaulted (D21)
  [ ] a program with no shelf declaration maps no SHELF segment (test 33)
  [ ] only SHELF segments are victims (I4)
  [ ] victims are never destinations (I1)
  [ ] phase 2 completes before phase 3 (I2)
  [ ] marking completes before moving (I3)
  [ ] the id table is never consulted to decide liveness
  [ ] ring writer and reader lines are 128 bytes apart
  [ ] a ring has exactly one writer, enforced by affineness
  [ ] secret pages are never scanned and are zeroed on release
  [ ] wasm builds never call munmap-for-real; regions return to the pool
  [ ] MAIN is never a victim and never moved
```
