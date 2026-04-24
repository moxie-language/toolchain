# Plan: Self-Host Moxie Compiler in Moxie + Binary Release

## Context

Moxie compiler is 42k LOC Go. Now that moxie, smesh, and iskra share a workspace, nothing prevents rewriting Go parts of the compiler in Moxie. This also enables binary releases (no Go toolchain needed to build moxie).

Two additional requirements:
1. Eradicate go module proxy dependency — imports must work offline
2. Binary release tagging regime (amd64 only initially)

## The Hard Floor

LLVM bindings (`tinygo.org/x/go-llvm`) are cgo wrapping C++. This stays as-is — LLVM is the native codegen backend and rewriting it would mean writing a new codegen backend from scratch (years of work, not months). The strategy is: keep LLVM as an external C library, rewrite everything *above* the FFI boundary in Moxie.

## Completed Phases

### ✅ Phase 0: Vendoring Command
**Status:** Complete. `moxie vendor` subcommand exists, resolver prefers vendor/ before module cache, `moxie.sum` and `modules.txt` generation work. Smesh has no external deps so vendor is a no-op there.

### ✅ Phase 1: Wire Bootstrap Parser
**Status:** Complete (commit 8d6969e). Bootstrap parser (`syntax/` package) replaces `go/parser`. Adapter converts `syntax.File` → `go/ast.File`. Pragma directives preserved. Smesh builds and runs identically.

## The Type System Wall (Phases 2+3+4)

### Why the original plan was wrong

The original plan treated Phases 2, 3, and 4 as separable (2+3 atomic, then 4). The audit reveals they are a **single indivisible unit**. Here's why:

**go/ast, go/types, and go/ssa form a dependency triad:**

```
go/ast.File ──→ go/types.Checker.Check() ──→ types.Info + types.Package
     │                                              │
     └──────────────────────────────────────────────┼──→ ssa.CreatePackage()
                                                    │           │
                                                    │           ▼
                                                    │     ssa.Function/ssa.Package
                                                    │           │
                                                    ▼           ▼
                                              compiler/*.go (16 files use types.*)
                                              jsbackend/*.go (3 files use types.*)
```

You cannot replace go/ast without replacing go/types (the checker consumes ast nodes). You cannot replace go/types without replacing the SSA builder (it consumes types.Info). You cannot replace the SSA builder without rewriting the compiler backend (it consumes ssa.Function with types.* throughout).

**The actual dependency surface:**
- `go/types` is used in **26 files** across loader, mxtext, compiler, jsbackend, builder, diagnostics
- `go/ast` is used in **16 files** (same consumers minus some compiler files plus mxtext)
- `types.*` type assertions (`*types.Basic`, `*types.Pointer`, `*types.Struct`, etc.) appear **100+ times** in compiler codegen
- `ssa.*` IR types are the interface between type checking and codegen — both backends depend on them
- `mxtext/rewrite.go` (3318 LOC) does 50+ `info.Types[expr]` queries and 12+ `info.TypeOf()` calls

**Size reassessment:**
- go/types replacement: 8,000-12,000 LOC (original estimate holds — edge cases in method sets, interface satisfaction, assignability, constant folding, untyped constants)
- go/ssa replacement: 5,000-7,000 LOC
- mxtext/rewrite.go rewrite against native AST: 3,000 LOC
- compiler/*.go rewrite against native types (16 files): 5,000-8,000 LOC of changes
- jsbackend/*.go rewrite against native types (3 files): 1,000-2,000 LOC of changes
- **Total: 22,000-32,000 LOC touched**, not 11,000-15,000

This is not a phase — it's the entire compiler rewrite minus the LLVM FFI layer.

### Revised approach: The Seam Strategy

Instead of replacing go/ast+go/types+go/ssa in one shot, exploit the existing adapter seam. The adapter already translates `syntax.File` → `go/ast.File`. The strategy becomes:

**Keep the adapter as a permanent bridge.** go/types and go/ssa continue to consume go/ast. The native parser handles parsing. The adapter handles translation. The compiler and jsbackend remain Go code consuming ssa.* and types.* as they do today.

This means Phases 2+3+4 as originally conceived are **deferred indefinitely**. They become relevant only if/when a concrete need arises (e.g., the adapter becomes a performance bottleneck, or go/types diverges from Moxie semantics in a way that can't be patched).

**What this buys:** The adapter seam is the bridgehead. The native parser (Phase 1) is in place. go/types and go/ssa continue working while B1 and B2 are built behind the seam. When B3 lands, the adapter is deleted.

## Revised Phase Order

### Phase 2 (NEW): Eliminate Re-Typecheck Pass
**Goal:** The current loader does typecheck → AST rewrite → re-typecheck. Since the native parser already handles Moxie syntax (chan literals, slice literals are rewritten before parsing), some AST rewrites may now be redundant. Audit which rewrites still need the two-pass approach and which can be folded into the adapter or parser.

**Potential wins:**
- If pipe concat (`|` → `+`) is handled at text level before parsing, the post-typecheck `FindPipeConcat` pass might be simplifiable
- If chan/slice literal rewrites happen pre-parse (they do), the re-typecheck might be eliminable for some packages
- Fewer rewrites = fewer type-check passes = faster compilation

**Files:** `loader/loader.go` (the Check() method), `mxtext/rewrite.go`, `loader/mxrewrite.go`
**Size:** Investigation + ~200-500 LOC changes
**Test:** Build smesh, identical binary, measure compile time delta
**Risk:** Low — this is optimization, not rewrite. Falls back to current behavior if any rewrite still needs two passes.

### Phase 3 (NEW): Rewrite CLI + Builder in Moxie
**Goal:** The orchestration layer becomes Moxie. This is the old Phase 5, renumbered. It's tractable (~3k LOC, pure orchestration, no type system dependency).

- `main.go` (843 LOC) → `main.mx`
- `builder/build.go` (1354 LOC) → Moxie, keeping LLVM tool invocation via exec
- `loader/` orchestration parts → Moxie
- `cmd/moxiejs/` (400 LOC) → Moxie

**Constraint:** loader/loader.go calls go/types.Checker which is Go stdlib. The Moxie rewrite of loader must still call into Go code for type checking. This means the loader is partially Moxie (file resolution, module parsing, text rewrites) and partially Go (type checking, SSA construction). The boundary is: everything before `checker.Check()` and after SSA construction can be Moxie; the middle stays Go.

**Files:** `main.go`, `builder/build.go`, parts of `loader/`, `cmd/moxiejs/`
**Size:** ~3000 LOC rewrite
**Test:** `moxie build .` on smesh produces working binary

### Phase 4 (NEW): LLVM Backend FFI
**Goal:** Replace `compiler/*.go` (11k LOC) with Moxie calling LLVM-C API via `import "C"`.

This is the old Phase 6. It's large but mechanically translatable — the LLVM-C API is stable, and the existing Go code is already thin wrappers around C calls via the go-llvm bindings.

**Constraint:** The compiler files use `types.*` extensively (100+ type assertions). These stay as-is since go/types is retained. The Moxie code will import the Go types package (which is fine — Moxie can import Go packages).

Wait — Moxie CANNOT import Go packages. Moxie compiles to native via LLVM, not via the Go compiler. So the compiler/*.go files that use go/types MUST remain Go until go/types is replaced.

**Revised constraint:** Phase 4 can only rewrite compiler files that DON'T depend on go/types or go/ssa. Looking at the audit:
- ALL 16 compiler/*.go files import go/types
- ALL compiler/*.go files consume ssa.* types

**Phase 4 is blocked by the Type System Wall.** The LLVM backend cannot be rewritten in Moxie until go/types and go/ssa are replaced. This was the original plan's Phase 6 for good reason.

### The Actual Dependency Order

```
                    ┌─────────────────────────────────────┐
                    │  Type System Wall                    │
                    │  go/types + go/ssa + go/ast          │
                    │  (26 files, 100+ type assertions,    │
                    │   22-32k LOC to replace)             │
                    └──────────┬──────────────────────────┘
                               │ blocks
              ┌────────────────┼────────────────────┐
              ▼                ▼                     ▼
      compiler/*.go    jsbackend/*.go    mxtext/rewrite.go
      (11k LOC, LLVM)  (2.7k LOC, JS)   (3.3k LOC, rewrites)
```

Everything below the wall is Go that uses go/types. Everything above the wall (parser, resolver, vendoring, CLI dispatch, build orchestration) can be Moxie.

### What Can Actually Be Done Without the Type System Wall

1. **CLI dispatch** (`main.go` flag parsing, subcommand routing) — no type deps
2. **Module resolution** (`loader/mxlist.go`) — already Go, could be Moxie
3. **Text-level rewrites** (chan literals, slice literals) — operate on bytes, no type deps
4. **Vendor command** — already done
5. **Build orchestration** (`builder/build.go` job scheduling, LTO linking, linker invocation) — uses LLVM types but not go/types... actually it does: `builder/build.go` imports go/types for `types.Sizes`. **Partially blocked.**

After careful analysis, the only unblocked work above the wall is CLI dispatch and module resolution — maybe 1500 LOC total. This gets absorbed into B4.

## The Plan

Replace go/types, go/ssa, go/ast, then rewrite everything above. The phases:

**B1. Moxie Type Checker** (~8-12k LOC)
- Implement type checker consuming syntax.File directly (no go/ast adapter)
- Produce typed info in Moxie structs (`typecheck/` package)
- Subset of go/types: only what Moxie's type system needs (no complex64, no goroutines, no reflect)
- Must handle: basic types, structs, interfaces, functions, methods, pointers, slices, maps, channels, arrays, type assertions, type switches, constants (including untyped), generics
- Pre-work: remove all actual Go `reflect` package usage (3 files) — clears the baseline
- Test: type-check smesh, iskra, all stdlib — compare error output with go/types

**B2. Moxie SSA Builder** (~5-7k LOC)
- Consumes typed syntax.File + Moxie type info
- Defines new IR; backends updated in B3
- Test: compile smesh native + JS, compare output

**B3. Rewrite Backends Against Native Types** (~8-10k LOC)
- compiler/*.go: replace types.* (600+ assertions) with Moxie type system, replace ssa.* (400+ assertions) with Moxie SSA
- jsbackend/*.go: same
- mxtext/rewrite.go: rewrite against syntax.File + Moxie types
- loader/mxtypes.go unsafe patch: deleted (checker owns int≡int32 natively)
- diagnostics/diagnostics.go reflect hack: deleted (native TypeError carries EndPos)
- Test: build smesh, identical binary

**B4. Rewrite Everything Else** (~3k LOC)
- main.go, builder, loader, cmd/moxiejs
- Test: moxie builds moxie (self-hosting proof)

**B5. Binary Release** (1 day)
- Tag, build, ship — Go toolchain no longer required

## Completed Work Summary

| Phase | Status | Commit |
|-------|--------|--------|
| Phase 0 (vendor) | ✅ Complete | Prior session |
| Phase 1 (parser) | ✅ Complete | 8d6969e |
| Phase 2 (eliminate re-typecheck) | ✅ Complete | f58c068 |
| B1 (type checker) | ✅ Complete | f58c068 — `typecheck/` package, 37/37 stdlib + smesh + iskra PASS |
| B2 (SSA builder) | ✅ Complete | f58c068 — `mxssa/` package, mxssadump tested on bytes/strconv/fmt/sort/strings/net/http |
| B3 (backend migration) | 🔄 In Progress | see below |

## B3: Backend Migration — Detailed Plan

### Strategy: Dual-Representation Bridge

The compiler currently consumes `go/types` + `go/ssa`. The native replacements (`typecheck` + `mxssa`)
mirror the same structure. The bridge strategy:

1. `ssabridge` package translates `ssa.Program` → `mxssa.Program` at `CompilePackage` entry
   - Uses existing `typecheck/bridge` for `go/types.Type` → `typecheck.Type` translation
   - Walks functions, blocks, instructions; creates mxssa equivalents
2. `compilerContext` holds **both** `*ssa.Program` and `*mxssa.Program` temporarily
3. Each compiler file migrates independently (atomic commit per file):
   - Change imports: `go/types` → `typecheck`, `go/ssa` → `mxssa`
   - Change type assertions: `*ssa.Call` → `*mxssa.Call`, `*types.Basic` → `*typecheck.Basic`
   - Adjust any API differences
4. When all files use `mxssa`/`typecheck`, drop `*ssa.Program` from compilerContext
5. Delete `ssabridge` and `typecheck/bridge`

### API Surface (measured)

- 166 `go/types` type assertions across 35 files
- 81 `go/ssa` type assertions across 35 files
- 1284 `types.*` method/function calls
- 477 `ssa.*` method/field accesses

### Migration Order (by dependency — leaf files first)

**Tier 0: Bridge infrastructure**
- [x] `ssabridge/` — ssa.Program → mxssa.Program translator
- [x] `CompilePackage` entry point — produce mxssa.Program, store in compilerContext
- [x] `compiler/tccompat.go` — parallel type/value/size layer:
  - `getLLVMTypeTC(typecheck.Type) llvm.Type` — type→LLVM with own cache
  - `getLLVMFunctionTypeTC`, `getFuncTypeTC` — function type variants
  - `AlignofTC`, `SizeofTC`, `OffsetsofTC` — size computation on stdSizes
  - `getValueTC(mxssa.Value, token.Pos) llvm.Value` — value resolution
  - `createConstTC(*mxssa.Const, token.Pos) llvm.Value` — constant creation
  - `isPointerTC`, `getPosTC` — utility helpers
  - `validateBridgeTypes` — cross-check (MOXIE_VALIDATE_BRIDGE=1)
- [x] `builder` gains `mxFn *mxssa.Function` and `mxLocals map[mxssa.Value]llvm.Value`
- [x] `mxssa.Const.IsNil()` + `nillable()` added
- [x] `ssabridge.Translator.LookupFunction()` / `Importer()` accessors

**Tier 1: Per-method migration (files share builder infra, migrate method-by-method)**
- [ ] compiler/sizes.go
- [ ] compiler/errors.go
- [ ] compiler/volatile.go
- [ ] compiler/atomic.go
- [ ] compiler/inlineasm.go
- [ ] compiler/interrupt.go
- [ ] compiler/syscall.go

**Tier 2: Mid-complexity files**
- [ ] compiler/symbol.go
- [ ] compiler/gc.go
- [ ] compiler/llvm.go
- [ ] compiler/channel.go
- [ ] compiler/map.go
- [ ] compiler/defer.go
- [ ] compiler/asserts.go
- [ ] compiler/spawn.go
- [ ] compiler/mxbuiltins.go
- [ ] compiler/restrict.go

**Tier 3: Core files (highest assertion count)**
- [ ] compiler/calls.go
- [ ] compiler/func.go
- [ ] compiler/goroutine.go
- [ ] compiler/alias.go
- [ ] compiler/interface.go — 14 type assertions, 249 types method calls
- [ ] compiler/compiler.go — 42 type assertions, 289 types method calls, 117 ssa accesses

**Tier 4: JS backend**
- [ ] jsbackend/*.go (3 files, 27+33 assertions)

**Tier 5: Other consumers**
- [ ] mxtext/rewrite.go (43 type assertions)
- [ ] loader/loader.go
- [ ] builder/build.go

**Tier 6: Cleanup**
- [ ] Drop ssa.Program from compilerContext
- [ ] Delete ssabridge/
- [ ] Delete typecheck/bridge/
- [ ] Remove go/ssa from go.mod

## What Stays Go/C Permanently

- LLVM library (C/C++) — codegen backend, never rewriting
- System linker (lld/ld) — exec'd, not linked
- jsruntime (JavaScript) — browser runtime
