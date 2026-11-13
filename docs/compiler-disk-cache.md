# Compiler Phases as Disk-Cached Units

Status: working brief and prompt. Implements D27 and D28 of `memory-model.md`
(section 23 states the design). Phase 9 of that document; no dependency on
phases 0a-8.

The rule this document exists to enforce: **a compiler is a pipeline of
processes over a content-addressed directory, not a program that accumulates a
universe in memory.** Everything below follows from it.

---

## 1. Why this, and why now

The measured failure is in `memory-model.md` 22.4e: one compile process made
**137 million 32-byte allocations** in a single arena, chained 64 MiB segments to
several gigabytes of address space, and reached an encoder seen-set of **262,144**
and kept climbing. Every attempt to fix it by tuning the allocator, the seen-set
index, the region scan or the arena tests moved the number by less than 10%.

The diagnosis recorded there is that the walk is a type graph containing **every
package compiled so far**, because each package's result was folded into the same
in-memory structure. That is not a runtime problem. It is a build-graph problem
solved the wrong way, and the correct repair is the one the Go toolchain has used
for fifteen years: one package per process, one artifact per package, artifacts
read back from disk by address.

Three consequences, and they are the reason this brief is short:

- The process boundary **is** the memory boundary. Nothing needs reclaiming, so
  the compiler needs no `shelf`, no layout descriptors, no stack maps and no
  sweep. Phases 3, 5 and 7 of the memory model can wait for a program that
  actually declares a store.
- The cache replaces the accumulation. "Everything I have compiled" becomes a
  directory, which is inspectable, shareable, and bounded.
- The artifact becomes the contract, so a rebuild that should be a no-op is
  provably a no-op rather than approximately one.

---

## 2. The prompt

Copy from here to section 7. Everything after is reference.

> ### Task
>
> Refactor the moxie compiler so that each package is compiled in its own
> process, its result is written once to a content-addressed artifact on disk, and
> no package's in-memory state is inherited by the next package's compile. The
> goal is three measurable properties, in priority order:
>
> 1. **Bounded memory per package.** Peak RSS for compiling package P is a
>    function of P and its direct imports, not of the program. Report the number.
> 2. **No repeated work.** A rebuild of an unchanged package performs no parsing,
>    no typechecking and no code generation for it. Count the operations, do not
>    assume.
> 3. **Deterministic artifacts.** The same inputs produce byte-identical
>    artifacts across runs, across `-j` values, and across a cold and a warm cache.
>
> ### Hard constraints
>
> - **Do not change the language.** No syntax, no semantics, no type-system
>   change. This is a build-system refactor.
> - **Do not touch the memory model.** No `shelf`, no segment changes, no
>   allocator rework. If you believe an allocator change is required, stop and
>   report it as a finding with the measurement that forced it.
> - **Both compilers keep working.** `_mxc_stage4` (self-hosted, moxie) and
>   `legacy/` (Go) must both be able to produce a cache entry, and the two must
>   agree on the artifact format. If that is impossible in one step, make
>   `_mxc_stage4` the producer and `legacy/` the reader, and say so explicitly.
> - **No phase renumbering anywhere.** `memory-model.md` 16 and the record in
>   section 21 refer to phases by number.
> - **Do not delete the fallback.** An in-process build path must remain behind a
>   flag until the process path has passed the full suite, the bootstrap chain and
>   a clean-tree rebuild.
>
> ### Step 1 — Measure the boundary before moving it
>
> No code changes. Produce a table, one row per package in the stdlib closure:
>
> | package | wall time | peak RSS | allocations | max seen-set | encode roots | import count |
>
> Instrument what does not already exist. The allocation histogram and the
> segment ledger landed in phase 0b; use them. For peak RSS, read
> `/proc/self/status` `VmHWM` at exit and report it per package.
>
> Exit criterion: the table exists, and it names which packages dominate. If one
> package accounts for most of the bytes, say so before proceeding, because it
> changes step 5.
>
> ### Step 2 — One package, one process
>
> The driver stops calling the front end in-process. For each package in
> dependency order it spawns:
>
> ```
> <self> -compile-one <import-path> \
>        -imports <file-of-import-artifact-addresses> \
>        -o <cache>/tmp/<addr>.<ext> \
>        -cache <cache>
> ```
>
> The child compiles exactly one package and exits. It reads import export data
> from the cache; it does **not** receive an in-memory graph from its parent.
>
> Parallelism is the parent's business: a bounded worker pool of children, with
> the dependency order enforced by the artifact addresses it already holds.
>
> Exit criterion: `strace -f -e trace=clone,clone3` on a two-package build shows
> two compile children, and each child's `VmHWM` is reported separately.
>
> ### Step 3 — The artifact and its address
>
> One artifact per package, one address per artifact.
>
> ```
> addr = hex(sha256(canonical manifest))
>
> manifest = {
>   "format":  1,
>   "target":  "native" | "wasm32",
>   "flags":   [...sorted...],
>   "sources": [ (relpath, sha256) ... sorted by relpath ],
>   "imports": [ addr ... in source order ],
>   "abi":     <export-data format version>
> }
> ```
>
> Rules:
>
> - The manifest is canonical: sorted keys, sorted lists, no absolute paths, no
>   timestamps, no hostname, no `$PWD`. A hash that changes when the tree moves is
>   not an address.
> - The artifact is written to `cache/tmp/`, `fsync`ed, then `rename`d into
>   place. A reader never sees a partial artifact.
> - On read, verify the bytes hash to the address. A mismatch is a cache miss, not
>   a warning.
> - `abi` is versioned separately from `format`. An export-data format change
>   invalidates entries without changing what an address means.
> - Cache layout:
>
> ```
> cache/v1/<addr>.mxh      export data (what importers read)
> cache/v1/<addr>.o        object code
> cache/v1/<addr>.json     the manifest, for debugging and for gc
> ```
>
> Exit criterion: two cold builds with different `-j`, in different working
> directories, produce identical `.mxh` and `.o` bytes for every package.
>
> ### Step 4 — Kill the inherited universe (the actual bug)
>
> This is the step that fixes the 137-million-allocation walk, and it is not
> optional.
>
> Today a package's type graph reaches objects allocated for earlier packages,
> because the universe accumulates across compiles and each return encodes a graph
> that transitively contains it. That must become impossible by construction, not
> improbable by tuning.
>
> The child process must:
>
> 1. Start with an empty universe (builtins only).
> 2. For each direct import, read its `.mxh` and decode it into **the child's own
>    arena**, producing exactly the imported symbols it needs.
> 3. Never retain a pointer into a decoded import beyond the package's own
>    compile, and never let a decoded import reference another package's
>    allocation.
> 4. Free everything at exit, which exit does for free.
>
> The invariant to assert, in debug builds: **no object reachable from the
> package's type graph has an owner other than this process's compile arena or
> MAIN.** A violation is the bug reappearing, and it should trap with the owning
> segment named.
>
> Exit criterion:
>
> - `max seen-set` for every package is under 10,000, down from 262,144+.
> - Total allocations across the closure fall by an order of magnitude, measured
>   by the histogram, not estimated.
> - The `codec bad pointer tag` family of failures in compiler-built compilers is
>   either gone or unchanged from its pre-existing state (see the trap list).
>   Distinguish the two explicitly.
>
> ### Step 5 — The memory ceiling
>
> A package compile that exceeds its ceiling fails the build with the package
> named. Start with a ceiling derived from step 1's table:
>
> ```
> ceiling = max(64 MiB, 4 * observed_peak_for_this_package)
> ```
>
> assert at exit:
>
> ```
> VmHWM            <= ceiling
> arena_bytes_mapped <= ceiling
> segments_live    <= 64
> ```
>
> This is D13's `-no-main-arena` grown up: instead of a special mode that
> allocates into MAIN and hopes, it is a measurement mode that catches a package
> whose compile has begun to accumulate again.
>
> Exit criterion: the ceiling holds on the full closure, and a deliberately
> regressed build (retain one extra cross-package reference) trips it.
>
> ### Step 6 — Prove the no-op
>
> Count operations, not seconds:
>
> ```
> parse_calls    typecheck_calls    emit_calls    link_calls
> ```
>
> A second build with no source change must report zero of the first three for
> every cached package.
>
> Exit criteria:
>
> - warm rebuild: `parse_calls == 0 && typecheck_calls == 0 && emit_calls == 0`.
> - touching one leaf package rebuilds that package and its dependents only; the
>   count of rebuilt packages equals the count of transitive dependents, printed
>   and asserted.
> - deleting one `.mxh` rebuilds only its package.
>
> ### Step 7 — Verification
>
> Run, in this order, and report each separately:
>
> 1. The full fixture suite (`tests/`, `run_tests.sh`).
> 2. The stdlib test suite, 11 of 11 as recorded in 21.3.
> 3. A clean-tree build of `./_mxc_stage4` from a fresh directory with an empty
>    cache.
> 4. The bootstrap chain C0 -> C1 -> C2, comparing against the same chain on
>    unmodified sources so that the pre-existing fixpoint failure (21.3) is
>    separated from anything this work introduces.
> 5. A `wasm32` build of the compiler, because `-target wasm` is a supported
>    output and the cache address includes the target.
>
> ### Reporting rules
>
> - Every claim names a fixture, a command, or a printed number. "Should now be
>   faster" is not a result.
> - If a step cannot be completed, stop at the first one that fails and report the
>   failure with its measurement. Do not proceed to later steps on an unverified
>   earlier one.
> - If a pre-existing failure is encountered, say which commit last showed it and
>   which binary builds the comparison. Do not attribute it to this work without
>   that comparison.
> - Keep a table of before/after for the three goal properties. Three numbers, not
>   a narrative.

---

## 3. The trap list

Every one of these has already cost a session in this tree. They are in the prompt
above only by reference; they are spelled out here so the next reader does not
rediscover them.

1. **`./mxc-stage4` is a build artifact and can be stale.** A copy in the tree
   predated the child channel handle copy and the ownership surface, so fixtures
   compiled with it exercised none of it and appeared to regress. Rebuild it from
   current sources with the previous one, then use *that* for fixtures.
2. **A timeout kill is not a segfault.** Several "the compiler crashed" conclusions
   in 22.4e were 300-second timeouts landing inside the runtime. Read the exit
   status before reading the backtrace.
3. **A cache hit can mask a compiler-side check you just added**, even with `-a`,
   when the source path was built before. Confirm negative fixtures in a fresh
   directory.
4. **The bootstrap fixpoint is pre-existing red.** Any compiler built *by* a stage4
   compiler panics at start-up in `codecDecodeValue`. Build comparisons with the
   checked-in `./mxc-stage4` (C0), which works and is reproducible.
5. **`sysroot/runtime_go.bc` is gitignored** and must be re-baked
   (`<compiler> build-runtime`) after any runtime change, or applications link the
   previous runtime and fail with undefined symbols.
6. **`-no-main-arena` is required for the bootstrap path** in its current form.
   Do not remove the flag while touching the driver; the cache address must
   include it (as `pkgHash` already does).
7. **Do not fix the codec on the way past.** The return-path and descriptor issues
   in 22.4e are real and are phase 1/4 territory. If the process split makes them
   unreachable, record that; if it exposes them, record that. Do not paper over
   either with a cache.

---

## 4. Acceptance criteria, as a checklist

```
  [ ] one process per package, observed with strace
  [ ] no in-memory state crosses a package boundary
  [ ] artifact address is a hash of canonical inputs only
  [ ] artifact write is atomic (tmp + fsync + rename)
  [ ] artifact read verifies the hash
  [ ] max seen-set per package < 10,000
  [ ] total allocations down by >= 10x, measured
  [ ] peak RSS per package is bounded and reported
  [ ] ceiling violations fail the build with the package named
  [ ] warm rebuild performs zero parse/typecheck/emit
  [ ] one-package edit rebuilds exactly its transitive dependents
  [ ] byte-identical artifacts across -j and across directories
  [ ] stdlib suite 11/11
  [ ] bootstrap chain compared against the unmodified baseline
  [ ] wasm32 target builds and caches under its own address
```

---

## 5. Cache policy (small, decided, and in one place)

- **Eviction:** none by default. The cache is a directory; operators delete it.
  If a size cap is ever added, LRU over `(addr.json).mtime`, and never evict the
  artifact of a package in the current build graph.
- **Invalidation:** by address, which means by content. There is no `-a` semantic
  other than "ignore the cache". Keep `-a` as the explicit override and make it
  the only way to bypass, so that "it built differently with `-a`" is always a
  determinism bug rather than a mystery.
- **Sharing:** an artifact is valid for any tree that produces the same manifest.
  Paths are relative, so two checkouts share a cache. Do not include the user, the
  host or the time in the manifest.
- **Provenance:** the `.json` exists so that a stale or suspicious entry can be
  explained without a debugger. It is never read by the compiler.

---

## 6. What this deliberately does not do

- **No per-phase caching inside a package (yet).** Storing typecheck output
  separately from codegen output would let a codegen-only change skip the
  typecheck. It is a real win, and it is *stage B*: it needs a stable interface
  between the two halves, and that interface is exactly the export-data format
  this stage is still stabilising. Land stage A first, measure how much of the
  remaining time is typecheck, then decide with a number.
- **No distributed or remote cache.** The address scheme is ready for it. The
  transport is not this task.
- **No memory-model change.** Named twice because it is the temptation: a
  bounded-memory compiler looks like a case for a store, and it is not. The bound
  comes from the process boundary. If it ever comes from a store, D25 has been
  violated somewhere.

---

## 7. Definition of done

A clean checkout, an empty cache, and:

```
moxie build ./...          # cold: works, memory bounded per package
moxie build ./...          # warm: zero parse/typecheck/emit, same bytes
```

with the three goal properties printed in the build summary — peak RSS per
package, operations skipped, artifact addresses — because a property that is not
printed will not be maintained.

---

## Appendix A — Facts already established, so they are not re-measured

| Fact | Where |
|---|---|
| 137M 32-byte allocations in one non-root arena, ~5 GB VA | 22.4e |
| seen-set reached 262,144 and climbed | 22.4e |
| seen-set cap correctness fix, 2^21 | 22.4e, `f8361251` |
| region table now chains for every arena | 22.4e, `1374d8fd` |
| `TypeCheckOnly` frees its per-package arena and returns by deep copy | 22.4e |
| stdlib suite 11/11 on the checked-in generation | 21.3 |
| wasm build of the self-hosted compiler completes | 21.3 |
| C0 is reproducible; C1+ panics in `codecDecodeValue` | 21.3 |
| `pkgHash` already includes `nm1|` for `-no-main-arena` | 21.3 |

## Appendix B — Open questions the implementer must decide and record

1. **Who owns the worker pool.** The parent (a scheduler over children) or the
   shell (`xargs`-style). Recommendation: the parent, because it already holds
   the dependency graph.
2. **Whether `legacy/` produces or only consumes artifacts.** Producing from both
   is the ideal; consuming only is acceptable for stage A if it is stated.
3. **Whether the `.o` and `.mxh` share an address or have separate ones.** They
   are produced by one invocation but consumed differently. Recommendation:
   separate addresses, derived from the same manifest plus a kind tag, so that a
   `.mxh`-only consumer does not fetch an object file.

---

## 8. Execution plan: synchronous domains, not processes

Sections 1-7 state the goal and the reason. This section fixes the MECHANISM,
because the mechanism in section 1 ("one package per process") is not the one
this codebase can implement, and it was written before the measurement below.

### 8.1 What already exists (verified, not assumed)

| piece | where | state |
|---|---|---|
| worker threads, not processes | `host.c:452` `mxc_spawn_worker` -> `mxc_spawn_thread` -> `pthread_create` | in use, 7 workers |
| job/result transport | `main.mx:9513` `cRingCreate(262144)` pairs, `ringSend`/`ringRecv` | in use |
| TLS package globals per domain | `host.c:469` `mxc_detect_tls` probes a global write in a thread; `-j 1` fallback | in use |
| per-domain heap + arena stack | `workerEntryExport` -> `runtime.InitCShared()` | in use |
| export data written to disk | `TypeCheckOnly` -> `generateMxh` -> `writeFile(mxhPath)` | in use |
| export data read from disk | `loadMxh` for dependencies | in use |

So the isolation the plan needs is not a runtime feature to build. It is the
worker pool that already compiles objects. The work is to move ONE PHASE into it
and to delete the in-memory return.

### 8.2 The correction, and what it costs

A domain is a thread in one address space. Therefore:

- The memory bound does NOT come from the process dying. It comes from two
  places: (a) the worker's arena is reset between jobs, so the previous package's
  graph is gone before the next job starts; and (b) the parent's universe is
  built per package from that package's DIRECT imports read off disk, so no
  package ever sees every package compiled so far.
- Crash containment is NOT available. A Moxie `runtimePanic` is death and the
  thread shares the process, so a panic in a worker kills the build. This is the
  one property a kernel process would give that a domain cannot, and the
  mitigation is the existing discipline: a worker reports `reportCompileError`
  as a status word over the ring, never as a panic.
- What is gained over fork/exec: no `exec`, no re-mapping of the binary, no
  re-reading of sources to warm a fresh process, and the pool amortises the
  domain init (64 MB stack, `InitCShared`, package initialisers) across jobs
  instead of paying it per package.

The property that actually fixes the 137-million-allocation failure is neither
the thread nor the wait: it is that **the result crosses as a file, not as a
return value**. A returned `*TCPackage` is what walks Pkg -> Scope -> every
object -> universe. A `.mxh` on disk is a byte string; its lifetime is the
filesystem, and the codec is never asked to relocate it.

### 8.3 The four gaps, in the order they must close

1. **Typecheck is not in a worker.** `prepareDispatchIndex` ->
   `typeCheckPkgSource` -> `TypeCheckOnly` -> `typeCheckPkg` run in the parent.
2. **The result returns in memory.** `TypeCheckOnly` writes the `.mxh` AND calls
   `loadMxhInto(mxhData)` into the parent's universe. The second call is the copy.
3. **The parent accumulates one universe.** `ImportRegistry` and `u.Scope` grow
   across the whole build, so each return carries every package compiled so far.
4. **Errors must be values.** See 8.2; already the compiler's style, must be
   enforced on the new job type.

### 8.3b Step 0, measured first: the tree cannot currently rebuild its own compiler

This was measured before any code was written, and it changes the order of work.

| compiler | built by | hello build | verdict |
|---|---|---|---|
| `./mxc-stage4` (checked in, Nov 12 20:02) | - | **37 s** | works |
| sources at `334a6714` (Nov 12 19:33) | `./mxc-stage4` | abort at once | `panic: codec bad pointer tag` |
| sources at `492a65aa` (Nov 12 23:10, the commit that was supposed to fix that panic) | `./mxc-stage4` | 118 s | `SIGSEGV` |
| sources at HEAD | `./mxc-stage4` | > 400 s | stalls in `internal/bytealg` typecheck |

So the checked-in generation is **better than anything these sources can now
produce**, and every source commit since Nov 12 20:02 makes a rebuilt compiler
worse. The checked-in binary predates the change that folded the ownership types
and the accumulated universe into what a return must carry.

That is the same defect 22.4e chased, seen from the other side: it is not merely
slow, it is **codec desynchronisation on the universe graph**. `492a65aa` moved
where the ownership types are installed, which stopped the immediate panic and
turned it into a segfault 118 s later -- a guard-shaped fix that moved the
failure instead of removing it, exactly the pattern `claude.md` warns about.

The segfault at `492a65aa` has a backtrace, so it is not a desync after all:

```
#0 runtime[arenaBump]        <- the crash lands here
#1 runtime[alloc]
#2 runtime[codecEncodeValue]
#3..#N runtime[codecEncodeValue]   (recursion, hundreds of frames)
```

`codecEncodeValue` recursing until the stack runs out, allocating a copy at every
level. That is the 137-million-allocation walk seen from the inside: the encode
does not terminate on the type universe graph, because every descent COPIES and
the copy has a new address, so the seen set never matches and the same subgraph
is expanded again. A larger cap and a hash index both fail for the same reason:
they make each step cheaper while the walk itself is unbounded.

The consequence for this plan is that **the domain boundary is not only a
performance fix**. With the artifact as the only channel, the universe graph
never crosses a return, so the codec is never asked to encode it, and this entire
failure class disappears rather than being tuned. That is why Step 2 comes before
any attempt to make the current path faster.

### 8.3b2 The crash site, named: a returned `syntax.Expr` whose slot holds a slice header

Measured with a compiler built from **clean HEAD** by the checked-in binary, so
none of the uncommitted work is in it, plus a temporary print at the top of the
codec's interface case (`typ`, the interface's `typecode` and `value`, and the
two transient flags):

```
ci typ=0x...20f0c0 td=0x...21c250 v=0x7f...  rt=1 to=0   <- pointer:syntax.CallExpr
ci typ=0x...20f0c0 td=0x...21cab8 v=0x7f...  rt=1 to=0   <- pointer:syntax.ExprStmt
ci typ=0x...20f0c0 td=0x7f316e095730 v=0x0000000000000003 rt=1 to=0
     ^ pkg/syntax.Expr  ^ a heap address, not a typecode   ^ 3
```

`nm` names the descriptors: `0x20f0c0` is `reflect/types.type:named:pkg/syntax.Expr`,
`0x21c250` is `...pointer:named:pkg/syntax.CallExpr`, `0x21cab8` is
`...pointer:named:pkg/syntax.ExprStmt`. So the first two encodes are ordinary
`Expr` returns; the third is handed a 16-byte slot holding `{heap pointer, 3}` -
the first two words of a **slice header**, not an interface - and dies in
`codecEncodeValue` reading the pointee.

That is not a corrupted typecode; it is the wrong 16 bytes. The frame arena that
held the interface's pointee was released and the region reused for a slice
before the return protocol walked it - `FnArenaPop` hands the region to the next
callee, which is exactly what the arena-escape warning is about. The interface
itself is `raw` in `typeAccess`, so a codec IS emitted for it; what failed is the
address judgement inside the codec, not the decision to encode.

Two controls, so the finding is not attributed to the wrong change:

| compiler | sources | result |
|---|---|---|
| `./mxc-stage4` (Nov 12) | anything | builds and runs apps |
| built from clean HEAD by `./mxc-stage4` | HEAD, nothing uncommitted | `SIGBUS`/`SIGSEGV` at `tc-in` of `internal/task`, `math`, `mxutil` |
| built from `0d1f6ddf^` (pre-dates every commit since Nov 13 20:46) | that commit | `SIGSEGV` at `tc-in internal/task` |

So the rebuild loop has been broken since before Nov 12 20:02, which is what
8.3b already concluded; what is new here is the *shape* of the fault. It is also
why `graphNeedsInit`/`computeGraphNeeds` were wrongly suspected: instrumented,
they run to completion (`cur == root` throughout, 17/17 packages), and the crash
is one package later, inside typechecking a package whose source was just
parsed.

### 8.3c What the codec cannot do, measured

Two codec-level answers were tried and both are now closed off with numbers.

**Publish instead of return** (done, `39010786`): the typecheck result travels
through the context rather than as a returned `*TCPackage`. This removed the
largest single walk and is correct.

**Alias the phase arena** (tried, reverted): with the codec treating pointers into
the phase arena as stable, the encode for one package dropped from **7,412,272**
entries to **280,513** -- a 26x reduction, and the wrong thing to do. The
typecheck arena is FREED at the end of the phase, so aliasing stores pointers
into released memory in the parent's registry, and the build then stalls
indefinitely a few packages later: measured, ENCS stops printing after
`mxregexp` and the build runs until its 900-second timeout.

That result is the important one, because it says the boundary is not negotiable:
a pointer that crosses from the phase to the parent must be either COPIED (the
walk) or backed by an arena that outlives the parent (the accumulation). Both are
the failure. The only third option is that **nothing crosses**, which is the
artifact: the parent re-reads the `.mxh`. That is Step 3, and it is now the only
remaining path rather than a preference.

### 8.3d Every codec-level option, measured and closed

Five attempts, each built and run against the same 37-second baseline (the
checked-in `mxc-stage4` building a hello):

| attempt | result |
|---|---|
| raise the seen-set cap to 2^21 | still stalls; the walk is unbounded, not throttled |
| O(1) hash index over the seen set | still stalls past 600 s at `internal/bytealg` |
| publish the typecheck result through the context (kept, `39010786`) | 37 s once, then nondeterministic: fast / segfault / stall |
| alias pointers into the phase arena | encode 7.4 M -> 280 k entries, then a permanent stall: the arena is freed |
| parse the export text into the ROOT arena | no change |

A gdb sample taken during the stall of the last two lands in `codecSeenLookup`
from `codecEncodeValue`, during `internal/bytealg`'s typecheck - the largest
package in the runtime closure. So the remaining cost is a genuinely enormous
walk, and every mechanism that could make it cheaper or skip it has been tried and
measured.

**The fault is a dangling interface pointer, not a deep stack.** With the fatal
signal handler printing the faulting stack (and `sysMapDie` naming oversized
mappings), the rebuilt compiler's crash resolves to one instruction:

```
runtime.codecEncodeValue+0xb9:  movups (%r12),%xmm0     <- r12 is unmapped
    -> alloc(0x10)
```

Sixteen bytes, copied to the stack and then allocated: an interface. The type
descriptors adjacent on the stack are `pkg/syntax.Expr` and `runtime._interface`,
so what the return protocol is encoding is an AST node reached through an
interface whose value pointer names memory that is gone. The same walk under a
memory ceiling instead of a stack limit shows the other face: `mxutil.Ensure`
growing a `[]byte` by doubling until the arena opens 64 MB regions, because the
encode's buffer tracks the size of what it walks.

**Step 3's first half, measured.** Removing `loadMxhInto(mxhData)` from
`TypeCheckOnly` - so the parent keeps only the mxh text on disk - advances the
build past the packages that used to die immediately (`mxutil`, `mxregexp`, `io`,
`internal/cpu`) and into `math`, where the ceiling still fires. So the publish
was one of the crossings, not the only one. The stack at the ceiling is the other
half in its purest form:

```
mxutil.Ensure[byte] <- arenaBump <- alloc <- codecDecodeValue <- codecWriteRaw
```

`codecWriteRaw` writes a block whose length came from the value being encoded,
and `Ensure` grows the codec buffer to that length: a dangling slice length makes
the encoder copy gigabytes. Nothing about the buffer is wrong; the number it was
given is.

One defect, two symptoms, and both are the boundary in 8.3b: something
graph-shaped is crossing a return. Where the stack exhaustion ends and the
dangling pointer begins is not yet separated, which is why Step 3 - nothing
crosses - is the next move rather than a narrower fix.

The one option not yet taken is the architectural one, and it is Step 3: the
parent must not hold a universe built from every package compiled so far. That is
not a tuning change, it is a change to what the parent keeps, and it is the
remaining path.

### 8.4 Steps, each with its acceptance

Step 1 - **Measure the boundary before moving it.** No code changes.
Record per package: wall time, peak RSS, `.mxh` bytes written, and the size of
the type graph at return (the existing `memLine` counters plus the seen-set
number from 22.4e). Run the same build with `-j 1` and `-j 7`.
Accept: a table, one row per package, that names the packages dominating time and
memory. Nothing else.

Step 2 - **A typecheck job type.** Add "typecheck package P, write `<addr>.mxh`,
report `{status, addr, bytes}`" to the worker's job switch, next to the existing
object job. The parent dispatches it and waits for the status word.
Accept: the `.mxh` files are byte-identical to the ones `TypeCheckOnly` writes
today for the same sources (compare with `sha256sum` over the cache dir), and a
deliberately broken package reports an error word instead of killing the build.

Step 3 - **Delete the in-memory return.** Remove `loadMxhInto(mxhData)` from the
typecheck path; the parent loads a dependency's exports from its `.mxh` when the
dependent is dispatched, and drops them when that package finishes.
Accept: peak RSS for the worst package stops growing with the number of packages
built, measured across three tree sizes (hello, a mid-size package, the
compiler). A second build with a warm cache reports zero parse/typecheck/emit.

Step 4 - **Address the artifact.** The `.mxh` (and later the `.bc`) path is
`H(normalised sources, ordered import addresses, target, flags)`. Nothing else -
no commit, no timestamp, no user, no host.
Accept: two builds of the same tree in different directories produce byte-
identical artifacts; a `-j 1` and a `-j 7` build produce byte-identical
artifacts; changing one leaf package rebuilds only its dependents.

Step 5 - **Bound the workers.** `-j` caps worker count; each worker resets its
arena between jobs; print per-worker peak RSS in the build summary.
Accept: peak RSS per worker is a function of the largest single package, not of
the program, and it is printed.

Step 6 - **Acceptance.** `moxie build ./...` cold, then warm, on a clean
checkout with an empty cache. Cold works and is bounded; warm does no
parse/typecheck/emit and produces the same bytes. `morly` and `smesh` build and
behave as they do today.

### 8.5 Risks, and the check that catches each

| risk | caught by |
|---|---|
| a worker panic kills the build | Step 2's broken-package acceptance; every worker path returns status, none panics |
| the parent still accumulates (gap 3 half-closed) | Step 3's three-tree-size RSS check |
| non-determinism from map iteration or worker order | Step 4's cross-directory and cross-`-j` hash comparison |
| export format drift between parent and worker | Step 2's byte-identical `.mxh` comparison |
| the pool hides a shared-state bug | `mxc_detect_tls` already probes globals; keep the `-j 1` fallback and run the suite both ways |
| wood law | this changes the compiler's PROCESS STRUCTURE, not emitted code: stage4-only, per the scope clause in `claude.md`. The legacy compiler stays the bootstrap and must still build this tree |

### 8.6 Rollback

Every step is behind the existing cache: `-a` bypasses it, and `-j 1` serialises
the pool. If a step misbehaves, `-j 1 -a` is the reference path and must keep
working at every commit. A step that breaks `-j 1 -a` is not landed.

### 8.7 What "done" prints

The build summary prints, for every package: the artifact address, whether it was
reused or rebuilt, wall time, and peak RSS; and for the build: workers used,
artifacts written, artifacts reused, peak RSS per worker. A property that is not
printed is not maintained.
