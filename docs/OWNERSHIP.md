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

Every function that allocates gets a region within the current arena. The
compiler emits a mark at entry and a restore at exit. Between mark and
restore, all allocations are the function's private property.

```
Arena state during nested calls:

    [root arena ─────────────────────────────────────────────────]
    [main's region][f's region][g's region]
                   ^mark_f     ^mark_g     ^current offset

    When g returns:
    [main's region][f's region][g's return values copied here]
                   ^mark_f     ^restored to mark_g
```

### Return = Transfer

A function cannot hold onto its allocations past its return. Anything it
wants the caller to have must be returned, and the compiler deep-copies
returned data into the caller's region before restoring the function's mark.

This is not overhead - it is the ownership transfer mechanism. The copy is
the proof that ownership moved. Without the copy, the data would die with
the function's region.

### Parameters = Borrowed References

Function parameters point into the caller's region. The callee borrows them
for the duration of the call. The callee can read and write through them
(mutation is fine - the caller owns the storage), but must not store their
addresses into structures that outlive the call. The deep-copy-on-return
handles the case where a return value contains a parameter address - it gets
copied into the caller's region alongside everything else.

### Local Variables = Private

Anything allocated inside a function that is NOT returned is private. It
ceases to exist when the function returns. No other code can name it, reach
it, or be affected by its death. This is sovereignty at the function scale.

---

## Domain-Scoped Isolation

Each domain is a process with its own arena. Domains communicate exclusively
through serialized IPC channels. The spawn boundary is a hard serialization
point - `moxie.Codec` enforces that everything crossing the boundary is
explicitly encoded and decoded. The child gets fresh allocations in its own
arena populated from the wire format.

### Why Not Shared Memory

Shared memory between processes is shared ownership of address space. Two
claimants, zero owners. Every shared-memory system requires synchronization
primitives (mutexes, atomics, memory fences) which are the runtime admission
that the ownership question was not resolved at compile time.

Moxie resolves it at compile time by forbidding it. A domain's memory is
sovereign. Nothing outside the domain can read or write it. Communication
happens by message, which means by copy, which means the sender and receiver
each own their own copy. Two owners of two copies is not shared ownership -
it is bilateral exchange (wood).

### Domain Death

When a domain exits - whether normally, by panic, or by signal - `munmap`
releases its entire arena to the kernel. No finalizers, no cleanup callbacks,
no reference counting, no grace period. The parent is notified on the control
channel (death is reported, not hidden - earth). The parent decides what the
death means.

---

## Globals Are Heaps

A mutable global is a heap in disguise. It accumulates allocations across
function calls with no mark/restore cycle to reclaim them. In Moxie:

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
all times. When a function returns, its data is either copied upward (owned
by caller) or destroyed (owned by nobody). Contested claims cannot form.

**Wood (bilateralism)**: no contract signed by one hand. The deep-copy-on-return
is a bilateral exchange - the callee provides the data, the compiler copies it
into the caller's region, both sides participate. Move semantics at spawn are
bilateral - the parent gives up the value, the child receives it.

**Fire (measurement)**: weigh it, count it, time it. Arena offsets are
measurable (`ArenaMark` returns a byte offset). Memory consumption is a
linear function of the arena offset - no hidden fragmentation, no GC
metadata, no free-list overhead. What you allocated is what you used.

---

## Consequences

### No Use-After-Free

A pointer into a function's region cannot escape that function except through
return (which copies it). A pointer into a domain cannot escape that domain
(spawn serializes). The temporal relationship between pointer validity and
owner lifetime is structural, not checked at runtime.

### No Memory Leaks (Within a Domain Lifetime)

Every function's allocations are reclaimed on return. The only accumulation
point is the root arena (globals) and the current function's stack frame in a
long-running loop. Both are bounded by design - globals are initialized once,
and loop bodies should delegate allocation to helper functions whose
mark/restore reclaims per-iteration.

### No GC Pauses

There is no GC. Arena restore is O(1) - a single pointer write. Arena free
is one syscall (munmap). Allocation is O(1) - a pointer bump and memzero.
Latency is deterministic and bounded.

### No Fragmentation

Bump allocation within a contiguous mmap'd region produces zero
fragmentation. When the arena is restored, the freed region is a single
contiguous block at the top. No compaction needed.

---

## Design Constraints

The arena model imposes real constraints:

1. **Return values are copied.** Deep functions returning large graphs pay
   a copy cost on every return. This is the price of ownership clarity.
   If profiling shows it matters, flatten the call chain.

2. **Long-running loops must delegate.** An infinite event loop that
   allocates without calling helper functions will grow the arena forever.
   The fix is structural: extract allocation into functions whose
   mark/restore reclaims per-iteration.

3. **No arena-to-arena references.** A pointer into one function's region
   must not be stored in another function's region without going through
   return (which copies). The compiler enforces this through the
   deep-copy-on-return mechanism.

4. **Globals are expensive.** Every byte written to a global lives until
   domain death. Use globals only for truly long-lived infrastructure.

These are not limitations - they are the shape of the design. A system that
makes ownership unambiguous necessarily constrains where state can live. The
constraints ARE the safety guarantee.
