# Ownership in Moxie

Memory ownership in Moxie is not a bolt-on safety system. It is the physical
expression of the language's design axiom: every piece of state has exactly
one owner, and ownership boundaries are enforced by the compiler and runtime
structurally - not by discipline, not by annotation, not by convention.

---

## The Principle

Two claimants on one piece of state means zero owners. This is the root cause
of every use-after-free, every data race, every dangling pointer, every GC
pause, and every memory leak in shared-ownership systems. Moxie eliminates the
entire class by making shared ownership inexpressible.

Ownership in Moxie operates at three nested scales:

1. **Function** - a function owns its allocations from entry to return
2. **Domain** - a domain (OS process) owns all memory within it
3. **System** - the kernel owns address space; domains lease it via mmap

At each scale, the same invariant holds: the owner's death releases
everything it owned, in one operation, with no survivors.

---

## Function-Scoped Arenas

Every function that allocates gets its **own arena**, pushed onto the domain's
function arena stack. The compiler emits `FnArenaPush` at entry and
`FnArenaPop` + `ArenaRelease` at exit. Between push and pop, all allocations
are the function's private property in its own mmap'd region.

```
Function arena stack during nested calls:

    fnStack[0] = root arena     (domain lifetime)
    fnStack[1] = main's arena
    fnStack[2] = f's arena      <-- fnTop during f()
    fnStack[3] = g's arena      <-- fnTop during g()

    When g returns:
    - return values relocated into fnStack[2] (f's arena)
    - fnStack[3] released to pool (or munmap'd)
    - fnTop decremented to 2
```

### Return = Transfer

A function cannot hold onto its allocations past its return. Anything it
wants the caller to have must be returned, and the compiler relocates
returned data into the caller's arena before releasing the function's arena.

This is not overhead - it is the ownership transfer mechanism. The relocation
is the proof that ownership moved. Without it, the data would die with
the function's arena.

### Parameters = Immutable Borrows

Function parameters point into the caller's arena. The callee borrows them
**read-only** for the duration of the call. Writing through a parameter
pointer is a compile error.

This prohibition exists because parameter mutation is structurally unsound
in a per-function arena model. When a callee writes through a parameter
pointer, it places arena-local allocations (which die on return) into
caller-owned structures (which outlive the call). The result is dangling
pointers in the caller's data - silent corruption that manifests as a crash
in unrelated code later.

The Go pattern of pointer receivers that mutate fields (`func (t *T) Add(v V)`)
is the canonical source of this bug class. In Moxie, methods that need to
produce modified state return it:

```moxie
// illegal: writes arena-local allocation into caller's struct
func (t *Table) Add(entry Entry) {
    push(t.entries, entry)    // compile error: mutation through parameter
}

// legal: return the new state, caller assigns
func (t *Table) WithEntry(entry Entry) (result *Table) {
    result = &Table{entries: t.entries | []Entry{entry}}
    return
}
```

The caller decides where the result lives - in its own arena, as a return
value that gets relocated upward, or assigned to a local that dies with the
function. Sovereignty stays with the longer-lived party.

### Local Variables = Private

Anything allocated inside a function that is NOT returned is private. It
ceases to exist when the function's arena is released. No other code can
name it, reach it, or be affected by its death. This is sovereignty at the
function scale.

---

## Domain-Scoped Isolation

Each domain is a thread with its own arena tree. Domains communicate through
channel handles: the rings are in shared memory, values are copied (or
codec-relocated when they carry references) into the receiving arena, and a
channel handle is the only shared capability. The spawn boundary is a
copy/relocation point, not a shared-pointer boundary.

### Why Not Shared Memory

Shared addressable state between domains is shared ownership of memory. Two
claimants, zero owners. A general shared heap would require synchronization
primitives (mutexes, atomics, fences) - the runtime admission that the
ownership question was not resolved at compile time.

Moxie resolves it at compile time by confining the shared window to the channel
rings: one writer and one reader per direction, a depth fixed by the
declaration, and no addressable object graph crossing. A domain's arena memory
is sovereign; nothing outside the domain can read or write it. Communication
happens by copy (or codec relocation), so the sender and receiver each own
their copy - bilateral exchange (wood), not shared ownership.

### Domain Death

When a domain exits - whether normally, by panic, or by signal - its arena tree
is released. No finalizers, no cleanup callbacks, no reference counting, no
grace period. The parent observes the domain breaker (death is reported, not
hidden - earth) and the ring liveness bits. The parent decides what the death
means.

---

## Globals Are Heaps

A mutable global is a heap in disguise. It accumulates allocations across
function calls with no arena-release cycle to reclaim them. In Moxie:

- Package-scope `var` declarations must be zero-value only
- Non-zero initialization happens in `init()`
- No package-scope assignment outside `init()` (compile error)

This constraint means the root arena holds only long-lived infrastructure
(type tables, package-level state initialized once). Functions that need to
persist state return it to their caller. The caller owns the lifetime. State
flows upward through returns, never sideways through globals.

---

## The Wu Xing Mapping

Each ownership rule maps to an element:

**Earth (sovereignty)**: what you control is yours. A function's region is its
territory. A domain's arena is its territory. Death releases territory
unconditionally. No negotiation with the dead.

**Metal (precision)**: give the stranger a key, not the house. Parameters are
borrowed references - precise access to the caller's region, not ownership of
it. Spawn arguments are Codec-serialized - the child gets exactly the data,
not a handle into the parent's memory.

**Water (resolution)**: what two claim, no one owns. The arena model makes
double-ownership inexpressible. Every pointer is inside exactly one arena at
all times. When a function returns, its data is either relocated upward (owned
by caller) or destroyed (owned by nobody). Contested claims cannot form.

**Wood (bilateralism)**: no contract signed by one hand. The relocation on return
is a bilateral exchange - the callee provides the data, the compiler relocates it
into the caller's arena, both sides participate. Move semantics at spawn are
bilateral - the parent gives up the value, the child receives it.

**Fire (measurement)**: weigh it, count it, time it. Arena region offsets are
measurable. Memory consumption is a linear function of the region offsets - no
hidden fragmentation, no GC metadata, no free-list overhead. What you allocated
is what you used.

---

## Consequences

### No Use-After-Free

A pointer into a function's arena cannot escape that function except through
return (which relocates it). A pointer into a domain cannot escape that domain
(spawn serializes). The temporal relationship between pointer validity and
owner lifetime is structural, not checked at runtime.

### No Memory Leaks (Within a Domain Lifetime)

Every function's allocations are reclaimed when its arena is released. The
only accumulation point is the root arena (globals) and the current function's
arena in a long-running loop. Both are bounded by design - globals are
initialized once, and loop bodies should delegate allocation to helper
functions whose arenas are released per-call.

### No GC Pauses

There is no GC. Arena release is pool-return (O(1)) or munmap (one syscall).
Allocation is O(1) - a pointer bump and memzero. Latency is deterministic
and bounded.

### No Fragmentation

Bump allocation within a contiguous mmap'd region produces zero
fragmentation. When an arena is released, its entire address space returns
to the kernel or is reset for reuse. No compaction needed.

---

## Design Constraints

The arena model imposes real constraints:

1. **Return values are relocated.** Deep functions returning large graphs pay
   a relocation cost on every return. This is the price of ownership clarity.
   If profiling shows it matters, flatten the call chain.

2. **Long-running loops must delegate.** An infinite event loop that
   allocates without calling helper functions will grow the arena forever.
   The fix is structural: extract allocation into functions whose
   arenas are released per-call.

3. **No cross-arena references.** A pointer into one function's arena
   must not be stored in another function's arena without going through
   return (which relocates). The compiler enforces this through the
   relocation mechanism.

4. **Globals are expensive.** Every byte written to a global lives until
   domain death. Use globals only for truly long-lived infrastructure.

These are not limitations - they are the shape of the design. A system that
makes ownership unambiguous necessarily constrains where state can live. The
constraints ARE the safety guarantee.
