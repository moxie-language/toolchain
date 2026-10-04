# Open work

State carried across context compaction. Ordered queue, the decisions already
taken, the findings that motivated them, and the two designs in flight
(module removal, user-code exemption). Update this file when the queue moves.

## Where things stand

Four repositories, all on branch `dev`, all level with their remotes
(`git@git.smesh.lol:<name>.git`, port 2222, `GIT_USER=owner`).

| repo | what it is | last known state |
|------|------------|------------------|
| `moxie` | the compiler, runtime and stdlib | `74c1fbeb` push-nil fix; suite 155/155, fixpoint converged |
| `nostr` | shared nostr primitives and codecs | `98c7069` (musiquay vocabulary moved out) |
| `morly` | the relay (`pkg/relay`, `pkg/store`, ...) | `b7dba37` |
| `musiquay` | the web app, docs corpus, protocol vocabulary | `8dd6f59` (vocabulary at `pkg/protocol`) |

`gitweb` on the VPS serves all of them at <https://git.smesh.lol>; the bare
repos live in `/home/git/<name>.git`, owned `git:git`, mode 775, HEAD ->
`refs/heads/dev`.

Working layout that matters: the checkouts live at
`~/moxie/git.smesh.lol/<name>`, which is *exactly* `$MOXIEPATH/git.smesh.lol/<name>`
(`$HOME/moxie`). That coincidence is why builds resolve imports without
`replace` directives, and it is the fact the module-removal design leans on.

## Queue

1. **Explain the exemption and the divergences.** Done in the session; the
   findings are below.
2. **Close the legacy/stage4 restriction gap.** **Done.** All six rules are
   bilateral and pinned in both compilers: unnamed returns, parenthesised call
   targets and `+` on text (`ea7bb392`), value receivers (already there), and
   the last two - package-level var initializers and named slice/map types -
   landed in `3b13de5e`. Closing the last two needed three things: stage4 had
   no implementation of either rule at all (a dotted-path `main` with
   `type Tags []string` and `var counter = 5` built under stage4 and was
   rejected by legacy, dependencies included); legacy's alias form
   (`type X = []T`) had to stay legal, and stage4 had to mirror the demoted
   `[]byte("literal")` constant form legacy allows; and legacy's package-decl
   gate read the directory path, so the same program was checked from
   `/tmp/moxie-tests.123` and exempted from `/tmp/moxie-tests-123`. A main
   package is now user code wherever it sits. `declvar` and `namedslice` pin
   both rules in both compilers (phase6 6x.1/6x.2); suite 171/171.

   **Landing the three exposed the real bill, and it is paid in app code, not
   in the compiler.** stage4 compiles `vendor/golang.org/x/...` while legacy
   sees `golang.org/x/...`, so the vendored exemption missed and
   `golang.org/x/crypto/chacha20poly1305` was rejected the moment the rule
   landed — fixed by stripping `vendor/` before the gate (`3d015c07`). Then the
   rule reached **musiquay's own packages**, which are `git.smesh.lol/musiquay/
   web/...` and so were never covered by the `/pkg/` accident. Measured
   unnamed-return signatures: musiquay 142 in 28 files (103 in
   `web/common/mls`, 13 `marmot`, 10 `jsbridge/schnorr`, 9 `web/wasm/app`,
   5 `web/wasm/signer`), morly 68 and nostr 36 — the last two all under `/pkg/`
   and therefore still exempt (nostr `make test` passes, 13 targets).

   **Musiquay migrated and green** (`a22269d`, plus `c54578d`).
   **morly migrated** (`993378b`): its top-level `main` package is user code —
   only `pkg/` is covered by the `/pkg/` exemption — so 6 signatures in
   `main.mx`/`main_test.mx` needed naming. `make test-unit` now compiles;
   `TestCrawlRelayAndPublish` then hits a **SIGSEGV that predates this work**
   (reproduced with the migration stashed and a compiler built with the rule
   disabled), so morly's unit suite is red for a runtime reason, not a language
   one — that is a separate bug to chase.

   The migration also found a parser defect (fixed, see the finding below) and
   the latent parse errors that must be cleared before recovered parses can be
   treated as fatal (see the finding after it).

   Also fixed on the way, both real bugs the rules surfaced: `moxie test` never
   called the package's `init()` (`de85a620`, phase6 `6aa`), and stage4's
   `isUserPackagePath` saw vendored paths that legacy never sees.
3. **`moxie test` never runs the package under test's `init()`.** **Done.**
   The harness builds a synthetic `main` around the library and injects a
   `__varinit` call at the start of it, but never a call to `init()`: the
   branch for a **synthesised** main already called both
   (`ssa_builder.mx:1213`), the branch for a main that **already has a body**
   called only `__varinit`. A program's main package cannot declare `init()`,
   so a normal build never noticed; the synthetic main of a test build can.
   Fixed, pinned by phase6 `6aa` (fixture `inittest`). stage4-only: legacy has
   no `moxie test` and no `__varinit` of its own, it uses go/ssa's lowering.
   The lazy-ensure workaround in `pkg/protocol` is no longer *required*, but it
   stays: a root-arena table built on first use has no ordering question.
4. **Clear the latent parse errors and make a recovered parse fatal.** **Done.**
   All three defects are gone: the concatenator skips blank lines and leading
   plain comments (but never `//:` pragmas) when it finds the body start, the
   `//:generate` pragma was moved below the import block, and the eight blank
   imports are deleted - which also required removing legacy's
   `hasUnsafeImport` gate on `//:linkname`, since stage4 honours the pragma
   without one and legacy did not (see the codec finding for the bootstrap
   failure that exposed it). `typeCheckPkg` now returns without a package on
   `len(cctx.parseErrors) > 0` as well as on `file == nil`, with a comment
   saying a partial tree is a guess. Details and verification in the finding
   above.
5. **morly: `TestCrawlRelayAndPublish` SIGSEGVs.** **Done.** It was a `time`
   package bug, not the crawler: `Location.cacheZone` is an interior pointer
   into `zone`, and a Location value is *copied* (returned from LoadLocation,
   assigned into the `localLoc` global); the copy leaves the pointer aimed at
   the source's backing array, which dies with the source's frame. The first
   `clog` call after a zone load then read through it. See the finding below.
   morly's `make build` also needed two fixes in front of it: `chan T{:n}` did
   not lower, and `main.mx` had one `)` too many in the version-JSON response,
   so the package did not parse at all. With all three, morly builds and
   `make test-unit` is green: 361 tests, 0 failures.
6. **Remove modules** (design below). Delete `moxie.mod` handling from both
   compilers, resolve imports only through `MOXIEPATH` + `$MOXIEROOT/src`
   (+ vendor), and move version pinning into a single mod-style **`MANIFEST`**
   file at the build root. Delete the `moxie.mod` files from every repo and the
   `replace`/`require` lines from the Makefiles. **`moxie get <path>[@<ref>]`**
   is the companion command: fetch, place at `$MOXIEPATH/<repo-path>`, check
   out the tag/branch/commit, record it in `MANIFEST`, and take a real
   (waiting) lock instead of dying on contention. It can land before the
   removal, since it is just today's auto-fetch with a ref argument and an
   exposed front door.

   **Landed so far (stage4):** `moxie get` (with `-pin`, `-worktree`,
   `-offline`, `-force`, `-remote`, `-protocol`, and `moxie get` with no
   arguments to materialise every MANIFEST entry) and MANIFEST-aware *build*
   resolution. The build root's `MANIFEST` is read once per process (`$MOXIEROOT`
   is the fallback, and only the build root's file counts, so a nested MANIFEST
   cannot shadow it); a pinned repo is materialised with `git worktree add` at
   `$MOXIEPATH/.refs/<repo>/<ref>` and the build reads that tree, leaving the
   checkout a developer is editing untouched. An unpinned repo still resolves to
   its checkout, fetched on demand. A pin whose ref cannot be materialised is
   fatal - silently building the checkout instead would compile a version nobody
   asked for. Verified with a local repo carrying tags v1/v2 and a MANIFEST
   pinning v2: the build read v2 from the worktree and the checkout stayed at v1
   on a clean tree; a bogus ref fails loud; no MANIFEST uses the checkout.

   **Still to do:** remove `moxie.mod` resolution from stage4 and legacy (stage4
   is done; legacy still reads `moxie.mod`/`go.mod` and falls back to
   `moduleFromRoots`), delete the files and the Makefile `replace`/`require`
   lines, and key the build cache on repo + resolved commit.

   **Legacy now resolves by layout too.** `legacy/loader/mxlist.go` gained
   `layoutRepoDir`, consulted after the package's own module and before the
   `replace`/module-cache readers: `git.smesh.lol/moxie/...` is `$MOXIEROOT`,
   a repository the build root's `MANIFEST` pins is the
   `$MOXIEPATH/.refs/<repo>/<ref>` worktree `moxie get` materialises, and every
   other repository is its checkout under `$MOXIEPATH`. A pin whose worktree is
   missing is an error naming the `moxie get` command, never a silent fallback
   to the checkout. Legacy does not clone or add worktrees itself - the
   materialisation stays in one implementation, stage4's.

   Verified with a hermetic fixture (`git.smesh.lol/depa` with `pkg/greet`
   printing a different word per tag, a consumer importing it, no module file
   anywhere): with no MANIFEST the legacy build resolves the sibling repository
   and prints the checkout's word (it failed "not found" before); with
   `require git.smesh.lol/depa v2` and the v2 worktree present it prints the
   v2 word; with a pin whose worktree is absent it fails loud; stage4 builds the
   same fixture with the same result. Suite 167/167 after the change.

   **Build cache keying is done and verified.** The package key is the content
   hash of the package's files plus the sorted hashes of its dependencies, plus
   target, `-cover`, capture policy and the link-set hash; and
   `cachedBcPath`/`cachedMxhPath` append `pkg.version`, which for a repository
   package is `manifestCommit(repoRootOf(pkgPath))` - the **resolved commit**,
   never the tag. Compiler identity is one level up in the cache directory name
   (`mxc-<version>-<compilerHash>-<buildID>-<os>-<arch>`, and `compilerHash`
   covers `_mxc_stage4/` *and* `src/runtime/`), so a compiler or runtime change
   cannot reuse an object. A hit pushes the cached `.bc` straight to the linker
   (plus any C files, which still compile) and the build does not recompile the
   package.

   The key is a strict superset of "repo + resolved commit": the commit alone
   would hit an object built from a *different* tree that claims the same
   commit, so content hash + commit + compiler identity is kept. The layout
   stays `$MOXIEPATH/cache/pkg/<package-path>/<hash>_<commit>.bc` rather than
   `$MOXIEPATH/.build/<repo>/<commit>/<triple>/`: the unit the linker consumes
   is the package, and the content hash is what makes the path a build happens
   to run in irrelevant. `moxie clean` empties the cache.

   Verified with the hermetic fixture above: the pinned tag `v1` was compiled
   once, the second build printed `cached example.com/thing/pkg/greet @
   d6af9e93...` (the resolved commit) and linked it, and the checkout under
   `$MOXIEPATH/example.com/thing` stayed where it was while the build read the
   worktree under `.refs/`.
7. **Make the model rules universal; make the conflict rule strict.** The
   immutability guarantee is a model invariant, not a style preference, so it
   belongs on the package root everywhere — with the **runtime/unsafe carve-out
   only** (measured: 69 global stores in `src/runtime`, the layer that builds
   sovereign arenas; see the finding below). Concretely:
   (a) apply global immutability and the two decl rules to every package above
   the runtime, deleting the `/pkg/` carve-out — costs the compiler tree
   nothing, ~77 stdlib sites and 39 app-repo sites. **Approved as staged work**
   (decision taken this session, measured then): the named slice/map-type rule
   alone is 74 sites in `src/` outside `src/runtime` and 16 across the app
   repos (nostr 3, morly 7, musiquay 6), plus 4 package-level var initializers
   in `src/`; the runtime keeps its carve-out. **First stage is not a rewrite:
   legacy already rejects named slice/map types while stage4 accepts them, so
   settle which side the rule is on, then enable it for one scope at a time and
   keep the tree green after each stage.**
   (b) forbid **dot imports** (all 20 are the compiler's
   `. "git.smesh.lol/moxie/pkg/types"`; 3087 identifiers to qualify) or, if
   that price is refused, make a dot import's names participate in the conflict
   check instead of being invisible. **Done, by the second option**: the 20
   dot imports in `_mxc_stage4/` remain, and a dot import's names now
   participate in the conflict check - phase6 `dotconflict` requires stage4 to
   reject a declaration colliding with an imported package name and legacy to
   reject one "already declared through dot-import of package math";
   (c) make import conflicts hard errors: two imports binding one name, and an
   import colliding with any declaration — currently import-vs-import keeps the
   first binding silently and stage4 dedupes the names so the second is not
   even seen. **Done**: phase6 `importconflict` requires stage4 to reject
   "is imported twice in one file" and legacy "redeclared in this block".

   Both are in the suite today (167/167), so what is left of this item is (a)
   alone, and (a) needs the decision recorded below: the runtime/unsafe
   carve-out is fine, but ~77 stdlib sites and 39 app-repo sites become
   compile errors the moment the `/pkg/` carve-out is deleted, and the app
   sites are other repositories' working code.
8. **VS Code / VSCodium: full Moxie language support.** **Done**
   (`editors/vscode/`, installed into ~/.vscode-oss/extensions as
   `mleku.moxie-lang-1.1.0`).
   - `syntaxes/moxie.tmLanguage.json`: keywords from `pkg/token/tokens.mx`
     (no `go`, no `fallthrough`), builtin types from `pkg/types` (`byte`/`rune`
     as aliases, no `int`/`uint`), builtin functions (`push`, `pop`, `resize`,
     `len`, `cap`, `copy`, `delete`, `close`, `clear`, `min`, `max`, `panic`,
     `recover`, `print`, `println`, `spawn`), `//:build`/`//:linkname`/`//export`
     pragmas, and the *removed* names (`make`, `new`, `append`, `reflect`, `any`,
     `int`, `uint`, `complex*`) as invalid so their use is visible.
   - `snippets/moxie.json`: named returns, pointer receivers, sovereign types,
     `init()` + zero-value globals, `[]T{:n:cap}`, `mxutil.Ensure` before a
     spread push, a spawn worker, interface assertions.
   - `src/extension.js`: diagnostics are the compiler's own - open/save builds
     the document's package with `moxie build` and turns its positioned errors
     into problems (`Moxie: Build the package and report diagnostics`,
     Ctrl+Alt+B); completion and hover explain the Moxie semantics from the
     keyword/builtin tables; go-to-definition indexes `func`/`type`/`var`/`const`
     declarations across `**/*.mx`. Settings: `moxie.path`, `moxie.moxieRoot`,
     `moxie.buildOnSave`, `moxie.buildFlags`, `moxie.trace`.
   - Verified: manifest and grammar parse as JSON, the client source passes
     `node --check`, and `codium --list-extensions` reports `mleku.moxie-lang`.
     A headless run cannot exercise the extension host, so the in-editor
     behaviour is verified by installing and reloading the window.
   - A dedicated LSP server is not needed for this scope; the providers give the
     same surface without a second process to keep alive.
9. **Wire `musiquay/pkg/protocol` into the running system.** Relay side:
   storage/index/validation policy per kind, the `#x` blob -> asset index hosts
   need to gate, and the replaceable-range exception for 32218-32220. App side:
   adapters between these structs and `event.E` (native) / `core.Event` (wasm),
   plus the publish paths. **Started:** the 32218-32220 exception is fixed and
   pinned (`pkg/relay/pipeline/policy.mx`, morly 28/28); the inventory, the
   seams, the ordered edits and the open decisions are in the finding below.

### Repository locks are held for the whole build

A build resolves a repository, then spends minutes compiling packages out of it;
another build wanting a different ref must not check the tree out underneath it.
Every repository a build reads is locked at resolution (`holdRepoLock`) and the
lock is kept until the process exits. The lock file records the holder's pid,
and waiting past the timeout names that pid and the lock path instead of failing
anonymously. `moxie get` keeps its shorter acquire/release around the checkout
itself.

The first version leaked the lock: `releaseHeldRepoLocks` lived only in
`exitNow`, and only `fatal` and the usage errors go through `exitNow` - every
successful build returns from `main` and left the lock file behind. The next
build then waited three minutes and died naming a pid that had been gone since
the previous build finished. Two more defects sat behind it:

- **The acquire was not atomic.** It read the lock path and then wrote it, so
  two builds could both see an empty path and both write their pid; neither
  waited and both checked out the same tree. The create is now the test:
  `os.OpenFile(lp, O_WRONLY|O_CREATE|O_EXCL, 0644)`, and only the process that
  creates it holds the lock.
- **A dead holder wedged the repository for good.** A SIGKILLed or crashed
  build leaves the file, and pid reuse is the only thing that could ever clear
  it. After five failed acquires (one second) the waiter runs `kill -0` on the
  recorded pid: a gone holder's lock is removed and the waiter proceeds, a live
  one is left alone and the wait continues to the three-minute fatal.

Release is now a `defer releaseHeldRepoLocks()` in `main` beside
`defer cleanupScratch()`, so every `return` path drops it, and `exitNow` still
releases explicitly for the paths that bypass defers.

Verified on a fresh `-a` build with a hermetic fixture (`example.com/thing`
tagged `v1`/`v2`, a consumer whose `MANIFEST` pins `v1`): a successful build
leaves no lock file; a second build is a pure cache hit
(`cached example.com/thing/pkg/greet @ d6af9e93...`); a lock file written with a
dead pid is broken after one second and the build proceeds; a lock held by a
live pid is still respected (the build waits, does not break it).

### `+` on text is the target's rule, and is bilateral again

Removing `moxie.mod` took away what scoped the `+`-on-text rule: legacy gated it
on `p.Module.Main` (the input package and its module, never a dependency) and
stage4 on the target's module prefix. Without a module file the two disagreed -
stage4 rejected a `+` inside a dependency, legacy accepted a `+` in a
module-less directory target.

Both now mean the same thing by "the target's own code":

- stage4 records the target package's compiled path from the resolver entry for
  the requested path, and `isTargetPkg` admits that package plus everything
  under the target's repository (`rootModPrefix`); the rule is gated on both.
- legacy marks the input package `Module.Main` even when no module file names
  it, so its existing `p.Module.Main` gate covers exactly the same set.

The `aeaddep` fixture was itself violating the rule - it joined the known-answer
hex with `+` across lines, Go style - and now uses `|`. Suite: 167/167.

## Decisions taken

- **Three-repo split**: `nostr` = shared primitives and codecs; `morly` = the
  relay only; `musiquay` = the web app, docs and the protocol vocabulary.
- **The Musiquay vocabulary lives in the musiquay repo** at `pkg/protocol`
  (package `protocol`, import `git.smesh.lol/musiquay/pkg/protocol`). It is
  Musiquay's own kinds and payloads, not generic nostr. `morly` will reach it
  with a `replace` until modules are removed altogether.
- **Spec examples are the test vectors**: every payload type parses the tag
  list from the docs and re-renders it tag for tag. Unknown tags are kept in
  `Extra` and re-emitted. `Validate` enforces only what the documents state;
  everything they leave open is a `Warnings` entry.
- **Vocabulary tables are filled lazily** (`ensureRoles`, `ensureFormats`) in
  the root arena, not by an initialiser or `init()` — see finding 3.
- **Parked features keep their code**, only entry points are gated (kind-1
  forum, MLS/Marmot DMs).

## Findings

### Builtin Stringer methods work in both compilers — FIXED

`n.String()` on any builtin is now a real method call in the native build of
both compilers, `fmt` formats `byte` and `rune` instead of printing `?`, and the
loader no longer deletes go/types errors to make it type-check. Verified with one
probe (`bool`, every int and uint, `uintptr`, `float32/64`, `string`, `byte`,
`rune`, `[]byte`) covering direct calls, conversion receivers, a `[]byte` and a
string through `fmt.Stringer`, and `fmt.Println`, on both compilers:

```
bool true / int8 -8 / ... / uint64 64 / float32 +1.500000e+000
float64 +2.50000000000000e+000 / string str / uintptr 0xff / byte 65 / rune 128512
conv 7 / iface -32 / ifacebyte 65 / byteslice abc / ifacebytes abc / ifacestr lit
fmt true -8 -16 -32 -64 8 16 32 64 1.5 2.5 str 0xff 65 128512
```

The three gaps and what each needed:

1. **stage4 never resolved the call.** `resolveMethodCallWithRecv` returned nil
   for a `*Basic` receiver. It now resolves the method out of the type's method
   set (`resolveBuiltinMethod`): inside the runtime package that is the local
   definition, elsewhere it is the ordinary ctx-carrying cross-package call into
   `runtime.(*T).String`. The receiver ABI is a pointer even for a value
   receiver, so the caller materialises the value and passes its address.
2. **The interface table named a thunk, not the method.** `findIfaceImpls`
   registered `basic:<T>.String$identity` wrappers (right only for `[]byte`,
   which String()s to itself) and never registered the real symbol, so a boxed
   basic dispatched to an identity load. It now registers each builtin against
   `runtime.(*T).String`. `emitBasicIdentities` is deleted. The table is gated on
   `buildTagTrue("moxie.stringer")`, so a compiler that does not carry the
   methods does not reference them either.
3. **legacy suppressed the type error.** `filterImpossibleAssertErrors`
   (deleting every "missing method" error) is gone, and `moxie.stringer` is in
   `BuildTags()` for non-wasm. The go/types patches are now real: a method may be
   declared on a builtin, it is recorded in the type's method set, and a named
   type over a builtin inherits it (mirroring stage4's `collectMethods`
   recursion, which is why `math/big.Word` can go to `fmt.Sprintf`).

Decisions taken while landing it:

- **`byte` and `rune` are aliases.** In go/types they are *separate* `*Basic`
  values (`Typ[Byte] != Typ[Uint8]`) that are identical to `uint8`/`int32`;
  stage4 keeps them as named types over the same kinds. Their String() is the
  underlying method, mapped by name (`builtinAliasName` in stage4,
  `basicCanon` in the patched go/types) — so both compilers print `byte(65)` as
  `65` and `rune(0x1f600)` as `128512`. The bespoke hex byte/rune String bodies
  were removed from `src/runtime/stringer.mx`: they printed `0xf600` for a rune
  above the BMP (the buffer was a fixed four nibbles) and could not exist in
  legacy at all, where `byte` *is* `uint8` and would have been a duplicate
  method. `uintptr` keeps its own hex `String()`, which both compilers can
  declare (legacy's `uintptr` is a distinct Basic, stage4's is a named type).
- **Shadowing**: a named type that declares its own `String()` wins over the
  inherited one. The go/types patch checks the method set before adding the
  inherited method; without that, go/types records a same-depth collision and
  drops the method entirely.
- **The universe declares the methods.** A package can be type-checked before
  the runtime that supplies the bodies, and `fmt.Stringer` satisfaction is
  decided at check time, so `defBuiltinStringMethods()` pre-registers the
  placeholders in the universe; the runtime's declaration replaces the
  placeholder object. stage4 has the same shape (`addBasicStringMethod` at
  universe construction).
- **A byte slice inherits string's methods only when unnamed.** `[]byte("x")`
  must satisfy Stringer; `type Bytes []byte` must not inherit string's — if it
  does, `createPackage` emits the *string* method in the named type's module and
  the link fails with "symbol multiply defined". stage4's `collectMethods` has
  the same boundary (no `*Slice` case).

Legacy-side root cause worth remembering: `createPackage` emits methods only
through a package-level `*ssa.Type` member's method set, so a method whose
receiver base is a builtin - which has no such member - existed in the SSA
program with a body but was never emitted, and every call to it was an undefined
symbol. `createBuiltinMethods` emits the ones the package declares, and skips
the ones a named type merely inherits (that method belongs to the builtin's
declaring package).

Latent issues found on the way (not fixed here):

- stage4's per-file typecheck called `AddMethod` on the *universe* `byte`/`rune`
  named types, storing a TCFunc whose Name string belonged to the file's parse
  arena. In `src/runtime/stringer.mx` that produced two methods named `String`
  on `byte` and a SIGSEGV inside `runtime.stringEqual` at emit time. Fixed by
  skipping universe Named receivers in the typecheck attach step; the universe's
  method set is pkg/types' business, not a file's.
- `Named.AddMethod` (pkg/types) stores a caller-arena `*TCFunc` in a
  root-arena-backed slice and compares method names by string equality, so a
  name whose backing bytes died is read as garbage. Harmless today because the
  only AddMethod calls with parsed names come from `registerMethod`/mxh loading,
  but it is the reason the crash above was possible.
- stage4 accepts an impossible interface type assertion (`val.(*SSANamedConst)`
  where the type does not implement the interface); legacy's go/types rejects
  it, so such dead assertions must be deleted to build stage4 with legacy (one
  was, at `ssa_builder.mx` `assignable` checking).
- Variadic generic inference picks an untyped constant's *default* type over the
  slice element type: `push(dst, '0', byte(exp)+'0')` with `dst []byte` builds a
  `[]int32` literal and stores an i8 into an i32 slot. It is latent while `byte`
  is a named type (the inference then lands on `byte`); making `byte` an alias
  exposed it as an LLVM type error in `strconv`. Worth a root-cause fix.
- Calling `String()` on an untyped constant receiver (`"lit".String()`)
  resolves in stage4 (via `UntypedToTyped`) but not in legacy, which has no
  such conversion for a selector receiver.

### `time`'s zone cache was an interior pointer, and a copied Location dangles

morly's `make test-unit` died in `TestCrawlRelayAndPublish` inside
`time.(*Time).locabs` (gdb: `locabs` <- `appendFormat` <- `AppendFormat` <-
`Format` <- `clog` <- `crawlPass` <- the test). The panic address was a code
address, so the faulting load was `Location.cacheZone` (or the slice read
through it) pointing at memory freed with a frame.

Two independent lifetime mistakes, both Moxie-specific:

1. **`cacheZone *zone` is an interior pointer.** `LoadLocationFromTZData`,
   `LoadLocation` and `FixedZone` set `l.cacheZone = &l.zone[i]`.
   `loadLocation` returns a `*Location`; the return relocates the value (and
   its slices) into the caller's arena, but the interior pointer still aims at
   the *source's* array, which dies with the source's frame. A lookup that hits
   the cache window then reads dead memory: a wrong offset if the page is still
   mapped, an implausible allocation size (0x1000000030) once it is not.
   Fixed by making the cache an **index** (`cacheIdx int32`, `cachedZone()`
   resolves it against the Location's own `zone`); the one site that cached a
   synthesized `&zone{...}` now appends that zone to `zone` so it stays
   addressable by index. `zoneinfo_read.mx`, `zoneinfo.mx`, `time.mx`.
2. **The globals were built in the initializing frame.** `localLoc` and
   `unnamedFixedZones` are package globals filled lazily; the zone data they
   held was allocated in the frame of `initLocal`/`FixedZone`, so it died at
   return. Both now build in the **root arena** (`initLocal` ->
   `initLocalInRoot`, `buildUnnamedFixedZones`), which is the same rule
   `pkg/types` follows for its caches and the reason package globals are meant
   to be settled in `init()`.

Measured before the fix: `time.Local().String()` printed an empty name and
`t.Local().String()` showed `+0000` where `date +%z` says `+0500`; after, both
the local zone and `LoadLocation("Asia/Almaty")` report `+05` / offset 18000.
legacy and stage4 share `src/time`, so the fix applies to both; the suite and
morly's 361-test unit suite are the verification.

### `chan T{:n}` did not lower, and the parse position is off by one

`rewriteChanMakeLiterals` turns `chan T{...}` into `__slicealloc(chan T, ...)`
before parsing. A capacity-only literal (`chan struct{}{:64}`) carried its colon
into the argument list, producing `__slicealloc(chan struct{}, :64)` - a syntax
error. A channel has no length separate from its capacity, so the leading colon
is simply dropped in both compilers (`ir_rewrite.mx` stage4,
`legacy/mxtext/rewrite.go`). morly's relay is the first code to use the form.

While chasing it: the line number a stage4 parse error names is **one behind**
the real line for a single-file package (`main.mx:104:82` for an error on 105,
whose length is 83 - the column is right, the line is not). The column is what
pinpoints the character, so this is a reporting defect in the line remap
(`concatLineToFile`/`concatSourceMap`), not a parse defect. The same off-by-one
made the morly diagnosis take longer than the one-character fix did.

### Legacy vs stage4: the same source compiles as two dialects

Same program, ordinary user module (`example.com/app`), built with both.
**Re-measured; three rows of the original table were wrong** — they were
observed through the module gate below without realising it:

| program | legacy | stage4 | now |
|---|---|---|---|
| unnamed return values | reject | accept | both reject (`ea7bb392`) |
| `(f)()` parenthesised call target | reject | accept | both reject (`ea7bb392`) |
| `println("a" + "b")` | reject | accept | both reject (`ea7bb392`) |
| `var Version = []string{"1"}` | reject | accept | **open** — needs item 5 |
| `type Names []string` | reject | accept | **open** — needs item 5 |
| `type M map[string]string` | reject | accept | **open** — needs item 5 |
| closure capture without `-allow-closure-captures` | reject | reject | parity |
| `init()` in a main package | reject | reject | parity |

Legacy's messages: `moxie: package-level var with initializer is not allowed:
use zero-value declaration and init()`, `... named slice type 'Names' is not
allowed: use []T directly`, `... named map type 'M' is not allowed: use map[K]V
directly`, `... '+' is not allowed for text concatenation, use | operator`
(a comma, not the colon `restrict.go:274` has — the live message comes from
`mxtext.CheckPlusOnText`, not the SSA check), `... unnamed return values are
not allowed: name all return values`, `... parenthesized call target is not
allowed: write f(x), not (f)(x)`.

### A paren-less named result list is accepted and mis-parsed — FIXED

`func F() err error {` — a named result **without** parentheses — is not valid
Moxie (nor Go). The parser did not reject it: it recovered by swallowing the
body, so the function's declarations landed at package scope, and
`packageDeclNames` returned the locals. Every *other* function declaring one of
those names then reported `'n' shadows declaration in enclosing scope` — 167 of
them in `musiquay/web/common/mls` — and the signature itself was reported as
`unnamed return values are not allowed` when it names one. Found while migrating
musiquay: the first rewrite emitted `func F() err2 error` for the 8 signatures
with a single anonymous result; the correct form is `func F() (err2 error)`.

**Fixed** (`210d2f99`), in both parsers — `pkg/syntax` (what the compiler uses)
and `_mxc_stage4/syntax` (what `mxlex` uses):

- the malformed shape is a syntax error and the parser bails out, the way
  `syntaxError` does when no handler is installed. Legacy's go/parser rejects
  the same input (`unexpected name error after top level declaration`).
- `typeCheckPkg`'s `file == nil` branch had an **empty loop** where the parse
  errors should have been reported, so a hard parse failure surfaced as whatever
  unrelated symptom came next. It now reports them (`parseErrorReport`).

Pinned by phase6 `6ab` (fixture `badnamedresult`).

### Latent parse errors: what must be fixed before they can be fatal

A parse that *recovers* is still tolerated, because the stdlib carries several
and making them fatal is its own cleanup. Found by temporarily treating every
recovered parse error as fatal — each one is a real defect that has been silent:

1. **The concatenator leaves an import line behind.** `scanFileRegions`
   (`main.mx`) starts a file's body at the first non-blank line after the
   package clause, so the common `package X\n\n// doc\nimport ...` prefix puts
   the import inside the body: it is hoisted *and* left in place, and the parser
   reports `imports must appear before other declarations` for `pkg/token`, the
   runtime, and others. **Fixed during the investigation but reverted for scope**:
   the correct rule is to skip blank lines and leading comments (but not `//:`
   pragmas, which carry `linkname`/`embed`/`build`) when finding the body start.
   The fix is proven: with it, `pkg/token` and the runtime parse clean.
2. **Eight blank imports** in the stdlib (`import _ "unsafe"` ×7 for linkname,
   `import _ "embed"` ×1) which the parser has always rejected
   (`blank imports are not allowed in Moxie`) and silently ignored. Moxie
   handles both via its own pragmas, so they are vestigial — removing them is
   the fix, and it was verified to build.
3. **One `//:generate` pragma before an import**
   (`src/crypto/internal/fips140/mlkem/mlkem768.mx`); moving it below the
   import block is enough.

Doing all three makes recovered parse errors fatal-able, which is the honest
end state: a recovered parse builds a partial tree, and every downstream
diagnostic from it is a guess.

### Legacy's rules are gated twice, and one gate needs a module

Nothing is enforced on a directory target that has no `moxie.mod`: legacy
skips `CheckPlusOnText` unless `p.Module.Main && p.Module.Path !=
"git.smesh.lol/moxie"` (`legacy/loader/loader.go:439`), and outside a module
the loader rewrites `+` to the pipe operator before any check sees it. So
`println("a" + "b")` in a manifest-less directory is **accepted** by legacy,
and the same program with a `moxie.mod` is **rejected**. stage4 now mirrors
that with `targetIsMainModule()` (`bst.rootModPrefix` empty means no module),
which is why the three new fixtures carry a `moxie.mod`: without one they
would prove nothing.

Splitting the checks by gate, for the remaining work:

- **`isUserPackage` (SSA import path, `/pkg/` exempts, no module needed):**
  unnamed returns, parenthesised call targets, global stores, closure
  captures, spawn-move, slice-to-array, value receivers. stage4 mirrors it and
  these are done.
- **`isUserPackageByPath` (AST, real directory path, `/pkg/` does *not*
  exempt, `main` always user):** package-level var initializers, named
  slice/map types, `init()` rules. This is the pair still open, and the reason
  it cannot simply be switched on: it would reject 39 sites in the three app
  repos (see queue item 2).
- **`CheckPlusOnText` (main module only):** `+` on text. Done, with the gate.

### Why the immutability rule is user-gated (measured)

Removing the gate from stage4's `checkGlobalMutation` and building the tree
with the ungated compiler fails on the **first package compiled: `runtime`**,
with 69 global stores outside `init()` — `spawnDomain` (7), `sovQueueCompact`,
`InitCShared`, `codecSovereignCompact`, `ArenaAcquire`, `ArenaRelease`,
`sovWalkValue`/`sovRebaseValue`/`sovFullMark`, `SovDrainCompactions`,
`ManualArenaExit`, `alloc`, `semacquire1`/`semrelease1`, `fastrand`,
`poll_runtime_pollServerInit`, `coverHit`. That is the arena/spawn/timer
machinery itself: the layer that *constructs* sovereign arenas cannot route
every one of its own globals through a sovereign type. CLAUDE.md already
carves this out ("Runtime and unsafe packages are exempt — they implement the
arena system"), so the runtime exemption is principled, not a shortcut.

Everything above the runtime is already clean: `_mxc_stage4` and `pkg/` have
**zero** package-level var initializers and zero named slice/map types, so
making both decl rules apply to the compiler tree costs nothing. The stdlib has
3 initialised vars in one `var(...)` block (`src/mime/type.mx`) plus a handful
of top-level ones, and 74 named slice/map types across 49 files.

Conclusion: the `/pkg/`-shaped carve-out is the accident to delete, not the
runtime exemption. Deleting it costs a migration of 39 sites in the three app
repos (see queue item 2) and ~77 in the stdlib, but costs the compiler tree
nothing.

### The full cost of universal immutability (inventory)

Exempting the runtime and running the same check over everything else gives the
complete list in one pass — **107 global stores outside `init()`**, by package:

| package | sites | what it is |
|---|---|---|
| `internal/cpu` | 36 | `doinit` (32) and `processOptions` (4) |
| `syscall` | 20 | `Setenv`/`Unsetenv`/`Clearenv`/`copyenv`/`loadenvs`, `_getMapper`, `ensureRlimit`/`Setrlimit`/`prlimit` |
| `time` | 11 | `FixedZone`, `LoadLocation`, `_startNano`, `_ensureUtcLoc`, `(*Location).get` |
| `os` | 10 | `signalsInit`, `ensureMinrand`/`minrand`, `Mount`, `ensureFS` |
| `_mxc_stage4` (main) | 8 | `addCoverSite`/`resetCoverSites`, `(*irEmitter).typeFromTail`, `typeCheckPkg` |
| `pkg/types` | 8 | `SetUniverseGlobals`, `LookupImportByName`, `DirectImportPaths` |
| `crypto/internal/sysrand` | 4 | `urandomRead`, `Read` |
| `math/rand`, `math/rand/v2`, `math` | 6 | `globalRand`, `checkUseFMA` |
| `internal/testlog`, `internal/syscall/unix`, `crypto/internal/fips140` | 4 | logger/random/indicator hooks |

Two distinct problems, and only the second is a migration:

1. **The initialiser exemption is a name prefix.** `isInitFuncName` exempts
   `init` and anything starting `init`, so `doinit` (32 sites — it *is*
   internal/cpu's initialiser, called from `init()`), `processOptions`,
   `signalsInit`, `_startNano`, `loadenvs`, `_ensureUtcLoc`, `checkUseFMA`,
   `urandomRead`, `SetUniverseGlobals` all report as violations of a rule they
   do not actually break. A principled "this function initialises package
   state" notion is missing; the prefix test is the same substring hazard as
   the `/pkg/` gate.
2. **Genuine post-init caches** must move into sovereign holders: `syscall`'s
   env/mapper/rlimit, `time`'s zone cache, `os`'s signal and minrand state,
   `math/rand`'s global source, `crypto/internal/sysrand`, the compiler's cover
   sites, and `pkg/types`' import registry.

Measured, "immutability everywhere except the runtime" is a ~107-site stdlib
program plus the 69 runtime sites, not a switch. Reverted to the gated rule
until that is decided; the inventory above is the evidence for the decision.

### Dot imports are a hole in the no-conflict rule

`grep '^\s*\.\s*"'` over every tree: `_mxc_stage4` 20, and **zero** in `pkg/`,
`src/`, `nostr`, `morly`, `musiquay`. All 20 are the same import —
`. "git.smesh.lol/moxie/pkg/types"` — pulling ~100 exported names into file
scope, with 3087 identifier occurrences across 34,488 lines.

Neither compiler can see them: legacy `checkImportNameCollisions` skips blank
and dot imports explicitly (`restrict.go:684`), and stage4
`packageImportNames` skips a local name of `.` (`ir_scope.mx:781`). So a dot
import can inject a name that collides with a package declaration and no rule
reports it — precisely the no-shadow guarantee the gate is supposed to give.

Two further holes in the same rule: import-vs-import (two imports binding one
local name keep the first silently — stage4 `registerImport`'s "first-wins"
fallback), and stage4's `packageImportNames` *dedupes*, so the second binding
is invisible even where a conflict check would look.

### The no-conflict rule is now universal, and dot imports are inside it

Implemented in both compilers: a **dot import's names are reserved like any
other import's**, and two imports binding one name in one file are a conflict.
stage4 gained `checkImportConflicts` (it had no collision check at all — only
`checkShadowing`, which sees a local shadowing an import, not two imports
colliding); legacy already rejected both shapes through go/types and needed the
dot names for its own check.

Three things that measurement taught, worth keeping for the next rule:

- **The tree is clean**, so the strict rule costs nothing: zero local or
  package-level bindings in `_mxc_stage4` collide with any of the ~100 names
  `pkg/types` exports, and the compiler builds with the rule on. Banning the
  dot import outright would have meant qualifying 3087 identifiers across 20
  files for no gain; making it visible to the rule enforces the same guarantee.
- **A package's files are concatenated into one `syntax.File` with a hoisted
  import block** (`ssa_builder.mx:59`), so the *same* import appears twice in
  one `DeclList` for a one-file package like `pkg/mxutil`, and — worse — every
  source file's imports land in one scope. Grouping by that file reported
  `encoding/hex` (`event.mx`) and `nostr/pkg/hex` (`canonical.mx`) as two
  bindings of one name in one file in `nostr/pkg/event`; they are in different
  files and were always fine. The unit is a **concatenation segment**, which is
  one original file: `checkImportConflicts` reads `pb.segImports`
  (`ssa_builder.mx:407`), not the parsed file's decls. `importNamesOf` stays
  for the package-wide reserved set, where per-file grouping does not matter.
- **Legacy must skip objects whose `Pkg()` is not the package under check.**
  A dot import's objects sit in the file scope but belong to the imported
  package; without that guard every name a dot import injects reports as a
  collision with itself.
- **Only exported names count.** `Scope().Names()` returns unexported helpers
  *and* the `__moxie_*` builtins the legacy loader injects into the imported
  package itself — `pkg/types` gets them, which is how `hexDigit`,
  `isHexDigit`, `untypedNil`, `__moxie_concat`, `__moxie_eq`, `__moxie_lt`,
  `__moxie_secalloc` all reported as colliding with themselves on the first
  run. A dot import injects neither, so the filter is `token.IsExported`
  (stage4: first byte `A`–`Z`).

Pinned by phase6 `6y`/`6z` (fixtures `importconflict`, `dotconflict`). The two
compilers reject for *different* reasons — legacy through go/types
("redeclared in this block", "already declared through dot-import"), stage4
through the rule — so the checks pin each side's message instead of pretending
they share one.

### The user-code exemption is a path substring, implemented three times

stage4 `_mxc_stage4/ir_scope.mx:44` — exempt when the path is `main` or
`command-line-arguments` and the build root is the moxie module, or it starts
`golang.org/x/`, or it contains `/pkg/`, or it contains no `.`. Legacy has two
more shapes: `isUserPackage` (SSA import path, same clauses plus "imports
`git.smesh.lol/moxie/pkg/`") and `isUserPackageByPath` (AST, real directory
path, *no* `/pkg/` clause, and `main` is always user code).

What the gate turns off (user-only checks): package-level initialisers and
global mutation outside init, value receivers, unnamed returns, closure
captures, named slice/map types, `+` for text, parenthesised call targets,
slice-to-array-pointer conversion, spawn-move, `os.Exit` in `main`. Two rules
are universal by design and prove the model can express "everywhere": channel
completeness and no fresh declarations in a loop body of a self-mutating
method.

Consequence: moving a file changes its language (`nostr/ws` failed;
`nostr/pkg/ws` passed), and most of our own code is exempt — 83 files under
`morly/pkg/`, all of `nostr/pkg/`, the compiler tree — which is precisely where
arena-sensitive code lives. Proposed replacement: a declaration that travels
with the package (`restrict = strict|none` in a manifest that survives module
removal, or a `//:moxie unrestricted` file tag beside the existing
`//:build wasm` tags), with the stdlib/vendor exemption as a short explicit
prefix list rather than a substring test.

### Why `replace` exists today (and what removing modules changes)

Resolution order in `discoverPkg`: directory paths, then replaces (build root's
first, then each resolved dependency's), then the module prefix, then
`$MOXIEPATH/<import path>`, then `$MOXIEROOT/src` and `src/vendor`, then
module-relative, then `autoFetchRepo` (`git clone https://<import path>` into
`$MOXIEPATH` under a per-repo `.lock`).

`replace` is a pin, not a workaround: without it, resolution depends on the
checkout happening to sit at the cache path (step 4), or on a network clone at
compile time (step 7). `require` pins nothing — there is no version solver,
lockfile or checksum database; the version string only feeds the auto-fetch
checkout.

Fixed this session, both worth keeping in mind for the removal:

- a replace whose target held no `.mx` files resolved to an empty package and
  every imported symbol reported `undefined: X`; it now names the replace
  (`tests/data/badreplace`, phase6 `6s`);
- absolute replace targets were mangled (`JoinPath(base, "/abs")` ->
  `<base>/abs`);
- nested manifests shadow the root's (`findModuleRoot` stops at the first one
  walking up), so musiquay needs the same replace at 12 different depths.

## Design: no modules

Proposal: no `moxie.mod` anywhere. An import path *is* a path under
`$MOXIEPATH`, e.g. `git.smesh.lol/nostr/pkg/event` ->
`$MOXIEPATH/git.smesh.lol/nostr/pkg/event`, with the repo boundary found by
walking up to the nearest `.git` (what `autoFetchRepo` already does). A build
that needs a different revision checks it out and holds a lock so two builds
cannot fight over the same checkout.

What must be built or replaced:

1. **Repo boundary**: make the upward `.git` walk the primary rule and cache
   the repo root per import path; the import path is the clone path. For a
   fresh clone the repo root is the first two components of a dotted import
   path (`git.smesh.lol/nostr` from `git.smesh.lol/nostr/pkg/event`), with an
   explicit override for anything unusual.
2. **Auto-fetch stays**, unchanged in shape: `git clone https://<repo path>`
   into `$MOXIEPATH/<repo path>`. `moxie get` below is the explicit front door
   to the same machinery, so a build and a manual fetch can never disagree.
3. **Version selection** lives in a single mod-style **`MANIFEST`** file at the
   build root — see "Versioning: the MANIFEST file" below. No `MANIFEST`, or no
   entry for a repo, means "whatever is checked out".
4. **Concurrency**: a per-repo exclusive lock while checking out a different
   ref, and *either* a shared lock held for the whole compile *or* per-ref
   worktrees (`git worktree add` from a bare mirror, e.g.
   `$MOXIEPATH/.refs/<repo>/<ref>`). Worktrees are the better answer: distinct
   refs get distinct directories, so builds do not serialize and no build sees
   its dependency change underneath it. The current `.lock` is fatal on
   contention rather than waiting, so at minimum it must become a real wait.
5. **Co-development**: a modified sibling repo must live at its `MOXIEPATH`
   path; in this layout it already does, so editing `nostr` and building
   `musiquay` just works with no `replace`. CI must clone into a clean
   `MOXIEPATH` instead of building in a working tree.
6. **The toolchain's own repo**: the compiler imports
   `git.smesh.lol/moxie/pkg/...`. Without the module-prefix rule that resolves
   through `$MOXIEPATH` too, so the toolchain checkout must sit at
   `$MOXIEPATH/git.smesh.lol/moxie` (it does). Either document that as a layout
   invariant or keep one explicit toolchain-prefix rule for `$MOXIEROOT`.
7. **Stdlib and vendor**: unchanged (`$MOXIEROOT/src`, `$MOXIEROOT/src/vendor`).
8. **`moxie test`'s package identity**: the test harness maps its synthetic
   `main` back to the package's real path for restriction checks
   (`restrictPath`, `bst.testSrcPkgPath`), and that value comes from module
   discovery. It needs a replacement derived from `$MOXIEROOT`/`$MOXIEPATH`.

Code to delete, in both compilers: `findModuleRoot`, `parseReplaceLine`,
`parseModRequires`, `mergeModReplaces`, `globalModPrefix`/`globalModDir` and the
module-relative resolution step, plus the `moxie.mod` files in every repo and
the `replace` lines in the Makefiles. In their place: one `MANIFEST` reader at
the build root (repo -> ref), a ref materialiser (`worktree add` under
`$MOXIEPATH/.refs/`), and one waiting per-repo lock. `tests/data/badreplace` and
phase6 `6s` go with the replace code; the other fixtures (`maplit`,
`untypedvar`, `pushnil`, `aeaddep`) do not use manifests.

### Versioning: the MANIFEST file

Without modules nothing holds a version, so there is one small manifest-style
file at the **build root**, named `MANIFEST`, holding a ref per repository:

```
# MANIFEST
require (
	git.smesh.lol/nostr v1.2.3
	git.smesh.lol/morly dev
)
```

- **Mod-style syntax, pin semantics.** `require` with a version per repo, plus
  `replace` for a local override; the file is the one place a version is
  written. There is no module graph, no minimum-version selection, no
  transitive requirement, no checksum database — a line is a pin, nothing more.
  A ref may be a tag, a branch, or a commit; a tag is preferred because it
  names one commit.
- **One ref per repo, so a tag can be re-pinned but two versions cannot
  coexist.** Two repositories, or one repo at two refs, is not expressible and
  is not supported: the layout is one checkout per repo. Duplicate lines for
  the same repo are an error naming both.
- **The manifest is the lock.** There is no second file: to freeze a moving
  branch, `moxie get -pin` rewrites that line to the commit the branch resolved
  to (or to the tag). Nothing resolves a version at compile time that is not in
  this file.
- **Only the build root's MANIFEST is read.** Nested manifests do not shadow it
  — that was the `moxie.mod` failure mode (`findModuleRoot` stops at the first
  manifest walking up, so musiquay needed the same replace at 12 depths). A
  library's MANIFEST is ignored; the root build decides every version.
- **Refs are materialised, not checked out in place.** The main checkout at
  `$MOXIEPATH/<repo-path>` is the mirror; a ref goes to
  `$MOXIEPATH/.refs/<repo>/<ref>` via `git worktree add`, so a build never
  mutates what a developer is editing and two builds cannot fight over one
  tree. A repo with no MANIFEST entry resolves to the main checkout itself,
  which is the co-development path (edit `nostr`, build `musiquay`).
- **Toolchain imports stay unversioned.** `git.smesh.lol/moxie/pkg/...`
  resolves once against `$MOXIEROOT`: the compiler cannot import a revision of
  itself other than the one it is built from, and `MANIFEST` never names it.
- **Repo boundary is unchanged**: the first two components of a dotted import
  path (`git.smesh.lol/nostr` from `git.smesh.lol/nostr/pkg/event`), confirmed
  by walking up to `.git`.

`moxie get <import-path>[@<ref>]` writes and honours these lines, so a manual
fetch and a build resolution cannot disagree. `moxie env` prints the manifest
path, every manifest entry, and the directory each resolved to.

### Build cache keyed by repo and ref

A dependency at a pinned ref is immutable, so compiling it again is pure waste:
the build cache must be keyed by **repo + resolved commit**, not by the
directories a build happens to run in, and a cache hit means **linking the
cached objects, not recompiling**.

- **Key**: repo path, resolved commit (not the tag — the tag can move, the
  commit cannot), target triple, and the identity of the compiler + baked
  runtime that produced the objects. Any element changing is a miss; that is
  what keeps a compiler change from reusing stale objects, the same hazard `-a`
  exists for today.
- **Layout**: `$MOXIEPATH/.build/<repo>/<commit>/<triple>/…` holding the
  per-package object and `.bc` files the linker and IR emitter already consume.
  Worktrees under `$MOXIEPATH/.refs/` become pure sources: nothing is written
  into them by a build.
- **A hit skips the compile and goes straight to the link**, which is the whole
  point: a tag already built anywhere on the machine is a link-only cost.
- **Consequence for MANIFEST**: a tag or commit pins a stable commit and hits
  the cache; a branch re-resolves to a new commit and misses. One more reason
  `require` lines name tags.
- **Local edits bypass it**: a repo with no manifest entry is the checkout
  being edited, whose objects must be rebuilt when it changes. A dirty worktree
  is never a cache key.

### `moxie get`

One command that fetches, places and checks out a repository, so nobody has to
know where the tree lives or clone it by hand. It is the explicit front door to
the same resolution step a build uses.

```
moxie get <import-path>[@<ref>] [<import-path>[@<ref>] ...]
moxie get                       # materialise and install every MANIFEST entry
```

- **Placement**: `$MOXIEPATH/<repo-path>` (`$MOXIEPATH` defaults to
  `$HOME/moxie`; the toolchain tree `$MOXIEROOT` is a different thing and is
  never written to). The repo path is the import path truncated to its repo
  root: `git.smesh.lol/nostr/pkg/event@v1.2.3` -> repo `git.smesh.lol/nostr`,
  package `pkg/event`.
- **Fetch**: clone when the directory is absent, otherwise `fetch --tags
  --prune`; `-offline` skips the network and fails if the ref is unknown.
- **Ref**: a tag, branch, or commit. `@` splits at the *last* `@`. Tags and
  commits check out detached; a branch checks out and fast-forwards unless
  `-no-pull`. With no ref, a clone stays on its default branch and an existing
  tree is left alone unless `-u`.
- **Dirty trees are never discarded**: checking out a different ref refuses
  while the tree has changes, naming them, unless `-force`.
- **Concurrency**: an exclusive per-repo lock is held for the checkout, and a
  waiting caller prints that it is waiting rather than dying (today's lock
  fatals on contention, which killed a parallel build). `-worktree`
  materialises the ref with `git worktree add` under
  `$MOXIEPATH/.refs/<repo>/<ref>` and prints that path, leaving the main
  checkout untouched — the way two builds can want two refs at once.
- **Pinning**: `-pin` adds or rewrites a `require` line in the build root's
  `MANIFEST` — `<repo> <ref>` as given, or the current HEAD commit when no ref
  is given; a branch becomes the commit it resolved to, so a moving branch is
  frozen. `moxie get` with no arguments reads `MANIFEST` and materialises every
  entry. All of this is the same file the compiler reads at build time, so the
  pin and the build agree by construction.
- **Remotes**: `https://<repo-path>` by default, `-remote <url>` to override
  (an ssh remote, a mirror, a local path), `-protocol ssh|https` for the
  common case.
- **Output**: the resolved directory on stdout so scripts can use it, progress
  and errors on stderr; non-zero exit with a name-and-reason message for an
  unknown ref, a dirty tree, a fetch failure, or a lock timeout.
- `moxie env` should print the resolved `MOXIEPATH`, each repo's checkout path
  and ref, so "why is it building that version" has an answer.

Sequencing: after the rule-parity work (queue item 2), because both compilers
change either way, and the module code is bilateral too. `moxie get` can land
first inside that work, since it needs no module removal to be useful — it is
just the current auto-fetch with a ref argument and a real lock, exposed as a
command.

What it does not fix: the `/pkg/` exemption (language rules, a separate axis
from resolution) and the legacy/stage4 rule gap. It does remove the temptation
to use `/pkg/` as a resolution device, which strengthens the case for an
explicit exemption declaration.

## Environment facts

- Build: `MOXIEROOT=/home/mleku/moxie/git.smesh.lol/moxie ./moxie build -a -o out ./_mxc_stage4`;
  full bootstrap `./build.sh` (needs `moxie-new` absent or it bootstraps from a
  stale binary); legacy needs `PATH=/tmp/moxie-goroot/bin:$PATH GOROOT=/tmp/moxie-goroot`.
- Tests: `tests/run.sh --full` (155 checks, both compilers);
  `cd nostr && make test`; `cd musiquay && make test` (unit, harnesses, signer,
  12 playwright smoke on port 3401); `cd morly && make test-unit`.
- The compiler's module cache is `$HOME/moxie` (`MOXIEPATH` overrides), the
  stdlib is `$MOXIEROOT/src`.
- Concurrent compiles against the same `MOXIEPATH` serialise on a per-repository
  lock file (`$MOXIEPATH/<repo>.lock`, holder pid inside): a second build waits
  and prints whose lock it is waiting for, a lock whose holder is gone is broken
  after a second, and every path releases on exit.
- The relay serves the app with `ORLY_STATIC_DIR=<musiquay>/web/static`; the
  smoke suite starts its own relay, so a stray listener on the test port fails
  the fixture on purpose.

## Module-file audit (what still reads a module file)

An exhaustive sweep of `moxie/legacy/`, the four live repos and the eight older
ones, with probes rather than recollection. `moxie.mod` is gone from the four
live repos and stage4 has no module machinery left at all; what remains:

**legacy is the lagging side, and it is not just the reader.** `legacy/mxlist.go`
still walks up for `moxie.mod`/`go.mod` (`findModFile`, `readModInfo`,
`parseGoMod`, `readModRequires`, `readModReplaces`), and - the part that
actually blocks the removal - legacy's `resolveDir` has **no `$MOXIEPATH/<repo>`
layout fallback**. Proven: with `morly/go.mod` moved aside, `legacy/moxie build
./pkg/access` fails at `resolving "git.smesh.lol/nostr/pkg/event": package not
found (not in stdlib, module, or cache)`, while stage4 resolves the same tree by
layout. `nostr`'s own packages do resolve without a module file (self-imports
via `moduleFromRoots`), so the gap is exactly the cross-repo import.

Legacy also cannot start without a Go toolchain: `legacy/goenv/goenv.go` shells
out to `go env -json` (`could not find 'go' command` with Go off `PATH`). The
`legacy/go.mod`/`go.sum` and the `llvmpure`/`probe` module files are Go build
inputs, not Moxie ones, and stay until that dependency is ported.

**A bilateral language bug sits in the way of any legacy end-to-end test.**
legacy rejects `nostr/pkg/lol/errorf/errorf.mx:8-13` - `[]fmt.Stringer` passed
where `fmt.Stringer` is wanted - and stage4 compiles the same file cleanly.
Until legacy agrees, "legacy builds it" cannot be used as evidence.

**Cross-repo resolution gaps that are not about module files.** `nostr/pkg/core`
imports `git.smesh.lol/musiquay/web/common/helpers`, which no `go.mod` replace
ever covered and the layout rule will. `iskra` (28 files) and `transdb`
(16 files) import `git.smesh.lol/iskradb/lattice`; `gio-demo` imports
`git.smesh.lol/gio`; `transdb` imports the bare root `git.smesh.lol/iskra`.

**Files still on disk:** `go.mod` in `nostr`, `morly`, `musiquay`, `smesh`, and
vestigial Go ones in `gnarl-hamadryad`, `iskra/go-ref`,
`iskra/tools/mx-transpile`, `zilch`; `moxie.mod` in `gio`, `gio-demo`,
`iskra` (+3 under `cmd/`), `iskradb`, `transdb`, `smesh` (+13 nested under
`web/`), `musiquay/web/**` (11 nested; note `web/common/moxie.mod` and every
`web/wasm/*/moxie.mod` declare the *sub-directory* as their own module, which is
already inconsistent with the layout rule), `gnarl-hamadryad/moxie`.

**`smesh/Makefile:179`** derives `COVER_PREFIX` with
`sed -n 's/^module[[:space:]]*//p' moxie.mod`. Deleting the file yields an empty
prefix, and `pkg/mxcover/report.mx:257` treats empty as *no filter*, so `make
cover` would report runtime and stdlib sites too. Replace with the layout value
(`git.smesh.lol/$(notdir $(CURDIR))/`).

**Order to finish it:** (1) give legacy the `$MOXIEPATH/<repo>` + `.refs` pin
resolution stage4 already has; (2) collapse legacy's `readModInfo` onto
`moduleFromRoots` and delete the module readers and the `requires`/`replaces`
plumbing; (3) rewrite `legacy/cover/cover.go` `ModulePrefix` the same way;
(4) delete legacy's `fetch`/`vendor` commands and their files; (5) fix the
`errorf.mx` divergence bilaterally; (6) delete the sibling module files repo by
repo with a build check each; (7) docs. Two decisions are outstanding and are
not mine to take: the `.mxh` API-stability gate
(`legacy/header.go:92`, `legacy/compiler/apicheck.go:68`) reads
`ParseMoxieModVersion`, so deleting the files silently removes the major-version
escape hatch; and whether the older repos are in scope now or only the four.

## Protocol wiring: what is there and what it needs

`musiquay/pkg/protocol` (6278 lines, 20 payload types) is imported by nothing.
Every payload has `Tags() [][]string`, `Parse*(tags, content)` and
`Validate() (err error)`; `kind.mx` is the only policy surface, and it is a
taxonomy, not a retention table: `IsAddressable` (32210-32217), `IsRegular`
(3221, 3222, 32218-32220), `IsPrivate` (3222, 32218), `InReplaceableRange`
(30000-39999), `RegularInReplaceableRange()` = exactly {32218, 32219, 32220}.

**The 32218-32220 bug is live and now fixed.** `kind.IsParameterizedReplaceable`
is `30000 <= k < 40000`, so `pipeline.mx` sent delivery receipts, host
aggregates and publisher rollups down the addressable path; none carries a `d`
tag, so they all compared equal and the second record deleted the first. The
relay now consults the protocol first (`pkg/relay/pipeline/policy.mx`,
`countedInReplaceableRange`) and stores them as regular events, leaving 32210's
`d`-tag replacement untouched. Pinned by
`TestIngestCountedKindsInReplaceableRange`: three records for each of 32218,
32219, 32220 all survive, and a 32210 pair on one `d` still collapses to the
newer. morly `pkg/relay/pipeline`: 28 passed.

**The `#x` lookup already exists.** `store/engine.mx` indexes every
single-letter tag into `tc/tkc/tpc/tkp`, so `{"kinds":[32210],"#x":["<sha256>"]}`
answers today and survives a WAL rebuild; what is missing is a named resolution
entry point (`ResolveBlob`) and an ingest-time asset index. No schema change.

**The Blossom serve path is ungated and has no store.** `pkg/blossom/blossom.mx`
serves any 64-hex hash; `wire.BlossomRequest` carries no requester identity, and
the dbengine domain is the only holder of `*store.Engine`, so a gate needs a new
tree op (`OpAsset`) plus a decision in the root server before `doDispatchBlossom`.
The gate order the docs describe is: unresolvable blob -> serve; asset with no
32217 filter -> serve; gated with no `Authorization: Nostr` -> 401; valid
24242 `t=get` with a bloom member -> serve; otherwise 402. `nostr/pkg/httpauth`
implements NIP-98 (27235) and hard-rejects other kinds, so 24242 needs its own
checker.

**App side has no native binary** - `musiquay` is wasm only, and
`nostr/pkg/core.Event.Tags` is already `[][]string`, so the wasm adapter is
nearly free while an `event.E` adapter would drag secp256k1 into every wasm
bundle. Publish path: unsigned JSON -> `signer.SignEvent` -> `routeMsg` ->
relay-proxy `PublishTo` -> `Conn.Publish`; it is copy-pasted a dozen times and
wants one helper plus thin per-payload entry points. Do not add publish paths
for 3222/32218 - they are private and belong inside NIP-59 wraps.

**Decisions this needs:** whether the relay is also the host that gates blobs
(suggested: gate behind an env flag, default off); whether a bare 3222/32218
should be rejected (the protocol says they are never published as-is); the
BUD-11 24242 checker versus widening `httpauth`; and hex case, where
`protocol.IsHex64` is lowercase-only while `blossom.isHex` accepts uppercase.

The protocol's own tests were not in any gate: `musiquay`'s `test-unit` walked
only `web/`, so the 62 tests beside the vocabulary never ran. The loop now walks
`pkg` and `web`; `make test-unit` is 81 passed (protocol 62, marmot 18, mls 1).

Landed since the audit: legacy resolves sibling repositories and MANIFEST pins
by layout (`legacy/loader/mxlist.go`, `layoutRepoDir`); `nostr/go.mod` is
deleted (194 tests pass, and stage4 type-checks `nostr/pkg/core`, whose
cross-repository import no go.mod ever covered); `musiquay/go.mod` and the
eleven nested `web/**/moxie.mod` files are deleted (`make test-unit` 81 passed,
`build-app-wasm` and `build-store-wasm` link). What is left is `morly/go.mod`,
the older repos' module files, and the two decisions above - plus the legacy
language-rule gaps, which are now the only thing standing between legacy and an
end-to-end build of these repos: `pkg/core/tags.mx` is rejected for a named
slice type (`Tag`, `Tags`) that stage4 accepts.

### The relay now enforces the vocabulary, and an `event.Sign` segfault fell out

Landed in morly, on top of the counted-kind fix:

- `pkg/relay/protowire` binds `event.E` to the vocabulary: `TagsOf` copies the
  tag list into `[][]string` with exact presizing, `ValidateEvent` dispatches
  every kind that has a parser, and `AssetOf` answers the asset address a
  blob-bearing event belongs to (first `a` for a track or delivery receipt, the
  asset inside the `d` for an access filter).
- `ingestStageB` step 2b validates Musiquay payloads after the limit checks and
  before the ACL, rejecting with `invalid: <reason>`. The gate is
  `protocol.IsMusiquay`, deliberately: the kinds the vocabulary reuses from a
  NIP (comment 1111, calendar 31923, listing 30402) are shared with clients
  that never heard of this protocol, so Musiquay's stricter parser would turn
  their valid traffic away.
- `store.Engine.ResolveBlob(xHex []byte)` answers the sha256 → asset lookup on
  the existing single-letter `tc` index - newest first, serials deduped, no new
  index family and no on-disk change, so it survives the WAL rebuild.
- Tests: protowire 5, store 24, pipeline 30, `make build` links.

Two things the work exposed:

**A segfault in `event.Sign` when the tags come from the vocabulary.**
Signing an event whose tag rows were produced by `protocol.Track.Tags()` and
re-wrapped into a `*tag.S` - shallow copy and byte-deep copy both - faults
inside `event.Sign`, while the identical wrapping of literal rows signs fine and
`ValidateEvent` reads `Track.Tags()` output without trouble. Only the test path
hit it (a decoded wire event is a different construction), and the pipeline
fixtures were built with the existing `tTag` helper to get past it. Not
explained yet; it is an arena/relocation question, not a protocol one, and it
is the kind of thing the interior-pointer rule predicts (a pointer taken into a
struct that is then copied).

**The validators are much stricter than a tag-shaped fixture.** `Track.Validate`
requires a non-empty `d`; `DeliveryReceipt` requires a well-formed
`a`/`p`/`x`/`mode`/`amount`; `HostAggregate` and `PublisherRollup` require an
asset and a period. The counted-kind test had to grow real payloads for its
receipts, aggregates and rollups - the assertions did not change, the fixtures
became things the protocol admits. Anyone publishing a slightly out-of-spec
payload will now be rejected with a reason, which is the point, but it makes the
protocol's `Validate` the contract for anything a host sends.

Also unanswered by the vocabulary: `ParseStall` and `ParseProduct` take content
alone, and there is no parser at all for 31922, 31924, 30403, 30315, 24242,
10063, 9734, 9735, 13 or 1059, so `ValidateEvent` correctly returns nil for
them. `ParseCalendarEvent`'s doc says 31922 and 31923 share a struct but only
31923 is wired, and the same split exists between 30402 and 30403; neither was
guessed at.

### A `[][]string` built by repeated calls comes back aliased (FIXED)

Found while wiring the app: `protocol.*.Tags()` could not be called at runtime.
**Fixed in `src/runtime/codec.mx`** - see the mechanism and the fix below the
repro. Verified on a fresh bake with the repro and with a real
`protocol.ParseTrack(...).Tags()`: three rows, each reading its own data, on
stage4; the alias probe is correct on legacy too. Pinned by
`tests/regressions/should_compile/nested_slice_return/`.

Minimal repro, native, no test harness (`$MOXIEPATH/git.smesh.lol/aliasprobe`):

```
package pb

func Ret(dst [][]string, k, v string) (out [][]string) {
	out = dst
	t := []string{:2}
	t[0] = k
	t[1] = v
	out = push(out, t)
	return
}

func Three(a, b, c string) (tags [][]string) {
	tags = Ret(nil, "d", a)
	tags = Ret(tags, "title", b)
	tags = Ret(tags, "artist", c)
	return
}
```

`pb.Three("d1","t1","a1")` then `println(r[0][1], r[1][1], r[2][1])` prints
`d1 a1 a1`: rows 1 and 2 carry the *last* row's data. Four calls give
`d1 x1 x1 x1` - every row after the first shows the final value. Two calls
happen to work. The same shape built by one helper that appends every row
itself is fine, so the trigger is the row buffer being allocated in a callee
frame that the next call reclaims (FnArenaPop hands the position to the next
callee), with the returned `[][]string`'s inner rows not deep-copied into the
caller's arena.

Consequence: `protocol.Track.Tags()` returns corrupt rows (it appends
`[][]string` rows through several helpers), so anything that publishes a
protocol payload built from `Tags()` would publish garbage or fault. The
app-side adapter therefore has no test that calls `Tags()`; the publish
wrappers do call it and are correct once this is fixed. Suspected to be the
same family as the committed `map_value_return_reloc` fix, and it is likely a
legacy/stage4 divergence - the return-value codec in `src/runtime/codec.mx`
(`codecRetTransient`) is the place to look first.

### Item 7(a) decision: staged universal migration

Taken this session: **staged, verified per stage.** Measured at decision time -
the named slice/map-type rule alone would hit 74 sites in `src/` outside
`src/runtime` and 16 across the app repos (nostr 3, morly 7, musiquay 6), plus
4 package-level var initializers in `src/`; the runtime keeps its carve-out
(69 global stores). Legacy already rejects named slice/map types while stage4
accepts them, so the two compilers disagree today and the first stage has to
settle which side the rule is on before any site is rewritten.

### App side of the protocol wiring (done)

`musiquay/web/common/protocolwire` renders `[][]string` to JSON
(`TagsJSON`), emits the unsigned event object (`UnsignedJSON`) and reads a
`nostr.Event` back into each payload type (`XFromCore`, kind check then Parse
then Validate; `Stall`/`Product` read content alone because they have no
`Tags()`). `web/wasm/app` gained `publishProtocol` plus wrappers for the
thirteen publishable payloads - no `PurchaseOrder`/`DeliveryReceipt` wrapper,
since the protocol marks them private. Verified: protocolwire 7 passed,
`make build-app-wasm` links, `make test-unit` 88 passed.

**Mechanism.** `codecEncodeValue`'s `KindSlice` branch takes a shortcut when
the backing array is judged stable, and its guard exempted only
`codecTopOnlyTransient` (sovereign store-back). During a return
(`codecRetTransient`, set by both compilers around the result encode) the
caller's backing array counts as stable, so on the second and later calls into
one helper the element walk never ran - and the element just pushed had been
allocated in the helper's own frame, which `FnArenaPop` releases and the pooled
`FnArenaPush` hands to the next callee. Every later row therefore held a header
pointing at one recycled slot, so rows 1..n-1 all read the last call's data.
Row 0 survived only because a nil start lazy-inits the outer backing inside the
callee, which makes it transient on the first return; presize the outer in the
caller and row 0 breaks too. The guard now exempts `codecRetTransient` as well,
so a stable-backed slice whose element type can hold pointers is walked. The
charge is O(len) instead of O(1) for those returns, the same trade-off already
accepted for the sovereign store-back, and leaf buffers still alias through
their own stability tests.

**How the two compilers were involved.** The bug itself is runtime-only and
therefore bilateral by construction: both compilers emit the same
`codecSetRetTransient(true) -> codecEncodeValue -> FnArenaPop -> decode`
sequence (`_mxc_stage4/ir_deepcopy.mx`, `legacy/compiler/compiler.go`) and call
the same `src/runtime/codec.mx`. The legacy binary reproduced `d1 a1 a1`
identically before the fix. The old three-pass `Reloc*` lifecycle in
`src/runtime/relocate.mx` has no call sites in either compiler - `codec.mx`
says outright that the codec primitives replaced it - so the fix stays in the
codec.

The repro also exposed the reason the corpus of "vestigial" blank imports was
not vestigial to legacy: `legacy/compiler/symbol.go` enabled `//:linkname` only
when the package imported `"unsafe"` (`hasUnsafeImport(f.Pkg.Pkg)`), so
deleting `import _ "unsafe"` from `src/math/rand/rand.mx` made the legacy
bootstrap fail with `linker could not find symbol math/rand.runtime_rand`.
Stage4 has no such gate - `scanLinknameMap` reads the pragma directly - so the
gate was the divergence, not the pragma. Both legacy sites (function and global
linkname) now honour `//:linkname` unconditionally, which is what made the
eight blank imports genuinely removable: `src/internal/cpu/cpu.mx`,
`cpu_arm64_hwcap.mx`, `internal/runtime/atomic/atomic_andor_generic.mx`,
`math/big/arith_decl.mx`, `math/rand/rand.mx`, `net/http/roundtrip.mx`,
`syscall/linkname_unix.mx` (`import _ "unsafe"`) and
`crypto/ec/secp256k1/precomps.mx` (`import _ "embed"`). With those gone the
stdlib parses clean, so queue item 4 is closed.

### Stage 1 of item 7(a): the direction is not mechanical (measured)

The approved plan's first step was to settle which side the named slice/map-type
rule is on. Probes, both run on a dotted-path package under `$MOXIEPATH` with a
named slice type and an initialised package var:

- **stage4 accepts both.** It has no implementation of either rule at all - not
  a scope difference. `moxie build` returns 0 and the program runs.
- **legacy rejects both**, with `moxie: named slice type 'Tags' is not allowed:
  use []T directly` and `moxie: package-level var with initializer is not
  allowed: use zero-value declaration and init()`.
- **legacy checks dependency packages too**, not just the build root: a `main`
  package importing a library with `type Tags []string` fails on the library's
  line. So switching stage4 on means every package in every repo, not only
  targets.

The cost divides into two very different migrations:

- **Package-level var initializers** - 6 in nostr, 13 in morly, 20 in musiquay
  (39, the number the finding already recorded; it is the var sites, not the
  named types), scattered through packages, tests and `_test/` harnesses. All
  are convertible to a zero-value declaration plus an assignment in `init()`,
  and that is the model's preferred shape anyway, so migrating them is
  behaviour-preserving work that no direction change can invalidate.
  **Started this round.**
- **Named slice/map types** - 16 in the app repos (`nostr/pkg/core/tags.mx`
  `Tag []string` and `Tags []Tag`, `nostr/pkg/normalize` `Reason []byte`, seven
  `...List` channel slices in `morly/pkg/relay/server/server.mx`, and six in
  `musiquay/web/common/mls`: `ratchetLabel`, `secretTree`, `proposalRef`,
  `GroupID`, `KeyPackageRef`, `ratchetTree`), plus 74 across 49 stdlib files.
  This one is **not** mechanical: `nostr.Tag` and `nostr.Tags` carry the tag API
  (`Key`, `Value`, `Relay`, `Marker`, `GetFirst`, `GetAll`, `GetD`,
  `ContainsValue`), they are referenced 68 times across 9 files in all three
  repos, and `[]T directly` has no methods. Enforcing the rule means dissolving
  that API into free functions. That should be confirmed before it is done,
  because the model elsewhere *depends* on named types with methods - builtin
  Stringers are exactly that (`byte`/`rune` are named types over `uint8`/
  `int32`, and the Stringer design says a named type's own `String()` wins).
  The alternative reading of the rule is that it is legacy's leftover and
  should be dropped, which costs no sites at all.

**Stage 1 landed: the package-level var initializers are gone from the app
repos.** The real set was far larger than the 6/13/20 a flat grep suggested -
**220 declarations** (nostr 36, morly 80, musiquay 104) once `var (...)` blocks
and the demoted `var x = []byte("literal")` form are counted. All are now a
zero-value declaration plus an assignment in `init()`, in dependency order where
one initializer read another (`lol.Main` before its printers; the metrics
histograms before `allHistograms`). Where a package is a `main` - morly's
`main.mx`, musiquay's wasm bundles and the two `_test/` harnesses - the
assignments live in an `initXxxGlobals()` called first in `main()`, which is
exactly when the old package initializer ran, because a main package cannot
declare `init()`. The mls package shares one `init()` in `encoding.mx`; its four
`!wasm` test globals became function-local instead, since the single `init()`
must live in an always-compiled file. Committed in all three repos and verified
independently here: **653 tests green** (nostr 194, morly 370, musiquay 89),
morly links, `app.wasm` links.

**Direction for the named-type half: enforce it as written** (user decision,
taken with the cost measured). Stage4 has no implementation of the rule at all -
a `main` package under a dotted path with `type Tags []string` and a method
builds and runs - while legacy rejects it everywhere, dependencies included, so
"settle which side" resolves to legacy's. The blanket rule costs 75 named
slice/map types in `src/` outside `src/runtime` and 16 in the app repos, and 63
of the 75 and 8 of the 16 declare methods, which `[]T directly` cannot carry:
`nostr.Tag`/`Tags` (8 methods, 68 references in 9 files), `normalize.Reason`,
`mls.ratchetTree` (25 methods), `mls.secretTree`, `math/big.BigInt` and the
rest all dissolve into free functions. The app-repo half is in flight; stage4's
implementation of both rules and the stdlib's 75 are the stages after it.

**Stage 1's compiler half landed (`3b13de5e`).** Stage4 had no implementation of
either declaration rule, so `main` with `type Tags []string` and
`var counter = 5` built under stage4 and was rejected by legacy, dependencies
included. `checkPackageDecls` now mirrors legacy behind the same gate as the
other restriction checks, with legacy's two refinements kept so the compilers
agree on every input: an alias introduces no new named type (`type X = []T` is
exempt in both), and a var whose value is a `[]byte(...)` conversion of a
string literal is the form the loader demotes to a constant and legacy allows.

**One gate bug fixed on the legacy side while doing it.** Its package-decl rules
read the *directory* path, so the same user program was checked from
`/tmp/moxie-tests.123` and exempted from `/tmp/moxie-tests-123` - the rule
depended on whether the temp directory had a dot in it. A `main` package is now
user code whatever directory it sits in, which is what stage4 already means by
the package path. That is also why the suite could not pin these rules before:
every fixture is copied into a dot-free temp directory.

**Fixtures.** `declvar` and `namedslice` pin both rejections in both compilers
(phase6 6x.1/6x.2); `globalinit` and `shiftconst` lost their package-level
initializers - a main package cannot declare `init()` and a global may not be
assigned in `main()`, so the values are assigned in an init-named helper called
first, which is exactly when the old initializer ran; `multi_value_var` keeps
the demoted-string form at package scope and moves the numeric pairs into
`main`; `named_slice_method.mx` is deleted because its subject, a named slice
type with a method, is now illegal in user code (the stdlib still exercises the
same slice-typing path, and the bootstrap covers it).

**Suite 171/171, 0 failures**, including both new checks in both compilers and
`selfhost:stage2`. Still open in this stage: the app repos' 16 named slice/map
types are being dissolved into free functions (in flight), then the `/pkg/`
carve-out can go for the app repos, and the stdlib's 75 plus the global-stores
scope expansion (107 sites above the runtime) are the stages after that.

Operational note for the next round: the verified stage4 binary of `3b13de5e`
is at `/tmp/mxc-rules2` (that is what the 171/171 run used). The in-tree
`./moxie` could not be replaced because the named-type migration was building
against it (`Text file busy`); copy it over once that stops.

**What dropping the `/pkg/` carve-out is actually blocked on (measured).**
Deleting the `/pkg/` exemption from `isUserPackagePath` and self-building the
compiler with the result fails at **16 closure-capture violations in
`pkg/syntax`** - `(*Parser).appendGroup__anon1` captures `f, g, list`,
`argList__anon1` captures `list, p, hasDots`, and fourteen more - with no
`immutable outside init` errors at all in the compiler tree. So the carve-out is
load-bearing through the *closure* rule, not the declaration rules (which the
compiler tree satisfies) and not the global-store rule as far as this build
shows. The order is therefore: give `pkg/syntax`'s sixteen closures explicit
parameters (which is the model's own rule anyway), then drop the carve-out for
the compiler and the app repos together, then the stdlib (75 named types and its
share of the global stores), and the runtime carve-out stays.

**The sixteen `pkg/syntax` captures are two different things.** Seven of them
are not source closures at all: `fileOrNil__mval1..4` and `stmtOrNil__mval1..3`
are wrappers `ssa_builder.mx` generates itself (`buildMethodValue`, the
`__mval` name at `ssa_builder.mx:5528`) for a **bound method value**, and the
wrapper's free variable is `recv`. A bound method value *is* a capture - the
receiver is the environment - so the compiler is emitting code that its own
capture rule rejects. The lowering needs to resolve a selector that is being
called directly without materialising a bound value first, or a bound method
value has to be rejected by name in user code, because a Moxie function value
carries a code pointer and no environment and so cannot represent one. The
other nine (`appendGroup`, `argList`, `complitexpr`, `init`, `initBytes`,
`interfaceType`, `paramList`, `structType`, `trace`) are real source closures
and want explicit parameters.

Confirmed by probe: in user code `f := t.get` (a bound method value) is
rejected with `main__mval1: closure captures outer variable(s) [recv]`, so the
value form is already refused - the sixteen sites are the compiler
materialising a bound value for a selector it is about to call directly
(`defer p.trace("file")()` in `pkg/syntax`). A direct-call lowering would
remove seven of them without touching the parser.

**Where the `__mval` wrappers are made.** `buildMethodValue` has exactly one
caller, `buildSelector` (`ssa_builder.mx:5473`), so a wrapper appears only when
a selector is evaluated *as a value*. `fileOrNil`'s only selector is `p.trace`
in `defer p.trace("file")()`, and its four wrappers are named
`fileOrNil__mval1..4` - so the deferred call-of-a-call path is evaluating the
inner function expression through `buildExpr`/`buildSelector` instead of
routing the inner call through `buildCall`, whose selector branch already
resolves `recv.M(...)` directly. Instrumenting that branch (or teaching
`buildDeferStmt` about a CallExpr fun) is the next step; with the wrappers gone,
nine real source closures in `pkg/syntax` are all that stands between the
compiler tree and the `/pkg/` carve-out.

**Stage 1's app-repo half landed** (nostr `0ece20e`, morly `5fa4e19`, musiquay
`a22b643`). All 16 named slice/map types are gone from the three app repos, so
`grep -rE '^type [A-Za-z_]+ (\[\]|map\[)'` now returns nothing there. Each type
became free functions whose first parameter is the value: the nostr tag API is
`TagKey`/`TagValue`/`TagRelay`/`TagMarker`/`TagsGetFirst`/`TagsGetAll`/`TagsGetD`/
`TagsContainsValue` over `[]string` and `[][]string` (68 references updated),
`normalize.Reason` is `[]byte`, morly's seven `...List` channel slices are their
underlying `[]chan T`, and the mls types - including `ratchetTree` with its 25
methods - are free functions. Verified with the compiler that enforces the rule
(`3b13de5e`, installed as `./moxie`): nostr 194 tests, musiquay 89 tests,
`make build-app-wasm` links, 0 failures, and morly's 370 across 26 packages are green.

**Correction, with instrumentation: all sixteen are real source captures.** A
temporary log in `buildSelector` (`MVDBG fn=... sel=...`) shows the seven
`__mval` wrappers come from `pkg/syntax/parse_decl.mx:47-59` -
`p.appendGroup(f.DeclList, p.importDecl)` and its three siblings pass a **bound
method value**, which is a capture by definition, and `parse_stmt.mx:192` does
the same. The compiler is right to reject them; there is no lowering bug, and
the earlier note blaming `buildDeferStmt` is withdrawn. `appendGroup` itself
also captures (`list`, `f`, `g`) in the closure it hands to `p.list`, so the fix
is a signature change: pass a declaration kind and dispatch inside, and either
give `p.list` explicit state parameters or inline its loop, because a Moxie
function value carries no environment for the closure to read. The other nine
are ordinary source closures (`argList`, `complitexpr`, `init`, `initBytes`,
`interfaceType`, `paramList`, `structType`, `trace`) and want the same
treatment.

**Seven of the sixteen captures are gone** (`f2ef194b`). `appendGroup` and
`declStmt` now take a small declaration-kind enum and dispatch through
`declOfKind` instead of receiving `p.importDecl` / `p.varDecl` / ... as bound
method values, and `appendGroup`'s loop is inlined so it no longer hands
`p.list` a closure that reads `list` and `g` from the caller's frame. That is
eight of the sixteen (seven method values plus the closure). Verified: the
compiler builds - it parses its own source with the new parser - a probe with
grouped `import`/`const`/`type` at package scope and grouped `const`/`var`
inside a function prints `ok 33 6 1`, and the suite is **171/171 with 0
failures**, bootstrap included. The remaining eight are ordinary source
closures in `pkg/syntax` (`argList`, `complitexpr`, `init`, `initBytes`,
`interfaceType`, `paramList`, `structType`, `trace`) and want explicit
parameters.

**The trace closure is gone too** (`8fbe23f1`). `p.trace` returned a `func()`
that captured the parser and the message, so all 47 `defer p.trace("x")()` sites
were captures in disguise. It now returns the indent length it replaced and
`traceEnd(prev)` restores it, so each site reads
`defer p.traceEnd(p.trace("x"))` - the argument is still evaluated at defer
time and the recover/panic behaviour is unchanged. Compiler builds; suite
**171/171, 0 failures**. Nine of the sixteen are now gone (appendGroup's method
value and closure, the seven `__mval` method values, and trace); the remaining
seven are `argList`, `complitexpr`, `init`, `initBytes`, `interfaceType`,
`paramList` and `structType`.

**Four more closures are gone** (`846788c5`). `p.list` took a `func() bool`
callback, so every caller handed it a closure over its own locals. `listSep`
now consumes the separator and reports whether the list continues, and
`argList`, `complitexpr`, `structType` and `interfaceType` drive their own
loops. Verified: compiler builds, a probe covering struct and interface
declarations, nested composite literals and a variadic argument list prints
`point 7 n 2 area 25 square sum 10`, and the suite is **171/171, 0 failures**.
Thirteen of the sixteen are gone; `paramList`, `init` and `initBytes` remain.

**paramList is inlined too** (`583f7258`), the last `p.list` closure: the
callback captured `name`, `typ`, `named`, `typed` and `list`. `listSep`
advances exactly as `p.list` did, so the position `p.list` returned survives as
`end` on both the success and the unrecoverable-separator paths, and the closer
is consumed only when the separator did not fail. Compiler builds; suite
**171/171, 0 failures**. Fourteen of the sixteen are gone; only the `init` and
`initBytes` scanner callbacks remain, and they need the same shape one level
down: `Scanner.Init`/`InitBytes` take `errh func(line, col uint32, msg string)`
and hand it to `Source.init`, where the only implementation is a closure over
`p`. Storing the owning `*Parser` on the Scanner/Source and calling
`p.scanError(...)` directly removes both without inventing a function value
that would capture.

**All sixteen captures are gone** (`5ecd1740`). The scanner no longer takes an
error-handler function: `Scanner`/`Source` hold the owning `*Parser` and call
`p.scanError(line, col, msg)` directly, and the closure body is that method
(`Pragh` became `p.Pragh`); `ErrorAtf` routes the same way. The two-stage build
(a compiler built with the `/pkg/` exemption removed rebuilding the tree)
reports **zero closure captures** in `pkg/syntax`, and the suite is
**171/171, 0 failures**. The carve-out is not yet removable for two other
reasons, both in `pkg/syntax` and only when it is treated as user code:
`Pos undefined (type git.smesh.lol/moxie/pkg/syntax.AssignStmt has no field or
method Pos)` and `not enough arguments in call`. Both need positions - stage4's
`compileErrors` carry bare strings and the log shows neither line - so the next
step is to instrument `checkNamedReturns`/the type-checker error path to print
the enclosing function, or to bisect by compiling `pkg/syntax` alone with the
exemption removed.

**Narrowing the two remaining errors.** `pkg/syntax/nodes.mx` has
`type stmt struct{ node }`, `type simpleStmt struct{ stmt }`,
`type AssignStmt struct{ ...; simpleStmt }`, and `func (n *node) Pos()` - a
pointer receiver, so `Pos` is promoted to `*AssignStmt` but not to the value
`AssignStmt`. The error text names the value type, so the failing selector is
on an addressable value: Go takes the address for such a selector, and stage4
does too while `pkg/syntax` is exempt, but not when it is user code. No
`value receiver is not allowed` error is emitted for it, so
`registerMethod`'s early return is not the cause - the difference is in
inherited-method lookup for value receivers, and the next step is to make the
"no field or method" report name the receiver type and whether the package was
treated as user code (the message is built where the lookup fails). `pkg/types`
does not reference the user-path predicate, so the gate itself is not there.
The second error, `not enough arguments in call`, is likely a cascade of the
first: a call whose callee expression failed to resolve.

**The carve-out now waits only on `pkg/types`, and its globals are already
marked deprecated.** The two-stage build reports nine stores, all in
`pkg/types/registry.mx` and `pkg/types/tc_universe.mx`:
`SetUniverseGlobals` (6), `LookupImportByName` (2), `DirectImportPaths` (1).
The file's own comment says what they are: "Global bridge variables -
DEPRECATED. These exist only for the legacy compiler's codegen which reads them
directly. Stage4 code accesses these through cctx.universe.Registry /
cctx.universe.DirectImports", and it names the replacements
(`UniverseState.LookupByName`, `UniverseState.DirectImportPaths`, which already
exists and is the twin of the global). So this is a deletion plus routing job,
not a sovereign-holder migration: the callers are `_mxc_stage4/main.mx`
(`SetUniverseGlobals`), `_mxc_stage4/ir_descriptors.mx` and
`pkg/types/tc_types.mx` (`ImportRegistry`), and `pkg/rewrite/resolve.mx` has its
own separate `ImportRegistry` global that the same sweep should cover.

Also this round: `(*Parser).header` read `a.Pos()` on an asserted `*AssignStmt`
where stage4 resolved the selector against the value type; it reads `a.pos`
directly now (`fecf1c12`). Suite **171/171, 0 failures** after clearing
`~/.cache/moxie`, whose object files had been reaped and left 213 stale
`.c.lock` files that failed `aeaddep:legacy` with `cannot open ... .bc`.

**The `/pkg/` carve-out is gone** (`04331d6b`). `isUserPackagePath` no longer
exempts paths containing `/pkg/`. Getting there took the sixteen `pkg/syntax`
captures, the `(*Parser).header` value receiver, and the deprecated `pkg/types`
bridges: `LookupImportByName`, `ImportByName`, the global `DirectImportPaths`
and the `EnsureImportRegistry` stub are deleted (they had no callers), the
universe and registry globals are written only from `initUniverseGlobals`
(the immutability rule's own init-named exemption, with `SetUniverseGlobals` as
the exported wrapper because `pkg/types` is dot-imported), and
`extractTypeArgs` returns its names instead of pushing them through a callback
that captured the caller's accumulator and map. The two-stage build - a
compiler built with the exemption removed rebuilding the tree - is clean, and
the suite is **171/171, 0 failures**.

**Consequence, and the next stage: the app repos were relying on the
carve-out.** With it gone, stage4 enforces every rule on `git.smesh.lol/*/pkg/`
as well, and `nostr` stops at `pkg/signer/p8k` with five
`unnamed return values are not allowed`. The queue already recorded this debt:
"the last two all under `/pkg/` and therefore still exempt (nostr `make test`
passes, 13 targets)" - nostr's 36 unnamed returns were never migrated because
`/pkg/` exempted them. morly and musiquay pass `-allow-closure-captures`, so
their closures stay legal, but their unnamed returns and any global stores in
`pkg/` now count too. The app-repo migration is the next round's work, and it
is the same shape as the earlier 220-initialiser sweep.

**App-repo debt: the enumeration trick and the first package.** The
carve-out removal surfaced nostr's debt at `pkg/signer/p8k`, and the fix is
mechanical but needs positions - stage4 reports `unnamed return values are not
allowed` with no line number, while **legacy reports `file:line:col`**, so the
work list comes from `legacy/moxie build ./pkg/<p>` and the fix is applied
against it. p8k's five (New, Sign, Verify, ECDH, ECDHRaw) are named and its
nine tests pass (`53d8512`); the remaining nostr signatures are delegated. Note
that legacy reports **10** unnamed results in p8k where stage4 reported 5 - it
names each result, stage4 counts each function once.

The same sweep reaches morly (68 measured under `pkg/`) and musiquay (its
unnamed returns were migrated earlier because its packages live under `web/`,
but its `pkg/` global stores and any closure captures now count; its Makefile
passes `-allow-closure-captures`). `internal/cpu`'s 36 stores are already
exempt through the `initDo` rename (`3a47951e`), so the stdlib stage starts
from ~71 stores and 75 named slice/map types across 49 files.

**The nostr work list, enumerated.** Stage4 has no positions, so the loop is
`for d in $(find pkg -name '*.mx' | xargs -n1 dirname | sort -u); do moxie build
./$d; done` for the package-by-package failures and the legacy compiler for the
positions (`/tmp/nostr-s4.txt`, `/tmp/nostr-unnamed.txt`). Three kinds show up,
not one:
1. unnamed return values across most packages (core, hex, ints, tag, filter,
   envelope, event, varint, timestamp, crypto/nip44 six, crypto/
   chacha20poly1305 two, crypto/aes256gcm two, ...);
2. **value receivers** - `pkg/lol/log` reports three `value receiver is not
   allowed: use pointer receiver (*Printer)`;
3. **sovereign-loop declarations** - `pkg/core`'s
   `(*Reader).readContinuedLineSlice` declares `:=` inside a loop in a
   self-mutating method, which the rule sends to a helper function.

The enumeration also reported `net/textproto`'s `dotWriter.Write` and
`dotReader.Read` for the same sovereign-loop rule. That check is **not gated**
by `isUserPackagePath` (`checkSovereignLoopDecls` runs for every package), so
the stdlib carries that violation independently of the carve-out; it surfaced
here only because a nostr package's import closure reaches it. It is a stdlib
item for the next stage, not nostr debt.

**The ungated sovereign-loop rule has stdlib debt too.** Since
`checkSovereignLoopDecls` is not gated by `isUserPackagePath`, every stdlib
package with a declaration inside a loop in a self-mutating method fails as
soon as anything compiles it - which is why nostr's build stopped at
`net/textproto`. `src/net/textproto` is fixed (`2b2a1bd4`: `dotWriter.Write`,
`dotReader.Read`, `(*Reader).readContinuedLineSlice` and `skipSpace` declare
their loop scratch once, before the loop - behaviour-identical, and the scratch
is allocated once instead of per iteration in the sovereign arena). A sweep
over every stdlib package is running and has already flagged more, including
`hash/crc32`'s `(*CRC32).archInitCastagnoli` and several decoder readers
(`bitReader.ReadBits64`, `readFromBlock`, `(*reader).read`). Each is the same
two-line hoist. The list lands in `/tmp/sovloop.txt`; it is stdlib work for the
next stage, but it blocks any build whose closure reaches one of them, so it
comes before the named-type and global-store sweeps.

**nostr is migrated and green** (`1a0acf5`). 23 files, all under `pkg/`: 31
named result lists, 12 value receivers in `pkg/types`, `kind.ensureKinds` ->
`initKinds` (124 stores, init-named), `kind/embed`'s data builder likewise with
a thin wrapper kept for its tests, `filter`'s `Tainted` stores moved into
`initSetTainted`, `lol`'s global `level int32` replaced by a self-mutating
`logLevel`, plus shadowing, variadic-spread and receiver fixes. `make test` is
**194 passed, 0 failures** and the legacy unnamed-return enumeration is empty.
Correction to the round-24 note: nostr's Makefile *does* pass
`-allow-closure-captures`, so its 17 remaining closures are legal; and
`(*Reader).readContinuedLineSlice` is `net/textproto` (stdlib), not `pkg/core`.

**morly's work list is enumerated** (`/tmp/morly-s4.txt`): 15 packages, 12 unnamed
returns, global stores in `ensureDNSAddr` (12), `ensureRoles` (6), `resolveHost`
(4) and `ensureFormats` (4), and two value receivers (`*Response`, `*Request`).
The `ensure*` names are the same rename-to-init opportunity nostr used. A morly
agent is working the list, gated on `make build` plus **370 passed, 0
failures**.

**musiquay's debt is in `pkg/protocol` itself.** Building `web/common/protocolwire`
and `web/wasm/app` fails on `git.smesh.lol/musiquay/pkg/protocol`:
`ensureFormats` (4 stores, `credits.Formats`) and `ensureRoles` (6,
`tag.RosterRoles`/`CreditRoles`) hit the global-store rule, and four sites use
`+` for text. The `+` rule is gated on the target's own repository, so protocol
is checked whenever a musiquay target is built - and protocol is the package the
relay and the app both import. The taxonomy is small (10 stores, 4 concatenations)
and the names are the same rename-to-init opportunity as elsewhere.

**The stdlib sovereign-loop sweep is much larger than the first six packages.**
Fixed so far, beyond `compress/{bzip2,flate,lzw}` and `archive/zip`:
`debug/dwarf` (LineReader.readHeader, buf.entry x17, Reader.SeekPC, buf.varint),
and found-but-pending `encoding/{ascii85,base32,csv}`,
`evloop` (Conn.drainWrite, Loop.Run), `image/jpeg` (~50 sites),
`image/png` (~35) and `index/suffixarray`. Three **pre-existing, unrelated
blockers** surfaced where the sweep landed, none of them the rule:
1. `compress/flate` compiles with zero rule errors but **clang-22 segfaults**
   selecting `flate.(*Writer).Close` - a huge struct value field plus
   `o.d.close()` produces a dead aggregate load and an i1 load at offset 655600
   (exit 139). Everyone importing flate/gzip/zlib/archive/zip inherits it.
2. `compress/gzip` does not parse: `data := []byte{:binary.LittleEndian().Uint16(z.buf[:2])}`
   - a slice expression inside the `[]T{:...}` allocation form is rejected by
   the parser; and `gzip.mx` references an undefined package-level `le`.
3. `archive/zip` carries ~9 pre-existing shadow errors and `debug/dwarf` an
   unused `errors` import, both present at HEAD.

**musiquay is migrated and green** (`9ec2359`). Its only debt was `pkg/protocol`
itself: `ensureFormats` and `ensureRoles` (the lazy one-time builders behind
`credits.Formats` and `tag.RosterRoles`/`CreditRoles`) take the init-named
exemption as `initFormats`/`initRoles`, and the two `"unknown role: "` joins use
`|`. `make test-unit` is **89 passed** (protocol 62, marmot 18, mls 1,
protocolwire 8) and `build-app-wasm` links. With nostr green too, only morly
remains of the three app repos.

**morly's migration, second pass.** The first sweep's heuristic (result lists
whose elements contain no space) missed every signature whose parameter list
spans lines or whose single result is a bare type: `pickSmallest`'s `[]byte`,
`mergeStep`'s `([]byte, []byte)`, `wal`'s Open/Append/Read, `transport`'s five,
`wire`'s eighteen `EncodeTo`/`DecodeFrom` and `config`'s three parsers. A
scanner that walks from `func` to the signature's opening brace catches them;
that is the tool to use on any remaining repo. `acl`'s anonymous value
receivers (`func (Open) AllowWrite(...)`) are named pointer receivers now.
Remaining: `pkg/broadcast` (`New`/`PreSpawn` store globals, plus a
`chan struct{}` / `<-chan string` mismatch), and the compiler diagnostic that
names the failing function is built but still cannot be installed while the
stdlib sweep holds `./moxie`.

**morly's last package is a compiler question, not a migration one.**
`pkg/broadcast`'s stores are fixed (the pre-spawn handles live in a
`preSpawnState` with `set`/`take`), but the build now stops on
`cannot use value of type <-chan string as chan struct{}` at both
`done := spawn(wire.BroadcastWorker, in, out, ready)` sites. The worker is
`func BroadcastWorker(in chan SubCommand, frames chan BroadcastFrame, ready chan struct{})`
and none of those element types is a string, yet the spawn result is typed
`<-chan string`. Either spawn's result type is inferred from the wrong operand,
or an earlier spawn in the same compilation leaked a type into it; the next step
is to instrument the spawn builtin's result typing in `ssa_builder.mx` (grep
`"spawn"` in the builtin-call path, not `checkInitExpr`).

**All three app repos are green under the carve-out-free compiler.** morly
closed with `eb9dcb2`: `make build` links the relay and `make test-unit` is
**370 passed across 26 packages, 0 failures**. The last fixes were a
pre-existing type error the restriction errors had masked (transport's
accept-loop timestamps were `time.Time` where `metrics.Now`/`Since` are
`int64`), `server.OnTick`'s `tickCount` moving onto the `Server` (a global may
not be written outside init), `routeHTTP`'s result renamed to `rc` (the body
declares `status`), and - the one that only shows at runtime - passing `acl`'s
checkers as pointers, because the value receivers became `*Open`/`*ReadOnly`
and the interface dispatch table has no impl for the value forms
(`interface dispatch: no impl for AllowWrite`).

| repo | tests | note |
|---|---|---|
| nostr | 194 | `make test` |
| morly | 370 | `make build` + `make test-unit`, 26 packages |
| musiquay | 89 | `make test-unit` + `make build-app-wasm` |

The compiler that made this possible (`/tmp/mxc-spawn`) carries three fixes not
yet installed in-tree because the stdlib sweep holds `./moxie`: the `spawn`
control channel is `chan struct{}`, the unnamed-return diagnostic names the
function, and `checkPackageDecls`/`isUserPackagePath` are as landed. Legacy's
mirror of the spawn typing still needs checking (wood law).

**Wood law checked for the spawn typing.** A probe that assigns `spawn(...)`'s
result to a `chan struct{}` variable builds and runs identically on both
compilers (`ok true`): legacy already typed the control channel `chan struct{}`,
so the round-32 fix removed a divergence rather than creating one. The probe
lives at `/tmp/spawnprobe` and is the shape any future spawn-result change
should be checked against.

**Suite re-verified with the spawn-fix compiler: 171/171, 0 failures.** The run
used `MOXIE=/tmp/mxc-spawn` (the binary still cannot be installed - the swipe
sweep holds `./moxie`), and it covers the carve-out deletion, the `spawn`
control-channel fix, the function-naming diagnostic and every stdlib
sovereign-loop fix committed so far. That is the strongest single piece of
evidence for the whole stage: nothing in the tree regressed while the app repos
and the stdlib were being migrated.

**The stdlib sovereign-loop sweep is done.** A whole-tree pass with the same
loop prints nothing except `compress/gzip`, whose two sites (`(*Reader).readString`,
`(*Reader).Read`) sit behind a pre-existing parse error so the checker never
reaches them. 50 files were fixed - `compress/{bzip2,flate,lzw}`, `archive/zip`,
`debug/dwarf`, `encoding/{ascii85,base32,csv}`, `evloop`, `image/jpeg` (46
sites), `image/png` (26), `index/suffixarray`, `internal/{zstd,dag,coverage,maps}`,
`text/*` and vendored packages - and the last two (`internal/coverage`'s
decodecounter/encodecounter) are in `95050d7f`. Every one was the same two-line
hoist: declare the loop scratch once, before the loop, and assign inside it.

**What the sweep leaves behind is pre-existing debt of the ungated rules, not
sovereign-loop debt:** `archive/zip` ~9 shadow errors, `debug/dwarf` an unused
`errors` import, `internal/runtime/maps` 8 shadow errors, `index/suffixarray`
`undefined: regexp` plus shadow errors, `math/big/internal/asmgen` `undefined:
slices.Backward`/`Arch386`/`ArchARM64`, `image/{jpeg,png}` and
`x/text/unicode/{bidi,norm}` shadow/undefined errors, `encodecounter`'s two
`err` shadows plus `undefined: slices.Sorted`, and `gzip`'s parse rejection
(`[]byte{:binary.LittleEndian().Uint16(...)}`, a slice expression inside the
allocation form) and undefined `le`. Three of those are compiler or stdlib
bugs in their own right: the `[]T{:...}` slice-expression parse rejection, the
clang-22 segfault on `flate.(*Writer).Close` (huge struct value field plus
`o.d.close()`, exit 139), and the missing `slices.Sorted`/`slices.Backward`.

The compiler carrying the spawn fix, the function-naming diagnostic, the
carve-out deletion and this sweep is now **installed in-tree** (round 37).

**Stdlib store migration: started.** `src/syscall`'s environment loaders are
`initEnvs`/`initCopyenv` now (`cd612ee8`) - the largest single block there, and
they take the init-named exemption - and the compiler builds clean with the
rename. What is left of the inventory, in the order it should be taken:

- **syscall (the rest)**: `Setenv`/`Unsetenv`/`Clearenv` write `env` and `envs`
  after init and are public API, so they need the DNS-cache treatment - the
  two globals behind a self-mutating holder whose methods do the writes;
  `_getMapper`, `ensureRlimit`, `Setrlimit` and `prlimit` hold the other caches.
- **time (11)**: `FixedZone`, `LoadLocation`, `_startNano`, `_ensureUtcLoc` and
  `(*Location).get`. The zone *cache* is already an index; these are the
  remaining global writes.
- **os (10)**: `signalsInit`, `ensureMinrand`/`minrand`, `Mount`, `ensureFS`.
  `ensureMinrand`/`ensureFS` are one-time builders and can be renamed; the
  signal table is a genuine post-init cache.
- then the **75 named slice/map types across 49 files**, and finally the scope
  widening in `isUserPackagePath` - the migration should be complete *before*
  the predicate changes, so the tree still builds at every step.

The pre-existing debt list from the sweep section above (shadow errors,
`undefined: slices.Sorted`/`Backward`, the `[]T{:...}` parse rejection, the
clang-22 segfault) is independent of the store migration and can be taken in
parallel.

**os's lazy builders are named too** (`b322167e`): `ensureMinrand` -> `initMinrand`
and `ensureFS` -> `initFS`, and the suite is **171/171, 0 failures** with both
the syscall and os renames in place. `signalsInit` and the restart/fs state
remain for the holder treatment.

**time's lazy builders are named too** (`fe15a8fa`): `_ensureUtcLoc` ->
`initUtcLoc` and `_startNano` -> `initStartNano`, with the suite at **171/171,
0 failures**. That is four stdlib packages this session (`internal/cpu`,
`syscall`, `os`, `time`) whose one-time builders now carry the init-named
exemption.

### Where the objective stands at the end of the automatic rounds

Done and verified, with a fresh build and the suite as the gate each time:
builtin Stringers in both compilers (no caller workarounds); `moxie get` with
`-pin`/`-worktree`/`-offline`/`-force`/`-remote`/`-protocol`, MANIFEST-aware
resolution, module-file removal from moxie/nostr/morly/musiquay, and the
commit-keyed build cache (a hit links, it does not recompile); morly's
`TestCrawlRelayAndPublish` SIGSEGV (the `time` interior pointer); the VS
Code/Codium extension; `musiquay/pkg/protocol` wired into the relay
(validation, `ResolveBlob`, the 32218-32220 exception) and the app
(`protocolwire`, publish paths); all six restriction rules bilateral with the
`/pkg/` carve-out deleted; all three app repos migrated and green (nostr 194,
morly 370, musiquay 89 + wasm); the stdlib sovereign-loop rule satisfied across
the tree; and 171/171 on the in-tree compiler.

Left for item (3)'s stdlib stage, in the order they should be taken:
1. `syscall`: `Setenv`/`Unsetenv`/`Clearenv` need the `env`/`envs` pair behind a
   self-mutating holder (the DNS-cache pattern); `_getMapper`, `ensureRlimit`,
   `Setrlimit`, `prlimit` hold the remaining caches.
2. `time`: `FixedZone`, `LoadLocation` and `(*Location).get` write globals the
   zone cache no longer covers.
3. `os`: `signalsInit`'s signal table wants a holder.
4. The **75 named slice/map types across 49 files** (63 of them carry methods,
   which `[]T` cannot; each method becomes a free function).
5. Only then widen `isUserPackagePath` past the dot rule, so the tree builds at
   every step.
Independent of that, the pre-existing debt the sweep surfaced: the `[]T{:...}`
slice-expression parse rejection (`gunzip.mx:210`), the clang-22 segfault on
`flate.(*Writer).Close`, missing `slices.Sorted`/`slices.Backward`, and the
shadow/unused-import errors in `archive/zip`, `debug/dwarf`,
`internal/runtime/maps`, `index/suffixarray`, `image/*` and `x/text/*`.

### The widened scope, measured (round 40+)

`isUserPackagePath` was widened to every package above the runtime (carve-outs:
`runtime`, `unsafe`, `internal/runtime/`, vendored `golang.org/x/`) and a
compiler carrying it swept every stdlib package. The result:

- **3,704 violations across 263 packages**;
- **3,118 unnamed return values** (the bulk, and purely mechanical: give each
  result a name, avoiding the body's locals - the app-repo sweep is the recipe);
- 180 shadowing errors, 18 `Const` value receivers, 14 package-level var
  initializers, and the rest global stores;
- the first package the *compiler's own* build stops at is `errors`, now
  migrated (`13a1564d`), followed in the log by `internal/byteorder`,
  `math/bits`, `unicode/utf8` and `internal/bytealg`.

`errors.Const` is the one genuinely design-level item: it is used as
`const X = errors.Const("...")` in 264 places across 95 files, and a `const`
cannot be addressed, so the value-receiver rule cannot be satisfied without
converting those to `var X error` plus an `init()` assignment first. That should
be done as its own change, not folded into the mechanical sweep.

The widened predicate is **not** in the tree: it would stop the compiler build
at `errors` and then at each next package. The tree ships with the dot rule (the
stdlib exempt) so every app keeps building; the wide predicate exists only in
`/tmp/mxc-wide-a` and the sweep output in `/tmp/wide-sweep.txt`. The migration
order that keeps the tree green at every step is: migrate the compiler's own
dependency closure until `_mxc_stage4` builds with the wide predicate, then the
rest of the stdlib, then land the predicate.

### errors: named results

`errors.New`, `Error`, `String`, `Join` (result `jerr`, the body declares
`err`), `Unwrap`, `Is` and the inner `is` are named. `Const` keeps value
receivers until the 264 `const` sites move to `var` + `init()`.

### The plan to the end (and what "done" means)

Three agents own disjoint slices of the 263-package sweep and verify each package
with `/tmp/mxc-wide-a` (the wide predicate), with the full suite as the behaviour
gate:
- **A**: `internal/**`, `math/**`, `unicode/**`, `strconv`, `strings`, `bytes`,
  `bufio`, `sort`, `slices`, `maps`, `cmp`, `io/**`, `fmt`.
- **B**: `encoding/**`, `compress/**`, `archive/**`, `image/**`, `text/**`,
  `hash/**`, `crypto/**`, `index/**`, `debug/**`, `container/**`, `context`,
  `path/**`, `evloop`.
- **C**: `net/**`, `os/**`, `syscall/**`, `time/**`, `io/fs`, `mime/**`,
  `log/**`, `regexp/**`, `math/rand/**` - the store-heavy slice, which also needs
  the holder treatment (`syscall`'s `env`/`envs` pair and rlimit caches,
  `time`'s `FixedZone`/`LoadLocation`, `os`'s signal table).

Then, in order:
1. **`errors.Const`**: convert its 264 `const X = errors.Const("...")`
   declarations across 95 files to `var X error` + an `init()` assignment, then
   switch `Const`'s receivers to pointers. Its own change, after the slices, so
   the conversion is done once and not per package.
2. **The rest of the sweep**: whatever the three slices do not finish, from
   `/tmp/wide-sweep.txt` - the list is complete, so this is a work queue, not a
   search.
3. **Land the predicate**: put the widened `isUserPackagePath` in the tree, build
   `_mxc_stage4` with it (the compiler complies with its own universal rules),
   run the suite, and re-run the three app repos (nostr 194, morly 370,
   musiquay 89 + wasm).
4. **The independent debt** the sweep surfaced: the `[]T{:...}` slice-expression
   parse rejection (`gunzip.mx:210`), the clang-22 segfault on
   `flate.(*Writer).Close`, missing `slices.Sorted`/`slices.Backward`, and the
   shadow/unused-import errors in `archive/zip`, `debug/dwarf`,
   `internal/runtime/maps`, `index/suffixarray`, `image/*`, `x/text/*`.

**Done is all four.** Until then the tree stays on the dot rule (stdlib exempt),
so every app keeps building and the suite stays green at every commit; each
slice lands as its own commit once its packages are clean and 171/171 holds.

### Two findings from the stdlib migration that outrank the site count

**1. The package cache key does not distinguish the compiler binary.** The sweep
agent proved it: `compilerHash()` hashes the compiler's *source files*
(`_mxc_stage4/` plus `src/runtime/`), and two binaries built from the same tree -
`./moxie` with the narrow predicate and `/tmp/mxc-wide-a` with the wide one -
read those same sources, so they compute the same hash and share a cache
directory. A probe with one can then reuse `.bc` files compiled by the other and
**falsely report a package clean**. Every wide sweep must pass `-a`; the
measurement of 3,704 sites across 263 packages was taken without it and is
therefore a **lower bound**. The real fix is to put the running compiler's
identity in the key (its own build id), which the old comment rejected because
hashing `/proc/self/exe` stops the self-host fixpoint from converging - so it has
to be a build id that is stable per build but not derived from the binary's
bytes, or `-a` on every probe stays the rule.

**2. `errors` gates every per-package probe.** The wide probe aborts at the first
failing package in a closure, and almost everything imports `errors`, so its two
`Const` value receivers made every other package unmeasurable. `type Const` is
being deleted in favour of `errors.New` (already pointer-receiver), with its 264
`const X = errors.Const("...")` sites becoming `var X error` plus an `init()`
assignment - one atomic pass across 95 files, because it is the blocker for all
three slices.
