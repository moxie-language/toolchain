# Finish-Bootstrap Plan

Goal: eliminate all `go/types`, `go/ssa`, `go/ast` (and `golang.org/x/tools/go/...`) from the moxie toolchain, so the compiler runs entirely on the native `typecheck/` + `mxssa/` packages. Then rewrite the orchestration layer in Moxie itself for a self-hosted release.

Two milestones: **M1 = no Go stdlib type/SSA imports anywhere except `typecheck/bridge` and `ssabridge`, both of which then get deleted.** **M2 = the moxie binary is built by Moxie, no Go toolchain required.**

## Inventory (current, verified 2026-04-26)

### compiler/ — 5 of 23 files still importing go/types or go/ssa

| File | types | ssa | Blocker |
|------|-------|-----|---------|
| symbol.go | 8 | 14 | `getFunction` central cache key, see §1 |
| interface.go | 174 | 6 | type-code/methodset machinery, see §2 |
| compiler.go | 129 | 110 | `getLLVMType`/`createDIType`/builder fields, see §3 |
| calls.go | 1 | 2 | one ssa cast in `getRuntimeFunction`, unblocks once §1 lands |
| sizes.go | 36 | 0 | implements `types.Sizes` for the loader, blocked on §6 |

errors.go, defer.go, llvm.go, tccompat.go contain only comment references — already clean.

### jsbackend/ — 3 files, 149 type refs total
- compiler.go (677 LOC, 14)
- instructions.go (1537 LOC, 83 + ssa)
- types.go (248 LOC, 52)

### Loader/orchestration
- loader/loader.go (814 LOC) — calls `checker.Check()`
- loader/ssa.go (28 LOC) — calls `ssa.NewProgram()`
- loader/loader_go122.go, loader/mxtypes.go, loader/mxrewrite.go
- builder/build.go (1353 LOC) — `types.Sizes` for size queries
- diagnostics/diagnostics.go (230 LOC) — wraps `types.Error`

### Text rewrites
- mxtext/rewrite.go (3318 LOC) — 50+ `info.Types[]` queries

### Bridges (delete-targets, last)
- ssabridge/ssabridge.go (726 LOC)
- typecheck/bridge/bridge.go (296 LOC)

### Tooling commands (own go/types use, can be cut after compiler-side migration)
- cmd/moxiejs/main.go (uses go/types + go/ssa)
- cmd/mxssadump/main.go
- cmd/mxcheck/main.go
- cmd/mxcorpus/ir.go
- build/patch-gotypes.go (the patched-go/types build helper — delete when go/types is no longer linked)

### Native packages (kept, the destination)
- typecheck/ — 13 files, 3169 LOC, all stdlib + smesh + iskra pass
- mxssa/ — 3 files, 3174 LOC, mxssadump-tested

---

## M1: Backend uses native types only

Order is rigid: compiler-side migrations cascade upward. Each step ends with a `cd /home/mleku/s/smesh && env MOXIEROOT=/home/mleku/s/moxie /tmp/moxie-test build -o /tmp/smesh-test .` test gate. Don't proceed if smesh stops building.

### §1. symbol.go — getFunction migration (highest leverage)

**Why first.** `getFunction(fn *ssa.Function)` is keyed on `*ssa.Function` via `c.functionInfos`. Every callsite in calls.go, compiler.go, interface.go ultimately funnels through here. Re-keying onto `*mxssa.Function` removes the last load-bearing ssa cast in calls.go and the field accesses that pin compiler.go to ssa.

**Steps.**
1. Re-key `functionInfos`: `map[*ssa.Function]functionInfo` → `map[*mxssa.Function]functionInfo`.
2. Change signatures: `getFunction(*mxssa.Function)`, `getFunctionInfo(*mxssa.Function)`.
3. Update callers — most have `*ssa.Function` and convert via `c.mxTranslator.LookupFunction(f)` or `EnsureFunction(f)`.
4. Port `maybeCreateSyntheticFunction`.
5. `getGlobal`/`getGlobalInfo` get the same treatment using `LookupGlobal`.
6. `getAllMethods` uses `prog.MethodValue` — needs an mxssa equivalent. Add `mxssa.Program.MethodValue` returning `*mxssa.Function`, populated via translator at package translation time. Stays SSA in a thin `ReverseFunction`-fed wrapper if necessary.
7. Drop the runtime-package ssa cast in `calls.go.getRuntimeFunction` once mxssa has the runtime registered.

**Effort.** 1-2 focused days. Touches ~40 callsites.

**Test.** After each commit: smesh build at 7MB, runs `version`. The MOXIE_SSA_TRACE env-var instrumentation should still report zero hits to the deleted SSA paths.

### §2. interface.go — type-code + methodset

**Why next.** Once getFunction is on mxssa, interface dispatch is the next domino. Type-codes and methodsets are computed from `types.*` — migration mirrors them on `typecheck.*`.

**Steps.**
1. `getTypeCode`, `getTypeKind`, `getTypeCodeName` — straight type-kind switches; mostly mechanical.
   - `*types.Named` → `*typecheck.Named`
   - `*types.Pointer` → `*typecheck.Pointer`
   - `*types.Slice`/`*types.Array`/`*types.Map`/`*types.Chan` likewise
   - watch alias resolution: types.Alias path collapses earlier than typecheck.Alias (covered by `tcbridge.translateType`).
2. `getTypeMethodSet`, `getMethodSignatureName`, `getMethodSignature` — use `typecheck.MethodSet(T)` (already added). The MethodSet shape diverges in ordering — sort by name for stable layout.
3. `getInterfaceInvokeWrapper` — wraps non-pointer-receiver methods. Use `typecheck.LookupMethod` and feed mxssa `Function`.
4. `findGoMethod`, `methodSignature`, `signature`, `typestring` — these compute string forms used as cross-module symbol keys. Critical: keep the same string form as the SSA path during transition or LinkOnceODR struct layouts diverge (see SELF-HOST-PLAN.md §"LinkOnceODRLinkage struct layout"). Best path: derive from `mxTranslator.ReverseFunction(f).String()` so the names match what go/ssa would produce, then in a follow-up pass switch the canonical form to native.

**Effort.** 3-4 days. Big file but heavily redundant — the type-kind switch is pasted multiple times.

**Test.** Smesh builds at 7MB AND runs nostr signing roundtrip — interface dispatch is in the hot path for `signer.Sign`.

### §3. compiler.go — getLLVMType + createDIType + builder fields

**Why now.** With symbol.go and interface.go off ssa.Function, the only remaining ssa surface in compiler.go is `program *ssa.Program` and the builder's `fn *ssa.Function`. Same for types.Type in `getLLVMType`/`createDIType`.

**Steps.**
1. `getLLVMTypeTC` already exists in tccompat.go. Promote it to canonical: rename, delete the ssa version, repoint callers. Ensure the cache (`tcLLVMTypes`) is consistent across packages — same identity rule as the bridge.
2. `createDITypeTC(typecheck.Type) llvm.Metadata` — mirror the SSA dispatch. Tedious but mechanical (one branch per type kind). Reuse `getDITypeTC` cache once added.
3. `runtimePkg`: change from `*ssa.Package` to `*mxssa.Package`. Translator already populates this.
4. Builder field `b.fn`: change from `*ssa.Function` to `*mxssa.Function`. All readers already have mxssa equivalents after §1.
5. `createConst`: `getValueTC` + `createConstTC` already exist in tccompat.go. Repoint.
6. `createPackage`/`createFunctionStart`/`createEmbedGlobal`: replace `*ssa.Package`/`*ssa.Function` parameters.
7. Remove `program *ssa.Program` from builder context. The translator becomes the only ssa-touching field, internal to ssabridge.

**Effort.** 4-5 days. The `createDIType` chain alone is ~200 lines of dispatch.

**Test.** Smesh build + `dlv-dap` debug-info smoke test. DWARF correctness can break silently — verify struct field offsets in `objdump --dwarf=info /tmp/smesh-test | head -200`.

### §4. calls.go cleanup

After §1, only the runtime/internal-task lookup remains. Replace `member.(*ssa.Function)` with mxssa lookup once runtime + internal/task packages are pre-registered in `mxssa.Program`.

**Effort.** 0.5 day.

### §5. jsbackend/ migration

Same shape as compiler/, smaller surface. instructions.go is the bulk.

**Steps.**
1. Mirror tccompat.go for jsbackend if needed (parallel TC helpers).
2. Migrate types.go's type-emission first (parallel of getLLVMType but for JS).
3. instructions.go: per-instruction migration, ssa.* → mxssa.*. Most mechanical.
4. compiler.go: package-level orchestration.

**Effort.** 3-4 days.

**Test.** `cd /home/mleku/s/smesh && env GOWORK=off /tmp/moxiejs-test -runtime ../moxie/jsruntime -o /tmp/web-test web/app/` and exercise the resulting bundle with the relay.

### §6. mxtext/rewrite.go migration

3318 LOC, 50+ `info.Types[expr]`, 12+ `info.TypeOf()`. The text rewrites operate on the AST after the first type-check pass.

**Steps.**
1. The rewrites consume `types.Info`. Replace with `typecheck.Info` (same shape, different type identity).
2. `info.Types[expr]` → `info.Types[expr]` (typecheck.Info has parallel field; verify population).
3. `info.TypeOf(e)` → `info.TypeOf(e)` parallel.
4. Type-kind switches: same mechanical migration as compiler.

**Effort.** 3-4 days.

**Test.** The rewrite output is text. Diff old vs new on smesh's full source tree — bytes-identical or migration is wrong.

### §7. loader/ migration — switch primary path off go/types

This is the central pivot. Until now, the loader runs `checker.Check()` (go/types), then `ssa.NewProgram()`, then ssabridge translates. After this step, the loader runs `typecheck.Checker.Check()` directly, then `mxssa.NewProgram` from typecheck.Info.

**Steps.**
1. loader.go: replace `typeChecker types.Config` with `typecheck.Config` (or equivalent). Replace `checker.Check(...)` with `typecheck.NewChecker(...).Check()`.
2. Remove the `info` field's go/types form — typecheck.Info becomes primary.
3. loader/ssa.go: `ssa.NewProgram` → `mxssa.NewProgram`. The mxssa builder consumes typecheck.Info directly (no AST adapter).
4. loader/mxtypes.go (the `int≡int32` patch): delete. Native typecheck owns this rule.
5. loader/mxrewrite.go: if it depends on go/types, repoint to typecheck.
6. loader_go122.go: usually a thin wrapper; migrate or fold into loader.go.

**Effort.** 4-5 days. This is where unforeseen edge cases live.

**Test.** The full stdlib type-check parity test (37/37 packages) must still pass after switch. Then smesh build + runtime smoke.

### §8. builder/build.go — types.Sizes

Replace `types.Sizes` use with `typecheck.Sizes` equivalent. compiler/sizes.go follows once the loader's go/types invocation is gone.

**Effort.** 1 day.

### §9. diagnostics/diagnostics.go

Wraps `types.Error` for IDE/CLI surface. Replace with `typecheck.Error` (already carries position info — kills the reflect hack mentioned in SELF-HOST-PLAN.md).

**Effort.** 0.5 day.

### §10. Bridge deletion + go.mod cleanup

**Steps.**
1. Delete `ssabridge/ssabridge.go` (726 LOC).
2. Delete `typecheck/bridge/bridge.go` (296 LOC).
3. Delete `loader/mxtypes.go` (the unsafe patch — already gone after §7).
4. Delete `build/patch-gotypes.go` and the patched-go/types build step.
5. Run `go mod tidy`. `go/types`, `go/ssa`, `golang.org/x/tools/go/...` should disappear from go.mod.
6. `cmd/mxcheck/main.go` (compares native vs go/types) becomes pointless — delete or repurpose as native-only.
7. `cmd/moxiejs/main.go`, `cmd/mxssadump/main.go`, `cmd/mxcorpus/ir.go`: migrate their own go/types usage, identical pattern as loader.

**Effort.** 2 days.

**Test.** `grep -r "go/types\|go/ssa\|golang.org/x/tools/go" --include="*.go" .` returns empty (modulo comments). Smesh + iskra still build and run.

### M1 total estimate
- §1: 1-2 days
- §2: 3-4 days
- §3: 4-5 days
- §4: 0.5 day
- §5: 3-4 days
- §6: 3-4 days
- §7: 4-5 days
- §8: 1 day
- §9: 0.5 day
- §10: 2 days

**~22-30 focused days. M1 = no go/types or go/ssa anywhere in moxie.**

---

## M2: Self-hosted (compiler is Moxie)

Pre-req: M1 done. The Go code at this point is moxie's own implementation; LLVM-C is the only foreign code path.

The work is mechanical translation Go→Moxie, not redesign. Moxie has no reflection/goroutines, so any Go-side use of those (some in cmd/* tooling) needs a different shape.

### §11. cmd/* tooling in Moxie
- main.go (843 LOC) → main.mx
- cmd/moxiejs/main.go (~400 LOC after M1) → moxiejs.mx
- cmd/mxssadump, cmd/mxcheck, cmd/mxcorpus → .mx

These are pure orchestration; tractable.

**Effort.** 5-7 days.

### §12. builder/build.go in Moxie
Job scheduling, LTO linking, linker invocation. Calls LLVM via cgo today; Moxie's `import "C"` covers this once the LLVM-C wrapper is exposed.

**Effort.** 4-5 days.

### §13. loader/ in Moxie
File resolution, module parsing, vendor lookup, then native typecheck + mxssa drive. The typecheck/mxssa packages themselves get translated next.

**Effort.** 6-8 days.

### §14. typecheck/ + mxssa/ in Moxie
The big rewrite. ~6300 LOC across two packages. No reflection, no go/ast (already migrated to syntax.File), no go/token (use moxie's own positions). The packages are designed for migration — internal Go conventions only, no external reflection.

**Effort.** 12-18 days.

### §15. compiler/ + jsbackend/ in Moxie
After M1, the compiler is ~10k LOC of Go that calls LLVM-C and consumes typecheck+mxssa. Translation is mechanical but lengthy.

**Effort.** 14-20 days.

### §16. mxtext/ + diagnostics/ in Moxie
**Effort.** 5-7 days.

### M2 total estimate
**~46-65 focused days for full Moxie self-host.**

### §17. Binary release (B5)
Tag, build amd64 + arm64 native, ship. Go toolchain no longer required to build moxie.

**Effort.** 1 day after M2.

---

## Risks and known gotchas

1. **LinkOnceODR struct layout drift** (SELF-HOST-PLAN.md §"LinkOnceODRLinkage"). Methodset descriptor structs must have identical layout across compilation units. `hasMethodSet` MUST agree on every translation unit. Migration must keep `ReverseType` working until all callers are on typecheck-native methodset.
2. **Named type LLVM identity** (SELF-HOST-PLAN.md §"Named type LLVM identity"). `Named.String()` form must be qualified (`encoding/base64.Encoding`), not short (`Encoding`). tcbridge already handles this; ensure the native path keeps the convention.
3. **Method receiver in defer frames** (SELF-HOST-PLAN.md §"Method receiver missing"). `Function.Params` is sometimes empty for cross-package shells. Use `getParams(goFn.Signature)` — port to mxssa.Function.Signature.
4. **mxssa.RelString collision**: `(*Rand).Int` and pkg-level `Int` both resolve to `math/rand/v2.Int`. Until full deletion, prefer `mxTranslator.ReverseFunction` for canonical link names.
5. **isUserPackage vendored exemption**: mirror `isMakeExempt` logic — `golang.org/x/...` is stdlib-equivalent for restriction purposes.
6. **LinkOnceODR + cross-package ReverseType returns nil**: when migrating §1 and §2, ensure the methodset symbol is declared `extern` if ReverseType fails for the local translator. See SELF-HOST-PLAN.md.
7. **Smesh + iskra parity**: every step ends with both projects building AND running version + signing roundtrip. iskra exercises generics; smesh exercises full nostr+lightning.
8. **JS target parity**: `string` vs `[]byte` representation pragma is enforced in the JS backend. Migration must preserve the source spelling — typecheck.Info needs to surface this, not just resolve to identical type.

---

## Per-commit discipline

Carry forward from prior session:
- One file or one logical migration per commit.
- Test gate: smesh build at 7MB + runs `version` + signs an event.
- Re-read modified files after each Edit.
- If a commit produces 1000+ line diff, the scope was wrong — split or revert.
- Never bypass `--no-verify`. Investigate hook failures.
- MOXIE_SSA_TRACE=1 env var instrumentation stays in place until all SSA paths are deleted; final commit removes the instrumentation.

---

## Order summary

```
§1  symbol.go.getFunction          [unblocks §2 §3 §4]
│
├─→ §2  interface.go               [parallel with §3 if careful]
├─→ §3  compiler.go core           [parallel with §2]
└─→ §4  calls.go cleanup           [trivial after §1]
       │
       ↓
§5  jsbackend                      [independent, can parallelize with §6]
§6  mxtext/rewrite.go              [independent]
       │
       ↓
§7  loader switch                  [requires §1-§6]
       │
       ↓
§8  builder.go                     [requires §7]
§9  diagnostics                    [trivial]
       │
       ↓
§10 BRIDGE DELETION + go.mod cleanup    ← M1 complete
       │
       ↓
§11-§16 Moxie translations         ← M2 complete
       │
       ↓
§17 Binary release                 ← bootstrap shipped
```

The Go dies in §10. The Go toolchain dies in §17.
