# Moxie Memory Management

## No Garbage Collector

Moxie uses a block-scoped ownership model. No GC, no escape analysis. Every allocation has a deterministic lifetime from source structure.

## Block-Scoped Deallocation

Every `{}` block that contains statements is a scope boundary. Variables created within a block are freed at block exit:

```moxie
func example() {
    a := int32(1)              // scope: function body
    if cond {
        b := []byte{:1024}    // scope: if-block
    }                          // b freed here
}                              // a freed here
```

Variables allocated at declaration point in execution order. No hoisting.

## Function Parameters and Returns

- Parameters: owned by caller, not callee. Arrive already allocated.
- Return values: survive function return, become caller's responsibility.

## Scope Promotion via Pointer

When a pointer to an inner-scope variable is assigned to an outer-scope variable, the inner variable's scope is promoted:

```moxie
func example() (result *Item) {
    for _, v := range items {
        item := &Item{v}      // initially for-block scope
        result = item         // scope promoted to function body
    }                         // item NOT freed (promoted)
    return result             // survives via return
}
```

No escape analysis needed - compiler sees pointer assignment and moves scope tag. Local, syntactic operation.

## free() Builtin

Explicitly ends a variable's lifetime before block exit:

```moxie
func process() {
    buf := []byte{:65536}
    // ... use buf ...
    free(buf)
    // buf is dead - using it here = compile error
    buf = []byte{:128}    // legal: new lifetime
}
```

After `free(v)`, using `v` without reassignment = compile error. Reassigning starts a new lifetime.

Essential for long-running event loops with `select` - large allocations should be freed rather than waiting for block exit.

## Between Domains

- No shared memory between domains.
- Values crossing spawn boundary serialized via `moxie.Codec`.
- Child receives independent copy.
- When a domain exits, all its memory is released.

## Long-Running WASM Workers

The WASM target's mark phase uses recursive depth-first scan with a fixed call stack (~1MB). Large live heap graphs overflow the stack.

Rules for long-lived WASM workers:
1. `delete(map, key)` is mandatory when done. `map[k] = nil` keeps slot reachable.
2. Every add path needs a corresponding remove path.
3. Clean ALL containers at the same boundary.
4. `slice = nil` releases backing array. `slice = slice[:0]` retains allocation.
5. Monotonically growing maps need delete paths or two-generation rotation.
