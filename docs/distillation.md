# Distillation: decomposing Moxie into smaller compilation steps

Status: analysis and change map only. Nothing here is implemented, and no
compiler behaviour is changed by this document.

The goal is to turn "compile Moxie" into a chain of small programs, each of
which rewrites valid Moxie into simpler valid Moxie, until the last program
compiles a language small enough that its front end, its IR and its LLVM
backend can each be understood on their own. The final subset is still Moxie
source: structs, fixed-width scalars, pointers, functions and `unsafe`, with
slices, maps, channels, strings, interfaces and generics already gone from the
text.

This document answers three questions:

1. What are the stages, and in what order do they run?
2. For every stage, which existing files and functions already do that work?
3. What has to move, be added, or be deleted for the stage boundaries to be
   real?

Where each part of the brief is answered:

| brief | section |
| --- | --- |
| generics rewritten away by a template generator | §4.1 |
| interface access/resolution lowered to per-type dispatch | §4.2 |
| the core language under them | §5 |
| core compiler split into AST, IR and LLVM steps | §4.9, §4.10, §4.11 |
| slices/maps/channels boiled down to blocks, accessors, iterators | §4.3-§4.7, §5.2 |
| the whole chain as Moxie-to-simpler-Moxie | §1.1, §6.3 |
| the final form is valid Moxie, memory blobs use `unsafe` | §3, §5 |
| where every part currently lives | §1.4, §2, and each stage's "what exists today" |

---

## 0. Objective, and what this plan does not achieve

### 0.1 The objective

This plan serves **modularity, independent comprehension, testability and
incremental replacement** of the compiler. It does **not** reach the 64 KB
scratchpad target, and it is not a plan for the small-machine port.

The arithmetic, using the ledger in §7:

- the stage4 compiler is 45,555 lines;
- the ledger removes roughly 12,000-14,000 lines of feature code and moves
  about 4,000 more into generated source or the driver;
- the core that remains is about 28,500 lines, roughly 2.2 MB of source at the
  density measured earlier (~77 bytes/line);
- 2.2 MB / 64 KB is about **35x**.

The gap is structural. The parser (4,847 lines) and the type model/checker
(3,877 lines) stay in the core, and neither is reducible by seam-cutting. A
64 KB compiler is a different design: delete features (generics, interfaces,
slices, maps, channels, strings, closures, defer), drop LLVM and SSA in favour
of a direct RV32 (or other) emitter, and accept a subset language with no
compatibility promise. That is a valid destination, but it is a different plan,
and it starts by choosing what to remove rather than where to cut.

If a reader takes the distillation framing as "smallness", §7 will disappoint
them. Read this document as: one monolithic compiler becomes a chain of small
compilers, each independently comprehensible, testable, cacheable and
replaceable, with behaviour held identical by differential tests. Size falls as
a side effect; it does not approach 64 KB.

What it does buy: a stage can be read and changed without holding the whole
compiler; a stage can be tested against its input/output alone; a stage's
product is a cacheable source tree that the next stage consumes; a stage can be
turned off (`-pipeline`, §6.11) when it breaks; and the monolithic path remains
the oracle while each stage is proven identical. Those are the returns on the
22,000-40,000 new lines the chain costs (§8.1). The deletion ledger's 12-14k
does not pay for that in size.

### 0.2 Corrections to older notes

Three facts overturn assumptions a reader may bring from older project notes.
They are promoted here because they change the plan, not a detail of it.

1. **The `Reloc*` API is dead.** `RelocInit`/`RelocMark`/`RelocForward`/
   `RelocCompact`/`RelocFixupAll` in `src/runtime/relocate.mx` are emitted by
   neither compiler. The live return protocol is codec encode in the callee
   arena, `FnArenaPop`, decode in the caller arena
   (ir_deepcopy.mx:811-928, `emitRetWithCodec` :707). Any plan that preserves
   "the three-pass mark/compact" preserves a protocol that is not running.
2. **`.mxh` means two different things.** stage4's `.mxh` is type export data
   for dependents (main.mx:4494). legacy's `.mxh` is an FFI/protocol header
   (loader/mxh.go:41, header.go) and is not the type-import format. A pipeline
   that says "the `.mxh`" without naming which one will build the wrong
   artifact.
3. **Legacy cannot be slimmed while it is the bootstrap.** It must keep
   compiling the full language, including the pipeline's own stages, which are
   Moxie programs. Distillation shrinks the stage4 core and its input language,
   not the bootstrap compiler.

A fourth fact is the reason the runtime boundary section exists (§5.5): the
runtime is already written against erased interfaces, but "mostly erased" is
not the same as "exempt", and a mixed ABI is the most likely silent
miscompilation in the plan.

## 1. The shape of the answer

### 1.1 The chain

```
.mx source, .mxh exports
        │
        ▼
  G  mxc-gen        generics  ── monomorphize: no type parameters, no instantiation
        │
        ▼
  I  mxc-iface      interfaces ── tag+data structs, generated method dispatchers,
        │                         no `interface` type, no `error` interface
        ▼
  T  mxc-text       text      ── no `string`; `__text{data,len,cap}` + accessors,
        │                         static blobs for literals
        ▼
  S  mxc-slice      slices    ── no `[]T`, no array type, no `range`;
        │                         `__slice` + per-T accessors + index loops
        ▼
  M  mxc-map        maps      ── no map type; handle + per-K/V accessors + iterators
        │
        ▼
  E  mxc-codec      ABI       ── generated encode/decode/relocate functions and
        │                         type descriptors; no compiler-side type walks
        ▼
  C  mxc-chan       channels  ── no chan type, no send/recv/select syntax;
        │                         handles + per-T send/recv + select-state calls
        ▼
  D  mxc-defer?     optional  ── defer and method values to explicit calls
        │
        ▼
  F1 mxc-front      parse + check the base language, emit typed AST / IR input
        │
        ▼
  F2 mxc-ir         typed AST → IR (a small, explicit instruction set)
        │
        ▼
  B  mxc-llvm       IR → textual LLVM → clang / opt / lld / ld.lld
```

`?` marks a stage whose necessity is a separate decision (see §9).

Every stage runs over the **whole link set**, including the standard library and
the runtime, not just the user's packages. A signature or field layout lowered
one way in package A and another way in package B does not link or, worse, links
and misreads memory. Per-package caching is allowed only because every stage's
output names and layouts are pure functions of their inputs (rule 2 in §3).

Two things are deliberately not stages:

- **Package resolution, build tags, pragmas, caching and linking** are the
  driver. They are shared by all stages, not a rewrite.
- **The runtime** (`src/runtime/*.mx`) is not below the pipeline. It is input
  to it, exactly like user code. It is already written mostly in the target
  form (see §2.4), which is the strongest evidence the target form is
  sufficient.

### 1.2 The four cuts, and why this order

1. **Generics first.** Constraints are interfaces themselves (`cmp.Ordered`,
   method interfaces), and dispatch over an instantiated type needs the type to
   be concrete. Monomorphizing first removes both problems and makes every
   later pass see ordinary declarations. It also means `comparable`, type sets
   and type-parameter method sets never reach the interface pass.
2. **Interfaces second.** An interface is a whole-link relation, not a local
   one (docs/type-db.md:12-18), so it is the one pass that must enumerate
   implementers across the whole link. Doing it while field layouts, method
   sets and named types are still normal keeps that enumeration
   understandable.
3. **Representations third, in dependency order.** Text, slices and maps are
   independent once interfaces are gone. Then the ABI stage E generates the
   codecs, descriptors and relocation functions for those lowered layouts.
   Channels come last, because a channel frame needs E's encoder for its
   element type.
4. **Core last.** The core compiler is what remains when the rewrite stages
   have taken their features away. Its stages are not new work so much as the
   existing fused pipeline cut at two seams that do not exist yet (§6.1-6.2).

The order is not arbitrary in one more way: each stage's *input checker* can be
smaller than the previous stage's. G needs the full checker (type parameters,
method sets, inference). I needs a checker with interfaces but no type
parameters. T/S/M need types and layouts; E needs only the lowered layout; C
needs E's encoders. F1 needs no interfaces, no
generics, no slice/map/chan/string types at all. That monotone shrink is the
whole point of the design.

| stage | what its input checker must understand | what it must not need |
| --- | --- | --- |
| G | type parameters, constraints, inference, method sets | nothing yet removed |
| I | interfaces, method sets, conversion sites, `error` | type parameters |
| T | text types, literals, conversions | interfaces |
| S | slice types, leftover array declarations, layouts, subslice rules | text, interfaces |
| M | map types, key comparability, iteration | slices/text |
| E | the lowered struct/enum layout only | any source-level feature |
| C | lowered channel handles + E's encoders | maps, slices, text |
| F1 | structs, pointers, functions, basics, `unsafe` | everything above |

The same table is the reason the pipeline is worth building: by C the checker
is looking at ordinary structs and pointers, and by F1 it is a small language.

**The canonical order is one sequence, and every section uses it:**

```
G → I → T → S → M → E → C → D? → F1 → F2 → B
```

`D` is optional and independent of C; it may run anywhere after I, but it is
listed before F1 for concreteness. The two constraints that fix the sequence
are:

- **S before E, and E before C.** S lowers slices into `__slice` structs and
  records the types that need relocation; E then generates the encode/decode
  pair for those layouts; C needs those encoders for channel frames. S's
  *output is not runnable on its own* - "S is not semantically complete
  without E" means the pipeline is incomplete until E runs, not that E runs
  before S. E consumes S/M's output; it cannot precede them.
- **I before T/S/M/E/C.** Dynamic dispatch must be gone before the layouts it
  would dispatch on are erased.

Any text or table that lists C before E, or E before S, is wrong; this is the
one ordering claim the rest of the plan depends on.

### 1.3 Two missing products, and one very large refactor

The stage chain needs two things that do not exist, plus one extraction that is
not a product at all:

1. **A Moxie source printer** (and a source model that preserves pragmas,
   build tags and file identity). There is no AST printer in `pkg/syntax` or in
   `_mxc_stage4`; the compiler only prints LLVM IR. Without a printer there is
   no source-to-source stage at all.
2. **A stage contract** (per-package source + `.mxh` in, rewritten source +
   rewritten `.mxh` out, with deterministic naming and no process-global
   state). Today `cctx` is a global, per-package compilation is one process,
   and `.mxh` is generated inside `CompileToIR` (§6.3).

The third thing is **not** an artifact: **a typed-AST product has to be
extracted from the SSA builder.** Stage4 has no typecheck-then-build split;
`CreatePackage` (ssa_builder.mx:48) registers declarations, builds every
function body, and typechecks expressions as it emits SSA. The rewrite stages
need a checked tree *without* SSA. The checking halves of `buildExpr`,
`buildStmt`, `buildAssign`, `buildCall`, `buildOperation`, `operandType`,
`exprStaticType`, `checkAssignable` and `checkCallArgs` interleave type
resolution with SSA emission; lifting them is roughly 1,500-2,500 lines of the
most intricate code in the compiler, and it must not change behaviour.

That extraction is the **top enabling risk** in §9, not a line item. Every
*standalone* stage depends on it, and if it is underestimated the pipeline
stalls at M0. The plan's recommendation is to extract the checker once (not
write a second, lighter one for the stages), because two answers to "what is
the type of this expression" is exactly the incoherence the axioms forbid.

There is a pragmatic escape for the first stage, and the generics plan takes
it: a stage can be implemented **as a mode of the existing compiler** and
borrow the fused checker while emitting source alongside the SSA build. That
defers the extraction without pretending the type tree exists. It does not
remove the risk; it moves it to the point where a stage must run standalone or
the core must stop carrying the full checker.

### 1.4 The decomposition inventory

One row per part of the current compiler that has to move, and where it goes.
The detailed evidence for each row is in §4.

| part today | current owner (main references) | destination |
| --- | --- | --- |
| type parameters, instantiation, inference, mangling | pkg/syntax parse_decl.mx:292-497; pkg/types tc_types.mx:514-539; ssa_builder.mx:10483-11350, :7123-7350; ir_types.mx:17-112; ir_descriptors.mx:848; main.mx:4666 | G |
| interface types, boxing, invoke, assert, type switch, dispatch arms, type IDs | pkg/types tc_types.mx:427, tc_assign.mx:356; ssa_builder.mx:2109, :4145; ir_iface.mx (all); main.mx:7175, :7342; runtime/interface.mx | I |
| text type, literals, concat, compare, conversions, rune decode | pkg/types TCString; ir_emit.mx:684-724, :2880-3024; ir_binop.mx:603, :977-1015; runtime/string.mx | T |
| slices, arrays, indexing, len/cap/push/pop/resize/copy, range | pkg/types Slice/Array; ssa_builder.mx:3790, :6363, :6606, :8010, :8455; ir_emit.mx:3270-3900; ir_iter.mx; runtime/slice.mx | S |
| maps, lookup/update/delete/clear, map range | pkg/types TCMap; ssa_builder.mx builtin/index paths; ir_map.mx; ir_iter.mx:242; runtime/hashmap.mx | M |
| return relocation, codec, type descriptors, frame budgets, sovereign decisions | ir_deepcopy.mx (all); ir_descriptors.mx (all); ir_chan.mx:846-993; runtime/codec.mx, typekind.mx | E |
| channel type, send/recv, select, close, frame routing | pkg/types TCChan; ssa_builder.mx:2070, :4371; ir_chan.mx (all); runtime/chan.mx, pipe_channel.mx, ringbuf.mx | C |
| `shutdown`/`closed` words | ir_rewrite.mx:481; tc_universe.mx:298; ir_call.mx:1238 | C (validation before) |
| defer, defer thunks, method values | ssa_builder.mx:4861-5068; ir_defer.mx; runtime/panic.mx deferFrame | D (optional) |
| parser, type checker, restrictions, entry checks | pkg/syntax; pkg/types; ir_typecheck.mx:439; ir_scope.mx | F1 |
| SSA construction and LLVM text emission | ssa_builder.mx (remaining); ssa_types.mx; ir_emit.mx, ir_types, ir_binop, ir_call (remaining) | F2 |
| clang/opt/lld invocation, object cache, initAll, asm pass | main.mx:353-585, :8120-8185, :8330+ | B |
| package discovery, build tags, pragmas, mxh, caches, commands | main.mx:2425-2700, :3062, :3780-4010, :4494-5000 | shared driver |
| legacy equivalents | legacy/{loader,compiler,transform,interp} | reference + bootstrap, not removed |

---

## 2. What exists today

### 2.1 The stage4 pipeline (Moxie, the primary compiler)

45,555 lines of Moxie across `_mxc_stage4/*.mx`.

| phase | entry | what it does |
| --- | --- | --- |
| driver | `main` main.mx:7554 | flags, target, resolve packages, dispatch index, build loop, link |
| package resolve | `resolveAll` main.mx:3999, `discoverPkg` main.mx:3780 | read sources, build tags, imports, `.mxh` |
| dispatch index | `prepareDispatchIndex` main.mx:7175 | write every `.mxh`, load method index for interface arms |
| whole-link typecheck | main.mx:7983-8065, `typeCheckPkgSource` main.mx:4149 | `TypeCheckOnly` per package, registry populated |
| dispatch measure | `measureDispFingerprints` main.mx:7342 | freeze each package's interface arms for the cache key |
| per-package compile | `buildPkgsSerial` main.mx:8427, `buildPkgsParallel` main.mx:9259 | `compilePkgSource` main.mx:4161 |
| compile entry | `compileSource` main.mx:4120 → `CompileToIR` ir_typecheck.mx:1314 | one package: source → IR text |
| byte rewrites | ir_typecheck.mx:453-457 | `stripUnusedConsts`, `rewriteShutdownWord`, `rewriteChanMakeLiterals`, `rewriteSliceMakeLiterals` |
| parse | `syntax.ParseBytesArena` pkg/syntax/syntax.mx:45 (called ir_typecheck.mx:472) | one synthetic file per package |
| typecheck | `typeCheckPkg` ir_typecheck.mx:439 | declarations into `TCPackage`, `String()` injection, mxh |
| SSA | `SSAProgram.CreatePackage` ssa_builder.mx:48 | imports, declarations, **bodies and expression checking**, restrictions |
| IR emit | `newIREmitter` ir_emit.mx:124, `emit` ir_emit.mx:755 | SSA → textual LLVM; optional stream to `cctx.irOutputFile` |
| backend | `objFiles` main.mx:535, `clangCompileToObj` main.mx:573, link main.mx:8330+ | `.ll`→`.bc`→`.o`, initAll, ld.lld/wasm-ld |

Sizes that matter for the split:

| file | lines | role |
| --- | --- | --- |
| ssa_builder.mx | 11,323 | front end + SSA + semantic checks |
| main.mx | 9,699 | driver, caches, mxh, link, commands |
| ir_emit.mx | 4,125 | LLVM text emitter core |
| ir_typecheck.mx | 2,841 | parse/check/SSA/emit orchestration |
| ir_iface.mx | 2,366 | interface coercion, dispatch, asserts |
| ir_scope.mx | 1,941 | Moxie restrictions and scope checks |
| ir_binop.mx | 1,593 | operators, text compare/concat |
| ir_call.mx | 1,589 | calls, builtins, spawn |
| ir_descriptors.mx | 1,383 | type descriptors for codec/interfaces |
| ir_chan.mx | 1,153 | channel and select emission |
| ir_deepcopy.mx | 1,012 | function arenas, return relocation, codec |
| ir_types.mx | 672 | type → LLVM type |

The pipeline is **per package in the middle and whole-link at the edges**: the
dispatch index and registry need every package's exports before any emission
(main.mx:7983-8065), but each package is then typechecked, built and emitted
alone. That is the seam a stage split can use.

### 2.2 The legacy pipeline (Go, the bootstrap mirror)

29,724 lines of Go across `legacy/{main.go,builder,loader,compiler,transform,interp}`.

| phase | entry | what it does |
| --- | --- | --- |
| driver | legacy/main.go:131 `Build`, `builder.Build` builder/build.go:96 | same command surface |
| load | `loader.Load` loader/loader.go:113 | package graph, source read |
| parse/rewrite/check | `Package.Parse` loader/loader.go:358, `Package.Check` loader/loader.go:388 | `legacy/syntax` (a go/syntax fork) + adapter to `go/ast` (adapter.go:50), `injectBuiltinStrings`, AST rewrites, go/types, recheck. `go/parser` is used only for the synthetic Stringer file (loader/stringers.go:167). |
| SSA | `Program.LoadSSA` loader/ssa.go:13 | `ssa.InstantiateGenerics` (this is the monomorphizer) |
| per-package IR | `compiler.CompilePackage` compiler/compiler.go:327 | go/ssa → LLVM module per package |
| link | program job builder/build.go:511 | `llvm.LinkModules` |
| interpret | `interp.Run` builder/build.go:629 (and `interp.RunFunc` in each package job :447) | folds `runtime.initAll` at compile time |
| interface lowering | `transform.LowerInterfaces` transform/interface-lowering.go:116 | whole-module IR pass |
| interrupts | `transform.LowerInterrupts` | |
| object/link | builder/build.go:699, :813 | `.o`, ld.lld |

Legacy is the existing proof that every one of these phases can be a separate
function over an explicit product. It is also where the only source-rewriting
machinery lives (§2.3), and where generics are already monomorphized by
`ssa.InstantiateGenerics` rather than by hand.

### 2.3 The rewrite precedent (legacy `mxtext` + loader, stage4 `ir_rewrite`)

Source rewriting is not new. It is just not structured as stages.

Legacy rewrites, before and around go/types:

| rewrite | location | kind |
| --- | --- | --- |
| `shutdown` word | mxtext/rewrite.go:337, loader see loader.go:388 | bytes |
| `chan T{...}` literal | mxtext/rewrite.go:106 | bytes |
| `[]T{...}` literal | mxtext/rewrite.go:410 | bytes |
| `string`/`[]byte` type unification | mxtext/rewrite.go:721, :885 | AST |
| String/[]byte conversions | mxtext/rewrite.go:872 | **no-op** (documented); unification handles the real conversions |
| string literals / const demotion | mxtext/rewrite.go:921 (`RewriteStringLiterals`, `splitConstBlocks` :1253) | AST |
| `\|` concat and `+=` | mxtext/rewrite.go:1190, :2443 | AST |
| `pop` statement | loader/poprewrite.go:20 | AST |
| value receiver on named slice/map | loader/recvrewrite.go:28 | AST |
| `String()` injection | loader/stringers.go:79 | AST (generated source) |
| interface-mandated returns | mxtext/rewrite.go:793 | AST |

The loader then **checks, rewrites, and checks again** (loader/loader.go:388-470)
because the rewrites depend on go/types information. That is a two-pass
source-to-source pipeline already, with go/types as its oracle. The order is
load-bearing: `shutdown` runs before every other byte rewrite because the later
passes parse its output; channel literals run before slice literals because
`chan []byte{:n}` contains `[]byte{`; the string-type rewrite runs before the
`+`→`|` rewrite, which is why the latter needs a second check.


Stage4 does the same work inside `typeCheckPkg` on bytes
(`rewriteShutdownWord` ir_rewrite.mx:481, `rewriteSliceMakeLiterals`
ir_rewrite.mx:684, `rewriteChanMakeLiterals` ir_rewrite.mx:928) and inside the
SSA builder on syntax nodes (e.g. `pop` at ssa_builder.mx:6606). It has no
printer, so every rewrite is either a byte splice or a direct AST mutation that
never becomes text again.

That is the gap: **rewriting exists; re-emitting does not.**

Mechanically, neither compiler ever round-trips a rewrite through a printer.
Legacy's byte rewriters build a throwaway `token.FileSet` for offsets and splice
bytes (mxtext/rewrite.go:115, :344, :418); its AST rewrites mutate nodes in
place, keyed by node identity, and at most one re-typecheck follows
(loader/loader.go:455-524). Diagnostic fidelity depends on recomputing line
starts from the rewritten bytes (syntax/adapter.go:65-76) and on
`stripUnusedConsts` blanking a line rather than deleting it
(loader/conststrip.go:59). Whole classes of go/types errors are then dropped by
substring match because the rewrites make valid Moxie look invalid to go/types
(`filterStringByteMismatch`, `filterNoNewVarErrors`, `filterUndefinedSliceAlloc`,
`filterPipeErrors`, `filterByteCompareErrors`; loader/loader.go:409-414,
loader/mxrewrite.go:52-113). Those filters are load-bearing today; a
printer-based pipeline that emits types the checker accepts can retire them, but
not before.

### 2.4 The runtime ABI is already erased; its source is not yet in the subset

The runtime's **calling convention** is already the simplified one, which is
why the target representation is achievable:

| runtime | representation | entry points |
| --- | --- | --- |
| slice | caller-passed `(ptr, len, cap)`, element size | `slicePush` src/runtime/slice.mx:42, `sliceConcat` :79, `sliceCopy` :14 |
| map | `*hashmap` with byte sizes + function pointers | `hashmapMake` hashmap.mx:62, `*BinarySet/Get` :653/:661, `*ContentSet/Get` :699/:722, `hashmapNext` :549 |
| channel | `*channel` ring, element size + frame budget | `chanMake` chan.mx:316, `chanSend` :422, `chanRecv` :519, `chanClose` :663, select :719+ |
| string | `_string{ptr,len,cap}` | `stringEqual` string.mx:33, `stringConcat` :96 |
| interface | `_interface{typecode,value}` | compose/decompose interface.mx:16/:22, `interfaceEqual` :26 |
| codec | structural walk over `RawType` | `codecEncodeValue` codec.mx:918, `codecDecodeValue` :1308 |
| type descriptors | byte-layout `RawType` family | typekind.mx:57-96 |

The map and channel APIs take **sizes and function pointers**, not types. The
compiler is the only component that still insists on knowing the type. Moving
that knowledge into generated accessors is therefore not a runtime rewrite; it
is a codegen rewrite.

**But the source is not yet the final subset, and the plan says so.** An audit
of `src/runtime` today:

- **generics: 1.** `AddCleanup[T comparable, S comparable]` (runtime.mx:209).
  G must erase it; the self-host gate asserts zero generic declarations in
  `src/runtime` afterwards. Nothing generic should be added meanwhile.
- **arrays: ~20 declarations.** `tophash [8]uint8`, `_pad [8]uint8`
  (hashmap.mx:31-44), `codecBufScratch [256]byte` and the seen tables
  (codec.mx:734, :831, :891), `(*[3]uintptr)` casts (offsetframe.mx:112-199),
  `pollDesc [512]pollDesc` (poll.mx:72), `digits [20]byte` (print.mx:79),
  `[64]unsafe.Pointer` (relocate.mx:191), `[7]byte` reserved (os_linux.mx:55).
  Moxie has no array type; S removes every one (§4.4).
- **interfaces: one imported interface type** (`moxie.Codec` in chan.mx:195)
  and the `_interface` struct. I lowers the former; the latter is the target
  layout, not a language interface.
- **maps: none** in the source (the hashmap implementation is not a map type).
- **slices/strings: pervasive**, and that is expected: T/S lower them.

So "the runtime is already the simplified implementation" is true of its ABI
and false of its source. The stages run over it like any other package; the
remaining work is the array cleanup, the one generic, and the language-level
slices/strings. It is not exempt (§5.5).

---

## 3. Rules the chain must obey

1. **Every stage's output is valid Moxie.** It must parse, typecheck and run
   under the same semantics. Generated code is allowed to use `unsafe`, as the
   user's brief says, and may use blob memory (linkdata, mapped regions,
   compiler data objects), but it must not use a construct that a later stage
   is supposed to have removed - including an array type, which Moxie does not
   have.
2. **Deterministic names.** A rewrite stage may be run per package, cached, and
   run in parallel. Every generated symbol (slice accessor, map handle, tag,
   dispatcher, instance) must therefore be a pure function of its type or
   declaration identity - never a global counter, never iteration order over a
   map. Existing manglers to imitate: `mangleGenericName` ssa_builder.mx:10483,
   `typeDescName` ir_descriptors.mx:87, `safeName` main.mx:2762.
3. **Pragmas, build tags and file identity survive.** The compiler reads
   `//:build`, `//:linkdata`, `//:extern`, `//:linkname`, `//:embed`,
   `//:wasmimport`, `//:inline` from source before parsing
   (`scanEmbedPragmas` ir_rewrite.mx:101, `scanLinkdataPragmas` :283,
   `scanLinknameMap` :347, `scanExternGlobals` :223, `scanWasmImportMap` :387).
   A printer that drops comments destroys the build. Also note that stage4
   **concatenates every file of a package into one synthetic file**
   (`concatSources` main.mx:3062) and maps diagnostics back with
   `cctx.concatSourceMap`; a rewrite stage that emits one file per package must
   either preserve that map or emit per-file source again.
4. **Whole-link facts stay whole-link.** Interfaces and generics need the whole
   closure; per-package caching must not change the answer. The existing
   dispatch fingerprint (ir_iface.mx:947, measured at main.mx:7342) already
   solves "cache key must move when arms move" for IR; the source stages need
   the same discipline.
5. **Wood law.** Language behaviour changes are mirrored in `legacy/`. The
   rewrite stages themselves can be Moxie-only at first (they are new
   programs), but every restriction or semantic they rely on must exist in both
   compilers until legacy is retired. Legacy must keep supporting the full
   language for as long as it is the bootstrap path.
6. **The core subset is a compiler profile, not a fork of the language.** The
   same `pkg/syntax`/`pkg/types` sources should grow a "base profile" mode so
   there is one parser and one checker, not two copies drifting apart.
7. **Generated representation types must be link-unique.** If package A emits
   `type __slice_int32` and package B emits its own `type __slice_int32`, the
   two are different named types and a function in A cannot accept a value from
   B. This is the single easiest way to make a source pipeline produce code that
   does not typecheck across packages. Two shapes avoid it:
   - **One universal header per family**, defined once in a generated shared
     package: `__slice{data unsafe.Pointer; len, cap int32}`, `__map{p unsafe.Pointer}`,
     `__chan{p unsafe.Pointer}`, with `__text` an alias of `__slice`. All element
     typing lives in the per-T accessor *functions*, never in the header type.
   - **Per-interface types defined once**, in the package that declares the
     interface, and referenced (or aliased) by every user. `io.Reader`'s lowered
     struct is `io.Iface_Reader`; a caller writes `Iface_Reader` only through the
     package alias emitted for it.
   Generic instances follow the same rule: one definition in the declaring
   package, referenced everywhere (§4.1).
   **Every generated symbol that crosses a package boundary must be exported**
   (start with an uppercase letter); Moxie has no other visibility mechanism,
   and the `.mxh` export list only carries exported names. The short names used
   in this document (`__slice`, `__text`, `__map`, `__chan`, `__I_Reader`) are
   therefore per-package **aliases** of one exported shared definition
   (`mxrt.SliceView`, ..., `io.Iface_Reader`); an alias preserves type identity,
   a second copy of the struct does not.

---

## 4. Stage contracts

Each subsection follows the same shape: what the stage removes, what exists
today (with the exact functions that do the work), the mechanism, and what
leaves the downstream compiler.

### 4.1 G - `mxc-gen`: generics erasure

**Removes:** type parameter lists, type arguments, constraint types, generic
methods, generic type declarations, `comparable`, and the generic instantiation
machinery in SSA/IR/descriptors.

**What exists today (stage4)**

| part | location |
| --- | --- |
| parser fields `TParamList` | pkg/syntax/nodes.mx:60 (`FuncDecl`), :79 (`TypeSpec`); parse pkg/syntax/parse_decl.mx:292, :346, :466, :497 |
| type representation | pkg/types/tc_types.mx:514 `TypeParam`, :534 `Named{TParams,TArgs}`, :538-539; :520 `NewTypeParam` |
| declaration scan | ir_typecheck.mx:942 `ScanGenericDecls`, :926 `fileHasGeneric`; registries `genericFuncDecls`, `genericPkgLocalDecls`, `genericMethodDecls`, `genericPkgScopes` (writes ir_typecheck.mx:1052-1066, :1197) |
| AST retention | `cctx.genericArena` / `scannedGeneric` (ir_typecheck.mx:945-973); `copyGenericsToRoot` main.mx:5886 |
| inference | ssa_builder.mx:10669 `resolveTypeArgsExpr`, :10731 `matchTypeParam`, :10814 `extractConstraintTypeParams`, :10869 `inferTypeArgsFromFunc`, :11030 `unifyParamAST` |
| substitution | ssa_builder.mx:10269 `substTypeIn`, :10247 `substTypeParams`, :2275 `instantiatedUnder`, :2297 `bindInstantiation`, :10516 `resolveTypeExprWithSubst`, :10685 `resolveSignatureWithSubst`, :7327 `resolveMethodSigWithSubst` |
| instantiation and cloning | ssa_builder.mx:11050 `monomorphizeAndCall`, :11184 `tryGenericLocalCall`, :11218 `tryGenericPkgCall`, :11234 `tryGenericCallFromIndex`, :7177 `genericMethodInstance`, :7123 `tryGenericMethodCall`, :7274 `ensureIfaceInstMethods` |
| naming | ssa_builder.mx:10483 `mangleGenericName`, :10500 `localTypeTagOf` (ir_descriptors.mx:608) |
| layout | ir_types.mx:17 `namedTParams`, :99-112 `llvmTypeInner` substitution, :659 `namedUnder` |
| descriptors | ir_descriptors.mx:134 instantiated tail `pkg.T[arg,...]`, :848 `instantiateTail`, :862 `tailStripTArgs` |
| export | main.mx:4666 `mxhWriteTParams`, :4695 `parseMxhTParams` (constraints dropped), :4575 `gfunc` lines; generic methods deliberately not exported main.mx:4620 |
| dispatch | ir_iface.mx:534 skips generic declarations; :631 reads `cctx.genericMethodInsts` |
| restrictions | ir_scope.mx:369-376 init-clone name rule |
| legacy mirror | loader/ssa.go:13 `ssa.InstantiateGenerics`; legacy/compiler/symbol.go:73 `getFunction` names; go/types instantiation |

**The mechanism that is actually there.** A generic body is never re-parsed at
an instantiation. `ScanGenericDecls` keeps the one AST and each instantiation
re-runs only `buildBody` with `typeSubst` (ssa_builder.mx:11133, :7269). The
clone is a `SSAFunction` inserted into **the caller's package**
(`targetPkg = fb.fn.Pkg`, `targetPkg.SetMember`, ssa_builder.mx:11105-11123),
and the builder temporarily swaps `cctx.sigDeclPkgImports` to the declaring
package's imports so the body's qualified names resolve (ssa_builder.mx:11113,
`declPkgImports` :11142).

That is the wrong ownership for source-level emission: a source clone placed in
the calling package cannot name the declaring package's unexported helpers or
types. Source output has no semantic resolver; it has names. So the source
stage must emit instances **in the declaring package**, with names exported
enough to be referenced, and rewrite call sites to those names.

**Mechanism for the source stage (three passes over the whole link).**

1. **Collect.** Typecheck every package (the existing whole-link pre-pass
   already does this: `prepareDispatchIndex` main.mx:7175 + `TypeCheckOnly`
   main.mx:7983-8065). Walk every body and record each instantiation request
   `(declKey, typeArgs, usePos)`, using the existing inference entry points
   above. Record requests, do not emit.
2. **Emit instances.** For each distinct `(declKey, typeArgs)`, clone the
   declaration AST, substitute every type in the signature and body, mangle the
   name deterministically, and append the declaration to a generated file in
   the **declaring** package. For a generic type, clone the `TypeSpec` and every
   method whose receiver names its parameters, substituting the receiver type
   to the instantiated named type. Instance names must be exported:
   `Gen_<PkgHash>_<Decl>__<ArgHash>` (or the existing `mangleGenericName`
   plus a package-hash prefix; see §3 rule 2).
3. **Rewrite uses.** Replace `f[T](x)` with `f__inst(x)`, `pkg.F[T](x)` with
   `pkg.F__inst(x)`, and `Box[T]` in type positions with the instantiated type
   name. Remove the type-parameter lists from the original declarations.

**Problems the current design hides, and the source stage must solve**

- **Unexported generic functions become exported instances.** That is fine for
  generated code but changes the package's symbol surface; the instance must be
  added to the `.mxh` so downstream packages can reference it.
- **Function-local types as type arguments.** `mangleGenericName` includes
  `localTypeTagOf` precisely because two function-local `T`s collided
  (ssa_builder.mx:10500). A package-level generated declaration cannot name a
  function-local type. The stage must lift such a type to package scope with a
  generated name, or instantiate locally (a local `func`), and either way keep
  the tag in the mangled name.
- **Generic methods are not exported today** (main.mx:4620). The stage must
  export the instantiated methods, because a downstream package's interface
  implementation may need them (today `ensureIfaceInstMethods` manufactures
  them at the coercion site, inside the caller).
- **Type identity across packages.** Two packages asking for `Pair[int32]`
  must get the same type, or field layout and method sets diverge. The instance
  name must be a pure function of `(declaring package, declaration, canonical
  type-argument text)`. That is stronger than the current per-caller clone,
  which is legal only because SSA clones are per-module and type identity is by
  descriptor tail.
- **Constraints disappear, but their method sets do not.** A constraint like
  `cmp.Ordered` or an interface constraint is not emitted; it only selects the
  type arguments. Make sure a type argument's method set is checked *before*
  erasure, because after erasure there is no record of the requirement.
- **`unsafe.Sizeof` on an instantiated type** folds at compile time
  (pkg/types/tc_layout.mx) and must be evaluated after substitution.

**Leaves the downstream compiler:** every row of the table above. In
particular `ssa_builder.mx` loses the 11050-11350 region, the generic branches
of `buildCall`/`buildSelector`, `instantiatedUnder`, `bindInstantiation`, the
`TArgs` layout path in `ir_types.mx`, `instantiateTail`, `genericMethodInsts`,
the generic branches of `findIfaceImpls`, and `.mxh` `$P` / `gfunc` support.

**Legacy:** `ssa.InstantiateGenerics` is the semantic oracle. The source stage
should be validated by compiling the same program with legacy and comparing
behaviour, not by comparing text.

---

### 4.2 I - `mxc-iface`: interface erasure

**Removes:** the `interface` type constructor, interface-typed values,
interface method calls, type assertions/switches, `error` as an interface,
`implements`/method-set machinery in the compiler, type-ID dispatch arms, and
the descriptor's method-set fields.

**What exists today**

| part | location |
| --- | --- |
| type model | pkg/types/tc_types.mx:427 `TCInterface`, :476 `Complete`, :417 `IfaceMethod` |
| satisfaction | pkg/types/tc_assign.mx:356 `Implements`, :377 `methodSet`, :472 `IfaceMethodSetLocal`, :541 `IfaceMissingMethod` |
| error interface | pkg/types/tc_universe.mx:240-246 (built from `String`+`Error`) |
| boxing | ssa_builder.mx:2109 `coerceToInterface`, call sites :1725, :2008, :2070, :2849, :2911, :2946, :2962, :4755, :6174; `checkIfaceAssignments` :2316 |
| type switch | ssa_builder.mx:4145 `buildTypeSwitch` (one comma-ok assert per case + `||` chain) |
| SSA nodes | ssa_types.mx:508 `SSAMakeInterface`, :518 `SSAInvoke`, :661 `SSATypeAssert` |
| emit | ir_iface.mx:8 `emitMakeInterface`, :1812 `emitInvoke`, :1961 `emitIfaceChain` (linear type-ID compare chain), :2138 `emitTypeAssert`, :1400 `emitIfaceAssertOK`, :2353 `extractScalarFromIface` |
| implementer enumeration | ir_iface.mx:470 `findIfaceImpls` (own members, embedded, generic clones :631, imports :642, whole registry :673), :1020 `findEmbedMethod`, :1147 `findExtEmbedMethod` |
| type identity | ir_iface.mx:236 `typeIDHash` (address of `@reflect/types.type:tail`), :1545 `irDescriptorGlobalFromTID` |
| descriptors | ir_descriptors.mx:87 `typeDescName`, :1247 `emitNamedDesc` (method set), :690 `emitTypeDescs` |
| whole-link index | main.mx:7175 `prepareDispatchIndex`, :6082 `cmdDispIndex`, :7342 `measureDispFingerprints`, :6228 `tier1`; cache key main.mx:2546 |
| runtime | src/runtime/interface.mx:10 `_interface`, :16 compose, :26 `interfaceEqual`, :35 `rawValueEqual` |
| legacy | compiler/interface.go, transform/interface-lowering.go:116 `LowerInterfaces`, builder/build.go:603-640, transform/optimizer.go:71 |
| Stringer injection | loader/stringers.go:79, ir_typecheck.mx:2671 `injectBuiltinString`, ir_iface.mx:255 `builtinStringerTypes` |

**Mechanism A: tag + data struct with generated method dispatchers (recommended).**

Every interface type `I` becomes a generated struct with the same two-word
layout as today. It is defined **once**, in the package that declares `I`, and
every other package references or aliases it (rule 7 in §3); a per-package
`__I_Reader` would not be the same type as its neighbour's.

```moxie
// defined once, in io, because io declares Reader
type Iface_Reader struct {
    tag  int32            // whole-link type ordinal, 0 = nil
    data unsafe.Pointer   // pointer to a copy of the concrete value
}

func (i Iface_Reader) Read(p []byte) (n int32, err Iface_Error) {
    switch i.tag {
    case os.TagFile:   return (*os.File)(i.data).Read(p)
    case bytes.TagBuf: return (*bytes.Buf)(i.data).Read(p)
    ...
    }
    panic("nil interface call")
}
```

Why this form:

- It is **source-level valid Moxie**: a struct with methods. The core compiler
  needs no interface concept at all; `i.Read` is an ordinary method call.
- Dispatch is still "resolved by the type", as the brief asks: the generated
  method switches on the tag.
- Interface conversion becomes an explicit generated call:
  `i = io.GF_Box_IfaceReader_File_<hash>(v)` allocates a copy in the current
  arena, stores the tag and the pointer. Generated names follow §6.9; the short
  names in this section are illustrative.
- `I1 → I2` becomes a struct copy that keeps the same tag and data, re-typed
  as the target interface; a generated adapter is needed only when the method
  sets differ.
- A type switch becomes `switch i.tag { case pkg.TagT: ... }`, which the core
  compiles as an integer switch, not a chain of interface asserts.
- Nil is the zero struct; `i == nil` becomes `i.tag == 0`.
- `i == j` becomes a generated `Equal_Iface(i, j)` that compares the tags and
  then calls the dynamic type's generated equality; it cannot be a raw struct
  compare once `data` points at a copy.
- A method value (`f := i.Read`) becomes a generated standalone thunk taking the
  interface struct as an explicit first argument; closures do not capture, so
  the receiver must be a parameter. A method expression (`I.Read`) is the same
  thunk without the receiver bound.
- `err != nil` keeps working; `error` is just another generated interface
  struct (`Iface_Error`) with `Error() string`.

**Tag assignment is a whole-link decision and must be specified.** The base
allows `switch` on integers, not pointers, so the tag is an `int32`, not the
descriptor address the current runtime uses. The interface stage enumerates
the concrete types that can flow into an interface (the conversion-site set it
already computes), sorts them by canonical type key, reserves `0` for nil, and
assigns `1..N`. It emits the assignments as exported constants in a generated
tag package (`const Tag_os_File int32 = 17`), and the link's tag table is part
of the stage output and of the cache key. Every dispatcher, assertion and
equality function uses those constants.

The cost is that adding a type renumbers the tail of the table and therefore
changes the packages that embed those constants. That is acceptable while I is
a whole-link stage (§6.3); if per-package reuse later makes the churn
expensive, switch to a 64-bit hash of the canonical type key with a
whole-link collision check (a collision is a build error, never silent). Do
not use pointer values in a source-level tag: it reintroduces a representation
the base is trying to remove and makes the switch non-portable.

**Stringer injection is not part of this stage.** `injectBuiltinString`
(ir_typecheck.mx:2671, loader/stringers.go:79) adds ordinary `String()` methods
to structs and named func types so they satisfy `fmt.Stringer`. Those are normal
methods and must be injected before I (or kept as an F1 normalization); removing
them with the interfaces would break `fmt`.

This is more tractable than it first looks because **a function that takes an
interface parameter does not need to be specialized**. Its parameter type is
replaced by the generated struct, and its body's method calls dispatch through
the generated methods. Only conversion sites, assertions and comparisons need
generated helpers. `fmt`'s `...Stringer`, `io.Reader/Writer`, `error` and
`sort.Interface` all keep their shapes.

**Mechanism B: vtable in the value.** The struct carries one function pointer
per method, filled at boxing time. Dispatch becomes an indirect call and there
is no whole-link implementer scan. It costs a wider value per interface and a
per-`(T,I)` adapter at every conversion, and `I1→I2` needs a real adapter. It is
the better answer if whole-link dispatch enumeration is judged too expensive;
it is not the recommended first cut because stage4 already has the whole-link
index and because a switch keeps generated code readable.

`Iface_Error` itself is predeclared like `error` today: one generated
`Iface_Error` in the shared generated package, aliased to `error` in every
package so source that names `error` keeps compiling.

**Two things this stage still needs from elsewhere**

1. **Relocation of interface values.** A boxed value points into the current
   function's arena. Returning it, or storing it into a receiver, must relocate
   the *dynamic* value, which needs a tag→relocate-function switch. That
   generated function belongs to E (§4.6). Without E, interface erasure cannot
   preserve return semantics.
2. **Aggregate keys/values.** A map with an interface key/value, a slice of
   interfaces, and a struct field of interface type all become aggregates of
   `__I_*`. Their equality/hash/codec functions must dispatch on the tag; the
   generated functions come from I (equality) and E (codec).

**Problem: whole-link enumeration and caching.** The dispatcher for
`__I_Reader.Read` must list every implementer in the link. That is the same
information `findIfaceImpls` already gathers, and the same cache problem: the
generated file of the interface's package changes when any implementer changes.
Two ways out:

- keep the existing `pkgHash` dispatch-fingerprint discipline and accept that
  the interface package rebuilds when arms move (today's behaviour), or
- put dispatchers in a **generated per-link package** (`__dispatch`) that is
  rebuilt whenever the link set's arms change. Per-package objects then stay
  stable. The second is cleaner for a staged pipeline and is the recommended
  shape.

**Unexported methods and tags.** Tag constants are generated in the shared tag
package (§4.2), so they are visible everywhere; if a tag lives in the concrete
type's package instead, it must be exported (`os.TagFile`, not `__tag_File`).
The same visibility rule applies to both. Moxie has unexported methods in interfaces (the checker
reports them: ssa_builder.mx:2374). A cross-package dispatcher cannot name an
unexported method or call it. The dispatcher must live where the method is
visible, or the stage must generate an exported trampoline in each implementing
package. Today the IR emitter invokes the method symbol directly, which works
because symbols are not subject to source visibility. At source level this is a
real constraint: generate one exported thunk per implementing type next to the
type (`func (t *T) __m_M(args) ... { return t.M(args) }`) and have the
dispatcher call the thunk.

**Leaves the downstream compiler:** `TCInterface`/`Implements`/method sets, all
of ir_iface.mx, the `SSAMakeInterface`/`SSAInvoke`/`SSATypeAssert` nodes, the
dispatch index and its fingerprints, the method-set halves of
ir_descriptors.mx, `keepIfaceImpls`/`@llvm.used` interface retention, and the
interface branches of the runtime (`composeInterface`, `interfaceEqual`,
`rawValueEqual`). `fmt`'s Stringer plumbing can stay as normal method calls.

**Legacy:** `transform/interface-lowering.go` is the reference for
implementer enumeration and type identity (it already does a whole-module pass
with a `typeInfo`/`interfaceInfo` map). The source stage is the same algorithm
one level up, with names instead of LLVM symbols.

### 4.3 T - `mxc-text`: text lowering

**Removes:** the `string` type (which is `[]byte` in Moxie), text literals as
typed values, `+`/`|` concatenation, text comparison, indexing/slicing text,
`range` over text, `string`/`[]byte`/`[]rune` conversions, `unsafe.String` and
`unsafe.StringData`.

**What exists today**

| part | location |
| --- | --- |
| unified type | pkg/types/tc_types.mx:50 `TCString`; `isStringLike` ir_map.mx:153, ir_types.mx:245-246 |
| literal emission | ir_emit.mx:706 `stringConstOperand`, :684 `addStringConst`, :723 `strConstGlobal`, :1059-1066 private writable globals (one object per occurrence) |
| compare/concat | ir_binop.mx:603, :1007-1015 (`runtime.stringEqual`), concat around the `\|` lowering |
| conversions | ir_emit.mx:2880 `emitConvert` (`stringToRunes` :2919, `stringFromRunes` :2913, `stringFromUnicode` :3024) |
| runtime | src/runtime/string.mx:11 `_string{ptr,len,cap}`, :33 `stringEqual`, :96 `stringConcat` |
| legacy rewrites | mxtext/rewrite.go:721 `RewriteStringTypes`, :872 `RewriteStringConversions`, :885 `RewriteUnsafeString`, :921 `RewriteStringLiterals`, :793 `WrapInterfaceMandatedReturns` |
| byte builtins | loader/mxtext builtins.go:47 `__moxie_concat`, :55 `__moxie_eq`, :59 `__moxie_lt` |

**Lowered form**

```moxie
// In the generated shared package; __text is an alias, because string and
// []byte are one type in Moxie (rule 7).
type __slice struct {
    data unsafe.Pointer   // byte blob
    len  uintptr
    cap  uintptr
}
type __text = __slice

func __text_at(t __text, i int32) byte
func __text_slice(t __text, lo, hi int32) __text   // allocates and copies
func __text_concat(a, b __text) __text              // runtime.stringConcat
func __text_equal(a, b __text) bool                 // runtime.stringEqual
func __text_less(a, b __text) bool
func __text_from_bytes(s __slice) __text            // same header, no copy
```

`len`/`cap` become field reads. `s[i]` becomes a bounds-checked `__text_at`.
`s[a:b]` is a copy, as Moxie requires. `string(x)`/`[]byte(s)` are header
reinterpretations; `[]rune(s)` calls generated code over the existing
`runtime.stringToRunes`/`fromRunes`.

**The static-blob bootstrap problem.** The base language is supposed to have no
`string`, but generated code needs constant bytes: panic messages, map key
literals, format strings, tag names. There must be exactly one way to name a
static block of bytes. Three candidate mechanisms:

1. **A literal-only intrinsic.** `__blobtext("...")` yields a `__text` whose
   `data` is a writable private global, one object per occurrence, exactly as
   `stringConstOperand` does today (ir_emit.mx:1059; writable because Moxie
   literals are mutable). The core typechecker knows one untyped "static text"
   form, not a `string` type. This is the smallest change and reuses existing
   emission.
2. **A new `//:blob` pragma** binding a generated external symbol to an
   `unsafe.Pointer` global, with the bytes in a generated `.s` file. The
   assembler pass already exists (`collectAsmFiles` main.mx:3214) and linkdata
   already binds external symbols (`scanLinkdataPragmas` ir_rewrite.mx:287,
   `emitLinkDataGlobal` ir_emit.mx:1369), so this is mechanically close - but it
   adds a linker step to every stage that emits a message.
3. **Keep a literal-only `string` type in the base.** Cheapest to implement,
   weakest distillation: the core still reserves the 3-word layout.

Recommendation: (1). It keeps the base free of a text *type* while keeping the
data in the object file the compiler already knows how to write.

Two intrinsics, not one, because their semantics differ:

- `__blobtext("...")` for **user literals**: writable, one object per
  occurrence, so Moxie's mutable-literal identity rule holds.
- `__msgtext("...")` for **compiler-generated messages** (panic texts, site
  messages, tag names): read-only and interned by content, because generated
  code never mutates them. Without this split, five hundred generated accessors
  that all say "index out of range" produce five hundred globals. The phase
  plan's `__blobtext` must be read as `__msgtext` wherever the emitter writes
  the message.

**Ordering with S.** `string` is `[]byte`, so T and S share the `__slice` header (with `__text` as an
alias). They can be one binary with two passes, or T can emit `__text` and S can
treat it as the byte case of `__slice`. Do not let them
diverge: a `string` and a `[]byte` must remain assignable to each other after
lowering, exactly as today.

**Leaves the downstream compiler:** `TCString` as a distinct kind, string
literal emission, `stringEqual`/`stringConcat` call selection, all text
conversions and text `range`, and the string-specific branches of
`emitConvert`, `emitBinOp`, `emitIndexAddr` and `emitSliceOp`.

---

### 4.4 S - `mxc-slice`: slice, array and range lowering

**Removes:** the `Slice` type; every array *operation* (indexing, slicing,
length arithmetic); `len`/`cap`/`push`/`pop`/`resize`/`copy` on slices; spread
pushes; subslice semantics; `range` over slices/arrays/text/int;
`unsafe.Slice`/`unsafe.SliceData`; slice literals; slice-typed named types.

**Moxie has no array type.** There is nothing to "keep as a storage form".
Fixed extents in the tree are one of three things, none of them an array type:

- **linkdata**: an external symbol bound to a slice header (`//:linkdata`),
  with the all-ones capacity sentinel that marks foreign storage
  (`scanLinkdataPragmas` ir_rewrite.mx:287, `sliceExternalCap`
  src/runtime/panic.mx:222);
- **explicit mapping**: a DMA buffer, framebuffer or shared-memory region
  returned as `(unsafe.Pointer, length)` and wrapped in a `__slice` header;
- **compiler-emitted static data**: the IR emitter writes a private data object
  (today a `[N x i8]` global) and the source constructs a header over it.

Array *declarations* still in the tree (`[8]uint8` tophash, `[256]byte` codec
scratch, `[3]uintptr` casts, `[512]pollDesc`, `[20]byte` digits, `[7]byte`
reserved) are leftover Go slice-bypass code, as claude.md says. S removes them:
inline storage becomes named scalar fields or a blob accessed by `unsafe.Add`;
globals and scratch become pointer+extent allocations; casts become pointer
arithmetic at an offset. The base has **no array type and no array
operations**, and the output must contain neither.

**Dependency:** S runs before E, and its output is not runnable until E has
generated the encode/decode pair for the lowered layouts (§4.6). E consumes
S's/M's output; it cannot precede them.

**What exists today**

| part | location |
| --- | --- |
| type model | pkg/types/tc_types.mx `Slice`, `Array`; `NewSlice`/`NewArray` |
| AST lowering | ssa_builder.mx:8455 `buildSliceLit`, :8010 `buildArrayLit`, :8070+ constant array text, :6606 `emitPopShrink`, :3790 `buildRangeLoop`, :6363 `buildBuiltinCall` (push/pop/resize/copy), :5755 `checkSliceToArrayConv`, :6292 `coerceSliceArgs` |
| SSA | ssa_types.mx:563 `SSAMakeSlice`, :589 `SSASlice`, :611 `SSAIndexAddr`, :577 `SSAConstMemCpy` |
| IR | ir_emit.mx:3647 `emitMakeSlice`, :3733 `emitSliceOp`, :3316 `emitIndexAddr`, :3270 `emitIndexBoundsCheck`, :2880 `emitConvert`; ir_types.mx:283 `sliceType`; ir_call.mx:448-900 len/cap/push; ir_iter.mx `emitRange`/`emitNext*` |
| relocation | ir_deepcopy.mx slice/array branches; runtime slice.mx:42 `slicePush`, :79 `sliceConcat`, :14 `sliceCopy` |
| legacy | compiler.go `createBuiltin` :2176, `createExpr` :2867; transform/interface-lowering not involved |

**Lowered form**

```moxie
// One header for every element type (rule 7); the element type lives in
// the accessor's name and return type.
func __slice_at_uint32(s __slice, i int32) *uint32 {
    if i < 0 || uintptr(i) >= s.len { panic(__blobtext("index out of range")) }
    return (*uint32)(unsafe.Add(s.data, uintptr(i)*unsafe.Sizeof(uint32(0))))
}
func __slice_slice_uint32(s __slice, lo, hi int32) __slice   // alloc + copy
func __slice_push_uint32(s __slice, v uint32) __slice        // runtime.slicePush
func __slice_concat_uint32(a, b __slice) __slice             // runtime.sliceConcat
func __new_slice_uint32(n int32) __slice                     // runtime.alloc, n*4
func __slice_copy_uint32(dst, src __slice) int32             // runtime.sliceCopy
```

A **blob** is a named region of memory, not an array type: a compiler-emitted
data object, a `//:linkdata` symbol, or a mapped device region. Generated code
holds `(unsafe.Pointer, len, cap)` in a `__slice`/`__text` header and every
access is `unsafe.Add` from that pointer. Static literals become a compiler
data object plus a header; dynamic ones call `runtime.alloc`; mapped buffers
come from the mapping call. There is no `[N]T` declaration in the source.

**Range becomes a loop, always.** `for i, v := range s` becomes

```moxie
__r := s
for i := int32(0); i < int32(__r.len); i++ {
    v := *__slice_at_<elem>(__r, i)
    ...
}
```

with the range expression evaluated once into a temporary. The existing SSA
lowering already does exactly this shape (`buildRangeLoop` ssa_builder.mx:3790,
`emitNextSlice` ir_iter.mx), so the source pass is a re-expression of known
semantics. `for i := range n` becomes an integer loop. `range` over text uses
`__text_at`; `range` over a mapped extent uses its length.

**Named slice types with methods.** `type Names []string` becomes
`type Names __slice` (same layout, distinct type, so it can carry methods),
and its methods are rewritten in place:
`len(n)` → `n.len`, `n[i]` → `*__slice_string_at(n, i)`, `n = push(n, x)` →
`n = __slice_string_push(n, x)`, `for ... range n` → the loop above. The
existing value-receiver rewrite (`recvrewrite.go:28`, mirrored in
ssa_builder.mx:918) is the precedent for the receiver form.

**Leftover array declarations.** The runtime and compiler still declare fixed
arrays, and Moxie does not have them. S rewrites every site:

- `tophash [8]uint8`, `_pad [8]uint8` in hashmap.mx:31-44: inline layout. Replace
  with eight named `uint8` fields (or a `uint64` accessed by shifts) so the
  bucket size and offsets are unchanged; the field access is ordinary struct
  access, not indexing.
- `codecBufScratch [256]byte`, `codecSeenTab [256]unsafe.Pointer`,
  `codecDecodeSeenTab`, `pollDesc [512]pollDesc`: globals/scratch. Replace with
  a `runtime.alloc` block plus a pointer/extent, or a `//:linkdata`/mapped
  region; access through `unsafe.Add`.
- `(*[3]uintptr)(p)` and friends in offsetframe.mx: replace with
  `*(*uintptr)(unsafe.Add(p, off))` at the explicit offsets.
- `digits [20]byte` in print.mx, `[7]byte` reserved in os_linux.mx,
  `[64]unsafe.Pointer` in relocate.mx: the same treatment.

This is real work in the runtime, not a formality, and the ledger must count it
(§7). A base-profile compile rejects any array declaration that survives.

**Bounds checks.** The generated accessor checks and panics. This preserves
today's behaviour but moves the check from compiler-emitted IR
(`emitIndexBoundsCheck` ir_emit.mx:3270) into generated source. That is the
point: the core compiler no longer needs an index operation at all. Note that
today the check is deliberately a *call* (`lookupCheck` panic.mx:194), not an
inline branch, because a branch introduced between a value and its phi
predecessor breaks the SSA emitter (ir_emit.mx:3298). A generated accessor can
branch freely because it is an ordinary function; that is one of the concrete
simplifications the split buys.

**The relocation dependency, stated plainly.** A `__slice` is a struct with
an `unsafe.Pointer` field. The core compiler can no longer know that the pointer
points to `len` elements of `T`, so it cannot deep-copy the backing when the
slice is returned. Today the return protocol does exactly that through
`typeNeedsCodec` and the structural codec (ir_deepcopy.mx:713-800). Therefore S
either:

- **S1:** leaves the core with a compiler-known slice type (i.e. does not finish
  the job), or
- **S2:** also lands E (§4.6), so a generated `__reloc_slice_T` moves the
  backing into the caller's arena, or
- **S3:** changes the memory model so returned backings are already owned by
  the caller (not acceptable: it needs escape analysis).

This is the single most important ordering constraint in the whole plan: **S is
not semantically complete without E.**

**Leaves the downstream compiler:** `Slice`/`Array` types, `SSAMakeSlice`,
`SSASlice`, `SSAIndexAddr`, `SSAConstMemCpy`, `SSARange`/`SSANext`, `len`/`cap`/
`push`/`pop`/`resize`/`copy` builtins, bounds-check emission, slice relocation,
and the slice/text branches of the IR emitter.

---

### 4.5 M - `mxc-map`: map lowering

**Removes:** the map type, `make(map)`, `m[k]`, comma-ok lookup,
`m[k] = v`, `delete`, `clear`, `len(m)`, map literals, `range` over maps, and
map-typed named types.

**What exists today**

| part | location |
| --- | --- |
| type model | pkg/types `TCMap` |
| SSA | ssa_builder.mx:6363 builtins, :1781+ index assignment/lookup, :3790 range; ssa_types.mx:540 `SSAMakeMap`, :631 `SSALookup`, :779 `SSAMapUpdate` |
| IR | ir_map.mx (163 lines): `emitMakeMap`, `emitMapUpdate`, `emitLookup`; ir_iter.mx:265 `emitNextMap`; ir_call.mx:448 `len` |
| runtime | hashmap.mx:14 `hashmap`, :31 bucket, :38 iterator, :62 `hashmapMake`, :304 `hashmapSet`, :477 `hashmapGet`, :513 `hashmapDelete`, :549 `hashmapNext`, :613 `hashmapSortKeys`, :653 `hashmapBinarySet`, :661 `hashmapBinaryGet`, :699 `hashmapContentSet`, :722 `hashmapContentGet` |
| ordering guarantee | `hashmapNext` walks sorted keys (`hashmapSortKeys` :613); map iteration is deterministic today |
| legacy | compiler/map.go; compiler.go `createBuiltin` :2176 |

**Lowered form**

```moxie
// One __map header for every K,V (rule 7); the accessor names carry K,V.
type __map struct { p unsafe.Pointer }   // runtime.hashmap handle

func __map_K_V_make(hint int32) __map
func __map_K_V_get(m __map, k K) (V, bool)
func __map_K_V_set(m __map, k K, v V)
func __map_K_V_del(m __map, k K)
func __map_K_V_len(m __map) int32
func __map_K_V_clear(m __map)
func __map_K_V_next(m __map, it *__map_iter_K_V) (K, V, bool)
```

The accessor body is a thin call to the runtime, with the sizes and hash/equal
functions chosen by K at generation time:

```moxie
func __map_string_int32_get(m __map, k __text) (int32, bool) {
    var v int32
    ok := runtime.hashmapContentGet((*runtime.hashmap)(m.p), k, unsafe.Pointer(&v),
        unsafe.Sizeof(int32(0)), nil)
    return v, ok
}
```

For a struct key the binary path is used; for an interface key (present only if
I has already run) the generated key-hash/equal functions dispatch on the tag.
The type's arena owner is preserved because the runtime records it at
`hashmapMake` (hashmap.mx:95 `hashmapOwner`), so generated code does not need to
know about arenas.

**Map iteration.** The generated iterator calls `hashmapNext`, which uses the
sorted-key buffer (`hashmapSortKeys`). Keep that: iteration order is observable
and Moxie deliberately made it stable. A hand-rolled bucket walk would change
program output. The iterator state is also a hard size contract: the runtime
`hashmapIterator` is 48 bytes (hashmap.mx:38) and the compiler hard-codes
`alloca [48 x i8]` for it (ir_iter.mx:11). A generated iterator struct must
match that size, or both sides must be changed together.

**Named map types with methods.** Same treatment as named slices: a generated
struct holding the handle, methods rewritten to the accessors.

**Leaves the downstream compiler:** `TCMap`, the map SSA nodes, ir_map.mx,
map `range`, map branches of `len`, and the map-specific relocation/codec
knowledge (a map handle is an owned object; E must generate its copy/clone).

### 4.6 E - `mxc-codec`: codec, descriptors and relocation generation

**Removes from the compiler:** the type-directed return protocol, the
structural codec's type walk, type descriptor synthesis, and the "does this type
contain a reference" analysis.

This is the stage that makes the rest of the distillation possible. Everything
above lowers a type into structs and pointers, and in doing so destroys the
compiler's ability to answer "what is inside this value". The compiler's
answers to that question are:

| question | today | location |
| --- | --- | --- |
| does this type need relocation on return? | `typeNeedsCodec` / `Depth` | ir_deepcopy.mx:8, :12 |
| does this function? | `funcNeedsCodec` | ir_deepcopy.mx:47 |
| encode / decode a value | `emitCodecEncode` / `emitCodecDecode`, structural walk in the runtime | ir_deepcopy.mx:969, :985; codec.mx:918, :1308 |
| descriptor for a type | `typeDescName`, `emitTypeDesc*`, `runtimeKindOf`, size/align/offsets | ir_descriptors.mx:87, :278, :342-487, :690-1383 |
| channel frame budget | `frameBudget` / `frameBudgetDepth` / `offsetFrameMax` | ir_chan.mx:846, :916, :950 |
| spawn argument budget and channel marking | `ir_call.mx:1389`, :1131-1500 | |
| sovereign-arena borrow/compaction | `emitArenaPrologue`, `emitSovereignEpilogue`, `isSovereignNamedType` | ir_deepcopy.mx:386, :547, :99 |
| map handle copy on return | `emitFixupMaps` | legacy compiler.go:5075 |

Every one of these needs type structure that a lowered `unsafe.Pointer` no
longer carries.

Note for anyone reading older design notes: the `RelocInit`/`RelocMark`/
`RelocForward`/`RelocCompact`/`RelocFixupAll` functions in
`src/runtime/relocate.mx` are **not emitted by either compiler**. The live
return protocol is codec encode → `FnArenaPop` → decode (ir_deepcopy.mx:811-928,
`emitRetWithCodec` :707). The statements in claude.md that describe the
three-pass mark/compact as the current protocol are stale; the structure is
still useful as a description of the ownership model, but the code to preserve
is the codec path.

**Mechanism E1 (descriptors as generated data).** The stage emits, per type,
the descriptor the runtime already understands (`RawType` family in
typekind.mx:57-96) as ordinary Moxie data plus a constructor. Generated channel
sends and returns pass the descriptor explicitly. The runtime's structural codec
stays. This removes `ir_descriptors.mx` from the core and makes frame budgets
generated constants, but it does **not** remove the return relocation decision:
the core would still have to know which returns carry descriptors. E1 therefore
needs a small core hook (a `//:reloc` annotation or a generated per-function
metadata file), and it does not by itself unlock slice erasure.

**Mechanism E2 (per-type functions).** The stage generates, per type:

```moxie
func __enc_T(cb *runtime.CodecBuf, seen *runtime.SeenSet, v *T)
func __dec_T(cb *runtime.CodecBuf, seen *runtime.DecodeSeen, v *T)
func __equal_T(a, b *T) bool
func __hash_T(a *T, seed uintptr) uint32
```

The pair, not a single `__reloc_T`, is required by the actual protocol: the
callee encodes while its own arena is current, `FnArenaPop` reclaims the frame,
and the caller-side decode allocates the copy in the caller's arena
(ir_deepcopy.mx:811-870). A copy function called *after* the pop would allocate
its destination from memory the source may already be reusing. If a future core
changes the protocol to a caller-owned destination, `__reloc_T(dst, src)` can
replace the pair; it is not a drop-in today.

Both functions need a seen-set for cycles, exactly as the current codec does
(codec.mx `codecSeenSet`). The generated functions must be able to reach the
seen-set/arena APIs, which are currently unexported runtime internals. They
cannot live in package `runtime`: the encoders are per user type, and runtime
cannot import user packages. So `runtime` must grow an exported mini-ABI for
codec buffers, seen sets, arena allocation and stable registration, and the
generated functions live in the package that declares the type (exported, per
rule 7, because a callee in another package calls them).

The core then needs exactly **one** piece of type knowledge: "returns of this
type use this encode/decode pair". That is a declarative annotation on the
generated type, for example:

```moxie
//:codec __enc_Node, __dec_Node
type __Node struct { ... }
```

and the compiler's prologue/epilogue calls the named functions instead of
walking the type. The buffer allocation, pop, decode and release scheduling stay
in the core (they are type-independent); only the walk is outsourced. This
single hook replaces `ir_deepcopy.mx`'s ~1,000 lines and the descriptor-driven
codec path for returns.

**Generated-code contract: a restricted `unsafe`.** E replaces the compiler's
type-directed walk with generated code that necessarily manipulates pointers
through `unsafe`, while the compiler's arena and relocation behavior depends on
knowing where the pointers are. That looks circular, and it is unless the
generated code is constrained so the compiler never has to reason about its
internals:

- The compiler calls the generated encoder/decoder through the `//:codec` hook
  and treats them as opaque leaves. It does not type-walk the value across the
  call. The protocol boundary - encode in the callee arena, `FnArenaPop`,
  decode in the caller arena - is what makes this sound.
- Generated code obeys a **restricted unsafe subset**:
  1. allocate only through `runtime.alloc`/the current arena; never let a
     pointer to a local frame escape (no `&local` returned or stored);
  2. store references as typed `*T` wherever a field must survive a boundary;
     `unsafe.Pointer` and `uintptr` are transient inside one expression, never
     a persisted reference (`uintptr` fields are offsets, not addresses);
  3. derive every offset from `unsafe.Sizeof`/`unsafe.Offsetof`, never from a
     hand-computed constant, and never add past the known extent of an
     allocation;
  4. handle cycles with the runtime seen-set, not unbounded recursion;
  5. return only values whose pointers are into the current arena and are
     covered by the caller's decode/relocation, or plain scalars;
  6. no closures that capture (already a language rule).
- Enforcement is a **generated-code verifier**: a lint pass over the emitted
  source for `uintptr` fields holding addresses, returned `unsafe.Pointer`
  values, `&local` escapes, and `unsafe.Add` beyond a declared extent; plus
  tests that force arena reuse after the pop and check the decoded value.

If a rule cannot be verified mechanically, E cannot be trusted and falls back
to E1's typed descriptors and the runtime's structural walk. This is the
foundation choice the previous draft did not price: either generated code is
constrained enough to be opaque, or the compiler keeps type knowledge.

**Sovereign arenas are the hard part.** A self-mutating receiver owns an arena,
and the codec decides what is stable inside it (`codecSovereignArena`,
`SovDrainCompactions`, `codecRegStable`; see claude.md "Sovereign Arena"). A
generated reloc function does not know that a pointer is into a sovereign arena
unless the generated code asks the runtime, which it can: `runtime` already
exposes the registration functions. But compaction decisions read the
*structure* of a value to decide whether data escaped. If E2 moves that walk
into generated code, the generated code must reproduce the escape test. This is
the largest single risk in the whole plan; see §9.

**Ordering constraint.** E2 must run **after** I, T, S, M and **before** C, so
channel element types are already plain structs and the encoders are generated
over the final layout. E1 can run earlier, but it does not buy the slice/map
erasure.

**Legacy:** the legacy compiler's `emitCodecEncodeReturn` /
`emitCodecDecodeReturn` / `emitFixupMaps` (compiler.go:4896, :5023, :5075) are
the reference for exactly which values the return protocol must handle. The
runtime codec is shared source (`src/runtime/codec.mx`), so its behaviour is the
contract both compilers must satisfy.

---

### 4.7 C - `mxc-chan`: channel, select and spawn lowering

**Removes:** the channel type, `make(chan)`, send/receive statements,
`select`, `range` over a channel, `close`/`len`/`cap` on channels, `closed` and
`shutdown` as predeclared words (they become generated handles), and the
compiler's frame-budget analysis.

**What exists today**

| part | location |
| --- | --- |
| type model | pkg/types `TCChan`; parse pkg/syntax/parse_type.mx:56, :81 |
| SSA | ssa_builder.mx:4371 `buildSelect`, :2070 `coerceSendValue`, :3790 range; ssa_types.mx:549 `SSAMakeChan`, :757 `SSASend`, :791 `SSASelectState`, :823 `SSASelect` |
| IR | ir_chan.mx:135 `emitChanRecv`, :148 `emitExtentChanRecv`, :260 `emitStructuralChanSend`, :296 `emitExtentChanSend`, :328 `emitStructuralChanRecv`, :366 `emitChanSend`, :379/:422 codec send/recv, :462 `emitSelectFrame`, :482 `emitSelect`, :796/:846/:950 frame budget |
| element policy | `elemNeedsReloc` ir_chan.mx:210; `codecIface` :45; `emitCodecIface` :121 |
| spawn | ir_call.mx:1131-1500 (arg packing, channel marking, `runtime.spawnDomain` :1484), `checkSpawnCalls` :1256, `spawnTargetChannelGlobal` :1319; ir_wasmspawn.mx |
| runtime | chan.mx:53 `channel`, :177 `chanSelectState`, :195 `codecFrameEncode`, :285 `codecFrameDecode`, :316 `chanMake`, :422 `chanSend`, :519 `chanRecv`, :663 `chanClose`, :739 `chanSelect`; pipe_channel.mx, ringbuf.mx |
| termination words | `rewriteShutdownWord` ir_rewrite.mx:481; `__shutdown` builtin pkg/types/tc_universe.mx:298; `shutdown`/`closed` checks ir_call.mx:1238 |
| legacy | compiler/channel.go, compiler/spawn.go, compiler/goroutine.go |

**Lowered form**

```moxie
// One __chan header for every element type (rule 7).
type __chan struct { p unsafe.Pointer }   // *runtime.channel

func __chan_T_make(depth, budget int32) __chan   // elemSize baked in
func __chan_T_send(c __chan, v T)
func __chan_T_recv(c __chan) (T, bool)
func __chan_T_close(c __chan)
func __chan_T_len(c __chan) int32
func __chan_T_cap(c __chan) int32
```

Each send/recv chooses one of three routes, and the choice is made by the stage,
not the core: raw extent (`elemNeedsReloc` false), the type's own codec, or the
structural codec. The generated function passes the size, and the codec /
descriptor pointers that E produced:

```moxie
func __chan_T_send(c __chan, v T) {
    runtime.chanSend((*runtime.channel)(c.p), unsafe.Pointer(&v), nil)
}
```

For a relocating element the same body passes the encoder identity as the
runtime expects it. After I, `moxie.Codec` is the generated `moxie.Iface_Codec` struct,
so the generated code builds that struct around the concrete encoder.

**Select.** `select` cannot hold arbitrary statements inside a generated helper
without closures (and closures do not capture), so the rewrite hoists:

```moxie
__sel, __ok := runtime.chanSelect(nil, __states, __ops)
switch __sel {
case 0:  /* arm 0 body */
case 1:  /* arm 1 body */
default: /* default body */
}
```

`__states` and `__ops` are `__slice`s over a runtime-allocated block of
`runtime.SelectState` and `runtime.ChannelOp`, built with the arms' channels,
value pointers, codec structs and descriptors; `__ops` carries whether each arm
is send or receive. The runtime's own parameters are slices, so S has already
lowered those signatures to `__slice` by the time C calls them - C must not
assume the source-level `[]T` spelling.

The construction must be shown, because it is the one place generated code
builds a slice header by hand:

```moxie
n := int32(len(arms))
size := uintptr(n) * unsafe.Sizeof(runtime.SelectState{})
states := __slice{data: runtime.alloc(size, nil, nil), len: uintptr(n), cap: uintptr(n)}
var i int32
for i = 0; i < n; i++ {
    st := (*runtime.SelectState)(unsafe.Add(states.data, uintptr(i)*unsafe.Sizeof(runtime.SelectState{})))
    runtime.SelectStateInit(st, armChan(i), armValue(i), armCodec(i), armDesc(i))
}
sel, ok := runtime.chanSelect(nil, states, ops)
```

This exposes a runtime-surface requirement the earlier draft missed:
**generated code lives in user packages, so every runtime type and field it
touches must be exported or constructed through an exported function.**
`hashmap`, `channel`, `chanSelectState`, `channelOp` currently have unexported
names and fields. The runtime must add exported aliases for the opaque handles
(`type HashMap = hashmap`, `type Channel = channel`, `type SelectState =
chanSelectState`, `type ChannelOp = channelOp`) and exported constructors or
setters for fields the stage fills (`SelectStateInit`, `ChannelOpInit`). The
alias preserves layout and type identity; the constructor is the only way to
set an unexported field from another package. This is part of the runtime
boundary (§5.5), not a convenience. The existing `emitSelect` (ir_chan.mx:482) and
`emitSelectFrame` (:462) are the exact template, and the runtime already
implements the whole protocol (`chanSelect` chan.mx:739). The hard part is
source syntax: evaluating the channel and send-value expressions once, binding
received values, and preserving `case <-closed: return`.

**Frame budget.** The budget is a compile-time constant derived from the
element type and its `cap` struct tags (`frameBudget` ir_chan.mx:846). The stage
computes it while the type is still visible and emits it as a literal into
`chanMake`. The `cap` tags themselves survive the printer as ordinary struct
tags.

**Range over a channel.** Stage4's `emitNext` has no `*TCChan` arm
(ir_iter.mx:21 falls through to the slice path), so a `for v := range ch` is not
lowered there. If the language permits it, C must generate the receive loop
explicitly; do not assume existing support.

**Spawn.** The spawn call is not a simple rewrite: it packs arguments into an
arena block, marks each channel argument's ring slot, verifies the
`closed chan struct{}` result shape, rejects closure captures, and calls
`runtime.spawnDomain(fn, argp, nChans)` (ir_call.mx:1484). All of that needs a
**function code pointer**, which the base language does not expose as a value.
Two options:

- keep `spawn` as a core builtin (operating on lowered `__chan` handles), or
- add one base intrinsic, `__fnptr(f) unsafe.Pointer`, and lower spawn at
  source level.

Recommendation: keep spawn in the core for the first cut. It is part of Moxie's
domain model, not a representation detail, and its arg-packing needs the arena
ABI. It is **not** exempt from E: the core's spawn packing calls the generated
`//:codec` encoders for each value argument, exactly as a channel send or a
return does. It must not keep `typeNeedsReloc` and a descriptor walk, or the
ledger overstates what E removed; the per-target encoder lookup stays and is
counted. The channel *types* and operations still lower; spawn then operates on
handles. Revisit only if a code-pointer intrinsic is wanted for other reasons.
The wasm path is separate (`ir_wasmspawn.mx`, `runtime_wasm.mx`) and stays
alongside the native one. If spawn stays in the core, its target-shape rule
(`closed` of type `chan struct{}`) must either run before C or be restated on
the lowered handle (`closed __chan`), and its argument codec must come from E
rather than from a type walk.

**`closed` and `shutdown`.** `shutdown` is a predeclared word rewritten to
`__shutdown()` today (ir_rewrite.mx:481). After lowering, it should be a
generated `__chan` handle from a runtime call, so the keyword disappears.
`closed` is a spawn target's named result; if spawn stays in the core, the rule
and the binding stay too.

**Leaves the downstream compiler:** `TCChan`, `SSAMakeChan`/`SSASend`/`SSASelect`,
all of ir_chan.mx, frame-budget analysis, the channel branches of the runtime
protocol selection, and the `shutdown` rewrite.

---

### 4.8 D - `mxc-defer` (optional): defer and method values

**Removes:** the `defer` statement, defer thunks/records, and method values
(if wanted).

**What exists today:** ir_defer.mx (353 lines), `buildDeferStmt`
ssa_builder.mx:4861, `buildDeferThunk` :4986, `buildDeferMethodTarget` :4886,
SSA nodes :727-745, and the recover block in the function prologue/return path.
The live runtime records are in `src/runtime/panic.mx:39` (`deferFrame`) and the
per-statement record in ir_defer.mx:76, arena-allocated so a defer in a loop
gets a fresh link; `src/runtime/defer.mx:5` `_defer` is dead (the compiler does
not emit it). The checkpoint before each call is inline assembly
(ir_emit.mx:203-220), which is the one construct with no portable source
lowering.

**Why it is optional - and a premise that is not established.** The intended
semantics are: panic is death and deferred calls do not run on the way out, so
`defer` is equivalent to "run these calls at normal return" and can be lowered
to explicit calls before each `return`. But the same compiler has a **Recover
block in the function prologue/return path** and an arena-allocated defer
record whose execution is driven by the panic path (`ir_defer.mx:163`). Those
two statements cannot both be true.

Before D is scheduled, a test must settle it: register a deferred call, panic,
and observe whether it runs. If it does, the lowering must reproduce unwinding
(unwind tables/longjmp, ordering across frames), which is far larger than
epilogue calls and probably not worth doing; if it does not, the Recover block
is vestigial for this purpose and the epilogue lowering stands. D is gated on
that measurement, not on the documentation.

If the premise holds, the lowering is local: evaluate the deferred call's
arguments into temporaries at the `defer` statement, then emit the call before
every `return`. Multiple returns and named results make it a real pass, but one
function wide and runtime-free. Do it only if the core's defer machinery is
judged worth removing: 353 lines plus the recover block, against representation
stages that are much larger.

### 4.9 F1 - `mxc-front`: the base front end (the AST step)

**What it is.** Parse and check the base language, run the remaining Moxie
rules, and hand a checked tree to F2. This is the "one step makes ast" in the
brief.

**What exists today:** the parser (`pkg/syntax`, 4,800 lines), the type model
and resolution (`pkg/types`, 3,900 lines), the declaration/registration half of
`typeCheckPkg` (ir_typecheck.mx:439), and the expression/statement checking
that **currently lives inside the SSA builder**. There is no separate checked
tree: `CreatePackage` (ssa_builder.mx:48) registers, builds every body, checks
assignments, and emits SSA in one pass. That is the single largest structural
obstacle to the AST/IR split.

**Required changes.**

1. **Extract a checked-tree pass.** Move the expression/statement type
   resolution and checking out of `ssaFuncBuilder.buildBody`/`buildExpr`/
   `buildStmt` into a pass that annotates the AST (or a parallel
   `map[syntax.Expr]Type`). The functions involved are the checking halves of
   `buildAssign` (ssa_builder.mx:1896), `buildExpr` (:5118), `buildCall` (:5922),
   `buildOperation` (:5465), `checkAssignable` (:2337), `checkCallArgs` (:2142),
   `operandType` (:3533), `exprStaticType` (:3560), and the constant evaluator.
   This is not a mechanical move: those functions interleave type checking with
   emitting SSA values, so the extraction is a real refactor (roughly the
   1,500-2,500 line region that resolves and checks operands).
2. **A base profile.** The same parser and checker should take a profile flag
   that rejects generics, interfaces, slices and every array declaration, maps, channels, text,
   `range`, `select`, type switches and `recover`. One parser, one checker, two
   profiles - not a fork.
3. **Move the rules that need pre-lowering syntax.** `checkTerminationSelects`
   (ir_call.mx:1238, "main must select on shutdown") and the spawn-shape rules
   (`checkSpawnCalls` :1256, `checkSpawnSignature` ssa_builder.mx:6707) inspect
   constructs that C removes. They must run **before** C - in practice as a
   validation pass on the pipeline's input, or at the start of C - not in F1
   where the construct no longer exists.
4. **Remaining restrictions.** The rules that still make sense on the base
   (`checkNamedReturns` ir_scope.mx:119, `checkGlobalMutation` :398,
   `checkClosureCaptures` :1851, `checkMissingReturn` :1656,
   `checkDuplicateCases` :1551, `checkPackageDecls` :169, `checkShadowing`
   :1075) move to F1 or to a dedicated validation pass. They need a live AST and
   object info, which F1 has.
5. **The AST product.** Today the AST is arena-allocated pointers
   (`ParseBytesArena` pkg/syntax/syntax.mx:45); there is no serializer and no
   deserializer. Two viable contracts:
   - **in-process:** F1 and F2 are functions in one binary, passing the tree in
     memory. No serializer, no new format; the steps are still separate passes
     and can be tested independently. Recommended first.
   - **separate processes:** define a binary AST encoding for
     `pkg/syntax/nodes.mx` (451 lines of node kinds) plus the type facts the IR
     needs (layouts, named types, signatures). A real, bounded project, but it
     should wait until the base language has stopped changing.

**Leaves the downstream compiler:** nothing yet - F1 *is* the downstream
compiler's front half. What it must **not** contain is any of the erased
features: no type parameters, no interfaces, no slices/maps/channels/strings, no
range/select/type switch, no descriptor or codec analysis.

---

### 4.10 F2 - `mxc-ir`: the IR step

**What it is.** Translate the checked tree into the IR. In the brief this is
"another step translates ast into ir".

**What exists today:** `ssa_types.mx` defines a clean, explicit IR (the 33
instruction variants declared at :416-830) and `ssa_builder.mx` is the only
producer. `ir_emit.mx:755` `emit()` turns that IR into textual LLVM.

**Two possible boundaries.**

- **IR = textual LLVM (recommended first).** F2 = SSA build + `ir_emit`, output
  `.ll` (already supported: `cctx.irOutputFile`, ir_typecheck.mx:1420,
  `flushDedup` ir_emit.mx:254). B then only runs the toolchain. No new format,
  no serializer, and the artifact is debuggable with `llvm-as`/`opt` directly.
  The cost is that the "IR step" still knows LLVM and type layout.
- **IR = the SSA program (purer).** F2 emits a serialized `SSAProgram`; the
  LLVM emitter then moves into B. This requires a serializer for `ssa_types.mx` and a stable encoding
  of types/layouts, and it moves `ir_emit` into the backend. It is the cleaner
  conceptual split and the one that makes the backend type-free, but it is new
  format work.

**What F2 can lose after the rewrite stages:** every SSA node that no longer
has an input - `SSAMakeInterface`, `SSAInvoke`, `SSATypeAssert`, `SSAMakeMap`,
`SSAMapUpdate`, `SSALookup`, `SSAMakeSlice`, `SSASlice`, `SSAIndexAddr`,
`SSARange`, `SSANext`, `SSAMakeChan`, `SSASend`, `SSASelect` - plus the builtin
handler (`emitBuiltinCall` ir_call.mx:448), the interface/chan/map/iter
emitters, the descriptor pass, and the type-directed return protocol (with E).
What remains is exactly the set in §5: allocas, fields, loads/stores, calls,
arithmetic, conversions, branches, phis, returns, globals, closures/defer if
kept.

**The split inside F2.** Even without processes, the SSA builder should stop
emitting IR-specific decisions. Today it chooses LLVM-facing shapes (e.g.
`emitMakeMap` builds a call signature, `ir_chan` picks a codec route). After the
rewrites those decisions are gone, and F2 becomes a small lowering from typed
operations to the IR instruction set.

---

### 4.11 B - `mxc-llvm`: the LLVM step

**What it is.** Run the LLVM toolchain: assemble the `.ll`, optimize, compile to
objects, link, and produce the executable. This is "the last step manages the
running of the llvm code generation".

**What exists today (main.mx):** `objStampFor` :438, `objReusable` :456,
`objTool` :482, `objCmd` :495, `objFiles` :535, `clangCompileToObj` :573,
`optBc` :353, `run`/`runAsync` :595/:614, the initAll generator
main.mx:8120-8185, the asm pass `collectAsmFiles` :3214, the runtime bake
`ensureRuntimeBake` :1182, `loadSysroot` :2711, and the link sections
:8299/:6859. The same logic is duplicated in the test command.

**Required changes.**

- Move the toolchain runner out of the compiler driver into one component with
  a single input: a list of `.ll`/`.bc`/`.o`/`.s` files plus a target, and a
  single output: an executable. Today `main.mx` mixes this with package
  resolution, caches, dispatch measurement and commands.
- Keep the assembler pass. `.S` files must still be assembled (the project's
  own rule), and the runtime's prebuilt objects still come from `sysroot/obj`.
- Keep the object cache, but key it on the `.ll` content plus the toolchain
  stamp; the current `objStampFor`/`objReusable` already do this and no longer
  need to know about packages.
- The runtime bake is a separate product: `ensureRuntimeBake` builds
  `sysroot/runtime_go.bc`. After distillation the bake must be produced by the
  *distilled* pipeline (or by legacy for bootstrap), not by the old compiler
  path. This is a build-system change, not a language change.

---

## 5. The base language (the fixed point)

### 5.1 What is allowed

The base language should contain exactly what the runtime needs and nothing
that a stage above can generate instead:

| construct | allowed? | notes |
| --- | --- | --- |
| `int8/16/32/64`, `uint8/16/32/64`, `uintptr`, `float32/64`, `bool` | yes | fixed widths; no `int`/`uint` |
| `struct` (with methods, embedding, tags) | yes | the only aggregate |
| pointers `*T`, `unsafe.Pointer` | yes | typed pointers preserve relocation hooks |
| `func` declarations, function values `{code, env}` | yes | closures do not capture (existing rule) |
| `if`, `for` (condition and 3-clause), `switch` on integers | yes | no `range`, no `select` |
| `const`, `var`, package globals, `init()` | yes | package globals immutable outside init (existing rule) |
| `panic`, `defer` (unless D lands), `unsafe.Sizeof/Offsetof/Alignof/Add` | yes | arithmetic on blobs |
| array type | **no** | no declarations, no indexing, no slicing |
| blob memory | yes, as pointer + extent | linkdata, explicit mapping, compiler data object; wrapped in a `__slice` header |
| static message literal | yes, one form | `__msgtext("...")` (read-only, interned); `__blobtext` for user literals |
| packages/imports, build tags, pragmas | yes | carried through every stage |
| `spawn`/`closed` | **keep in core** (recommended) | domain model, needs a code pointer |
| `__reloc_T` annotation | yes | the single type-directed hook (§4.6) |
| generics, interfaces, slices, maps, channels, `string`, `range`, `select`, type switches, `error` | **no** | removed by G/I/T/S/M/C |

### 5.2 What the generated code looks like

A slice of `uint32`, after S:

```moxie
// __slice is the one header from the generated shared package (rule 7).
// A framed-memory region is mapped by the runtime; no array type is involved.
func __slice_at_uint32(s __slice, i int32) *uint32 {
	if i < 0 || i >= s.len {
		panic(__msgtext("index out of range"))
	}
	return (*uint32)(unsafe.Add(s.data, uintptr(i)*unsafe.Sizeof(uint32(0))))
}

func __framebuffer_view() (s __slice) {
	ptr, n := runtime.MapFramebuffer(0)          // (unsafe.Pointer, uintptr)
	return __slice{data: ptr, len: n, cap: n}
}
```

A map of `__text` to `int32`:

```moxie
func __map_get___text_int32(m __map, k __text) (v int32, ok bool) {
	ok = runtime.hashmapContentGet((*runtime.hashmap)(m.p), k,
		unsafe.Pointer(&v), unsafe.Sizeof(int32(0)), nil)
	return
}
```

An interface method dispatcher (after I):

```moxie
func (i io.Iface_Reader) Read(p __slice) (n int32, err Iface_Error) {
	switch i.tag {
	case os.TagFile:
		return (*os.File)(i.data).Read(p)
	case bytes.TagBuffer:
		return (*bytes.Buffer)(i.data).Read(p)
	}
	panic(__blobtext("nil interface call"))
}
```

### 5.3 What the core typechecker no longer needs

This is the shrink that justifies the whole exercise. After the stages, the
core type system has: basics, named types, structs, pointers, signatures,
tuples, constants and `unsafe`. It does not need:

- type parameters, type arguments, constraint satisfaction, inference;
- interface types, method-set satisfaction, channel direction, `comparable`;
- slice/array/map/chan type constructors or their element rules;
- string/byte unification rules (text is gone);
- `typeNeedsReloc`/codec/descriptor kinds;
- range/select/type-switch type rules.

`pkg/types/tc_assign.mx` `Implements`, `methodSet`, `IfaceMissingMethod`;
`tc_types.mx` `TCInterface`, `TypeParam`, `TArgs`; the universe entries for
`error`, `comparable`, `append`-era builtins, `spawn`'s codec constraints - all
of these leave the core.

### 5.4 Deliberately not distilled - mechanisms, not representations

These are out of scope because they are platform mechanisms, not because the
runtime is exempt from the representation stages. The `.mx` sources of
`src/runtime` are input to G/I/T/S/M/E/C like any other package; what is not
rewritten is the allocator/spawn/panic/FFI machinery underneath them. The
distinction matters: if the runtime were exempt, every slice/string/interface
signature at the boundary would be a mixed ABI (§5.5).

Out of scope as mechanisms:

- **arenas and the allocator** (`src/runtime/alloc.mx`), including
  `FnArenaPush/PopFree`, `ArenaRelease` and sovereign groups;
- **domains and spawn** (pthread/TLS/rings/futex, `spawn.mx`,
  `pipe_channel.mx`, `ringbuf.mx`);
- **panic and death** (`panic.mx`), including the defer frame if D is not done;
- **FFI, C shims and `.S` assembly** (`ffi_linux.c`, `runtime_unix.c`,
  `asm_amd64.S`, the assembler pass);
- **coverage instrumentation** (`cstate.mx`, `emitCoverHitLine`
  ir_emit.mx:2096, `writeCoverSites` main.mx:3644) - it is orthogonal to
  lowering and should stay a backend/driver feature;
- **build tags, manifests, remote package resolution and caches** (the driver);
- **the type database** (`type_db.mx`): on the current tree it appears to be a
  reporting index (writes at ssa_builder.mx:2121, :4760; reads in stats and
  `cmdTypeDB`), not an input to emitted code - confirm before removing it. The
  dispatch index is different: it is emitted, so it moves with I.

### 5.5 The runtime boundary

This is the most likely source of silent miscompilation in the whole plan, so
it is a contract, not a footnote.

**The rule: no mixed ABI.** If a package is in the closure, its `.mx` source is
rewritten by the representation stages. That includes `src/runtime`: the same
passes that turn a user's `[]byte` into `__slice` and `error` into
`Iface_Error` turn the runtime's `[]byte` and `error` too. There is no
"user code lowered, runtime original" mode. A configuration that exempts a
package must instead expose a **raw boundary** and a generated thunk.

**Raw boundary.** Every hand-written interface to the runtime is raw already or
must become raw:

- C (`ffi_linux.c`, `runtime_unix.c`, `secalloc.c`), `.S` (`asm_amd64.S`,
  `asm_arm64.S`), `//:extern`, `//:linkname` and `//:wasmimport` symbols take
  and return only pointers, integers, floats and `unsafe.Pointer`. No slice,
  string, interface, map, channel or `error` crosses that line.
- The size-erased runtime APIs already obey this: `slicePush(ptr, len, cap, n,
  elemSize)`, `hashmapMake(keySize, valueSize, hint, alg)`, `chanSend(ch, value,
  op)`, `alloc(size, ...)` (src/runtime/slice.mx:42, hashmap.mx:62,
  chan.mx:422, alloc.mx:176).
- APIs that still name a source-level type are rewritten by the stages, not
  exempted: `stringEqual(string, string)` (string.mx:33) becomes `__text`;
  `hashmapContentSet(m, key string, ...)` (hashmap.mx:699) takes `__text`;
  `panic(message string)` (panic.mx:80) takes `__text`;
  `codecFrameEncode(c moxie.Codec, ...)` (chan.mx:195) takes the generated
  `Iface_Codec` struct. Their callers are rewritten in the same link, so the
  signatures agree.
- If a runtime function must remain in source-level form (a deliberate
  exception), it gets a generated thunk at the boundary that converts the
  lowered representation to the runtime's form, and the exception is listed.
  There is no third option.

**The bake.** `sysroot/runtime_go.bc` is a build product of the runtime source.
A tree whose runtime source was rewritten must rebuild the bake from that tree
(`build-runtime` with the same stage pipeline). Linking a rewritten app against
the baseline bake is exactly the mixed ABI this section forbids. The generator
never writes the bake into the source tree (§6.8 of the phase plan); the build
driver produces it beside the toolchain.

**Spawn.** Spawn stays in the core, but its argument packing and channel frames
must call E's generated encoders through the same `//:codec` hook as returns
and sends. The ledger must count that: the core keeps a per-target encoder
lookup, not a type walk. If spawn instead keeps `typeNeedsReloc` and the
descriptor walk, E has not removed the compiler's type knowledge from the
spawn path and the ledger is overstated.

**Audit and test.** After each stage, a boundary scan over every symbol that
crosses a package edge checks that its signature mentions only base types
(structs, pointers, scalars, `unsafe.Pointer`), with declared exceptions. The
fixtures include a runtime helper returning `error`, one taking `[]byte`, one
taking a map, and a spawn target whose frame holds a slice - these are the
cases that would silently misread memory if the ABI mixed.

## 6. Cross-cutting machinery

### 6.1 The printer and the source model (does not exist)

There is no Moxie source printer in `pkg/syntax`, and neither compiler uses one.
`pkg/syntax` has a parser and a scanner; `legacy/syntax` has a parser plus an
adapter to `go/ast` (so Go's `go/printer` is *available* on that side but is
never used); `legacy/mxtext` and stage4's `ir_rewrite` mutate bytes or nodes and
never print.
The only source generation that exists is `injectBuiltinString`
(ir_typecheck.mx:2671, legacy loader/stringers.go:79), which builds a small
fragment of text and parses it.

A printer needs to preserve, at minimum:

- every `//:...` pragma above a declaration (`//:build`, `//:linkdata`,
  `//:extern`, `//:linkname`, `//:embed`, `//:wasmimport`, `//:inline`,
  and the proposed `//:backing`, `//:codec`) and the `//:build` lines themselves;
- struct tags (`cap:"..."`), because frame budgets are derived from them
  (ir_chan.mx:846);
- string/rune literal escaping and raw strings, so byte content survives;
- semicolon insertion rules, so generated text parses identically;
- numeric literal forms, including untyped constants and shifts;
- file boundaries. Stage4 concatenates files (`concatSources` main.mx:3062) and
  maps diagnostics with `cctx.concatSourceMap`; a rewrite stage that emits per
  package must either preserve that map or emit one output file per input file.
  The second is strongly preferred: generated declarations get their own file
  (`zz_gen_<pkg>.mx`), user files stay recognizable.

A source model (per file: name, build constraints, imports, decl list, comment
map) is probably a better intermediate than printing from the raw AST, because
it lets a stage keep untouched declarations byte-identical and only reprint what
it changed. That reduces the blast radius of printer bugs and keeps diffs
reviewable.

**Gate for the printer: two tests, not one.** They prove different properties
and a printer can pass the second while failing the first.

1. **Byte identity for untouched code.** Every file, declaration, comment and
   pragma the stage did not change must be byte-identical to the input. A
   printer that reflows the whole tree fails this even when the AST round-trips
   perfectly. (The phase-G plan takes this further: it edits bytes and copies
   declarations instead of printing at all.)
2. **AST identity for printed constructs.** parse → print → parse the changed
   constructs and require structural equality (modulo the intended edit), then
   run the program and compare behaviour.

Do both before any semantic stage. A source model that keeps unmodified text as
byte ranges is how test 1 is made passable.

### 6.2 The typed AST (does not exist in stage4)

The rewrite stages need types: inference for G, method sets and conversion sites
for I, element types and sizes for T/S/M/E/C. `pkg/types` provides the type model
and resolution helpers, but the checking of expressions and statements happens
inside `ssaFuncBuilder` as it emits SSA (ssa_builder.mx:48, :1499, :5118). To
have a checked tree without SSA, that checking must be extracted.

A stage can avoid the extraction by running as a compiler mode (the generics
plan's choice): the SSA builder does the checking, and hooks emit source while
it runs. That is the cheap path and it is how G should land. It is not a
substitute for the extraction if stages are to run as separate programs, and it
does not shrink the core. The two ways below are for the standalone
destination:

- **Extract a checking pass from the SSA builder.** Move the type resolution and
  checking halves of `buildExpr`, `buildStmt`, `buildAssign`, `buildCall`,
  `buildOperation`, `operandType`, `exprStaticType`, `checkAssignable`,
  `checkCallArgs` into a pass that records `(Expr → Type)` and validates. The
  SSA builder then consumes the recorded types instead of re-deriving them.
  This touches the most intricate file in the compiler and must be mirrored in
  legacy (which already has the split via go/types). It is the honest path.
- **Write a second, lighter checker for the rewrite stages.** It would cover
  only what the stages need (declarations, expression types, method sets,
  conversion sites) and leave the core compiler's checker alone. The risk is
  divergence: two answers to "what is the type of this expression" is exactly
  the incoherence the project's axioms warn about.

Recommendation: extract the one checker. The pipeline and the core should share
it, with the base profile as a restriction of it.

### 6.3 The stage contract and driver

Each stage is:

```
input:  a package set (source files per package, build tags resolved),
        exports (.mxh or equivalent) for every dependency,
        the stage's own configuration (target, tags)
output: the same package set rewritten, exports regenerated,
        a diagnostic/source map for generated code
```

Rules:

- **No process-global state across packages.** Today `cctx` is one global
  (ir_typecheck.mx:88, :190) and a worker process owns one. A stage should take
  a program value, not reach for globals, so that two stages can run in one
  process for tests.
- **Deterministic, content-addressed cache keys.** Per-stage key = hash(stage
  version, input sources, dependency export keys, target, tags). Stage4's split
  between `pkgExportHash` (main.mx:2596: sources + imports' export keys) and
  `pkgHash` (main.mx:2488: adds the dispatch fingerprint) is the model: the
  expensive *whole-link* term must be in the object key, never in the export
  key (docs and the round-371 note in claude.md).
- **Validation before lowering.** Entry-point and spawn/select rules inspect
  constructs that C removes. Run them in a validation stage on the incoming
  source (or at the head of C), not in F1.
- **Stage versioning.** Every stage's output is Moxie source with a stage tag in
  a header comment (`//:stage gen=3`), so a stale work directory can be
  detected and rebuilt rather than silently mixed.

### 6.4 Export facts: two things are called `.mxh`

The name collides and the pipeline must not:

- **stage4 `.mxh`** (`generateMxh` main.mx:4494) is the **type export** used to
  typecheck dependents: package, imports, funcs, vars, consts, types, aliases,
  interfaces, generic function names, type parameter lists.
- **legacy `.mxh`** (`legacy/loader/mxh.go:41` `synthMXHPackage`, `header.go`)
  is an **FFI/protocol header**: codec types, symbol names for exported C
  functions, API compatibility. It is not the type-import format, and legacy
  typechecks imports in memory through the shared `*ssa.Program`.

A staged pipeline needs the first kind, and it needs it to evolve per stage:
after G it has no `$P` type parameters; after I no `iface` lines; after T/S/M/C
no slice/map/chan type syntax. The export format and the stage's checker must
change together. Do not reuse the legacy FFI `.mxh` for this.

### 6.5 Diagnostics and source maps

Generated code has no source position in the user's file. Two mechanisms are
needed:

- a **generated-code map** (generated file/line → origin construct), written
  beside the stage output, so a panic or type error in generated code names
  the user's line;
- preservation of the existing `concatSourceMap` behaviour (main.mx:3132,
  `remapErrorLines` main.mx:4079) for whatever still concatenates.

This matters more than it sounds: the rewrite stages will produce thousands of
lines of accessors and dispatchers, and a failure inside one must not read as a
failure in `zz_gen_slice.mx`.

**The specific case that will appear first: bounds checks.** A generated
`__slice_at_T` panics from inside the accessor, so the runtime's
`runtimePanicAt(returnAddress(0), ...)` resolves to the accessor's line in the
generated file, not the user's `s[i]`. The diagnostic map (§6.5) does not fix
this by itself because the panic has no site identity. The fix is explicit:
every generated accessor takes a **site ID** (or a `//:srcline` constant), and
the panic helper maps it through a stage-emitted `site -> file:line` table.
Bounds checks pass the use site's ID. The cost is a per-site table and an extra
argument on the hot path; it is the price of a readable failure. Alternatively,
specialize the accessor per call site and inline it; that trades code size for
the same result. Decide before S, because the accessor signature is baked into
every call the stage emits.

### 6.6 Pragmas, assembly and the toolchain

The pipeline must carry:

- `//:build` selection (done by `discoverPkg` main.mx:3780 before source is
  read) and per-file platform suffixes (`hasPlatformSuffix` main.mx:3197);
- `//:linkdata`/`//:extern`/`//:linkname` symbol bindings (scanned in
  ir_rewrite.mx:223-347);
- `//:embed` data (`scanEmbedPragmas` ir_rewrite.mx:101, `emitEmbedGlobal`
  ir_emit.mx:1396);
- `//:wasmimport` (`scanWasmImportMap` ir_rewrite.mx:387);
- `.S` files via the assembler pass (`collectAsmFiles` main.mx:3214);
- C files attached to packages (`pkg.cfiles` in the build loop).

A stage that emits source must not silently drop a pragma. This is a testable
property: for every input declaration, the output must carry the same pragma
set **excluding the stage's own header/tag** (`//:stage ...`), which the stage
adds and which would otherwise make the comparison fail on the first run. The
test compares only pragmas present in the input.

### 6.7 Runtime bake, sysroot and bootstrap

The runtime is compiled from source into `sysroot/runtime_go.bc`
(`ensureRuntimeBake` main.mx:1182, `BuildRuntime` legacy/main.go:101). Once the
pipeline exists:

- the bake must be produced by the distilled pipeline (or by legacy for
  bootstrap), not by the old compile path, or apps link the old ABI against the
  new generated code;
- `sysroot/obj` (prebuilt `.o`/`.bc`) and the C shims (`rt_shims.bc`) are part of
  that product and need the same treatment;
- the bootstrap protocol in claude.md still applies: legacy builds stage4,
  stage4 bakes, fixpoint check between generations. The new stages must be
  inserted without breaking the "legacy first, stage4 second" order.

The consequence for wood law is subtle but important: **legacy cannot be
slimmed.** It must keep compiling the full language (including the pipeline's
own stages, which are written in Moxie and use every feature) for as long as it
is the bootstrap compiler. Distillation shrinks the *stage4 core and its input
language*, not the bootstrap compiler.

### 6.8 Tests

- **Printer identity:** parse → print → parse, AST equal, program equal.
- **Stage golden tests:** a small program per feature, input/output source
  compared, plus a run test.
- **Differential tests:** every stage's output compiled by legacy (which still
  understands the full language) and by the next stage; behaviour compared.
- **Bilateral tests:** the semantics that both compilers implement today
  (interface dispatch, channel frames, map iteration order, string range)
  must be pinned *before* rewriting, because the rewrite fixes one behaviour
  and the divergence becomes invisible. The string-range divergence noted in
  §9 is an example of a decision that must be made first.
- **Self-host gate:** compile `_mxc_stage4` through the pipeline, then compile
  the pipeline itself, then require a fixpoint.

---

### 6.9 Generated names and mangling

Names are a correctness property, not cosmetics: the cache is keyed by content,
the generated tree is diffed, and two different types must never collide. The
document's earlier `__map_get___text_int32` is illustrative and ambiguous about
where the type name ends. The real grammar is:

```
G<kind>_<human>_<hash>
  kind  ∈ F (function), T (type), M (method), A (accessor)
  human ∈ [A-Za-z0-9_]{1,24}   sanitized declaration/method name, truncated
  hash  ∈ [0-9a-f]{16}          FNV-1a-64 of the canonical key, lowercase
regex: ^G[FTMA]_[A-Za-z0-9_]{1,24}_[0-9a-f]{16}$
```

Examples: `GF_Less_9f3a1c2d4e5b6078`, `GT_Box_0a1b2c3d4e5f6071`,
`GM_Get_77aa88bb99cc00dd`, `GA_slice_at_uint32_5e6f708192a3b4c5`.

**Canonical key** (the only input to the hash), length-prefixed so no
separator is ambiguous:

```
kind | len(pkgpath) pkgpath | len(decl) decl | len(method) method |
nargs | for each arg: len(argkey) argkey
```

`argkey` recurses: `basic:<name>`, `named:<len(pkgpath)><pkgpath>.<name>` plus
`[argkeys]` and the local-type tag; `ptr:<arg>`, `slice:<arg>`,
`map:<k><v>`, `chan:<dir><elem>`, `func:<params>-><results>`,
`struct:<fields>`. Length prefixes make `a_b` and `a` + `b` different keys.

- **Uniqueness:** a process-local registry maps name → key. A 16-hex collision
  between different keys is handled by extending to 32 hex and re-checking; a
  remaining collision is a build error, never a silent merge.
- **Stability:** the key is fully qualified and ordered, so the name is
  independent of map iteration, package discovery order, locale or the host
  path. A hash collision is a property of the input, not of the run.
- **Export:** the `G` prefix is uppercase, so every generated symbol is
  exported and nameable across packages (rule 7 in §3).
- **Methods on generic types** keep their source method name; only the
  receiver type is renamed. **Generic methods with their own type parameters**
  get `GM_...`. Accessors get `GA_...`.
- **Cache:** the name is part of the generated package content hash. Changing
  the mangling deliberately invalidates the stage's cache via the stage
  version, not by accident.

A test vectors table (fixed key → fixed name) is part of the stage tests, so a
refactor that changes naming is caught before it churns every cache entry.

### 6.10 Incremental propagation

Per-stage keys alone do not give a usable pipeline: if stage 3 changes, a naive
driver reruns stages 3..N for every package. The design that avoids this is
content-addressed **per-package** stage outputs plus a hash barrier:

- For each stage and package, compute a key from: stage version, the package's
  input content hash, the content hashes of its dependencies' outputs at the
  same stage, and - for a whole-link stage (G, I) - the link fingerprint.
- Run the stage; hash its output package. If the output hash equals the cached
  one, **stop propagating for that package**. Downstream stages' keys do not
  change because their input hashes did not change.
- If the output changes, only the downstream stages for that package and its
  dependents whose inputs changed rerun. The worst case is a full rebuild; the
  common case is one package and its dependents.

This is why the cache stores per-package outputs and not only a whole-link
tree. The whole-link key is correct for a first cut (and is what the phase-G
plan uses), but the long-term driver is per-package with a link fingerprint for
the stages that genuinely need the whole link.

Two dependencies make the barrier imperfect and must be measured:

- whole-link stages (G, I) can change placement/arms for many packages at once;
  their per-package output hashes decide how far the change propagates;
- an export-format change (`.mxh`) invalidates dependents even when a package's
  code is unchanged.

Test it by changing one leaf package in a large fixture and counting the
packages that rerun at each stage. If the count is the closure, the barrier is
not working.

### 6.11 The bypass flag

The pipeline is selectable from M0, not retrofitted:

```
mxc build -pipeline=off|<stage> ...
MOXIE_PIPELINE=off|<stage>
```

`off` is the current monolithic compiler. `generics` runs G and then compiles
the generated tree with the monolithic compiler; `generics,interfaces` runs G
and I, and so on. Every prefix is valid because the core compiler still
accepts the full language until F1 lands; a prefix simply feeds it less work.

The flag is the rollback path. If I lands and breaks `fmt`, `-pipeline=generics`
is the working build the same day, and the failing stage is diagnosed against
the previous one. It is also the differential harness: build the same fixture
at every prefix and compare behaviour. A stage that cannot be turned off is not
ready to land.

### 6.12 The `pkg/types` split

`pkg/types` is 3,877 lines and serves two audiences that want different
languages:

- the rewrite stages need the **full** model (generics, interfaces, slices,
  maps, chans, text) plus method sets and assignability;
- the core needs the **base** model (basics, named types, structs, pointers,
  signatures, constants, `unsafe`).

A profile flag rejects features at check time but does **not** shrink the
model: the core binary still contains the feature node kinds and the code that
handles them. Three ways to split, with honest costs:

1. **One model, profile-gated (recommended first).** No duplication, no build
   machinery, no risk of two answers to "what is the type of this expression".
   The core carries perhaps 500-800 lines of unused feature code and all of
   `pkg/types`; the binary does not shrink much.
2. **One package, feature files selected by build tag.** `tc_base.mx` always;
   `tc_iface.mx`, `tc_generics.mx`, `tc_slice.mx` under a `profile_full` tag.
   The base build omits them, so the core compiles less, but shared files must
   not call the omitted functions. `Implements`/`Assignable` interleave feature
   rules; they would need a hook table installed by the feature files, which
   reintroduces runtime dispatch and an initialization order. This is real
   refactoring, not a build-tag afterthought.
3. **Separate packages with an extension interface.** Cleanest boundary, most
   refactoring: the base checker exposes the points a feature package
   implements, and the full compiler wires them. It duplicates nothing but
   moves a lot of code.

Recommendation: (1) now, because the 64 KB target is not met regardless and
the pipeline's value is modularity. Revisit (2) only if a binary-size
measurement shows `pkg/types` dominating the core, and only with the hook table
designed first. (3) is not worth it for this phase. The risk entry in §9
records that the split is unresolved; this section is the decision path.

## 7. Deletion ledger

What leaves each binary once the stages are real. Line ranges are approximate;
the point is the mass and the owner.

| current location | lines | owner after split | disposition |
| --- | --- | --- | --- |
| ssa_builder.mx generic region (:10480-11350) | ~900 | G | deleted from core |
| ssa_builder.mx generic method path (:7177-7350) | ~200 | G | deleted |
| pkg/types `TypeParam`/`TArgs` paths | ~150 | G | profiles only |
| ir_types.mx `namedTParams`/substitution | ~80 | G | deleted |
| ir_descriptors.mx `instantiateTail`/TArgs | ~80 | G | deleted |
| main.mx `mxhWriteTParams`/`parseMxhTParams` | ~60 | G | deleted in mono exports |
| ir_iface.mx entire file | 2,366 | I | deleted from core |
| ssa_builder interface coercion/type switch (:2109, :4145, :4961) | ~250 | I | deleted |
| pkg/types `TCInterface`, `Implements`, `methodSet` | ~300 | I | deleted in base profile |
| main.mx dispatch index/fingerprints (:6082, :7175, :7342, :7392) | ~700 | I | deleted |
| ir_descriptors.mx method-set/descriptor emission | ~600 | I/E | generated |
| ir_binop.mx text compare/concat | ~200 | T | deleted |
| ir_emit.mx text conversions | ~150 | T | deleted |
| ssa_builder.mx slice/array literals, push/pop, range (:3790, :6606, :8010, :8455) | ~500 | S | deleted |
| ir_emit.mx make/slice/index/bounds (:3270-3900) | ~600 | S | deleted |
| ir_iter.mx | 265 | S/M | deleted |
| ir_call.mx len/cap/push/pop/resize/copy/spread (:448-1100) | ~650 | S | deleted |
| ir_map.mx | 163 | M | deleted |
| ssa_builder.mx map ops/range | ~150 | M | deleted |
| ir_deepcopy.mx return codec/relocation (:386-1012) | ~620 | E | generated/hook |
| ir_descriptors.mx type descriptors (:87-1383) | ~1,300 | E | generated |
| ir_chan.mx entire file | 1,153 | C | deleted from core |
| ssa_builder.mx select/send/chan make (:2070, :4371) | ~250 | C | deleted |
| ir_call.mx spawn packing (:1131-1500) | ~370 | core (kept) | domain model; calls E's `//:codec` encoders, no type walk |
| runtime leftover arrays (~20 sites: hashmap tophash/pad, codec scratch, offsetframe casts, poll, print, relocate, os) | ~200-400 | S | rewritten to fields/blobs/unsafe.Add |
| runtime `AddCleanup[T,S]` (runtime.mx:209) | ~20 | G | monomorphized or deleted |
| ir_defer.mx + recover block | 353+ | D (optional) | lowered to epilogue calls |
| main.mx toolchain/link sections | ~1,500 | B | moved to driver |
| main.mx package resolve/caches/mxh | ~2,500 | driver | shared |

Rough total: about **12,000-14,000 of the 45,500 stage4 lines** are feature
code that a fully distilled core would no longer contain, and a further
~4,000 move into generated source or the driver. The core that remains is
mostly parser/checker/SSA/emit for structs, pointers, ints, calls and control
flow, plus the arena and the runtime.

---

## 8. Milestones, effort and abandon points

### 8.1 Effort

New code, not moved code; estimates are order-of-magnitude (+/-50%), because the
point is to schedule and to abandon rationally, not to promise a date.

| milestone | new code (lines) | risk | depends on |
| --- | --- | --- | --- |
| M0 printer + source model | 1,500-2,500 | medium: comments/pragmas | - |
| M0 typed-AST extraction | 1,500-2,500 | **highest**: intricate, behaviour-preserving | - |
| M0 driver + cache + bypass flag | 1,000-2,000 | medium | - |
| G generics | 2,000-3,000 | high: placement/cycles | M0 |
| I interfaces | 3,000-5,000 | high: whole-link, visibility | G |
| T text | 1,000-2,000 | medium | I |
| S slices | 2,000-3,000 | high: needs E | T |
| M maps | 1,000-2,000 | medium | I |
| E codec/ABI + verifier | 3,000-6,000 | **highest**: unsafe contract | S, M |
| C channels | 2,000-4,000 | high: select/spawn | E |
| D defer (optional) | 1,000-2,000 | premise unproven | I |
| F1 base profile | 1,000-2,000 | medium | all stages |
| F2 IR split | 1,000-2,000 | medium | F1 |
| B driver split | 500-1,000 | low | - |
| fixtures/harness | 2,000-4,000 | - | each stage |

Total: roughly **22,000-40,000 new lines** across the chain, plus the moved
code. The typed-AST row can be deferred by running early stages as compiler
modes (the generics plan does); the table prices the standalone destination,
where it cannot be deferred. That is a second compiler's worth of source, which is the honest scale of
"decompose a compiler into compilers". The 12-14k deletion does not pay for it
in size; it pays in comprehension, testability and independent replacement.

### 8.2 Abandon points

- If typed-AST extraction exceeds ~3,000 lines or changes emitted IR on the
  existing test suite, stop and reassess: every stage depends on it, and a
  second checker is worse than no pipeline.
- If the G0 audit (phase plan) shows a large class of unsolvable
  visibility/cycle cases, narrow the first cut instead of building bridges for
  every case.
- If E's generated-code verifier cannot enforce the restricted unsafe rules,
  fall back to E1 descriptors; do not ship a codec whose correctness depends on
  review.
- If the incremental hash barrier does not stop propagation (a leaf change
  reruns the closure), the pipeline is not usable at scale; fix that before
  adding stages.

### 8.3 Milestones

The order below is dictated by the dependencies, not by ambition.

**M0 - enabling machinery.**
Printer + source model + stage driver + typed-AST extraction + `-pipeline`
bypass + the per-package cache. Gate: byte identity for untouched files and
AST identity for printed constructs on `src/` and `_mxc_stage4`; every pipeline
prefix (including `off`) builds and behaves identically on the fixtures; a leaf
change reruns only its package and dependents.

**M1 - G (generics).**
Gate: the stdlib and a set of generic-heavy fixtures compile with no type
parameters; behaviour matches legacy; the stage's generated instances are
visible in exports.

**M2 - I (interfaces).**
Gate: `fmt`, `errors`, `io`, `sort`, `encoding` and the compiler's own packages
compile with no `interface` type; interface dispatch, assertions, type switches
and `error` behave identically; dispatch arms are generated and cached
correctly.

**M3 - T + S + M (text, slices, maps).**
The representation stages, in order. Gate: the lowered source parses, typechecks
and uses only `__slice`/`__text`/`__map` structs; subslice copies, push/pop,
range, map iteration and literals behave identically *within* a package. It is
not yet safe to return a slice/map across a frame boundary - that is M4.

**M4 - E (codec and ABI).**
Depends on T/S/M's lowered layouts. Gate: slices, strings, maps and interface
values returned across function boundaries survive arena reuse; subslice copies
and codec round-trips through the return protocol behave identically; the
generated-code verifier passes. This is the hardest gate in the plan.

**M5 - C (channels and select).**
Depends on E's encoders for frames. Gate: channel frames, select semantics,
spawn handoff (reduced to handles, calling E's encoders) and arena ownership
behave identically.

**M6 - core split and base profile.**
F1/F2/B separated; the base profile rejects removed features; the runtime and
the compiler compile through the pipeline; self-host fixpoint.

**M7 - optional.** D (defer/method values), serialized AST/IR artifacts,
separate stage binaries, legacy slimming if the bootstrap path is ever retired.

---

## 9. Risks and open decisions

1. **Typed-AST extraction (top enabling risk).** This is not a product; it is
   1,500-2,500 lines lifted out of the most intricate code in the compiler
   (`buildBody`/`buildExpr`/`buildStmt` and the checking halves around them).
   Every standalone stage depends on it, it must not change emitted IR, and no
   second checker may be written to avoid it. A stage can postpone it by running
   as a compiler mode (the generics plan does), but the extraction returns when
   stages become separate programs or the core drops the full checker. If it
   overruns, the pipeline stalls at M0; §8.2 is where that is measured.
2. **`pkg/types` serves two audiences and the split is unresolved.** The
   stages need the full model (generics, interfaces, slices, maps, chans); the
   core needs the shrunken one. A profile flag rejects features but does not
   shrink the model: the core binary still carries the node kinds and their
   checks. Options: (a) keep one model, accept the size (recommended first,
   because the 64 KB target is not met anyway); (b) move feature code into
   files selected by a build tag, one package, no duplication (hard: `Implements`
   and `Assignable` interleave feature rules); (c) split packages behind an
   extension interface (most refactoring). Decide after G/I prove the pipeline;
   do not fork the checker.
3. **Relocation after type erasure (highest).** The current return protocol is
   codec encode in the callee arena → `FnArenaPop` → decode in the caller
   (ir_deepcopy.mx:811-928), not the `Reloc*` three-pass noted in older
   documentation (`src/runtime/relocate.mx` functions are emitted by neither
   compiler). Any slice/map/interface lowering must supply equivalent
   type-directed copy/relocation or the program returns pointers into a freed
   arena. Decide E's mechanism (E1 descriptors+structural codec, or E2 generated
   per-type functions) before S's accessor ABI is fixed; E itself runs after
   S/M by the canonical order.
4. **Sovereign arenas.** A self-mutating receiver owns an arena and compaction
   decisions depend on whether data escaped, which today is a type-directed
   question in codec.mx. Generated relocation must reproduce it, or sovereign
   behaviour regresses silently (the class of bug the project's notes warn
   about).
5. **Interface dispatch is whole-link and cache-visible.** Tag dispatchers must
   enumerate implementers, and the enumeration is part of the object key
   (main.mx:2546). Either keep that discipline or move dispatchers to a
   generated per-link package. Do not put the whole-link term in the export key.
6. **`error` is an interface.** After I it is a generated struct; `errors.Is`,
   `errors.As`, `Unwrap`, wrapping and `%w` must remain correct. `As` is
   generic (monomorphized by G) and needs tag comparisons; `Is` needs per-type
   equality. This is a semantic area, not a mechanical rewrite.
7. **Boxing cost.** Mechanism A allocates a copy per interface conversion.
   Today small values are packed inline (ir_iface.mx:44-217). A generated
   representation can keep an inline word rule, but then the codec and
   relocation must agree with it exactly. Decide before I.
8. **String range semantics diverge today.** Stage4 loads bytes
   (ir_iter.mx:127); legacy decodes runes (`stringNext`, runtime/string.mx:235).
   The pipeline must pick one before T, or it will freeze the wrong one.
9. **Mutable literal identity.** Literals are writable `private global`s, one
   object per occurrence (ir_emit.mx:1059). The `__blobtext` intrinsic must
   preserve that, including the "two occurrences are two objects" rule.
10. **Leftover array declarations.** Moxie has no array type, but the runtime
    still declares about twenty fixed arrays (`tophash [8]uint8`, `[256]byte`
    scratch, `(*[3]uintptr)` casts, `[512]pollDesc`, ...). S must rewrite every
    one to named fields, pointer+extent blobs, or `unsafe.Add` at an offset;
    the base profile must reject any survivor. This is runtime work the ledger
    must count, not a formality.
11. **Generic identity across packages.** Instances must be emitted in the
   declaring package with deterministic names, and function-local type arguments
   must be lifted or instantiated locally. The current per-caller clone
   (ssa_builder.mx:11105) is not a source-level model.
12. **Unexported methods and fields.** A generated dispatcher/trampoline cannot
    name another package's unexported method. Generate exported thunks beside
    the type. The same applies to fields used by generated accessors: keep the
    accessors in the defining package.
13. **`spawn` and `defer` are not representation.** Spawn needs a code pointer
    and the arena bundle; defer needs the register checkpoint (inline asm,
    ir_emit.mx:220). Keeping both in the core is the low-risk choice; lowering
    them is optional (M6).
14. **Diagnostics.** Generated code must map back to user constructs, or the
    first production bug will be undebuggable. Build the map with the printer,
    not after.
15. **Bootstrap and wood law.** Legacy must keep the full language to build the
    pipeline and the core. Every semantic decision made in a stage must be
    mirrored or explicitly declared stage-only under the emitted-code rule.
16. **Performance.** Bounds checks become calls, interface conversions allocate,
    map access goes through an extra function layer that LLVM may or may not
    inline. Measure against the current compiler on the same fixtures before
    committing to the final representation; the accessors should be marked
    `//:inline` where the emitter supports it.
17. **The interpreter.** Legacy's `interp.Run` folds `runtime.initAll` at
    compile time and depends on `MakeInterface` type codes and the linked
    module shape (builder/build.go:629). A pipeline that changes generated code
    shapes will need the interp to keep working through the bootstrap, or the
    init path must be handled explicitly.

---

18. **Runtime boundary ABI.** §5.5: if any runtime package is exempt from
    the representation stages while user code is lowered, every slice/string/
    interface/error signature at the boundary is a mixed ABI. Rewrite the
    runtime with the link, or expose a raw boundary plus a thunk; rebuild the
    bake from the rewritten runtime. Audit signatures at every stage.
19. **`unsafe` vs arena tracking.** E's generated code is opaque to the
    compiler by design; its correctness rests on the restricted-unsafe contract
    and verifier in §4.6. If the verifier cannot enforce a rule, fall back to
    E1 descriptors. Do not assume the compiler can relocate through generated
    pointer arithmetic.
20. **Tag numbering churn.** Interface tags are whole-link ordinals. Adding a
    type renumbers the tail and invalidates the packages that embed those
    constants. Acceptable while I is whole-link; if per-package reuse makes it
    expensive, move to a 64-bit hash with a whole-link collision check.
21. **Mangling collisions.** A 16-hex hash is not a proof. The registry must
    extend and fail loudly on a real collision, and fixed test vectors must pin
    the naming, or the cache and the tree are not reproducible.
22. **Incremental propagation.** If the hash barrier cannot stop at unchanged
    package outputs, a stage change reruns the closure and the pipeline is not
    usable at scale. Measure rerun counts on a large fixture before adding
    stages.
23. **Whole-link cache key.** The phase-G cache is conservative: a one-line
    change misses the link. That is correct but expensive; narrowing to a
    specialization fingerprint is a measurement-led optimization, not a
    correctness change.

## 10. Appendix: where to look first

| topic | file |
| --- | --- |
| parse/check/compile orchestration | `_mxc_stage4/ir_typecheck.mx` |
| the one fused front end + SSA builder | `_mxc_stage4/ssa_builder.mx` |
| IR instruction set | `_mxc_stage4/ssa_types.mx` |
| LLVM text emission | `_mxc_stage4/ir_emit.mx` (+ ir_types, ir_binop, ir_call) |
| interfaces today | `_mxc_stage4/ir_iface.mx`, `legacy/compiler/interface.go`, `legacy/transform/interface-lowering.go` |
| generics today | ssa_builder.mx generic region, ir_typecheck.mx:942, ir_descriptors.mx:848, loader/ssa.go:13 |
| descriptors/codec | ir_descriptors.mx, ir_deepcopy.mx, src/runtime/codec.mx, typekind.mx |
| channels/select | ir_chan.mx, ir_call.mx spawn region, src/runtime/chan.mx, pipe_channel.mx, ringbuf.mx |
| slices/maps | ir_call.mx builtins, ir_map.mx, ir_iter.mx, src/runtime/slice.mx, hashmap.mx |
| source rewrites that exist | `_mxc_stage4/ir_rewrite.mx`, `legacy/mxtext/rewrite.go`, `legacy/loader/*rewrite.go` |
| legacy pipeline and job graph | legacy/builder/build.go, legacy/loader/loader.go, legacy/compiler/compiler.go |
| export data | main.mx:4494 (stage4 type facts), legacy/loader/mxh.go (FFI header - different thing) |
| build/caches/link | main.mx:2425-2700 (caches), :8330+ (link); legacy builder/build.go:75-88, :337-378 |
| type DB rationale | docs/type-db.md |
| constraints the pipeline must keep | claude.md (memory model, spawn, interfaces, slices, generics) |





