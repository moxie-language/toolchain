# B3 Migration: Next Steps

After 68+ commits eliminating the SSA codegen pipeline and 18 of 23 compiler files
fully purged of `go/ssa`+`go/types`, the remaining 5 files all hit deep
architectural blockers:

## Files still importing go/ssa or go/types

### `compiler/calls.go` (1 ssa cast)
The single `member.(*ssa.Function)` cast in `getPackageFunction` is the entry
point for runtime/internal-task function lookup. Eliminating it requires:

1. Pre-register `runtime` and `internal/task` packages in `mxssa.Program` via
   ssabridge. Currently only the main package is in `mxProg.imported`.
2. Migrate `getFunction` to take `*mxssa.Function` (see symbol.go below).

### `compiler/symbol.go` (8 types, 10 ssa)
**The central blocker.** `getFunction(fn *ssa.Function)` is keyed on
`*ssa.Function` via `c.functionInfos` cache and used everywhere. Migration:

1. Re-key `functionInfos` from `map[*ssa.Function]functionInfo` to
   `map[*mxssa.Function]functionInfo`.
2. Change `getFunction` signature to take `*mxssa.Function`.
3. Change `getFunctionInfo` to take `*mxssa.Function`.
4. Update all callers — most have `*ssa.Function` already; convert via
   `mxTranslator.LookupFunction(f)` or `EnsureFunction(f)`.
5. `maybeCreateSyntheticFunction` ports.
6. `getGlobal`/`getGlobalInfo` get the same treatment using `LookupGlobal`.
7. `getAllMethods` uses `prog.MethodValue` — needs mxssa equivalent or
   stays SSA in a thin wrapper.

Estimated: 1-2 days of focused work. Touches every getFunction caller.

### `compiler/interface.go` (174 types, 4 ssa)
Heavy go/types usage in:
- `getTypeCode`, `getTypeKind`, `getTypeCodeName` — type codes for runtime
  reflection, deep type traversal
- `getTypeMethodSet`, `getMethodSignatureName`, `getMethodSignature` —
  method-set machinery for interface dispatch
- `getInterfaceInvokeWrapper` — wraps non-pointer-receiver methods
- `findGoMethod`, `methodSignature`, `signature`, `typestring` — string-form
  of types and signatures, used for cross-module symbol matching

Migrating is a multi-day port. Each function dispatches on every Go type kind
(*types.Named, *types.Pointer, *types.Slice, etc.) — TC has parallel
type kinds but the equivalence isn't 1:1 (e.g., typecheck.Named's String() is
qualified; types.Named's is too but with subtle alias handling).

### `compiler/compiler.go` (129 types, 40 ssa)
Core infrastructure split into:
- `getLLVMType`/`makeLLVMType`/`createDIType`/`getDIType` chain — 200+ lines
  of go/types dispatch for LLVM type construction and DWARF debug info
- `runtimePkg`/`getRuntimeType` — runtime named-type lookup
- `program *ssa.Program` — central ssa.Program reference
- `fn *ssa.Function` builder field — would migrate when getFunction does
- `createConst`, `createPackage`, `createFunctionStart`, `createEmbedGlobal`

These migrations cascade: migrating getLLVMType to TC requires migrating
createDIType (otherwise types diverge between value and debug paths).
createDIType is itself deep go/types dispatch.

### `compiler/sizes.go` (36 types)
Implements `types.Sizes` interface for the **loader's** go/types invocation.
This is fundamentally B5 work — the loader still uses go/types to
type-check Go source before mxssa translation. Cannot be migrated until
the loader switches to native typecheck.

## Recommended sequence

1. **symbol.go.getFunction migration** — unlocks calls.go and reduces
   compiler.go significantly. Highest leverage.
2. **interface.go type code migration** — needed for interface dispatch
   to work on TC types. Can be done in parts: getTypeCode + getTypeCodeName
   first (mostly mechanical), then method-set machinery.
3. **compiler.go createDIType migration** — needed for full debug info on
   TC types. Add `createDITypeTC(typecheck.Type) llvm.Metadata` mirroring
   the SSA dispatch.
4. **sizes.go** — blocked on loader migration. Skip until B5.

## What was achieved

68+ commits, ~3500 lines of SSA codegen deleted. The breakthrough was
removing complex64/128 cases from runtime sources, which made the SSA
`createFunction` pipeline unreachable and triggered cascading deletions.

Files fully purged (18/23): alias, asserts, atomic, channel, defer, errors,
func, gc, goroutine, intrinsics, llvm, map, mxbuiltins, restrict, spawn,
syscall, tccompat, volatile.

Smesh builds at ~7MB and runs `version` after every commit.
