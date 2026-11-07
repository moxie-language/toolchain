# Moxie Memory Model v2: Sovereign Receiver Arenas

## Problem

The per-function arena model has a boundary violation: when a method mutates its
receiver (`s.M[k] = v`), the stored value is allocated in the method's function
arena but must survive into the caller's arena. The current mechanism records
every receiver write in a table (`emitRecvWriteAppend`) and runs the full codec
serialization cascade on each entry. Encoding a `*TypeName` follows `Pkg ->
TCPackage -> Scope -> map -> ALL TypeNames` -- O(graph) per write. Type system
initialization takes 90+ seconds.

The root cause: the function arena is temporary (pushed at entry, popped at
exit), so every mutation to a receiver that outlives the call must be
serialized. N writes in a loop produce N independent O(graph) cascades.

## Solution

Give mutable objects their own persistent arenas. Methods borrow the receiver's
arena instead of creating a new function arena. Receiver writes become local
allocations within the sovereign arena -- no relocation, no codec, no write
table. The receiver write table and all its supporting code are deleted.

The codec retains full recursive serialization for spawn channel wire encoding
(portable across processes) and adds a relocate mode for the arena epilogue
(fast, skips sovereign pointer recursion within the same address space).

Zero new annotations. Zero pragmas. The compiler infers everything from
existing source.

## Core Architecture

Three arena tiers:

```
Root Arena (global, never freed)
    |
Function Arena (per call, freed at return, Reloc* relocates returns to parent)
    |
Sovereign Arena (per object, persists for object lifetime, NO relocation)
```

- **Root arena:** type descriptors, compile-time constants, string literals.
  Never freed.
- **Function arena:** temporary allocations, local variables. Push/pop per call.
  Three-pass relocation (RelocInit/RelocMark/RelocForward/RelocCompact/
  RelocFixupAll). Released at function return.
- **Sovereign arena:** receiver fields, map buckets, object state. Born with
  the object (`ArenaNew` in constructor). Methods borrow it via `SetCurrentArena`
  save/restore (same pattern hashmap already uses for owner-arena switching).
  Persistent until the owning object's arena is released or the domain exits.

## Sovereign Type Inference (Zero Annotations)

The compiler infers which types need sovereign arenas by scanning method bodies
during SSA construction. No `//go:sovereign` pragma.

Rule:

```
For each named struct T in the package:
  Scan all methods with receiver *T:
    If any method contains a Store instruction to a FieldAddr rooted
    at the receiver parameter
    AND typeNeedsCopy(T) is true
    THEN T is sovereign
```

`isReceiverAddr` already exists (compiler.go:1629) and detects writes to
receiver fields. `typeNeedsCopy` already exists (compiler.go:3908) and detects
pointer-containing types. The inference is a loop over SSA instructions per
type -- O(methods * instructions), negligible.

A method on a sovereign type automatically gets the sovereign prologue
(borrow receiver's arena) and epilogue (restore parent arena + relocate-mode
codec for return values). A method on a non-sovereign type gets the existing
fn arena push/pop. The developer writes normal methods, the compiler handles
arena strategy.

## `_arena` Field: Compiler-Injected, Zero-Visibility

The `_arena *runtime.Arena` pointer is NOT declared in source. The compiler
injects it during IR construction:

- **LLVM struct type:** field at index 0, type `*Arena` (pointer)
- **Type descriptor:** reports only user-visible fields (excludes `_arena`)
- **Codec:** enumerates only user-visible fields (excludes `_arena`)
- **Moxie syntax:** no access via `s._arena`, no field name, no reflection entry

The compiler accesses it via hardcoded `CreateStructGEP(type, val, 0, ...)`
in sovereign prologues and epilogues. It is invisible to all user code.

## Codec Modes: Relocate vs Serialize

Two modes, one codec, one buffer format:

| Mode | Use case | Sovereign pointer encoding | Portable |
|------|----------|---------------------------|----------|
| `codecModeRelocate` | Arena epilogue (fast, same process) | Raw 8-byte pointer copy | No |
| `codecModeSerialize` | Spawn channels (wire encoding) | Full recursive | Yes |

### Relocate mode (arena epilogue)

Sovereign pointer fields are written as raw 8-byte values. The decoder writes
these back as-is, producing a pointer into the same sovereign arena. Correct
because: the sovereign arena outlives the function call; the codec is decoded
within the same process; the caller already holds a reference to the sovereign
object via the return value.

No relocation passes needed for sovereign pointer fields. The encode/decode is
a trivial memcpy of 8 bytes per pointer field.

### Serialize mode (spawn channels)

All fields are fully recursively encoded, producing a self-contained byte
stream decodable in a different process with an independent address space.
Sovereign pointer fields are recursively encoded just like non-sovereign
pointer fields. The decoder allocates new copies in the receiving process's
arena.

The mode is a `uint8` field on `CodecBuf` and `codecDecodeSeen`. The arena
epilogue always uses relocate mode; spawn channel encode always uses serialize
mode.

## `//go:arena inherit` -- Unchanged, Orthogonal

Existing `//go:arena inherit` annotations on constructors (36 occurrences in
the type system) remain as-is. They are a micro-optimization that skips
FnArenaPush/FnArenaPop for functions that allocate directly into the caller's
arena. They are not required for sovereign arena correctness and are
orthogonal to this plan.

## Detailed Changes

### 1. Runtime: `metaSovereign` type flag

**`src/runtime/typekind.mx`** -- add sovereign detection:

```
const metaSovereign uint8 = 0x40

func (t *rawType) isSovereign() bool {
    return t.meta & metaSovereign != 0
}
```

### 2. Runtime: codec modes

**`src/runtime/codec.mx`** -- add mode constants and fields:

```
const codecModeRelocate  uint8 = 0
const codecModeSerialize uint8 = 1
```

Add `mode uint8` to `CodecBuf` and `codecDecodeSeen` structs.

### 3. Runtime: codec relocate mode in `kindStruct`

**`src/runtime/codec.mx`** -- in `codecEncodeValue`, `kindStruct` case:

When `typ.isSovereign() && cb.mode == codecModeRelocate`:
- Pointer fields: write `codecTagNil` for nil, `codecTagPtr` + raw 8 bytes for non-nil
- Non-pointer fields: recursive encode as normal
- The `_arena` field is invisible to the codec (type descriptor excludes it).
  It is accessed only via compiler-generated GEP(0) in prologues/epilogues.

When `cb.mode == codecModeSerialize` or type is non-sovereign:
- All fields recursively encoded (existing behavior)

Matching decode logic in `codecDecodeValue`, `kindStruct` case:
- Relocate mode: allocate struct in current arena, read raw 8 bytes for
  sovereign pointer fields, write directly to struct slots
- Serialize mode: allocate + recursive decode (existing behavior)

### 4. Compiler: sovereign type inference

**`legacy/compiler/compiler.go`** -- add function `isSovereignType(t types.Type) bool`
that checks the `metaSovereign` bit on the type descriptor.

**`legacy/compiler/interface.go`** -- during type descriptor generation for
named struct types, scan all methods with receiver `*T` for receiver field
writes using the SSA-level check (same logic as `isReceiverAddr`). If found
and `typeNeedsCopy(t)` is true, set `metaSovereign` in the metadata byte.

This pass runs first. Steps 5 and 6 (prologue/epilogue) query
`isSovereignType` which reads the bit set here.

### 5. Compiler: method prologue borrows receiver arena

**`legacy/compiler/compiler.go`** -- add fields to builder:

```
arenaBorrowed  bool       // true if method borrowed receiver's sovereign arena
parentArenaVal llvm.Value // alloca holding parent arena for epilogue restore
```

**`emitArenaPrologue`** -- before the `FnArenaPush` path, check if the receiver
type is sovereign:

```
if b.hasReceiver() {
    recvType := b.fn.Signature.Recv().Type()
    if isSovereignType(recvType) {
        recvVal := b.getValue(b.fn.Params[0], getPos(b.fn))
        recvLLVMType := b.getLLVMType(recvType)
        arenaGEP := b.CreateStructGEP(recvLLVMType, recvVal, 0, "sov_arena_gep")
        arenaVal := b.CreateLoad(b.dataPtrType, arenaGEP, "sov_arena")
        parentArena := b.createRuntimeCall("CurrentArena", nil, "sov_parent")
        b.parentArenaVal = parentArena
        b.createRuntimeCall("SetCurrentArena", []llvm.Value{arenaVal}, "")
        b.arenaActive = true
        b.arenaBorrowed = true
        return // skip FnArenaPush + write table init
    }
}
// existing FnArenaPush + write table init follows (unchanged)
```

### 6. Compiler: method epilogue -- restore arena, relocate-mode codec

**`emitArenaEpilogue`** -- short path for sovereign methods:

```
if b.arenaBorrowed {
    if len(instr.Results) == 0 {
        b.createRuntimeCall("SetCurrentArena", []llvm.Value{b.parentArenaVal}, "")
        b.CreateRetVoid()
        return
    }
    if !needsCodec {
        b.createRuntimeCall("SetCurrentArena", []llvm.Value{b.parentArenaVal}, "")
        b.CreateRet(retVal)
        return
    }
    // Encode return values with relocate mode. Sovereign pointer fields
    // are written as raw 8-byte values. Data lives in sovereign arena.
    ... encode with cb.mode = codecModeRelocate ...
    // Switch to parent arena before decode.
    b.createRuntimeCall("SetCurrentArena", []llvm.Value{b.parentArenaVal}, "")
    // Decode return values into parent arena. Sovereign pointer fields
    // are read as raw 8-byte values, producing valid pointers into the
    // sovereign arena.
    ... decode ...
    // NO FnArenaPop, NO ArenaRelease (borrowed, not owned)
    b.CreateRet(cur)
    return
}
```

The `SetCurrentArena` save/restore pattern is the same pattern hashmap
already uses for owner-arena switching (`src/runtime/hashmap.mx:323-328`).
Arena switching is distinct from arena lifetime management (the Reloc*
domain). See claude.md line 32-33.

### 7. Delete entire receiver write tracking infrastructure

**Removed from `legacy/compiler/compiler.go`:**

| Lines | What |
|-------|------|
| 218-220 | `recvWriteTable`, `recvWriteCount`, `recvWriteMax` fields in builder |
| 1464-1475 | Receiver write table initialization in `emitArenaPrologue` |
| 1629-1654 | `isReceiverAddr` -- kept (needed for inference, step 4) |
| 1656-1705 | `valueIsMethodAllocated` function |
| 1707-1741 | `emitRecvWriteAppend` function |
| 1831-1838 | Store instruction hook calling `emitRecvWriteAppend` |
| 4050-4058 | `codecEncodeRecvWrites` call in epilogue encode |
| 4101-4109 | `codecDecodeRecvWrites` call in epilogue decode |

Note: `isReceiverAddr` stays -- it's needed for sovereign type inference.

**Removed from `src/runtime/codec.mx`:**

| Lines | What |
|-------|------|
| 815-827 | `codecEncodeRecvWrites` function |
| 829-843 | `codecDecodeRecvWrites` function |

### 8. Compiler: error on non-sovereign receiver writes

After deleting the write table, non-sovereign types with mutating methods
would silently drop receiver writes. Add compile-time check: if a `Store`
instruction writes to a receiver field and:
1. The receiver type is NOT sovereign
2. The receiver type contains pointer fields (`typeNeedsCopy`)

...emit error: "receiver write on non-sovereign type T with pointer fields;
declare T as sovereign or copy to local variable first."

This catches the error at compile time instead of producing silent data
corruption. A non-sovereign type can still have methods that mutate primitive
fields (no pointer escape, no codec needed). Only pointer-containing types
require sovereign arenas for receiver writes.

### 9. Compiler: inject `_arena` into sovereign LLVM types

During LLVM type construction for a sovereign struct, the compiler prepends
the `_arena` field:

```
llvmType := b.ctx.StructType(
    []llvm.Type{b.dataPtrType, userField1, userField2, ...},
    false,
)
```

The type descriptor reports only user-visible fields. The codec enumerates
only user-visible fields. The `_arena` field at index 0 is accessible only
via compiler-generated GEP(0) in prologues/epilogues.

### 10. Constructors: auto-inject `_arena` initialization

The compiler detects constructor patterns and injects `_arena` field
initialization automatically. No source changes, no intrinsics.

Detection rule: a function returning `*T` where `T` is sovereign AND the
function body contains `&T{}` (or equivalent struct literal allocation).

The compiler emits, immediately after the `&T{}` allocation:
1. Call `runtime.ArenaNew(65536)`
2. Store result to GEP(0) of the allocated `*T`

The source code is completely unchanged from today. Example unchanged source:

```
func NewScope(parent *Scope) (s *Scope) {
    s = &Scope{}
    s.parent = parent
    s.elems = map[string]*TypeName{}
    return s
}
```

The compiler transforms this into the equivalent of:

```
func NewScope(parent *Scope) (s *Scope) {
    s = &Scope{}
    // injected by compiler:
    //   GEP(0, s) = ArenaNew(65536)
    s.parent = parent
    s.elems = map[string]*TypeName{}
    return s
}
```

The `ArenaNew` call returns an arena pointer; the store to GEP(0) sets the
injected field. The `//go:arena inherit` annotation on the constructor
(if present) remains an orthogonal micro-optimization.

## Expected Impact

| Metric | Before | After |
|--------|--------|-------|
| `resolveAll` for minimal package | 90s+ | <1s |
| Codec cascade per `Scope.Insert` | Full type graph | 0 (no write table) |
| Receiver write table entries | Up to 16 per method | 0 (deleted) |
| Lines of code change | - | -200 (delete) + ~150 (new) |
| Arena lifetime | Per function call | Per object (sovereign), per call (regular) |
| Spawn channel serialization | Not implemented | Full recursive (`codecModeSerialize`) |
| New pragmas required | - | 0 (compiler-inferred) |

## Execution Order

1. **`src/runtime/typekind.mx`:** Add `metaSovereign = 0x40`, `isSovereign()` method
2. **`src/runtime/codec.mx`:** Add `codecModeRelocate`/`codecModeSerialize`
   constants, `mode` field to `CodecBuf` and `codecDecodeSeen`, sovereign
   pointer skip logic in `kindStruct` encode/decode for relocate mode
3. **`legacy/compiler/symbol.go`:** No changes (no pragma needed)
4. **`legacy/compiler/interface.go`:** Add sovereign inference pass: scan
   named struct methods for receiver field writes, set `metaSovereign`
5. **`legacy/compiler/compiler.go`:** Add `arenaBorrowed`, `parentArenaVal`
   fields, sovereign prologue (borrow receiver arena via GEP(0)),
   sovereign epilogue (restore parent arena, relocate-mode codec)
6. **`legacy/compiler/compiler.go`:** Inject `_arena *runtime.Arena` at
   struct index 0 in LLVM type generation for sovereign types
7. **`legacy/compiler/compiler.go`:** Add compile-time error for receiver
   writes on non-sovereign pointer-containing types
8. **`legacy/compiler/compiler.go`:** Delete receiver write tracking
   (6 functions/fields, Store hook, epilogue calls)
9. **`src/runtime/codec.mx`:** Delete receiver write encode/decode functions
10. **`legacy/compiler/compiler.go`:** Auto-inject `_arena` field
    initialization in constructors (detect `&T{}` where T is sovereign,
    emit GEP(0) store of `ArenaNew` result)
11. **Test:** Write `src/runtime/codec_test.mx` with ~32 test cases covering:
    - Relocate mode: sovereign struct encode/decode round-trip
    - Relocate mode: sovereign struct with nil pointer fields
    - Relocate mode: non-sovereign struct encode/decode (unchanged)
    - Serialize mode: sovereign struct encode/decode (full recursive)
    - Serialize mode: spawn channel boundaries
    - Write table: verify zero entries for sovereign methods
12. **Verify:** `moxie build` on pkg/types, then `moxie test src/runtime/`
    Full bootstrap: stage1 build -> stage1 compiles pkg/types in <2s

## Design Notes

- **Constructor arena inheritance:** Existing `//go:arena inherit` annotations
  are kept. They are a micro-optimization (saves one fn arena push/pop per
  constructor call, ~microseconds). Not required for correctness. Can be
  eliminated later via compiler inference of the "returns pointer to local
  allocation" pattern.

- **Sovereign arena cleanup:** Sovereign arenas persist for object lifetime.
  For root-persistent objects (type descriptors), the arena lives until
  domain exit (munmap). For shorter-lived objects, the arena is registered
  in the creating function's arena cleanup list. If the constructor's return
  value is stored in a persistent location (detected by escape analysis), the
  arena is removed from the cleanup list. Otherwise, the arena is released when
  the fn arena is popped. Stack-like deterministic cleanup, no reference
  counting.

- **Block-scoped arena marks (follow-up):** `{}` boundaries in method bodies
  can generate arena save/restore marks, allowing allocations within blocks
  to be reclaimed at `}` unless they escape to receiver fields. This reduces
  peak memory in long-lived method loops but is not required for the type
  init fix. Deferred to follow-up.

- **Constructor `_arena` injection:** The compiler detects `&T{}` patterns in
  functions returning `*T` where `T` is sovereign. It emits a GEP(0) store
  of `ArenaNew(capacity)` before the rest of the constructor body. No source
  changes needed. No `SetStructArena` intrinsic needed. The existing `&T{}`
  pattern Just Works, with the arena silently added by the compiler.
