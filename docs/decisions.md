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
| R4 | `mxutil.Ensure`/`Reserve` still have ~1300 call sites | each returns a pointer-bearing slice, so each call deep-copies | finish the sweep: drop the call where the next statement is a `push` |

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

## How to change this record

A new design note is not a rule. It becomes one by: landing in both compilers
(wood law) or being marked stage4-only with a reason, being added to
**Implemented** here, and having the document it supersedes deleted - not left
beside it.
