# Memory model

Status: **implemented**. This document is the single description of how Moxie
manages memory. It replaces the earlier `ownership.md` sketch and the
`memory-model.md` / `memory.md` proposals: those described a target design
(`MAIN`, `SHELF`, `shelf T`, `%fn_base` offset arenas, `(epoch<<40)|counter`
identity, handle tables) that **does not exist in the tree**. Nothing here is a
proposal; where a claim is not yet true it is marked TARGET and listed in
[decisions.md](decisions.md).

The axioms force the shape: A1 says the release point must be derivable from
structure, A2 says memory is per-domain with one authority per fact. Their
intersection admits exactly one model (L19): a per-function arena released at
return, with the boundary values relocated. There is no heap, no GC, no
`free()`, and no startup bump allocator.

---

## 1. The arena stack

One domain = one thread = one arena stack, held in `DomainRoot` at a TLS slot
(`src/runtime/alloc.mx`):

```
DomainRoot:
    fnStack[512]*Arena   the arena stack
    fnTop int32          current top
    manualFrame []uint64 the slots pushed by ManualArenaEnter
    manualStack  []*Arena
    stableArena *Arena   the receiver arena of a read-only method
    stableStack  []*Arena
    sovereignRoot *hashmap  receiver address -> sovereign group
```

`alloc(size)` **never** goes anywhere but `fnStack[fnTop]`. There is no fallback
path: if `domainRoot` is unset, `alloc` panics.

Four kinds of arena sit on that stack.

| arena | pushed by | released by | holds |
|---|---|---|---|
| root | `initDomain` (once per domain) | domain exit (`munmap`) | anything that must outlive every frame: the domain's own tables, package globals, lazily-built caches |
| function | `FnArenaPush(cap)` at function entry | `FnArenaPop` (+`ArenaRelease`) at return | the callee's whole working set, including every intermediate a growth step abandons |
| manual phase | `ManualArenaEnter(a)` / `ManualArenaExit(a)` | explicit, in the same function | compiler phases: the package arena, the generic arena, the scan arena |
| sovereign | `SovereignSetArena(data)` in a mutating method's prologue | `SovereignRestoreArena` (the arena itself lives as long as its receiver) | a self-mutating receiver's own state |

A pool (`ArenaAcquire`) recycles released function arenas; `ArenaReset` empties
an arena back to its first region and `ArenaFree` unmaps it. An arena is never
reused *within* a frame: `FnArenaPop` reclaims the position for the next callee,
not for you, so every buffer a growth step abandons stays live until the frame
returns.

### Manual phase arenas

`ManualArenaEnter` marks the fn-stack slot in `DomainRoot.manualFrame`. The codec
tests that bit (`codecInManualFrame`) to decide whether a pointer into the arena
may be aliased: no return inside the phase can release it, because
`ManualArenaExit` is not a return. The slot is the authority, not the
`manualArena` global, which can diverge from `fnStack[fnTop]`.

---

## 2. Ownership rules

These are the language rules, and they are what the two compilers implement.

**Parameters are owned by the caller.** A parameter points into the caller's
arena. A callee may write through it (parameters are mutable) and the write is
visible to the caller, which is the point. A callee that stores a pointer to its
own frame through a parameter hands back a dangling pointer at return: the return
protocol relocates return values and receiver store-backs, and it knows nothing
about a parameter's target.

**Returns are created in the function arena, then relocated into the caller's
arena.** Everything in the frame that is not part of the tree attached to the
named results is gone at return. A named result slot is required
(`func f() (n int32)`), because that is the object the relocation writes.

**Only method receivers are tracked for store-back.** Writes to receiver fields
are serialized at return alongside the return values. A store through a
`*T` parameter or a package-level pointer is not tracked.

**Package-scoped variables are immutable outside `init()`.** Each domain gets its
own copy (`InitCShared` runs the initialisers), so a write after `init()` would
mean the same declaration has two meanings. That is a language error.

**A subslice is a copy.** `s[a:b]` allocates. A write through it does not reach
the parent, and `f(s[a:b])` where `f` writes through its parameter is the same
lost write. Authoring rule: never pass a parameter as a return value.

---

## 3. The return protocol (what actually runs)

The live protocol is **codec encode → `FnArenaPop` → decode**:

1. The callee encodes the named results (and receiver store-backs) with the
   structural codec into a buffer allocated in its own frame.
2. `FnArenaPopFree` releases the callee's frame.
3. The caller decodes into its own arena, rewriting pointers.

`src/runtime/codec.mx` is the implementation and both compilers emit calls to it
(`emitRetWithCodec` in `_mxc_stage4/ir_deepcopy.mx`). **The `Reloc*` functions in
`src/runtime/relocate.mx` are not emitted by either compiler**: `RelocInit`,
`RelocMark`, `RelocForward`, `RelocCompact` and `RelocFixupAll` have no caller.
They remain in the tree as a description of the ownership model, not as the
protocol. Any plan that says "preserve the three-pass mark/compact" is preserving
something that is not running.

Two decisions inside the codec make this affordable:

- **`codecRetStable` fast path.** A single result whose pointers are already
  stable is returned as-is (no encode, no decode). It handles pointer, bytes,
  slice, struct and interface results; the interface case is what makes an
  accessor returning `SSAValue` or `error` cheap.
- **A manual-phase frame is stable.** A pointer into a phase arena may be
  aliased, because no return inside the phase releases it. (It must still not be
  *stored* past `ManualArenaExit`; the phase exit is a real exit.)

### Codec dispatch policy

Chosen by the element type before any code is generated:

| element | route |
|---|---|
| no references (no pointer, slice, string, map, interface, func) | the extent is the message: `PipeChanSendRaw`/`PipeChanRecvRaw`, a memcpy of the bytes |
| references, and the type defines its own codec | `EncodeTo`/`DecodeFrom` run; the decode is the relocation |
| references, no custom codec | the builtin structural codec with back-references |

A channel or a function value is a handle, not a payload: its bytes are the
message. The predicate is `typeNeedsReloc` (legacy) / `elemNeedsReloc` (stage4)
and it is checked **first**.

---

## 4. Sovereign arenas

A **self-mutating type** is a named struct with at least one method that stores
into a receiver field, directly or through a method called on a receiver field.
Each such receiver owns a group:

```
sovGroup: data *Arena, allocTbl *Arena, recs []sovRecord, ...
```

held in `DomainRoot.sovereignRoot`, a root-arena hashmap keyed by receiver
address (no struct layout change). The group is never freed while the receiver
lives.

- **Mutating methods borrow.** The prologue calls `codecSovereignArena(recv)`
  (lazily creating the group), pushes `SovereignSetArena(data)` and opens an
  allocation batch. Every direct allocation lands in the group and is recorded
  with its type descriptor. The epilogue pops the borrow.
- **Read-only methods do not borrow.** They get a regular function arena, so
  their scratch dies at return, and they register the receiver's arena as
  *stable* (`codecRegStable`) so a slice or pointer they return is a view into
  receiver data rather than a deep copy.
- **Compaction is drained at safe points.** `chanSelect` calls
  `SovDrainCompactions(1)` before it looks at the arms; the compiler calls it
  between functions and between blocks. A group with a live frame is rotated to
  the tail and retried, never moved under that frame. A group whose data escaped
  (a pointer into it crossed a codec boundary) is not compacted.
- **Safe point for a sovereign type = the arena of its creator.** Its lifecycle
  ends with that arena unless it is returned, in which case it is bequeathed to
  the calling arena and inherently compacted by the relocation.

**Authoring rule: helpers for scratch.** A mutating method's direct allocations
persist. When it needs heavy scratch, push the work into a free helper function,
which gets its own frame arena, and write only the product back into the
receiver. A method body that builds large temporaries is a leak by construction.

---

## 5. Growth and arena discipline

`push` grows. From a zero-capacity slice it lazy-inits to exactly the first batch
(max(n, 4) elements) in one allocation; past capacity it doubles, moves the live
elements, and the abandoned backing stays in the arena that allocated it. That
garbage is reclaimed by the arena, not refused.

**There is no capacity guard and no growth helper.** `runtime.slicePush` takes an
element *count*, so `push(x, v)`, `push(x, a, b)` and `push(x, src...)` all grow
by themselves - a spread passes `len(src)` and the runtime makes room. Writing
`if len(x) >= cap(x) { ... }` or `x = mxutil.Ensure(x, 1)` around a push is
redundant, and for a pointer-bearing element type it is **quadratic**: `Ensure`
returns the slice, and a returned pointer-bearing slice is deep-copied by the
return protocol on every call, whether or not it grew.

The one exception is storage the program does not own: a `//:linkdata` binding
carries the all-ones sentinel in the raw capacity word and push/pop/resize panic
rather than write past the symbol.

Choose by how the final size is known:

| how the size is known | technique |
|---|---|
| one scan, or a `len()` of the input | presize `[]T{:n}`, write through a cursor |
| a second pass over a cheap predicate | presize, cursor write |
| a second pass over a user-supplied func | grow, but inside a free function |
| genuinely streaming | grow, but inside a free function |

An arena is never reused within a frame, so the peak is the **sum** of every
intermediate. Doubling to N costs roughly 2-3x N of arena; allocating N costs N.
Growth belongs where its garbage dies early.

**A helper must not return a growable slice of pointer-bearing values.** Either
presize and cursor-write, or write into the caller's map/struct through a
pointer and return a bool or a count.

---

## 6. The naming law

**D29: indirection is what you need when you cannot enumerate the references.
Persistence destroys enumerability.**

A value that can be reached from an unknown number of places cannot be moved, so
it cannot be compacted, so it cannot be reclaimed before its arena dies. That is
the whole reason a sovereign group has an escape flag, why `codecNoteStable`
exists, and why an escaped group is skipped by compaction rather than traced.
When a design reaches for a handle table or an indirection, the question to ask
first is why the references cannot be enumerated.

---

## 7. Reading the accounting

- `memLine(tag)` in `_mxc_stage4/main.mx` prints one line per phase: RSS, map
  count, root usage, registry sizes, codec call counts.
- `runtime.SovDomainReport(tag)` prints one compact line of arena accounting:
  retained bytes, net mapped bytes, live group bytes versus capacity, compaction
  count, pending compactions, and the top group's liveness.
- A `mmap` failure prints `out of memory: mmap N bytes failed, live M` and the
  asking callers, then aborts. That is deliberate: the alternative is a SIGKILL
  from the cgroup with no stack at all.

The box that runs every build is described in [building.md](building.md): 4 GB
ceiling, `MemoryHigh` below it so a runaway run throttles where it can still be
inspected.
