# Moxie Memory Management

## No Garbage Collector

Moxie has no GC - no mark phase, no scan phase, no roots, no finalizers. Memory management is deterministic and structural: every allocation belongs to exactly one arena, and arenas are released in bulk.

## Domain Root

Each domain (process) has a single `DomainRoot` structure at a fixed TLS slot, created once at domain init. It contains:

- `fnStack[512]*Arena` - function arena stack
- `fnTop int32` - current stack top index
- `root Arena` - inline root arena (fnStack[0] = &root)

The active arena is always `domainRoot.fnStack[domainRoot.fnTop]`. All calls to `alloc(size)` allocate from this arena.

## Arena Structure

Each arena is a bump-pointer allocator with a flat region registry:

```
Arena struct:
    nregions int32
    regions  [256]ArenaRegion   // flat table, no linked lists

ArenaRegion:
    base uintptr   // mmap'd address
    off  uintptr   // bytes allocated (bump pointer)
    cap  uintptr   // region capacity
```

The Arena struct is embedded at offset 0 of its primary mmap region - no external allocation needed for metadata. When a region fills, a new 1MB region is mmap'd and added to the table.

`ArenaAlloc(arena, size)` bumps the active region's offset and returns zeroed memory. If the region is full, it chains to the next. If 256 regions are exhausted, panic.

## Per-Function Arena Lifecycle

Every function that allocates gets its own arena, pushed onto the domain's function arena stack. The compiler emits:

1. **Prologue**: `FnArenaPush(65536)` - acquires an arena from the pool or creates one via mmap, pushes to `fnStack[++fnTop]`
2. **Body**: all allocations bump within this function's private arena
3. **Epilogue**: three-pass mark-compact relocation of return values into the caller's arena, then `FnArenaPop` + `ArenaRelease` returns the arena to the pool

```
Call stack visualization:

    fnStack[0] = root arena (domain lifetime)
    fnStack[1] = main()'s arena
    fnStack[2] = f()'s arena        <-- fnTop during f()
    fnStack[3] = g()'s arena        <-- fnTop during g() called from f()

    When g() returns:
    - return values relocated into fnStack[2] (f's arena)
    - fnStack[3] released to pool
    - fnTop decremented to 2
```

### Return Value Relocation

When a function returns a value containing pointers or slices (anything that references arena memory), the compiler emits a three-pass mark-compact:

1. **Mark**: walk the return value's type graph, record all pointers into the function's arena into a `RelocEntry` linked list
2. **Forward**: pop the function arena from the stack (so the caller's arena becomes active), allocate forwarding space for each marked pointer in the caller's arena
3. **Compact + Fixup**: copy data to forwarding addresses, patch all internal pointers

If no return values contain pointers (pure scalars), the epilogue is just `FnArenaPopFree()` - pop and release with no relocation.

### Arena Pool

A bounded freelist (8 entries) of reset arenas eliminates mmap/munmap syscalls for short-lived function arenas:

- `ArenaAcquire(capacity)` - first-fit scan of pool, or `ArenaNew` if none fit
- `ArenaRelease(arena)` - reset (munmap overflow regions, reset primary offset) and return to pool; if pool full, `ArenaFree` (munmap everything)

## Ownership Rule

A function's allocations are invisible outside that function. The only way data escapes is through return values, which are explicitly relocated. This means:

- No escape analysis needed
- No pointer tracking across call boundaries
- No use-after-free possible (returned data is always a fresh copy in caller's arena)
- No dangling references into a called function's arena

## Ownership Boundaries

| Boundary | Mechanism | Direction |
|----------|-----------|-----------|
| Function return | Relocate into caller's arena | Callee -> Caller |
| Spawn | Codec serialization over IPC | Parent -> Child |
| Domain exit | munmap (bulk release) | Domain -> Kernel |

### Parameters

Function parameters are owned by the caller. The callee receives them by reference (pointer into the caller's arena). The relocation pass handles the case where a return value contains a parameter address - it gets relocated alongside everything else.

### Globals

Package-level variables are allocated in the root arena at domain init. They persist for the lifetime of the domain. Mutable globals are an anti-pattern because they accumulate allocations that never die (the root arena is never freed until domain exit). Package-scope `var` declarations must be zero-value only; initialization happens in `init()`.

## Domain Isolation

Each domain (spawned process) has its own `DomainRoot` and arena tree. When a domain exits, `munmap` releases all its address space to the kernel in one operation. There is no shared memory between domains.

Values crossing the spawn boundary are serialized via `moxie.Codec`. The child receives an independent copy in its own arena.

## Long-Running Event Loops

The per-function arena model means allocations inside `for { select { ... } }` loops are NOT automatically reclaimed between iterations (the function never returns and its arena is never released). For long-running loops:

1. Factor heavy allocation work into helper functions - their arena is acquired/released per call
2. `delete(map, key)` when entries are done
3. `slice = nil` releases the backing array reference
4. Monotonically growing maps need rotation or bounded-size policies

```moxie
func eventLoop(ch chan Request) {
    // this function's arena lives forever (never returns)
    for {
        select {
        case req := <-ch:
            handleRequest(req)  // <-- gets its own arena, released on return
        }
    }
}
```

## Summary

| Property | Value |
|----------|-------|
| GC | None |
| Allocator | Bump pointer within mmap'd arena regions |
| Arena structure | Flat [256]ArenaRegion table, no linked lists |
| Function lifetime | FnArenaPush at entry, relocate+release at return |
| Reclamation | Arena release per function (pooled), munmap per domain |
| Individual free | Does not exist |
| Escape analysis | Not needed (relocate on return) |
| Shared memory | None (domains are isolated processes) |
| Root arena lifetime | Domain lifetime |
| Arena pool | 8-entry bounded freelist |
