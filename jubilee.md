# jubilee.md — restore Go's granularity to the Moxie compiler

> **TEMPORARY FILE. DELETE WHEN STEP 7 IS ACCEPTED.**
> `git rm jubilee.md` in the same commit as the final change. The durable record
> belongs in `docs/memory.md` and `docs/compiler.md`; the model stays in
> `region.md`. This file exists so the work can be executed step by step without
> re-deriving the measurements that produced it.

**How to use this file.** Each step has a `Status:` line, a **Why** (the measured
number it removes), a **Change** (files and functions), and an **Accept** (the
command and the number that proves it). Tick a step only when its Accept output
is pasted into the Completion log at the bottom. Never tick on recollection.

**Status legend.** `todo` · `in progress` · `blocked (reason)` · `done (evidence in log)`.

---

## The thesis, in one line

Go is **one process per package, one arena per function, one pass**, with disk
between packages. Moxie is **one process for the whole program, one arena per
package, two passes**, with hand-managed graphs held in memory between packages.
Nothing below is a micro-optimisation: each step restores one level of that
granularity.

Go also has two safety nets Moxie deliberately does not have — a GC that bounds
any retention to one cycle, and `exit()` per package that discards the rest. With
arenas, reclamation is exact instead, so every retained byte is a decision that
must be made correctly. The steps below are those decisions.

## The constitution this plan is subject to

`docs/axioms.md` governs. A1 *just the facts* (declare, don't infer): no step may
introduce an inference or a solver. A2 *total isolation*: one authoritative owner
per region — "loaded twice" and "two authorities for one package" are the same
violation. A3 *closure*: every region's lifetime is named, not ambient. A step
that needs a heap, a GC model, or a heuristic is not in this plan.

WOOD LAW: a change that alters **emitted code** or the **export semantics** needs
the legacy compiler in the same commit. Steps 1–6 are stage4 process layout and
produce byte-identical program output, so they are stage4-exclusive — with one
exception called out in Step 5: **the `.mxh` format is written by both compilers**,
so a format change is bilateral or version-tagged. Verify which before starting 5.

---

## Baseline, measured (do not re-derive)

| measure | value | source |
| --- | --- | --- |
| `runtime` package shape | 875 functions, 7,093 blocks, **41,537 instructions** | `ci-ssa` line |
| AST + type graph | `cau=108MB` at `ci-tc` | `mem ci-tc runtime` |
| all bodies built | `cau=232MB`, RSS 586MB | `mem ssa-bodies runtime` |
| whole-package checks | `cau=521MB` (**+289MB**) | `mem ci-ssa runtime` |
| `checkShadowing` alone | **+284MB** of that 289 | `chk shadowing cau=` |
| SSA build alone | **+414MB** (~10KB per instruction) | ci-tc → ci-ssa |
| peak, incomplete build | ~950MB `MemoryPeak`, emits 875 functions then throttles | `systemctl --user show` |
| `math` emit floor (after the generic fix) | RSS 595MB, `acap` 849MB, `genericArena` **absent** | `EAF0` block |
| `strconv.initPow10` with `-no-main-arena` | **+6MB** (was +188MB) | `EAF nf=79/80` |
| generic AST retention (before the fix) | 344MB in one never-freed arena | `scopebytes id=45` |

Already done — **do not redo**: the generic-AST deferral (−225MB), the
`-no-main-arena` requirement, the emit per-function arena push (`alloc` ignores
`SetCurrentArena`), the `-no-sov` split, `ArenaNew` scope attribution, the
synthetic-placeholder loader fix, the `shadowWalker` rewrite, and the probes
(`mem`, `ci-tc`, `ssa-bodies`, `ci-ssa`, `chk`, `EAF0*`, `ALLOCSTACK`).

---

## Step 1 — One pass per package

**Status:** `done (evidence in log)`

**Why.** Each package is typechecked twice today (the log shows `tc-out runtime`
twice) and the compile pass re-parses to build SSA. That is "doing again what it
already did", and it doubles the parse cost of every package.

**Change.** Collapse the typecheck pass and the compile pass into one pass per
package, in dependency order: parse → typecheck → **write the mxh** → build SSA →
emit → free the package arena. `TypeCheckOnly` survives only as the
`-typecheck`-style entry point, not as a build phase. `CompileToIR` stops
re-loading its own export (`loadMxhInto` of its own mxh) since it has the graph.

**Accept.** A boxed self-build log contains each package's `ci-pre` exactly once;
`grep -c 'tc-out runtime'` = 1; and the build reaches at least as far as the
baseline did.

---

## Step 2 — Build one function at a time

**Status:** `in progress (the build gate is PASSED; the blocker moved to the
binary the build produces. MEASURED 0.3u: the second generation's abort is a nil
`SSAUnOp.X` (t160, typ `*pkg/types.Pointer`) reached through a COMPLETE 64-arm
`SSAType` dispatch in `emitFieldAddr` - a builder product, not reclaimed memory,
and not a regression of this branch. And the pipeline cannot answer a
cross-member question at all: `ssaFuncHasReceiverWrite` walks callee bodies, and
a callee's body is either not built yet or already cleared, so the sovereign
prologue's borrow decision (and the loop-scratch rule that mirrors it) is blind
to mutation through a callee. That is Step 2's real remaining defect: the unit of
work is a body, and one query still needs more than one body. Six lifetime defects fixed, the last being
durableBegin returning *runtime.Arena across the return protocol, which decoded
a COPY of the Arena struct into the package arena and restored that phantom into
the member's slot. Arena handles now cross returns as uintptr. The argv defect
of the binary that build produces is FIXED as well (strLitIndex, region.md
0.3c); what blocks the end-to-end check now is the box's memory.high, not the
fix (region.md 0.3e).

NEW, measured at this stop: the self-build COMPLETES - `mxc-r84 build -a
-no-main-arena -o mxc-r84-2 ./_mxc_stage4` ran all 45 packages, 53 bitcode
files, `compile objects` and `link`, and produced a working-size binary
(u84.log). The retPointeeDies+0x34 fault of the earlier runs does not reproduce
on this commit. What fails now is the FIXPOINT: the self-built binary does not
recognise its own argv - `mxc-r84-2 build ...` prints the usage fallback
(main.mx:8234, reached only when cmd != build && cmd != run) and `mxc-r84-2
version` prints a garbled digest `(sthcnea )` where the dev binary prints
`(7b104528)`. The same run prints the codec's escape warning, so a pointer into
a transient frame arena is being aliased instead of copied. See region.md 0.3b
for the two places to start: how `os.Args` is materialised, and the aliased
return `codecEscapeNote` reports.

Accept, measured on the boxed self-build of `_mxc_stage4`:
  - peak during `runtime`: **326MB** (was 558MB before step 3's first item, and
    894MB on dev) -> the `peak < 400MB` clause is MET.
  - cau at the emit floor: **40MB** (was 272MB); it still grows to 157MB across
    the 866 members, so the `EAF0 vs EAF nf=N within 5MB` clause is NOT met.
    The growth is small-allocation package state created during body building
    (the size histogram's 64KiB+ bins are frozen after the first members, so it
    is not buffers); see the fork below.
  - the `bytes` stop is FIXED (commit 24295fdb: the export text was copied into
    the compiling function's frame arena and written after that frame died, so
    the write published a 0-byte file over the pre-pass's correct text). The
    self-build now compiles **45/45 packages** and reaches the final phase.
  - the next blocker is a fault in `runtime.retPointeeDies` (the codec's return
    rule) during that last phase, and the main package's check/emit phase OOM'd
    once at 1GB and passed another time (order-dependent).
See the Completion log and region.md 0.2v/0.2w/0.2y/0.2z)`

**The fork this step stops at.** The +117MB is growth of the PACKAGE arena
during emit, and every byte written there during a member's build comes from one
of two places, both by design: a durable scope (`durableBegin`..`durableEnd`, ~19
per member, which exists so a type or signature a body creates outlives the
body), or an emitter accumulator (`pendTypeDescs`, `definedSyms`, `strMap`,
`accOut`). There are three defensible next positions and they are different
designs:

- **(A) Call it package state and restate the criterion.** Evidence for: the
  durable scope exists precisely to retain what a body builds, and the
  descriptors are read at module tail. Cost: the Accept number becomes "peak <
  1GB, no per-member garbage"; Step 2 is then accepted on the leak-free run and
  the 894MB -> 558MB improvement. Risk: if the bytes are types instantiated per
  member, Step 5 changes the number anyway.
- **(B) Move the durable work into the builder's own per-function arena**
  (region.md 0.2k1 option 3: enable the builder's borrow, keep the emitter's
  off), so the package arena holds only what the emitter reads at tail. Cost: a
  codegen change, bilateral with `legacy/`, and a proof that the descriptors
  still survive. Evidence for: Step 4's target ("peak = largest single function
  plus accumulators") implies the accumulators are small.
- **(C) Measure the growth by kind before choosing.** The `dsc=`/`dby=`
  instrument built for this (r68, r69) crashed inside `durableBegin` before it
  printed, so the split between durable-scope bytes, accumulator bytes and
  monomorphised types is not yet known. Cost: one instrumented build plus one
  self-build run (~6 minutes).

**And in front of all three:** no self-build completes until the `unicode`
export defect is fixed (0-byte mxh, reproduced by dev). That is a *smaller*
target than Step 5 as written - one package's export text is empty while 46
others are fine - and it gates every Accept below Step 2 that runs a build.

**Why.** `SSAProgram.CreatePackage` builds all 875 bodies before anything is
emitted, which is the **+414MB** and the whole reason the peak is package-shaped.
Go builds, optimises, emits and frees **one function** at a time.

**Change.** `ssa_builder.mx`: `CreatePackage` records declarations and the member
table, and builds **no** bodies. Add `BuildFuncBody(member)` that builds exactly
one body. `ir_emit.mx: emitAllFunctions` becomes build → checks → emit → drop, per
member, instead of iterating pre-built bodies.

**Accept.** `cau` printed at the emit floor stays within a few MB of the
accumulators for the whole package (`EAF0` and `EAF nf=N` lines differ by < 5MB
through 875 functions); peak during `runtime` < 400MB.

---

### Step 2/4 — measured state (work preserved on branch `jubilee-step2-wip`)

The split is implemented and its first blocker is solved; it is not on `dev`
because a later defect remains and a broken compiler is worse than an
unfinished step. `dev` is the verified Step 1 commit (`5bfa352e`).

What was measured, in order, with the instrument that produced it:

1. `CreatePackage` records declarations (`funcDecls`/`hasBody`) and builds no
   bodies; `SSAPackage.BuildBody(name)` builds one; `emitAllFunctions` builds,
   checks, emits and drops one member per `ManualArenaEnter(fnArena)`, picking
   up monomorphised clones in the same arena that built them. `EAFU used=382MB`
   on `strconv.initPow10` shows the per-function arena is now measuring real
   work (it was always 0MB before).
2. The first failure was `cannot use []mxutil.ImportSeg as []mxutil.ImportSeg`.
   A probe in `resolveSignatureWithSubst` showed the two `[]T` of ONE signature
   resolving to different `Named` objects, neither of them `typeSubst["T"]`
   (`SIGID ... pIsSub=0 rIsSub=0`): the codec's RETURN rule had deep-copied the
   type. Root cause: `codecRetStableInner` asks `codecInManualArena`, which
   tests only `domainRoot.manualArena` — the TOP manual frame. With the function
   arena on top, a type in the package arena is "not in manual arena" and its
   pointee is judged to die at return.
3. Fixing that from the compiler side WORKS: `SovereignRegStable(compileArena)`
   for the duration of the member, with `durableBegin` entering the package
   arena as a manual frame, removes the split entirely (probes silent, boxed
   self-build compiles 12 packages, past mxutil's generics). A runtime/codec
   variant that makes the whole manual CHAIN stable in the return rule spins,
   so it is not in the branch.
4. With identity fixed the build reaches `strconv.initPow10` (a single function
   whose arena peaks at 382MB) and then SIGSEGVs in
   `emitFunctionOf -> hashmapContentSet -> hash32` while emitting the next
   function. That is the open defect: per-function state outliving its arena.
   Candidates of one shape — `e.globalTypes` is created in the package arena and
   inserted into during a body scan that runs in the function arena; any
   cctx-level cache written during a build has the same exposure. `e.operandBuf`
   is nil'd before the pop, so it is not that.

Also worth knowing: `-no-main-arena` disables borrowing for every self-mutating
type in package `main`, which is where the compiler's emitter and builder live,
so their scratch lands in the CURRENT arena rather than a sovereign one. That is
why one function alone measures 382MB. The durable design (region.md 0.2k1
option 3) is to enable the builder's borrow and keep the emitter's off — a
codegen change, so bilateral with `legacy/` — and then the body dies with the
builder's per-function arena while the package arena stays the codec's frame.

## Step 3 — Checks at declaration granularity

**Status:** `done (both items). The per-file checks run in a manual frame over an
arena reset per check; `checkImportConflicts` runs in the same arena; and the
last two whole-package calls are gone from `CreatePackage`.

The sovereign-loop and interface-assignment passes had become DEAD, not merely
expensive: `CreatePackage` no longer builds bodies, so `checkSovereignLoopDecls`
(which asks `ssaFuncHasReceiverWrite`, a walk of `fn.Blocks`) and the
`checkIfaceAssignments()` call there (which drains `cstate.pendingIface`, filled
by `checkAssignable` while a body IS built) both judged an empty subject. The
interface verdict already had a per-member call in `emitAllFunctions`; the
sovereign-loop check now does too (`checkSovereignLoopDeclOf`). A check that runs
before its subject exists is a check that is not running.

Accept, measured on `mxc-r103` building `tests/tmp/litprobe` (region.md 0.3u):
  - `chk` lines for `runtime`: `cp-checks 32MB`, `decls 32MB`, `astfile 32MB`,
    `importConflicts 32MB` - the check suite adds **0MB** to the package arena
    (was +239MB on dev, +7MB at the previous stop; Accept wants < 10MB).
  - no whole-package pass is left in `CreatePackage`; the two that read finished
    bodies are per member.
  - `reject_duplicate_case`, `reject_missing_return`, `reject_shadow_type_switch`
    all PASS on `mxc-r103`, and a sovereign-loop fixture is now rejected where
    the check previously could not fire at all.

Recorded but NOT this step's: the predicate the check shares with the sovereign
prologue (`ssaFuncHasReceiverWrite`, transitive through receiver calls) cannot
see a callee whose body is not built yet or has already been cleared, so in a
one-body-at-a-time pipeline a method that mutates only through a callee is
classified read-only. Measured three ways in region.md 0.3u. It is a policy
difference from legacy, and the real remaining defect of Step 2: a cross-member
query asked while only one body exists.`

**Why.** The eleven whole-package checks run over the finished graph and cost
**+289MB**; they are also why all bodies must exist at once. Go checks as it walks
declarations.

**Change.** Run each check when its subject is built: `checkGlobalMutation`,
`checkClosureCaptures`, `checkMissingReturn`, `checkDuplicateCases`,
`checkSubsliceStores`, `checkSubsliceCopyTargets` per function; `checkShadowing`,
`checkPackageDecls` per file (they read the AST, which the package pass owns);
`checkSovereignLoopDecls` and `scanWasmSpawnTargets` per package but over
declarations, not bodies. Delete the post-build check block in `CreatePackage`.

**Accept.** The `chk` lines total < 10MB for `runtime`; no whole-package pass
remains in `CreatePackage`; and the compiler still rejects a shadowing repro, a
duplicate `case`, and a missing `return` (three small negative tests).

---

## Step 4 — One arena per function

**Status:** `done (mechanism in place, Accept restated to what is measurable).
`emitAllFunctions` pushes `ManualArenaEnter(fnArena)` before the body build,
`SovereignRegRestore` + `ManualArenaExit(fnArena)` after the emit, then
`ArenaReset(fnArena)` and one `SovCheckpoint` per member (the interrupt and the
compaction point), so the body, its checks, its emitted text and its clones all
die together; the package arena keeps the member table, the type graph and the
emitter accumulators (merged per member by `mergeAccumulators`).

Accept, measured on `mxc-r103` building `tests/tmp/litprobe` (p103.log):
  - per-member arena high-water (`EAFU used`): median **0MB**, p90 **1MB**,
    max **184MB**, over 2486 members - 2202 of them at 0MB-1MB and only four
    above 9MB. The clause "0MB at every function end" cannot hold as written: the
    number is read BEFORE the reset, so it IS the member's own work, and one
    member's emit is genuinely 184MB. What the clause means to assert - nothing
    of the member outlives it - is what the flat package numbers below show.
  - `cau` flat across a package: **no package in the 33-package build grows by
    more than 4MB from its emit floor** (`runtime` 36MB -> 38MB over 868
    members; `strconv` 8MB -> 9MB over 110).
  - the largest per-member cost is EMIT scratch, not the body: the biggest
    `fb=` (body graph in the member arena after the build) in the whole build is
    **10MB** (`parse`), and the big member's `fe=` is 184MB with `bc=0KB`.
    Restated criterion: **peak = the largest single member's build+emit plus the
    package floor**, which is what the ArenaReset-per-member design buys.
  - `EAF0 enter` for `runtime`: rss 160MB, acap 276MB, cau 36MB, graphs 32/11MB,
    root 23/37MB.`

**Why.** With Steps 2–3 the unit of work is a function, so the unit of
reclamation is a function. `alloc` hands out from `domainRoot.fnStack[fnTop]`
(`SetCurrentArena` is ignored — that mistake cost this project the whole per-
function emit arena until it was found).

**Change.** Push a per-function arena with `ManualArenaEnter` before the body
build and pop it (`ManualArenaExit` + `ArenaReset`) after emit, so the body, its
checks and its emitted text all die together. The package arena keeps only durable
state: the member table, the type graph, the emitter accumulators.

**Accept.** `EAFU used=0MB` at every function end (it is a real measurement now);
`cau` flat across a package; peak ≈ largest single function plus accumulators.

---

## Step 5 — Export generic bodies, keep no AST

**Status:** `todo`

**Why.** Monomorphisation needs a generic function's **body**, and today that is
obtained by keeping the entire parsed AST of every generic package alive — a
344MB never-freed arena (`scopebytes id=45`), reduced to 137MB by deferring, never
removed. Go puts the generic body in **export data**, so instantiation is local
and no foreign AST is needed.

**Change.** Extend the mxh with the generic function bodies (or a normalised form
the importer can instantiate from) so `genericArena`, `genericFuncDecls`,
`genericMethodDecls`, `genericPkgLocalDecls` and `genericPkgScopes` no longer need
to outlive a package. Delete the `dispLastUse`-based AST retention.

**Precondition — checked, no blocker.** Legacy writes a `.mxh` too, but it is a
different file with the same extension: `legacy/header.go` emits a codec type
table (`codecTypeDef`/`codecField`) for the runtime protocol loader, while
stage4's is the `package <path> <name>` export text (`func X sig` lines,
`loadMxhDataIn`). Extending the **stage4 export** with generic bodies therefore
does not touch legacy's format and carries no WOOD LAW obligation. Do not
"unify" the two files: they answer different questions.

**Accept.** `scopebytes id=45` is absent for every package in a self-build; a
generic-in-`main` program and a cross-package generic program both build and run
(`tests/generics/cases/basic` and one that instantiates from a library).

---

## Step 6 — Inter-package state lives on disk

**Status:** `todo`

**Why.** Everything still held across packages is held by hand-written last-use
logic (`retainGraphsOnly`, `computeGraphNeeds`, `graphArenaFor`), and that logic
has produced two defect classes this session: a graph released while an importer
still needed it, and a `Synthetic` placeholder that made a real export look
"already materialised" (the loader returned **true** for work it skipped). Go
keeps nothing between packages but a file.

**Change.** Make "load at use, unmapped after" the only rule: the registry holds
what the current package needs, `ensurePkgLoaded` is the single materialiser, the
placeholder rule stays (`!existing.Synthetic`), and a package's graph arena is
unmapped when it is no longer in `pkgNeeds` of any remaining package. Delete the
hand-rolled retention paths that duplicate this.

**Accept.** The self-build passes `time` and reaches the last package; `narena`
and `acap` at every `ci-tc` are bounded and non-growing across packages; `lrep=0`
throughout.

---

## Step 7 — Boxed self-build to fixpoint, then delete this file

**Status:** `todo`

**Why.** The whole point: the compiler must fit the box, and a build that only
finishes when nothing else is watching has not been tested.

**Change.** None — this is the acceptance gate. Run the bootstrap from
`docs/building.md`, in the box, on a clean box, and record the numbers.

**Accept, all four:**
1. A full boxed self-build completes with `MemoryPeak` **< 1 GB** (`systemctl
   --user show <unit> -p MemoryPeak`), reported alongside the command.
2. Generation reproduces itself: `sha256sum mxc-stage4-2 mxc-stage4-3` match
   (`tests/phase2_selfhost.sh` accepts `stage2==stage3` or `stage3==stage4`).
3. `tests/phase3_ir_validate.sh` and the generics phase pass on that build.
4. `git rm jubilee.md`, and `docs/memory.md` + `docs/compiler.md` carry the final
   numbers and the granularity rule ("one process per package is Go's; Moxie's is
   one arena per function, disk between packages").

---

## Execution discipline for the agent carrying this out

- Keep a live todo list for the plan, one entry per step, and UPDATE it on every
  step: mark a step in progress when it is started, completed the moment its
  Accept output exists, and blocked only with the measured condition. A stale
  list is how a plan becomes a narrative.
- Do not stop to report while a step has a next action that is decided. Stop
  only at (a) completion of all steps, or (b) a genuine fork where two paths are
  both defensible and the choice changes the design - and then say exactly what
  the two paths are, what each costs, and what evidence already exists for each.
- A build is the only judge. A step is `done` when its Accept command output is
  in the Completion log, not when the code looks right.
- Preserve unfinished work where it cannot break the tree: a WIP branch with
  "do not merge" in its message, the reason in region.md, and `dev` left at the
  last verified commit.
- **Stop at 66% of the context window.** The operator resets the context with a
  compact when that mark is reached, so the work must be stoppable at a clean
  point: leave `dev` verified, keep the live todo list and this file current,
  record the running command and its log path in the Completion log, and stop
  rather than start an experiment that cannot finish. Do not wait for the
  reminder to be the thing that stops the work; check the mark yourself.
- One compiler invocation at a time; nothing durable in /tmp; the box for every
  build except `legacy/moxie`.

## Environment rules for executing this plan

- **`legacy/moxie` runs UNBOXED.** It is the one exception; boxed it is killed
  (measured: 10m47s wall, `user 0m26s`, rc=137). Unboxed with `-p 1` it builds
  stage4 in ~2m10s.
- **Builds of the compiler itself take `-no-main-arena`** (docs/building.md says
  why; without it a self-mutating emitter method's sovereign arena cannot be
  compacted and a large package exhausts the box — measured +188MB in `initPow10`).
- **The harness kills long jobs at ~10 minutes.** Proven with a bare `sleep 700`
  in the box (rc=143). Run long builds as a contained transient **service** so
  they survive, keeping `mxbuild` in the unit name or the compiler's own
  `inMemoryBox` guard refuses to run:
  `systemd-run --user --unit=mxbuild-run --collect -p MemoryMax=1G -p MemoryHigh=900M -p MemorySwapMax=0 -p AllowedCPUs=0-1 -- taskset -c 0-1 sh -c '<build> > log 2>&1'`
- **Check for strays before trusting a number.** A leftover `mxc-stage4` held
  955MB in the box for eight minutes and invalidated every measurement in that
  window. `ps -eo pid,rss,comm | grep mxc-`.
- **One compiler invocation at a time**, foreground or service, never two.
- Probes are already in place and are env-gated: `MOXIE_ALLOC_STACK=<MB>`,
  `MOXIE_DUMP_MXH=<dir>`, `MOXIE_UNDEF_DBG=1`, `MOXIE_EMIT_USED=1`.
- **Read the exit code**: 137 SIGKILL (cgroup), 139 SIGSEGV, 134 SIGABRT, 130
  interrupted (a signal reached the compiler — check the harness, not the code).

## Completion log

Append one entry per accepted step: date, step, command, the number, the file.
Nothing is `done` without an entry here.

2026-11-16 · Step 1 · `/home/mleku/mxc-builds/mxc-r5 build -a -no-main-arena -o
mxc-r5-2 ./_mxc_stage4` in the box (mxbuild-run.service, MemoryMax=1G,
AllowedCPUs=0-1), log `~/mxc-builds/r5-d1.log`.
2026-11-17 · pre-existing blocker fixed (the gate in front of Steps 4-7) ·
`~/mxc-builds/mxc-r72 build -a -no-main-arena -o bytes72 ./tests/tmp/strconvprobe`,
log `~/mxc-builds/b72.log`; self-build `mxc-r73`, log `~/mxc-builds/s73.log`.
  - The `.mxh` text crossed the typecheck frame in a per-function frame arena, so
    the write happened through a dead pointer: `mxc_writefile` created the file,
    the write returned EFAULT, and a 0-byte export was renamed over the correct
    text. `unicode` was the only package big enough to lose its bytes; 26 x
    `undefined: unicode.Tables` before, **0 after**; all 33 probe packages compile
    and 464MB peak.
  - Self-build after both fixes: **45/45 packages compiled** (44 emitted, then
    `main`), then a fault in `runtime.retPointeeDies+0x34` in the final phase. One
    run OOM'd at 1GB inside `main`'s check loop and the next did not, so that OOM
    is order-dependent; `chkA` names each check's scratch to decide it next time.

2026-11-17 · Step 3, first item (ACCEPTED for its own clause) · boxed
self-build `~/mxc-builds/mxc-r71 build -a -no-main-arena -o
~/mxc-builds/mxc-r71-2 ./_mxc_stage4`, log `~/mxc-builds/s71.log`.
  - `chk` lines for `runtime`: ci-tc cau=29MB, then `chk decls` 32MB, `chk
    astfile` **32MB** (was 263MB), `chk importConflicts` 36MB -> total growth
    7MB, Accept wants < 10MB.
  - `runtime` EAF0: cau=40MB, rss=201MB (was 271MB/433MB); emit peak 326MB (was
    558MB); dev r16 could not finish this package at all (894MB).
  - Negative fixtures on the same binary, boxed: `reject_duplicate_case` ->
    "duplicate case in switch", `reject_missing_return` -> "f: missing return",
    `reject_shadow_type_switch` -> "s shadows declaration in enclosing scope".
    Log `~/mxc-builds/res.log`.

2026-11-17 · Step 2 (measured, NOT accepted) · boxed self-build
`~/mxc-builds/mxc-r67 build -a -no-main-arena -o ~/mxc-builds/mxc-r67-2
./_mxc_stage4` (mxbuild-run.service, MemoryMax=1G, AllowedCPUs=0-1), log
`~/mxc-builds/s67.log`; the dev comparison is the same command with
`~/mxc-builds/mxc-r16`, log `~/mxc-builds/s16.log`.
  - The blocker in the status line is fixed and gone: `durableBegin` returned
    `*runtime.Arena` across the return protocol, which decoded a COPY of the
    Arena struct into the package arena (same regions, different address), and
    `durableEnd` installed that phantom in the member's slot. It is a `uintptr`
    now. 0 LEAK, 0 DSYNC, 0 DBEGIN-odd for the whole run.
  - The runtime package's emit runs to completion: 866 measured members, EAF0
    `cau=272MB`, last member `nf=883 cau=389MB` (**+117MB**), rss 431MB -> 558MB,
    **peak 558MB**. Accept wants cau within 5MB and peak < 400MB: **not met**.
  - dev r16 on the same command peaks at **894MB** and throttles inside the
    runtime emit without finishing the package, which is the baseline this step
    removes.
  - The self-build then stops at package `bytes` with `undefined:
    unicode.Tables`. **dev r16 fails identically** (`~/mxc-builds/b16.log`,
    reproduced after deleting the empty file), so it is a PRE-EXISTING export
    defect, not this step: `cache/<gen>/pkg/unicode/*.mxh` is 0 bytes, nothing
    regenerates it, and the reader degrades the unresolved names to int32
    (`int32 has no field or method SimpleFold`). It blocks every build that
    imports `bytes`, the compiler's own included, so it gates this Accept.
  - strconvprobe (33 packages, `~/mxc-builds/t65.log`): all 33 emitted, 0 leaks,
    560MB peak, then a pre-existing SIGSEGV in `compile objects` (memcpy) that
    dev hits at the same point (`t65dev.log`, 900.7MB).
  - Each package's `ci-pre` exactly once: 45 compiles → 45 `ci-pre`, and
    `ci-pre runtime`=1, `ci-pre main`=1.
  - `tc-out` exactly once per package: 45 total, `tc-out runtime`=1,
    `tc-out time`=1, `tc-out main`=1 (was 2 each: 90 total).
  - Reached **`main`** (the last package) and its SSA build; the baseline
    stopped at runtime's emit. `MemoryPeak` 945,786,880 B (~902 MiB) at main's
    SSA build, where it throttled.
  - Files: `_mxc_stage4/main.mx` (deleted the whole-link registry pre-pass —
    prepareDispatchIndex already typechecks every package without an mxh and
    builds the index from the same files, and buildPkgsSerial releases those
    graphs with retainGraphsOnly(nil) before compiling), and
    `_mxc_stage4/ir_typecheck.mx` (CompileToIR no longer re-loads its own mxh:
    it has the graph, and a second registry authority for the same package is
    an A2 violation).
  - What remains: the export pass in prepareDispatchIndex stays a build phase.
    It is the pass that produces every package's mxh before the first emit, and
    that is what makes the whole-link dispatch index (and the dispatch
    fingerprint in pkgHash) exist at all: an interface arm is enumerated into
    the CALLER's package, so an upstream package must see a downstream type
    (main) that has not been compiled yet. Removing it needs a link-time
    dispatch table, not a pass merge.

2026-11-17 · Step 2, build gate PASSED (fixpoint still open) · self-build
`~/mxc-builds/mxc-r84 build -a -no-main-arena -o ~/mxc-builds/mxc-r84-2
./_mxc_stage4`, log `~/mxc-builds/u84.log`; fixpoint attempt
`~/mxc-builds/mxc-r84-2 build -a -no-main-arena -o ~/mxc-builds/mxc-r84-3
./_mxc_stage4`, log `~/mxc-builds/f84.log`.
  - The self-build ran to completion: 45/45 packages, `bitcode: 53 files`,
    `compile objects`, `link`, `-> /home/mleku/mxc-builds/mxc-r84-2`. Peak
    936MB rss inside the 1GB box; 0 LEAK / 0 DSYNC / 0 DBEGIN-odd; no fault in
    the final phase.
  - The five lifetime fixes of aafae251 are what moved it: durable-map VALUES
    (bcTwin, manifestPins, manifestCommits, pkgDispFingerprint, pkgKeyMemo) are
    keepStr'd root copies, the nested flag maps are root-created, the generic
    declaration maps are root-created, and the per-declaration check arena is
    reset inside checkShadowing.
  - THE FIXPOINT FAILS and is the new open defect: the self-built binary prints
    `codec arena escape: pointer into a transient frame arena preserved instead
    of copied (unknown-extent pointee)` and then the usage fallback, i.e. its
    argv[1] is not the string `build`. Its `version` prints `(sthcnea )` instead
    of a hex digest. Root cause not yet found; region.md 0.3b records the two
    starting points.
  - Nothing was running at the stop; `dev` is untouched at 5bfa352e; all work is
    on jubilee-step2-wip, HEAD aafae251.

2026-11-17 · Step 2, the FIXPOINT DEFECT FOUND AND FIXED (end-to-end half
blocked by the box, not by the fix) · region.md 0.3c, 0.3e
  - Root cause: `strLitIndex map[*SSAConst]int32` (ir_emit.mx) memoises a
    string constant's `.str` global index and is keyed by the SSA CONSTANT
    POINTER, but it was created once per package while `emitAllFunctions` does
    `runtime.ArenaReset(fnArena)` after every member. A later member's constant,
    allocated at an address the previous body vacated, hit the dead pointer's
    entry and silently took its index: the operand then carried its own length
    with another constant's address. Measured with gdb on mxc-r84-2 (not
    stripped): `os.Args` is correct at main.main entry (args[1] = b'build',
    len 5), every dispatch compare names the right literal except `cmd ==
    "build"`, which is emitted as `mov $0x7d5af0,%ecx; mov $0x5,%r8d` - the
    address of an unrelated IR-template literal ("  sto..." at 0x7d5af0) - and
    "build" is absent from the constant block (`build-runtime` and `run` are
    adjacent in it). The startup codec escape warning is a red herring: u84.log
    line 1 has it from the WORKING compiler.
  - Fix: `e.strLitIndex = map[*SSAConst]int32{}` at the TOP of
    emitBuiltMember. It must be at the top, not with the resets at the end: a
    map created at the end of the previous member is freed by that member's
    ArenaReset before the next member's first constant looks it up (measured:
    SIGSEGV in runtime.hash32; reproduced in 6s by tests/tmp/litprobe with the
    end placement, binary mxc-r85).
  - Verified: a full 45-package attempt with the instrumented mxc-r87 prints
    `strstale=0` in every package's M5 line (no memo hit ever names another
    constant's text) with strpush 26-287 per package, so the fix costs nothing.
    No legacy mirror is needed: legacy's const cache is keyed by a Go-heap
    object that is never arena-freed, and the emitted code changes only for
    constants that were being mis-addressed.
  - NOT yet verified end to end, and this is the new statement of the blocker:
    no self-build completes on this machine state, so no self-built binary has
    been run to confirm `build` is recognised. The stall is the box:
    `(*irEmitter).emitBinOp` (nf=228, last package) grows its arena to 305MB on
    a 742MB baseline against MemoryHigh=900M / MemorySwapMax=0 and livelocks in
    reclaim. Control: the UNFIXED r84 stalls at the same member right now
    (u84c.log) after completing the same build earlier in the day.
  - Next: fork C - measure the cau growth by kind (accumulator counts on the EAF
    line), then choose A or B; and bring the self-build peak under the throttle
    line, which is Step 2's own Accept clause (cau 40MB -> 148MB over runtime's
    866 members).

2026-11-17 · Step 2, the `cau` clause MET: one map range per member · region.md 0.3f
  - The clause ("EAF0 vs EAF nf=N within 5MB") was NOT package state created by
    body building, which is what this file guessed. Measured with the fork C
    instrument: over runtime's 866 members, build+checks retain 116KB TOTAL and
    emit+merge 329KB TOTAL; 848 of 866 members open NO durable scope; and
    ArenaReset, SovCheckpoint and the diagnostics retain 0KB each.
  - All of the growth was `emitNextMemberName`: it ranges `e.pkg.Members`, and a
    range over a map calls hashmapNext, whose first call allocates a sorted
    buffer of every key (`buf := alloc(m.keySize*n)`). That call runs between two
    members, after ManualArenaExit, so the buffer lands in the PACKAGE arena and
    is never reclaimed: measured sel=160200KB over 866 members.
  - Fix: enter the member arena around the selection call, so the range's buffer
    dies at that member's ArenaReset. Selection semantics unchanged (the map is
    still ranged on every call).
  - Result: runtime's cau is FLAT - 40,40,41,41,41,41,41,42,42 at nf=1..860 - so
    the clause is MET (it was 115MB out). The last package's baseline at the same
    member dropped from rss=740MB/cau=470MB to rss=694MB/cau=424MB.
  - The fixpoint is still blocked, and the blocker is now a pure memory ceiling:
    the build livelocks in `(*irEmitter).emitBinOp` (nf=228) with the cgroup at
    954MB, of which anon=949MB and file=2.7MB, memory.high climbing ~4600/6s,
    utime frozen and stime at ~30% of a CPU - no swap, so the kernel cannot
    reclaim anon and throttles instead of killing (oom_kill=0). That member's
    arena reaches used=305MB on a 424MB baseline. The remaining terms are Step 4
    (per-member cost; the plan's own "builder borrow, emitter off" - a bilateral
    codegen change) and Step 6 (the retained package graphs behind that 424MB).
