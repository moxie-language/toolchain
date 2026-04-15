# Language-Surgery Audit Checklist

Run this after any commit that changes compiler passes, SSA flags, lowering
transforms, or runtime conventions. Not exhaustive — a ready-made "suspect
this first" list for weird behavior.

The meta-pattern: TinyGo set SSA flags intending to replace a subsystem with
its own emission. Every such flag is a fork in the road where the surgery
might have taken only one path. Moxie added more forks.

## The ten-minute sweep

### 1. Init chain emission
- [ ] `ssa.BareInits` set? Then **every** backend must emit its own
      cross-package init chain. Verify each backend's generated entry point
      iterates packages in dep order and calls `pkg.init()` for each.
- [ ] Regression test: a package-level `var x = f()` where `f` is in another
      package. Confirm `x` is set before main runs.
- [ ] Check: `jsbackend/compiler.go:writeEntryPoint`, `builder/build.go`
      `runtime.initAll` construction (~line 524).

### 2. Anonymous functions / closures
- [ ] SSA flags `BuildSerially` or `NaiveForm` set? If yes, SSA skips
      automatic closure lifting — backends must handle `FreeVars` manually.
- [ ] Backend parameter emission order must match the binding order of
      `MakeClosure.Bindings`. JS binds via `.bind()` — silent misalignment
      if order drifts.
- [ ] Test: a closure capturing two distinct types; call through a
      `func()` value after the constructor returns.
- [ ] Check: `compiler/func.go:parseMakeClosure`, `jsbackend/compiler.go:functionParams`.

### 3. Interface method tables
- [ ] Empty interface (`any`, `interface{}`) method invoke path — does it
      reach `getMethodsString` with zero methods? That yields empty
      signature strings and NPE in `transform/interface-lowering.go`.
- [ ] Type assertions on empty interface handled separately from
      non-empty case?
- [ ] Test: `var x any = concreteVal; _ = x.(Method)` should not crash
      the compiler.
- [ ] Check: `compiler/interface.go`, `transform/interface-lowering.go`.

### 4. Reflect elimination
- [ ] Any stdlib package depend on `reflect`? If so, it must be stubbed
      in `src/` or removed.
- [ ] `errors.As` gone? `errors.Is` uses only type assertions?
- [ ] No silent "compiles but panics at runtime" path — missing reflect
      must error at link time.
- [ ] Check: `src/errors/wrap.mx`, `mxtext/rewrite.go` exemption list.

### 5. Intra-package var order
- [ ] `types.Info.InitOrder` is still authoritative (it is — checker-level,
      not backend-level). `BareInits` does **not** touch this.
- [ ] Test: `var b = a + 1; var a = 5` — both backends must init `a` first.
- [ ] Check: `vendor/.../ssa/builder.go:3238-3273` (outside BareInits guard).

### 6. Spawn static analysis
- [ ] Runs on pre-lowered SSA — confirm by checking it happens inside
      `CompilePackage` before `OptimizePackage`.
- [ ] Move semantics: does it track cross-block uses of spawned args?
      Currently **does not** (`restrict.go:checkSpawnMoveRestriction` only
      walks same-block forward). If the surgery extends to flow analysis,
      verify dominator-tree traversal.
- [ ] Codec enforcement: if the `moxie` package isn't imported, spawn
      silently skips Codec checks. Should be a hard error.
- [ ] Test: `x := foo(); spawn(worker, x); if cond { use(x) }` — the
      if-branch use is currently not flagged.
- [ ] Check: `compiler/spawn.go`, `compiler/restrict.go`.

### 7. Goroutine / scheduler
- [ ] `go f()` in user code rejected at SSA? Exemption list in
      `restrict.go:isUserPackage` still accurate (`runtime*`, `internal/*`,
      `os`, `syscall`, `unsafe`)?
- [ ] `coro.mx` still needed only for `iter` package?
- [ ] Scheduler config (`"tasks"`/`"asyncify"`/`"none"`) — is anything
      other than `"none"` reachable? If not, strip the branches.
- [ ] Check: `compiler/compiler.go:1524-1530`, `compiler/goroutine.go`.

### 8. Defer chains
- [ ] Loop-detected defers use heap allocation? Tarjan SCC still runs?
- [ ] JS backend: `$rt.runtime.recover()` exists in `jsruntime/`? Native
      `_recover` supported on target arch?
- [ ] Closure-in-defer: captured vars reach deferred call correctly?
- [ ] Panic path: try/catch/finally in JS, landing-pad in LLVM.
- [ ] Check: `compiler/defer.go`, `jsbackend/instructions.go` finally block.

### 9. SSA pass ordering
- [ ] Native builds SSA per-package inside parallel jobs; JS builds
      once globally. Any new cross-package analysis added since the last
      audit? If yes, it must live in the global-build (JS) pipeline or be
      moved out of per-package parallel jobs (native).
- [ ] Any pass assuming "entire program's SSA is built"? Verify it runs
      after `Build()` — not inside `CompilePackage`.

### 10. Generated-output smoke
- [ ] Regenerate one small test program for each target (JS, native).
      Diff the entry point / init-chain structure against a known-good
      baseline.
- [ ] Run `tests/` end-to-end on both targets.

## When to run

- After any change to `compiler/`, `jsbackend/`, `transform/`, `builder/`.
- After bumping `vendor/golang.org/x/tools/`.
- Before release tags.
- When a user reports behavior that only reproduces in one backend.

## When a check fires

Don't patch symptoms. Trace back to whether the underlying assumption
(SSA flag, pass order, backend responsibility) is what it should be.
The bug is usually a seam between TinyGo's assumptions and Moxie's
surgery, not a single wrong line.
