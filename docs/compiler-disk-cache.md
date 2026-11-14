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

The reading is exact, not approximate: a `[]Expr` header is `{ptr, len, cap}`, and
`{0x7f316e095730, 3}` are its first two words. So the encoder was handed the
address of a **slice header** while the type it was told to encode is `Expr`.
Either a struct field descriptor puts `Expr` where the real layout has a slice,
or a slice-element walk used a stride that is not the element size. The two
valid encodes immediately before it prove the descriptor walk itself works for
`Expr` fields of `CallExpr` and `ExprStmt`, so the fault is one specific
descriptor or one specific stride - not the decision to encode, and not
`computeGraphNeeds` (instrumented: it completes, `cur == root` throughout).

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

### 8.3b3 Post-mortem on the core: what is corrupt, and what is ruled out

`systemd-coredump` keeps every crash, so the fault is read post-mortem instead
of guessed at (`coredumpctl dump <pid> > core`, then `gdb -batch -ex bt`). That
works on this machine even though `gdb` attached to a live inferior in cgroup
reclaim hangs - nothing is running to interrupt.

```
#0..#3 runtime[codecEncodeValue]      (4 levels)
#4    moxielang.dev/pkg/types[NewTCVar]
#5    main[ScanGenericDecls]
#6    main[TypeCheckOnly]
#7    main[typeCheckPkgSource]
#8    main[prepareDispatchIndex]
```

At the fault, `typ` (`$rbp`) is `reflect/types.type:named:pkg/syntax.Expr` and
the slot address (`$rdx`) is `0x100000` - exactly `codecBadPtrFloor()`, which
the guard tests with `<` so a pointer AT the floor passes. One frame up the
call site is the slice-element loop (`ep = sl.ptr + i*elemSize`), and the slice
it walked is `p=0x10 l=1003030640 c=1`, where `l` is the low half of the
node's own address. So the AST node and the type-graph objects below it are
reads of memory that has been reused.

Byte-level findings, each checked rather than inferred:

| question | answer |
|---|---|
| Is the struct descriptor wrong? | No. `pkg/types.Named` = size 96, fields at 0/8/24/48/72, matching `{Obj, Under, Methods, TParams, TArgs}` exactly. `syntax.CallExpr` = size 104, `ArgList` at 16, matching `{Fun, ArgList, HasDots, expr}` where `expr` embeds `typeAndValue`. |
| Is the typecheck arena freed too early? | No. Leaking it (`if false { ArenaFree(arena) }`) leaves the crash identical; RSS rose 11 MB -> 1850 MB and nothing else changed. |
| Does the `Named.Under = under` store land? | Yes. Re-reading the field immediately after the store compares equal every time (`0` mismatches in a full run). |
| Which object is corrupt? | A `Named` whose `Under` reads `{0x7fdf4e7f2e10, 10}` and whose `Methods`/`TParams`/`TArgs` are nil - i.e. `{heap pointer, 10}`, first two words of a slice header, in a field declared `Type`. |

The corruption is therefore upstream of every store this code performs, and it
arrives in a value the codec produced: the decoder wrote a slice header where a
16-byte interface belongs, which is either a tag-stream desync (the decoder is
a field behind and writes each value into the wrong slot) or a slot size smaller
than the write. That is the same class as the 8.3b `movups` fault, now with the
object named.

One fix did land on the way, and it is independent of this: `ScanGenericDecls`
re-parsed a generic package with `SetCurrentArena(genericArena)` and no manual
mode, so every parser `return Expr` relocated its copy into the callee's frame
while the AST holding a pointer to it lived in `genericArena`. `FnArenaPush`
ignores `SetCurrentArena` and pushes a fresh frame, so those copies died with
the method. It now uses `ManualArenaEnter`, like the probe parse above it. The
crash moves (from the AST encode to `NewTCVar`), which is why it is recorded
here as a real fix and not as the fix.

### 8.3b4 Nothing is freed, so the corruption is a WRITE

Every "stale pointer" explanation was tested and each one is now closed:

| experiment | result |
|---|---|
| leak the typecheck arena (`if false { ArenaFree(arena) }`) | crash identical; RSS 11 MB -> 1850 MB |
| never compact (`SovDrainCompactions` returns 0) | crash identical |
| never unmap any arena (`ArenaFree` returns at entry) | crash identical; RSS 1905 MB |
| detect a phase-arena rewind (highest `a.alloc` per arena, printed on a drop) | 333,292 rewinds, **every one `manual=0`** - all of them pooled frame arenas being reset on acquire, which is normal |
| does the emitted epilogue release the phase arena (`FnArenaPush` returns it in manual mode)? | never fires; the epilogue releases `FnArenaPop`'s result, which is nil in manual mode |

`arenaBump` memzeroes every allocation and only ever moves its offset forward
within a live arena, so with nothing freed and nothing rewound the bytes at the
corrupt field were **written there**, not inherited. The corrupt object is a
`Named` whose `Methods` (`[]*TCFunc`, at +24) reads `{10,
named:pkg/types.ConstInt, <heap address>}` - which is exactly a `TCVar`'s tail
(`{Name.cap, Typ.typecode, Typ.value}`). The codec was told
`{typecode = *Named, value = <address of a TCVar>}`: an interface whose two
halves do not describe the same object.

The encode/decode pair was also instrumented for desynchronisation - every
pointer-returning call now checks, after its decode, that `cb.pos` consumed the
whole encode (`codecCheckConsumed`, temporary). Zero desyncs in a full run, so
the two sides walk the same field stream and the decoder never wrote into the
wrong slot.

The object has since been NAMED from the core. Its `Name` field is a
ten-byte string, and the bytes at that pointer read `"allocSites"`:

```
pkg/mxutil/mxutil.mx:140   var allocSites allocSiteTable
```

So the corrupt object is mxutil's own package variable, whose `Typ` reads
`{typecode = *types.Named, value = <the TCVar's own address>}` - a Type that
points at the variable holding it. Nothing was freed, so that value was
written or resolved there, and the only place such a self-reference can arise
is the `Named.Obj <-> TypeName.Typ` cycle: the encoder registers a struct's
base and then its fields, the decoder mirrors it position for position, and a
back-reference index that lands one entry off resolves to the enclosing
object. The next probe is therefore the seen set itself: print
`(index, address)` for every `codecSeenAdd` and `codecDecodeSeenAdd` around the
cycle and compare the two sequences, which is a local, decisive check.

So the interface header itself is the fault, and the leading hypothesis is a
static-type/typecode disagreement at the box - the compiler believes the value
being boxed is `*Named` when the pointer it stores is a `*TCVar`. The next probe
belongs there: print the concrete `SSAType` at the boxing site that produces the
interface stored into `TCVar.Typ` / `TypeName.Typ` and compare it with the
object actually passed.

### 8.3b5 The self-typed variable, fixed and verified

`NewTCVar` was instrumented to compare the data word of its `Type` argument with
the address of the TCVar it is about to return. It fired on exactly one caller,
mxutil's `allocSites`: the Type handed in was already pointing at the address the
new variable was about to occupy, so the variable was typed at itself and the
codec read its `{Pkg, Name, Typ}` fields as a `Named`'s.

The cause is lifetime, not arithmetic. `ScanGenericDecls` re-parses a generic
package into `cctx.genericArena`, but it only held that arena as the manual
phase for the parse; the type objects built immediately afterwards - one
TCPackage, one object per declaration - were allocated in whatever frame was
current, from Types the original function also produced. An address in that
outer frame could be reused for a new object while the graph still named it.

Holding the phase for the whole function fixes it. Verified:

| | before | after |
|---|---|---|
| `SELF-TYP` reports in a full run | 1 (`allocSites`) | **0** |
| `mxutil` type-check | SIGSEGV in the codec | **`tc-out` at rss=50 MB** |
| next failure | - | `math`: 3.2 GB live at 54 MB resident |

That last row is the next wall and it is the one `docs/compiler-disk-cache.md`
8.3c calls the encode storm: `codecWriteRaw`/`codecWriteVarint` copying the type
graph at returns until the arena ceiling stops it. It is a separate defect from
the one fixed here, and the tree already carries its plan (do not let the graph
cross a return; the published `tcPkg`/`tcFile` path plus Step 2's domain
boundary).

The wall is not a legitimate appetite. Raising the in-process ceiling from 3 GB
to 3.4 GB moved the stop from `live 3252834304` to `live 3588509696` - it does
not plateau at 3.2 GB, it keeps going, so `math` is in an unbounded allocation
loop and the ceiling is only what stops it first. The stack is the same every
time:

```
mxutil.Ensure[byte] <- alloc <- arenaBump <- arenaOpenSegment
                    <- codecWriteRaw <- codecWriteVarint <- codecRetStableInner
```

That is the encode storm exactly as 8.3c describes it: a decode produces a copy,
the copy has an address the encoder's seen set has never seen, so the next
descent encodes the copy, and the walk grows instead of terminating. Confirmed
by the one control that matters: with `math` reached, `mxutil` and
`internal/cpu` now complete (they never did before the phase fix), so the storm
is the *next* defect, not a consequence of the one just fixed.

Two candidate causes were then measured and BOTH are wrong, which is why this
one is recorded as open rather than attributed:

- **Not a huge source.** `stripUnusedConsts` was instrumented with `len(src)`:
  math's concatenated source is 105,385 bytes, and every earlier package is
  smaller. The coalescer is not over-allocating.
- **Not a growing encode buffer.** Every `codecEncodeValue` prints its type
  descriptor once `cb.pos > 1 MB`. Nothing printed before the ceiling killed the
  run, so no single encode ever wrote a megabyte.

What the post-mortem does say, from the core rather than from the stack print:

```
#8  main.moxielang.dev_pkg_mxutil.Ensure__byte
#9  main[stripUnusedConsts]
#10 main[typeCheckPkg]
#11 main[TypeCheckOnly]
```

`stripUnusedConsts` presizes its output to `len(src)` and copies line by line,
so a 105 KB input cannot make it allocate gigabytes - yet the allocation that
dies is reached through it. That is where the next session starts: the frame
above `Ensure[byte]` is unreliable because the binary carries no unwind info,
so instrument `mxutil.Ensure`'s callers or the arena in question
(`arenaOpenSegment` printing the arena, its `alloc` and `nregions`) rather than
trusting the printed chain.

### 8.3b6 The math runaway is manual-mode accumulation

Segment instrumentation settled it. Every segment opener printed its arena,
scope, sovereign/root flags and running total; ONE arena accounts for the whole
3 GB:

```
SEG arena=0x...8cf9000 scope=0 sov=0 root=0 size=105392 nreg=44 alloc=2760999568 r0cap=5271552
SEG arena=0x...8cf9000 ...        nreg=45 alloc=2828098016
...                                nreg=49 alloc=3096314880
```

`r0cap = 5271552` is `len(math's source) * 50`, i.e. the per-package typecheck
arena (`arenaSize := uintptr(len(src)) * 50` in `TypeCheckOnly`). It reached
3.09 GB over 49 segments.

And the allocation is not one big thing:

- `stripUnusedConsts` is called **once** for math (instrumented: a single
  `len=105385`), and its output is presized to the input.
- Large raw codec writes are bounded: 20 writes above 40 KB in the whole run,
  the largest 6,814,688 bytes (an `.mxh` read), summing to about 7.9 MB.

So the 3 GB is tens of thousands of small allocations, each retained. The
reason is the mode the typecheck runs in: `TypeCheckOnly` enters the arena with
`ManualArenaEnter`, and in manual mode `FnArenaPush` returns the phase arena
instead of pushing a frame, so **no scratch is reclaimed for the whole
package**. Peak becomes the sum of every allocation the typecheck ever makes.
The codec's return decoding must target that arena (it is where the AST and
type objects live), so manual mode is load-bearing for correctness - which is
what makes this a design boundary rather than a bug to patch: object memory
needs the phase arena, scratch needs frames, and the two must be separable.
That is the same boundary 8.3c's plan draws.

The size distribution says it is a loop, not retention. Sampling every 8192nd
allocation once the phase arena passes 200 MB:

```
122 samples of 16 bytes, 109 of 32 bytes, 19 larger (25 KB .. 105 KB)
```

231 of 250 samples are 16 or 32 bytes, at a running total of 3.09 GB - that is
on the order of a hundred million tiny allocations during the type-check of a
105 KB package. A package that size cannot legitimately need them, and nothing
in the pass is called often enough to make them one-per-iteration (the const
stripper runs once, the large raw codec writes total 7.9 MB). So something is
looping.

Two fixes were tried on the way and both are measured, not assumed:

- **Interface fields decoded in place** (`codecDecodeInto` handling
  `KindInterface` itself instead of allocating a 16-byte temporary per field):
  plausible, since 16 bytes is exactly an interface, but it did NOT move the
  wall (3.254 GB vs 3.253 GB). Reverted - an unverified change to the hottest
  decode path is not worth landing.
- **Registering interface pointees in the seen set on both sides** so cycles
  that close through an interface edge are detectable: no effect. Reverted, for
  the same reason plus the table-alignment risk.

Next session should find the loop rather than treat the symptom: the sampled
sizes are 16 and 32 bytes, which is a two-word and a four-word allocation, so
instrument `alloc` to record the *calling* function (the emitter can pass a
site id) or bisect by disabling one typecheck sub-pass at a time.

Two more candidates are now closed by measurement, and both were worth the
build:

- **The parser is not looping.** `operand` was instrumented with a per-parse
  call counter that reports at 100k, 1M and 4M: it never printed, so a 105 KB
  source does not enter `operand` a hundred thousand times. The parser frames in
  the stack sample are noise from an unwinder with no CFI.
- **The seen-set re-growth is not the accumulator.** `codecSeenInit` reset the
  table to the 256-entry static array every encode, so each encode re-grew it
  from 256 entries to the graph's size and abandoned every growth buffer. A
  process-wide backing for the table changed the total by 0.6% (3.258 GB vs
  3.253 GB). Reverted - it is a real inefficiency, but not this one.

The reliable next instrument is stated here so it is not re-invented:
`runtime.alloc` takes `(size, layout, td)` and every emitter call site passes
`ptr null` for `td`, so an allocation carries no identity at all. Giving the
emitted calls a **site id** (an i32 literal, or the type descriptor the emitter
already knows) makes the tiny allocations name themselves, and that is the
change to make before attempting the wall again. `returnAddress(1)` inside the
allocator is NOT usable for this - it faults, because the allocator's frame does
not match what the intrinsic expects; measured twice.

### 8.3b7 The wall moves: math is fixed, the rewrite passes are not

Per-phase arena marks (a `tcMark(tag, arena)` after each rewrite) turned the
"somewhere in the typecheck" wall into a list, and the first entry was a real
bug with a clean fix:

| phase (105 KB package) | arena |
|---|---|
| `enter` | 0 MB |
| after `stripUnusedConsts` | 336-801 MB |
| after the five source rewrites | +3 MB |
| after the parse | +4 MB |
| after the eight declaration passes | +0 MB |

So the const stripper was allocating hundreds of megabytes and the typecheck
itself almost nothing. It built its output with `out = mxutil.Ensure(out, 1);
push(out, c)` **per byte** while the buffer was already presized to `len(src)`;
rewritten as a cursor write (`e73f3efe`) the typecheck passed `math` and `moxie`
for the first time and stopped at `runtime`.

The same instrument then pointed at `RewriteSliceMakeLiterals`: 31 MB -> 1829 MB
for the `runtime` package's ~500 KB source. Inside that function:

- `LIndex(src[i:], pattern)` copied the remaining source per iteration - fixed
  with an offset scan (`bb479835`), worth only 15 MB, so it was not the term.
- The interior of each literal was taken as a slice and trimmed by further
  slices. Rewritten with index arithmetic: no change.
- `findSliceTypeStart` scanning back to the file start and `findMatchingBrace`
  forward to the block end, per match. Bounded to 8 KB: no change.
- A reversed range (`src[i:lbrack]` where the bracket precedes `i`) suspected of
  producing an enormous length: guarded, no change.

All four are reverted except the offset scan, which is worth keeping on its own
terms. The 1.8 GB is still inside that pass and the next step is a per-iteration
arena delta inside it, not another guess.

**The pattern behind all of this** is worth stating because it is the fastest way
to find the rest: this tree has several loops that are quadratic *because a
sub-slice is a copy* or *because Ensure+push runs per element*. Two are fixed
(cursor write, offset scan), a third is measured but not yet located, and a grep
for `X[a:]` inside a loop body - or `Ensure` immediately before `push` in a
per-byte loop - would find them all at once.

### 8.3b8 The slice pass, and two dangling universes under it

The 1.8 GB in `RewriteSliceMakeLiterals` was not a scan and not a sub-slice
copy. It was the segment list: every `chunks = mxutil.Ensure(chunks, 1)` RETURNS
a pointer-containing slice across a function boundary, so the return protocol
walks and copies every segment accumulated so far - once per push. Quadratic by
construction, and invisible in the source because the growth is in the return
protocol rather than in the buffer. Measured on `runtime` (519 KB): 32 MB ->
1.92 GB inside that one function; `io` (32 KB): 1.5 MB -> 35 MB.

Rewritten as two passes and one buffer (`c319641c`): pass one totals the exact
output length from a scan that allocates nothing, pass two writes through a
cursor. `runtime` now costs 32.7 MB -> 35.3 MB, and every package's rewritten
source has the same length as the old pass produced (`runtime`: 519626 bytes
either way). All 34 packages of the tiny app now reach `tc-out` with 0 parse
errors, where the run previously died inside `runtime`.

The instrument that found it is worth repeating: a per-phase arena mark
(`ArenaRootUsage()` plus `ArenaUsed(CurrentArena())`) around each rewrite, then
per-match counters inside the pass. The clue that the segments were the term,
not the scan, was that the byte count of the output was linear while the arena
was not.

**Two dangling references surfaced the moment typecheck stopped dying.** Both
are the same mistake: a graph that outlives the package it was built for,
allocated in an ordinary fn arena.

- `SetUniverseGlobals` installs `Edge`/`Sender`/`Receiver` after `InitUniverse`
  returns, and installed them in an fn arena. `installChannelOwnershipTypes` is
  a free function with its own frame, so the graph died at the next return while
  the universe scope kept pointing at it. `emit`'s universe walk then read
  `Edge`'s `Obj` out of memory a cache-path string had since been written into -
  the "lengths" it read back were the ASCII of `/cache/` and `mxc-` - and the
  compiler segfaulted in `SafeUnderlying` on the first package to reach IR emit.
- `cctx.scannedGeneric` records which packages have been scanned, so it outlives
  every package, and it was created in `ScanGenericDecls`' own frame. The next
  package's write ran `hashmapSet` against freed buckets.

Both fixed with `ManualArenaEnter(RootArena())`, not `SetCurrentArena`:
`FnArenaPush` ignores the current arena and pushes a fresh frame, so manual mode
is the only mechanism that keeps an allocation in the root (`722960b6`).

**How to recognise this class.** A pointer that outlives its frame reads back as
text: the arena it came from was released and something else - usually a path, a
source file or an error string - was written into the same pages. Print the
lengths, not the strings: `len(x.Name)` is safe on a dangling header, `x.Name` in
a concatenation allocates the garbage length and dies with "implausible size".

### 8.3b9 The current wall: the codec walking compiler state

With the three above fixed, the tiny app's build type-checks all 34 packages,
emits IR for the FIRST package of the emit phase (`array`, `ci-free array`
prints), and dies in the SECOND (`internal/task`) at the 4 GB ceiling.

The package's contents are irrelevant, which is the useful part: reducing
`src/internal/task` to

    package task
    type Task struct { Data uint64 }

still dies, and so does removing its `unsafe.Pointer` conversions. So the walk
is not over the package being compiled - it is over compiler state that the
codec reaches from something the return protocol is relocating.

Two core dumps, two sides of the same walk:

    sysMapDie <- arenaOpenSegment <- arenaBump <- alloc <- mxutil.Ensure__Pointer
      <- codecEncodeValue x20 <- buildExpr <- buildArgs <- buildCall
      <- buildExpr <- buildArgs <- buildCall <- buildExpr <- buildStore ...

    sysMapDie <- arenaOpenSegment <- arenaBump[0m <- sovGrowRecs <- sovAddRec
      <- alloc <- codecDecodeValue <- codecDecodeMapValue <- codecDecodeInto x...

So one run exhausts the arena on the ENCODE side with a 20-deep
`codecEncodeValue`, the other exhausts it on the DECODE side growing a SOVEREIGN
arena's allocation record table (`sovAddRec` per allocation, `sovGrowRecs`
doubling to 256 MB segments). Both are the same disease: a graph crossing the
return boundary whose walk registers millions of pointers.

Two things to look at first, in this order:

1. **`codecDecodeMapValue` in the stack.** A MAP is reachable from the relocated
   value. `*Scope`, `*SSAPackage.Members`, `SSAProgram.packages` and
   `cctx.universe.Registry` are all maps. A returned `SSAValue` is an interface
   whose node carries `Pkg *SSAPackage` and `Prog *SSAProgram`, so if the encode
   follows those the walk is the whole program, and it grows by one package per
   package compiled - which is exactly why the first package survives and the
   second does not.
2. **`codecSeenLookup` is a LINEAR SCAN** over every registered pointer, and
   `codecSeenAdd` grows the table with `Ensure` per pair. So the seen set costs
   O(N^2) time and O(N) arena per encode, and the tables of nested returns are
   all live at once. It is not the cause, but it is the amplifier: 20 nested
   levels each holding a table sized by the walk turns a big graph into 2.6 GB.

`SafeUnderlying`'s `Named` walk is also capped at `depth < 20`, which matches the
20 encode frames and may be the same number seen from the other end.

The known-good `mxc-stage4` compiles the same app in 327 MB, so this is a
regression in this session's sources, not a property of the design. The
regression range is `6fa2e18a..HEAD` plus the working-tree `main.mx`, and the
first suspect is whatever made a map reachable from a relocated value.

### 8.3b10 The comparison that localises it: 147 MB against 880 MB

The known-good `mxc-stage4` and a rebuilt compiler, same app, same `-a`, both
building the same 17 packages one after another:

| | after `array` (pkg 1) | root arena | after pkg 2 | peak |
|---|---|---|---|---|
| `mxc-stage4` (pre-session) | `ci-free array` rss=**147 MB** | **139 MB** | `ci-free internal/task` 150 MB | 327 MB |
| rebuilt (this tree) | `ci-free array` rss=**880 MB** | **9 MB** | dies, 3.2 GB live | 4 GB |

So the rebuilt compiler walks a graph 20x larger than the known-good one *from
the first package*, and it is the **root arena** that differs: the known-good
compiler holds 139 MB of materialised package graphs in the root, and the codec
aliases a pointer into the root instead of walking it - `codecIsStableArenaAddr`
returns true for `rootArenaAddr()` unconditionally, in every encode mode. The
rebuilt compiler keeps 9 MB there, so those graphs live somewhere the codec
treats as a dying frame and walks.

That is the shape to chase, not the package contents: emptying
`src/internal/task` entirely makes the SECOND package fail instead, and the
failure follows the position, not the source. Two experiments are already
recorded as dead ends so they need not be repeated:

- `graphArenaFor` returning `RootArena()` unconditionally: no change, and
  `root` still reported 9 MB - so `loadMxhInto` is not the path that
  materialises these graphs (`mxhg=1` per package in both runs).
- the pre-session `main.mx` (before the graph-arena work): SIGSEGV inside
  `array`'s SSA build at 877 MB, earlier than the working-tree version, so the
  working-tree `main.mx` is the best of the three and should be kept.
- clearing `cctx.scannedGeneric` at the top of `CompileToIR`, on the theory that
  the compile phase was reusing a generic scan whose types belong to the freed
  typecheck arena: identical 3.2 GB OOM, so the reused scan is not the term.
- a 300000-step trace of `codecEncodeValue`'s kind and arena decision, gated on
  `manualArena != nil`: the budget was consumed entirely by the typecheck
  pre-pass before the first `compile`, so it produced no data about the SSA
  build. **Gate the next attempt on the compile arena itself** (for example
  `manualArena == cctx.compileArena`, or a flag the compiler sets around the SSA
  build), not on manual mode being active.

Where the walk enters is `codecInManualArena` and `codecIsStableArenaAddr`:

    codecInManualArena(p)   -> manualArena, ie. the arena ManualArenaEnter pushed
    codecIsStableArenaAddr  -> root | stableArena, and NOT the top fn frame under
                               codecRetTransient

The rebuilt compiler's SSA nodes are allocated in `cctx.compileArena`, which IS
the manual arena and IS the top of the fn stack. `codecInManualArena` therefore
has to win the decision for them; the next measurement is to print the two
predicates for the pointer being encoded at the first return in `array`'s SSA
build and see which one says "transient".

### 8.3b11 The divergence: manualArena != fnStack[fnTop]

The measurement 8.3b10 asked for, and it answered outright. A trace of
`codecEncodeValue`'s kind and arena decision, raised by the compiler around one
package's SSA build and capped at 200 decisions per package, prints four states:
`z` manual arena nil, `n` manual arena is not the top frame, `M` the pointer is
in the manual arena, `F` in the top frame, `S` stable, `x` none of those.
Outside the fix every one of the 137 decisions was `n`.

So `manualArena` and `fnStack[fnTop]` had DIVERGED, and they cannot by
construction - `ManualArenaEnter` sets both, `ManualArenaExit` pops both, and
`FnArenaPush` returns `manualArena` without touching the stack while manual mode
is on. The mechanism is still unlocated; the fix does not depend on it, because
the slot is what matters: `ManualArenaEnter` now marks the fn-stack slot it
pushes in a `DomainRoot.manualFrame` bitmask and `codecInManualFrame` tests a
pointer against every marked slot.

Two facts made that the right fix rather than a rescue:

- only the `KindPointer` branch ever consulted the manual arena, so a STRUCT or
  a SLICE living in the phase arena was walked no matter what;
- the `KindSlice` short-cut is refused during a return when the elements hold
  pointers, which is right for a dying frame and wrong for a phase frame. The
  emitter's `[]string` segment list therefore walked in full on every `Ensure`
  return - O(len) per push, quadratic - which is what killed the third package
  inside `(*irEmitter).w`.

Effect: 1 package emitted -> 5, RSS flat at ~667 MB instead of
880 MB -> 2.6 GB -> OOM (`4938ec8a`).

**Two more measured dead ends, both reverted.** `SovereignRegStable(arena)`
around the SSA + emit phase in `CompileToIR` - the documented "a frame registers
its arena as stable" mechanism, independent of the manual-arena bookkeeping - is
exactly as good as the slot fix and no better: same five packages, same 669 MB.
That is the useful negative result, because it says the slot fix already covers
what the stable registration would cover, and the sixth package's cost is
somewhere neither mechanism reaches. And exempting a phase frame from the
`retPointeeDies` refusal in the `KindPointer` alias decision is also identical
in reach and slower (2m36 against 2m for the same five packages).

**The measurement that names the shape.** Counting `codecEncodeValue` entries
and printing the running total in every `mem` line gives the per-phase volume
for the tiny app's build:

| point | encode calls |
|---|---|
| before `ci-pre array` (the whole typecheck pre-pass) | **23,612,691** |
| `ci-free array` / `ci-free internal/task` | 23,620,176 / 23,639,258 |
| `ci-free internal/bytealg` | 23,680,993 |
| `ci-free internal/stringslite` | 26,182,002 |
| `ci-free math/bits` | 26,806,160 |
| `ci-pre mxregexp` | 29,292,024 |

The emit-phase cost per package is 3k, 15k, 16k, **880k**, **3.2M** - it grows
superlinearly with the number of packages already loaded, not with the package
being emitted (`math/bits` is 25 KB and costs 3.2M encodes; `array` costs 3k).
That is the signature of walking something that the registry accumulates, and it
is why the sixth package is the one that dies: the walk is now over five
packages' materialised graphs as well as its own.

Corrected reading of that table, per package's own compile (ci-pre -> ci-free):
`array` 7,485; `internal/task` 14,988; `internal/bytealg` 41,444;
`internal/stringslite` 501,009; `math/bits` 624,158. Monotonic with the package's
POSITION, not with its size - that is a cost proportional to the number of
packages already loaded, i.e. quadratic in the build, and it is the same shape
the whole session has been finding in buffers and segment lists. `mxregexp` then
dies at three orders of magnitude below what the growth curve predicts for its
position, so the wall is the accumulation, not the package.

**Three aliasing levers were tried against that accumulation and all three are
no-ops**, which is the important negative result:

- `SovereignRegStable(packageArena)` around SSA + emit, plus making an explicit
  stable registration outrank the `codecRetTransient` top-frame veto. The veto
  does run first and does hide the registration, so this was a real ordering
  defect - fixing it changes nothing measurable.
- `graphArenaFor` returning the root arena, so every materialised mxh graph is in
  the one arena the codec always aliases.
- the `manualFrame` slot fix, the one that DID help (1 -> 5 packages), does not
  stop the accumulation either; it only removes the per-return copy of the
  program graph.

So the transfer is not through pointer identity at all: aliasing every arena the
walk can enter changes the cost by nothing, which means the volume is in VALUES
being encoded - slices and structs re-encoded at every crossing - and not in
graphs being re-walked. The next instrument should therefore measure per CALL:
count entries per `codecEncodeValue` and report the calls that exceed a
threshold, with the enclosing function name, instead of counting the phase.

And the last population that could be named is ruled out too: instrumenting
`loadMxhInto` shows all six loads taking the `perkey` path, with mxh blobs of
308, 366, 444, 856, 916 and 1788 bytes. A per-key graph arena therefore holds a
couple of kilobytes of materialised graph, which cannot be what a 3.2M-encode
walk is traversing.

So the volume is inside the package's OWN object graph, reached from the values
the SSA builder and the emitter return. The one structure that grows while a
package is being built - and is reachable from every SSA node - is
`SSAPackage.Members` (via `SSABasicBlock.parent -> SSAFunction.Pkg`) and
`SSAProgram.packages` (via `SSAFunction.Prog`). A walk that costs O(members) at
every return is O(n^2) in the package, which is exactly the shape of the measured
3k / 15k / 16k / 880k / 3.2M: the packages with more and larger functions cost
disproportionately more. The slot fix aliases a pointer whose ADDRESS is in a
manual frame; the question for the next pass is why the `Pkg`/`Prog` pointers
read out of an SSA node are not being aliased by it - `M` fires on 41,919 of
212,000 decisions during emit and `x` on 170,200, so most of them still are not.

The sixth package therefore dies on a graph that is in neither the phase arena
nor the root. The remaining candidate population is the mxh-materialised package
graphs when `mxhPkgKey` returns "" and `loadMxhInto` takes the `loadMxhData`
path instead of `loadMxhDataIn(data, graphArenaFor(key))` - a per-key arena is
neither root nor a manual frame, so the codec walks it. `mxhg` in the mem line
counts only a handful of loads, so this is cheap to check by instrumenting which
of the two paths each load takes.

**A dead end worth recording.** Also replacing `codecInManualArena` with
`codecInManualFrame` at the other three sites - the `KindPointer` alias decision,
`codecRetStableInner`, and the byte/slice header test - HANGS the build: no
package finished in 400 s, where the narrower change does five. Those three sites
are reached with a value whose address is on the stack or in a frame the phase
does not own, and aliasing there leaves the caller reading a slot that is
rewritten. The narrower set is the one to keep.

### 8.3b12 Two counters that moved the wall out of the codec

Two instruments settled the codec question and relocated the wall.

**Encode entries per return.** A counter in `codecEncodeValue` plus one in
`codecSetRetTransient` gives the ratio per package: `bytealg` 1.61M encodes over
39,897 returns, `stringslite` 886k over 39,406, `math/bits` 2.48M over 87,783.
So ~13-40 objects per return - the graph each return walks was never enormous -
across **2.2 million return relocations** in one app build. At a fresh codec
buffer per return that is ~1.5 KB of arena per return and 3.2 GB live, which is
what `9fc0eb33` fixes: `codecBufScratch[:0]` is a COPY, so the "reusable" buffer
had capacity 0 and allocated on every return. RSS after the fifth package
673 MB -> 577 MB.

**The wall is not the codec any more.** With that fixed the sixth package
(`mxregexp`) still dies, but in **IR emit**, after `ci-tc` and `SSA build` - and a
counter in `(*irEmitter).w` shows fewer than 200,000 calls for the WHOLE build,
so the 2.6 GB it allocates is not IR text and not segment growth. `w()` is the
only thing that accumulates text, and it barely runs, which leaves per-instruction
work: `llvmType`/`typeDescName`/`reflective-type` strings recomputed for each
instruction, a few KB each, over a package's instruction stream.

That is the next measurement: count `llvmType` calls and total bytes of the
strings it returns for one package, and cache it by `Type` instead of recomputing.

### 8.3b13 The wall, located: two functions in the emitter

A per-function line in the emitter's `emit()` loop (name, RSS, `len(e.segs)`)
names it. Emitting `moxielang.dev/pkg/mxregexp`:

| function | RSS when it starts |
|---|---|
| `(*parser).classFrag` | 680 MB |
| `(*parser).emit` | 742 MB |
| `(*parser).literal` | 800 MB |
| `(*parser).parseAlt` | 840 MB |
| `(*parser).parseAtom` | **1644 MB** |
| `(*parser).parseClass` | **2785 MB** |

`parseAtom` adds 804 MB and `parseClass` 1141 MB *inside one call each*, and
`segs=0` at every start, so this is NOT the text being accumulated and not
`w()` (which runs fewer than 200,000 times for the whole build). It is the
per-function scratch: `emitFunction` rebuilds `valName`, `nameUsed`,
`allocTypes`, `regTypes`, `blockExitLabel` and `typeCache` on entry and leaves
every one of them in the phase arena, which reclaims only at package end. At a
few hundred bytes per instruction that is tens of MB, not 800 - so one of those
either has an entry per *type expansion* rather than per value, or something in
the per-instruction path re-derives a type string whose size grows with the type.

Next instrument: count the entries of those six maps and the bytes returned by
`llvmType` per function, and run it on `parseAtom` alone.

### 8.3b14 The wall, named: the sovereign record table

A guard that fails loud on any single allocation above 32 MB, plus a core dump,
gives the whole chain:

    arenaBump <- sovGrowRecs <- sovAddRec <- alloc <- codecDecodeValue
      <- types[SafeUnderlying] <- (*irEmitter).emitAlloc <- emitInstr
      <- emitFunction <- emit <- CompileToIR

So it is not user data and not IR text: it is a SOVEREIGN ARENA's allocation
record table doubling to 64 MB, repeatedly - ~40 of those is the 2.6 GB.

The mechanism is the one the memory model warns about, in the compiler's own
hot path. `irEmitter` is a self-mutating type, so `emitAlloc` is a mutating method
and its prologue borrows the emitter's sovereign arena. Everything allocated
inside then lands in that arena and is RECORDED, because the arena has to know
what to compact. And what is allocated there is the return relocation of
`SafeUnderlying`, which returns an interface: the codec decodes a fresh copy of a
type graph into the current arena at every call, so a 31-block function with 226
values causes millions of recorded allocations.

That is why the per-function maps were tiny (val=226, tc=168) and the function
still cost 804 MB, and why every aliasing lever was a no-op: the allocations are
not the walk of a graph, they are copies CREATED by the walk, each one recorded.

The fix the memory model already prescribes is the authoring rule: "a mutating
method's direct allocations land in the sovereign arena and persist; push the
heavy work into a regular helper function". `emitAlloc`/`emitInstr` should call a
FREE function for the type work rather than doing it in the borrow - or the type
accessors should stop returning an interface that the return protocol relocates.
A third option, narrower, is to not record codec-decode allocations in the rec
table, which needs an argument about compaction safety.

### 8.3b15 What the decode is copying, and the size-as-key trap

A kind trace in `codecDecodeValue`, raised by the emitter for the package it is
emitting, gives the population of one app build:

| kind | decodes |
|---|---|
| interface | 413,863 |
| pointer | 399,647 |
| struct | 199,631 |
| bytes | 143,978 |
| slice | 115,382 |
| map | 5,564 |

and the shape inside a single package's emit is

    llll...ipipipipbipipbbbbllll ipipipb llb ll ipipip b...

which reads as a SLICE of INTERFACES each holding a POINTER, with byte strings
underneath - `[]SSAInstruction` and `[]SSAValue`. So what is being copied at every
crossing is the function body's instruction list, reached from an interface
return, and every `p` in that trace is an allocation.

The aliasing rule should cut this at the first phase-arena pointer and does not,
and the size-as-key rule on the seen set is the prime suspect: a back-reference
is honoured only when `entrySize >= pointeeSize`, so one address reached through
two differently-typed pointers (an `SSAValue` interface seen as `*SSAOperation`,
then as `*SSABasicBlock`) is registered once with the smaller size and then
refuses the reference, so the walk re-expands instead of closing. That is exactly
the "a pointer to a SMALLER object at a registered address must stay a
back-reference" rule working against a recursive graph.

That was measured and it is WRONG: a counter on the refusal branch of
`codecSeenLookup` (address matches, registered size too small) never fires once
in the whole app build. The seen set always honours the back-reference it finds,
so the walk is visiting genuinely distinct objects - and there are 400k of them
per package at ~6.5 KB each, which is where the bytes are.

Which leaves the plain reading: the graph really is that large, because an
`SSAValue` node reaches its block, its block reaches the function, the function
reaches `Pkg` and `Prog`, and `Prog` reaches every package. Aliasing is supposed
to cut that at the first pointer that lives in the phase arena, and 400k decodes
say it does not. The next instrument is therefore not on the seen set but on the
alias decision itself: count, per encode, how many `KindPointer` values take the
`codecTagStable` path versus `codecTagPtr`, and print the ratio per package. If
`codecTagPtr` dominates, the pointer being aliased is not the one that matters -
the walk is entering through a struct or a slice header that carries no arena
pointer at all.

### 8.3b16 The seen tables were copies too, and the alias ratio settles it

Two counters on the pointer alias decision in `codecEncodeValue` -
`codecTagStable` (address preserved) versus `codecTagPtr` (pointee deep-copied) -
give the ratio per package, and it is decisive:

| package | aliased | copied |
|---|---|---|
| array | 9,990,711 | 4,412 |
| internal/task | 9,994,379 | 5,109 |
| internal/bytealg | 10,038,651 | 163,576 |
| internal/stringslite | 10,062,990 | 250,697 |
| math/bits | 10,271,749 | 505,089 |

So aliasing WORKS - 10.2M aliased against 505k copied - and 10.2M is the number
to look at instead. Every one of those is `codecSeenAdd` plus a table growth,
and both seen tables were `[256]unsafe.Pointer` reset with `[:0]`: a COPY, so
capacity 0 at every call and a fresh table grown from scratch, ten million times
over, in an arena that reclaims only at package end. That is the same defect as
the encode buffer in `9fc0eb33`, sitting in the two variables next to it, and it
is why the comment claiming "the same 256-entry capacity" was false.

Both tables are now slices that keep their grown capacity, adopted back by
`codecSeenGrow`/`codecDecodeSeenGrow`, which allocate in the ROOT arena so the
capacity survives every phase. RSS after the fifth emitted package:
**577 MB -> 352 MB** (`22347b8b`). The wall is unmoved.

What that leaves for the sixth package: it still adds ~2.9 GB during `mxregexp`'s
IR emit, and it is not text (fewer than 200k `w()` calls for the whole build,
`segs=0` at every function start) and not the per-function maps (226 values,
168 type-cache entries for the expensive function). The next instrument is inside
one `emitFunction`: RSS after the prologue, after every tenth block, after the
epilogue, for `parseAtom` alone.

### 8.3b17 The wall moved three times in one pass: 1 -> 8 packages, flat RSS

Three changes, each measured, each moving the wall to the next package.

**The emitter's sovereign arena had no safe point.** `irEmitter` is a
self-mutating type, so every allocation it makes lands in its sovereign arena and
only compaction reclaims it - and compaction is drained at a `select`, which a
compiler never runs. So nothing the emitter allocated was ever reclaimed: RSS
across the emit phase went 741 -> 1643 -> 2785 MB, ~1.6 MB retained per IR
instruction, while the package's own maps stayed tiny (226 values, 168
type-cache entries). Two changes (`ffc9a5ea`): `CompileToIR` drains between
packages - the safe point `src/web/callback/dispatch.mx` uses on wasm - and
`emit`/the per-function loop became FREE functions, because a group with a live
frame is never compacted and, as a METHOD, `emit`'s own frame was live for the
whole package. That is why the between-package drain alone made RSS flat ACROSS
packages (347, 348, 353, 353, 356 MB) and still left each package growing within
itself.

**`pkgMembers()` returned an interface slice.** Every call relocated - and so
deep-copied and walked - every member, and a member reaches its block, its
function, and through that the whole package. It is called five times in the emit
path. Measured as a 28-deep `codecEncodeValue` recursion under
`mxutil.Ensure__main.SSAMember <- pkgMembers <- emitAllFunctions`. The list is
built once by `emitIR` into `e.members` and read as a field (`358cfc72`).

Effect: **1 emitted package -> 8** (`array`, `internal/task`, `internal/bytealg`,
`internal/stringslite`, `math/bits`, `mxregexp`, `mxutil`, `internal/cpu`) with
`tc-FAIL=0` and RSS **flat at ~357 MB** the whole way. The failure is no longer
an OOM.

**The wall now.** The ninth package, `math`, dies with a SIGSEGV in
`mxutil.ExtractQuoted` under `(*ssaPkgBuilder).buildImportSegScopes` inside
`CreatePackage` - a corrupt string reaching a function that only indexes within
`len(s)`, so the length or the pointer is garbage. That is an import-segment
state problem (`mxutil.ConcatImportSegs` / `ResetConcatImportSegs`), not memory
pressure, and it is the first failure in this whole sequence that is not a
growth curve.

### 8.3b18 The sovereign rule is in place; the drain point was the hole

The lifecycle rule - a sovereign type ends with the arena that created it, and
if it is returned it is bequeathed to the receiver's arena and relocated there -
IS implemented: `arenaReleaseGroups` -> `sovInvalidateArena` releases every group
whose `ctxArena` is the arena being freed (`codecSovereignFree`), the bequest is
`sovRekeyRelocated`/`sovRebaseFrameRoots` on relocation, and the invariant is
checked loudly on every exit path ("arena: exit orphaned sovereign groups").

What was missing is not the lifecycle but a DRAIN POINT. Compaction is drained at
a select, and a compiler never runs one, so the emitter's arena was never
compacted - 8.3b17. That is now fixed at two safe points that follow the rule
exactly: between packages, where the previous package's arena is gone and
nothing it created can still be borrowed, and between functions, from a free
function so no frame of the emitter is live. Effect: 1 -> 8 emitted packages at
flat ~357 MB.

**An explicit unconditional compaction was built and REVERTED, because it found a
real bug.** `SovCompactAll()` queued every group in the receiver map and drained
with no ratchet or budget. It faults immediately in

    sovWalkValue1 <- sovMarkReceiverRoots <- sovFullMark <- SovDrainCompactions
      <- SovCompactAll <- CompileToIR

- i.e. it visits groups whose RECEIVER outlived the arena that created it. The
pending-queue path never sees them because nothing queued them, and
`sovInvalidateArena` refused to release them at arena free
(`RecordSovReleaseRefused`, `arena_scope.mx:336`). So there are groups in the
map whose creating arena was released without them, which is the rule broken -
and an unconditional sweep is exactly the instrument that surfaces it.

The lead is the accounting: `a.sovGroups` is maintained separately from
`len(a.sovGroupList)` at `sovGroups++` (alloc.mx:1781), `--`/`++` on rekey
(3108-3112), `--` on release (3617), and tested together with the list length at
`arenaReleaseGroups`. A drift between the counter and the list is what makes
`sovInvalidateArena` return early (`a.sovGroups <= 0`) with the list still
populated, and the group then keeps a dead receiver.

Until that is fixed, the explicit full compaction cannot be used: it segfaults.
The budgeted drain remains the working mechanism.

### 8.3b19 The import-segment list is corrupt on the WRITE side

The ninth package (`math`) dies with a SIGSEGV in `mxutil.ExtractQuoted`, a
function that only indexes within `len(s)`. Two probes fixed the location.

A dump of `target.segs` taken INSIDE mxutil - straight from the global, before
any return relocates it - prints one char per spec, `.` for a plausible length
and `X` for garbage. The FIRST package of the app already reports
`D segs:XX|`. So the corruption is not the return protocol and not the reader: the
concatenated-import list is stored corrupt, and `ExtractQuoted` then reads past
the end of a string whose length word is garbage (`1043149363` in the panic
dump).

That list is filled by the source-concatenation step and consumed by
`buildImportSegScopes` during `CreatePackage`, in a different phase - and with a
warm cache the compile phase does not re-run the concat at all, so the reader
consumes what the TYPECHECK phase wrote. Two candidate fixes were tried and both
made the build fail EARLIER (ci-free 8 -> 0), so both are reverted:

- copying the spec strings into the root arena at append time with
  `rootCopyString`;
- the same with `unsafe.String` over the root allocation, to rule out
  `string(b)` re-allocating into the current arena.

`rootCopyStr` is worth knowing about here: despite the name it allocates in the
CURRENT arena (`b := []byte{:len(v)}; copy(b, v)`), so it cannot make anything
survive the phase. `rootCopyString(a *Arena, s)` is the one that takes an arena.

Since neither copy changed the `X`, the strings are already bad at append time,
or the reader is looking at a list from a phase that no longer matches the
package. The next probe is therefore at the SOURCE: print `.`/`X` per spec inside
the concat loop, before the append, and again in `AppendConcatImportSeg` itself.
That decides between "the parse handed over dead strings" and "the global's list
is stale for this package".

### 8.3b20 `math` was the package graph being encoded on every accessor return

The ninth package did not hang in the hashmap. Markers inside the member builder
put the stall at `e.pkg.Members[k]` (272 keys; the range, the sort and lookups
0..16 completed, the next never returned), and gdb on the core showed the
process 40 frames deep in `codecSeenLookup <- codecEncodeValue`, not in
`hashmapGet` at all: the map read was fine, the RETURN of the member builder was
encoding every member's graph (member -> block -> function -> package -> the
member map). Every wall after that was the same shape, and each was measured
before it was changed.

1. **The member slice never crosses a function boundary.** `buildMembers(e)` is
   a FREE function (so its allocation lands in the compile arena the emitter
   reads from - as a method it went to the sovereign arena and was dangling by
   the time `emitAllFunctions` walked it, SIGSEGV in `main[emitAllFunctions]`)
   and it fills `e.members` in sorted key order. `pkgMembers()` is gone and its
   five call sites read the field. `sortedMemberKeys` is the only return left:
   strings, a few bytes each, reaching nothing. `math` then built its 272
   members instantly.
2. **`instrOperands` returns nothing.** It returned `[]SSAValue`, so every
   instruction with operands re-encoded the package: 3.2 GB live at
   `sovAddRec <- alloc <- codecDecodeValue <- instrOperands <- emitFunction`.
   It now leaves the operands in `e.operandBuf` (already the scratch field) and
   the two callers iterate the field. The `*SSAPhi` case is copied in rather than
   aliased, because the buffer is grown in place.
3. **`InstrBlock()`/`InstrParent()` are deleted from `SSAInstruction`.** A
   read-only accessor returning a graph node costs a whole-package encode per
   call. The six call sites use `e.currentEmitBlock`, which `emitBlock` already
   maintains - `emitJump`, `emitIf` and `emitPhi` were each asking for the block
   they were being emitted from.
4. **The runtime half was tried and is reverted.** `ManualArenaEnter` never
   marked its fn-stack slot, so `codecInManualFrame` - the predicate that makes
   the codec ALIAS a pointer into a manual phase arena instead of copying it -
   answered false for every phase, and every return inside a phase deep-copied
   what it touched (both the alloc.mx and the codec.mx comment already claimed
   the slot was marked). Marking it made `math` complete and dropped the encode
   count from 23M to 5.5M, and then the alias dangled: the next run died in
   `errors` with `alloc: implausible size 1216690128 arena=140060234366976` from
   `concatLineToFile <- remapOneLine <- remapErrorLines`, i.e. a source-line
   string aliased out of a phase arena that had already been released - exactly
   the failure 8.3c recorded for the same idea. The phase arena IS freed at
   `ManualArenaExit`, so every value that crosses a phase boundary needs a copy;
   the fix has to be at the STORE side (which globals keep phase data), not at
   the codec's stability question. Both alloc.mx hunks are reverted.

Result: `math` completes, and in the first run `errors` and `unicode/utf8`
followed it and the build stopped in `strconv` at the compiler's own 3 GB
ceiling - the three source fixes alone, with the runtime untouched. That result
is NOT reproducible: a second run of the SAME binary stopped in `errors`
instead, with a spurious SSA-build compile error whose remap then died at
`alloc: implausible size 8317679511831650432` in
`sliceConcat <- remapOneLine <- remapErrorLines`. So the next blocker is not a
wall but a nondeterminism, and it lives where 8.3b19 left it: the multi-file
concat / import-segment list. `errors` is the first package with three files
(errors.mx, join.mx, wrap.mx); every package before it is single-file. The
probe 8.3b19 asked for - `.`/`X` per spec inside the concat loop, before the
append, and again in `AppendConcatImportSeg` - is the next step, and it should
be taken on a package with more than one file. `SovDomainReport` (measured with the
runtime hunk in place, so the shape is right and the absolute numbers are a
floor) for `math`: net mapped 342 MB at ci-tc, 1.36 GB after 16 functions,
glive 78 MB against gcap 373 MB, back to 191 MB at ci-free - the peak is inside
one package and it is released at `ManualArenaExit`.

Next wall, with numbers: the peak is per PACKAGE because a manual phase makes
`FnArenaPush` return the phase arena, so nothing is recycled until the phase
exits. `strconv` alone climbs past 3.8 GB RSS (memory.high 66,358 hits, wchan
`__mem_cgroup_handle_over_high`) and dies at the compiler's own 3 GB ceiling.
The step after this one is a per-function nested phase arena, with the retained
stores audited first - `e.globalTypes`, `e.globalDeclTypes` and `e.extDecls`
hold string headers built in the per-function arena, so freeing it without
copying those would dangle.

The cheap win after that: `codecRetStableInner`'s `KindSlice` branch answers
"not stable" for any slice whose element type holds pointers, which forces the
slow path (encode+decode of stable headers) even though the encoder's own slice
shortcut would alias the same backing. Aligning the two is the next measurement.

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
