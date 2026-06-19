# Moxie Memory Management

## No Garbage Collector

Moxie has no GC - no mark phase, no scan phase, no roots, no finalizers. Memory management is deterministic and structural: every allocation belongs to exactly one arena, and arenas are released in bulk.

## Arena Model

All allocations go through a bump-pointer arena backed by `mmap`. The runtime maintains a `currentArena` pointer. When a function calls `alloc(size)`, the allocator bumps the arena offset and returns zeroed memory. There is no individual free - memory is reclaimed in bulk when an arena is reset or unmapped.

```
Arena layout:

    base                              off                             cap
    |                                  |                               |
    v                                  v                               v
    [====== allocated (zeroed) ========|-------- available ------------|]
    ^                                  ^
    mmap'd anonymous pages             bump pointer advances here
```

### Arena Lifecycle

1. `ArenaNew(capacity)` - mmaps anonymous pages, returns an Arena header
2. `ArenaAlloc(arena, size)` - bumps offset, returns zeroed pointer
3. `ArenaReset(arena)` - resets offset to zero (reuse without unmap)
4. `ArenaFree(arena)` - munmaps the backing pages, returns address space to kernel

### Root Arena

A root arena is created at process init (`initHeap` -> `initRootArena`). This is the process-lifetime arena. All allocations outside of function-scoped arenas go here.

## Per-Function Arena Ownership

Every function that allocates owns a region within the current arena. The compiler emits:

1. **Prologue**: `mark := ArenaMark()` - saves the current arena offset
2. **Body**: all allocations bump the arena normally
3. **Epilogue**: deep-copy return values to the caller's region, then `ArenaRestore(mark)` - reclaims everything the function allocated

```moxie
func buildList(n int32) (result *Node) {
    // compiler-emitted prologue: mark := ArenaMark()

    head := &Node{Val: 0}
    cur := head
    for i := int32(1); i < n; i++ {
        cur.Next = &Node{Val: i}
        cur = cur.Next
    }
    result = head

    // compiler-emitted epilogue:
    //   result = deepcopy(result)   -- copies to caller's region
    //   ArenaRestore(mark)          -- frees everything this function allocated
    return
}
```

After `ArenaRestore(mark)`, ALL memory between mark and the previous offset is reclaimed. The return value survives because it was deep-copied to the caller's region (above the mark) before the restore.

### Deep Copy at Return

When a function returns a pointer or slice that may reference arena memory allocated after the mark, the compiler emits a deep copy. The copy walks the type graph (structs, slices, pointers) and allocates fresh copies in the caller's region.

Two runtime helpers support this:
- `arenaCopyPtrIfLocal(ptr, mark, size)` - copies a struct if its address is above mark
- `arenaCopySliceIfLocal(s, mark)` - copies a slice's backing array if above mark

Data that was already in the caller's region (passed in as parameters, or allocated before this function was called) passes through without copying.

### Ownership Rule

A function's allocations are invisible outside that function. The only way data escapes a function is through return values, which are explicitly copied. This means:

- No escape analysis needed
- No pointer tracking across call boundaries
- No use-after-free possible (returned data is always a fresh copy)
- No dangling references into a called function's region

## Domain Isolation

Each domain (spawned process) has its own arena. When a domain exits, `munmap` releases all its address space to the kernel in one operation. There is no shared memory between domains.

Values crossing the spawn boundary are serialized via `moxie.Codec`. The child receives an independent copy in its own arena. The parent's memory is never accessible to the child.

## Ownership Boundaries

Moxie's ownership model has exactly three boundaries where memory changes hands:

| Boundary | Mechanism | Direction |
|----------|-----------|-----------|
| Function return | Deep copy to caller's region | Callee -> Caller |
| Spawn | Codec serialization | Parent -> Child |
| Domain exit | munmap (bulk release) | Domain -> Kernel |

### Parameters

Function parameters are owned by the caller. The callee receives them by reference (pointer into the caller's arena region). The callee must not store parameter addresses beyond its own return - the compiler's deep-copy-on-return handles this automatically.

### Globals

Package-level variables are allocated in the root arena at process init. They persist for the lifetime of the domain. Mutable globals are an anti-pattern because they accumulate allocations that never die (the root arena is never freed until domain exit). Package-scope `var` declarations must be zero-value only; initialization happens in `init()`.

## Long-Running Event Loops

The per-function mark/restore model means allocations inside `for { select { ... } }` loops are NOT automatically reclaimed between iterations (the function never returns). For long-running loops:

1. `delete(map, key)` when entries are done - `map[k] = nil` keeps the slot reachable
2. `slice = nil` releases the backing array - `slice = slice[:0]` retains it
3. Monotonically growing maps need rotation or bounded-size policies
4. Factor heavy allocation work into helper functions - their mark/restore will reclaim on return

```moxie
func eventLoop(ch chan Request) {
    // mark set here - this function runs forever, never restores
    for {
        select {
        case req := <-ch:
            handleRequest(req)  // <-- allocations inside here ARE reclaimed on return
        }
    }
}
```

## Summary

| Property | Value |
|----------|-------|
| GC | None |
| Allocator | Bump pointer within mmap'd arena |
| Reclamation | Bulk (ArenaRestore per function, munmap per domain) |
| Individual free | Does not exist |
| Escape analysis | Not needed (deep copy on return) |
| Shared memory | None (domains are isolated processes) |
| Root arena lifetime | Process lifetime |
| Function arena lifetime | Function entry to function return |
