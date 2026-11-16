# Design record: what is implemented, what is proposed, what is red

Status: living. This is the document that stops the tree working at cross
purposes: a design note in this repository is not a description of the tree
unless it is listed here as **implemented**.

Precedence, when two documents disagree:

1. [axioms.md](axioms.md) - the axioms and the pentalogue/octalogue are the
   authority. A design that violates one is wrong even if the code does it.
2. [memory.md](memory.md), [language.md](language.md), [concurrency.md](concurrency.md),
   [restrictions.md](restrictions.md) - the implemented language and runtime.
3. [architecture.md](architecture.md), [patterns.md](patterns.md) - how programs
   are built out of the language.
4. Everything else - measurements, plans, packaging.

`claude.md` is the working rule sheet for agents and holds the same normative
content as 1-3 in condensed form; where it disagrees with 2 or 3, the docs are
the record and `claude.md` is the bug.

---

## Implemented

| area | state | reference |
|---|---|---|
| arena stack (root, function, manual phase, sovereign) | implemented | [memory.md](memory.md) §1 |
| codec return protocol (encode, pop, decode) | implemented, both compilers | [memory.md](memory.md) §3 |
| sovereign groups, borrow, stable registration, compaction drain | implemented | [memory.md](memory.md) §4 |
| codec dispatch: raw extent / custom codec / structural | implemented | [memory.md](memory.md) §3 |
| one-slot and `:N` channels, select frame encoding, breakers | implemented | [concurrency.md](concurrency.md) |
| spawn as a thread in one process, copied values, shared handles | implemented | [concurrency.md](concurrency.md) |
| `push` growth for any arity including spread | implemented | [memory.md](memory.md) §5 |
| compiler per-package arena, disk cache (`.mxh` export, `.bc` object) | implemented | [compiler.md](compiler.md) |
| wasm domain hub protocol | implemented (round 386) | [concurrency.md](concurrency.md) §wasm |

## Removed from the record (was a proposal, never the tree)

These were written as if implemented and are not. Do not design against them.

| proposal | vocabulary it used | why it is gone |
|---|---|---|
| `memory-model.md` revision 3 | `MAIN`, `SHELF`, `shelf T`, `%fn_base`, identity `(epoch<<40)\|counter`, handle tables | a target design for offset-addressed arenas; the tree has address-based arenas and codec relocation |
| `memory.md` revision 3 | "MAIN is the process region", "SHELF is the only compacted region" | same |
| Reloc three-pass mark-compact | `RelocInit`/`Mark`/`Forward`/`Compact`/`FixupAll` | no caller in either compiler; the codec is the protocol |
| `pipe` capability syntax (`<-pipe[...]{:4096:"/path"}`) | capabilities-as-declarations | never built; the implemented primitive is `chan T{:N:bytes}` |
| kernel target with preemptive scheduler and MMU page tables | scheduler, ring transitions | contradicts L1 (no preemption within a domain); the kernel work is not started |
| `moxiefmt` | mechanical formatter | no such tool in the tree; [style.md](style.md) is a specification |
| `moxieport` as shipped tooling | Go→Moxie transformer | its own notes cite `sysroot/runtime.bc`, `os.runtime_args` and `//go:linkname`, none of which exist under those names |

## Known red (open defects, as of this record)

| # | defect | evidence | next step |
|---|---|---|---|
| R1 | The type checker can name a different `Object` for a *use* than the one a declaration was *bound* under, so `buildIdent`'s exact lookup misses and falls through to a nil constant | in `errors.is`, `err == nil` was emitted as `%ne = extractvalue {i64, ptr} null, 0`; clang rejects it (`null must be a pointer type`). Instrumenting the compare path printed `lt={i64, ptr} x=nil y=nil op=16 fn=is` | instrument `fb.vars` at the use (object identity, number of same-named bindings, scope id) and fix object identity at the declaration site; preferring `info.Uses` first was tried and breaks the tree, so Defs and Uses do **not** share identity today |
| R2 | Bootstrap fixpoint has not been re-verified since the arena work | `compiler-disk-cache.md` recorded "pre-existing red: any compiler built by a stage4 compiler panics at start-up in `codecDecodeValue`" | run the four-generation chain in [building.md](building.md) and record the first generation that reproduces itself |
| R3 | The compiler build requires `-no-main-arena` and the flag is easy to omit | without it, every self-mutating emitter method holds a sovereign arena that cannot be compacted while it runs; `math` and `strconv` exhausted 3.2 GB | the flag is now in [building.md](building.md); the build scripts should pass it |
| R4 | `mxutil.Ensure`/`Reserve` still have ~1300 call sites | each returns a pointer-bearing slice, so each call deep-copies | finish the sweep: drop the call where the next statement is a `push`. The parser's 20 sites are done (below); the rest is mechanical |
| R5 | `mxutil.ReadFile` returns a slice whose data pointer escapes as a pointer-to-basic, so the codec cannot copy it and **leaks the whole frame arena per call** | the first line of every build: `codec arena escape ... unknown-extent pointee`, then `codec arena escapes so far: 1024`; the printed stack is `ReadFile` `mxutil.mx:262` under `main.compilerBuildID`. A build reads hundreds of files, so a build leaks hundreds of arenas - the reason a wasm app emit reaches the 4 GB cap | migrate the hot call sites to `FileSize` + `ReadFileInto` (the pattern already used at `main.mx:3204`, `:3232`, `:4937`), starting with `compilerBuildID`; then decide whether `ReadFile` should keep its signature at all |
| R6 | The sovereign mark and rebase walkers allocated inside the walk | `sovWalkValue1` read `iface := *(*_interface)(p)` and took `&iface.value`, and an address-taken local is arena-allocated: 16 bytes per interface, in the group's own data arena during compaction. Measured while emitting `math`: 46 untouched 64 MB segments, 2.9 GB mapped against 600 MB RSS, ending in `sysMapDie` at the 3 GiB ceiling | **fixed** 2026-11-15: both walkers read the type and payload words in place; `sovRebaseValue` writes the payload back in place instead of storing a local |
| R7 | The codec's struct walk allocated once per field per encode and decode | `typ.StructFieldType(i)` and `typ.StructFieldOffset(i)` build an array view and return through the protocol; the stack on a build was `codecEncodeValue -> StructFieldType -> alloc` | **fixed**: `retFieldTypeAt`/`retFieldOffsetAt` read the descriptor entry in place; all four struct loops use them |
| R8 | `mapCensus` reported `mapMB=0` for every build | the size was decoded from the trailing hex digits of a `/proc/self/maps` line, which are the pathname or the inode, not the mapping | **fixed**: `end-start` from the line's first field |
| R9 | The memory ceiling compares virtual mapped bytes to a number derived from a resident-set limit | `sysMapDie` fires at `sysMapBytes-sysUnmapBytes > 3 GiB` while the cgroup and the box measure RSS; a program that reserves 3 GB and touches 300 MB dies for no reason | decide the metric: resident bytes (what the cgroup charges) rather than mapped bytes, or keep the ceiling and stop reserving 64 MB per phase arena |
| R11 | The scanner copied the rest of the source file once per non-ASCII rune | `Source.nextch` passed `s.buf[s.r:s.e]` to `utf8.FullRune` and `utf8.DecodeRune`; a subslice is a COPY, and those two functions read at most `utf8.UTFMax` bytes. gdb on the ceiling abort: `Source.nextch` at `source.mx:146` -> `alloc(size=618506)` -> 64 MB segments. One app package's pre-pass filled 2 GB of mappings with copies it never read | **fixed**: both windows are bounded by a `clamp(s.e-s.r)` helper. Any hot path that passes a *tail* subslice to a fixed-width reader has the same shape |
| R10 | A sovereign group can outlive the arena that holds its receiver, so a full mark walks dead memory | gdb on the emit crash: `sovMarkReceiverRoots(recv=0x7f3e2c4290d0, td=named:types[Named])` -> `sovWalkValue` -> `sovWalkValue1` -> `RawType.Kind(t=0x4)`; the receiver is a type-checker `types.Named` whose `Under` interface word had been overwritten by whatever reused the arena. The `home == nil` attribution fallback (a receiver allocated *by another sovereign group*, now attributed to that group's data arena) is in place and does not cover this case | find which exit leaves the group registered: instrument `arenaReleaseGroups` for groups whose receiver lies inside the arena being released but whose `ctxArena` is another arena or nil, then decide whether the group should follow the receiver or the arena. Until then the walk refuses an interface type word below `codecDataFloor` and counts it in `sovBadTypeWord` - a stop, not the fix. **The same fault has a second face**: after that guard the app build dies one frame earlier, in `sovWalkValue1` at `k := td.Kind()` (`alloc.mx:2679`), i.e. the *descriptor argument* is dead. A record's `typeDesc` for a generic instantiation is built at run time in the phase arena that is compiling, while the record itself lives in a sovereign arena that outlives it. So the same question has to be answered for descriptors as for receivers: either the descriptor is promoted to memory that matches the record's lifetime, or the record is attributed to the descriptor's arena and dies with it |


## Measurements of record

Numbers that decisions were made on, kept because a number you cannot trust is
worse than no number. The detail lives in [compiler.md](compiler.md).

| finding | number |
|---|---|
| `math` emit peak before the arena work | 1159 MB, then 3.2 GB death in `strconv` |
| `math` emit peak after (free `emitFunction`, presized lists, no per-node sovereign group) | 1039 MB entry, 1107 MB whole emit |
| `strconv` + `runtime` under `-no-main-arena` | `runtime` 581 functions, 1840 MB peak |
| reusable encode buffer | RSS 673 → 577 MB |
| adopted seen tables | 577 → 352 MB |
| app build, 8 packages before the emit fixes | now reaches the app's own bitcode |
| parser `mxutil.Ensure` in `complitexpr` (return-encode of the accumulated expr list) | 46 x 64 MB segments in one burst; the morly pre-pass died at the 3 GiB ceiling |
| sovereign walk: 16 bytes per interface, 46 segments | 2.9 GB mapped, < 600 MB RSS, `sysMapDie` |
| `morly` built by the 2026-11-13 compiler | 173 files, 1592 MB, links and runs (`--help`) |
| `smesh` wasm app emit | killed at the 4 GB cap before R6/R7; then the R10 fault during emit |
| `morly` integration suite against the fresh binary | 31 passed |
| `morly` unit suite (13 packages green) | stops at `pkg/relay/config` `TestLoadEnvOverride`: `alloc: implausible size 1704404576`, not yet diagnosed |
| scanner tail-copy per non-ASCII rune | `alloc` of 618506 bytes from `source.mx:146`, thousands of times in one pre-pass |

## How to change this record

A new design note is not a rule. It becomes one by: landing in both compilers
(wood law) or being marked stage4-only with a reason, being added to
**Implemented** here, and having the document it supersedes deleted - not left
beside it.
