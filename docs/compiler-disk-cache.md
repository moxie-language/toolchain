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
