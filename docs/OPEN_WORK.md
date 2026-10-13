# Open work

State carried across context compaction. Ordered queue, the decisions already
taken, the findings that motivated them, and the two designs in flight
(module removal, user-code exemption). Update this file when the queue moves.

## REMAINING WORK (as of round 127) - read this first

Everything below this section is history, evidence and closed items. This is the
actual queue, in the order to take it.

### 1. The stdlib's closure captures  [REFACTOR - the standing directive]

**Decision (owner, round 127): closures do not capture, ANYWHERE.** A function
value carries a code pointer and no environment; a frame arena dies at the
creating call's return (`FnArenaPop` + `ArenaRelease` per return, `munmap` only
per domain exit), and there is no escape analysis to separate a closure that
stays inside its frame from one that is returned, stored in a global or a
receiver field, or run by `defer` at return time. So compliance moves rather than
the rule: compiler first (DONE), then the stdlib, then drop `/pkg/` from
`isUserPackagePath`.

**Compiler: DONE.** `-report-closure-captures` on a full self-build reports
**0** (was 17: sixteen in `pkg/syntax`, one in `ir_deepcopy.mx`). The last one,
`ssaRecvRooted`'s recursive `walk`, is an iterative worklist now.

**Stdlib: 185 capturing functions** across the 42-package sweep (round-111
measurement; top packages `(*ClientTrace)` 16, `(*http2ServerConn)` 11,
`(*Logger)` 10, `(*Builder)` 10, `(*Transport)` 8, `(*persistConn)` 8). The sweep
now covers 45 packages, so RE-RUN the report first to get the current list.

Shapes to expect, and where a worked example already exists in this tree:
`p.list`-style callbacks (removed from `pkg/syntax` by consuming the separator
and letting each caller drive its own loop); `errh func(line, col, msg)` scanner
callbacks (removed by giving `Scanner`/`Source` the owning `*Parser` and calling
`p.scanError` directly); deferred closures that write a named result (store into
the slot, run the defers, then load the slot - round 111's ordering fix); bound
method values, where the receiver IS the environment (either resolve the selector
as a direct call or reject the value by name).

### 2. Function symbol passed where a FOREIGN instantiated generic named func type is expected  [CODE, narrow]

    func Collect(seq iter.Seq[int32]) (…)     // imported
    lib.Collect(lib.Counter)

    cannot use value of type func(func(int32) bool) as iter.Seq in argument to function

The target prints as the bare `iter.Seq`, so the parameter type loses its
instantiation args on that path (round 126 bound the instantiation everywhere
else, which is what fixed the two documented errors and made `maps.Values`/
`All`/`Keys` compile). A value already of the parameter's type works, which is
why the `iter_apis` fixture returns `iter.Seq[int32]` from a function instead of
passing the symbol.

### 3. Rule-9 arrays in `syscall` / `os.Pipe`  [DECISION, then maybe a pin]

`src/syscall/exec_unix.mx:141` declares `var p [2]int32`, and
`syscall_linux_amd64.mx` carries `*[2]Timeval` / `*[2]Timespec` in its //sys
directives. Alias/unsafe-bound and correct today; needs a decision (keep and pin
with a run-verified fixture that exercises `os.Pipe` and a time-setting syscall,
or convert), not a patch.

### 4. `syscall/js` and `net/http/roundtrip_js` rule debt  [DECISION]

Inside their `js && wasm` constraint they use value receivers, `any` and mutated
globals. They compile (phase 1c); nothing runs them. Either convert them to the
Moxie shapes, or pin them with a wasm fixture driven through the `gojs` stubs
(the validator host already has those behind `__moxie_test_stubs`).

### Operational rules learned the hard way (not code work)

- **Never run `moxie build -a` or `tests/wasm/validate.sh` while
  `tests/run.sh --phase N` is in flight.** Both clear the SHARED
  `$HOME/moxie/cache`. Observed, repeatedly: the phase's log stops mid-fixture
  with no error, and the next phase-5 run reports false FAILs
  (`context_breaker`, `hash_basic`). Plain concurrent builds are fine; only the
  cache-clearing ones are not.
- Phase 2 (selfhost) and the `:legacy` variants of phase 6 need libLLVM-22, which
  is not installed here. Environmental.

### Gate state on the current tree (commit `05087330`, compiler `05087330`)

- phase 0 regressions+restrictions: **123 PASS / 0 FAIL / 0 SKIP** (green)
- phase 1b: **45 stdlib packages** (green, now including `iter`, `maps`, `slices`)
- bootstrap fixpoint: **r127a == r127b == r127c** (green)
- phase 1c / 3 / 4 / 5 were NOT re-run for this compiler; the last clean run of
  all four was round 124 (1c 3/3, 3 4/4, 4 1/1, 5 73/0/0). Re-run them before
  trusting wasm, and run them alone (see the cache rule above).

### Closed in rounds 112-127, so nobody re-opens them

- generic instantiation is bound into the types it is compared against (round
  126): the two documented errors are gone, `maps.Values`/`All`/`Keys` compile,
  `iter`/`maps`/`slices` are sweep entries, and the sweep gap that hid it is
  closed.
- the compiler's own closure captures: 17 -> 0 (round 127).
- printed-but-ignored diagnostics: fixed (round 112, `reject_type_as_value`).
- untyped integer constants wider than 64 bits: fixed (round 123,
  `const_fold_exact`).
- `crypto/ed25519`: struct wrapper done earlier, pinned by `ed25519_roundtrip`.
- `debug/gosym` / the round-103 arena item: fixed in round 104 (codec seen
  tables), re-verified round 123 with `returned_object_identity`.
- the "newly surfaced, sized, not done" list: `crypto/ecdsa`, `compress/flate`
  and `gzip`, the `src/net` public types (aliases), net/http's `context.Value`
  (removed by design in `cbfbe313`), `internal/coverage/*` and `tracev1` - all
  resolved (round-124 reconciliation).
- `makeStrongFromWeak` removed; `regexp`/`text/template` excluded by design;
  relative-path entry point, tzdata registration, the `tests/util.sh`
  non-linking-pass guard: resolved/verified.
- `os/user`'s cgo-gated files: dead references, removed (round 125);
  `os_user_api` prints `os/user ok`.

## Where things stand

Four repositories, all on branch `dev`, all level with their remotes
(`git@git.smesh.lol:<name>.git`, port 2222, `GIT_USER=owner`).

| repo | what it is | last known state |
|------|------------|------------------|
| `moxie` | the compiler, runtime and stdlib | `5fb14785` (the compiler is `7fb19370`); phase 0 **122 PASS / 0 FAIL / 0 SKIP** and phase 1b (**42** stdlib packages) are green on this tree; phase 1c/3/4/5 were stopped mid-run at the user's request (last clean run, round 124: 3/3, 4/4, 1/1, 73/0/0), and phase 6 has 0 `:stage4` failures; phases 2 and the `:legacy` variants of 6 are environmental (no libLLVM-22). SEE "REMAINING WORK" AT THE TOP - the real queue is now one code defect (generic named function types unsubstituted in the typechecker), the iter/maps/slices sweep gap, the rule-9 array decision and the js&&wasm rule debt |
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

**The sweep's method was weaker than the number suggests - two independent
reasons, both found by the slice agents.**
1. The cache key does not distinguish the compiler binary (above), so a probe
   without `-a` can reuse narrow-built `.bc` and report a clean package.
2. A probe of a *directory* target compiles it under a **bare package name**
   (`./src/encoding/hex` becomes package `hex`), and the wide probe aborts at the
   first failing package in the closure before it ever reaches the target. So a
   per-package reading is only trustworthy when the whole closure is already
   clean, and the 3,704/263 figure is a **lower bound on both counts**.

The reliable probe the slice-B agent built is `/tmp/mxc-sliceprobe`: the wide
predicate restricted to one slice with `errors` carved out, driven by tiny
blank-import programs by **full import path**. Any future sweep should copy that
shape rather than building a package directory.

**`context` needs a real API change, and it is authorised.** `CancelFunc` is a
`func()`, and `WithCancel`/`WithDeadline`/`AfterFunc` return closures over their
cancel state; a Moxie function value carries no environment, so the type must
become a stateful value with a method (`CancelFunc.Cancel()`, a `stop` type with
`Stop() bool`) and the singletons stay pointer-receiver and address-stable so
`errors.Is`/identity checks hold. The ripple into `net/http` is part of the same
change, and any file outside slice B is reported rather than edited by it.

**`errors.Const` is gone** (slice A): `type Const` and its methods are deleted,
and its **264 `const X = errors.Const("...")` sites across 93 files** became
`var X error` plus an `errors.New("...")` assignment in one `init()` per package
- 54 `init()`s added or merged, none duplicated, no duplicate `errors` imports.
`src/errors` is clean under the wide predicate. Three ripples were routed with
it:
1. `path/filepath`'s `const SkipDir/SkipAll = fs.SkipDir/SkipAll` is invalid now
   that `io/fs` holds them as `var error`; they become `var` + `init()` (slice B).
2. `io/fs`'s `FileMode` methods are pointer receivers, so `info.Mode().IsDir()`
   on a non-addressable value no longer resolves and a `FileMode` **value** no
   longer satisfies `fmt.Stringer`; callers in `archive`/`debug`/`image` (B) and
   `os`/`net` (C) bind the mode to a local or take an addressable temp.
3. `maps.All`/`Keys`/`Values` capture the map because `iter.Seq` takes no map
   parameter - that is the documented `iter` skip-list workaround, not a new
   blocker; the real fix is refactoring those signatures.

**`context` is migrated** (slice B): `CancelFunc`/`CancelCauseFunc` are structs
with pointer-receiver `Cancel()`; `AfterFunc(ctx, f)` takes a
`context.Callback` (`Call()`) and returns a `StopFunc` with `Stop() bool`; and
`WithDeadline` no longer uses a closure at all - a package-level
`deadlineScheduler` arms one `time.AfterFunc(d, deadlineFired)` top-level
callback. The `AfterFunc` signature change has exactly one caller in the tree
(`src/crypto/tls/conn.mx`, slice B). The cross-slice call sites - `cancel()` ->
`cancel.Cancel()` and `cancel(err)` -> `cancel.Cancel(err)` - are 24 lines in
`src/net/dial.mx`, `src/net/http/{h2_bundle,server,transport}.mx`, all routed to
slice C with line numbers; the type annotations stay unchanged.

**`src/moxie` is the next top blocker, and now has its own agent.** The `moxie`
package carries the codec wrappers every other package encodes through, and it
fails the wide predicate on `type Bytes []byte` (`codec.mx:401`, a named slice
type) plus **20 value receivers** (`Bool`, `Int8`, `Uint8`, `Int16`, `Uint16`,
`BigInt16`, `BigUint16`, `Int32`, `Uint32`, `BigInt32`, `BigUint32`, `Int64`,
`Uint64`, `BigInt64`, `BigUint64`, `Float32`, `Float64`, `BigFloat32`,
`BigFloat64`, `Bytes`). Because `fmt`, `io/fs`, `io/ioutil`, `maps`, `iter` and
`internal/trace` all import it, the wide probe stopped there and none of them
could be verified. The fix ripple is the interesting part: a pointer receiver
needs an addressable value, and call sites pass conversions straight through
(`Uint32(len(v)).EncodeTo(w)`), so every such site binds to a local first.
`slices`-style free functions were considered and rejected as the wider change.

Also routed: `src/os/user` fails the *narrow* compiler with `cannot resolve type
of short var v in findGroupId/listGroupsFromReader` - a genuine compile error,
handed to slice C.

**`errors.Const` -> `errors.New` broke sentinel *identity*, and two of those are
real bugs** (slice A found them). `Const` compared equal by string; two
`errors.New` pointers do not. Six duplicate-message groups turned up, and the
two that break code are:
1. `crypto/rsa` now makes its own `ErrDecryption`/`ErrVerification`/
   `ErrMessageTooLong` while `crypto/rsa/fips.mx:383-391` switches on them for
   errors produced by `crypto/internal/fips140/rsa` - distinct pointers, so no
   case ever matches and `fipsError` returns the internal error instead of the
   public sentinel. Fix: alias the internal sentinels in `crypto/rsa`'s init.
2. `path/filepath.ErrBadPattern` and `path.ErrBadPattern` are now distinct, and
   `io/fs.Glob` returns the `path` one, so `errors.Is(err,
   filepath.ErrBadPattern)` stopped matching. Fix: alias one to the other.
The harmless duplicates (`crypto/cipher.errOpen` vs the gcm one,
`internal/poll.ErrNoDeadline` vs `os.ErrNoDeadline`) need no change, and
`src/syscall`'s `*Errno` value receivers are routed to slice C with the warning
that a bare `Errno` value stops satisfying `error`.

**The compiler's own parser no longer codegens** - `pkg/syntax` reproduces it
alone (`MOXIEROOT=$PWD ./moxie build -o /tmp/x ./pkg/syntax` ->
`invalid cast opcode for cast from '{ i64, ptr }' to '{ ptr, i64, i64 }'`), so
it is a `src/` change, not a concurrency artifact. The suspects are changes that
alter a *type* rather than a name: `const` -> `var error` conversions, pointer
receivers that change a method set (`io/fs.FileMode`, `syscall.Errno`), anything
that changes the static type of a value used as `string`/`[]byte`. Both slice
leads have been asked to run that build before declaring done and to bisect
within their own slice. Until it passes, the `topLevelColon` rewriter fix stays
parked at `/tmp/recolon.patch` (unverified compiler changes do not land) and
`compress/gzip` is blocked on both the parse bug and this.

**The `pkg/syntax` codegen failure was a direct victim of the `Const` change,
and is fixed (slice A).** `pkg/syntax/source.mx:136` compared
`s.ioerr.Error() != string(io.EOF)`: while `io.EOF` was an `errors.Const` (a
string type) that produced `"EOF"`, but once it became `var error`,
`string(io.EOF)` emitted an invalid bitcast (`{i64,ptr}` -> `{ptr,i64,i64}`) in
`(*Source).nextch` and the compiler could not build itself. It is
`s.ioerr.Error() != io.EOF.Error()` now. A sweep over 250 sentinel names found
exactly three such sites: that one, plus `src/net/http/server.mx:1840` and
`src/net/http/h2_bundle.mx:6247` (`string(ErrAbortHandler)`), all three fixed,
and slice C was told about its two.

**The general trap worth remembering:** converting the stdlib's sentinels from
string-typed constants to `var error` breaks every `string(SENTINEL)` site,
because a string constant and an `error` interface are not the same thing.

## CRITICAL: cross-package pointer-receiver interface dispatch is broken

Reproduced minimally (`/tmp/ifacedisp`): a named basic `type E int32` in package
`zzlib` with `func (e *E) Error() (s string)`, a constructor returning `*E`, and
a caller in `package main`:

```
p := zzlib.MK();  p.Error()            -> "one"   correct
var err error = zzlib.MK(); err.Error() -> "other" WRONG receiver
```

Same-package dispatch is correct; a custom `String()` interface behaves the
same; a `const` of a named basic used as an interface value panics
`interface dispatch: no impl for Error`; and boxing `syscall.Errno` in a stable
root-arena holder still dispatched to the wrong receiver while a direct call on
the box was correct.

**Why it matters:** rule 2 (pointer receivers) cannot be applied to any type
used through an interface until this is fixed - `syscall.Errno`/`Signal`,
`io/fs.FileMode`, and the 49 `time.Time` value receivers are all in this class.
The slice-C agent reverted `Errno`/`Signal` to value receivers to keep the tree
green, and slice A's `FileMode` pointer receivers are suspect for the same
reason. A diagnosis agent owns it now; a full write-up of the mechanism and the
fix is the deliverable.

**`math/big`'s `nat` alias is the current tree blocker.** `src/math/big/nat.mx:35`
is `type nat = []Word` (an alias to a slice) after the migration, and the
compiler now fails to codegen the call at `math_big.ll:5231`:

```
error: '%t54' defined with type 'ptr' but expected '{ ptr, i64, i64 }'
call {{ptr,i64,i64}, i32, i32, {i64,ptr}} @"math/big.natScan"({ptr,i64,i64} %t54, ...)
```

`natScan(z nat, ...)` (`natconv.mx:105`, called from floatconv/intconv/ratconv)
receives its first argument as `ptr` instead of a slice header, so a type alias
to a slice in a parameter position is not lowered like the slice it aliases -
a compiler bug of the same family as the interface-dispatch one, and it stops
the whole stage4 build and therefore the suite. Slice A owns it; the alias form
is the suspect, `[]Word` direct is the fallback.

Also noted: `index/suffixarray` is clean except three pre-existing
`undefined: regexp` lines - `src/regexp` does not exist in this tree at all, so
that is missing-stdlib debt, not migration debt.

## THIRD COMPILER BUG: slice type aliases are mis-lowered

Every rule-5 conversion in the tree used a type **alias** (`type X = []T`), and
the compiler mis-lowers slice aliases - two demonstrated failures:
`math/big`'s `nat = []Word` produced a `ptr` where a slice header was required
(`math_big.ll:5231`, `'%t54' defined with type 'ptr' but expected
'{ ptr, i64, i64 }'`), and `internal/poll`'s `String = []byte` produced
`undefined: internal/poll.String`. Aliases are *legal* by the rule (an alias
introduces no new named type, which is exactly why the rule exempts it), so this
is a genuine compiler defect like the interface-dispatch one, not a language
restriction.

**Immediate policy (in force):** a rule-5 conversion must **spell the underlying
type** (`[]T` / `map[K]V`) at every use site, never an alias. Who is doing which:
- slice A: `src/math/big/nat.mx`, `src/internal/poll/fd.mx` (plus a tree-wide
  `grep -rn "^type [A-Za-z_]* = \[\]\|^type [A-Za-z_]* = map\[" src/` sweep);
- slice B: `debug/dwarf/entry.mx` (`abbrevTable`), `crypto/internal/boring/doc.mx`
  (`BigInt`), `archive/tar/format.mx` (`sparseArray`, `sparseElem`),
  `archive/zip/{reader,writer}.mx` (`readBuf`, `writeBuf`),
  `image/jpeg/writer.mx` (`huffmanLUT`);
- done here: `src/net/rawconn.mx:89-90` (`poll.String` -> `[]byte`).

Same class as the `math/big` blocker recorded above; the compiler fix is the
second half of it.

## `src/moxie` is clean

The dedicated agent finished it with two files: `codec.mx` (pointer receivers on
all 20 `EncodeTo`s, `type Bytes []byte` replaced by `EncodeBytes`/`DecodeBytes`)
and its Go mirror `codec.go` (compiled by the legacy module through
`legacy/src -> ../src`; `go build ./src/moxie/` passes). The only call sites
outside are four interface dispatches in the runtime, which already box
`{*T, &v}` and need no edit. Two follow-ups it found: `_mxc_stage4/main.mx:4547`
`cmdHeader` still emits a **value-receiver** `EncodeTo` into generated `.mxh`
headers (needs the bilateral legacy mirror), and `sysroot/runtime_go.bc` is now
stale against the new codec and needs `build-runtime` before apps can call the
free functions.

**Alias sweep result:** `grep -rnE '^type [A-Za-z_][A-Za-z_0-9]* = (\[\]|map\[)' src` returned
exactly the seven routed (archive/tar, archive/zip x2, crypto/internal/boring,
debug/dwarf, image/jpeg) plus the two being fixed (math/big, internal/poll);
`pkg/` has none. So the alias-free policy has a bounded work list, and slice A
will re-run the grep after its two land.

**Remaining owner gap closed:** slice C finished and stopped, leaving
`src/net/**` unowned (275 value receivers, the removed-`any` debt in
`net/http/transfer.mx` and `httptrace/trace.mx`, and `rawconn`). A new agent owns
it, with the explicit instruction that value receivers on interface-used types
must be **left alone and reported** until the cross-package pointer-receiver
dispatch bug is fixed - converting them now would ship a runtime wrong-receiver
bug. `src/net/rawconn.mx:89-90` is already done here (`poll.String` -> `[]byte`).

**Probe status after `src/moxie` landed:** `fmt` still stops at
`mxc: compile error for syscall` with its seven `value receiver (*Errno,
*Signal)` lines - i.e. `fmt`'s own rules remain unmeasurable until the
interface-dispatch bug is fixed, since those seven are exactly the receivers
that cannot be converted yet.

# The three compiler bugs: closed, and what they were

All three landed as small commits after being reproduced *and* run, not just
compiled. Evidence for each is in the commit message.

1. **Cross-package pointer-receiver dispatch** (`_mxc_stage4/ir_iface.mx`,
   004c374c..f7a746d4 range). `findIfaceImpls`' cross-package scan registered a
   pointer-receiver method with `recvType` = the value `Named` while its typeID
   said `pointer:named`. For a `Named` over `int32` `llvmType` is `i32`, so
   `materializeIfaceValue` took the inline-scalar path and `ptrtoint`/`trunc`'d
   the real receiver pointer to 32 bits; the callee read the low half of its own
   address. Structs worked by accident (`intBits` returns 0 for a non-scalar and
   the pointer passed through). The same-package scan already wrapped in
   `NewPointer`; only imported types were hit. The **value form** also had no arm
   at all: `pkg/types` keeps a pointer-receiver method in `T`'s method set, so
   `var e error = K` was accepted and then panicked `interface dispatch: no impl
   for Error`. Both scans now register the value form, and `emitIfaceAssertOK`
   dedupes on `(base, form)` so the assertion set still covers both tids.
   Verified: `/tmp/ifacedisp` prints `direct: one` / `iface: one` (was `other`);
   `/tmp/vf` boxes a value, a const and a pointer of a named basic plus a struct
   value and pointer and prints `cone/cone/cone/sone/sone`; the in-tree fixture
   `tests/regressions/should_compile/iface_ptr_recv_crosspkg` builds, links and
   prints `ok`.

2. **Slice/map aliases dropped from the mxh** (`_mxc_stage4/main.mx`, 2033646b).
   `mxhWriteTypeName` wrote an `alias` line only when the target was a `*Named`.
   For `type X = []T` / `= map[K]V` / `= *T` / `= int32` the type-check pass
   replaces the `TypeName`'s `Named` placeholder with the resolved target, so no
   branch fired and *nothing* was written: the name was absent from the mxh and an
   importer fell back to `int32` (its unresolved default), while a `Named` with no
   underlying lowers to `"ptr"`. `internal/poll.String` came back `undefined` and
   `math/big`'s `nat` reached a call as `ptr` where a 3-word slice header was
   required. The loader already binds `alias <name> <typeDesc>` for any target.
   Verified: `/tmp/al` fails to compile before (`cannot use value of type int32 as
   []...Word in argument to function`) and prints `len: 3 / get: 11 33 / xlen: 3 /
   mlen: 2` after; the in-tree `slice_alias_crosspkg` fixture prints `ok`.

3. **`cmdHeader` emitted a value-receiver `EncodeTo`** (`_mxc_stage4/main.mx` +
   `legacy/header.go`, 2033646b). `*T`'s method set always includes `T`'s
   value-receiver methods, so the pointer spelling is valid either way and is the
   only one valid under the pointer-receiver rule. Verified by emitting a header
   with `./legacy/moxie header ./src/moxie`: it writes
   `func (*BigInt32) EncodeTo(w io.Writer) error`.

**A fourth bug, found on the way and fixed:** `src/math/big/rat.mx:540` had
`natMul(z.a, stk, x.a, y.a)` where HEAD had `z.a.mul(stk, &x.a, &y.a)`. The
free-function rewrite moved the receiver to the first argument and dropped the
result, and `Int.mul` still exists, so a zero-valued `Rat` kept `abs == nil`:
`Rat.Mul` returned `0/1` for `1/3 * 1/7` and the compiler's *own* constant folding
inherited the wrong values (this is why a freshly built compiler segfaulted in
`math/big.natMul` via `pkg/types[floatOp]`). Slice A's independent audit of all 43
`nat*` free functions and their call sites found no other instance.

## The type-checker hole behind it: soundness checks gated on a *style* predicate

`_mxc_stage4/ssa_builder.mx`'s soundness checks - `checkAssignable` (~2011),
`errArgs` (~1993), `checkConstConvert` (~2075), `checkDefaultRepr` (~2322) - each
begin with `if !isUserPackagePath(restrictPath(...)) { return }`. Every **dotless**
import path (the whole stdlib) is therefore exempt from argument type, arity and
const-representability checking, and `pkg/types`' correct `Assignable` is never
consulted. That is why the `Int`-where-`[]Word` call above compiled, and the IR
kept the mismatched call (`%t67 = call ... @"math/big.natMul"({i1,{ptr,i64,i64}}
%t59, ...)`).

Decisive A/B: identical library source imported as `gateprobe/lib` (dotless)
compiles; imported as `example.com/gateprobe/lib` (dotted) is rejected with
`cannot use value of type ...Int as []...Word in argument to function`. The gate
is a *correctness* check borrowing a *style* predicate.

**Fix (not yet applied):** remove the `isUserPackagePath` early-return from those
four checks; their existing nil / `ConcreteAssignTypes` guards already exclude
unresolved interfaces and type params. Keep the predicate only for genuine style
restrictions (`checkNamedReturns` etc.). This is bilateral: legacy's diagnostics
come from `go/types` and are likewise not surfaced for stdlib, so mirror it. It
should land together with the widened `isUserPackagePath`, because that is what
activates them anyway - and it will surface latent stdlib mismatches that are real
wrong-ABI code, so expect a wave and fix them rather than re-gating.

## Bilateral divergence: the method set of a value type (bootstrap blocker)

Moxie bans value receivers, so `pkg/types` keeps a pointer-receiver method in the
**value** type's method set (`tc_assign.mx` `methodSet`/`collectMethods` register
`t.Methods` on the `*Named`). Stage4 therefore accepts `var e error = C(1)`.
Legacy goes through `go/types` and **rejects** it: `lib.C does not implement error
(method Error has pointer receiver)`.

This is on the critical path, not cosmetic: the stdlib migration converts
`func (e Errno) Error()` to `func (e *Errno) Error()`, and the moment it does,
legacy - the bootstrap path - can no longer compile `src/syscall`, so the
bootstrap dies. Stage4's rule is the one to keep: with all receivers pointers, a
value must stay usable as an interface value (it dispatches on a copy, exactly as
`materializeIfaceValue` already materialises an inline scalar into a fresh
alloca). An agent is mirroring the rule in the patched-GOROOT `go/types`
(`legacy/build/patch-gotypes.go`) and verifying `/tmp/vf`, the in-tree fixture,
and the full bootstrap (`legacy/build.sh`, then legacy building `_mxc_stage4`).

Note for fixture authors: Moxie's builtin `error` is
`interface { Error() string; String() string }` (`pkg/types/tc_universe.mx`).
Named basics get a synthesised `String()`; a struct used as an error needs an
explicit one.

## `math/big/internal/asmgen` deleted

It generates `arith_$GOARCH.s`; nothing imports it, no `.s` file exists for
`math/big`, and `math_big_pure_go` is hardwired true in both compilers, so the
generated assembly is unreachable. It carried six value receivers that cannot be
converted without an unverifiable rewrite of ~60 `fmt.Stringer` call sites and
there is no runnable entry point to test them - delete, per "no stubs - remove
dead references instead". 19 files and the stale `//:generate` line are gone.

## `src/regexp` does not exist - bounded, pre-existing, do not stub

Five files import it: `internal/lazyregexp`, `internal/coverage/pods`,
`internal/profile/prune`, `internal/trace/testtrace/expectation`,
`index/suffixarray`. Only two have any consumer in-tree
(`internal/coverage/cfile/testsupport.mx` -> `coverage/pods`, and
`internal/trace/traceviewer/pprof.mx` -> `internal/profile`), both
trace/coverage/profiling corners with no path to the apps, and none is in the
compiler's closure. They fail identically before and after the migration
(`undefined: regexp`). Leave them; do not invent a `regexp` package and do not
rewrite them against `mxregexp` as part of this migration.

## The relative-path entry point: CORRECTED - it is Go's import-path rule

**This section previously called it a bug. It is not, and the correction is
measured (round 78).**

A **bare** path is an import path, exactly as in Go: `moxie build relmainprobe`
resolves `relmainprobe` as an import path, never reads `./relmainprobe/main.mx`
as the entry package, and the link fails with `undefined symbol: main.main` while
the build otherwise succeeds. Spelling it as a directory works in both forms:

    moxie build -o /tmp/rel.bin ./relmainprobe     # rc=0, runs
    moxie build -o /tmp/abs.bin "$PWD/relmainprobe" # rc=0, runs
    moxie build -o /tmp/bare.bin relmainprobe       # rc=1, undefined symbol: main.main

So the entry point does not depend on "how the target was spelled" beyond the
deliberate `import path` vs `directory` distinction - which is required for
`moxie build fmt` to mean the stdlib package. Nothing to fix here.

The second half of the old note is done: **`tests/util.sh`'s acceptance of
`undefined symbol: main.` is gone** and verified - `compile_and_run` returns 1 for
any linker failure, with the reason recorded in its comment. A fixture that does
not link now FAILS, which is what exposed the whole `undefined symbol: main.`
family in the first place.

## Named public-API types still to dissolve in `src/net/**`

The net agent finished its pass: `net/netip` is wide-clean, `net` and
`net/internal/socktest` compile clean, value receivers 275 -> 66, `any` debt in
`net/http` removed (a `httpMsg` marker interface; `[]any` -> `[]net.IPAddr`, which
required `src/internal/nettrace/nettrace.mx`), and `rawconn.mx`'s
`poll.String` -> `[]byte` confirmed. What remains there is the named slice/map
rule on public API, which needs a coordinated rewrite of call sites inside and
outside the tree: `net.IP` (18 methods), `net.IPMask`, `net.HardwareAddr`,
`net.Buffers`, `net/http.Header` (13), `net/textproto.MIMEHeader`,
`net/textproto.ProtocolError`, `net/url.Values` (6, a `map[string][]string`),
`net/http.Dir`, plus the internal `fileInfoDirs`/`dirEntryDirs` (`sort.Interface`),
`http2writeSettings` (`http2writeFramer`), `http2sortPriorityNodeSiblings`
(`sort.Interface`), `http2writeQueuePool`, `htmlSig`, `byPriorityWeight`,
`byPref`. `net/http` additionally **cannot compile** because `context.Context`
has no `Value` and `context.WithValue` does not exist in this tree - and cannot
exist under no-`any`. The Moxie-coherent answer is per-request state on the
`*Request`/connection instead of a dynamic attribute bag (`httptrace` becomes a
field), not a fake `any`.

## Second bilateral divergence: pointer methods on non-addressable values

Legacy rejects a pointer-receiver method called on a temporary; stage4 accepts it
(auto-addressing a copy, the same model as the value box):

    type V struct { r int32 }
    func (v *V) Get() (out V) { return *v }
    func Mk() (out V) { return V{r: 1} }
    a := Mk().Get()        // legacy: cannot call pointer method Get on V

Same root as the value-in-interface divergence above, and it is being patched at
the same level (the go/types method set/lookup in the patched GOROOT): in Moxie
every receiver is a pointer and a value is auto-addressed into a copy, so a value
type satisfies a pointer-receiver interface and a temporary can be called through.
It is the reason `syscall/js.Value`'s fluent API (`js.Global().Get(...).New()`)
could not be converted.

## `syscall/js` and `net/http/roundtrip_js` are recorded debt, not blocking

Both `syscall/js` files are `//:build js && wasm`, so they are build-constrained
out of every native build and cannot block the native wide-predicate landing, the
bootstrap, or the compiler closure. Under `-target js/wasm` they have ~80 sites:
~30 value receivers, ~25 unnamed returns, `any` in `ValueOf`/`Set`/`SetIndex`/
`FuncOf`/the funcs map, package globals mutated by `jsInit`/`funcsInit`/
`ensureEventHandler`, and capturing closures in `FuncOf`. The only caller is
`net/http/roundtrip_js.mx`. CLAUDE.md puts Moxie's browser surface in `jsruntime/`,
so these are Go-compat shims with no Moxie-native consumer: the eventual move is
to delete both or rebuild the wasm RoundTripper on `jsruntime/`, not to redesign
Go's API. Recognising a pointer-method chain on a temporary (see above) is now a
legacy bug, not an authoring limitation.

## Latent bugs found by the migration, independent of it

- **`weak` is a lie.** `runtime_makeStrongFromWeak` has no implementation
  anywhere; the runtime provides only `runtime_registerWeakPointer`, which is a
  no-op. `weak.Make`/`Value` compile and then SIGSEGV on a call to 0. Nothing
  instantiates `weak` yet (`crypto/internal/fips140cache` imports it).
- **Generic instantiation, two defects.** `map[T]H[T]` as a composite literal
  inside a generic function fails substitution (`cannot use map[int32]unique.Handle
  as map[T]unique.Handle in struct literal`); and loading a generic
  struct-with-pointer value out of a map emits `store i8 %tN` where `i8` is
  expected, i.e. an `i32` value stored as `i8` (`clang: '%tN' defined with type
  'i32' but expected 'i8'`). Minimal repro: `type H[T comparable] struct{v *T};
  type hm[T comparable] struct{m map[T]H[T]}; func Look[T comparable](x hm[T], k T)
  (out H[T]) { out = x.m[k]; return }`. Nothing imports `unique`, so it is latent.
- **`src/net`'s public named types** and the `context.Value`/`WithValue` gap in
  `net/http` are recorded above; `net/http` cannot compile until `context` gets a
  concrete per-request state API or net/http stops using one.

## Legacy cannot compile a package-level `error` global - bootstrap blocker

A 20-line dependency-free repro (package `io2` with `var EOF error`, `init() {
EOF = &E{...} }`, and a main that calls `EOF.Error()`) makes the legacy compiler
fail while interpreting its own runtime:

    src/runtime/runtime.mx:44:3: interp: ptrtoint integer size does not equal pointer size
      call void @"io.init#1"(ptr undef), !dbg !1925

Mechanism: `legacy/interp/memory.go:845` `rawValue.toLLVMValue` is asked to
serialize the global's raw bytes into an **integer** LLVM type; the buffer's first
word is a pointer (`buf[0] > 255`), so it takes the pointer branch and the
`checks` guard at line 863 fires because `TypeAllocSize(llvmType) !=
TypeAllocSize(dataPtrType)`. The global's *declared* LLVM type is a byte type
while the value written by `init` is a pointer. The comment there already calls
this "probably trying to serialize a pointer to a byte array, perhaps as a result
of rawLLVMValue() in a previous interp run", and `interp.go:164` treats the same
error as recoverable in one other place.

Why it started now: before the migration, `io.EOF` was
`const EOF = errors.Const("EOF")` - a constant expression, so no global existed
to serialize. The `errors.Const` -> `var X error` + `init()` conversion created a
mutable package-level `error` holding a pointer in 93 files. stage4 is unaffected
(the repro builds, links and prints `boom` under `/tmp/mxc-iface2`), so this is a
legacy/interp defect that the migration merely exposed. Nothing else in the
compiler closure is reachable with legacy until it is fixed, so the bootstrap
(`legacy/moxie build -a -o mxc-stage4 ./_mxc_stage4`) cannot run.

Note the second blocker behind it: the migration converted `syscall.Errno`'s
`Error()` to a pointer receiver, and legacy's `go/types` then rejects
`Errno` used as an `error` (`cannot use EINVAL (constant 22 of uintptr type
Errno) as error value in return statement: Errno does not implement error (method
Error has pointer receiver)`) - the method-set divergence above. Both must be
fixed for the bootstrap; the method-set one is staffed.

## Runtime pointer arithmetic used int32 offsets

`src/runtime/runtime.mx`'s `memcpy`/`memmove`/`memzero` cast their byte offsets
with `int32(i)` and `secalloc.mx` used `-int32(secPageSize)`. Those are the
`unsafe.Add` offsets, so an arena or copy larger than 2 GiB truncated. They were
made pointer-sized (`i`, `i-1`, `-int64(secPageSize)`) while chasing the interp
failure below, then REVERTED: the interp failure turned out to be the recoverable-
error classification in `legacy/interp/errors.go`, not the offset width, and a
runtime change should not ride along unverified. The truncation is real but needs
a >2 GiB single copy to bite; re-apply and run the full suite before landing it.
(The byte-by-byte `memcpy` loop itself is also why the interp walks it at all.)

## A compiler built by the OLD in-tree `./moxie` cannot run its own `os` - and it looks like memory corruption

Symptom: a compiler freshly built from the migrated tree panics on **every**
build, before compiling anything:

    panic: runtime error at 0x...: interface dispatch: no impl for Read

with a backtrace through `os.(*File).Read` -> `os[ReadFile]` -> the compiler's own
`mapCensus`/`memLine`. It is not memory corruption and not a stale cache (`-a`
does not change it).

Cause: `src/os/file_unix.mx` boxes the handle by VALUE
(`&File{&file{handle: unixFileHandle(h), name: name}}`), so the interface value
carries the tid `named:os.unixFileHandle`. The migration converted
`func (f unixFileHandle) Read(...)` to a pointer receiver. Under the OLD compiler
(no value-form arm) a pointer-receiver method registered ONLY `pointer:named:...`,
so the value box stopped matching and the dispatch fell to the default arm. The
fix is in `_mxc_stage4/ir_iface.mx` (the value form is registered for
pointer-receiver methods), but a compiler binary only contains the arms it was
BUILT with: `./moxie` predates the fix, so everything it builds panics.

Diagnosis that finds this in one step: `grep -c 'icmp eq i64 .*named:os.unixFileHandle'`
on the IR of `os` produced by a compiler that HAS the fix (non-zero, and both the
`named:` and `pointer:named:` arms are present), versus the panicking binary. Any
"dispatch arm exists in the IR but the program says none does" report should
first ask **which compiler built the panicking binary**.

Rule that follows: after a change that converts a *value-boxed* implementation to
a pointer receiver, install a compiler that already has the value-form arm
(`cp /tmp/mxc-now ./moxie`) before rebuilding. `./moxie` is gitignored, so this is
a bootstrap step, not a commit.

# State at the end of this pass

Committed (all verified by running, not by compiling):

| commit | what |
| --- | --- |
| `c6255131` | `math/big.Rat.Mul` stored nothing after the free-function rewrite (returned 0/1) |
| `158b2efb` | deleted the dead `math/big/internal/asmgen` generator |
| `004c374c` | `weak`/`fips140cache`: no constraint in a receiver type-parameter list |
| `f7a746d4` | cross-package pointer-receiver dispatch: `recvType` is `*T`, plus the value form for pointer-receiver methods |
| `2033646b` | mxh: an `alias` line for a non-named alias target; `EncodeTo` declared on `*T` bilaterally |
| `b0059dc9` | legacy: the value type's method set + the interp's recoverable pointer refusal |
| `b239b155` | `isInitFuncName` strips the monomorphization prefix/suffix |
| `b76ac386` | this document |

Verification actually observed, in one place:

- `tests/run.sh --full`: **175 passed / 0 failed / 0 skipped** with a compiler
  built from the tree (slice B's run). The run recorded here had one transient
  `wasm:hash_basic` build failure that rebuilds clean on its own.
- Self-hosting: `selfhost:stage1`, `stage1-version`, `runtime-bake`, `stage2`,
  `stage2-version` and **`fixpoint (stage2 == stage3)`** all PASS.
- Bootstrap via legacy: `legacy/build.sh` succeeds and
  `legacy/moxie build -a -o /tmp/s4-legacy ./_mxc_stage4` exits 0; the result
  runs (`mxc 1.9.11`).
- New regressions `iface_ptr_recv_crosspkg` and `slice_alias_crosspkg` pass on
  native **and** wasm (`output matches native`).
- Apps, with the rebuilt compiler and a fresh `build-runtime` bake: nostr
  `make test` PASS 20/20; morly `make build` links; musiquay `make test-unit`
  PASS 8/8 and `make build-app-wasm` writes `web/static/app.wasm`.
- `sysroot/runtime_go.bc` was re-baked with the fixed compiler.

Note: `./moxie` in the tree is gitignored and is now a compiler built from the
committed state. A compiler built by a `./moxie` that predates the dispatch fix
panics on its own `os` (see the bootstrap-artifact section above), so keep
`./moxie` current after any value-box-to-pointer-receiver conversion.

## Round 124: the "newly surfaced, sized, not done" list is checked, item by item

Each claim in this section (below, kept as written) was re-measured against the
tree and the round-123 gate logs. Most of them are DONE; two are real.

**DONE - `crypto/ed25519`.** The struct-wrapper design landed in `bc4a9067`:
`PublicKey`/`PrivateKey` are structs wrapping `[]byte`, `PublicKeyOf`/
`PrivateKeyOf` are the constructors, `Bytes()` reads the bytes back, and the
methods have pointer receivers. The call sites in `crypto/tls`/`crypto/x509`
already assert on the value type and use `*ed25519.PrivateKey`. Round 124 makes
the verification DURABLE: the original evidence was a throwaway `/tmp/edprobe`,
and no fixture in the tree mentioned ed25519 at all. `ed25519_roundtrip` (new
phase-0 regression, also wasm-validated) now runs, on every gate:

    GenerateKey -> Sign -> Verify; a tampered message is rejected;
    Seed -> NewKeyFromSeed -> Equal -> signs identically;
    PrivateKey.Public() -> crypto.PublicKey -> ed25519.PublicKey assertion
      (the shape crypto/tls depends on);
    `var signer crypto.Signer = &priv` and Sign/Public through the INTERFACE
      (the pointer receiver, which is what the rule conflict was about);
    PublicKeyOf(Bytes()) identity, and the rebuilt key verifies the signature.

It prints `ed25519 ok`.

**DONE - `crypto/ecdsa`'s 16 wrong-ABI returns.** `crypto/ecdsa` is a phase-1b
SWEEP entry (line 49) and the sweep is green, so the whole crypto/tls closure
compiles; those `cannot use value of type *crypto/internal/fips140/ecdsa.PublicKey`
errors are gone.

**DONE - the >64-bit untyped integer constant.** Fixed in round 123 (see the
entry above): `ConvertConstLocal` was dropping `ConstInt.Exact` on the
declaration path.

**DONE - `compress/flate`'s clang-22 crash.** `compress/flate` and
`compress/gzip` are both phase-1b sweep entries (lines 42-43) and the sweep is
green. `crypto/tls` links through them.

**DONE - the named public API types in `src/net`.** They are ALIASES now, not
named types: `type IP = []byte`, `type IPMask = []byte`,
`type HardwareAddr = []byte`, `Header = map[string][][]byte`; `Buffers`, `Dir`
and `ProtocolError` are structs. The ban on named slice/map types no longer
applies to any of them.

**DONE - `net/http`'s `context.Value` gap, by REMOVAL.** `cbfbe313`:
"net/http carries per-request state concretely; `context.Value`/`WithValue`,
`ServerContextKey`, `LocalAddrContextKey`, `httptrace`'s context API and
`internal/nettrace` are gone". The gap was not filled, it was designed out, and
net/http is a green sweep entry.

**DONE - `src/internal/coverage/{cfile,cformat,encodecounter,encodemeta}` and
`internal/trace/internal/tracev1`.** All are phase-1b sweep entries (lines
63-69) and the sweep is green.

**STILL OPEN, and now the only items from this section:**

1. The rule-9 arrays: `src/syscall/exec_unix.mx:141` still declares
   `var p [2]int32`, and `syscall_linux_amd64.mx` still carries `*[2]Timeval` /
   `*[2]Timespec` in its //sys directives. The doc's own note stands - they are
   alias/unsafe-bound and correct today, so they need a decision, not a patch.
2. `src/syscall/js` and `net/http/roundtrip_js` remain RULE DEBT inside their
   `js && wasm` build constraint (value receivers, `any`, mutated globals).
   They compile (phase 1c) but no fixture runs them.

**Hazard found while doing this, and CONFIRMED by its damage:**
`tests/wasm/validate.sh` deletes `$HOME/moxie/cache` at start. Run at the same
time as `tests/run.sh --phase 0/5` it kills its own cold build silently mid-SSA
with no error text, it killed BOTH sibling runs outright that way (each phase's
log simply stops mid-fixture), and the phase-5 rerun that followed reported
`context_breaker` and `hash_basic` as FAIL - two fixtures that pass in every
other run, failing on a cache that had just been deleted underneath them. With
nothing else running, phase 5 is 73 passed / 0 failed and both fixtures pass.
Run the validator alone.

The section below is kept as it was written, for the record.

## What is left for the widened predicate

With the soundness half done, the widened `isUserPackagePath` now waits only on
the STYLE blockers that are already recorded above: `crypto/ed25519`'s named
slice types vs value `crypto.Signer` (the struct-wrapper design), `net/http`'s
`context.Value` gap, the named public API types in `src/net`, `debug/gosym`,
`syscall/js`, and the `internal/trace`/`internal/coverage` codegen bugs. Note
that `isUserPackagePath` is still the predicate for both roles; when the style
migration finishes, the soundness checks are already unconditional and need no
further change.

# Queue item 2: methods on generic types

`resolveMethodCallWithRecv` resolved a method on a generic type through the
generic declaration's own member. The declaration emits no symbol - only the
instantiation does - so the call went to the unmangled name and the link failed
with `undefined symbol: main.(*Box).get`; `tryGenericMethodCall`, which
substitutes the type arguments, was never reached because the plain path
returned first. It now returns nil for a receiver whose type is a generic
`Named`. `tryGenericMethodCall` also had no `handled` flag, so a VOID generic
method returned nil and the caller reported `Set undefined (type main.Box has no
field or method Set)`; it reports handled like `tryGenericLocalCall` and
`buildFuncFieldCall` now.

**Nothing in the suite covered a generic TYPE with methods** - generic
*function* coverage existed (`generic_pkg_type`), which is why the whole feature
could be broken silently. `tests/regressions/should_compile/generic_type_method.mx`
now covers a method returning `T`, a void method and a method that does not
mention `T`, through value and pointer receivers, for two instantiations, and
passes natively and on wasm. Full suite: 177 passed / 0 failed, fixpoint holds.

## Still broken, with minimal reproducers

1. **[FIXED]** **A field element of an instantiated generic, in non-generic code,
   was typed `T` rather than the substituted type.** `fieldType` now merges the
   instantiated Named's `TParams`->`TArgs` over the body's `fb.typeSubst`, so
   `s.v[2]` is an `int32`; the minimal case prints `direct: 99` and
   `int64: 1234 1234` where it printed nothing and lost the value. History: `resolveTypeExprWithSubst` says fields
   typed by the type param are substituted "at use sites via `fb.typeSubst`",
   but that only happens inside monomorphized bodies. Outside one, `s.v[2]` is a
   `T`: passing it to an `int32` parameter carries the wrong value (a checked
   `s.v[2] == 99` came back `0`) and `println(s.v[2])` prints nothing. Repro:
   `type Box[T comparable] struct{ v []T }` then `var s Box[int32];
   s.v = []int32{:3}; s.Set(2, 99); chk(s.v[2], 99)` - `s.At(2)` (a method, so
   the signature is substituted) returns 99 while `s.v[2]` is 0.
   `src/internal/trace` is STILL blocked, by a different step: `ordering.queue`
   is a `queue[Event]` field, and `o.queue.pop()` in `(*ordering).Next` still
   reaches `resolveMethodCallWithRecv` with a receiver type that has no
   `TParams`, so the guard does not fire, the unmangled `trace.(*queue).pop` is
   called, and clang reports `%tN defined with type 'ptr' but expected
   '{ptr, {i64,i64,i64}, {i8,i64,[4 x i64]}}'`. The next step is to print what
   `o.queue`'s SSA type actually is at that point - the field resolves
   correctly for a `Box[E]` field in a main-package struct (the fixture's
   `h.b`), so the suspicion is the mxh round-trip losing `TArgs` for a field of
   an instantiated generic in a DEPENDENCY package.
2. **A method on a generic type declared in ANOTHER package is not found at
   all**: `Get undefined (type git.smesh.lol/genL2/lib.Box has no field or
   method Get)`. `mxhWriteTypeName` deliberately writes no methods for a generic
   type (`len(named.TParams) == 0`), so the imported `Named` has an empty method
   set, and the generic-decl table the importer builds from the dependency's
   source does not cover the lookup.
3. **A monomorphized method returning `T`, called in a non-main package, panics
   `codec default bad size` at runtime.** Only non-main packages relocate
   returns, so the codec runs and the monomorphized result's type metadata is
   wrong. Repro: `/tmp/genL2` - `lib.Use()` returns `b.Get().tag` where
   `Get() (out T)` and `b` is a `Box[E]`; it links and then panics.

## [FIXED, and the earlier diagnosis was WRONG] a generic method's receiver was copied

The "two-result return from a generic method loses its values" recorded below was
not a tuple/codec bug at all. `tryGenericMethodCall` built a temporary alloca,
stored a VALUE copy of the receiver into it and passed that, so every receiver
write went into the temporary: `o.queue.push`'s `q.end++` vanished, the next
`o.queue.pop()` saw `start == end` and returned the zero value and false. A
single-result method lost its write identically, and the two-result shape only
made it look like decoding. Local variables were unaffected because the code
looked up `fb.vars` for a NAMED LOCAL and used its alloca - which is exactly why
the fixture passed while `o.queue` and `h.b` did not. Fixed in 9cfaafb2: the
receiver's address is taken whenever it is addressable (`buildOperandAddr`), and
only a genuinely non-addressable receiver is copied. The fixture now mutates
through a Holder's Box field and reads the state back across calls; the suite is
179/179 with the fixpoint holding.

## [SUPERSEDED] A multi-value return from a generic method loses its values

While narrowing `src/internal/trace` I found a second, sharper defect in the same
area, and it is **silent data loss**, not a link error:

- `func (q *queue[T]) pop() (out T, okay bool)` on `queue[Event]`, called
  DIRECTLY from `main`: correct (`ok: true typ: 7 a: 11`).
- The same call received inside another function:
  - `func (o *ordering) Next() (out Event) { out, _ = o.queue.pop(); return }`
    returns `typ: 0` (the value never arrives);
  - `func (o *ordering) Next() (out Event, okay bool) { return o.queue.pop() }`
    returns `ok: false` (the receiver state does not arrive either).
- The single-result form is correct everywhere: `pop() (out T)` with
  `Next() (out Event) { return o.queue.pop() }` prints `typ: 7 cmd: 11`.

So a **two-result monomorphized method's tuple is not decoded/relocated
correctly** at a call site inside a function whose own returns are relocated -
`main` does not relocate, which is why the direct call works. This is almost
certainly the same root as the `codec default bad size` panic recorded above (and
the same family as the `[][]string` row-aliasing bug): the codec/relocation
metadata for a monomorphized result tuple is misapplied. Reproducers are
`/tmp/genE` (wrong state), `/tmp/genI` (minimal: wrong value), `/tmp/genH` and
`/tmp/genF` (correct controls).

**`src/internal/trace` has moved on.** The diagnostic (print the receiver's
`Named` name plus its `TParams`/`TArgs` counts at the top of
`resolveMethodCallWithRecv`) answered it in one build:
`ty=trace.queue named=queue tparams=0 targs=1`. The instantiated `Named` carries
its ARGUMENTS but an EMPTY parameter list, so a guard testing `TParams` never
fired. Keying the guard on `len(TArgs) > 0` as well routes it to the
monomorphizing path; the `ptr`-vs-struct error is gone and trace now fails later,
on `%tN defined with type 'i64' but expected 'i8'` - the same class as the
`unique` bug recorded earlier (a generic struct's narrow field stored with the
wide type). So TParams is unreliable and TArgs is the signal; anything else
testing `len(TParams) > 0` to mean "this is a generic instantiation" should be
audited the same way.

# Queue item 3: exact typed constants (time.Date/time.Parse), and the idna parse bug

**Fixed and verified** in `0c958eea` (four defects; see the commit message for
which file each lives in): a float constant converted to an integer truncated
through float64 instead of the exact rational; a declared type was applied late,
so a typed constant kept its untyped value and later folds promoted to float; a
unary operator on a constant materialized at int32 before a widen could happen;
and `stripUnusedConsts` deleted one LINE per unreferenced const, leaving a
multi-line value behind for the parser to trip over. `time.Date(2023,3,14,...)`.
`Unix()` is `1678806566` and its Format is `2023-03-14 15:09:26` now, and the new
`const_exact_fold` fixture pins the exact values plus the >int32 literal through
a parameter. The full suite is green with `selfhost:fixpoint` holding.

## STILL OPEN: x/net/idna's generated tables do not parse

`src/vendor/golang.org/x/net/idna/tables15.0.0.mx` fails to parse, so `net/http`
(which reaches idna for `ToASCII`/`ToUnicode`) cannot build. Narrowed as far as
this: the first 55 lines (`UnicodeVersion` plus the 46-line `const mappings`)
parse fine; the whole `func _mappingIndex() []uint16 { return []uint16{ ...244
lines... } }` parses fine in a fresh probe; the two TOGETHER fail, with the
parser left inside a parameter list at the following `func`. Each bisection step
is a build, and the recipe is: take the file's lines 1..N minus the `package
idna` line, prepend `package main`, append `func main() {}`, and build;
`N=55` gives 0 parse errors, `N=304` gives 3. So it is size/shape dependent, not
a single construct - a fixed buffer, a segment boundary, or an offset that only
overflows when both the big string const and the big slice literal are present.
Next step: `-work`-style dump of the *rewritten* source at that point and a diff
against the original, which separates a rewriter's output from the parser's
input in one build. BOTH of the earlier candidates were red herrings and are
disproven: `\x22` in a string literal decodes correctly (9 bytes for that line,
which is right - it is 7 escapes plus the literal `0`), and `{:` does not occur
in the file at all.

The net agent is unblocked meanwhile by an authorised vendored split of that
generated file (move `_mappingIndex` or `xorData` into its own `.mx`), with the
decoded bytes unchanged.

# ============ CURRENT STATE AND TODO LIST (context refresh) ============

Objective in force: address every open issue in this file, each at its root
cause, verified by running, committed, with the full suite green (181 now) and
`selfhost:fixpoint` holding.

## Landed in this stretch (all verified, all committed)

| commit | what |
| --- | --- |
| `c8a23e81` | this handover section (the todo list and the disproven hypotheses), committed so a context refresh does not lose them |
| `c22099ba` | `tests/util.sh`: a fixture whose LINK fails now FAILS (the `undefined symbol: main.` and `linker failed` guards returned 0, so a non-linking fixture was reported as a PASS - that guard is why the broken entry point stayed invisible). `stripUnusedConsts` blanks a stripped line instead of deleting it and is comment-, string- and balance-aware, so diagnostic line numbers stay correct |
| `bc4a9067` | `crypto/ed25519`: `PublicKey`/`PrivateKey` become structs with `PublicKeyOf`/`PrivateKeyOf`/`Bytes()`, pointer receivers; `x509` updated. VERIFIED by running: sign/verify, `crypto.Signer` through the interface, the `crypto.PublicKey` type assertion, and `crypto/tls` compiling against the new shapes |
| `a364d951` | **two silent-wrong-answer soundness holes closed** (`ssa_builder.mx`): an unknown method on an interface value, and an interface value passed where a concrete type is wanted. Fixtures `reject_unknown_iface_method`, `reject_iface_to_concrete` |
| `1f416420` | `encoding/asn1`: `oidEncoder.Encode` wrote every ObjectIdentifier as `00 00 00` (the `dst[:0]` no-aliasing trap). Fixture `asn1_oid_der` |
| `6cd8a7ca` | `weak` removed: no GC means a weak pointer cannot mean anything; `fips140cache` keys on `*K` |
| `0c958eea` | exact typed constants: float->int truncates the EXACT rational, the declared type is applied at the declaration, a unary op on a constant folds in constant space, and `stripUnusedConsts` no longer deletes a multi-line declaration's first line. Fixes `time.Date`/`time.Parse` and `-9223371966606163200` through an `int64` parameter. New fixture `const_exact_fold` |
| `9cfaafb2` | a generic method's receiver is ADDRESSED, not copied: receiver writes were lost (`o.queue.push`'s `q.end++`), which made the next `pop()` return zero and looked like a tuple-decoding bug. Fixture `generic_type_method` now mutates through a `Holder`'s `Box` field |
| `66df51d3` | vendor unblock: `bidi` `container/list` -> a slice stack, shadow renames in `bidi`/`norm`, and the **idna `tables15.0.0.mx` missing brace** (see below) |
| `cbfbe313` | net/http carries per-request state concretely; `context.Value`/`WithValue`, `ServerContextKey`, `LocalAddrContextKey`, `httptrace`'s context API and `internal/nettrace` are gone |
| `4f89c565` | `net`: `textproto.ProtocolError` (struct), `htmlSig` (struct), `IPMask`/`HardwareAddr` (aliases + free functions), `http.Dir` (struct), fs `anyDirs`, the three h2 structures, `byPriorityWeight`/`byPref` (free functions; also fixed a sub-slice-is-a-copy shuffle) |
| `a3aa971f`, `af460679`, `10f28a45` | generic methods: routed through the monomorphizing path, void methods report handled, field types substitute through the instantiated Named's `TArgs` (not just a body's `subst`), and an instantiated generic is identified by `TArgs` because `TParams` can be empty |
| `0435870d`-range | soundness checks ungated (assignment, arity, const representability, default repr) with `errPos` naming `file:line`, plus the wave: the constant-typing bug above, 20 `unsafe.Pointer` conversions in `src/net`, and `pkg/rewrite`'s `[]typeapi.Type` vs `[]types.Type` |


VERIFIED TOGETHER, rc=0, 179 PASS, `selfhost:fixpoint` PASS (`stage2 ==
stage3`): `tests/util.sh` with the masking guards removed AND
`_mxc_stage4/ir_typecheck.mx` with the blanking, comment-aware, balance-aware
`stripUnusedConsts`. The stripping pass now writes `//` over a stripped line
and keeps its `\n`, so the concatenated line count - and therefore every
diagnostic line number after the const block - stays correct.

VERIFIED AFTER THE SOUNDNESS FIXES, rc=0, **181 PASS** (`selfhost:fixpoint`
PASS): the full suite with `a364d951` in the tree, which is 179 plus the two new
restriction fixtures. `--phase 0` was then re-run with the `asn1_oid_der`
fixture added and passed (rc=0, 66 checks in that phase, which includes the new
fixture) - the full-suite total with it is 182 by arithmetic, NOT yet measured
in one run. THE TREE IS CLEAN - nothing is uncommitted.

WOOD LAW DEBT found while landing the above: `stripUnusedConsts` was added in
`21a09393` ("dead constant elimination pass") in `_mxc_stage4` only.
`legacy/` has no such pass, so the two compilers do not accept the same input:
stage4 drops an unused const declaration before parsing, legacy parses it. It
is latent today (legacy builds stage4 fine and the fixpoint holds), but it is a
bilateral violation and belongs on the list.

The two `ssa_builder.mx` soundness fixes are NOT wood-law debt: legacy rejects
both programs through `go/types`, so stage4 was the diverging side and the fixes
restore parity. Verified by running legacy on the same two files:
- `i.Bytes undefined (type I2 has no field or method Bytes)`
- `cannot use i (variable of interface type I2) as Box value in argument to
  takeBox: need type assertion`

### Repro probes that live in /tmp (rebuild them; they are not committed)

- `/tmp/vfp2` valid interface boxing (must compile and print 4/4/4/4/3)
- `/tmp/vfp3` unknown method on an interface (must now FAIL)
- `/tmp/vfp6` interface value into a concrete parameter (must now FAIL)
- `/tmp/edprobe` ed25519 end to end; `/tmp/tlsprobe` crypto/tls compile gate
- `/tmp/ecprobe` ecdsa P-256 (fails at `elliptic`)
- `/tmp/asn1probe` the OID + `[]byte`-field + string-field marshal check

Build any probe with:
`MOXIEROOT=$PWD ./moxie build -o <out> <dir>`. `./moxie` has been refreshed from
this tree (it was STALE and still compiled the soundness-hole programs; a quick
check on `/tmp/vfp3` proves the binary is current: it must exit 1). The compiler
binary must be rebuilt with `-a` from the tree whenever the compiler changes -
the build cache key does not distinguish compiler binaries.

## THE TODO LIST

1. **`./src/net/http` still does not parse.** A REAL parse error whose reported
   position is WRONG. This is the last thing between the tree and a compiling
   net/http. A single-package build needs a generous timeout (the earlier probe
   was killed at 60s while the package graph was still being built).
2. **Diagnostic positions are unreliable - fix the remapper.** `concatSources`
   merges every file's imports into one block at the top, so the concatenated
   line count differs from the sum of the files, and `concatLineToFile`
   (`_mxc_stage4/main.mx` ~line 1895, fed by `cctx.concatSourceMap`) maps a
   concatenated line to `file:line` with that offset. Proof: a parse error in
   `src/net/http` is reported as `clone.mx:121`, and `clone.mx` has exactly 120
   lines. This sent me and the net agent to the wrong file twice, and it is the
   single highest-value fix for future debugging: until it is right, every
   `file:line` in a *parse* diagnostic is a guess. Note the map's arithmetic is
   correct for the lines it is given, so the bug is in what `outStart`/`srcLine`
   are recorded as, or in a consumer that remaps a position that is not a
   concatenated line at all.
3. **generic: an imported generic type carries no methods.** `mxhWriteTypeName`
   writes no methods for a type with `TParams` (deliberate: only instantiations
   emit symbols), so `Get undefined (type lib.Box has no field or method Get)`
   across a package boundary. The importer must resolve the method through the
   dependency's generic decls (which `ScanGenericDecls` builds from source).
4. **generic: `i64` stored where `i8` is expected** for a narrow field of a
   generic struct - `src/internal/trace` is blocked by
   `%tN defined with type 'i64' but expected 'i8'`. Same family as the `unique`
   report. Use `-work` to keep the IR and read the store's GEP type against the
   value's type.
5. **generic: `map[T]H[T]` composite literal** fails substitution inside a
   generic function (`cannot use map[int32]unique.Handle as map[T]unique.Handle
   in struct literal`); `var x hm[T]; x.m = map[T]H[T]{}` compiles, so it is the
   literal form.
6. **`internal/coverage/{encodecounter,encodemeta,cformat}` and
   `internal/trace/internal/tracev1`** codegen bugs: `unsafe.TypeOf(*x)` +
   `binary.Write` struct loads, a `text/tabwriter` null-pointer, and a clang
   `-c` failure. Not source-attributable so far.
7. **`tests/util.sh`** masking guards REMOVED and VERIFIED (179 PASS, rc=0).
   The relative-path entry point turned out to be Go's own rule (a bare path is
   an import path), so correct this file's earlier "entry point" note if you
   touch it. A fixture that does not link now FAILS, which is what exposed the
   whole `undefined symbol: main.` family in the first place.
7b. **WOOD LAW: mirror `stripUnusedConsts` into `legacy/`** (see the note
   above). Legacy must drop an unused const declaration before parsing exactly
   as stage4 does, and the mirror must also blank-not-delete so line numbers
   agree across the two compilers.
8. **`crypto/ed25519` DONE and COMMITTED** (`bc4a9067`). Verified by running,
   not by inspection: sign/verify, tamper rejection, `crypto.Signer` through the
   interface, `crypto.PublicKey` -> `ed25519.PublicKey` assertion, `Seed`,
   `Equal`, and `crypto/tls` compiling against the new shapes and its type
   switches resolving.
9. **`encoding/asn1` and `x509` - the DER stack.**
   a. **DONE** (`1f416420`): the OID encoder wrote `00 00 00` for every
      ObjectIdentifier. Fixture `asn1_oid_der`.
   b. **OPEN - `[]byte` is marshalled as a string.** A plain `[]byte` field with
      no string tag falls into `case akBytes:` in `makeField`
      (`src/encoding/asn1/marshal.mx` ~line 607) and defaults to
      `makeUTF8String`, so it should be an OCTET STRING (`makeBytesEncoder`).
      Minimal reproducer (`/tmp/asn1probe`): a struct `{Key []byte}` holding
      `ff 01 02` fails with `asn1: string not valid UTF-8` instead of emitting
      `04 03 ff 01 02`; a `string` field is correct (`30 03 13 01 78`,
      PrintableString). The ROOT CAUSE is the unified `string = []byte`: the
      runtime type descriptor has one kind for both (`akBytes`, value 17 - see
      the comment in `src/encoding/asn1/typeinfo.mx`), so asn1 cannot recover
      the spelling. THE FIX IS IN THE DESCRIPTOR, not in asn1:
      `_rawType.meta` is a `uint8` with `_kindMask = 31` and `_flagNamed = 32`,
      so bits 6 and 7 are free - either mark a descriptor built from the
      `string` spelling with a new flag and let `akBytes` WITHOUT it mean a byte
      slice (asn1 then uses `makeBytesEncoder`), or give `string` its own kind
      value. BILATERAL: `_mxc_stage4/ir_descriptors.mx`, the legacy descriptor
      emitter, and `src/runtime/codec.mx` (which also switches on these kinds)
      must all agree. This is what blocks `x509.CreateCertificate` and the whole
      PKCS#8/`MarshalPKCS8PrivateKey` path.
   c. **OPEN - `x509.ParseCertificate`** fails on standard UTCTime/
      GeneralizedTime DER. `parseTime` (`src/crypto/x509/parser.mx:204`) reads
      through `cryptobyte`'s `ReadASN1UTCTime`/`ReadASN1GeneralizedTime`, so the
      defect is likely in `src/encoding/asn1`'s time decoding
      (`ReadASN1UTCTime`/`ReadASN1GeneralizedTime` in `asn1.mx`) or in how
      `cryptobyte`'s `PeekASN1Tag` reports the tag - check those before touching
      the x509 side.
   d. `x509.CreateCertificate` is blocked on (b). A minimal reproducer for
      (b) is the fastest way back in.
10. **`crypto/ecdsa` + `elliptic` - NEW, found by `/tmp/ecprobe`.** `ecdsa.
      GenerateKey(elliptic.P256(), rand.Reader)` panics
      `interface dispatch: no impl for Params`: a `elliptic.Curve` value boxed
      into the interface is missing the `Params` wrapper. Same family as the
      `f7a746d4` fixes (value-form interface dispatch for pointer-receiver
      methods, `emitIfaceAssertOK` per `(base, form)`). `/tmp/vfp2` shows the
      simple shape works, so this is a *specific* boxing site (a canned/global
      curve value, or a curve returned from a function that is not a plain
      local). Read the `elliptic` curve constructors first.
11. **The named public API types still to dissolve:** `net.IP` (18 methods, ~90
    refs), `net/http.Header` (13, ~227), `net/textproto.MIMEHeader` (5, ~77),
    `net/url.Values` (6, ~20), `net.Buffers` (3, mutating + `io` interfaces, so
    it needs the struct-wrapper route rather than an alias). Downstream app repos
    have ZERO refs to `url.Values`/`net.IP`/`MIMEHeader` and 3 `.Header` refs, so
    the blast radius is small. `crypto/ed25519` (`bc4a9067`) is the worked
    example of the struct-wrapper route.
12. **`syscall/js`** (js && wasm only): value receivers, `any`, globals mutated
    outside init, `FuncOf` captures. Bounded debt; the native wide predicate does
    not need it.
13. **`compress/flate`**: rule-clean, but clang-22 crashes in the backend
    (`Cannot select ... load`); `compress/gzip` links through it.
14. **`regexp` and `text/template` do not exist**: five importers of `regexp`
    (`internal/lazyregexp`, `internal/coverage/pods`, `internal/profile/prune`,
    `internal/trace/testtrace/expectation`, `index/suffixarray`) and one of
    `text/template` (`internal/lazytemplate`). Decision: implement the minimum,
    or remove the importers (all are trace/coverage/profiling corners with no
    app path).
15. **`debug/gosym`** 13 value receivers, **`os.Pipe`'s `[2]int32`** and
    syscall's `[2]Timeval`/`[2]Timespec` casts, **cgo-gated
    `os/user/cgo_lookup_unix.mx`**, and the **embedded tzdata registration that
    never runs** - all with the gosym agent (c96552b0); check its report.
16. **The widened predicate** still waits on items 1, 3, 11, 12, 14 and 15. The
    wide predicate binary `/tmp/mxc-wide-a` is GONE from /tmp; it must be
    rebuilt from the sweep before any package can be re-verified against it.

## Did not survive scrutiny (do not chase)

- "`\x22` in a string mis-scans": false, the scanner decodes it correctly (the
  line is 9 bytes because it is 7 escapes plus a literal `0`).
- "idna fails because of a size/shape-dependent parser bug": false. The
  vendored `tables15.0.0.mx` was missing a closing brace (`_mappingIndex` needs
  `}}`); the agent fixed it in `66df51d3`. My probes looked like they passed
  because I appended the brace myself.
- "`{:` in the idna tables": it does not occur there at all.
- "A two-result generic method loses its values": it was the copied receiver
  (item above), not tuple decoding.
- "The ed25519 struct wrapper is broken because `pub len: 0`": FALSE, and it is
  the reason two real compiler bugs were found. My probe wrote `pub :=
  priv.Public()` (type `crypto.PublicKey`, which has only `Equal`) and then
  called `pub.Bytes()` and `ed25519.Verify(pub, ...)`. Both were silently
  accepted: the unknown method evaluated to nil (so `len` was 0) and the
  interface was passed where the concrete struct was wanted (so the length was
  garbage). The wrapper itself is correct. LESSON: when a probe returns a
  suspiciously zero or garbage value, suspect the probe's TYPES first, and treat
  a silently-accepted type error as a compiler finding - not as an explanation.
- "`asn1` marshals a `string` field as UTF8String, so the string side is also
  broken": false. A printable ASCII string is a PrintableString (`30 03 13 01
  78`), which is correct; I misread `13` as `0c` and wrote the wrong
  expectation into the new fixture twice. Only the `[]byte` side is wrong
  (item 9b).
- "`x509.CreateCertificate` fails because of the ed25519 change": false. The
  OID defect (fixed) and the `[]byte`-as-string defect (open) are pre-existing
  and hit every key type; ecdsa never even reaches them because `elliptic`
  panics first (item 10).


# ============ UPDATE: the position/net-http/const-strip run ============

Green: `3ea479e4` (three compiler fixes), plus the legacy mirror in the commit
before it. Tree dirty ONLY with the asn1/x509 workstream's in-flight edits.

## Closed

**Item 2, the diagnostic remapper - ROOT CAUSE FOUND AND FIXED.** It was never
the concat map. `Parser.pos()` (`pkg/syntax/parse_init.mx`) did
`p.posAt(p.Line-Linebase, p.Col-Colbase)`, so EVERY `token.Pos` was 0-based
while `token.Pos`, that same file's `scanError`/`updateBase`/`NewLineBase`, and
`go/parser` are all 1-based. Consequences: every parse-error message was one
line and one column short; three call sites compensated with `+1`
(`ssa_builder.curLine` twice, `declScope`'s seg lookup); and three more trusted
the 1-based convention and were just wrong - `pkg/rewrite`'s
`ResolveArrayLenFromSrc` and `ResolveArrayLenFromConstMap`, and
`ir_typecheck`'s duplicate `resolveArrayLenFromSrc`. Fixed at the source, all
three compensations deleted. Positions are now exact and verified on seven
single-file cases, one with dropped header lines, and a two-file package with
imports (rm1 13:5, rm2 12:5, t1 3:5, t2 6:5, t3 6:5, t4 4, t5 3, t6 5,
multi-file b.mx:12:5). **Trust a reported position now** - but keep testing it:
this is the first run where it is right.

**Item 1, net/http - IT PARSES.** With positions trustworthy the error was
`doc.mx:108:1`, and it was a REAL position in a body that should never have
been built: `scanFileRegions` classified "package "/"import " lines with no
comment awareness, while `extractPkgName` right beside it already skipped
`/*...*/` (its comment even names fmt/doc.mx). net/http's doc.mx is a doc
comment ABOVE its package clause whose prose contains the line
`package takes precedence over the net/http package's built-in HTTP/2`, so that
became the package clause and the next line (`support.`) the first body line.
`firstCodeOnLine` now skips comments and carries block-comment state from the
top of the file; the directive-comment carve-out (`//:linkname`, `//export`) is
preserved. net/http now parses AND type-checks, and stops on ordinary source
diagnostics - the next work item, not a compiler crash:
- `undefined: nop` (x2)
- `serveContent__anon2: 'err' shadows declaration in enclosing scope` (x3)
- `cannot resolve type of short var handler in (*http2serverConn).processHeaders
  (rhs=nil)`

**NEW BUG, found by that: a compiler SEGFAULT on net/http, fixed.** `gdb`:
`resolveTypeAST <- resolveType <- buildCompositeLit <- buildExpr <-
buildCompositeLitInto`, repeating - infinite mutual recursion, stack overflow.
`buildCompositeLitInto`'s non-aggregate fallback called `buildExpr(e)` on the
SAME literal, and because the literal still carried its type, that path
allocated and came straight back. The reachable construct is the `&T{...}`
shorthand of a pointer-element table:
`formTable = []*formInfo{{...}}`, `return []*cipherSuite{{...}}`. A
pointer-to-struct target now allocates the pointee, fills it, and stores its
ADDRESS; anything else fails loud with the type and position instead of
recursing.

**Item 7b, the wood-law mirror - DONE** (`legacy/loader/conststrip.go` +
`loader.go`). `stripUnusedConsts` existed only in `_mxc_stage4` since
`21a09393`. The mirror hooks `Package.parseFiles`, builds ONE shared reference
map across all of a package's files (stage4's pass sees the concatenated body),
strips per file, and blanks rather than deletes. Verified: the agent's e2e
package, exact-line negative contrasts, and the self-host fixpoint (`s1 != s2`,
`s2 == s3`).

## Open, with the next action

1. **net/http's remaining source diagnostics** (listed above). Now ordinary
   work: `nop` is presumably a missing/dropped helper, the shadowed `err` is a
   real shadowing violation in a closure, and `rhs=nil` is a short var whose
   right-hand side failed to build - start with the `nop` and `rhs=nil` ones
   because both smell like a dropped or unresolvable callee, i.e. the same
   family as the `buildFuncFieldCall`/void-call bugs already fixed.
2. **asn1 `[]byte` (item 9b) - the approach is chosen and in flight, UNCOMMITTED
   and currently red.** The workstream chose to flip the default: `akBytes`
   becomes an OCTET STRING, and a string parameter (`stringLike`) promotes it to
   a character string; `pkix.mx`, `x509.mx`, `asn1.mx`, `common.mx` and
   `marshal.mx` are all being edited. **As it stands the tree breaks the
   committed `regression:asn1_oid_der` fixture at RUNTIME** (it builds and links,
   then the binary exits non-zero), so the full suite cannot go green until that
   workstream finishes or is reverted. Whoever picks this up: run
   `regression:asn1_oid_der` first, then the ed25519 round trip.
3. **`extractImports` (`_mxc_stage4/main.mx`) has the SAME lexical weakness**
   as `scanFileRegions` did - `line == "import ("` with no comment awareness.
   Only import specs are at stake (a comment line inside an import block is
   already collected as a bogus spec), so it is a smaller bug, but it is the
   same class and now has an obvious model to copy (`firstCodeOnLine`).
4. **Item 10, `elliptic.Curve` -> `interface dispatch: no impl for Params`**
   still blocks ecdsa P-256 - unchanged, and a probe exists at `/tmp/ecprobe`.
5. **Items 3, 4, 5 (generic family), 6 (coverage/tracev1), 11 (the named public
   API types), 12, 13, 14, 15** are unchanged from the list above.

## Method note that paid off

The first two fixes were found by refusing to accept a plausible story. "The
reported line is past the end of the file, so the remapper is broken" was wrong:
the arithmetic was fine and the *positions feeding it* were wrong one layer
down, in the parser. And the net/http parse error was not a malformed file at
all - it was the compiler building a body out of a doc comment. Both times the
symptom pointed at the wrong component, and the fix was one layer upstream.

# ============ UPDATE: asn1/x509 DER stack closed ============

`08018874` (+ the fixture). Full suite rc=0, **183 PASS**,
`selfhost:fixpoint` PASS. Tree clean.

## Closed: items 9b and 9c

**9b, `[]byte` marshalled as a string - FIXED.** Moxie unifies string and
`[]byte` into ONE runtime type descriptor (`akBytes`), verified directly:
`unsafe.TypeOf([]byte("abc")) == unsafe.TypeOf("abc")` and `[]uint8` too, so
asn1 cannot recover the spelling (approach (b), a compiler-side split, was out
of scope). The default is now the BYTE meaning - `TagOctetString` - which is
what DER needs, and a character string is requested with a new explicit
`asn1:"string"` field parameter that keeps Go's
PrintableString-when-printable-else-UTF8 choice. A plain `[]byte` field now
marshals `30 05 04 03 61 62 63`; raw non-UTF-8 key bytes marshal
`30 06 04 04 01 ff 80 00` where they previously failed outright with
`asn1: string not valid UTF-8`. Unmarshal accepts an OCTET STRING or any
character string into `akBytes`, and `[][]byte` sequences still parse.

**9c, "x509.ParseCertificate fails on standard DER times" - THE PREMISE WAS
FALSE, and the real defect was elsewhere.** Standard DER times always parsed:
`src/crypto/x509/test-file.crt` gives the correct 2017/2027 times, and
hand-built UTCTime and GeneralizedTime both read back correctly through
cryptobyte. The real blocker was on the **marshal** side:
`[][]pkix.AttributeTypeAndValue` encoded BOTH levels as SEQUENCE. Go decides the
inner level is a SET from the type NAME (`pkix.RelativeDistinguishedNameSET`),
and Moxie **forbids named slice types**, so asn1's "type name ends in SET" rule
can never fire - `t.typeName()` is empty for any slice. `parseName` then read an
ATV SEQUENCE as a SET and returned `x509: invalid RDNSequence`.
`crypto/x509.marshalRDNSequence` now builds SEQUENCE OF SET OF ATV explicitly
for a certificate subject and a CSR subject.

**Verified end to end by running** (ed25519 `CreateCertificate` ->
`ParseCertificate`, every field asserted): serial 4242; Subject/Issuer
CommonName; exact NotBefore/NotAfter; `cert.PublicKey` type-asserts to
`ed25519.PublicKey`; a multi-RDN subject (C=UK plus a non-ASCII CN) whose DER
shows `SEQUENCE{ SET{ SEQ{ OID, PrintableString "UK" } }, SET{ SEQ{ OID,
UTF8String "moxie ee test" } } }`; a CA cert with KeyUsage/BasicConstraints/SKI
parsed (`IsKeyUsage==CertSign|CRLSign`, MaxPathLen, SKI as OCTET STRING-wrapped
DER); and a post-2050 cert using GeneralizedTime parsed to
2055-06-01T12:30:45Z.

## NEW OPEN ITEMS from this work

1. **Struct tags do NOT cross the `.mxh` export boundary.** The writer
   (`_mxc_stage4/main.mx`, the `TCStruct` branch of `mxhWriteTypeName`) emits
   `struct{` then each field's NAME and TYPE and no tag. `pkix.
   AttributeTypeAndValue.Value` is tagged `asn1:"string"`, so an importer sees
   no tag and marshals it as an OCTET STRING - which is exactly why
   `marshalRDNSequence` had to build the value explicitly rather than trust the
   tag. This is a COMPILER defect with a wide blast radius: it silently changes
   the wire format, JSON/ASN.1/struct-tag behaviour, of every tagged struct
   field used across a package boundary. Fix: write the tag into the mxh and
   parse it back in `mxhParseStruct`/`typeFromTail`. Verify with a probe that
   marshals a tagged struct from an IMPORTING package.
2. **`x509.ParseCertificateRequest` fails** with `asn1: structure error:
   sequence tag mismatch`: it unmarshals `[][]pkix.AttributeTypeAndValue`
   directly, which has neither a tag nor a named SET type. A structural
   "slice-of-slices-of-structs => SET" convention on both parse and marshal got
   past the mismatch but then hit a **SIGSEGV in the slice-of-slice parse
   path** - that crash is its own bug and is the thing to fix first, since the
   convention is needed for gap 1's sibling reason (no named slice types).
3. **`CreateRevocationList` and `pkix.RelativeName`** still marshal their RDN
   without SET, now also with OCTET STRING values. A CRL was already
   non-conformant before this change; same root cause as item 1's sibling.

## Corrected list state

Items 1, 2, 7b, 9b, 9c are closed. Still open: 3, 4, 5 (generic family), 6
(coverage/tracev1), 10 (`elliptic.Curve` -> `no impl for Params`, blocks ecdsa
P-256), 11 (the named public API types), 12 (`syscall/js`), 13 (`flate`), 14
(`regexp`/`text/template`), 15 (gosym/os-user/tzdata), 16 (the widened
predicate), the new mxh-tag gap above, and two smaller recorded items:
`extractImports` shares `scanFileRegions`' old comment-blindness, and
net/http now has ordinary source diagnostics left (`nop` undefined, a shadowed
`err`, a short var with `rhs=nil`).

## Method note, second instalment

Two of my own stated premises were wrong this stretch and both are recorded in
the disproven list: `x509` time PARSING was never broken (the defect was the
missing SET on the marshal side), and the asn1 `[]byte` problem was not the OID
encoder (that was real but separate). In both cases the agent verified the
premise before implementing, which is why the fix landed on the real cause. The
verification habit that produced the answer each time was the same: build the
SMALLEST case that isolates the claim - `unsafe.TypeOf([]byte("abc")) ==
unsafe.TypeOf("abc")`, `[]byte("abc")` vs `"abc"` through Marshal, a hand-built
DER time - rather than reasoning from the symptom.

# ============ UPDATE: mxh struct tags ============

`a9a3f35a`. Tree clean.

## Done, with a hard caveat about what is proven

The **mxh now carries struct field tags**: the compiler writes
`type Tagged struct{Name:string#61736e313a22737472696e6722,Raw:[]byte,...}`
(the `#hex` is the field's tag) and `parseTypeDesc`/`mxhSplitFieldTag` decodes
it back, taking `#` only at bracket depth 0 so a nested struct's tagged fields
stay inside the field body and are handled by the recursion. Hex because a Go
tag is arbitrary text (spaces, quotes, braces, commas, colons) while a type line
is whitespace-delimited. **Verified directly on the cached artifact**: the same
line had no suffixes before the change. This also closed a real
stage4/legacy divergence - legacy's type writer already appended
`strconv.Quote(tag)`; only stage4 dropped it.

**WHY IT MATTERS**: struct field descriptors are `linkonce_odr` constants named
after the TYPE, so an importer that rebuilds the struct with empty tags emits a
competing blob under the same symbol as the defining package's tagged one and
the linker keeps whichever it sees. asn1 reads a field's tag from that blob, so
a tagged `[]byte` field (`pkix.AttributeTypeAndValue.Value` is
`asn1:"string"`) can reach an importer untagged and marshal as an OCTET STRING.

**WHAT IS *NOT* PROVEN, and must not be claimed**: that a same-module consumer
ever takes the mxh path. It does not - it re-type-checks the dependency from
source. Tested: strip the `#hex` suffixes out of the cached mxh, delete the
consumer's cached artifacts, rebuild -> still green. So the fixture
`asn1_tag_crosspkg` passes with AND without the fix, and its header says so; it
is coverage of cross-package tagging, not a regression test for the mxh round
trip. **A faithful end-to-end reproduction needs a dependency built into the
cache and then its source made unavailable** (the installed/cached-package
path). That is the next experiment for this item, and until it is run the
user-visible consequence of the tag loss is unconfirmed.

This also retroactively weakens the asn1 conclusion in the previous section:
`pkix.AttributeTypeAndValue` is compiled in the same invocation as `crypto/x509`
and the cert round trip passes, so the "importer saw an untagged []byte" report
was most likely an artifact of the workstream's own in-flight flip rather than a
demonstrated mxh loss. The asn1 flip itself is still correct and verified; what
is unproven is that the mxh was ever the cause of a failure.

## Next action for this item

Build a package into the cache, remove/hide its source, build a consumer that
uses it, and check whether the tag survives. If it does not, the round trip
above is the fix and the fixture should become a real two-stage test (build the
dependency, hide the source, build the consumer) rather than a single-invocation
one.

# ============ UPDATE: item 10 (elliptic) minimized ============

NOT fixed. Minimized, with the mechanism identified. This is a **silent wrong
answer**, not just elliptic's panic.

## The reproducer (5 minutes to rebuild, keep it)

```moxie
package main

import "fmt"

type Point interface{ Set(x int32) (p Point) }

type P256Point struct{ x int32 }
func (p *P256Point) Set(x int32) (out Point) { p.x = x; return p }

// A NON-generic interface, like crypto/elliptic.Curve.
type Curve interface {
	Params() (n int32)
	IsOnCurve(p Point) (ok bool)
}

// A GENERIC type with POINTER receivers, like elliptic's nistCurve[Point].
type CurveImpl[Pt Point] struct{ name string }

func (c *CurveImpl[Pt]) Params() (n int32)        { return 7 }
func (c *CurveImpl[Pt]) IsOnCurve(p Pt) (ok bool) { return true }

func main() {
	c := &CurveImpl[P256Point]{name: "p256"}
	var i Curve = c
	fmt.Println("params:", i.Params())          // prints 0, must print 7
	fmt.Println("oncurve:", i.IsOnCurve(&P256Point{})) // prints false, must print true
}
```

`MOXIEROOT=$PWD ./moxie build -o /tmp/geniface/out /tmp/geniface` then run: it
prints `params: 0` / `oncurve: false` and exits 0. In elliptic the same defect
surfaces as `interface dispatch: no impl for Params` (a panic) instead - the two
failure modes differ only in whether a zero-returning arm matches first.

## Mechanism

`findIfaceImpls` (`_mxc_stage4/ir_iface.mx:349`) builds the dispatch table by
walking `e.pkg.Members` and splitting each method symbol at its LAST dot:

    mname = "CurveImpl.Params"  ->  tname = "CurveImpl"
    looked := e.pkg.Pkg.Scope.Lookup(tname)      // finds the GENERIC Named
    if gn, okg := tn.Typ.(*Named); okg && len(gn.TParams) > 0 { continue }   // line 401

and line 401 **skips it on purpose** (only instantiations are meant to emit
symbols). So no impl is registered for `*CurveImpl[P256Point]` at all.

Evidence: the compiler's own cached artifact for the probe contains the symbols
`CurveImpl.Params` and `CurveImpl.IsOnCurve` and nothing carrying the type
arguments - so the instantiation has no distinct method symbol for
`findIfaceImpls` to key a concrete receiver type on. There is no
`CurveImpl[main.P256Point].Params` to find.

## The fix to write

Something must register the impl with the CONCRETE receiver type
`*CurveImpl[P256Point]` while the body stays the shared `CurveImpl.Params`
symbol. The registration already knows how to build `NewPointer(recvT)`
(line 407); what is missing is the mapping from the generic decl to each
instantiated Named in the package. Candidates, in order of preference:
1. If the instantiation machinery (`tryGenericMethodCall` /
   `resolveMethodCallWithRecv`, which already return nil for `len(TArgs) > 0`)
   records each instantiated Named somewhere, `findIfaceImpls` should consult
   that list and register `(generic method symbol, instantiated recvType)` per
   instantiation instead of skipping.
2. Otherwise the emitter must give instantiated methods distinct symbols
   (mangled with the type arguments) so the existing name-splitting resolves
   them. That is the larger change and is what the earlier `undefined symbol:
   main.(*Box).get` fix was inching toward.

Wood law: whatever lands must be mirrored in `legacy/` - go/ssa + the patched
GOtypes already build a proper method set for an instantiation (that is why the
legacy bootstrap does not hit this), so the divergence is stage4-only and the
fix restores parity.

Once it works, the reproducer above belongs in
`tests/regressions/should_compile/` with the printed values asserted - it is a
silent-zero bug today, so an assertion is the whole test.

## Item 10, mechanism pinned down further

The emitted symbols for the reproducer are `main.(*CurveImpl).Params` and
`main.(*CurveImpl).IsOnCurve` - **there is no type-argument mangling at all**
(`strings` on the cached `.bc`; the mxh carries only the bare `CurveImpl`).
So `mangleGenericName` is NOT involved on this path: one shared body serves every
instantiation, and `findIfaceImpls` strips the pointer from `(*CurveImpl)`, does
`Scope.Lookup("CurveImpl")`, finds the GENERIC Named, and skips it at
`ir_iface.mx:401`. Nothing ever registers a concrete receiver type for the
instantiation, so the invoke has no arm.

**Where the instantiated Named IS known**: at the BOXING site. In
`c := &CurveImpl[P256Point]{}` / `var i Curve = c` the SSA value's type is the
instantiated Named with `TArgs` populated (`pkg/types/tc_types.mx:521`). That is
the natural place to record `(methodName, instantiated recvType)` for the impl
table, rather than trying to reconstruct it later from a mangled symbol that does
not exist. The existing iface-arm machinery (`emitIfaceAssertOK` dedupes per
`(base, form)`, and main.mx around 5406 writes missing arms) is the neighbouring
code to extend; the earlier cross-package fix in this same area is
`f7a746d4`.

So the fix is: **register an impl per instantiated type at the boxing site**,
keyed on the instantiated Named, in addition to the existing per-declaration
scan. It must not regress the value-form vs pointer-form split that
`f7a746d4` and `emitIfaceAssertOK` already handle (`(*CurveImpl).Params` is the
pointer form here).

Not attempted: it needs a compiler change plus the legacy cross-check plus a
verifying build, and doing it half-way would leave the tree worse than the
documented bug.

# ============ UPDATE: net/http is down to ONE diagnostic class ============

`4b40204f`. Full suite rc=0, **185 PASS**, `selfhost:fixpoint` PASS. Tree clean.

## The composite-literal arm was the net/http segfault's real root

`buildCompositeLitInto`'s non-aggregate arm is reached by the port's spelling of
Go's `new(T)` for a **scalar Named type**:

    http2settingsTimerMsg = &http2serverMessage{}   // Go: new(serverMessage)
    type http2serverMessage int32

`dst` is already an allocation of exactly that type and the allocator zeroes it,
so there is nothing to store - **the address is the whole result**. The old
fallback called `buildExpr(e)` on the same node and re-entered itself (the gdb
cycle). Only a non-empty element list is an error now. Note this means the
earlier `[]*T{{...}}` pointer-shorthand fix was a *second, unrelated* instance of
the same re-entry, not the cause of the net/http crash.

## net/http source fixes (each replaces the errors it named)

- `client.mx`: **`nop` was never declared**, though both `setRequestCancel` call
  sites use it (Go declares it in client.go) -> two `undefined: nop`.
- `fs.mx` `serveContent`: the inline multipart-writer closure shadowed the outer
  `err`. Moxie rejects shadowing *including an if-scoped redeclaration*, so the
  three sites are now `werr`/`serr`/`cerr`.
- `fs.mx`/`server.mx`: `bytes.FieldsFuncSeq`, `bytes.SplitSeq` x2 and
  `slices.Sorted(maps.Keys(ms))` are the **iterator APIs**, which live in
  `bytes/iter_wasm.mx` and `maps/iter.mx` and are skipped because they import
  `iter` (banned as an `any`-style constraint). Rewritten to the slice-returning
  equivalents that DO exist: `bytes.FieldsFunc`, `bytes.Split`, and a hand-built
  key slice + `slices.Sort`.

## What is left for net/http

**Exactly one class, 229 diagnostics**: the sovereign-loop scratch rule,
`"':=' declaration inside a loop in a self-mutating method is not allowed: move
the loop scratch into a helper function"`. That is a real source refactor, not a
compiler defect, and it is delegated (agent `f5ead72f`): for each self-mutating
method with a `:=`/`var` inside a loop, move the loop's scratch into a free
function with its own freed-at-return arena and keep only the receiver write in
the method. Rationale in CLAUDE.md's Sovereign Arena section - a mutating
method's allocations persist for the life of the receiver, so loop scratch in
such a method leaks one allocation per iteration.

Sites named in the diagnostics: `(*routingIndex).addPattern`, `(*conn).serve`
x3, `(*Transport).roundTrip` x3 (+1 plain `var`), `(*Transport).tryPutIdleConn`
x2, `(*Transport).queueForIdleConn` x2, and more.

## Item 10 (elliptic / generic iface dispatch) - the other layer found

`dispMethodsByPkg` is documented as "the mxh-derived dispatch index: package key
-> the **non-generic** method names its mxh declares", and `findIfaceImpls` uses
it to SKIP packages that cannot contribute an impl. So there are **two** skips
stacked on this bug:

1. the tier filter skips a package whose index lacks the method name (the index
   deliberately records only non-generic names), and
2. `ir_iface.mx:401` skips the generic `*Named` itself.

Fixing it needs both: the mxh index must record generic method names, and the
instantiated `*Named` (whose `TArgs` is set) must be available to key the impl -
which it is only at the boxing site. Both are compiler changes plus a legacy
cross-check, so it stays open and is not started half-way.

# ============ UPDATE: extractImports fix ATTEMPTED AND REVERTED ============

Attempted: give `extractImports` (`_mxc_stage4/main.mx`) the same comment
awareness `scanFileRegions` now has, by calling `firstCodeOnLine(data, ls, le,
inBlock)` and tracking the block-comment state across lines, so a doc comment
containing the text `import (` cannot contribute a bogus import spec.

**DONE in round 79, by delegation instead of by copying the logic.** Re-writing
the scan inside `extractImports` is what legacy could not lower (`unknown special
variable with appending linkage: @0` after LowerInterfaces). The scanner does not
need to be written twice at all: `scanFileRegions` already returns the import
specs, so `extractImports` is now

	_, _, fimps := scanFileRegions(data, map[string]bool{})
	for _, spec := range fimps {
		if imp := mxutil.ExtractQuoted(spec); imp != "" {
			ss = appendUniq(ss, imp)
		}
	}

one comment-aware scanner, with the raw specs (quotes, aliases, blank identifier)
reduced to paths at the boundary. Verified that legacy can lower it -
`legacy/moxie build -a -o /tmp/s1 ./_mxc_stage4` returns 0, where the previous
shape failed - and the whole suite is green on it.

**What is and is not proved.** `import_in_comment` pins the shape: a real `/* */
block whose prose contains `import (` plus a quoted path, with the real import
after it. It passes BEFORE and AFTER the change, because the bogus spec is only
ever added to the dependency-discovery list and an unresolvable entry there is
tolerated - it never reaches the work dir, the cache or the mxh. So the honest
claim is that this removes a duplicated scanner that could drift from the
parser's (the failure mode the two copies invite), not that it fixes an
observable build failure. A file whose prose block swallowed the rest of the scan
could also have had REAL imports missed; that is the latent half.

Result: **it broke `selfhost:stage1`.** The suite went from rc=0 / 185 PASS to
rc=1 with only 72 PASS and

    selfhost:stage1  FAIL
      legacy compiler failed: [mem] after LowerInterfaces ...
      error: unknown special variable with appending linkage: @0

The change is reverted (tree back to only the net/http workstream's files). Not
diagnosed, and the clue is worth keeping:

- `src/net/http` is NOT in the compiler's closure (only comments in
  `_mxc_stage4` mention it), so the concurrent net/http refactor could not have
  caused this - the `extractImports` change did.
- The failure is in the LEGACY compiler compiling the stage4 source, after
  LowerInterfaces, and it is an LLVM linkage error: an appending-linkage global
  (`@llvm.used` / `@llvm.compiler.used` / `@llvm.global_ctors`) referenced as
  `@0`, i.e. a global whose emitted NAME is the literal "0".
- So this is a legacy-side IR-emission bug triggered by a construct in the new
  code, not a semantic problem with the fix. The next attempt should bisect: add
  only the `firstCodeOnLine` call (keeping the old whitespace trim), build
  `_mxc_stage4` with legacy directly (`legacy/moxie build -a -o /tmp/s1
  ./_mxc_stage4`), and see whether it is the multi-value call, the extra
  parameter, or the loop-carried `inBlock` that legacy cannot lower. If legacy
  cannot express it, the fix has to be written in a shape legacy already
  handles - wood law says the legacy side is the one that must be satisfied
  first, since it is the bootstrap.

Priority: lower than item 10 and net/http. The bug it closes is small (a comment
line inside an import block is already collected as a bogus spec, and only the
dependency-scan path uses `extractImports`), so it must not be retried in a way
that risks the bootstrap again without the bisect above.

## Also closed with no code change

`runtime_makeStrongFromWeak` and any surviving `strongFromWeak` reference:
**gone**. `grep -rn 'makeStrongFromWeak|strongFromWeak' src/ _mxc_stage4/
legacy/` returns nothing, so the `weak` removal (`6cd8a7ca`) already closed that
objective item - there is no dead reference left to remove.

# ============ UPDATE: generic instantiation root cause, typed constants, net/http ============

Commits this stretch: `f73b4670` (descriptors), `ab055a7b` (constants),
`5cf911b8` + `49f2c482` (net/http). Full suite green at 185 PASS with
`selfhost:fixpoint` PASS after each compiler change. Tree clean.

## THE ROOT CAUSE OF THE WHOLE GENERIC-INSTANTIATION FAMILY

`typeDescName` returned `named:pkg.Name` for a Named and **dropped TArgs**, so
two instantiations of one generic type shared ONE descriptor symbol and ONE
descriptor name. Verified before the change: the object contained a single
`named:main.CurveImpl` and a single `struct:{Name:basic:bytes}` for
`CurveImpl[P256Point]` and `CurveImpl[P384Point]`, while the monomorphizer had
correctly emitted `(*CurveImpl).TagOf__main.P256Point` and
`...__main.P384Point`. Two bugs at once: `linkonce_odr` descriptors for two
different layouts collide, and interface dispatch cannot tell the instantiations
apart, so an arm for one answers for the other.

After the fix: `named:main.CurveImpl[named:main.P256Point]` and
`named:main.CurveImpl[named:main.P384Point]`.

The name is also a ROUND TRIP - `emitTypeDesc` re-derives the Type from the tail
via `typeFromTail` - so `tailStripTArgs` (bracket-depth-0 split) and
`instantiateTail` were added; without them the field re-derived as
`basic:invalid` and the struct descriptor was emitted under a name that was
never defined:

  error: use of undefined value
  '@reflect/types.type:struct:{b:named:main.Box[basic:int32]}'

Caught by `tests/regressions/should_compile/generic_type_method.mx`.

This is a stage4-only divergence: legacy's writer returns `t.String()` for a
`*types.Named`, and Go's `Named.String()` already prints an instantiation's
arguments.

## Constants: a TYPED constant now keeps its type

Two defects behind net/http's `time.Duration` cluster:
1. `checkAssignable` judged a typed constant by the untyped kind-class rule
   (`constClass` lumps every integer kind together), so
   `const Nano int64 = 1; var y int32 = Nano` was ACCEPTED. Now only genuine
   untypedness takes that leniency.
2. `tcInferConstTypeFromExpr` did not resolve a reference to another constant, so
   every spec after the first in a const group fell through to
   `UntypedTypeOfCV`: `Microsecond = 1000 * Nanosecond` lost `Duration`, so
   `time.Second` was untyped and `5 * time.Second` defaulted to int32 and
   overflowed. It now reads the referenced constant's recorded type (plain name
   and `pkg.Const`).

Verified with minimal probes at 1/2/3/4 chain depths plus the minimal
`const Nano int64 = 1` / `var y int32 = Nano` case.

## net/http: type-checks completely now

Order of the blockers, each one exposed by fixing the previous:
1. 229 `; ` rule violations -> 0 (37 sovereign-loop + 192 shadowing; they were
   TWO classes, not one).
2. 12 positioned type errors -> 0: six were the `time.Duration` cluster (fixed by
   the constant work), three were a pointer where a value is wanted
   (`Header()`, and both `Consume` returns), one was `uint8 as rune` (Moxie's
   range-over-string yields bytes), three were the `handler` method value.
3. `cannot resolve type of short var handler ... (rhs=nil)` -> gone, same cause
   as the method value.
4. **WHAT REMAINS: one clang failure.**

     ./tmp/mxc-build-*/._src_net_http.ll:7338:13: error:
     '%t4' defined with type 'ptr' but expected 'i32'
       store i32 %t4, ptr %ml4

   `-a` reproduces it, so it is not a stale descriptor cache. It became
   reachable only because the fixes let codegen run at all (before, the type
   errors stopped the build), so there is no baseline to diff against. The
   immediate suspect is the field whose type changed from a one-word func to a
   two-word interface (`http2unstartedHandler.handler`), but `llvmNamedType`
   DOES unwrap a Named-over-interface to `ifaceType()`, so the `i32` comes from
   somewhere else. NEXT STEP: keep the IR. It is streamed to
   `./tmp/mxc-build-<pid>/<safeName>.ll` and deleted at exit, so run the build in
   the background and copy the file while clang runs, or find the flag that sets
   `printIR` (main.mx:6178/6197) and capture stdout. Then read the store's
   destination GEP and the value chain around line 7338.

## fmt: a Named type over a basic prints "?"

FOUND, NOT FIXED (two attempts reverted rather than left unverified):

    type Dur int64
    var d Dur = 68
    fmt.Println(d)        -> ?        (must print 68)
    fmt.Println(int64(d)) -> 68
    type WithString int64; func (w *WithString) String() string {...}
    fmt.Println(w)        -> custom   (a Named type WITH its own String works)
    fmt.Println(byte(65)) -> 65       (a universe named basic works)
    fmt.Println(int32(66))-> 66

`?` is `fmt`'s `unknownType` (print.mx:359): `printArg`'s default arm calls
`handleMethods`, and when it returns false `unknownType` writes a bare `?`.
`handleMethods` decides by an interface assertion, so the missing piece is an
assert/dispatch entry for the Named type. Adding an arm to `findIfaceImpls`'
`moxie.stringer` block that borrows `runtime.(*int64).String` for the Named and
`*Named` receiver types did NOT change the output, so the assertion path is not
reading that table the way assumed. NEXT STEP: instrument `findIfaceImpls` to
report whether it registers an arm for the named type at all, then find which
table `emitIfaceAssertOK` consults for a Stringer assertion. This is broad - it
makes `fmt` useless for `time.Duration` and every named basic type.

# ============ UPDATE: the net/http codegen error is a BLANK RESULT bug ============

Tree clean at `454e78e1`. The gated state is unchanged (suite was green at 185
PASS with the compiler as committed; the only change since is `454e78e1`, which
is stdlib source and not in the compiler's closure).

## Fixed this round

`454e78e1`: `(*ConnState).String` indexed `map[ConnState]string` with `c`, a
`*ConnState` (the port's pointer receiver where Go has a value), so codegen
passed the receiver POINTER as the key and stored it into the key's i32 slot:

    '%t4' defined with type 'ptr' but expected 'i32'
      store i32 %t4, ptr %ml4
      %t4 = load ptr, ptr %t1        ; %t1 holds the receiver pointer

Now `_stateName()[*c]`. The build advances to the next error, so this site is
fixed; the package still does not build.

## The remaining error, and what it actually is

    (*Transport).getConn  ->  '%cext100' defined with type 'i64' but expected 'ptr'
      %cext100 = extractvalue {i64, ptr} %t168, 0
      store ptr %cext100, ptr %cet101
      ; then encoded as reflect/types.type:pointer:named:http.persistConn
      %cext102 = extractvalue {i64, ptr} %t168, 1
      ; then encoded as the error interface

The exit serialiser took ONE interface value (the error) and decomposed its two
words as the two return values, feeding the interface's TYPE word to the codec as
the `*persistConn`. **The cause is a blank result**: getConn's signature is

    func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (_ *persistConn, err error)

so `NamedResults` holds ONE entry (`err`) while the signature has TWO results. A
temporary print in emitReturn, gated on `sig.Results.Len() > 1`, produced:

    DBGRET (*Transport).getConn ssaResults=1 sigResults=2 named=1 atline=0

`atline=0` means the offending `SSAReturn` carries no source position - it is
synthetic or position-less, NOT one of getConn's three explicit two-value
returns (`return nil, er6`, `return r.pc, r.err`, `return nil, er7`). Finding
which synthetic return loses a result is the next step; candidates are
`buildReturn`'s self-tail-call path (`isSelfTailCall` -> jump to `tailBlock`) and
the synthetic return builders at ssa_builder.mx 1154/1238/4385/5659 (4385 and
5659 build a SEPARATE wrapper/thunk function, so they would print a different
function name, which is why the tail-call path is the prime suspect).

## Two more real defects found on the way, ATTEMPTED AND REVERTED

1. **The bare-return value list mis-aligns named results with result
   POSITIONS.** In `buildReturn`'s `len(fb.namedResults) > 0` branch the named
   results are pushed in order and then paired with `Signature.Results.At(i)`.
   For `(_ *Inner, err error)` that compares `err` against `_`'s type:
       main.mx:22: cannot use value of type interface{Error() string; String()
       string} as *main.Inner in return statement (an interface value needs a
       type assertion)
   A bare return in such a function is legal Go and must yield BOTH results with
   the blank one zero. Nothing in the tree hits it yet except net/http's shape,
   so it is invisible today.
2. **The exit serialiser's single-value shortcut keys on the wrong count.** In
   `emitReturn`, `if len(e.curFunc.NamedResults) == 1` takes the "this value is
   the whole result" path. It must require `Signature.Results.Len() == 1` too:
   NamedResults is the NAMED results, which is not the result set once a result
   is blank. This is what turns one interface value into a two-result payload
   for the codec.

I implemented both (aligning named results to positions, zeroing the blank one
with `NewSSAConst(&ConstNil{}, res.Typ)`, and widening the shortcut's guard), and
a focused probe - `func (h *Holder) Get(bad bool) (_ *Inner, err error)` with a
bare `return` - PASSED against it. That pass was a CACHE ARTIFACT: the probe build
did not use `-a`. Made into a fixture
(`tests/regressions/should_compile/blank_result_bare_return.mx`) and run by the
suite with a clean build, it FAILED with a different codegen error:

    '%t10' defined with type 'i32' but expected '{ ptr, i64, i64 }'
      %cc29 = extractvalue {ptr, i64, i64} %t10, 0

so the `ConstNil` zero I synthesised for the blank result is not a valid value
for the codec path. **Both changes and the fixture are reverted**, and the suite
is back to its gated state. The next attempt should build the blank result's
zero the way the rest of the compiler does (find how `nil` reaches an SSA value
for a pointer result - `NewSSAConst(&ConstNil{}, t)` is evidently not it, or the
type passed is wrong), and MUST verify with `-a`.

LESSON, again: a passing probe is only evidence if the build was forced. Every
compiler verification in this session used `-a` except this one, and it cost a
revert.

# ============ UPDATE: the net/http codegen chain, and the blank-result fix ============

`6a356673` (compiler) and `4cde9fdf` (source). Full suite rc=0, **185 PASS**,
`selfhost:fixpoint` PASS. Tree clean.

## FIXED: the implicit-return result loss (this was the getConn blocker)

The builder synthesises a return when a body falls off the end, and it pushed
ONLY the named results. `getConn`'s signature is `(_ *persistConn, err error)` -
ONE named result, TWO results - so the synthetic `SSAReturn` had one element,
the exit serialiser took its single-value shortcut, and the codec decomposed that
one interface across both results, feeding the error's type word to the codec as
the `*persistConn`. The value list is now built positionally: `vals[i]` belongs to
`Signature.Results.At(i)`, a blank result occupies its position as the zero of its
type, and named results fill the rest in order.

Verified at the IR level (a body that falls off the end cannot be written
deliberately - Moxie rejects a missing return - so this path is reached only when
the checker misses a terminating shape):

    %rv98 = insertvalue {ptr, {i64, ptr}} undef, ptr null, 0           ; blank = null
    %rv99 = insertvalue {ptr, {i64, ptr}} %rv98, {i64, ptr} %t168, 1   ; err

and by the build advancing two functions: 53560 (getConn) -> 132232
((*http2PriorityParam).IsZero) -> 171708 ((*http2responseWriter).Push).

An earlier attempt appended the zeros after the named results and INVERTED the
aggregate; it was reverted, and the lesson (a probe only counts with `-a`) is in
the previous section.

## FIXED: (*http2PriorityParam).IsZero compared a pointer with a struct

`return p == http2PriorityParam{}` with a pointer receiver. Go has a value
receiver, so it must dereference: `*p == http2PriorityParam{}`. Same class as
`(*ConnState).String` (454e78e1). A pointer/struct comparison is a Go type error
that stage4 accepted - worth a check, like the map-index key check that would
also have caught ConnState earlier.

## The pattern in net/http, and what it means

Every remaining net/http failure so far has been the SAME porting class: Go's
VALUE receiver became a POINTER receiver (Moxie bans value receivers), and the
body still uses the receiver where Go's value was already the value. Instances so
far: `ConnState.String` (map index), `http2PriorityParam.IsZero` (comparison),
`http2FrameHeader.Header` and `http2FrameWriteRequest.Consume` (returns), and the
`sc.handler.ServeHTTP` method values (5cf911b8). EXPECT MORE OF THESE - the cheap
way to find them all at once is to add the two missing checks (map-index key
assignability, pointer/struct comparison) and let the compiler list every site
instead of discovering them one clang error at a time.

## NEXT BLOCKER: select-send codegen in (*http2responseWriter).Push

    '%t193' defined with type 'ptr' but expected '{ i64, ptr }'
      %t193 = load ptr, ptr %t183
      store {i64, ptr} %t193, ptr %selsv289
      store ptr %selsv289, ptr %selval288
      ... [3 x {ptr, ptr, i32, ptr}] zeroinitializer   ; the select case table

`{i64, ptr}` is what an INTERFACE looks like in this IR (an i64 typeID plus a
data pointer), so the select's send-value slot expects an interface while the
value is a plain pointer. The source at h2_bundle.mx ~6988 is
`case sc.serveMsgCh <- msg:` (a 3-case select with one send). NEXT STEP: decide
whether the port sends a pointer where an interface is wanted (source fix) or the
select-send path fails to box the value (compiler fix) by looking at
`serveMsgCh`'s element type, and check `%t183`'s definition in the captured IR.
The IR capture recipe (find + copy while clang runs) is in the section above.

# ============ UPDATE: value CONTEXT (sends box, bare returns align) ============

Round 48. Tree: three fixes UNCOMMITTED while the gate runs. Compilers built from
the tree: `/tmp/mxc-send4` (send coercion only) and `/tmp/mxc-bare1` (send
coercion + bare-return alignment).

## FIXED (uncommitted): a SEND must give its value the channel ELEMENT type

`_mxc_stage4/ssa_builder.mx`: new `coerceSendValue(val, elem)` plus its use at
BOTH send sites - the plain `case *syntax.SendStmt` in `buildStmt` and the
`case *syntax.SendStmt` in `buildSelect`'s `SSASelectState` loop.

Class: a send value's context is the channel's ELEMENT type, exactly as a
parameter type is the context of an argument, and stage4 was storing the raw
value. Two shapes followed from its absence:

- `serveMsgCh <- msg` with `serveMsgCh` a `chan http2serveMsg` and `msg` a
  `*http2startPushRequest`: the pointer was stored into the interface's 2-word
  slot -
    `'%t193' defined with type 'ptr' but expected '{ i64, ptr }'`
    `store { i64, ptr } %t193, ptr %selsv289`
  which blocked `(*http2responseWriter).Push`.
- `errc <- nil` into a `chan error`: an untyped nil has no SSA type at all
  (`llvmType(nil)` is `"void"`), so the send emitted
  `store void null, ptr %chansv29` ("void type only allowed for function
  results") from `net/http`'s `readPreface__anon1`. `coerceSendValue` turns it
  into the nil OF THE ELEMENT TYPE.

LEGACY PARITY (wood law): x/tools' builder already does this. Its vendored
`builder.go` emits `emitConv(fn, b.expr(fn, comm.Value), chtyp.Elem())` in both
places (the select-comm clause ~1790 and `case *ast.SendStmt` ~2850). This is
stage4 catching up to legacy, not a new rule.

## FIXED (uncommitted): a bare `return` fills result POSITIONS, blanks included

`_mxc_stage4/ssa_builder.mx`, `buildReturn`'s bare-return arm (`s.Results ==
nil`). It pushed ONLY `fb.namedResults`, and `NamedResults` holds only the NAMED
results - one entry for `(_ T, err error)`. The one-element `SSAReturn` then made
`emitReturn` take its single-value shortcut, and the exit serialiser decomposed
that single value across every signature slot:

- `(_ *persistConn, err error)` with a first result that is an interface -
    `'%cext769' defined with type 'i64' but expected '{ i64, ptr }'`
    `store { i64, ptr } %cext769, ptr %cet770`
  (this is the round-47 net/http blocker, and the same class as the getConn
  `%cext100` error fixed for the IMPLICIT return in `6a356673`),
- `(_ *E, err error)` with a concrete first result, which instead reports at the
  type level - `cannot use value of type interface{...} as *main.E in return
  statement`, because the blank slot was holding the error.

The arm is now positional: `vals[i]` belongs to `Signature.Results.At(i)`, a
blank result occupies its position as the zero of its type, and the named
results fill the remaining positions in order. Same alignment the implicit
return at the end of a body already uses.

LEGACY PARITY (wood law): x/tools does this already -
`Function.results` (`func.go: addResultVar`) has one entry per result SLOT,
blanks included (`for _, n := range field.Names` sees `_`), so its `Return`
carries every slot and legacy's `emitArenaEpilogue` is driven by
`instr.Results`. Stage4 catching up again.

The two fixes above are the SAME defect family: a value stored where its context
demanded a different type. That is why the net/http chain produced them
back-to-back.

## FIXED (uncommitted): `src/net/http/h2_bundle.mx` passed an error to `panic`

Moxie's `panic` takes a string; the port passed `fmt.Errorf(...)` straight
through, so the emitter called `runtime._panic` with an interface where a string
is wanted (`'%t14' defined with type '{ i64, ptr }' but expected
'{ ptr, i64, i64 }'`). Now `panic(fmt.Sprintf(...))`. Ordinary port bug, not a
compiler defect.

## NEW FIXTURE: `tests/regressions/should_compile/blank_result_bare_return.mx`

Pins the bare-return alignment with three shapes: `(_ error, err error)`,
`(n int64, _ error, err error)` (blank in the MIDDLE), and `(_ *E, err error)`.
Each asserts the blank slot is the ZERO of its type and the named slot kept its
value, so a swap fails both branches rather than passing one.

Observed evidence, fresh compiler each time:

- PRE-FIX (`/tmp/mxc-send4`, `-a`): fixture rc=1 -
  `main.mx:57: cannot use value of type interface{Error() string; String()
  string} as *main.E in return statement` (and a same-shape probe fails clang
  with `value doesn't match function result type '{ { i64, ptr }, { i64, ptr }
  }'`).
- POST-FIX (`/tmp/mxc-bare1`, `-a`): fixture builds, runs, prints `ok`, rc=0.

## net/http advanced past the round-47 blocker

`'%cext769' ... at .ll:246679` is GONE. The chain moved to:

    ./tmp/mxc-build-*/._src_net_http.ll:296054:12: error:
      '%t64' defined with type '{ ptr, i64, i64 }' but expected 'i8'
      296054 |   store i8 %t64, ptr %mu18

`{ptr, i64, i64}` is a STRING and `%mu18` is a MAP-UPDATE alloca (`ir_map.mx`
names both the key and the value temp `mu`, lines 66 and 69), so a string is
being stored into an `i8` key/value slot. NEXT STEP: the enclosing function name
(IR capture with a pid-targeted watcher, below) decides whether the map's
declared key/elem type is wrong or the ported body passes a string where a byte
is wanted - the same `(*ConnState).String` class of port error, but this time
caught in the map-update store instead of a map index.

## Gate status at the time of writing

- `tests/run.sh --full` with `/tmp/mxc-send4` (send coercion alone): in flight,
  83 PASS / 0 FAIL at phase 5 (wasm). Not yet a result.
- `/tmp/mxc-bare1` (both compiler fixes): no full-suite run yet.
- Neither compiler fix is committed until a full suite is green with
  `selfhost:fixpoint` PASS.

## Remaining queue (objective items, unchanged by this round)

1. Generic monomorphization: imported generic types carrying no methods / the
   method set of an instantiated generic (`dispMethodsByPkg` records only
   non-generic names and `ir_iface.mx:401` skips the generic `*Named`; item 10),
   the `i64`-vs-`i8` narrow field store in `src/internal/trace` (`base.mx:148
   d.present[id/8] |= uint8(1) << (id % 8)`), and the `map[T]H[T]` literal
   substitution failure.
2. `elliptic.Curve` -> `interface dispatch: no impl for Params`, blocking
   `crypto/ecdsa` P-256.
3. `internal/coverage/{encodecounter,encodemeta,cformat}` and
   `internal/trace/internal/tracev1` codegen.
4. The named public API types in `src/net/**`: `net.IP`, `http.Header`,
   `textproto.MIMEHeader`, `url.Values`, `net.Buffers` (struct-wrapper route).
5. `syscall/js` (js && wasm only).
6. `compress/flate` clang-22 backend crash.
7. `regexp` and `text/template` do not exist.
8. `debug/gosym`, `os.Pipe`'s `[2]int32`, syscall's `[2]Timeval`, cgo-gated
   `os/user/cgo_lookup_unix.mx`, the embedded tzdata registration that never
   runs.
9. `x509.ParseCertificateRequest`'s `[][]ATV` SET plus the slice-of-slice parse
   SIGSEGV.
10. `extractImports` shares `scanFileRegions`' old comment-blindness (reverted
    once; needs the bisect).
11. stdlib capturing deferred closures are mis-compiled (13 files).
12. The widened predicate sweep (items 1, 3, 11, 12, 14, 15 gate it).
13. `fmt` prints `?` for a Named type over a basic (`time.Duration`) - but only
    for NUMERIC verbs. Measured in round 53 with a probe:

        var d time.Duration = 1500000000
        d.String()        -> "1.5s"
        Sprintf("%v", d)  -> "1.5s"      (was the broken case; now correct)
        Sprintf("%s", d)  -> "1.5s"
        Sprintf("%d", d)  -> "?"         (Go prints 1500000000)
        Sprintf("%v", MyInt(42)) with its own String() -> "MyInt!"
        Sprintf("%v", int32(7))  -> "7"

    So the Stringer path through `fmt` is fixed and what is left is the numeric
    verbs: `%d`/`%x`/... on a Named type whose underlying type is a Basic.
    `fmt` takes `...Stringer`, so there is no numeric method to call, and a
    Named-over-basic has no promoted one; Go reaches the underlying int64 by
    reflection, which Moxie does not have. Narrow, not broad - and it needs a
    decision (a promoted numeric formatter on Named-over-basic, or leave `%d`
    on such a type unsupported).
14. `runtime_makeStrongFromWeak`: closed with no code change (no references).

# ============ UPDATE: round 49 — generic aggregates, dead aggregate loads, phase 3 ============

Round 49 landed four stage4 compiler fixes, one stdlib source fix and one test
harness fix. Each was verified with a fresh `-a` compiler and an isolated
reproducer; the suite gate is `tests/run.sh --full`.

## 1. Monomorphization must substitute type parameters inside AGGREGATES

`substTypeParams` had cases for `*TypeParam`, `*Pointer`, `*Slice` and
`*Signature` only, so a substituted field that was an aggregate came back still
holding the parameter: `m map[K]V` is a `TCMap`, `s []entry[K,V]` is a `Slice`
wrapping a `Named`. `llvmType` has no case for a `TypeParam` and its fallback is
`"i8"`, so `mapping[string,*node]`'s map slot was `alloca i8` receiving a
`{ptr,i64,i64}` string key and `entry[K,V]` lowered to the layout `{i8, i8}`:

    store i8 %t64, ptr %mu18
    clang: '%t64' defined with type '{ ptr, i64, i64 }' but expected 'i8'

This is net/http's `mapping[K,V]`/`entry[K,V]` (src/net/http/mapping.mx), the
first blocker behind `src/net/http`.

- New `substTypeIn(t, sub)` (`_mxc_stage4/ssa_builder.mx`) walks `TypeParam`,
  `Pointer`, `Slice`, `Array`, `TCMap`, `TCChan`, `TCStruct`, `Named` and
  `Signature`. `substTypeParams` now delegates to it.
- A bare generic written inside a generic declaration (`s []entry[K,V]` inside
  `mapping[K,V]`) carries no `TArgs` of its own, so its parameters are bound from
  `sub` by NAME — the same binding Go applies.
- The `Named` case moves only the ARGS and leaves `Under` as the generic body.
  Minting a substituted struct and hanging it off `Under` gave the descriptor
  pass a type nobody had declared:
    use of undefined value '@reflect/types.type:struct:{s:slice:
    named:main.entry[basic:invalid,basic:invalid],m:map:{...}}'
- The LAYOUT is instead resolved in the emitter: `llvmTypeInner`
  (`_mxc_stage4/ir_types.mx`) now lays out a `Named` that carries `TParams` +
  `TArgs` by substituting the body for the duration of that one string. Only a
  layout comes out of it, so nothing new enters the descriptor stream.
- Legacy needs no mirror: x/tools + go/types instantiate generics before legacy
  codegen ever sees them. Verified — `legacy/moxie build` accepts the reproducer
  with rc=0.

Reproducer: `tests/regressions/should_compile/generic_aggregate_subst.mx`
(string/`*node`, `int32`/string and string/`int64` mappings, each crossing
`maxSlice` so both the slice and the map paths run, plus a range over the map
through a `func(k K, v V) bool` parameter). Pre-fix: rc=1 with the clang error
above. Post-fix: builds, runs, prints `ok` on native and builds for wasm.

## 2. `tailSplitTop` must count `[` `]` as well as `{` `}`

The descriptor NAME for an instantiated generic carries a comma list in
brackets (`named:main.entry[basic:invalid,basic:invalid]`). `tailSplitTop`
tracked only brace depth, so splitting the `struct:{...}` tail cut one field into
several — `struct:{s:slice:named:main.entry[a,b],m:map:{...}}` became THREE
fields, `s`, a field literally named `basic`, and `m`. The type rebuilt from
that name therefore named itself
`struct:{s:slice:basic:invalid,basic:basic:invalid,m:map:{...}}`, the definition
and the `named:` descriptor's reference disagreed, and clang rejected the module.
Fixed in `_mxc_stage4/ir_descriptors.mx`.

## 3. A discarded receiver value leaves dead aggregate loads

`buildCall` / the defer path truncate the instructions emitted for the receiver
when the resolved method needs its ADDRESS, but the test was
`methodNeedsAddr(fn2, fixedRecv)` — and `resolveMethodCallWithRecv` has usually
already taken that address itself, so asking about its own result could never
report "a value where a pointer is wanted". The rollback never ran and the
discarded value stayed as dead IR. For a large struct the dead IR is a
full-struct load; `flate.(*Writer).Close` (`return w.d.close()`) emitted

    %t4 = getelementptr inbounds {HUGE}, ptr %w, i32 0, i32 0
    %t5 = load {HUGE}, ptr %t4, align 8

a 640KB copy nothing read. clang's instruction selector, lowering that aggregate
load field by field, hit an `i1` field and aborted:

    fatal error: error in backend: Cannot select: 0x...: i1,ch = load<(load (s8)
    from %ir.t4 + 655496, align 8)>

Fixed by testing `methodNeedsAddr(fn2, recv)` — the operand that was just built.
Verified: the dead load is gone from `(*Writer).Close`.

## 4. `src/net/http/h2_bundle.mx` passed an `error` to `panic`

`panic` takes a `string`; the two `panic(fmt.Errorf(...))` sites are now
`panic(fmt.Sprintf(...))`.

## 5. `tests/phase3_ir_validate.sh` never validated anything

The phase reported PASS for a failing build. Three separate defects:

- `rm -rf "$td"` ran INSIDE the loop, so from iteration 2 the log redirect
  failed, the build never started, and the non-zero rc was turned into
  `PASS ("0.0s (linker only)")` by a guard that accepted any non-zero rc whose
  log did not match a crash pattern. A build that did not complete is a FAILURE.
- `find "$td" -name '*.ll'` never matched: the compiler writes scratch under
  `./tmp/mxc-build-<pid>/`, and it unlinks each module right after clang. `-work`
  keeps that directory, and `-a` is needed or the cache answers everything and
  nothing is emitted.
- `opt -verify` is retired syntax in LLVM 22; it needs `-passes=verify`
  (with `-disable-output`, or opt echoes the module).

The phase now does ONE forced build of `pkg/types` — which pulls in `pkg/token`,
`pkg/syntax` and `pkg/mxutil` — verifies EVERY emitted module with
`opt -passes=verify`, and requires each package to have been compiled. Verified
standalone: 4/4 PASS. `-o /dev/null` replaces the executable target, because
these are libraries and the tree has no `main.main`; the link failing for want
of `main` is not this phase's subject, and the old guard used exactly that
failure to fabricate a pass.

## 6. Where net/http now stands

`src/net/http` reaches the link with all 155 modules compiled — the `%mu18`
map-slot blocker, the bare-return `%cext769` blocker and the descriptor blocker
are all gone. It now stops in a dependency, `compress/flate`, on a second LLVM
backend crash:

- `@flate.NewWriter` segfaults inside `SelectionDAG::ReplaceAllUsesWith` during
  DAG combine. Its IR still contains the same class of dead aggregate load
  (`%t6 = load {HUGE Writer}, ptr %t5`, unused) from `var dw Writer` followed by
  `dw.d.init(w, level)`. Fixing the `Close` instance moved the failure from
  `Cannot select` on `(*Writer).Close` to this segfault on `NewWriter`; the
  remaining instance is not caught by the receiver rollback, so the receiver
  expression for a local `var` of a huge struct still emits a value load before
  the address. Next step: find where `var dw Writer` + `dw.d.method()` builds the
  value, and stop emitting the dead load at its source.

## Suite

`tests/run.sh --full` with the final compiler (`/tmp/mxc-r49f`, installed as
`./moxie`): phases 0-5 give 181 PASS and 0 FAIL, `selfhost:fixpoint` PASS
(stage2 == stage3), and phase 6 gives 63 PASS and 0 FAIL when re-run with a real
Go toolchain exported (its legacy half needs `go env GOROOT`; see below). The 8
FAILs in the phase-6 section of the combined run were the hardcoded
`/tmp/moxie-goroot`, now fixed - not one of them was a compiler disagreement.

Phase 3 now really validates IR: it verifies every module the build emits.
Phase 5's `wasm:push_index_nested_slice` failed once with
`Failed to fetch ... /jsruntime/loader.mjs`, which is a harness flake, not
codegen; it passed on the next run. Note that a single FAIL in phase 5 ends
`run.sh` early (`set -e`), so phase 6 does not run at all in that case - that is
why phase 6 needs its own invocation to be seen.

# ============ UPDATE: round 50 — compress/flate's backend crash is fixed ============

## The two LLVM backend crashes in `compress/flate`

Round 49 removed the dead aggregate load from `flate.(*Writer).Close` and the
crash moved to `@flate.NewWriter` (`SelectionDAG::ReplaceAllUsesWith`
segfault). Both remaining causes were in the same family — a huge aggregate
operation that never needed to exist — and both are fixed:

### A field select must address its base, not load it and throw the load away

`buildSelectorAddr` built the operand's VALUE with `x := fb.buildExpr(e.X)` and
then replaced it with the variable's alloca when `e.X` was an addressable local
or global. The load stayed in the instruction list as dead IR, and for a large
struct that dead IR is a full-struct copy. `flate.NewWriter`
(`var dw Writer`, then `dw.d.init(w, level)`) emitted

    %t5 = call ptr @runtime.alloc(...)
    store { ...[131072 x i32], i1, i1 ... } zeroinitializer, ptr %t5
    %t6 = load { ...[131072 x i32], i1, i1 ... }, ptr %t5     ; dead, 640KB

The addressable case is now resolved BEFORE anything is emitted for the operand.

**A regression this introduced, and how it was caught.** The first version used
the alloca directly for every variable. But the alloca holds the variable's
VALUE: for a struct-valued variable the alloca IS the address, while for a
pointer-valued variable (`o := &outer{}`) the alloca holds the pointer and the
select has to LOAD it first. Addressing the alloca directly read `o.in.n` out of
the stack slot holding `o` and printed `2270074` instead of `41`. It was caught
by a probe that prints every field rather than by a single pass/fail check, and
it is pinned by
`tests/regressions/should_compile/field_select_base_and_zero_store.mx`, which
FAILS on the intermediate compiler (the fixture has teeth) and passes now.

### A zero store of an aggregate is a memset

`emitZeroInit` exists (ir_emit.mx) precisely because one
`store {big} zeroinitializer` is expanded by the backend into one store per
8 bytes, while `llvm.memset` lowers to a loop. It was only ever used for allocas;
an explicit zero STORE went out as the aggregate form. On flate.Writer's 640KB
that is ~80k stores, and clang's instruction selector aborted on one of them.
`emitStore` now routes a zero store of an aggregate through `emitZeroInit`.

Result: `MOXIEROOT=$PWD /tmp/mxc-r50c build -a -o /tmp/flate.bin
./src/compress/flate` produces NO clang error at all. It ends with
`ld.lld-22: error: undefined symbol: main.main`, which is the expected link
failure for a library and is not this item's subject.

### Left standing

`flate.Writer` still carries `[131072 x i32]` and `[32768 x i32]` fields. Arrays
are supposed to be gone from this tree ("an array type is leftover Go
slice-bypass code"), so those fields are the next thing to rewrite as slices —
512KB and 128KB of struct that no zeroing or copying wants to touch at all. The
crash is fixed; the layout is still wrong by the project's own rule.

## `hash/crc32`'s hardware kernels did not exist

With flate compiling, `src/net/http` reached the LINK for the first time and
reported:

    ld.lld-22: error: undefined symbol: hash/crc32.castagnoliSSE42
    ld.lld-22: error: undefined symbol: hash/crc32.castagnoliSSE42Triple
    ld.lld-22: error: undefined symbol: hash/crc32.ieeeCLMUL
    ld.lld-22: error: undefined symbol: main.main

`crc32_amd64.mx` declared those three as bodiless functions "defined in
crc32_amd64.s"; the file does not exist anywhere in the tree and the `//:noescape`
assembler marker has no pass behind it. `crc32_arm64.mx` did the same for
`castagnoliUpdate`/`ieeeUpdate`. A `grep -E "^func [^{}]*$"` over `src/` reports 443 bodiless
declarations, but that number overstates the risk: it counts files behind build
tags that are never compiled (e.g. `internal/race/race.mx` is `//:build race`
while its no-op twin `norace.mx` carries `//:build !race` and has every body).
The ones that matter are the ones a real link reaches; crc32's were the first.

A CRC is a function of its input, not of the instruction that computed it, so the
same update is now written in Moxie: `crcRawUpdate`, the reflected register
update with NO pre- or post-inversion — which is the form the hardware kernels
have and the form the callers expect, since both invert themselves
(`^ieeeCLMUL(^crc, ...)`, `crc = ^crc; ...; return ^crc`).

Verified three ways, in `tests/regressions/should_compile/crc32_hw_kernels.mx`:

- the standard check values: IEEE("123456789") == 0xCBF43926 and
  Castagnoli("123456789") == 0xE3069283;
- the arch path agrees with the generic table path for 9, 63, 64, 503, 504, 505,
  4031, 4032, 4033 and 10000 bytes. `Update` selects the arch kernels only for the
  instance's OWN tables, so a table built by a second instance falls through to
  `simpleUpdate` — that is how both paths for the same polynomial are compared;
- the 10000-byte values (ieee 1494190728, castagnoli 369340837) match an
  independent non-Moxie oracle, and the IEEE one matches `zlib.crc32` exactly.

## `src/net/http` — where it stands after round 50

Every codegen blocker is gone: the build compiles all 155 modules with **zero
clang errors**. The link then needed the crc32 symbols above, now provided.

The remaining link errors cannot be re-tested in this environment: the compiler's
module resolver needs the `golang.org` checkout under `$MOXIEPATH`, and it is not
on disk any more. The build logs `auto-fetch golang.org` and then
`fatal: repository 'https://golang.org/' not found`, which is a network fetch for
a path that is not a git repository. The earlier runs in this round got through
because the checkout was still present; nothing in the tree provides it now
(`find` finds no `hpack.mx` and no `golang.org` directory anywhere).

That is worth a look on its own: the checkout disappeared between two builds in
the same session, and a fetched module root is not scratch — if the compiler
registers it as scratch it will be deleted on the first successful build and the
next build will always try to re-clone.

# ============ UPDATE: round 51 — a subslice ALIASES, and the rule says it copies ============

## The documented subslice rule does not match the compiler

CLAUDE.md states, and the porting method depends on: "There is no aliasing: a
subslice is a copy. `s[a:b]` allocates and copies. A write through a subslice
does NOT reach the parent, so Go code that relied on overlapping views - in-place
XOR, 'read src fully before writing dst', `copy` overlap reasoning - is wrong
here." It is also in the compaction preservation line.

Measured with the installed compiler, in one program (all four rows print the
parent's element, not the copy's):

    a := []int32{:4}; full := a[:];    set(full)   -> a[0]=7
    b := []int32{:4}; part := b[0:2];  set(part)   -> b[0]=7
    c := []int32{:4}; tail := c[1:];   set(tail)   -> c[1]=7
    d := []int32{0,0,0,0}; e := d[:2]; e[0] = 5    -> d[0]=5

and across a call boundary, where the rule would matter most:

    func tailOf(s []int32) (t []int32) { return s[1:] }
    a := []int32{1,2,3,4}; t := tailOf(a); t[0] = 99
    -> a[1]=99, t[0]=99        (a method returning h.buf[1:] behaves the same)

Three more, covering the forms porting actually leans on:

    func onParam(s []int32) { s[1:3][0] = 55 }
    a := []int32{1,2,3,4}; onParam(a)         -> a[1]=55
    b := "abcd"; c := b[1:3]; c[0] = 'Z'      -> b="aZcd"
    d := []int32{1,2,3,4}; d[1:3][0:1][0]=42  -> d[1]=42

**Within a frame a subslice is a VIEW.** The one thing that does hold is arena
safety at the escape boundary: a subslice of a callee-local slice, returned to
the caller and then read after the callee's arena has been refilled with fresh
allocations, still reads the right value (t[0]=2, len=3) - the return
relocation copies it into the caller's arena. So the accurate model is:
aliases while the backing is alive, relocated when the owning frame dies.

That is not the same model as the rule, and the difference is load-bearing:

- Code written to the rule (defensive copying before writing, rewriting Go
  in-place algorithms) is not wrong, only slower.
- Code that relies on aliasing - Go's own in-place XOR, `copy` overlap, "read
  src fully before writing dst" - actually works here.
- A future change that makes subslices copy "to match the rule" would silently
  break all of that.
- The os.Pipe comment in src/os/file_unix.mx cited the rule ("p[:] would be a
  copy") as its rationale. That claim is false and the comment is corrected;
  the code was always fine either way.

Decision needed: is the RULE wrong (and should be rewritten to the
alias-while-live / relocate-on-escape model) or is the IMPLEMENTATION wrong
(and subslices must start copying)? Everything ported under the current rule
assumes one of the two. Round 51 records it rather than changing either.

## The rule-9 arrays in os.Pipe and syscall

- `os.Pipe` was already converted to `p := []int32{:2}`.
- The `[2]Timeval` / `[2]Timespec` in `src/syscall` are the CAST TARGET of
  `(*[2]Timeval)(unsafe.Pointer(&tv[0]))` for the utimes/futimesat ABI, and
  `pipe2`'s `*[2]_C_int` is the same shape. There is no slice spelling of "a
  pointer to two consecutive Timevals" - Moxie removed slice-to-array
  conversion - so these must stay arrays. The public API already takes slices
  (`Utimes(path, tv []Timeval)`).
- `caps.data [2]capData` and `iflags.name [IFNAMSIZ]byte` are struct fields
  whose layout IS the kernel ABI (`struct caps`, `struct ifreq`); a slice field
  would change the layout and the ioctl/capget would read garbage. They stay.
- `mapPipe [2]int32` and `var p [2]int32` are passed as `mapPipe[:]` / `p[:]` to
  `forkExecPipe`, which fills them through `Pipe2`. Under the rule as written
  that looked like a lost write; under the measured behaviour it is a view and
  works. Left alone, with the measurement above as the reason.

## A program that imports net/http now builds, links and RUNS

    MOXIEROOT=$PWD mxc build -o probe-nh.bin ./probe-nh    # imports net/http
    ./probe-nh.bin

    wrote GET /a/b?q=1 HTTP/1.1
    Host: example.com
    User-Agent: Go-http-client/1.1
    Accept: text/plain
    X-Test: hello

    panic: ReadRequest

The whole closure compiles (zero clang errors), links, and the program runs:
`http.NewRequest`, `url` parsing, `Header.Set`, and `Request.Write` all produce
exactly the right bytes - the request line, Host, and both headers, in order.

`http.ReadRequest(bufio.NewReader(buf))` over the buffer just written then
returned an error and the probe panicked on it. **RESOLVED in round 52** - see
the section below: every letter was being rejected as a header byte by the
128-bit constant in `validHeaderFieldByte`. The same probe now prints `ok` with
rc=0, so the whole round trip - request construction, `url` parsing,
`Header.Set`, `Request.Write`, `bufio`, `textproto.ReadMIMEHeader` - is verified
end to end. The old text follows for the diagnosis trail. That is the next thing to look at, and the first candidate is the same
class this round kept finding: the reader is handed as an INTERFACE
(`*bytes.Buffer` -> `io.Reader` -> `bufio.NewReader`), and a value stored in an
interface at a different type than the callee expects is how several of the
earlier net/http blockers presented. Second candidate: `bufio.Reader` over a
`bytes.Buffer`. Third: `readRequest` rejecting the shape, which is least likely
given the bytes above are a well-formed request.

To diagnose cheaply, print the error instead of panicking on it
(`gerr.Error()` / `gerr.String()`), then read `readRequest`'s failure paths.
The probe takes ~20 minutes to build, so get the whole diagnosis out of one run.

## Operational: background builds must be detached

Every long build in this round that was started with `nohup cmd &` from a tool
call DIED silently when that call returned - the harness kills the process
group, and `nohup` only ignores SIGHUP. It reads exactly like an OOM or a
compiler crash at whatever stage it reached (`IR emit`, `compile objects`) and
cost several rounds of misdiagnosis. Use
`setsid bash -c '...' > log 2>&1 < /dev/null &` so the build owns its own
process group, or keep the tool call alive until it finishes.

# ============ UPDATE: round 52 — every MIME header failed, and it is the const-fold bug ============

## `http.ReadRequest` failed because every LETTER was rejected

`net/http`'s round trip was the last functional blocker. `ReadRequest` returned
an error on a request `Request.Write` had just produced correctly. Isolating it
with a cheap `bufio` + `net/textproto` probe (no net/http closure, so seconds
instead of twenty minutes) put the failure in `textproto.ReadMIMEHeader`:

    ................ ..... .......................... (ReadLine x4 all correct)
    6 ReadMIMEHeader err=malformed MIME header line: Host: example.com
    7 bytes.Cut ok=true k=[Host] v=[ example.com]      (Cut is fine)

The line is well formed, `bytes.Cut` finds the colon, the value bytes are
printable - so the rejection had to be `canonicalMIMEHeaderKey` returning
`ok=false`, which it does when `validHeaderFieldByte` rejects a byte. That
function built its bitmap as ONE 128-bit untyped constant:

    const mask = 0 | (1<<(10)-1)<<'0' | (1<<(26)-1)<<'a' | (1<<(26)-1)<<'A' | ...
    return ((uint64(1)<<c)&(mask&(1<<64-1)) | (uint64(1)<<(c-64))&(mask>>64)) != 0

Measured, with the same expression in a standalone program:

    mask int64 view = 288068722172624896
    mask>>64        = 0
    mask low 64     = 0
    c=72 (H)  -> false      c=111 (o) -> false      c=65 (A)  -> false
    c=122 (z) -> false      c=126 (~) -> false      c=57 (9)  -> true
    c=45 (-)  -> true       c=33 (!)  -> true       c=32 ( )  -> false

**Both halves of the 128-bit constant come back zero**, so every letter is
rejected, so every real header name is rejected, so every MIME header block
fails to parse. Only the sub-64-bit characters (digits, `-`, `!`, ...) survived.

This is the SAME constant-folding precision bug the objective names for
`time.Date`/`time.Parse` - "Moxie's folder goes through a float64 and loses the
low bits of a ~9.2e18 intermediate" - reached through a different expression,
and it is a second, much simpler reproduction of it.

### Fixed

`validHeaderFieldByte` now uses two 64-bit halves, neither of which needs more
than 64 bits:

    const (
        validHeaderFieldLo = (1<<(10)-1)<<'0' | 1<<'!' | ... | 1<<'.'
        validHeaderFieldHi = (1<<(26)-1)<<('a'-64) | (1<<(26)-1)<<('A'-64) | ... | 1<<('~'-64)
    )
    func validHeaderFieldByte(c byte) (rok bool) {
        if c < 64  { return validHeaderFieldLo&(uint64(1)<<c) != 0 }
        if c < 128 { return validHeaderFieldHi&(uint64(1)<<(c-64)) != 0 }
        return false
    }

Verified: `ReadMIMEHeader err=<nil> host=[example.com] xtest=[hello]
accept=[text/plain]`. The new fixture
`tests/regressions/should_compile/mime_header_parse.mx` covers the whole tchar
set from RFC 7230 and FAILS pre-fix with
`malformed MIME header line: content-type: text/plain`.

Note this is a symptom-level fix by necessity: the ROOT cause is the folder, and
the folder is shared with `time.Date`/`time.Parse`. Until the folder is exact,
every >64-bit constant in the tree is a landmine, and this one was found only
because it happened to gate HTTP parsing. The folding fix (plus its legacy
mirror) is still the real item.

## Second site of the 128-bit bitmap bug: `mime.isTokenChar`

Round 57 found the same 128-bit untyped-constant bitmap in `src/mime/grammar.mx`
that round 52 fixed in `src/net/textproto/reader.mx`. Both halves came back zero,
so every letter was rejected as a token character and

    mime.ParseMediaType("text/plain; charset=utf-8")
      -> "mime: no media type"

`mime` is on net/http's Content-Type path and net/mail's, so this was live. Fixed
with the same two-64-bit-halves split; verified `mediatype=[text/plain]
charset=[utf-8]`, and the new fixture `mime_media_type_parse.mx` panics pre-fix.

Also found while sweeping for >64-bit constants, and NOT yet fixed:

- `src/math/j0.mx`, `j1.mx`, `jn.mx` (several sites): `Two129 = 1 << 129`, used
  in float arithmetic. Losing the high bits makes `Two129` wrong, so the Bessel
  functions are wrong for large arguments.
- `src/math/lgamma.mx`: `Tiny = 1.0 / (1 << 70)`.
- `frac`-style helpers in the same files.

These are the same class and should be checked with a probe rather than
rewritten blind - `1 << 129` feeding a float comparison may or may not change the
result. Sweep for `<<` with a count >= 64 and for 128-bit mask literals; two
have now been found only because they gated parsing.

## NEW and separate: an unsigned constant >= 2^63 converts to a NEGATIVE float64

Found in round 58 while probing the >64-bit constants in `src/math`. This one is
entirely WITHIN 64 bits and is a silent wrong value:

    float64(1<<62)   = +4.61168601842739e+018   correct (fits int64)
    float64(1<<63)   = -9.22337203685478e+018   want +9.223372036854776e+18
    float64(1<<63+1) = -9.22337203685478e+018   want +9.223372036854776e+18
    float64(1<<64-1) = -1.00000000000000e+000   want +1.8446744073709552e+19

`cvFloat` handles the `Unsigned` marker correctly (`float64(uint64(ci.V))`), so
the mark is being LOST before it gets there. **DISPROVEN in round 59**: making
`MaskConstToBasic`'s `Int64` branch preserve `Unsigned` instead of dropping it
changed nothing - all four probe lines stayed identical - so that branch is not
on this path. `ConvertConstLocal`'s float arm (`fv := cvFloat(v)`) is also
correct, so the loss is somewhere between the fold and the conversion call.

Next step: build the probe with the IR wrapper and read what the constant
becomes at the conversion, rather than guessing another branch. Two hypotheses
have now been eliminated by experiment; the third attempt should be
evidence-driven.

Blast radius: `1 << 64 - 1` is `math.MaxUint64` and appears in
`src/internal/runtime/math/math.mx`, `src/math/const.mx`,
`src/math/bits/bits.mx` (three sites), `src/internal/runtime/sys/intrinsics.mx`,
`src/saferio/io.mx`, `src/debug/elf/file.mx` and `src/net/http/h2_bundle.mx`
(twice). Any of those converting to float64 gets -1.

## NEW: `math.J0` is wrong for arguments >= 2

    math.J0(1.0)   = +7.65197686557967e-001    expect  0.7651976865579666   ok
    math.J1(1.0)   = +4.40050585744934e-001    expect  0.4400505857449335   ok
    math.J0(2.5)   = -4.04278302239057e-001    expect -0.04838377646819797 WRONG

J0 is right for |x| < 2 and badly wrong above it, so the fault is in the
rational-approximation branch (j0.mx), not in the `Two129` guard. Unexplored -
recorded so the finding is not lost. `Lgamma(3)` and `Gamma(3)` are correct.

## The `>= 64` shift sweep, completed

Round 62 swept the whole tree (`src/`, `pkg/`, `_mxc_stage4/`) for `<<` with a
count >= 64, because three live consumers of the >64-bit constant limitation had
been found only by accident. Result: **15 sites, one of them still broken.**

- `src/math/jn.mx:57,236` - `Two302 = 1 << 302`, twice, in `Jn` and `Yn`. Fixed to
  `0x1p302`. Same failure as j0/j1: the guard `x >= Two302` was true for every
  positive x, so `Jn(2,2.5)` was 0.411370710840181 against a true 0.4460590584396173.
- Every other hit is `1<<64 - 1` (MaxUint64), which FITS uint64 and is correct:
  `math/const.mx`, `math/bits` (x3), `internal/runtime/math`,
  `internal/runtime/sys/intrinsics`, `saferio/io.mx`, `debug/elf/file.mx`,
  `strconv/atoi.mx`, `net/http/h2_bundle.mx` (x3), plus the `(1<<64-1)/x` division
  forms.
- `src/mime/grammar.mx`'s `isTSpecial` was flagged and is **NOT affected**: all
  its tspecials are ASCII below 64, so its mask never exceeded 64 bits. Measured
  pre- and post-change behaviour on quoted-string escapes (`\(` unescaping) and
  RFC 2231 percent-encoding and they were identical, so the edit was reverted as
  cosmetic rather than committed as a fix. `isTokenChar` in the same file WAS
  affected (its mask spans bits 33..126) and was fixed in round 57.
- Huge decimal literals flagged by the sweep (`nistec.mx`'s curve primes,
  `strconv/decimal.mx`, `strconv/itoa.mx`'s digit pairs) are inside STRING
  arguments (`bigFromDecimal("...")`, `{9, "..."}`, `"2021..." | "..."`), not
  numeric constants. Not affected.

Method note: the sweep's first cut matched digits inside string literals and would
have sent me chasing `strconv` and `crypto/elliptic`; reading each hit before
acting is what kept that from becoming three wasted rounds.

## Operational: cheap isolation beats the expensive end-to-end probe

The net/http probe costs ~20 minutes to build, and the failing layer was found
in ~1 minute by probing `bufio` + `net/textproto` alone, then ~1 minute more to
test `bytes.Cut` and the mask expression. Build the smallest program that can
still fail, not the smallest program that contains the feature.

# ============ UPDATE: round 55 — a parenthesised CONSTANT as a method receiver is garbage ============

Chasing round 54's "where is the exactness lost" question turned up a
different, much more basic bug. Measured with the installed compiler:

    a := 42
    a.String()          -> 42                   correct (typed variable)
    (42).String()       -> 180388626474         want 42
    (40+2).String()     -> 180388626474         want 42
    (1<<26).String()    -> 180455735296         want 67108864
    (1<<26-1).String()  -> 288230376218820607   want 67108863
    (1<<26+1).String()  -> 288230371923853313   want 67108865

The value is deterministic, not uninitialised memory, and the shape is exact:

    180388626474 = 42<<32 | 42
    180455735296 = 67108864<<32 | 67108864
    288230376218820607 = 67108863<<32 | 67108863

That is **x<<32 | x** - the low 32 bits replicated into the high half. So a
parenthesised constant expression used as a METHOD RECEIVER is materialised by
replicating its 32-bit value instead of widening it. It is a 32/64-bit widening
bug on the constant-receiver path, and it is silent.

This is also what the round-54 probes were actually hitting: `(1<<70).String()`
and `(1<<26-1)<<'A'>>64` were not mis-folded 128-bit arithmetic so much as
rendered through this path. **Re-examine the >64-bit const-fold item in that
light before doing any big.Int work** - the reported garbage values
(9741466863665152, 288068722172624896, 9741466930774015) all have this
replicated-32-bit smell, and `1<<26-1` needs no more than 64 bits at all.

**FIXED in round 56** (`08cdbd61`). The cause was a type/width mismatch across
two functions: `resolveMethodCallWithRecv` resolves the builtin Stringer from the
operand's DEFAULT type (`UntypedToTyped` maps an untyped int to int64), while
`resolveBuiltinMethod` allocated its receiver temporary at the OPERAND's type -
i32 for a folded constant. `(*int64).String` then read 8 bytes out of a 4-byte
alloca. The temporary is now allocated at the resolved type and `emitStore`
widens into it. Verified: all six shapes above return the right value, a typed
variable is unchanged, negative constants sign-extend (`(-7).String()` is
`-7`), and a named type with its own Stringer still wins. The new fixture
`const_receiver_width.mx` panics pre-fix with
`(40+2).String() = 180388626474`.

Consequence for the item below: the >64-bit "garbage" values recorded in round
54 were largely this bug. `mask low 64` now reports 288068722172624896 instead of
0 - the low half of the 128-bit mask is folded correctly and only the high half
is still out of reach (`mask>>64` is still 0).

# ============ GOAL CLOSE: rounds 47-60 — what landed, what is left ============

The 60-round objective ended at round 60 with work remaining. This is the
consolidated hand-off. `git log --oneline e2286ed0..HEAD` has the detail;
this section is the summary a fresh context needs.

## Landed, each verified by running, each committed

| what | evidence |
|---|---|
| send values coerced to the channel element type | `errc <- nil` / `serveMsgCh <- msg`; x/tools already did this, so it was stage4 catching up |
| bare `return` fills result POSITIONS, blanks included | net/http's `%cext769` and the "cannot use value ... as *main.E" errors |
| generic type params substituted inside AGGREGATES (`substTypeIn`) | net/http's `mapping[K,V]`; fixture `generic_aggregate_subst.mx` |
| `tailSplitTop` counts `[` `]` as well as `{` `}` | the descriptor-name split that mangled a struct into 3 fields |
| field-select base addressed, not loaded-and-discarded | 640KB dead struct load in flate; fixture `field_select_base_and_zero_store.mx` |
| zero store of an aggregate is a memset | flate's second clang crash |
| `hash/crc32`'s asm-only kernels implemented in Moxie | undefined `castagnoliSSE42`; fixture `crc32_hw_kernels.mx`, checked against an independent oracle |
| `net/textproto`'s 128-bit header bitmap split in two | every MIME header failed; fixture `mime_header_parse.mx` |
| `mime`'s 128-bit token bitmap split in two | `ParseMediaType` failed; fixture `mime_media_type_parse.mx` |
| builtin Stringer receiver allocated at the RESOLVED type | `(42).String()` was `42<<32\|42`; fixture `const_receiver_width.mx` |
| phase 3 actually verifies IR (`-work`, `-a`, `opt -passes=verify`) | was reporting PASS for builds that never completed |
| phase 6 resolves GOROOT instead of hardcoding `/tmp/moxie-goroot` | 8 legacy cases were failing on a missing toolchain |
| `//:wasmtest skip` marker in the wasm phase | native-only fixtures no longer fail the wasm phase |

**net/http compiles all 155 modules with zero clang errors and a program that
imports it builds, links and RUNS** - request construction, `url` parsing,
`Header.Set`, `Request.Write`, `bufio`, `textproto.ReadMIMEHeader` round-trip
correctly. `compress/flate` compiles; `hash/crc32` links.

Suite: **198 passed / 0 failed / 0 skipped**, `selfhost:fixpoint` PASS
(stage2 == stage3). The installed `./moxie` is byte-identical to the compiler
that produced that run.

## Left, with the strongest available lead for each

1. ~~**`float64(unsigned >= 2^63)` is negative and `float64(1<<64-1)` is -1.**~~
   **FIXED in round 61** (`6b9a7064`). Reading the IR (rather than guessing a
   fourth branch) showed the runtime `sitofp i64 -1 to double`: `conversionOp`
   answers from the source TYPE, and an untyped integer constant's type says
   nothing about the `Unsigned` marker the folder put on its VALUE. One override
   in `emitConvert` keyed on the marker fixes it. Verified positive and correct
   for `1<<62`, `1<<63`, `1<<63+1`, `1<<64-1`, with `(-1)` and `-(1<<62)` still
   negative, plus typed `uint64` and a typed `uint32` above 2^31 (also wrong
   before). Fixture `unsigned_const_to_float.mx`.
2. **The constant folder is not exact above 64 bits.** Two 128-bit bitmaps were
   found only because they gated parsing; `math/j0.mx`/`j1.mx`/`jn.mx` still have
   `Two129 = 1 << 129` and `lgamma.mx` has `Tiny = 1.0/(1 << 70)`. The fix is the
   one `ConstFloat.Exact` already demonstrates (`ConstInt.Exact`), and `math/big`
   was verified exact at these sizes in round 54 - but do it AFTER item 1, which
   is cheaper and may explain some of the same symptoms.
3. **`math.J0` is wrong for |x| >= 2** (`J0(2.5)` = -0.404, want -0.0484). The
   rational-approximation branch, not the `Two129` guard. `J1`, `Lgamma`, `Gamma`
   are correct.
4. **Named public API types in `src/net/**`** (`net.IP`, `http.Header`,
   `MIMEHeader`, `url.Values`, `net.Buffers`) - unchanged, struct-wrapper route.
5. **`src/syscall/js`**, **`debug/gosym`**, **`internal/coverage/{encodecounter,
   encodemeta,cformat}`**, **`internal/trace/internal/tracev1`** - unchanged.
6. **`regexp` and `text/template` do not exist** (`src/regexp`,
   `src/text/template` are absent; `pkg/mxregexp` is not the stdlib package).
7. **`crypto/ecdsa`** blocked by `elliptic.Curve` -> `interface dispatch: no impl
   for Params`.
8. **`x509.ParseCertificateRequest`** `[][]ATV` SET and the slice-of-slice SIGSEGV.
9. **`fmt` numeric verbs on a Named-over-basic** (`%d` on `time.Duration` is `?`);
   `%v` and `%s` are fixed.
10. **The documented subslice rule does not match the compiler.** CLAUDE.md says a
    subslice is a copy; measurement in round 51 shows every form ALIASES within a
    frame, while escaping subslices are relocated at the return boundary. Neither
    the rule nor the implementation was changed. This is a decision the project
    has to make, and everything ported under the current rule assumes one of the
    two.
11. **443 bodiless declarations** across `src/` - the count includes build-tagged
    files, so it overstates the risk; crc32's were the ones a real link reached.

## Method notes worth carrying forward

- **Verify the premise before implementing.** Four rounds were spent on premises
  that turned out stale (`time.Date` already fixed; the >64-bit "garbage" was
  mostly the constant-receiver bug; the `MaskConstToBasic` branch).
- **Build the smallest program that can still fail.** The textproto failure was
  isolated in ~1 minute with a `bufio`+`net/textproto` probe instead of a
  20-minute net/http build.
- **Background builds must be detached** (`setsid`), or the harness kills the
  process group at the tool-call boundary and it reads as an OOM or a crash.
- **A guard that turns a failure into a pass is a bug.** phase 3 did exactly
  that for an unknown number of rounds.

# ============ UPDATE: round 61+ — float `++` was a no-op, and trig was broken with it ============

## `x++` on a float silently did nothing

`buildAssign`'s `Rhs == nil` branch (which is `x++` / `x--`) built the literal 1 as
a **ConstInt** and merely labelled it with the operand's type:

    one := &SSAConst{typ: lv.SSAType(), val: &ConstInt{V:1}}

so the right-hand domain never matched a float operand and the statement compiled
to nothing. Measured:

    y := 2.0; y++       -> y is still 2.0
    f := 1.5; f++       -> f is still 1.5
    i := int64(2); i++  -> 3        integers never affected
    y += 1              -> 3        compound assignment never affected

Fixed (round 61, `d13d84c5`): the 1 is a ConstFloat when the operand is a float.
Fixture `float_incdec.mx` panics pre-fix with "y++ on float64".

## The trig functions were wrong because of it

`math.Sin`/`Cos`/`Sincos` reduce the argument with `j++; y++` where y is a
float64. With `y++` a no-op the reduced argument was wrong and the functions
returned a valid sin/cos PAIR for the wrong angle:

    Sin(1.0) = 0.977061263899476    want 0.8414709848078965
    Cos(1.0) = -0.212958415159296   want 0.5403023058681398

Correct for x <= 0.785 (octant 0, no reduction), wrong above it - which is what
identified the reduction rather than the polynomials. After the fix Sincos, Sin,
Cos, Tan, Asin and Atan2 are all correct for arguments that need quadrant
reduction, and `math.J0` moved from -0.404278302239057 to -0.0723182119715167
(true value -0.04838377646819797).

## `math.J0`/`J1`/`Y0`/`Y1` were wrong for x >= 2 - `Two129` folded to 0

Now RESOLVED (round 61, `4aa52746`). `j0.mx` and `j1.mx` guard their asymptotic
branch with

    Two129 = 1 << 129      // |x| > ~6.8056e+38
    if x > Two129 { ... skip pzero/qzero ... }

and Moxie does not evaluate a constant wider than 64 bits exactly, so `1 << 129`
folded to 0 - **`x > Two129` was true for every positive x**. The shortcut was
taken permanently, `pzero`/`qzero` were never called, and every result for
x >= 2 was wrong with the right shape and sign:

    J0(2.5) = -0.0723182119715167   want -0.04838377646819797
    J1(2.5) = +0.499417646028127    want +0.4970941024642741
    Y0(2.5) = +0.499417646028127    want +0.4980703596152319

`Two129` is the exact hex float `0x1p129` now, and `lgamma.mx`'s
`Tiny = 1.0 / (1 << 70)` is `0x1p-70`, which was the same problem.

**How it was found, because the method mattered more than the fix.** The error
was small and systematic, so the suspect was the coefficient tables. Evaluating
`pzero(2.5)` and `qzero(2.5)` with an independent reference (solving J0 and Y0's
two equations for pu and qv) proved both correct to 16 digits, which ruled the
tables out and pointed at the guard. A specific suspicion that `qzero`'s
`(-0.125 + r/s) / x` was a factor-of-ten typo against the `s*(-1.25 + R/S)` in its
own comment was **wrong** - the reference confirms the `/x` form - and it is
recorded here so nobody re-opens it.

Correct now: J0 at 2.0/2.5/3.0/8.0/10.0, J1 at 2.5/10.0, Y0/Y1 at 2.5, Lgamma(3),
Gamma(3), Sin(1.0). Fixture `bessel_large_x.mx` panics pre-fix with "J0(2.0)".

This is the THIRD live consumer found for the >64-bit constant limitation
(textproto's and mime's 128-bit bitmaps were the first two), and the first where
the symptom was a wrong number rather than a rejection. Until the folder is
exact, sweep for `<<` with a count >= 64: those three are only the ones a test
happened to reach.


# ============ UPDATE: round 63 — the folder now keeps >64-bit integer constants exactly ============

`ConstInt` held only `V int64` plus an `Unsigned` marker, so an untyped integer
constant needing more than 64 bits lost its high bits. Four separate defects were
fixed one at a time in source before this (net/textproto's and mime's 128-bit
bitmaps, math's `Two129` and `Two302` guards); this lands the folder half of the
root cause.

`ConstInt` gains `Exact *big.Int`, mirroring `ConstFloat.Exact` - which is how the
untyped-FLOAT case (time.Date/time.Parse) had already been fixed. `intFoldBig`
folds exactly when either side carries Exact or the operation can mint one (a
shift), `intConstBig` keeps `V` as the low 64 bits so every existing reader keeps
working, and `cvInt`, `cvFloat` and the comparisons prefer Exact.

Verified by diffing the same probe against the same compiler without the patch:
exactly two lines differ, both fixes, nothing regressed.

    ((1<<26-1)<<'A') >> 64    0 -> 134217726
    (1<<70) >> 64             0 -> 64

## FIXED in round 64 (`b390dbf8`): the constant->float paths

Two more layers, both closing paths that still used a wide constant's low bits:

1. `T(constant)` was emitted as an `SSAConvert` even when it was a constant
   expression. `foldConstConvert` (`ssa_builder.mx`) now folds it at build time
   through `ConvertConstLocal`, which consults Exact - what go/types does.
2. `floatExact` read `ci.V` for an integer constant, ignoring Exact, so every
   mixed int/float operation used the low bits.

    float64(1<<129)       0 -> 6.80564733841877e+038
    float64(1<<70)        0 -> 1.18059162071741e+021
    1.0/(1<<70)           0 -> 8.470329472543003e-22
    ((1<<26-1)<<'A')>>64  0 -> 134217726
    (1<<70)>>64           0 -> 64

## Still open: RUNTIME materialisation of a wide constant

    x := 2.5;  x > 1<<129   ->  true   (want false)

The left side is a variable, so this is an `fcmp` against a MATERIALISED constant,
and materialising it goes through `intToFloat` (`ir_binop.mx`), which emits
`sitofp`/`uitofp` of the low bits. `emitConvert` already folds the constant case
(round 64); the binop path needs the same treatment - when an operand is an
integer constant carrying Exact and the other side is a float, fold it to a float
constant before the binop is created, rather than letting the backend widen the
low bits.

## Two inline-vs-named-constant discrepancies, both pre-existing

Measured on the UNPATCHED compiler, so not regressions, and both are the same
"inline expression takes a different path" shape as the constant-receiver bug
fixed in round 56:

    const X = 0x1p129;  float64(X)     -> 6.80564733841877e+038   correct
                        float64(0x1p129) -> 1.36112946768375e+039  TWICE the value
    const M = 1<<64-1;  ...            -> 18446744073709551615    correct
                        (1<<64-1).String() -> -1                  WRONG

Worth chasing together with the conversion layer above, since both smell like the
same emitter path.

## CLOSED: the over-64-bit constant limitation

Round 65 (`a984e0e6`) closed the last consumer. `intConstToFloat` (ir_binop.mx)
materialises a float operand from `ci.String()`, and that rendered `V` - the low
bits - so `1<<129` printed as "0" and

    x := 2.5;  x > 1<<129   ->  true   (want false)

for every positive x. `ConstInt.String` now returns the exact value when Exact is
set.

The full chain, all committed and suite-gated (204 PASS / 0 FAIL):

| layer | commit |
|---|---|
| the folder keeps wide integer constants exactly (`ConstInt.Exact`) | `5dfae83e` |
| a constant conversion is folded at build time (`foldConstConvert`) | `b390dbf8` |
| `floatExact` and `cvFloat` read Exact | `b390dbf8` |
| `ConstInt.String` renders Exact | `a984e0e6` |

Before -> after, one probe, one compiler otherwise:

    float64(1<<129)        0 -> 6.80564733841877e+038
    float64(1<<70)         0 -> 1.18059162071741e+021
    1.0/(1<<70)            0 -> 8.470329472543003e-22
    ((1<<26-1)<<'A')>>64   0 -> 134217726
    (1<<70)>>64            0 -> 64
    2.5 > 1<<129        true -> false

The four source-level workarounds this limitation forced (two 128-bit bitmaps in
`net/textproto` and `mime`, `Two129` in `math/j0.mx` and `j1.mx`, `Two302` in
`math/jn.mx`, `Tiny` in `lgamma.mx`) all remain - they are correct as written now
and there is no reason to churn them - but they were symptoms, and this is the
cause. Each is commented with why it is spelled that way.

## FIXED in round 66 (`25268ca8`): both remaining discrepancies

1. `cvFloat` returned `cf.V` for a ConstFloat and ignored `Exact`, so an inline
   hex float literal came out doubled:

       const X = 0x1p129; float64(X)       -> 6.80564733841877e+038   correct
                          float64(0x1p129) -> 1.36112946768375e+039   TWICE

2. The default type of an untyped int is int64 (`UntypedToTyped`), which cannot
   hold an unsigned-marked constant, so the builtin Stringer printed it signed:

       (1<<64-1).String()  ->  -1
       (1<<63).String()    ->  -9223372036854775808

   `resolveMethodCallWithRecv` now resolves the builtin method on uint64 when the
   receiver carries the Unsigned marker. Go rejects both expressions outright (the
   constant overflows int); uint64 is the type that holds them.

**Correction to the previous round's note:** this was NOT inline-only.
`const MaxU = 1<<64-1; MaxU.String()` was -1 as well - the earlier probe had
measured `float64(1<<64-1)`, a different expression. Measuring the *exact*
expression under discussion, not a nearby one, is the lesson; it has now cost
time in both directions.

# ============ UPDATE: round 67 — crypto/ecdsa fails on a NONDETERMINISTIC package-name lookup ============

## Survey first

Built each package the objective names, with the current compiler, reading the
first `*.mx:NN:` error rather than trusting the compiler's "compile errors"
header (which is printed even when there are none - the first survey said all
seven failed because of that):

    debug/gosym                        ok
    internal/coverage/encodecounter    ok
    internal/coverage/encodemeta       ok
    internal/coverage/cformat          ok
    internal/trace/.../tracev1         ok
    crypto/ed25519                     ok      <- the ed25519 item is closed
    crypto/x509                        ok
    crypto/ecdsa                       FAIL

So the objective's gosym, coverage, tracev1 and ed25519 items are done, and
`crypto/ecdsa` is the one real compile failure left among them. `net/http`
compiles (the survey timed out on it only because it takes ~25 minutes).

## The ecdsa failure is a nondeterminism bug, not a type bug

    ecdsa.mx:642: cannot use value of type *crypto/internal/fips140/ecdsa.PublicKey
                  as *ecdsa.PublicKey in return statement
    ecdsa.mx:650: ... *crypto/internal/fips140/ecdsa.PrivateKey as *ecdsa.PrivateKey ...

`src/crypto/ecdsa` is `package ecdsa` AND imports
`crypto/internal/fips140/ecdsa`, which is also named `ecdsa` - exactly as Go's own
source does. Inside that file a qualified `ecdsa.X` must therefore mean the
IMPORT, because Go's rule is that an import named X shadows the package's own
name within the file.

`resolveTypeExprWithSubst`'s SelectorExpr case (`ssa_builder.mx`) does not do
that. It calls `cctx.universe.LookupByName(pkgName.Value)`, and LookupByName
(`pkg/types/tc_universe.mx`) builds a global `ByName[p.Name] = p` map:

    for _, p := range u.Registry { u.ByName[p.Name] = p }

With two loaded packages named `ecdsa`, **the winner is decided by map iteration
order**. So this is the project's "incoherence" failure mode - the same source
can compile or not depending on the run - and it has a DEPRECATED comment on the
global bridge already ("Do not add new reads"), which this path is.

## The fix, ready to apply

Resolve the qualifier through the DECLARING package's imports before falling back
to the global map. `TCPackage` has `Imports []*TCPackage`, so the lookup is:

    for _, im := range declaring.Imports {
        if im.Name == pkgName.Value {
            if tn, ok := im.Scope.Lookup(ev.Sel.Value).(*TypeName); ok { return tn.Typ }
        }
    }

Two things to settle first:

1. `resolveTypeExprWithSubst` is package-level and receives only `pkgScope`. Either
   thread the declaring `*TCPackage` in (three call sites: resolveMethodSigWithSubst,
   resolveSignatureWithSubst, resolveTypeArgsExpr) or recover it from
   `cctx.genericPkgScopes`' key, which is the package path.
2. The `*syntax.Name` case just above consults `pkgScope` but accepts only a
   `*TypeName`; imports are not in that scope, so the SelectorExpr case is the only
   place that needs the import table.

Worth doing together: make `LookupByName` deterministic (iterate sorted keys) so
that any remaining caller is at least reproducible, and audit its other callers -
the comment says it is deprecated, and every remaining read is a latent collision.

## Round 68: import-first resolution attempted, and it did NOT fix ecdsa

Applied the plan above - `cctx.sigDeclPkgImports`, set from
`cctx.universe.Registry[pkgPath].Imports` at BOTH signature-resolution sites
(`resolveMethodSigWithSubst`'s caller and the generic-FUNCTION instantiation at
`ssa_builder.mx:9362`), read first by `resolveTypeExprWithSubst`'s SelectorExpr
case. Rebuilt, re-measured: **both ecdsa errors are byte-identical.** Reverted.

What that rules out, and what is left:

- Both `resolveSignatureWithSubst` call sites are now covered (there are only
  three, and the third is the recursive FuncType case), so `publicKeyToFIPS`'s
  result type is NOT resolved by either of them. It is resolved on a path not yet
  found.
- `TCPackage.Imports` IS populated (`ssa_builder.mx:597`
  `tcp.Imports = mxutil.Ensure(tcp.Imports, 1)`), so the field is usable - but the
  likely reason the patch was inert is that **`cctx.universe.Registry[pkgPath]`
  may not contain the package currently being compiled**: the Registry holds
  resolved dependencies, and `crypto/ecdsa` is the package under compilation. If
  `dp` was nil the context stayed empty and nothing changed, which matches the
  measurement exactly.
- Candidate sources for the declaring package's imports that do not depend on the
  Registry: `fb.fn.Pkg.Pkg.Imports` (the builder's own package) and the file's
  import decls, which the parser already has (`pb.segImports`,
  `extractImports` in main.mx).

**Next step is instrumentation, not another guess.** Print from
`resolveTypeExprWithSubst`'s SelectorExpr case (and from the `*syntax.Name` case)
when it resolves a selector whose qualifier is a known package name, with the
qualifier, the member, the resolved path, and the caller's package path. One
rebuild then names the path that actually resolves `ecdsa.PublicKey`, instead of
eliminating three candidates per round.

Do NOT start this by widening `LookupByName` again: three hypotheses have now been
built on where the resolution happens and all three were wrong.

## FIXED in round 69 (`4296622c`): crypto/ecdsa compiles

Instrumentation, not a fourth guess, found it in one build. The debug output was:

    DBG-TYRESOLVE qual=ecdsa member=PublicKey
    DBG-TYRESOLVE   LookupByName -> path=[ecdsa]
    DBG-REG ...                       <- no Registry key matching ecdsa at all

Two facts came out of that, and the second one is the bug:

1. **The package being compiled is NOT in `Universe.Registry`** - only its
   dependencies are. That is exactly why round 68's patch was inert: it set the
   import context from `Registry[pkgPath]`, which is nil for the package under
   compilation. The hypothesis recorded last round was right; the fix just used
   the wrong source for the imports.
2. **`LookupByName`'s `ByName` cache is keyed on the Registry's LENGTH.** A
   Registry write that REPLACES a package leaves the length unchanged, so the
   cache is never rebuilt - and `Synthetic` stubs exist precisely to be replaced
   by the real package ("Parallel workers must replace it with the real package
   loaded from the dependency's .mxh"). The stale entry pointed at a package whose
   `Path` was the bare name `ecdsa`, which is not a key in the Registry at all.

The import list now comes from the builder's own package
(`fb.fn.Pkg.Pkg.Imports`, populated for the package under compilation) and is
consulted before the global map, at both generic signature-resolution sites.

Verified: `crypto/ecdsa` compiles (only the expected `undefined symbol: main.main`
of a library), the compiler self-hosts, `crypto/x509` and `crypto/ed25519` still
compile, all six regression fixtures pass.

### Two follow-ups this exposed

1. **`LookupByName` is still wrong, and still nondeterministic in principle.**
   The length-keyed cache means any in-place replacement can leave a stale entry,
   and the `ByName` rebuild picks between same-named packages by map iteration
   order. Make the cache generation-keyed (bump a counter on every Registry
   write) or drop it, and audit the other callers - the global bridge is marked
   DEPRECATED with "Do not add new reads", and this path was one.
2. **Coverage gap.** No phase of the suite compiles `crypto/ecdsa`: phase 1 builds
   only the compiler's own packages (`token mxutil syntax types rewrite`). A whole
   class of stdlib codegen/type-check bugs is therefore invisible to the gate, and
   this one sat until a survey found it. Adding a stdlib compile sweep - even just
   the packages the objective names - would turn the next such find into a test
   failure instead of a discovery.

## FIXED in rounds 70-71: the coverage gap, and what it was hiding

The objective's list claimed `internal/coverage/{encodecounter,encodemeta,cformat}`
compiled. They did not, and neither did `text/tabwriter` or `net/http`. The claim
came from `moxie build -a ./_mxc_stage4` returning 0 - **the third time that
command was treated as proof a package compiled.** It builds the compiler; it says
nothing about any other package. Trust the artifact that runs the thing.

### Correction to the round-69 note above

The stale `ByName` entry's `Path` being the bare name `ecdsa` is not reproducible:
no Registry write in the tree keys a package by a bare name (`NewTCPackage` is
always called with the full path). The observable replacement is
`registerBuiltins`' `Synthetic` stub being overwritten **at the same key** by
`loadMxh` (`workerLoadDeps`, "existing != nil && !existing.Synthetic"). That is
enough - same key, same length, different object - and it is what the fix in
`rebuildByName`/`LookupByName` keys on now: the cached entry must be the object the
Registry currently holds, and the rebuild order is total (shorter `Path` first,
then lexicographic) instead of map iteration order.

### phase 1b: the stdlib sweep (`tests/phase1b_stdlib_sweep.sh`)

No phase compiled a stdlib package: phase 1 builds only `token mxutil syntax types
rewrite`, and the rest build fixtures and the compiler. One build now blank-imports
39 packages, which compiles all of them at once. A per-package build costs a full
closure each and bought nothing - `net/http` alone is ~20 minutes - so the sweep is
one build, ~20 minutes, added to `--full` only. Packages known broken are listed in
`SWEEP_KNOWN` and SKIP with their reason, so the broken set stays visible and
shrinking it is deliberate. **`SWEEP_KNOWN` is empty as of round 73** - every
package the sweep named now compiles; add an entry only for a defect already
recorded here, and move it back to `SWEEP` when it is fixed.

### What the sweep found, in the order it was fixed

1. **`push`/`__moxie_append` gave an element no type** (`e43bcae2`).
   `push(lines, nil)` in `text/tabwriter`'s `addLine` stored a `[]cell` element
   rendered as the pointer literal (`store {ptr, i64, i64} null`), which clang
   rejects. The element type is what decides the zero literal, and the builder is
   where the element gets it - the same rule `coerceSendValue` already applied to a
   send, so both sites call it now. Unblocked `text/tabwriter` and
   `internal/coverage/cformat`. Legacy compiles **and runs** the same fixture
   correctly, so this is a stage4-only defect; `nil_elem_push` pins it.
2. **A monomorphized clone's BODY was built without the declaring package's
   imports** (`d94ec154`). `monomorphizeAndCall` installed them only for the
   signature, and only when the declaring package was the one being compiled.
   `net/http` closes over `crypto/tls`, which instantiates
   `crypto/internal/fips140/ecdsa`'s `pad000`; in that clone `hmac` is not an
   import of the compiling package, so a method call on a `*hmac.HMAC` receiver
   resolved to nothing. `declPkgImports` falls back to `DirectImports` +
   `Registry` because a dependency has no live import list. No legacy mirror:
   legacy builds instantiated generics through x/tools `ssa`, whose types go/types
   already resolved with the declaring package's own imports.
3. **`binary.Write`'s (typ, data) pair passed backwards** (`082a5277`). Six sites
   in `internal/coverage` wrote `unsafe.TypeOf(*h), unsafe.Pointer(h)` where `h` is
   a header **value**: `*h` dereferences a non-pointer and `unsafe.Pointer(h)` turns
   the struct's bytes into a pointer. The decoder side of the same packages already
   had the correct `unsafe.TypeOf(hdr), unsafe.Pointer(&hdr)`.

### FIXED in round 73 (`1ed3d812`): internal/trace/internal/tracev1 compiles

clang crashed (exit 139) in `X86 DAG->DAG Instruction Selection` on
`(*Events).grow`, at `SelectionDAG::ReplaceAllUsesWith` - the same crash class as
the flate fix, with a much larger object. `grow` does
`push(l.buckets, &[eventsBucketSize]Event{})` with `eventsBucketSize = 524288`
(32 MiB). The IR materialized the whole array as a **value**:

      %t27 = alloca [524288 x Event]            ; 32 MiB stack alloca
      call void @llvm.memset.p0.i64(ptr %t27, i8 0, i64 %zi5, i1 false)
      %t28 = load [524288 x Event], ptr %t27    ; 32 MiB load
      %t29 = call ptr @runtime.alloc(i64 %ha6, ...)
      store [524288 x Event] %t28, ptr %t29     ; 32 MiB store

  The alloca+memset is already right (that is the flate fix); what is wrong is
  relocating the escaped aggregate by loading it as a value and storing it again.
  An aggregate copy is a **memcpy**.

  **Root cause located to the line.** `&x` goes to `buildOperandAddr`, which
  handles a name, an index, a selector and `&*p`, and then falls through to:

      xv := fb.buildExpr(e)              // whole array VALUE: alloca + load
      a := &SSAAlloc{Heap: true}
      fb.emitStore(a, xv)                // 32 MiB store

  `buildExpr` on `[524288]Event{}` is `buildArrayLit`, which ends in
  `return fb.emitLoad(alloc, typ)` (`ssa_builder.mx:6666`, and the same shape at
  `:6609`) - so the literal is loaded out of its alloca and then copied into the
  arena. The fix is in `buildOperandAddr`: handle `*syntax.CompositeLit` of
  **array or struct** type by building the literal *into* the allocation and
  returning its address (`emitAlloc` + `buildCompositeLitInto` +
  `markAddrEscaping`), never materializing the value - exactly what the
  pointer-to-struct branch of `buildCompositeLitInto` already does at
  `ssa_builder.mx:6919-6928` for the `[]*T{{...}}` shorthand. Slices and maps keep
  the existing fallback; their value is a header/pointer, not a 32 MiB aggregate.

  **No extra zeroing is needed, and adding it would be a mistake.** `emitAlloc`'s
  `a.Heap` branch emits no memset, but it does not need one: `ArenaAllocTD` ends in
  `memzero(ptr, size)` (`src/runtime/alloc.mx:2018`), so `runtime.alloc` already
  returns zeroed memory. The 32 MiB load+store was redundant work, not the thing
  carrying the zeros in - so the fix is the one change above, and a memset added
  to the heap branch would zero 32 MiB twice per call.

  Verified: tracev1 compiles and links; `addr_of_composite_lit` - deliberately
  bucket-sized, so a regression crashes the compiler rather than merely being
  wasteful - compiles, runs, and its IR has **zero** `load`/`store` of
  `[524288 x event]` where the old path had both; phase 0 is 79/79. Legacy
  compiles and runs the same fixture, so this is stage4-only: go/ssa lowers
  `&T{...}` to an alloc plus field stores and the address is that alloc.

### Round 73: context surveyed - it stays, minus `TODO`

Surveyed on the theory that `context` is irrelevant because spawn handles
cascading shutdown. The measurements:

- **No repository in the workspace imports it.** nostr, morly, musiquay, iskra,
  smesh, harness, oracle, zilch and transdb all have zero `"context"` imports
  (iskra's one hit was the string `"context"` in a `kindNames` table). Nothing an
  app builds creates a context or a deadline.
- **The stdlib does drive real work through it**, and none of it is
  cross-process coordination: `net/dial.mx` dial deadlines,
  `net/dnsclient_unix.mx` the DNS timeout, `net/lookup.mx` a lookup-group cancel,
  `net/http/transport.mx` request cancellation and the connect timeout,
  `net/http/server.mx` server and handler deadlines, `crypto/tls/tls.mx` the dial
  timeout/deadline, `crypto/tls/quic.mx` the handshake context, and
  `net/net.mx` defines `net.Error.Timeout()` **by identity** against
  `context.DeadlineExceeded()` and `context.Canceled`.
- It is not a Go shim: `breakerCtx` is Moxie's `chan struct{}` breaker with
  parent/child cascade, and `deadlineScheduler` arms **one** package-level
  `time.AfterFunc(d, deadlineFired)` for the earliest deadline instead of a
  goroutine per context.

So the package stays. What was removed is the one genuinely unreferenced piece:
`TODO()` and its `todo` singleton - its only caller was `src/runtime/trace`,
which is now de-contexted (its functions are no-ops, so a ctx parameter was
accepted and ignored). Both `context` and `runtime/trace` were then added to the
phase 1b sweep, so from round 73 they are compiled by the gate.

`context_breaker` is the first test the package has ever had: it pins
Done/Err/Cause identity, parent-to-child propagation, `AfterFunc` running on
cancel and `Stop` preventing it, `WithoutCancel`, an expired deadline cancelling
immediately, and `WithTimeout` actually firing through the scheduler.

**If context is ever removed** (not now), the recorded direction is a dedicated
timeout type inside `net` - a deadline plus a breaker - so dial, DNS and HTTP
keep per-operation timeouts. Dropping the timeouts instead would leave every
dial and lookup to block until it finishes or fails, which is a capability
change, not a cleanup.

### FIXED in round 74: a variadic parameter whose element type cannot be resolved

Found in round 73 while probing something else. Minimal reproducer:

    package main
    func f(args ...fmt.Stringer) {}
    func main() {
        var s fmt.Stringer = int32(1)
        f(s)
    }

stage4 emitted invalid IR instead of reporting anything:

    %t3 = insertvalue {ptr, i64, i64} %ms7, i64 %sx2, 2   ; the slice header
    %t4 = getelementptr inbounds i8, ptr %t3, i32 0       ; GEP through the VALUE
    call void @main.f(void %t3, ptr null)                 ; parameter type is void
    error: '%t3' defined with type '{ ptr, i64, i64 }' but expected 'ptr'

Legacy rejects the same source with `undefined: fmt` at both sites, so the source
is invalid. The mechanism is that every `DotsType` case resolved its element and
then **fell through to nil** when that failed, while the `variadic` flag is read
from the AST shape independently - so the parameter became `void` and callers
still built a slice for it.

**The wrong fix, and why it is recorded here.** The obvious reading is "the
resolver's global by-name fallback (`LookupByName`) let an unimported package
resolve; delete it". That was built and measured, and it is wrong: with the
fallback removed, a file that **does** `import "fmt"` got a nil element type
anyway (the scope handed to the checker's resolver does not always carry the
`PkgName` binding), the slice construction then computed a garbage element size,
and the compiler panicked with `alloc: implausible size 137438953856` while
compiling its own source. The same source compiled **and ran** correctly before
the change. The fallback is load-bearing.

The fix is at the three `DotsType` sites (`ir_typecheck.mx` `tcResolveNameInline`,
`ssa_builder.mx` `pkgResolveTypeAST` and `resolveTypeExprWithSubst`): an element
type that does not resolve is an error, reported by name via `typeExprName`
(`pkg.Sel`, or the bare name), never a silent nil. Verified: the reproducer now
reports `undefined: fmt.Stringer`; the same program **with** `import "fmt"`
compiles and runs (`f(s) == 1`, `f(a, b) == 2`); `restrict:reject_unimported_pkg_type`
pins the rejection; phase 0 is 81/81. The `LookupByName` fallback is left in place
with a comment saying it is load-bearing and why.

**Superseded in round 85 - the premise above was wrong.** The legitimate-import
failure was the nil variadic slice type, fixed at its root in round 84, not a
missing `PkgName` binding: the scope handed to `resolveTypeExprWithSubst` DOES
carry one binding per import (round 85 instrumented every miss and found a
non-nil scope each time), and the resolver simply never consulted it, only
`sigDeclPkgImports`. The fallback is gone: the scope route replaced it, and an
unimported qualifier is reported as undefined in both the non-variadic and the
variadic form. See the round-85 section at the end of this file.

Also in this round: `monomorphizeAndCall` now only replaces
`sigDeclPkgImports` when the declaring package could actually be identified.
Assigning nil for an unknown package cleared the caller's context, which is a
regression introduced in round 73.

## FIXED in round 77: syscall/js compiles for js/wasm, and three items re-measured clean

**`syscall/js` was never compiled by anything.** Phase 5 builds wasm fixtures,
which have a `main()` and never import it. `moxie build -target wasm ./src/syscall/js`
reported ten uses of **`any`** - a type Moxie does not have - plus a
`Value as Pointer` mismatch, and the resulting `rhs=nil` cascade.

The port, which is what CLAUDE.md's "stdlib still contains `any`" note asks for:

- `ValueOf(x any) Value` was a type switch over every Go kind, which cannot exist
  without an empty interface or reflection. It is replaced by one constructor per
  kind JavaScript can represent: `ValueOfNull`, `ValueOfBool`, `ValueOfInt`,
  `ValueOfUint`, `ValueOfFloat`, `ValueOfString`, `ValueOfFunc`, `ValueOfArray`,
  `ValueOfObject`. The conversion is now explicit at the call site, which is the
  trade the rest of the port already makes.
- `Set(p string, x Value)`, `SetIndex(i int32, x Value)` and
  `storeArgs(args []Value, ...)` take `Value` directly. `Call`/`Invoke`/`New` were
  already `...Value`, so `storeArgs` was being handed `[]Value` for a `[]any`
  parameter; that whole conversion layer is gone.
- `FuncOf(fn func(this Value, args []Value) Value)`, and the map behind it, no
  longer return `any`, so `handleEvent`'s `result := f(this, args)` has a type.
- The nineteen `runtime.KeepAlive(v)` calls are deleted. Moxie has no garbage
  collector - reclamation is per-frame arena release - so nothing can be
  collected early and `KeepAlive` is a no-op stub. They were also the
  `Value as Pointer` errors. `"runtime"` left the imports with them.

**`path/filepath` did not compile for wasm either**: `EvalSymlinks` calls
`evalSymlinks`, which lives in `symlink_unix.mx` under `//:build unix`. The file
contains one forwarding call into the portable `walkSymlinks`, so its tag is
Go's, not a real dependency - widened to `unix || (js && wasm)`, the same form
`path_unix.mx` already uses.

**`tests/phase1c_wasm_sweep.sh`** compiles the js/wasm-only packages
(`path/filepath`, `syscall/js`) and is wired into `--full` and `--phase 1c`.
`net/http` for wasm is listed there as known-broken: its next failure is
`internal/poll`'s js `accept`/`ReadFrom` (`rhs=nil`), which is its own chain.

**Three objective items re-measured and already done**, with probes:

- the `i64`-vs-`i8` narrow field store (`present[id/8] |= uint8(1) << (id % 8)`,
  the `src/internal/trace/base.mx:148` shape) - compiles and runs, single and
  looped;
- the `map[T]H[T]` literal substitution (`map[int32]box[int32]{1: {v: 7}}`) -
  compiles and runs, for `int32` and `string` keys;
- `src/internal/trace` itself compiles.

## VERIFIED in round 78: the embedded tzdata registration, and the relative-path correction

**Nothing imported `time/tzdata`, so its registration never ran in any build.**
That is the whole of the "unrun embedded tzdata registration" item: the mechanism
was already correct - `time/tzdata`'s `init()` calls
`registerLoadFromEmbeddedTZData`, which `//:linkname` binds to
`time.initRegisterLoadFromEmbeddedTZData`, which stores the loader in the
`loadFromEmbeddedTZData` package var that `loadLocation` consults after the
platform sources - but `grep -rln '"time/tzdata"'` over the tree returned
nothing. The embedded zip was dead weight in every binary.

`tzdata_embedded` now imports it and asserts three zones:

    tokyo=JST        (UTC+9, no DST)
    newyork=EST      (UTC-5)
    sydney=AEDT      (UTC+11, DST active on that date)

**Why this is a native-AND-wasm fixture, not a native one.** On a host with
`/usr/share/zoneinfo` the native run is answered by the system database, and
`_platformZoneSources()` is the same list for every target, so no native test can
reach the embedded path - there is no zone in the embedded blob that is missing
from the disk copy. In the browser there is no zoneinfo at all, so the wasm run is
answered ONLY by the embedded loader. Phase 5 compares the two outputs, so a
registration that stopped working would panic in the browser and fail. Verified by
running the module through `tests/wasm/run.py`: it printed the same four lines as
native, rc=0.

Two smaller facts this turned up:

- The wasm target does **not** set the `moxie.stringer` build tag, so builtin
  Stringers (`int32.String()`) do not exist there. A fixture that is compared
  native-against-wasm must not call them - the first draft of this fixture failed
  to build for wasm on `int32.String()` and prints the zone names instead.
- `typeExprName`-style diagnostics aside, the wasm build of this fixture links the
  whole embedded zip: the module is ~1.15 MB with the import.

## Round 80: net for js/wasm - the missing per-target files, and the real size of the job

`net/http` for wasm was the phase-1c skip. Chasing it turned up a family of
files that Go has and this port does not, then a much larger truth behind them.

**Ported (all verified by builds):**

- **`src/syscall/net_js.mx`** - Go's `$GOROOT/src/syscall/net_js.go`, which the
  port had never brought over. Its absence did not read as `undefined:
  syscall.Accept`: it read as `cannot resolve type of short var ns in accept`
  while compiling `internal/poll`, because `internal/poll`'s js build
  (`fd_unix.mx` and `sys_cloexec.mx` are tagged for js, exactly as Go tags them)
  calls `syscall.Accept`, and an unresolved call left the short var's type nil,
  which then became a `void` parameter. All sixteen functions return ENOSYS,
  except `SetSockoptInt` and `SetNonblock`, which return nil.
- **`src/syscall/net_fake.mx`** - Go's `net_fake.go` (`js || wasip1`), which
  declares the `Sockaddr` family. Go writes `type Sockaddr any` there, which is
  why it had been skipped: Moxie has no empty interface. It is now the same
  shape the unix build uses - one unexported `sockaddr()` method, so only the
  package can define a Sockaddr - and the js implementations report ENOSYS,
  which is the truth for a target with no sockets. `net/fd_posix.mx` needs
  `*syscall.SockaddrInet4` and `net/file_posix.mx` type-switches on it, so the
  types have to exist and implement the interface.
- **`src/net/splice_stub.mx`** - Go's `splice_stub.go` (`!linux`). The real one
  is `splice_linux.mx`, which the `_linux` filename suffix already restricts to
  linux, so every other target compiles the stub.
- **`src/net/mptcpsock_stub.mx`** - Go's `mptcpsock_stub.go` (`!linux`), same
  shape: `dialMPTCP`, `listenMPTCP` and `isUsingMultipathTCP`.
- **`src/net/rawdns_js.mx`** plus a `//:build unix` tag on `rawdns.mx`.
  `rawdns.mx` is Moxie-specific, not a Go file, so it inherited no build
  constraint and was being compiled for js, where it uses raw sockets and
  `syscall.SetsockoptTimeval` (which does not exist for js in Go either). Its
  only exported use is `rawLookupHost`, called from `lookup_unix.mx:69` and
  `dnsclient_unix.mx:556` as a fallback *after* the standard resolver returns
  nothing, so the js stub returns nil and those callers are unchanged.
- **Shadowing fixes in `net_fake.mx`**, which is js-only and therefore had never
  been compiled: `v, ok := v.(T)` re-declarations in `validateResolvedAddr`
  (three), the `ok`s in `fakeConnect`'s and `fakeListen`'s `wrapErr` closures,
  `err` in `readFromInet4`/`readFromInet6`'s callbacks, `port` in
  `assignFakeAddr`'s `nextPort`, and `if err := ...` in `recvfrom` and
  `fakeListen`. Moxie rejects any of these that shadows a name in an enclosing
  scope - including a named result.

**What that leaves, and it is the real story.** With the stubs in place the error
list stops being about missing files and becomes:

    18 errors in net's js-only and *_posix.mx files:
      iprawsock_posix, tcpsock_posix, unixsock_posix, ipsock_posix,
      udpsock_posix  - "not enough arguments in call"
      net_fake.mx:290,339,347,838 - "cannot use value of type []*net.netFD
                                     as bool in assignment"

So `net`'s js-only half has **never been compiled by anything**, and it is not a
few stubs away: it has call sites that do not match their callees' arity, and
comma-ok receives in `select` cases that the compiler does not accept. That is a
port of the js arm of `net`, not a fix, and it is recorded as such in phase 1c
rather than chased here. The five files above are still correct and still
necessary - they took the build from "missing syscall definitions" to "the js
code itself does not compile".

Native is unaffected and verified: `src/net` builds (only the expected library
link failure), and every new file is js-only or `!linux`, so Linux sees exactly
what it saw before apart from `rawdns.mx`'s new `unix` tag, which still includes
Linux.

## Round 81: net's js arm compiles - a misleading arity diagnostic, missing constants, and a comma-ok select bug

The round-80 conclusion was "18 arity and comma-ok errors remain, so net's js arm
needs a port". Half of that was wrong, and the way it was wrong is worth keeping.

**The diagnostic was hiding the cause.** `checkCallArgs` reported "not enough
arguments in call" without saying by how much or which way, and the numbers did
not add up: `sysSocket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)`
was reported as short of arguments while passing exactly the three the callee
declares. Printing the counts turned 18 mysteries into one fact:

    iprawsock_posix.mx:131: not enough arguments in call (want 8, have 7)
    ipsock_posix.mx:35:    not enough arguments in call (want 3, have 0)
    ipsock_posix.mx:57:    not enough arguments in call (want 4, have 2)

`have 0` for a three-argument call means the arguments were **dropped**, not
missed. An argument expression that fails to resolve is pushed as nil and
`buildArgs` skips it, so the call loses it and the arity check then blames the
caller. The undefined names were `syscall.AF_INET`, `SOCK_STREAM`,
`IPPROTO_TCP`, `SOCK_DGRAM`, `IPPROTO_IPV6`, `IPV6_V6ONLY` and `AF_UNIX`.

**Why they were undefined, and the fix.** Go's `$GOROOT/src/syscall/net_fake.go`
declares those constants at the top of the file, above the `Sockaddr` types -
and that is the file round 80 ported for the types only. Nothing else defines
them for js: `zerrors_linux_*` is linux-only by filename, and `syscall_moxie.mx`
covers the non-js wasm targets. `src/syscall/net_fake.mx` now carries the whole
file, constants included (explicit values, equal to what Go's `iota` blocks
produce). That removed 17 of the 18 errors at once.

**A real compiler bug was the last one.** `case incoming, ok = <-ln.incoming:`
in net's `fakeConnect` is the comma-ok pair in its ASSIGNMENT form, and
`buildSelect` recognised the pair only when the statement was a definition:

    pair := comm.Op == token.Def && len(names) == 2

With `=` it bound the received `[]*netFD` to both names, so `ok` got the
element's type: `cannot use value of type []*net.netFD as bool in assignment`.
Two names is the pair either way. `select_comma_ok_assign` pins it, and it is a
true before/after: the old compiler rejects it with
`cannot use value of type int32 as bool in assignment` on both the assignment
and the closed-channel case, the new one compiles and runs it.

**Where that leaves net/http for wasm.** `net` now compiles for wasm. `net/http`
gets all the way to its own SSA build and then the compiler stops on

    panic: runtime error: interface dispatch: no impl for String

which is the documented imported-generic-method-set item (`ir_iface.mx` skips
the generic `*Named`, and the mxh index records only non-generic method names),
not a js problem. phase 1c's reason now says exactly that.

## Round 82: the net/http-wasm "no impl for String" panic is an INSTANTIATION mismatch, not an empty list

The phase-1c skip for net/http was `interface dispatch: no impl for String`, which
the round-81 notes attributed to the documented imported-generic-method-set item
without evidence beyond the message. The message now says how many impls the
dispatch list holds, and it is **268, none matching** - and that list contains
every named type the compiler has (`*main.Named`, `*main.Basic`, `*main.SSAConst`,
`*time.Time`, ...), so an empty or un-scanned list is ruled out.

Two things had to be learned to get there:

- The panic is a **runtime** panic of the compiler binary itself, not a
  compile-time diagnostic. A core dump backtrace shows it raised by generated
  dispatch code inside `git.smesh.lol/moxie/pkg/types.(*Signature).String()`,
  called from `checkAssignable` <- `checkCallArgs` <- `buildPkgCall`: the compiler
  was building an argument-mismatch MESSAGE for a call in net/http, and rendering
  the signature dispatched `String()` on a `Type` that has no arm.
- The message text is baked into the generated dispatch arms when the compiler is
  BUILT, so the first build with the new message still printed the old one. Only a
  second-generation build (`/tmp/mxc-r82 build -a` of the same source) shows it.
  Worth remembering: diagnostics added to emitted code need two generations.

**Round 83 measured it, and the inference above was WRONG.** Printing the
unmatched descriptor from the default arm - which, as a diagnostic added to
generated code, again needed two bootstrap generations - gives

    interface dispatch: unmatched type descriptor 0x0

**NULL**. Not an instantiation whose hash differs: an interface value with a nil
type descriptor. So a `Type` in the 268-arm dispatch is `nil`, and the fix is not
about generic methods at all.

The chain, from the same core backtrace: the compiler was rendering an
argument-mismatch message for a call in net/http via
`pkg/types.(*Signature).String()`, and that renderer calls `.String()` on
`t.Results.At(0).Typ` / `v.Typ` / `sl.Elem` - which is **nil** whenever a
declaration's type failed to resolve (the checker records the error and leaves
the slot nil). Dispatching `.String()` on the nil interface boxes a NULL
descriptor and panics. The crash was in the ERROR PATH.

## FIXED in round 83: net/http compiles for wasm

Three changes, in the order the evidence forced them.

1. **A nil `Type` no longer crashes a diagnostic.** `pkg/types` renders types from
   `Array`/`Slice`/`Pointer`/`TCMap`/`Chan` and the tuple walkers
   (`writeParams`, `Signature.String`, `Struct.String`, the interface-method
   writer) through a new `typeString`, which prints `<unresolved>` for a nil type
   instead of dispatching `String()` on it. The nil case is expected - it is what
   an unresolved declaration leaves behind, and the error path is the one place
   that must never abort.
2. **That revealed the real errors, which were all in `net/http/roundtrip_js.mx`:**
   the js transport still used the OLD `any`-based `syscall/js` API that round 77
   replaced. Ported: `opt.Set("method", js.ValueOfString(req.Method))` and the
   other `Set`/`Call` arguments through `ValueOfString`/`ValueOfInt`/`ValueOfFunc`,
   the six `FuncOf` callbacks return `js.Value` with `js.ValueOfNull()` for the
   bare `return nil`, and the closure-local `body` is renamed `respBody` (it
   shadowed, which Moxie rejects).
3. **`net/http` now builds for `-target wasm`** (a 27 MB module) and phase 1c has
   no known-broken entries left: `net/http`, `path/filepath` and `syscall/js` all
   pass, 3/3 with zero skips.

The lesson worth keeping, on top of the two-generation rule: a fail-loud panic is
only as good as the path that reports it. "no impl for String" was loud, but it
was raised while trying to REPORT a plain type error, and it hid that error for
three rounds. The next dispatch-shaped panic should print the descriptor (now
trivial: the temporary print is removed, the message carries the impl count).

## FIXED in round 84: the compiler segfault on a qualified unresolved variadic type

**Root cause, finally.** `wrapVariadicArgs` takes the variadic parameter's type and
builds the slice that carries the trailing arguments:

    sliceType := sig.Params.At(sig.Params.Len() - 1).Typ
    var elemType Type
    if sl, ok := SafeUnderlying(sliceType).(*Slice); ok { elemType = sl.Elem }
    ...
    ms := &SSAMakeSlice{Len: lenVal, Cap: lenVal}
    ms.typ = sliceType

For this input the parameter's type IS nil - the DotsType sites report the
unresolved element by name and return nil, which is the round-74 fail-loud - and
the builder then went on to emit a **MakeSlice whose element type was nil**. The
allocation size and the slice header were computed from that garbage type: the
disassembly of the crash showed `slicePush` followed by
`memmove(dst = newdata + oldlen*24, src = &stackslot, 24)` with the header's
length holding a stack address, which is also where the `alloc: implausible size
1.4e14` panic in the same shape came from. One nil type, two symptoms, and the
intermittency was just whether the garbage size happened to be benign.

The fix is one refusal at the top of `wrapVariadicArgs`: if the variadic
parameter's type is nil, return the arguments unwrapped. The error is already
recorded, so the build fails exactly as before - but the compiler no longer
synthesizes a value of a type it could not resolve. This is the "refuse the exit
ramp" shape: the loud error already existed, and what was missing was declining to
continue.

Verified: 80 runs of the reproducer, **0 crashes**, all 80 reporting
`undefined: fmt.Stringer`; before the fix it was 3/40, 1/40 and 1/60 across three
batches. `reject_unimported_pkg_type` is restored with the QUALIFIED form, so the
rule is pinned by the case that used to crash, and `reject_undefined_variadic_type`
keeps the bare form.

The original notes are kept below, since the elimination work is what led here.

    package main
    func f(args ...fmt.Stringer) {}      // fmt is NOT imported
    func main() {
        var s fmt.Stringer = int32(1)
        f(s)
    }

Legacy rejects this with `undefined: fmt`. stage4 is *supposed* to fail loud the
same way (round 74 made the DotsType sites report an unresolved element type by
name instead of leaving a `void` parameter). Instead it crashes on roughly 2-7%
of runs:

    panic: runtime error at 0x000000000026e0c8: caught signal SIGSEGV

and `addr2line -e ./moxie 0x26e0c8` says **`runtime.memcpy`** - the same address in
every crash captured.

**Round 82 got a real backtrace** from a `systemd-coredump` core (the shell's
"core dumped" was not wasted):

    #0  runtime[memcpy]
    #1  runtime[memmove]
    #2  main[tcResolveNameInline]
    #3  main[tcResolveFuncInline]
    #4  main[typeCheckPkg]
    #5  main[CompileToIR]
    #6  main[compileSource]
    #7  main[compilePkgSource]
    #8  main[buildPkgsSerial]
    #9  main.main

So it is inside type checking, exactly where the unresolved variadic element is
being resolved - not in the emitter or the linker. The faulting call is a `push`
of a 24-byte element: the disassembly shows `call runtime.slicePush` followed by
`memmove(dst = newdata + oldlen*24, src = &stackslot, 24)`, and the value that
goes wrong is the old LENGTH taken from the stack slot holding the slice header.
A length that is a stack address (`0x7fff...`) is what produced the earlier
`alloc: implausible size 1.4e14` panic in the same shape - one garbage slice
header, two symptoms.

Two hypotheses were tested and are **eliminated**:

- the parallel worker path: `-j 1` still crashes;
- the arena switch in round 70's `rebuildByName`. Removing the switch entirely
  (allocating the by-name cache in the current arena instead) gave **1 crash in
  60 runs** against 3/40 and 1/40 with it, so the rate is unchanged and the
  switch is not the corruptor.

What is left, precisely: the faulting `push` takes a **24-byte element**, and
none of `tcResolveNameInline`'s own four pushes has a 24-byte element type
(`[]Type` and `string` are 16, `[]*TCField` is 8). So the faulting code is in an
INLINED callee, and the dwarf carries no `DW_TAG_inlined_subroutine` or variable
info for gdb to name it. Next step, in order: rebuild with a unique string pushed
next to each candidate push inside `tcResolveNameInline`'s callees (the
InterfaceType/StructType path and `typeExprName`'s callers are the candidates) to
see which one is on the stack at the fault; if that is unclear, `-work` plus the
crash PC will name the module, and the `alloc: implausible size` panic in the
same shape is the other reproducible face of the same corruption. Measured rates: 3/40 with the default job count, 1/40 with
`-j 1`, 0/30 under gdb (which re-times the process enough to hide it), 0/30 on
the compilers from rounds 73, 74 and 81 - so this is rare and possibly
timing/allocation dependent, and the earlier versions are not exonerated by those
small samples.

What narrows it down:

- The BARE form is clean: `func f(args ...Undefined) {}` reports
  `undefined: Undefined` and crashed 0/40. Only the QUALIFIED unresolved name
  (`fmt.Stringer` with no import) trips it, so the crash is on the selector path.
- It is not the parallel worker path: `-j 1` still crashed.

Because of it, `tests/restrictions/reject_undefined_variadic_type.mx` uses the
bare name to pin the round-74 rule, and the qualified form is **not covered by
the suite** - deliberately, and this note is the record of that. The next step is
a deterministic reproducer (it is 2-7%, so a loop plus `-work` and the crash PC
is the tool that has worked before) and then a backtrace; gdb's timing hides it,
so instrumenting the selector path in `tcResolveNameInline`/`DotsType` is the
more promising route.

Related coherence gap this exposed, same area: a **non-variadic** unimported
qualified type was still ACCEPTED (`func f(x fmt.Stringer)` with no import built
with rc=0), because the resolver's global by-name fallback answered it. The rule
"an unimported package qualifier is undefined" therefore held only in the
variadic path, which is why removing the fallback was attempted in round 74 and
reverted when it broke legitimate code. **FIXED in round 85** - see below.

### Still open

- **`unsafe.Pointer`/`unsafe.TypeOf` accept anything.** `unsafe.Pointer` is
  declared `__any->ptr`, so a struct value is accepted and silently becomes a
  pointer - which is why (3) produced invalid IR instead of a compile error. A
  compile-time "not a pointer" error would have caught all six sites. That is a
  restriction, so it is bilateral work (legacy + stage4, pinned by tests).
- Three objective items were re-measured and are **already done**: `regexp`,
  `text/template` and `os/user` all compile today. Only "compiles" is verified;
  nothing exercises them at run time yet.
- `crypto/ecdh`, `crypto/aes`, `crypto/cipher`, `crypto/sha256`, `crypto/hmac`,
  `encoding/json`, `encoding/xml`, `debug/gosym`, `math/big`, `time`,
  `compress/{flate,gzip,zlib}` and the rest of the sweep now compile and are in
  `SWEEP` (41 packages, tracev1/context/runtime/trace included), so a regression in any of them fails phase 1b.

### Environment note: the legacy compiler cannot be rebuilt here

`legacy/` links LLVM but the cgo config selects `-L/usr/lib/llvm-20/lib -lLLVM-20`
while `llvmpure_init.go` looks for `/usr/lib/llvm-22/lib/libLLVM-22.so`, and this
machine has LLVM 20 headers only (`/usr/include/llvm-22` does not exist; the
runtime `.so` at `/usr/lib/libLLVM-22.so` has no development files). So
`go build` in `legacy/` fails at link with an undefined `PassBuilder` symbol.
`tests/phase2_selfhost.sh` therefore runs the prebuilt `legacy/moxie` binary
(Oct 5) rather than rebuilding it, and any `legacy/` source change made in this
environment is **unverifiable** - state that plainly when it matters.

## FIXED in round 85: an unimported qualifier is undefined, in both forms

The coherence gap recorded at the end of round 84 is closed, and the global
by-name fallback that produced it is gone. Two things were wrong, and the first
was hiding the second.

**1. `resolveTypeExprWithSubst` never consulted the scope for a qualified name.**
Its `SelectorExpr` case answered `pkg.Sel` only from `cctx.sigDeclPkgImports`,
which is populated in the monomorphization paths (`monomorphizeAndCall`,
`tryGenericMethodCall`) and is EMPTY during a plain body build. But
`ScanGenericDecls` already rebuilds the declaring package's scope WITH a `PkgName`
binding per import (`ir_typecheck.mx:825-857`) and passes it in as `pkgScope` -
the resolver simply never looked at it.

Measured, not inferred: instrumenting the miss printed
`pkg=unsafe sel=Pointer n=0` four times and
`pkg=fs sel=PathError / FileMode / FileInfo / DirEntry` three times each while
building `runtime`, every one with a non-nil scope. `sigDeclPkgImports` was empty
(`n=0`) for all of them. The global short-name map answered them instead, which
is exactly why nothing looked wrong. Adding the scope route fixed the legitimate
case, and this is the route the type checker already used.

**2. The report could not be keyed on "the package is known by name".** An
unimported package is not in the Registry at all, so `LookupByName` answers nil,
no error is raised, and the parameter type comes back nil - the function still
type-checks and the CALLER silently miscompiles:

    package main
    func f(x fmt.Stringer) (s string) { return x.String() }   // fmt NOT imported
    func main() { s := f(int32(7)); if s != "7" { panic(s) } }

    build rc=0, then at run time: panic: got [] want [7]

The report now lives where a nil type cannot be tolerated: the parameter loop of
the function-type resolver (`tcResolveFuncInline`), the same shape as the
round-74/84 `DotsType` rule, and it names the type (`undefined: fmt.Stringer`).
A qualified name in a parameter type can only be a package; a bare name is left
alone, because that is also how an unbound type parameter looks there.

**It has to search through the type constructors, and that was measured too.** A
first version looked only at the top of the type expression, so it caught
`x fmt.Stringer` but not `p *fmt.Stringer` - which still built with rc=0 and
then died at run time with SIGABRT. `firstQualifiedTypeName` now walks pointer,
slice, array, map, channel, variadic, index and func types and reports the first
qualified name found; `reject_unimported_ptr_param_type` pins that form. The
slice form happened to be caught already, but for the wrong reason - the
composite literal argument was dropped and the arity check complained
(`not enough arguments in call (want 1, have 0)`), which is loud but misleading.

**A first attempt was wrong, and it is recorded rather than quietly dropped.**
Reporting `undefined: pkg` from the shared resolver whenever the qualifier was in
no scope made math/big, net/http (js/wasm) and `asn1_tag_crosspkg` fail with
`undefined: s`: a selector `x.Field` on a value the resolver's scope does not
carry reaches the same case. The two `LookupByName` reports are back to their
weak form ("the package is known AND the member is a type"), which is a pure
diagnostic, and the rule is enforced at the parameter site instead.

**Bilateral: no legacy change is needed.** Legacy already rejects
`fmt.Stringer` in a file that does not import fmt with `undefined: fmt`;
stage4's fallback WAS the divergence, and removing it brings stage4 to legacy,
which is the arbiter for behaviour. Wood law is satisfied by agreement here, not
by mirroring a feature.

### Verified

- `reject_unimported_param_type` and `reject_unimported_ptr_param_type` (new
  fixtures, the two sources above) are rejected with `undefined: fmt.Stringer`.
- `reject_unimported_pkg_type` (variadic, qualified) and
  `reject_undefined_variadic_type` (bare) are still rejected.
- The legitimate forms compile and RUN: the same function in a file that does
  `import "fmt"`, and a file importing fmt + unsafe with a `[]fmt.Stringer`
  parameter and an `unsafe.Pointer` result. `runtime`, the package the removed
  fallback was load-bearing for, builds, and phase 1b is 41/41.
- **FULL GATE at this commit: 224 PASS / 0 FAIL / 0 SKIP** (`bash tests/run.sh
  --full`), including `selfhost:fixpoint PASS (stage2 == stage3)`, phase 0 55/55,
  phase 0b 32/32 (the two new fixtures among them), phase 1b 41/41 and phase 1c
  3/3. That is the round's acceptance artifact - the same binary
  (`bcdd89bc874551d3f88c2c236aadfe6b8b1c1081fd25fea4bcd8a6feb4435842`) was
  rebuilt from the committed source and is byte-identical, so the gate covers
  exactly what is committed.
- The three `LookupByName` sites that remain are value-resolution contexts
  (`tcResolveFuncReturn`, `tcSelectorConst`) plus `ir_chan`'s `"io"` lookup; the
  two *type* resolvers no longer resolve anything by global short name.

- **FULL GATE at this commit: 232 PASS / 0 FAIL / 0 SKIP** (`bash tests/run.sh
  --full`), including `selfhost:fixpoint PASS (stage2 == stage3)` and the wasm
  runs of the two new guards. The gated binary was rebuilt from this tree and is
  byte-identical
  (`4a03cc1855d52d6f8da89feca3fcec6ddf2fa37eee4d91009eb58bef27bc3468`), so the
  gate covers exactly what is committed.

### Still open from this round

- `unsafe.Sizeof` reaches the checker's *type* resolver as a value selector; it
  is answered by no scope (it is compiler magic), and nothing reports it. That is
  a value path, not this one, but it is why the "member is a type" condition
  exists in both `LookupByName` reports.
- **The BARE non-variadic form leaks too, and it was measured**:
  `func f(x Undefined) (s string) { return x.String() }` in a file that defines
  nothing named `Undefined` builds with rc=0 and then panics at run time
  (`s != "7"` with an empty string). It is not reported yet because a bare name
  that does not resolve is ambiguous in a way a qualified one is not: a generic
  receiver's type parameter (`func (a Array[K,V]) Push(v V)`) and a
  forward-referenced type both have the same shape to `tcResolveNameInline` as a
  typo. That reading is a HYPOTHESIS, not a measurement - what is measured is the
  leak. Qualified names are free of the ambiguity (they come from the import
  list, not from declaration order or a type-param scope), which is why the rule
  could be made sound for them first. Closing the bare form means proving the
  scope is complete at the resolution site, or threading the enclosing
  `TParamList`/receiver params in, and only then reporting there.
- A nil *result* type is not covered either. The result list resolves through
  code that deliberately tolerates nil (there are empty branches for exactly that
  in `tcResolveFuncInline`), so the rule was not extended there; no miscompile
  from a nil result has been measured.

## FIXED in round 86: a bare undefined name is undefined too, in parameters and results

Round 85 removed the global by-name fallback and reported an unresolved
QUALIFIED parameter type. It also measured, and recorded as still open, that the
BARE form leaks: `func f(x Undefined)` built with rc=0 and then panicked at run
time. This round closes that, plus two more shapes found by looking for the same
thing rather than by guessing.

The cause is one nil type again: `tcResolveNameInline` returns nil for a name that
resolves to nothing, the Signature slot keeps the nil, and nothing downstream
objects. The qualified form could be reported from the shared resolver, because a
qualified name in a parameter type is always a package. A bare name there is
ambiguous - an unbound type parameter of a generic receiver resolves to nothing in
exactly the same way - so the rule is applied at the one site where the ambiguity
cannot exist: the non-generic top-level function, which `typeCheckPkg` reaches
only after `len(dd.TParamList) > 0` has `continue`d away.

**Measured, every one with rc=0 and a wrong or useless program:**

    func f(x Undefined) (s string) { return x.String() }        -> returned ""
    func f() (r Undefined) { return r }                         -> carried nothing
    func f(p Pair[Undefined, string]) (n int32)                 -> accepted
    func f(x *Undefined) / ([]Undefined) / (map[string]Undefined)

**The fix covers each:**

- `reportUndefinedSignatureTypes` walks the parameter AND result lists of a
  non-generic top-level function and reports the first name that does not
  resolve. Both lists, because a result leaks identically - the same nil, one
  slot further right. There is deliberately no "did the whole type resolve"
  gate: a generic instantiation whose type ARGUMENT does not resolve comes back
  as the un-instantiated base type, so the whole type is non-nil and
  `Pair[Undefined, string]` was accepted with rc=0.
- `firstUnresolvedTypeName` descends pointer, slice, array, map (key then
  value), channel, variadic, index and func type, the index LIST of a
  multi-argument instantiation, and inline struct/interface/func fields, and
  asks `tcResolveNameInline` about each Name/SelectorExpr leaf. So the diagnostic
  names the name that actually failed - `map[string]Undefined` says
  `undefined: Undefined`, not `string` - and the walker does not form a second
  opinion about what resolves.
- the append uses the established `mxutil.Ensure` + `push` pair. A qualified
  parameter can therefore carry the same message twice, once from
  `tcResolveFuncInline` and once from the walk; that is cosmetic, and the
  alternative was the dedupe read described below.

**Guards, so the rule cannot turn into a false positive.** A type declared later
in the file must not be reported, in every constructor and in both parameter and
result position (`forward_type_constructors`), and a generic receiver's type
parameter must not be reported (`generic_receiver_param_type`). The second one is
also why the rule does not cover methods or func literals at all: their
signatures are resolved with a scope that may not carry a generic receiver's
type params, where `V` is indistinguishable from a typo.

**Fixtures:** `reject_undefined_param_type`, `reject_undefined_elem_param_type`,
`reject_undefined_type_arg_param` and `reject_undefined_result_type` for the
rejections, plus the two `should_compile` guards above.

**The intermittent crash, root-caused, and the stdlib bug the rule caught.** The
first version of this report deduplicated by scanning `cctx.compileErrors`, and
it died once with SIGSEGV inside that scan. The same investigation found the
sweep failing on `math/bits` with an EMPTY error message, and a second run
aborting with `alloc: implausible size` - all three are one bug:

    #0 main[pushCompileError] 0x817daf   (mov 0x10(%rsi),%rdi)
    #1 main[reportUndefinedFieldTypes]
    #2 main[reportUndefinedSignatureTypes]
    #3 main[typeCheckPkg] -> main[CompileToIR] -> main.main

`TypeCheckOnly` - the pre-pass that typechecks a package to build its mxh - never
cleared `cctx.compileErrors`, and it FREES its arena when it returns. So its
entries, and the slice backing array holding them, were left pointing into
released memory. The next `CompileToIR` saw `len(compileErrors) > 0`, read the
freed bytes, and reported them against an unrelated package: that is the empty
`; ` message blamed on `math/bits`, the `alloc: implausible size` abort when the
reused memory fed the SSA builder, and the SIGSEGV when the dedupe scan read a
slice header from an address whose arena was gone.

Fixed by giving each typecheck pass its own errors: `cctx.compileErrors` is reset
at the entry of `TypeCheckOnly` and of `CompileToIR`, and the gate drops the
backing array (`= nil`) instead of truncating it, so nothing can outlive the
arena it was allocated in. The dedupe helper was removed with it - the report
does not need to read the list.

What the rule caught on its first full run is the point of having it:

    src/crypto/tls/prf.mx:182
    -func newFinishedHash(version uint16, cs *cs) (rfinishedHash finishedHash) {
    +func newFinishedHash(version uint16, cs *cipherSuite) (rfinishedHash finishedHash) {

`cs` is declared nowhere in the tree, and the function below it takes
`suite *cipherSuite`. So the parameter type of a crypto/tls function was
UNDEFINED, the checker left it nil, and nothing anywhere objected - the package
compiled and the arc was silently typed by a nil. Verified end to end: with the
typo reintroduced, crypto/tls now fails with `undefined: cs`; with it fixed, the
41-package sweep compiles and RUNS.

### Verified

- `reject_undefined_param_type` (bare), `reject_undefined_elem_param_type`
  (`*Undefined`, `[]Undefined`, `map[string]Undefined`), and
  `reject_undefined_type_arg_param` (`Pair[Undefined, string]`) and
  `reject_undefined_result_type` are each rejected with the name that does not
  resolve - `map[string]Undefined` reports `Undefined`, not `string`, because
  the walk asks the resolver about each name instead of taking the first.
- The two guards compile AND RUN: `forward_type_constructors` (every constructor,
  parameter and result position, with the type declared later in the file) and
  `generic_receiver_param_type` (a method's parameter typed by its generic
  receiver's type parameter, which must NOT be reported).
- Phase 0 93/93, phase 0b included; phase 1b PASS - all 41 stdlib packages
  compile and the sweep binary runs; phase 1c 3/3.
- The rule catches a real stdlib defect and attributes it correctly: with
  `cs *cs` restored in `src/crypto/tls/prf.mx`, the build fails with
  `undefined: cs` / `compile error for crypto/tls` (before this round it
  compiled, with a nil parameter type nobody looked at); with the fix, the
  whole sweep is clean.

**Still open from this round:** a method or func literal with a bare undefined
name (the METHOD half is fixed in round 90 and the func literal in round 93),
and a nil type inside an inline struct type. The TYPE DECLARATION half is fixed
in round 94, which also found and fixed a compiler crash in the local form.

## FIXED in round 90: a method's signature is checked like a function's

Round 86 applied the signature rule to non-generic top-level functions and listed
methods and func literals as still open. Both were measured this round, with the
compiler built at round 86:

    func (s *S) m(x Undefined) (r string) { return x.String() }
        build rc=0, then SIGABRT at run time
    f := func(x Undefined) (r string) { return x.String() }
        build rc=0, then SIGABRT at run time
    func (b *Box[K,V]) Set(k K, val V)     (legitimate)
        build rc=0, runs

The METHOD case is closed with the same helper, called from the method loop:
a method on a NON-generic receiver cannot declare type parameters - Go and Moxie
both refuse method-level type params - so every name in its signature must
resolve, exactly as for a top-level function. A generic receiver is excluded for
the same reason the top-level rule excludes generic functions: its type params
are not in the scope where the signature is resolved, and `V` there is
indistinguishable from a typo.

Measurement discipline as in round 86:

- `reject_undefined_method_param_type` pins the rejection (`undefined: Undefined`).
- `forward_type_constructors` grew two methods - one taking the imported
  `fmt.Stringer`, one taking a type declared later in the file - so the method
  rule is exercised on the two shapes that must NOT be reported.
- `generic_receiver_param_type` still compiles and runs, which is the guard that
  the generic-receiver exclusion stays.
- The 41-package stdlib sweep is the real false-positive test: it contains
  thousands of methods, including ones whose signatures name imported and
  generic types.

**Still open: the func LITERAL, measured above.** Its signature is resolved where
the enclosing function's genericity is not known, and a literal inside a generic
body may legitimately name the enclosing type parameters - which look exactly
like an undefined name. Closing it means carrying the enclosing `TParamList` (or
the absence of one) down to the literal's resolution site, and only then
applying the rule. The site with that information is `buildFuncLit`
(`ssa_builder.mx:7103`), which resolves the literal's signature and has `fb` in
hand: `fb.typeSubst == nil` is exactly "the enclosing body is not a generic
instantiation", and the literal inherits it (`ab.typeSubst = fb.typeSubst`). The
package scope passed there already resolves qualified and package-level names,
which is why the qualified half of this rule is safe at that site today.

**Also still open, and measured this round:** `type S struct { n Undefined }`
builds with rc=0. Unlike the signature cases it did NOT visibly misbehave -
`s.n.String()` returned the expected value in the probe - so it is recorded as a
nil field type that is tolerated end to end rather than as a confirmed
miscompile. `tcResolveNameInline` returns a `TCStruct` for an inline struct type
regardless of whether its fields resolved, so the walker used by the signature
rule never gets to explain it.

### Verified

- `reject_undefined_method_param_type` is rejected with `undefined: Undefined`.
  Before this round the same method built rc=0 and aborted at run time.
- `forward_type_constructors` (now including a method taking `fmt.Stringer` and
  one taking a type declared later in the file) and `generic_receiver_param_type`
  compile AND run.
- Phase 0 94/94 (phase 0b included), phase 1b PASS on all 41 stdlib packages,
  phase 1c 3/3.
- **FULL GATE: 233 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`. The gated binary was rebuilt from
  this tree and is byte-identical
  (`0c9f7c0b402e791fc8d27b7e192be330b12d7550d0d10cf85818bd391dc9f12b`).

## FIXED in round 93: func literals, and local types in a literal's signature

Round 90 closed the method form and left the func literal open with its site
identified. Measured with the round-90 compiler:

    f := func(x Undefined) (r string) { return x.String() }
        build rc=0, then SIGABRT at run time

The rule now runs in `buildFuncLit` under the soundness precondition that the
enclosing body is not a generic instantiation (`fb.typeSubst == nil`): a literal
there cannot name a type parameter, so every name in its signature must resolve.
A literal inside a monomorphized body may legitimately name the enclosing type
parameters, which are in no scope at that point and look exactly like a typo.

The first attempt was refused by the stdlib sweep, and the refusal was correct:
`internal/trace/internal/tracev1` declares `type gdesc` and `type pdesc` INSIDE
a function and passes both to a literal, and those names live in
`fb.localTypes`, not in the package scope the rule resolved against. Two things
followed.

**1. A real silent miscompile fell out of it.** A func literal whose parameter
type is declared locally in the enclosing body did not work at all:

    func localTypeLit() (r string) {
        type inner struct { n string }
        f := func(x inner) (out string) { return x.n }
        return f(inner{n: "ok"})
    }
    -> printed "" (empty), rc=0

`fb.resolveTypeAST` consults `fb.localTypes` for expression types, but
`buildFuncLit` resolved the literal's SIGNATURE straight against the package
scope, and the literal's own builder did not inherit `localTypes` at all - unlike
`typeSubst`, `srcScope` and `fileScope`, which it did inherit. So the parameter
was typed nil and the literal returned an empty string with no diagnostic
anywhere. Both halves are fixed: the signature is resolved against the package
scope with the visible local type names in a child scope, and the literal's
builder inherits `fb.localTypes` like its other context. After the fix the probe
prints `got: ok`.

**2. The rule's precondition is then just "not generic".** With local types
resolving correctly at that site, the temporary `len(fb.localTypes) == 0`
exclusion was removed: a bare name there now either resolves (local, imported or
package-level) or is a genuine typo.

Verified: `generic_funclit_param_type` (a literal inside a generic body naming
`T`) and `local_type_funclit_param_type` (a literal taking a locally declared
type) compile AND run; `reject_undefined_funclit_param_type` is rejected with
`undefined: Undefined`; `forward_type_constructors` gained literals taking
`fmt.Stringer` and a forward-referenced type; the `tracev1` package compiles and
runs.

**Found on the way, and NOT this round's bug: the wasm target traps on a func
literal inside a generic body.** `wrap[T](...)` with `g := func(x T) (y T)`
prints correctly natively and traps in the browser (`app.wasm._start`,
`wasm-function[152]`) on wasm. `generic_funclit_param_type` therefore carries
`//:wasmtest skip` with the reason in the fixture, and the wasm-safe half of the
guard was split out into `local_type_funclit_param_type`, which does run on wasm.

Measured with the COMMITTED round-90 compiler as well - the same wasm function
index and the same offset - so the trap predates this round and is not caused by
the func-literal rule; it is a separate wasm codegen defect and is recorded here
as open. The rule exclusion itself is still verified natively, which is where the
compiler's own diagnostics are checked.

### Verified

- `reject_undefined_funclit_param_type` is rejected with `undefined: Undefined`;
  the same literal built rc=0 and aborted at run time before this round.
- `local_type_funclit_param_type` compiles and RUNS (native and wasm); it
  printed an empty string instead of `ok` before the fix.
- `generic_funclit_param_type` compiles and runs natively, with
  `//:wasmtest skip` and the pre-existing wasm trap recorded above.
- `forward_type_constructors` (now with literals taking `fmt.Stringer` and a
  forward-referenced type) compiles and runs; the `tracev1` package compiles.
- **FULL GATE: 237 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)` and the wasm phase. The gated
  binary was rebuilt from this tree and is byte-identical
  (`d47ab865505bd4d29e35b6bab2e0c6bb918f2f36899764b0dade8e48058631fa`).

## FIXED in round 94: field types are checked, and a local type with a broken field no longer crashes the compiler

Round 93's still-open list still carried "a nil type inside an inline struct
type". Measured natively with the round-93 compiler:

    type S struct { n Undefined }
    s := S{}
    println("n=[" | s.n.String() | "]")
    -> build rc=0, printed NOTHING

and the same for an unimported `n fmt.Stringer`. A struct type RESOLVES even
when a field type does not, so the declaration was accepted, the field was typed
nil, and every use of it compiled away without a word. The signature rule cannot
see this - it walks parameter and result types - so the declaration is now
checked directly: `typeCheckPkg` reports the first name in a type declaration
that resolves to nothing, with the generic declaration's type params in scope so
`T` in `type S[T comparable] struct { v T }` still resolves. Package-level
forward references are order-independent and are not reported
(`type Outer struct { inner Inner }` before `Inner` is fine) - that is pinned by
a new guard case in `forward_type_constructors`.

**Chasing it found a compiler CRASH, in the function-local form.** A LOCAL type
declaration is resolved by the SSA builder, not by `typeCheckPkg`, and it was
never checked at all:

    func f() (r string) {
        type inner struct { n Undefined }
        x := inner{}
        return x.n.String()
    }
    -> SIGSEGV in the SSA build (litFullyCovers <- buildCompositeLit)

Confirmed with the committed round-93 compiler at the same address, so the crash
is pre-existing, not a regression. It is fixed the same way: the local
declaration is checked (with the local types visible so far PLUS the type's own
name in scope, so an earlier local type and a recursive field are not called
undefined), and a type whose field did not resolve is NOT registered. That is
the refusal shape from CLAUDE.md's guard rules: the error is recorded, and the
body must not build a value of a type that has a nil field. The probe now fails
with `undefined: Undefined` and no signal.

Two more local-declaration behaviours were measured while checking the fix, both
pre-existing crashes that the new check turns into loud errors rather than
silent ones:

- a LOCAL type referenced before its declaration (`type outer struct { b inner }`
  then `type inner struct{...}`) crashed with SIGSEGV on the committed compiler
  and now fails with `undefined: inner`. Go scoping says a local type must be
  declared before use, so the error is correct - but note that Moxie previously
  died rather than rejecting it.
- a RECURSIVE local type (`type node struct { n string; next *node }`) crashed
  with the same signature as the undefined-field crash, because the
  self-reference resolved to nil before the type's own name existed.
  **FIXED in round 95** - see below.

**The legacy compiler could not build the first version of this check, and that
is its own finding.** `bad := ""` - a variable initialized to an EMPTY STRING
LITERAL - made legacy emit an unnamed used global:

    error: unknown special variable with appending linkage: @0

which fails LLVM verification, so `selfhost:stage1` failed while stage4 built the
same source cleanly. Bisected by file and then by construct: the `ir_typecheck`
half alone builds under legacy; the `ssa_builder` half fails ONLY with that
empty-string variable, and an equivalent `bad := false` plus
`len(badName) > 0` builds clean. The wording was therefore rewritten (identical
behaviour), but the divergence is legacy's and is recorded here: **legacy
mis-handles a variable initialized to an empty string literal in this shape**,
producing an appending-linkage global whose name LLVM renumbers to `0`.
`legacy/compiler/llvmutil/llvm.go:MergeAppendingGlobals` only folds strays whose
base name it has already SEEN, and a global with no base name has nothing to fold
into. It cannot be fixed from here because legacy cannot be built in this
environment (see the note at the end of this file); stage4 does not share the
defect.

Verified: `reject_undefined_field_type` (bare), `reject_unimported_field_type`
(qualified) and `reject_undefined_local_field_type` are rejected with the name
that does not resolve and with NO signal; the local control (a valid local type,
an earlier local type referenced by a later one) compiles and runs;
`forward_type_constructors` (now with a forward-referenced struct field) and the
generic type declarations in the existing guards compile and run;
`type S[T comparable] struct { v T }` still resolves `T`; and
`legacy/moxie build -a ./_mxc_stage4` succeeds on this source.

### Verified

- `reject_undefined_field_type` (bare field), `reject_unimported_field_type`
  (qualified field) and `reject_undefined_local_field_type` (function-local
  type declaration) are each rejected with the name that does not resolve, and
  the local one no longer produces a signal: the crash is gone.
- `forward_type_constructors` (now with a forward-referenced package-level
  struct field) and the generic type guards compile AND run; a valid local type
  and an earlier-local-type reference compile and run.
- `legacy/moxie build -a ./_mxc_stage4` succeeds on this source, which is what
  `selfhost:stage1` tests.
- **FULL GATE: 240 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`, phase 1b (41 packages) and the
  wasm phase. The gated binary was rebuilt from this tree and is byte-identical
  (`4b99d6d760e95bdf6e552b5e113bae0488f2b541ca54fb430f3ac5007ff0b7ca`).

## FIXED in round 95: a recursive local type no longer crashes the compiler

Round 94 recorded the last piece of the local-type work: a recursive LOCAL type
declaration still crashed, because its body was resolved in one pass and the
self-referential field was typed before the type's own name existed - so `next`
came out nil.

    func chain() (r string) {
        type node struct { n string; next *node }
        b := node{n: "b"}
        a := node{n: "a", next: &b}
        return a.next.n
    }
    -> SIGSEGV before this round (same address as the undefined-field crash)

The declaration now registers its NAME first and attaches the underlying type
afterwards, which is exactly what the package-level path does with `NewNamed`:

    placeholder := NewNamed(NewTypeName(pkg, name, nil), nil)
    localTypes[name] = placeholder
    resolved := resolveTypeAST(dd.Type)
    if resolved != nil { placeholder.Under = resolved }

**One correction found by measurement.** The first version kept the plain
resolved struct as the registered type - "identical storage semantics, only
self-references change" - and that was wrong: `node{n: "a", next: &b}` then
failed with

    cannot use value of type *struct{n string; next *main.node} as *main.node

because the field pointed at the Named while the composite literal's type was the
plain struct: TWO representations of one local type. The registered type IS the
Named now, so every use and the recursive field are the same object - the shape a
package-level type has. A declaration whose field did not resolve is still
dropped outright (round 94's refusal).

Verified: the recursive probe compiles and RUNS (native and wasm);
`recursive_local_type` pins it, including assignment between two local-type
values and a field write; the local controls (a plain local type, an earlier
local type referenced by a later one), the `tracev1` package with its
`gdesc`/`pdesc` locals, and the undefined-field rejection all still behave; phase
1b passes on all 41 stdlib packages, so making local types Named did not disturb
how they are used elsewhere.

**Still open:** a local type referenced BEFORE its declaration is a loud
`undefined: <name>` error (it crashed before round 94). Go scopes a local type
from its declaration onward, so the error is correct, but Moxie previously died
rather than rejecting it - if order-independent local types are wanted, the
pre-registration pass belongs above the statement loop rather than inside the
declaration's own case.

### Verified

- `recursive_local_type` compiles AND runs, natively and on wasm (the wasm
  validator compares its output with the native run).
- The undefined-field local rejection still reports `undefined: Undefined` with
  NO signal; the local controls (plain local type, earlier-local reference,
  assignment and field write) compile and run; the `tracev1` package compiles.
- **FULL GATE: 242 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`, phase 1b (41 packages) and the
  wasm phase. The gated binary was rebuilt from this tree and is byte-identical
  (`3655d5c10531cbad78dd7cbd95796ea2c43a1c829b4ea9c916c7f1f9d5207db6`).

## FIXED in round 96: `url.Values` dissolved - alias plus free functions

The named-public-API list in `src/net/**` had five types left after `IPMask`,
`HardwareAddr`, `ProtocolError` and `Dir` were dissolved. This round takes the
smallest: `url.Values`, six methods, a named MAP type (`map[string][][]byte`) and
therefore not allowed in Moxie.

The route is the one the tree already uses for `IPMask`/`HardwareAddr` - an alias
plus free functions:

    -type Values map[string][][]byte
    +type Values = map[string][][]byte

    (v Values) Get(key string) (rs string)   -> ValuesGet(v Values, key string) (rs string)
    (v Values) Set(key, value string)        -> ValuesSet(v Values, key, value string)
    (v Values) Add(key, value string)        -> ValuesAdd(v Values, key, value string)
    (v Values) Del(key string)               -> ValuesDel(v Values, key string)
    (v Values) Has(key string) (rok bool)    -> ValuesHas(v Values, key string) (rok bool)
    (v Values) Encode() (rs string)          -> ValuesEncode(v Values) (rs string)

The NAME survives, which is the point of the route: `url.Values` still spells the
type, `url.Values{}` still builds one, and `http.Request.Form`/`PostForm` are
unchanged. What callers write changes from `v.Get(k)` to `url.ValuesGet(v, k)`.

Call sites: exactly two `.Encode()` uses - `net/http/client.mx`
(`Client.PostForm`) and `internal/trace/traceviewer/http.mx` - and nothing else
in the tree, which the compiler confirmed by building `net/http` clean after
them. **That a map can be written through a free function's parameter is what
makes this route viable for a map type at all**: `ValuesSet`/`ValuesAdd`/
`ValuesDel` do exactly that, and the new `url_values_api` fixture exercises
construction, both setters, `Get` returning the FIRST value, `Has`, `Del`,
`Encode`'s sorted output and a `ParseQuery` round trip.

Found next door, NOT caused here: `internal/trace/traceviewer/format` still fails
with `undefined: any` (an `Arg any` field) - the recorded any-removal debt. That
package is in no test phase; its call site was updated for correctness and cannot
be compiled until the debt is paid.

**Remaining named types to dissolve:** `net.IP` (18 methods), `net.Buffers`,
`net/http.Header` (13), `net/textproto.MIMEHeader` (5).

### Verified

- `url_values_api` compiles AND runs, natively and on wasm: construction, both
  setters, `Get` returning the first value, `Has`, `Del`, `Encode`'s sorted
  output (`a=1&a=3&b=2`) and a `ParseQuery` round trip.
- `net/http` compiles with the alias (the whole `Client.PostForm` path), phase 1b
  passes on all 41 stdlib packages, and phase 1c compiles the wasm sweep.
- **FULL GATE: 244 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`. This round changed stdlib source
  only - no compiler change - so the gate's own compilation of `net/url`,
  `net/http` and the fixture is the artifact under test.

## FIXED in round 97: `net.Buffers` dissolved into a struct - the mutating-method shape

Round 96 took `url.Values` with the alias-plus-free-functions route. `net.Buffers`
is the case where that route CANNOT work, and the reason is a language rule, not
taste:

    type Buffers [][]byte          // a named slice type: not allowed

`Read` and `WriteTo` CONSUME the runs they write, and free functions cannot
express that: a slice assigned through a parameter does not write back (there is
no aliasing, and a subslice is a copy). So Buffers became a struct that KEEPS its
pointer-receiver methods:

    type Buffers struct {
        Runs [][]byte
    }

What changed: the declaration; the field accesses inside the three methods
(`*v` -> `v.Runs`, `(*v)[0] = ...` -> `v.Runs[0] = ...`, `*v = (*v)[1:]` ->
`v.Runs = v.Runs[1:]`); and the writev cast in `writev_unix.mx` -
`fd.pfd.Writev((*[][]byte)(v))` -> `fd.pfd.Writev(&v.Runs)`, which no longer
needs an unsafe cast (the `unsafe` import stays for `unsafe.Pointer(fd)`).

What did NOT change: every call site - `v.WriteTo(w)`, `v.Read(p)`,
`w.writeBuffers(v)` and the `buffersWriter` interface all take `*Buffers` as
before. Construction becomes `Buffers{Runs: ...}`; no in-tree construction sites
existed, and the only other user of the type is `writev_unix.mx`.

`net_buffers_api` pins the semantics that forced the route: `WriteTo` drains the
runs it wrote, and `Read` drains whole runs plus the consumed part of a partial
one, leaving the remainder in `Runs[0]`.

### Verified

- `net_buffers_api` compiles AND runs, natively and on wasm: `WriteTo` drains the
  runs it wrote (6 bytes out, `len(Runs) == 0` after), and `Read` drains whole
  runs plus the consumed part of a partial one (3 bytes read leaves `Runs[0]` as
  `d`).
- `net` compiles alone and `net/http` (whose writev path is the only in-tree user
  besides the type itself) compiles in phase 1b; phase 1c compiles the wasm
  sweep.
- **FULL GATE: 246 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`. Stdlib source only this round - no
  compiler change - so the gate's compilation of `net` plus the fixture is the
  artifact under test.

## FIXED in round 98: `textproto.MIMEHeader` dissolved, and `http.Header`'s delegations inlined

`MIMEHeader` is a named MAP type (`map[string][][]byte`), so it takes the alias
route proven by `url.Values`:

    -type MIMEHeader map[string][][]byte
    +type MIMEHeader = map[string][][]byte

    (h MIMEHeader) Add(key, value []byte)     -> MIMEHeaderAdd(h, key, value)
    (h MIMEHeader) Set(key, value []byte)     -> MIMEHeaderSet(h, key, value)
    (h MIMEHeader) Get(key []byte) (rb []byte) -> MIMEHeaderGet(h, key)
    (h MIMEHeader) Values(key []byte)          -> MIMEHeaderValues(h, key)
    (h MIMEHeader) Del(key []byte)             -> MIMEHeaderDel(h, key)

The route was cheap here because almost nothing called the methods: the type is
used as a value (signatures, `Part.Header`, conversions, `MIMEHeader{...}`
literals) and the maps are read and written directly. The ONE caller shape was
`net/http.Header`'s five delegating methods, which did
`textproto.MIMEHeader(h).Add(key, value)` and friends - so those five bodies are
now direct map operations (canonicalize, then index/push/delete), which also
removes the conversion from every Header method call.

Six call sites in `mime/multipart` used the methods directly and moved to the
free functions: `p.Header.Get("Content-Disposition")` and
`bp.Header.Get(cte)`/`bp.Header.Del(cte)` in `multipart.mx`, and three
`h.Set(...)` calls in `writer.mx` (`CreateFormFile`, `CreateFormField`).

`mime_header_api` pins what makes this type more than a map: every operation
CANONICALIZES the key, so a key written as `x-lower` is found as `X-LOWER` and
one written as `Content-Type` is found as `content-type`; `Get` returns the FIRST
of several values and `Del` removes them all.

### Verified

- `mime_header_api` compiles AND runs, natively and on wasm: canonicalization
  both ways (`x-lower` found as `X-LOWER`, `Content-Type` found as
  `content-type`), `Get` returning the first of several values, `Values` keeping
  every value in order, `Del` removing them all.
- `mime_header_parse` (the pre-existing fixture) was updated to the free
  functions and passes again, natively and on wasm - it was the one caller of
  the removed methods inside the suite.
- `net/http`, `mime/multipart` and `net/textproto` compile (phase 1b, 41
  packages); phase 1c compiles the wasm sweep.
- **FULL GATE: 248 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`. Stdlib source only this round.

## FIXED in round 99: `net.IP` dissolved - the slice route, and what the compiler does NOT check

`net.IP` is a named SLICE type with 18 methods, none of which mutates the
receiver, so it takes the alias route (the `IPMask`/`HardwareAddr` shape):

    -type IP []byte
    +type IP = []byte

    IsUnspecified/IsLoopback/IsPrivate/IsMulticast/IsInterfaceLocalMulticast/
    IsLinkLocalMulticast/IsLinkLocalUnicast/IsGlobalUnicast
        -> IPIs*(ip)
    To4/To16/DefaultMask        -> IPTo4/IPTo16/IPDefaultMask(ip)
    Mask(mask)                  -> IPApplyMask(ip, mask)   (IPMask is the type name)
    String/AppendText/MarshalText -> IPString/IPAppendText/IPMarshalText(ip)
    Equal(x)/UnmarshalText(t)   -> IPEqual(ip, x)/IPUnmarshalText(ip, t)
    appendTo/matchAddrFamily    -> ipAppendTo/ipMatchAddrFamily(ip, ...)

**The important finding of this round is about the compiler, not the type.** After
the alias went in, `moxie build -a` on `net` reported **rc=0** while
`tcpsock_posix.mx:137` still said `l.IP.Equal(r.IP)` - a call that cannot resolve
against `[]byte`. A probe inside the package (`func _probeEqual(a, b IP) bool {
return a.Equal(b) }`) also compiled. The same expression in a USER program is
rejected:

    main.mx:9: Equal undefined (type string has no field or method Equal)

So the main package's bodies are checked and a dependency's are not: an
unresolved call inside a dependency body is dropped silently, and a short var
that depends on it only surfaces later ("cannot resolve type of short var X
(rhs=nil)") or not at all. Every remaining IP method call in the tree had to be
found by inspection instead of by the compiler, and one was found only after the
new fixture exercised `net.IPNet.Contains` at run time:

    src/net/{tcpsock_posix,iprawsock,ipsock,sock_posix,tcpsock,udpsock,udpsock_posix,addrselect,dial}.mx
    src/net/http/socks_bundle.mx

`ip_api` is the guard, and it is deliberately a MAIN-package fixture for exactly
that reason: it calls ParseIP, IPString, the Is* predicates, IPTo4/IPTo16,
IPEqual (across the 4-byte and 16-byte forms), CIDRMask, IPMaskSize and
`IPNet.Contains`, and it prints `ip ok`. It found the `addrselect.mx` sites that
the dependency build had passed over.

The wasm sweep caught one more site the native build had passed over -
`crypto/x509/verify.mx:826` called `ip.String()` on a `net.IP` LOCAL, which no
`.IP.<method>` grep could see, and the dropped argument surfaced as the
misleading `not enough arguments in call (want 8, have 7)`. A direct build of
`./src/net/http` - both targets - is the check that catches this class, because
building a package as the TARGET gives its whole closure the thorough treatment:
after that fix, native and wasm both compile.

Two traps worth recording for the remaining passes: `netip.Addr` has its OWN
IsLoopback/IsLinkLocalUnicast/IsMulticast/IsPrivate/IsGlobalUnicast methods, so a
blanket rewrite of those names breaks `addrselect.mx` (it did, and was reverted);
and reverting such a file wholesale also reverts the legitimate rewrites in it
(the `To4` sites had to be redone).

### Verified

- `ip_api` compiles AND runs, natively and on wasm: ParseIP, IPString, the Is*
  predicates, IPTo4/IPTo16, IPEqual across the 4-byte and 16-byte forms,
  CIDRMask, IPMaskSize and `IPNet.Contains` (which uses IPApplyMask internally).
- `./src/net/http` builds for BOTH targets with its whole closure given the
  thorough treatment, and phase 1b (41 packages) plus phase 1c (wasm) pass.
- **FULL GATE: 250 PASS / 0 FAIL / 0 SKIP**, including
  `selfhost:fixpoint PASS (stage2 == stage3)`. This round needed three gate runs:
  the first was invalidated by the `mime_header_parse` fixture (round 98's API
  change), the second by the silently-dropped `ip.String()` in
  `crypto/x509/verify.mx` that only the wasm sweep caught, and the third is
  green. Stdlib source only - no compiler change.

## FIXED in round 100: `http.Header` dissolved - the last named public API type

`http.Header` is a named map type, so it takes the alias route:

    -type Header map[string][][]byte
    +type Header = map[string][][]byte

    Add/Set/Get/Values/Del            -> HeaderAdd/Set/Get/Values/Del(h, ...)
    Clone/Write/WriteSubset           -> HeaderClone/HeaderWrite/HeaderWriteSubset(h, ...)
    get/has/write/writeSubset/sortedKeyValues
                                      -> headerGet/headerHas/headerWrite/
                                         headerWriteSubset/headerSortedKeyValues(h, ...)

The name survives (a Header is still `Header{...}`, `Header(...)`, `h[key]`, and
`Request`/`Response` fields are unchanged); call sites change from `h.Get(k)` to
`HeaderGet(h, k)`. Round 98 had already inlined the five map methods, so this
pass was signatures plus call sites.

**What made it long is round 99's finding, and it is worse than recorded there.**
Round 99 said a DEPENDENCY's bodies are not checked. Here `./src/net/http` was
built AS the target and its own bodies were not checked either: `header.get(...)`
in `server.mx:1234` (where `header := cw.header`, a Header) resolved to nothing,
`te` came out nil-typed, and the failure only surfaced at CLANG:

    ._src_net_http.ll:74732: error: '%t316' defined with type 'i32'
      but expected '{ ptr, i64, i64 }'
      %sc454 = call i1 @runtime.stringEqual(... %t316, ... "identity" ...)

So an unresolved call can be silent through typecheck, SSA and IR emit, and die
in the backend as a type mismatch with no source line. The compiler's only
reliable signal is the smattering of "not enough arguments" / "cannot use value
of type int32 as string" cascades that SOME forms produce. Every remaining
Header call site therefore had to be found by grepping for Header-typed
receiver names (`header`, `trailers`, `snapHeader`, `handlerHeader`, `hdr`, plus
`.Header`/`.Header()`), and the clang error was what proved the last of them.

Call sites fixed: ~40 across `src/net/http/{client,clone,csrf,fs,h2_bundle,
request,response,roundtrip_js,server,transfer,transport}.mx`.

`http_header_api` is the guard, a MAIN-package fixture for the reason above: it
pins canonicalization, first-value Get, all-values, Del, Clone independence and
both writers, natively and on wasm.

**All the named public API types in `src/net/**` are now dissolved**: IPMask,
HardwareAddr, ProtocolError, Dir, url.Values, MIMEHeader, Buffers, IP and Header.

### Verified

- `http_header_api` compiles AND runs natively: canonicalization, first-value
  `Get`, all-values, `Del`, `Clone` independence, `HeaderWrite` and
  `HeaderWriteSubset`.
- `./src/net/http` builds for BOTH targets with its whole closure - native
  compiles (it then fails to LINK only because a library has no `main.main`,
  which is what direct-building a package does) and `-target wasm` is rc=0.
  Phase 1b (41 packages) and phase 1c (`wasm:net/http PASS (compiles)`) are
  green, and phases 2-4 and 6 pass with `selfhost:fixpoint PASS`.

### Two wasm-phase findings, neither caused by the Header change

- **Any fixture importing net/http cannot be browser-validated on the
  standalone wasm target.** Linking net/http for `-target wasm` pulls its
  `js && wasm` files, so the module imports the `gojs` bridge, and the
  standalone validator host does not provide that module:

      WebAssembly.instantiate(): Import #18 "gojs": module is not an object or function

  `http_header_api` therefore carries `//:wasmtest skip` with that reason in the
  fixture. net/http's wasm COMPILATION is still covered by phase 1c, and the
  fixture covers behaviour natively.
- `wasm:field_select_base_and_zero_store` failed once with a browser
  NAVIGATION timeout ("navigating to http://127.0.0.1:5061/, waiting until
  load") - a harness flake, not a code failure; the fixture does not import
  net/http and it passes on re-run.

Phase 5 was then re-run after the fixture was marked native-only: **62 PASS / 0
FAIL / 0 SKIP**, including the previously flaky
`wasm:field_select_base_and_zero_store`, which confirms that failure was the
harness navigation timeout. Everything else in the full gate - phases 0, 0b, 1,
1b, 1c, 2 (with the selfhost fixpoint), 3, 4 and 6 - was already green against
these same sources, and only the fixture and this file changed between the two
runs.

## Working artifacts live in the tree's `./tmp`, never `/tmp`

`/tmp` is wiped between sessions on this machine. Losing half a gate's temp root,
status file and logs costs a full re-run - hours - so the policy is now built
into the harness rather than remembered:

- `STATUSFILE` and every log default to the tree: `$MOXIEROOT/tmp/...`, and long
  runs are redirected there (`> tmp/gate.log`).
- `TMPROOT` - the throwaway per-fixture copies - MUST stay OUTSIDE the tree, and
  that was measured the hard way: with `TMPROOT` under `$MOXIEROOT/tmp`,
  `restrict:reject_slice_to_array_ptr` FAILED because a fixture inside the tree
  is not a user package, so the language restrictions do not fire. The same
  fixture is rejected from `/tmp` and from `$HOME` and accepted from
  `$MOXIEROOT/tmp`. It now defaults to `${HOME:-/tmp}/moxie-tests-$$`: outside
  the tree, and still readable after a `/tmp` wipe.
- `tmp/` and `tests/tmp/` are already in `.gitignore`, and the compiler's own
  `./tmp/mxc-build-*` work dirs are relative to the CWD, so running builds from
  the repository root keeps everything under the tree.
- Long runs record their log in the tree too, e.g.
  `bash tests/run.sh --full > tmp/gate.log 2>&1`, and the compiler build is
  `MOXIEROOT=$PWD ./moxie build -a -o tmp/mxc-<round> ./_mxc_stage4`.
- Nothing needed for a resume may be left only in `/tmp` or only in `$HOME`:
  put it in `tmp/` (git-ignored) so it survives with the checkout.

## FIXED in round 101: the stdlib is judged again - 30 silently-dropped calls found

Rounds 99 and 100 each ended with the same complaint: an unresolved call inside a
non-main package body was dropped SILENTLY, so the only way to find one was to
grep, and one surfaced only in clang as

    ._src_net_http.ll:74732: error: '%t316' defined with type 'i32'
      but expected '{ ptr, i64, i64 }'

The diagnostic that would have caught them already existed:
`reportUndefinedField` (`ssa_builder.mx`), which reports
`X undefined (type T has no field or method X)`. It was restricted to USER
packages:

    if !isUserPackagePath(restrictPath(fb.fn.Pkg.Pkg.Path)) { return }
    // "the stdlib has selector shapes stage4 cannot resolve yet ... would be false errors"

That exclusion was measured, not assumed. Removing it and forcing a full rebuild
of the 41-package closure produced **six more `net.IP` call sites that greps had
missed** and **eighteen more `http.Header` call sites**, with ZERO false
positives in the first-party tree:

    src/net/ipsock_posix.mx (String x3, Equal)  src/net/lookup.mx (To16)
    src/crypto/x509/verify.mx (Equal, String)
    src/net/http/{fs,request,response,server,transfer,transport,h2_bundle}.mx (24 in all)

VENDORED `golang.org/x/...` stays excluded, and that is a MEASURED false-positive
shape, not a guess: `idna.Profile` carries `bidirule func([]byte) bool` and the
package calls `p.bidirule(label)`, a call through a function-typed field, which
the walker reports as an unknown field. So the path check became exactly that:
`golang.org/x/` (and its `vendor/` spelling) is skipped, everything else in the
tree is judged. If another false error appears, narrow the SHAPE checks.

The wasm target then gave up six more, none of which any native build could see:

- `src/os/file_posix.mx` had NO build tag, so the wasm `os` package used
  `file_other.mx`'s stub `File` and `file.mx`'s `SetDeadline` /
  `SetReadDeadline` / `SetWriteDeadline` called `f.setDeadline(...)` and friends
  into nothing. Go tags that file `unix || (js && wasm)`; it now carries the same
  tag here.
- `src/net/net_fake.mx` (wasm-only) called `ip.String()` twice.
- `src/crypto/x509/verify.mx` called `san.String()` in a wasm-reachable path.

Both targets now compile the whole closure with the diagnostic on, and the 41
package sweep compiles AND runs.

## RE-MEASURED in round 102: closure captures WORK - the "13 mis-compiled files" item is stale

OPEN_WORK item 11 has said for many rounds that "stdlib capturing deferred
closures are mis-compiled (13 files)", and CLAUDE.md states as architecture that
a closure "does NOT capture variables from the enclosing function scope" - a
function value carries only a code pointer. Both are contradicted by running the
compiler. With `-allow-closure-captures` (the escape hatch the app repos use),
four shapes were built and run:

    n := int32(41)
    f := func(x int32) int32 { return x + n }
    f(1)                      -> captured=42

    apply(g, 1)               -> escaped=42      (closure passed to another func)
    defer func(){ print(m) }() -> deferred=7     (deferred closure)
    inc := func(){ n = n + 1 }; inc() -> write-local=2   (WRITE propagates)
    p := &m; addp := func(){ *p = *p + 5 }; apply(addp) -> write-pointer=15
    k := 100; read := func() int32 { return k }; k = 200; read() -> 200
                                                 (aliasing: the closure sees
                                                  the enclosing local's later
                                                  mutation)

So reads, writes, aliasing through a deferred closure and a closure that escapes
into another function are all correct. Whatever the intent of the architecture
note, the implementation keeps the enclosing state alive and visible, and item
11's blanket claim is not supported: no counterexample was produced by any of
these shapes. What is still true is the *restriction*: a user package is
rejected without `-allow-closure-captures` (that is a language rule, enforced,
not a codegen defect) - and the 13 stdlib files that trip it are listed in item
11 only as files that USE captures, not as files that are broken.

The next step for this item is therefore not a rewrite of 13 files: it is a
counterexample hunt. Exercise the specific stdlib closures (net/conf.mx's debug
closure, iter.mx's panic propagation, hooks.mx's exit hook) at RUN time; if one
misbehaves, the probe above is the template for pinning it.

## FIXED in round 102: the generic-literal trap - a func literal in a monomorphized body lost its signature

The wasm trap recorded in round 93 (and measured with the committed round-90
compiler, so it predates the func-literal rule) is fixed, and the root cause was
NOT wasm-specific.

A func literal inside a monomorphized body got its signature from
`tcResolveFuncInline(e.Type, litScope)` where `litScope` held the package scope
plus local types - but NOT the enclosing instantiation's type ARGUMENTS. So
`func(x T) (y T)` resolved `T` to nothing:

    [lit] main.wrap__int32 src=fallback params=[nil] results=[nil]

(measured with instrumented `buildFuncLit`). One nil signature, two symptoms:

- NATIONALLY the call was emitted as a VOID indirect call, so the result was
  silently DISCARDED - `v = g(v)` left `v` unchanged. An identity closure hides
  that completely, which is why every probe so far passed natively.
- on WASM the same signature became `call_indirect (type (i32,i32)->nil)` with
  the closure value stored void, and the module trapped at run time with
  `null function or function signature mismatch` (wasm-objdump, node - the
  browser's message names the same function).

The fix is to put the instantiation's type arguments in the scope, the same way
local types already were:

    for tn, tt := range fb.typeSubst {
        ls.Insert(NewTypeName(fb.fn.Pkg.Pkg, tn, tt))
    }

and then, because a signature with a nil-typed slot is never legitimate, to
re-resolve once when either source produced one (`sigHasNilSlot`) - the checker's
own signature for a literal in a generic body can carry the unsubstituted
TypeParam, and `llvmType` has no case for one.

Verified by the new `generic_closure_result` fixture, whose closure returns a
CAPTURED value rather than its argument, so a discarded result is observable:
it PANICS with `replace int32` on the previous compiler (build rc=0, run rc=134)
and prints `generic closure result ok` on this one, natively and in the browser.
`generic_funclit_param_type` no longer needs `//:wasmtest skip` - it was the
fixture that pinned the trap - and passes on both targets.

## FIXED in round 103: an unresolvable import was skipped silently, and four SWEEP entries named packages that do not exist

The loader's dependency walk dropped any import whose path resolved to no
package:

    pkg := discoverPkg(path, r.root)
    if pkg == nil {
        if path == r.rootPkg { fatal("cannot find package " | path) }
        return                      // <- silent for every DEPENDENCY
    }

`discoverPkg` returns nil exactly when no `.mx` file is includable for the
target, so this is the "no such package" case, not a build-tag case that some
other layer handles. legacy has always failed loud
(`legacy/loader/mxlist.go`: `resolving %q: package %q not found`), and the
skipped import drops every symbol the importer needed while the build returns 0.
The walk now reports `cannot find package X (imported by Y)`, and `C` - the CGo
pseudo-import legacy skips by name - is skipped here too.

**What it was hiding, measured.** A main importing `_ "encoding/json"`,
`_ "text/template"`, `_ "regexp"` and `_ "definitely/not/a/package"` built with
`rc=0`; legacy rejects all four (`package %q not found`). Consequences:

- **Four phase-1b SWEEP entries named packages that do not exist at all**:
  `encoding/json` and `encoding/xml` went with the reflect/any exodus
  (`3b6e25e8`, `4da18fb0`), `regexp` and `text/template` with the same sweep.
  Phase 1b reported "41 packages ok" while skipping four of them - and it was
  the phase whose whole purpose is to make broken packages visible. They are
  off the list, and with the loader strict the list is now self-verifying: an
  entry that does not resolve fails the build.
- **Ten more importers of nonexistent packages, all unreachable dead code**,
  each verified to have no in-tree importer before removal: `internal/lazyregexp`,
  `internal/lazytemplate`, `internal/coverage/pods`,
  `internal/coverage/cfile/testsupport.mx`, `internal/trace/testtrace`,
  `internal/trace/traceviewer`, `internal/profile/prune.mx`, and
  `_mxc_stage4/{syntax,token,cmd/mxlex}` - stale duplicates of
  `pkg/{syntax,token}` whose imports (`mxc/token`, `mxc/syntax`) never resolved.
  `index/suffixarray` keeps its SA-IS core: only `FindAllIndex`, whose sole
  dependency was `regexp`, is gone (a comment names what it did).
- The `cgo` files are NOT affected: `evalBuildLine` treats an unknown tag as
  false, so `cgo && !osusergo && unix` excludes them and os/user compiles the
  pure-Moxie implementation. That is now RUN-verified by `os_user_api`
  (`user.Current`, `Lookup("root")`, `LookupGroup("root")`, and a lookup that
  must fail), not merely "it compiles".

**Wood law:** legacy already implements this rule, so the change is stage4
catching up - no legacy edit is needed, and the diagnostic text deliberately
mirrors legacy's.

## FIXED in round 103: a function-local named type lost its type descriptor

Found because the loader became strict and `internal/trace` could finally be
built as a target: **the package did not compile at all**.

    _src_internal_trace.ll:41908: error: use of undefined value
      '@reflect/types.type:slice:named:trace.perP'

`MutatorUtilizationV2` declares `type perP struct { gc, series int32 }` INSIDE
the function and uses `[]perP`; the monomorphized `mxutil.Ensure__trace.perP`
relocates its `[]perP` result through the codec, and the descriptor the codec
call names had no definition.

Two failures of the same root cause, measured in the IR:

    pre-fix:  @"reflect/types.type:named:main.perP" = constant { i8 0, ptr null }   <- kind 0 stub
              (no slice descriptor at all; the reference dangles)
    post-fix: @"reflect/types.type:named:main.perP"          = { i8 58, ... pkgpath, "main.perP" }
              @"reflect/types.type:pointer:named:main.perP"  = ...
              @"reflect/types.type:slice:named:main.perP"    = { i8 22, ptr private, ptr named }
              @"reflect/types.type:pointer:slice:named:main.perP" = ...

**Root cause.** Descriptors are emitted at module tail by RECONSTRUCTING the
type from the descriptor tail text (`typeFromTail`), because the collected Type
objects may point into released fn-arena memory. A package-level named type is
found through `e.pkg.Pkg.Scope` by name; a function-local type has no
package-scope declaration, so `typeFromTail("named:trace.perP")` returned
`Typ[Invalid]` (emitting the kind-0 stub, which is a silent wrong descriptor for
every codec/interface read), and `typeFromTail("slice:named:trace.perP")`
rebuilt the element as `basic:invalid`, emitting the slice descriptor under the
name `slice:basic:invalid` - a definition for a name nobody referenced.

**Fix.** A local type declaration now builds its type in the package arena and
registers it under the tail its own name produces (`registerLocalType`), and
`typeFromTail` consults that registry after the scope lookups. Two things had to
be true for this to be sound, and both were checked:

- The registry must outlive the declaring function, so it lives in the PACKAGE
  arena. `cctx.compileArena` was declared and cleared but never assigned - a
  dead field; it is now set where the package arena is created and cleared with
  it (`cctx.localTypeReg = nil` at every `ArenaFree`), which also fixes
  `collectTypeDesc`'s arena switch for any caller that used cctx rather than the
  emitter's own copy.
- A local type can only be NAMED by code in its own package, so the registry of
  the package being compiled is always the right one - the same module both
  references and defines these descriptors.

Verified by running: `./src/internal/trace` was rc=1 with clang's "use of
undefined value" before the change and compiles after it (rc=1 only for the
library link's `undefined symbol: main.main`). Also verified in the IR, above,
and by four new SWEEP entries that only compile because of this fix:
`internal/trace`, `index/suffixarray`, `internal/profile`,
`internal/coverage/cfile`.

Recorded, NOT fixed (measured, loud not silent): two local types with the SAME
name in one package collide, because a local type's descriptor tail and
instantiation symbol are built from pkg+name only:

    func a() { type T struct{ x int32; data []byte };  ... mxutil.Ensure(ts, 1) }
    func b() { type T struct{ y int32; name []byte };  ... mxutil.Ensure(ts, 1) }
    main.mx:24: cannot use value of type []main.T as []main.T in assignment

The instantiation cache keyed on the name is the thing to change, and a
per-declaration discriminator in the local type's name is the fix; it touches
descriptor tails, instantiation symbols and interface-dispatch matching at once,
so it wants its own round and its own gate.

Also measured while here, so nobody has to re-derive it: `emitCodecDecode` does
not call `collectTypeDesc` (only `emitCodecEncode` does). That is not a live
hole - a decode is always emitted in the same module as its matching encode
(intra-module return relocation), and cross-module calls do not use the codec
pair at all: the caller's module of a cross-package call contains ZERO
`codecDecodeValue` calls (verified on a two-package probe).

## RE-MEASURED in round 103: os/user, the rule-9 arrays, and the live fork path

- **os/user** compiles the pure-Moxie files (`lookup_unix.mx`,
  `listgroups_unix.mx`, `user.mx`, `lookup.mx`, `lookup_stubs.mx`) with the four
  cgo-gated files excluded, and it WORKS: `user.Current()` ->
  `uid=1000 name=mleku home=/home/mleku`, `user.Lookup("root")` -> `uid=0 gid=0`,
  `user.LookupGroup("root")` -> `name=root gid=0`. Pinned by `os_user_api`.
- **Rule-9 arrays**: `os.Pipe` already uses `p := []int32{:2}` with a comment
  saying why. The `[2]T` sites that remain in `src/syscall` are in
  `exec_linux.mx` (line 5 is `//:build ignore`) and `exec_unix.mx` (same tag),
  i.e. files no target compiles - reference copies of Go's syscall exec, not
  live code. The live fork path is `src/os/exec_linux.mx`, it contains no
  arrays, and it works: `os.StartProcess("/bin/echo", ...)` printed the child's
  `child-says-hi` and the parent exited 0.
- **`src/os/exec`** exists but holds only `ExitError`: `exec.Command` is not
  implemented, so `os/exec` cannot be used by an app yet (a missing feature, not
  a defect in the item list). `os.StartProcess` is the working entry point.

## FIXED in round 103: unsafe.Pointer's operand, and a bare relative build target

**`unsafe.Pointer(x)` where x is not a pointer is now a compile error.** The type
is declared `__any->ptr`, so every operand type was accepted and a value that is
not a pointer silently BECAME one: `unsafe.Pointer(b)` for a struct `b`
compiled and produced a pointer to nothing, and `unsafe.Pointer(int32(1))`
folded to a nil Pointer and printed nil. The spec allows exactly a pointer, a
uintptr, unsafe.Pointer itself or nil, and go/types enforces that on the legacy
side, so this is stage4 catching up.

Two things had to be measured to make the check right rather than plausible:

- **The constant fold ran before the check.** `unsafe.Pointer(int32(1))` folded
  to a constant and returned, so a check placed after `foldConstConvert` never
  saw it (`unsafe.Pointer(int32(1))` compiled, rc=0). The shape checks now run
  before the fold in both conversion paths.
- **Type identity is the wrong question.** `uintptr` in this tree is a NAMED
  type over the target word (`pkg/types/tc_universe.mx`: `NewNamed(NewTypeName(
  nil, "uintptr", nil), uintptrUnderlying)`), not the Basic, so
  `V == Typ[Uintptr]` and even a Kind test on the underlying type rejected
  `unsafe.Pointer(ring)` for a `ring uintptr` parameter - 49 false errors in
  `src/runtime`. The predicates compare KINDS and accept the uintptr named type
  by name.

Verified by running: struct / slice / string / int32 / map / constant operands
are all rejected with `cannot convert value of type T to unsafe.Pointer (only a
pointer, a uintptr or nil)`; `unsafe.Pointer(&b)`, `unsafe.Pointer(uintptr(0))`
and `unsafe.Pointer(nil)` compile and run; the whole `net/http` native closure
and `src/runtime` build with ZERO diagnostics from it, so no first-party site is
a false positive. Fixture: `reject_unsafe_ptr_nonpointer`.

**A bare relative directory is a target.** `moxie build pkg` died with
`cannot find package pkg` while `moxie build ./pkg` built the same tree.
`isDirPath` already called a bare relative directory a directory - that is what
selects `ensureModuleRoot` - but `discoverPkg`'s directory branch accepts only
".", "..", "/", "./" and "../", so the bare spelling fell through to
import-path resolution and resolved to nothing. `normalizeTarget` now spells it
`./pkg` once, where the target is read, so `discoverPkg`, `targetPkgPath` and
`compilePath` all see the same thing. Verified by running all five spellings
(absolute, `./pkg`, `pkg`, `a/b`, `.`) - they build and print `entry ok` - and a
library target (`src/net/url`) still behaves as a library.

**`debug/gosym` has a functional test written, and it is PARKED, not passing.**
`gosym_pclntab` builds a Go 1.2 pclntab by hand - header, functab with a
sentinel PC, two `_func` records, a file table and four pc-value programs - then
asserts `LookupFunc`, `PCToFunc`, `PCToLine` and `LineTable.PCToLine` against
it. Writing it found the relocation bug recorded below: `NewTable`'s returned
Table points at a recycled copy of the caller's LineTable, so `tbl.PCToLine`
dies in interface dispatch. The fixture now lives in
`tests/regressions/known_fail/` (outside the suite's auto-run set, so the suite
stays green) with the analysis in its header, and it moves back the moment that
bug is fixed. Nothing exercised that parser before this round either way: the
package compiled and no test called `NewLineTable` or `NewTable`.

## RESOLVED (round 103 found it, round 104 fixed it, round 123 re-verified): a returned object kept a pointer to a COPY of a caller's object

**STATUS: FIXED, and the fixture is back in the auto-run set.** The hypothesis
recorded below - that the relocation's transience test was classifying a
caller-arena pointer as callee-transient - was WRONG. The trigger was a CYCLE,
and the mechanism was in the codec, not the relocation: the encoder's and
decoder's seen tables drifted out of step, so a back-reference index resolved by
position to the wrong object. Round 104 reduced it to three probe variants (a
caller pointer alone: correct; with a plain slice: correct; with a slice whose
elements point back into it: garbage), instrumented both tables (ENC adds 16,
DEC adds 24), and fixed the drift. See "FIXED in round 104: the codec's two seen
tables drifted" below for the mechanism.

Round 123 re-verified it by running, on the round-112 compiler:

- `tests/regressions/should_compile/gosym_pclntab.mx` (the original
  reproduction, 220 lines, building a pclntab by hand) prints `gosym ok`;
- a new focused oracle,
  `tests/regressions/should_compile/returned_object_identity/`, reduces the
  recorded shape to ~40 lines: a pointer PARAMETER to a caller-arena `Holder` is
  stored into a local `Carried`, the frame arena is churned (32 x 48-byte
  allocations) inside the call, `&local` is returned, and the caller then (a)
  writes through the returned pointer and requires the write to land in its own
  `Holder`, and (b) re-reads it after 64 more allocation rounds and a second
  `Carry` call. It prints `returned-object identity ok`, and it passes wasm
  validation with the same output.

The original entry follows, unchanged apart from this header.

**Shape.** A function stores a POINTER PARAMETER into a local struct and returns
the address of that local:

    func NewTable(symtab []byte, pcln *LineTable) (out *Table, err error) {
        var t Table
        if pcln.isGo12() { t.go12line = pcln }   // caller-arena object
        ...
        return &t, nil
    }

At the return, the relocation is supposed to copy the returned Table into the
caller's arena and keep `t.go12line` pointing at the caller's own LineTable
(identity, because it is an ancestor-arena pointer, not the callee's transient
frame). Measured with temporary prints in `findFunc`, it does not: the Table's
field points at a second address in the same arena, allocated AFTER the returned
Table, whose contents are already recycled by the next call.

    main: lt=0x7f218c806c50 tbl=0x7f218c807a20
    findFunc recv=0x7f218c806c50 v=2 nfun=2      <- lt.PCToLine, correct
    findFunc recv=0x7f218c808138 v=0 nfun=3620744 <- tbl.PCToLine, recycled copy
    panic: interface dispatch: no impl for Uint64 (6 impl(s), none matches)

gdb on the failing call: `#1 (*funcTab).uint32  #2 (*funcTab).pc
#3 (*LineTable).findFunc  #4 (*LineTable).go12PCToFile  #5 (*Table).PCToLine`,
and the garbage fields are why the interface dispatch cannot match - the iface
slot read from the recycled memory holds a uint64-shaped word.

**What was ruled out by measurement** (each probe compiled and run): a plain
`return &local` and `return &CompositeLit` are relocated correctly and survive
churn; `(ptr, error)` returns in all three spellings (bare `return`, named
assignment, direct) are correct; a struct embedding `*T` returned by value from
a method on `T` is correct; a struct with map/interface/slice fields returned
across a package boundary is correct. What is NOT yet reduced to a small probe is
this exact combination - caller-arena object + pointer parameter stored into a
returned local + heavy allocation inside the same call.

**Next step.** Reduce it further with the parked fixture as the oracle (delete
fields from `LineTable`/`Table` until it stops failing), then fix the relocation:
the transience test that decides "copy or keep identity" is classifying a
caller-arena pointer as transient and copying it into the frame arena that the
return is about to release.

## ENVIRONMENT (round 103): libLLVM-22 is gone, so phase 2 cannot run here

`tests/phase2_selfhost.sh` stops at `selfhost:stage1`:

    legacy compiler failed: purego LLVM: cannot find libLLVM-22; set LLVM_LIB_PATH

`/usr/lib/libLLVM-22.so` no longer exists on this machine and neither does
`/usr/include/llvm-22`; the only LLVM left is `/usr/lib/llvm-20` (headers AND
libs, too old for the tree's pin). Earlier sessions had the runtime `.so`, which
was enough for the prebuilt `legacy/moxie` to run its backend even though it
could not be rebuilt; now legacy's CODEGEN cannot run either, so the
legacy -> stage4 half of the bootstrap chain is unavailable here, and the
`selfhost:stage1` failure is environmental, not a regression.

The stage4 -> stage4 fixpoint is still checkable without legacy, and was:

    tmp/mxc-r103k  (built by tmp/mxc-r103j, -a)  sha256 b1463fa4b268386f27fd0bf67ebe3955b7d4e41c85f8c9e7766d8749106d4f98
    tmp/mxc-r103l  (built by tmp/mxc-r103k, -a)  sha256 b1463fa4b268386f27fd0bf67ebe3955b7d4e41c85f8c9e7766d8749106d4f98

IDENTICAL, so a generation reproduces itself.

See the round-103 verification block below for the exact hashes and the phases
that were run in this environment.

## Round 103: what was verified, in this environment

The gate (tests/run.sh --full, MOXIE=tmp/mxc-r103j) was run and reached phase 2,
where the ENVIRONMENT stops it (libLLVM-22 is gone, above). The phases that do
not need legacy were then run on their own, and the results are:

| phase | result |
| --- | --- |
| 0 regressions (native, 130 fixtures) | PASS, 0 FAIL |
| 0b restrictions | PASS, 0 FAIL - `reject_missing_import` and `reject_unsafe_ptr_nonpointer` both reject with the expected message |
| 1 package compilation smoke | PASS |
| 1b stdlib sweep | **41 packages ok** - includes the four new entries (internal/trace, index/suffixarray, internal/profile, internal/coverage/cfile) |
| 1c js/wasm sweep | PASS (net/http, path/filepath, syscall/js) |
| 2 self-hosting chain | ENVIRONMENTAL FAIL at `selfhost:stage1` (legacy's backend cannot load libLLVM-22) |
| 3 IR validate | PASS |
| 4 binary smoke | PASS |
| 5 browser wasm validator | re-run after `os_user_api` was marked native-only: see below |
| 6 instrumentation | every `:stage4` variant PASS; every `:legacy` variant FAILs for the phase-2 reason |

`selfhost:fixpoint` equivalence was established without legacy, by the stage4
lineage, with byte-identical output:

    tmp/mxc-r103k  built by tmp/mxc-r103j -a  ==  tmp/mxc-r103l  built by tmp/mxc-r103k -a

Phase 5's first run reported three failures, and NONE of them was a code
regression - both causes are conditions of the target:

- `const_exact_fold` and `generic_type_method` print through `.String()` on a
  builtin, and the wasm target does not carry the builtin String() bodies:
  they live in `src/runtime/stringer.mx` behind `//:build moxie.stringer`, and
  `buildTagTrue("moxie.stringer")` is deliberately false for wasm
  (`_mxc_stage4/main.mx`: "the wasm target has its own codec path and does not
  carry them"), so the interface path uses the synthesized identity wrappers
  (`emitBasicIdentities`) instead of a real method. A DIRECT `x.String()` call
  therefore cannot type-check on wasm:

      main.mx:37: String undefined (type int64 has no field or method String)

  Reproduced with the round-95-era `./moxie` as well, so it predates this
  round. Both fixtures are about constant folding and generic methods, not
  about wasm stringer support, so they now carry `//:wasmtest skip` with that
  reason - the explicit opt-out `tests/wasm/validate.sh` documents for exactly
  this case.
- `os_user_api` reads /etc/passwd through os/user, and the standalone wasm
  validator host has no such file; it carries the same opt-out.

Phase 5 was then re-run alone: **total 61, passed 61, failed 0, skipped 0**.

## Round 103 commits

    efbdf32c  docs: rounds 85-103 - what was measured, what is still open
    727887ee  round 103: an import that resolves to nothing is an error, and a local
              type keeps its descriptor (39 files, -19330 lines: the dead importers)
    834b4485  round 102: a func literal in a monomorphized body lost its signature
    e43ee327  round 101: judge the stdlib again - 30 silently-dropped calls found
    09680a70  tests: TMPROOT must stay outside the tree

`./moxie` was refreshed from the tree and is the stage4 fixpoint binary:

    ./moxie == tmp/mxc-r103k == tmp/mxc-r103l  (sha256 b1463fa4...4f98)

## FIXED in round 104: the codec's two seen tables drifted, so cyclic back-references resolved to the wrong object

Round 103 parked a relocation bug in `tests/regressions/known_fail/`. It is now
fixed, the fixture passes (`gosym ok`), and it is back in `should_compile/`.

**The shape, reduced by measurement.** Three variants of one probe, each
returning `&localStruct` from a lib function, each holding a pointer to a
CALLER-allocated object:

| variant | contents of the returned struct | result |
| --- | --- | --- |
| A | caller pointer only | correct |
| B | caller pointer + plain slice | correct |
| C | caller pointer + slice whose elements point back into that slice | **garbage** (`ownerV=-1742715504`, want 10) |

The embedding was a red herring; the **cycle** is the trigger. In variant C the
leaf's `owner` field points back at the slice element that owns it, and after
the return it pointed at recycled memory - which is exactly what killed
`debug/gosym`: `NewTable`'s returned Table held a pointer to a recycled copy of
the caller's LineTable and the process died in interface dispatch
(`no impl for Uint64`).

**The mechanism, with instrumentation.** Both sides keep an append-only table of
addresses; the ENCODER writes a back-reference as the *index* of an address in
its table and the DECODER resolves that index *by position*, so the two tables
must receive exactly the same sequence of addresses. They did not. For the tiny
graph in variant C:

    ENC adds: 16      DEC adds: 24

Three decode-side extras, each of which shifted every later back-reference:

- the struct case registered the struct's base address AND each field address,
  while the encoder registered only the fields (for a struct reached through a
  pointer that made the counts match by accident, and for one reached directly
  it did not);
- the pointer `codecTagPtr` case registered `inner` AFTER the pointee's own
  branch had already registered its address, so a pointer-to-struct added one
  entry the encoder never added;
- the `codecTagBackref` case registered `orig` again, though the encoder adds
  nothing when it emits a back-reference.

**The fix** is one rule both sides now follow, written in `src/runtime/codec.mx`:

- a struct registers its OWN address first, then every field address (this is
  also what makes a self-referential struct - or a slice element that points
  back at itself - encode as a back-reference instead of recursing);
- a pointer registers its target ONLY when it does not descend into it (the
  stable and escaped-basic paths); a descending pointer leaves registration to
  the pointee, which registers itself before its children;
- a back-reference registers nothing;
- `codecDecodeInto` now decodes structs IN PLACE, so the address it registers is
  the field's real address. It previously decoded into a temporary and memcpy'd
  it into the field, which registered the temporary - so a back-reference into a
  struct-typed field pointed at a copy that nothing else would ever update.

**Verified by running.** The variant-C probe prints the right value; the gosym
fixture prints `gosym ok`; the compiler-bound cost is unchanged (5.7s against
7.1s for the pre-change compiler on the probe build, where the version that
registered the duplicated field-0 entry took over 7 minutes - the tables are
position-indexed and linearly scanned, so an extra entry per struct is both a
correctness hazard and a quadratic cost); the compiler rebuilds with the changed runtime and
the stage4 fixpoint is byte-identical:

    tmp/mxc-r104a (built by ./moxie)      sha256 a603ff3aa02d8b548344...
    tmp/mxc-r104b (built by tmp/mxc-r104a) sha256 a603ff3aa02d8b548344...

**A bake lesson, recorded because it cost an hour.** A user program links
`sysroot/runtime_go.bc`, NOT the runtime compiled from source, so an edit to
`src/runtime/codec.mx` is invisible to probes and fixtures until
`./moxie build-runtime` regenerates the bake. The instrumented probe printed
nothing at first for exactly this reason. The bake in the tree was from Oct 9
and is now current; any future runtime edit must re-bake before the suite can
see it.

**Fixture bug found on the way** (mine, not the compiler's): the first word of a
Go 1.2 file table is the table's own LENGTH - a table with two files is three
words (length, offset, offset) and file number N is read at `4*N`. Written as
"2" for two files, the second file is unreachable and the `Uint32` read runs off
the end. That surfaced as an index-out-of-range panic only AFTER the codec fix
let execution reach it; before, the LineTable pointer itself was garbage.

**New finding, NOT fixed** (a diagnostic gap of the round-100 class): the
concatenation operator with a plain builtin operand emits invalid IR instead of
a type error.

    println("i64=" | a)        // a int64
    error: '%t7' defined with type 'i64' but expected '{ ptr, i64, i64 }'
      %cc4 = extractvalue {ptr, i64, i64} %t7, 0

The idiom in this tree is `a.String()`, which is why nobody hit it; the compiler
should report "cannot use value of type int64 as string in concatenation". Both
targets are affected (native and wasm).

## Round 104: what was verified, in this environment

`tests/run.sh --phase N` for every phase that does not need legacy
(MOXIE=tmp/mxc-r104d, with the runtime re-baked from the fixed source):

| phase | result |
| --- | --- |
| 0 regressions (native) + 0b restrictions | **109 PASS, 0 FAIL** - includes the unparked `gosym_pclntab`, `os_user_api`, `reject_missing_import`, `reject_unsafe_ptr_nonpointer` |
| 1b stdlib sweep | **41 packages ok** |
| 1c js/wasm sweep | PASS (net/http, path/filepath, syscall/js) |
| 3 IR validate | 4/4 |
| 4 binary smoke | 1/1 |
| 5 browser wasm validator | **61 passed, 0 failed, 0 skipped** |
| 6 instrumentation | every `:stage4` variant PASS; the 28 FAILs are ALL `:legacy` variants, which cannot run here (libLLVM-22 is gone - see the environment note above). Zero non-legacy failures. |

`./moxie` is now the round-104 fixpoint binary (`tmp/mxc-r104d`,
sha256 1100c0c8ffb7eca48740c74f...), identical to `tmp/mxc-r104c`, and
`sysroot/runtime_go.bc` was re-baked from the fixed source with it.

## FIXED in round 105: `|` with a non-text operand died in clang instead of saying why

    var a int64 = 5
    println("x=" | a)

emitted a concatenation anyway and the backend rejected the module with

    error: '%t7' defined with type 'i64' but expected '{ ptr, i64, i64 }'
      %cc4 = extractvalue {ptr, i64, i64} %t7, 0

which names no source line, no type and no operator - the class of diagnostic
gap round 100 recorded for `header.get(...)`. `|` is the concatenation operator
when EITHER side is text and the bitwise OR otherwise, so a text/other MIX is a
type error, and it is now reported where it is written:

    main.mx:5: cannot concatenate value of type untyped string with value of type
    int64 ('|' concatenates text; use .String() on the value)

The rule needed three measurements to avoid rejecting legal code:

- `TextType` covers BOTH spellings, because `string` and `[]byte` are one type
  here, so `"a" | []byte("b")` stays legal;
- a NAMED string type is text through its underlying type (`type label string`);
- two non-text operands are a bitwise OR and must keep working (`(n | 2) | 8`),
  so only a MIX is an error;
- a type parameter is skipped: in a generic body the operand type is not
  resolved yet, and `"x" | v` with `v` constrained to `~string` is legal by
  instantiation.

Verified by running: the mixed form is rejected with the positioned message
above; `concat_text_forms` compiles and runs natively AND for the wasm target
(it deliberately prints no number, so it does not hit the builtin-Stringer gap);
the check does not fire on the compiler's own source (a self-build with it is
clean) and the stage4 fixpoint is byte-identical:

    tmp/mxc-r105a (built by ./moxie)       sha256 9a55f13f2bddd931cde72a7c...
    tmp/mxc-r105b (built by tmp/mxc-r105a) sha256 9a55f13f2bddd931cde72a7c...

Recorded rather than fixed, because fixing it would mean re-verifying the whole
suite for a cosmetic gain: `"x" | r` with a RUNE already had its own message
("cannot concatenate rune with string - convert to UTF-8 first") and now gets
this one too, so that one shape reports twice. Both name the same line and both
are true.

Fixtures: `reject_concat_non_text` (restriction) and `concat_text_forms`
(regression). Legacy's verdict on the source could not be measured here - the
prebuilt legacy binary now dies in LLVM init before it reaches its type checker
(libLLVM-22 is gone) - but the change cannot regress a working program: the
rejected source previously produced a clang error rather than a binary.

## Round 105: what was verified, in this environment

`tests/run.sh --phase N` for every phase that does not need legacy
(MOXIE=tmp/mxc-r105b), 195 PASS and ZERO non-legacy failures:

| phase | result |
| --- | --- |
| 0 regressions + 0b restrictions | **113 PASS, 0 FAIL, 0 SKIP** |
| 1b stdlib sweep | **41 packages ok** - the concat rule does not false-positive anywhere in the stdlib closure |
| 1c js/wasm sweep | PASS (net/http, path/filepath, syscall/js) |
| 3 IR validate | 4/4 |
| 4 binary smoke | 1/1 |
| 5 browser wasm validator | **62 passed, 0 failed, 0 skipped** - one MORE than round 104, because `concat_text_forms` is wasm-capable |
| 6 instrumentation | every `:stage4` variant PASS; the 28 FAILs are all `:legacy`, which cannot run here (libLLVM-22 is gone) |

`./moxie` is the round-105 fixpoint binary (sha256 9a55f13f2bddd931cde72a7c...).

## FIXED in round 106: the wasm Stringer gap - boxed builtins formatted as garbage and `.String()` would not compile

Two independent causes, both measured, both fixed.

**1. The builtin String() bodies were not selected for wasm.**
`buildTagTrue("moxie.stringer")` returned false for the wasm target (and the tag
was also listed in that target's forbidden set), so `src/runtime/stringer.mx`
stayed out and the interface path substituted identity thunks
(`emitBasicIdentities`). Those thunks are sound only for `[]byte`: boxing an
`int64` into a Stringer and calling `String()` produced a garbage slice header,
and the standalone host died printing it -

    JS error: RangeError: Offset is outside the bounds of the DataView
        at DataView.prototype.setUint32 (host.mjs:881, fd_write)
        at app.wasm.runtime.printstring
        at app.wasm.main.main

The tag is now true on EVERY target and out of the wasm forbidden list, so wasm
carries the real bodies - one source of truth for String(), as native does.

**2. A baked runtime's symbols could not be referenced.**
With the bodies selected the method set did have `String` (instrumented:
`methods=1` on the universe's int64), yet `resolveBuiltinMethod` still failed,
because it looks the runtime package up in `cctx.universe.Registry` and the wasm
runtime is BAKED (`sysroot/runtime_go_wasm.bc`), so it is deliberately not in
the registry - the keys at that point were
`[errors][io][main][moxie][os][unsafe]`. It now declares the method as an
EXTERNAL symbol against a stand-in runtime package, which the link resolves
against the bake; the bake does carry `runtime.(*int64).String` (verified with
`llvm-dis-22 runtime_go_wasm.bc`).

**Verified by running.** The boxing probe (`var s Stringer = n; s.String()`)
traps in the browser before the fix and passes after, printing `[42]`, `[7]`,
`[1.50000000000000e+000]` exactly as native does. A direct `.String()` probe
compiles for wasm and passes the validator. And FOUR fixtures that were
native-only *because of this gap* dropped their `//:wasmtest skip` and now pass
browser validation: `const_exact_fold`, `generic_type_method`, `gosym_pclntab`,
`const_receiver_width`. The only remaining opt-outs name reasons that have
nothing to do with stringers: `http_header_api` (the validator host provides no
`gojs` module) and `os_user_api` (the host has no /etc/passwd).

Fixpoint: `tmp/mxc-r106b` == `tmp/mxc-r106c` (sha256 d845fb52424abe0dfab25b6f...).

## Round 106: what was verified, in this environment

`tests/run.sh --phase N` for every phase that does not need legacy
(MOXIE=tmp/mxc-r106c, both runtimes re-baked from the fixed source):
**209 PASS, 0 non-legacy failures**.

| phase | result |
| --- | --- |
| 0 regressions + 0b restrictions | **113 PASS, 0 FAIL, 0 SKIP** |
| 1b stdlib sweep | **41 packages ok** |
| 1c js/wasm sweep | PASS (net/http, path/filepath, syscall/js) - the wasm runtime now carries stringer.mx and still compiles |
| 3 IR validate | 4/4 |
| 4 binary smoke | 1/1 |
| 5 browser wasm validator | **66 passed, 0 failed, 0 skipped** - FOUR more than round 105, exactly the fixtures the Stringer fix unparked |
| 6 instrumentation | every `:stage4` variant PASS; the 28 `:legacy` FAILs are environmental (libLLVM-22 is gone) |

`./moxie` is the round-106 fixpoint binary (sha256 d845fb52424abe0dfab25b6f...),
and `sysroot/runtime_go.bc` plus `runtime_go_wasm.bc` were re-baked with it.

## FIXED in round 107: two function-local types with the same name were ONE type

The repro was recorded in round 103 and left open. Two functions, each declaring
its own `type T struct{...}`, each calling `mxutil.Ensure(ts, 1)`:

    main.mx:24: cannot use value of type []main.T as []main.T in assignment

A true statement about a false premise. A local declaration's TypeName is only
`T`, so as far as two things were concerned the two types were the same one:

- the descriptor tail - `named:main.T` - so one descriptor described two layouts;
- the instantiation SYMBOL - `mangleGenericName` builds `Ensure__main.T` from
  `Type.String()`, and `monomorphizeAndCall` memoizes instantiations by that
  name in `targetPkg.Members` - so the second call reused the FIRST body and then
  the checker refused the assignment.

Each local declaration now gets a discriminator (`#1`, `#2`, ...) keyed by its
TypeName; every instantiation reuses the origin's `Obj`, so a generic local type
is covered too. The tag is appended in exactly two places - `typeDescName`'s
Named branch and `mangleGenericName` - and `registerLocalType`'s registry key
carries it, so `typeFromTail` still rebuilds the type at module tail.
`Obj.Name` is left alone, so diagnostics keep saying `main.T`, and a local type
can never be named by another package, so the mxh text (which keeps the plain
name) is unaffected.

Verified by running: the recorded failure compiles now and prints `a=5 b=22`,
while r106c still reports the old error on the same source - so the fixture
discriminates. New fixture: `local_type_same_name`.

Fixpoint: `tmp/mxc-r107b` == `tmp/mxc-r107c` (sha256 958955b0cb36072917274e8d...).
`tmp/mxc-r107a`, built by the PREVIOUS generation, differs - the one-generation
lag this file already documents, and the new lineage reproduces itself.

## Round 108: what was verified, in this environment

The change is jsruntime (host stubs) plus one fixture marker, so the phases whose
behaviour it can affect were run rather than all of them
(MOXIE=./moxie, the round-107 fixpoint binary - no compiler change this round):

| phase | result |
| --- | --- |
| 0 regressions + 0b restrictions | **114 PASS, 0 FAIL, 0 SKIP** |
| 5 browser wasm validator | **68 passed, 0 failed, 0 skipped** - the run WITH `http_header_api` wasm-enabled |

Phases 1b, 1c, 3, 4 and 6 do not touch the browser host: their results from
round 107 stand, and the compiler and stdlib sources are unchanged since.

## Round 107: what was verified, in this environment

`tests/run.sh --phase N` for every phase that does not need legacy
(MOXIE=tmp/mxc-r107c, per-compiler caches so nothing stale is reused):
**210 PASS, 0 non-legacy failures**.

| phase | result |
| --- | --- |
| 0 regressions + 0b restrictions | **114 PASS, 0 FAIL, 0 SKIP** - includes `local_type_same_name` |
| 1b stdlib sweep | **41 packages ok** - the stdlib's own local types (internal/trace's `perP` among them) go through the new naming |
| 1c js/wasm sweep | PASS (net/http, path/filepath, syscall/js) |
| 3 IR validate | 4/4 |
| 4 binary smoke | 1/1 |
| 5 browser wasm validator | **67 passed, 0 failed, 0 skipped** - one more than round 106 |
| 6 instrumentation | every `:stage4` variant PASS; the 28 `:legacy` FAILs are environmental (libLLVM-22 is gone) |

`./moxie` is the round-107 fixpoint binary (sha256 958955b0cb36072917274e8d...).

## FIXED in round 108: the validator host can load a module that imports gojs

`http_header_api` carried `//:wasmtest skip` because linking net/http for
`-target wasm` pulls its `js && wasm` files, the module then imports the `gojs`
bridge (`//:wasmimport gojs syscall/js.*`), and the standalone host provided
only `wasi_snapshot_preview1` and `env`:

    WebAssembly.instantiate(): Import #18 "gojs": module is not an object or function

The bridge itself stays unimplemented - it is Go-compat surface a jsruntime app
does not use - but the TEST host can now ask for load-only stubs:

- `buildHostImports` adds a `gojs` namespace ONLY when
  `globalThis.__moxie_test_stubs === true`, and every stub reports itself on
  stderr the first time it is called, so a fixture that actually reaches the
  bridge is visible instead of quietly wrong;
- `startDomain` carries that flag into the Worker's boot message and
  `worker-host.mjs` sets it before instantiating, because the module is
  instantiated in a Worker rather than on the page;
- `tests/wasm/host.html` - the test page - sets it. A production page does not,
  so a module that needs the real bridge still fails loudly there.

Verified by running: `http_header_api` - the one fixture that links the bridge -
PASSES the browser validator with the stubs, output matching native, and phase 5
reported **68 passed, 0 failed** on the run that included it.

It is nevertheless still marked native-only, and now by COST rather than by
failure: its native and wasm builds together take about **35 minutes**, and
`tests/wasm/validate.sh` wipes the per-compiler package cache at the start of
every run on purpose ("each run measures a cold compile"), so that cost is paid
on EVERY phase-5 run - a 2.4x tax on the phase. The marker's comment says all of
this and gives the one-line command to run it directly, so the capability is
documented and reachable rather than hidden; Header behaviour natively is
covered every run by phase 0, and net/http compiling for wasm by phase 1c.

## DECIDED and PINNED in round 109: `unsafe.TypeOf` accepts any operand on purpose

The objective's unsafe item named two builtins, and they are opposite cases.
`unsafe.Pointer(x)` was the bug - declared `__any->ptr`, it accepted a struct or
slice value and silently made it a pointer, which is why six sites in `src/net`
produced invalid IR instead of a compile error. That is fixed and pinned
(`reject_unsafe_ptr_nonpointer`, round 103). `unsafe.TypeOf(x)` returning the
compiler's own descriptor for whatever `x` is IS its purpose: the operand is
never converted, and comparing two descriptor pointers is how a Moxie program
asks "are these the same type" without reflection. Restricting it would remove
an ability, not close a hole - so the decision is to leave it and PIN the
semantics, which the new fixture `unsafe_typeof` does:

- two `int64` values have the same descriptor;
- `int64` and `int32` do not;
- two different struct types do not;
- a slice and a pointer do not;
- two function-local types in one function do not (the round-107 identity).

Verified by running: native `unsafe.TypeOf ok`, the wasm validator PASSes it
too, and phase 0 reports **115 passed, 0 failed, 0 skipped** with it in the
suite (114 before it).

## FIXED in round 110: `os/exec` works - and three TODO stubs were why it could not

`src/os/exec` held only `ExitError`. It could not have held more, because three
layers under it were stubs:

- `os.Process.Wait` returned `ErrNotImplemented`, so nothing could learn how a
  child ended;
- `os.ProcessState` answered `""` and `false` for every process, so even a
  successful child looked failed;
- `os.StartProcess` REFUSED `ProcAttr.Files` and `ProcAttr.Dir` outright
  (`ErrNotImplementedFiles`, `ErrNotImplementedDir`), so a pipe could not be
  handed to a child and `Output` had no way to capture anything.

All three are implemented:

- `Wait` calls `syscall.Wait4` (retrying on EINTR) and fills a real
  `ProcessState` - `Exited`, `ExitCode`, `Success`, `String`, `Sys` - which is
  also what `ExitError` reports through;
- `forkExec`'s child now `chdir`s to `Dir` and `dup2`s each `Files[i]` onto fd
  `i` before `execve`. It cannot report a failure to the parent yet (there is no
  error pipe), so it exits 127, the same code a failed `execve` gives;
- `exec.Cmd` lands on top: `Command`, `Start`, `Wait`, `Run`, `Output`,
  `CombinedOutput`, with `Dir`, `Env` and the three stream files.

**One measurement shaped the child wiring.** Every `dup2` must run BEFORE any
close, because two slots can name the SAME fd: `CombinedOutput` hands one pipe
over as both stdout and stderr, and closing the source after its first use left
the second `dup2` with EBADF - observed as `CombinedOutput: exit status 127`
while `Output`, which uses one slot, worked.

**Verified by running** (`os_exec_api`, native - a browser domain has no child
processes, so the fixture says so and phase 5 skips it):

- `exec.Command("/bin/echo", "hello", "world").Output()` returns exactly
  `hello world\n` through a real pipe;
- `exec.Command("/bin/sh", "-c", "exit 3").Run()` returns an `*exec.ExitError`
  whose `ExitCode()` is 3;
- `Cmd.Dir = "/"` reaches the child: `/bin/pwd` prints `/`;
- `CombinedOutput` collects `out\nerr\n`, both streams on one pipe.

`os/exec` is also a new phase-1b SWEEP entry, so "it compiles" is now checked
every run.

## FIXED in round 111: three slice-presize shapes were mis-scanned, and `return expr` ignored deferred writes

### The `{:n}` presize rewrite read the type backwards

`_mxc_stage4/ir_rewrite.mx` rewrites `T{...}` slice literals into
`__slicealloc(T, ...)` before parsing. `findSliceTypeStart` walks BACKWARDS from
the `{` to find where the type begins, and its three boundary rules each had a
hole:

| source | scan returned | result |
| --- | --- | --- |
| `[]map[int32]int32{:2}` | the `[` of `map[` | `[int32]int32` as the type text |
| `[]func() int32{:2}` | `int32` (stopped at the space) | `int32` as the type text |
| `[]chan int32{:2}` | `int32` (stopped at the space) | `int32` as the type text |

Instrumenting the rewrite (env-gated stderr, since removed) printed the fact
that settled it:

```
DBG presize scan at=50 -> lbrack=38 text=[[int32]int32]
DBG presize scan at=47 -> lbrack=42 text=[int32]
```

A balanced `[...]` pair closed to depth zero and was taken for the type's own
`[`, so the key brackets of `map[K]V` (and of a generic `List[T]`) won. And a
space ended the type unconditionally, which is right for `return nat{:1}` but
wrong for a type whose text contains a space.

Both rules are now qualified:

- a balanced pair starts the type only when the byte to its left is **not** an
  identifier byte, so `map[`/`List[` are skipped and the scan continues to the
  real `[`;
- a space ends the type only when the identifier it follows is not the result
  type of an open construct - `)`/`]` (`func() int32`, `map[K]V`) or a bare
  type keyword (`chan int32`, and `struct`/`interface`/`func`/`map` for
  symmetry).

Verified by running, all 17 shapes in one matrix (`x := <lit>` then
`len`/`cap`): `[]int32{:2}`, `[]byte{:4}`, `[]string{:3}`, `[]struct{ n int32 }{:2}`,
`[][]int32{:2}`, `[]map[int32]int32{:2}`, `[]func() int32{:2}`, `[]chan int32{:2}`,
`[]map[string]func(int32) int32{:2}` all give `len == cap == n`; `[]int32{:0:2}`,
`[]map[int32]int32{:0:2}`, `[]func() int32{:0:2}`, `[]chan int32{:0:2}` give
`len=0 cap=2`; `[]int32{:2:8}` gives `len=2 cap=8`; `[]byte{}` gives `0/0`;
`[]int32{7}` and `[]int32{1, 2, 3}` give `1/1` and `3/3`; and
`[]func() int32{:2}` with `x[0] = func() (r int32) { return 7 }` returns `f0=7`.

Earlier in the round a branch was added to the rewrite for a "presize form" of
`{:n}`. Instrumentation showed it was **unreachable**: `inner` is the text
AFTER the `{:`, so `{:2}` reaches the element/presize branch with `inner=[2]`,
which already emits the correct two-argument `__slicealloc(T, 2)`. The only
input that reached the new branch was `{::2}`, which the pre-change compiler
rejected loudly (`syntax error: unexpected comma, expected expression`), and the
branch silently gave it a meaning. It is removed.

**Bootstrap note:** the tree's `[]T{:n}` means `len == cap == n`; the "len 0,
cap n" spelling is `[]T{:0:n}`. A fixture that hand-wrote a Go-style
`if len(x) == cap(x) { grown := []T{:len(x)+4}; ... }` growth guard was
therefore appending past presized nil elements - `capture_semantics` segfaulted
on `fs[0]()` for exactly that reason, on BOTH the pre-fix and post-fix
compiler. The fixture now grows with a cursor write.

### `return expr` returned pre-defer values

`_mxc_stage4/ir_emit.mx` ran the defers (`emitRunDefers`) before returning, but
the value it returned was `r.Results[0]`, a register the SSA had loaded from the
named-result slot BEFORE the return instruction - so a deferred write to a named
result was silently discarded. Measured, before the fix:

```
ReturnLit=5   (want 11)   defer func(){ n = n + 6 }(); return 5
TwoResults=1,2 (want 7,2)
ReadSlot result=5 slotSeenByDefer=0   the slot itself read as zero inside the defer
Chain result=1 g=7        the deferred closures DO share the enclosing frame
```

`Chain` is what localized it: the first-registered closure read the second's
write (g=7), so the frame was live and shared - only the value handed to the
caller was stale.

`emitReturn` now does what Go does, in Go's order: store the operands into the
named-result slots, run the defers, then load the slots and return those. The
bilateral evidence is in the vendored go/ssa that legacy builds its SSA with
(`legacy/vendor/golang.org/x/tools/go/ssa/builder.go`): it stores the results,
emits `RunDefers`, and then "Reload (potentially) named result variables to form
the result tuple". Legacy already had the correct order through go/ssa, so the
fix is stage4-only and both compilers now agree.

Verified by running (`named_result_defers`, new fixture): `ReturnLit=11`,
`TwoResults=7,2`, `ReadSlot=5 slotInDefer=5`, `BareReturn=7`, `WriteBack=7`,
`Chain=7 g=107`, `ViaLocal=3`, `NoDefer=5`.

### Pinned: closure captures in dependency packages

`capture_semantics` (new fixture, main + lib) runs nine capture shapes inside a
DEPENDENCY package, where the user-code capture restriction does not apply:
read an outer local, escape the closure, write the outer local, write through a
captured pointer, observe a later mutation, defer a closure, bind a method
value, share a loop variable, and capture a slice element. It prints
`capture semantics ok (loop var shared: 3)`.

The recorded claim that "capturing deferred closures are mis-compiled in 13
stdlib files" had no run-time evidence either way. The measurement that
replaced it: compiling the whole 42-package phase-1b sweep with the new
`-report-closure-captures` flag lists **185** capturing functions, top packages
`(*ClientTrace)` 16, `(*http2serverConn)` 11, `(*Logger)` 10, `(*Builder)` 10,
`(*Transport)` 8, `(*persistConn)` 8. No counterexample to capture semantics was
found; the named-result ordering defect above was found by RUNNING the shapes
rather than by reading them.

### New open item: the parser silently accepts an expression where a type belongs

Found while chasing the mis-scanned rewrites, and independent of them:

```
x := []chan __slicealloc(int32, 2)     compiles, len=0 cap=0
```

This is what turned "the rewrite produced a wrong type text" into a silent zero
slice instead of an error - it is why `[]map[int32]int32{:2}` reported `0/0`
rather than failing.

Sharper, after mapping it: the PARSER is not at fault. It reports correctly
where a type is expected (`var y []chan 5` -> `unexpected literal 5`,
`var y []chan __slicealloc(int32, 2)` -> `unexpected ( at end of statement`),
and `stmtList` does enforce a statement terminator. What is accepted is the
EXPRESSION form, and it swallows trailing tokens:

```
x := []chan __slicealloc(int32, 2)   accepted, len=0 cap=0
x := []chan abc(1)                   accepted, len=0
x := []chan abc + 1                  accepted, len=0    <- `+ 1` silently dropped
x := []chan abc                      accepted, len=0    <- undefined type, no error
x := []chan int32(5)                 refused, but as a codegen error
```

Present on the pre-round-111 compiler as well (`mxc-r107c`), so it is old. The
statement parses, the RHS is a `*syntax.Operation` whose X is a slice/channel
TYPE rather than a value, and the front-end drops it and stores a zero slice
instead of failing loud. Two things follow: an expression that is not a value
must be an error at the point it is built (the type-checker or the SSA builder,
whichever sees it first), and an undefined type name inside `[]chan T` must be
`undefined: T`. **RESOLVED in round 112** (see below): the node WAS named by
instrumenting `buildExpr`'s fallthrough, and the count decided the fix - 230
slice-type drops in the compiler's own closure are composite-literal and
conversion types, so the fallthrough stays silent and the consumer that treated
the nil as a value reports instead.


## FIXED in round 112: a diagnostic that was printed and then ignored - the build exited 0 with a working binary

The open item from round 111 was exact: `x := []chan abc + 1`, `x := []chan
__slicealloc(int32, 2)` and `x := []int32` were all accepted, the trailing
expression vanished, and no undefined-type error was ever raised. What it did
not yet name was the mechanism.

The compiler was ALREADY reporting it:

```
compile error: cannot resolve type of short var x in main (rhs=nil)
main.mx:5: cannot use value of type []int32 as int32 in assignment
mxc: compile error for main
```

Both lines appear, and the build still succeeds. `buildExpr` returns nil for a
type node, `buildShortVarDecl` reads that as "type could not be resolved",
writes the diagnostic straight to stderr with `mxutil.WriteStr`, substitutes
`Typ[Int32]` so the pass can continue, and returns. The list that actually fails
a build is `cctx.compileErrors`, which `CompileToIR` gates on and turns into the
`;`-prefixed error IR that `compileSource` funnels into `fatal("compile error
for " | pkgName)`. A direct stderr write never reaches it, so the compiler
exits 0 with a binary built from the placeholder type.

**A measurement decided where the fix goes.** Instrumenting `buildExpr`'s
fallthrough and compiling the compiler's own closure counted the nodes dropped
there:

```
230 SLICE_TYPE
  5 CHAN_TYPE
```

So the fallthrough is hit constantly by REAL code - the type of a composite
literal, the target of a conversion (`[]byte(s)`) - and reporting there would
break every slice literal in the tree. The nil is not the bug; the bug is the
consumer that treats nil as a value. The fallthrough now says so in a comment
and stays silent.

**The fix** is one entry point and its callers:

- `reportCompileError(msg)` in `ir_typecheck.mx`, beside `cctx`, registers a
  diagnostic the way the typechecker always has;
- all six diagnostics that wrote to stderr and continued now register instead -
  the short-var one, `new()`, the two `clear` sites, and the rune/string
  concatenation one, which was pushing AND writing, so the write was a duplicate;
- `buildShortVarDecl` reports once, and only when nothing upstream already
  explained the statement (`compileErrorCount` before the RHS build), naming the
  kind when the RHS is a type node and the variable otherwise. `Typ[Int32]`
  stays as the continue-and-collect placeholder, and the registered error is
  what fails the build.

**Verified by running** (`reject_type_as_value`, new phase-0b restriction; each
is now a nonzero exit with the message, and each was ACCEPTED before):

| source | now |
| --- | --- |
| `x := []int32` | `cannot use a slice type as a value` |
| `x := []chan int32` | `cannot use a slice type as a value` |
| `x := []chan abc` | `cannot use a slice type as a value` |
| `x := map[int32]int32` | `cannot use a map type as a value` |
| `x := []chan abc + 1` | `cannot resolve the type of y in main - the right-hand side is not a value` |
| `x := []chan __slicealloc(int32, 2)` | `cannot resolve the type of y in main - the right-hand side is not a value` |

and every legitimate form still compiles: `x := []int32{:2}`, `var y []int32`
with `y = []int32{1, 2}`, `s := []byte("hi")`, `m := map[int32]int32{}`,
`x := []T{{1}}`.

`x := []int32` is NOT a supported declaration form, which is what makes
rejecting it right rather than a regression: it declares `x` as int32 (the
placeholder), proven by `main.mx:5: cannot use value of type []int32 as int32 in
assignment`, and no file in `src/`, `pkg/` or `cmd/` uses a bare type as a
short-var RHS. The gate logs for round 111 contained ZERO `compile error:` lines,
so no package or fixture was relying on an ignored diagnostic - which is why
routing all six through the gate could be done in one step.

Wording nuance left as is: for `x := []chan __slicealloc(int32, 2)` the RHS is a
CALL whose callee is the type, so the message says the type of the variable
could not be resolved rather than naming the callee. It is loud and it fails the
build; naming the callee is a refinement, not a correctness gap.

## FIXED in round 123: untyped integer constants wider than 64 bits lost their high bits WHEN NAMED

The entry below said `ConstInt` has no exact field and that the narrowing had to
be found before one could live anywhere. Both halves of that are now out of
date, and the measurement that replaced them is small.

**The exact machinery already existed.** `ConstInt` has carried `Exact
*big.Int` for a while, `intExact` prefers it, `intConstBig` keeps it while more
than 64 bits are needed, and floats have had `Exact *big.Rat` all along. What
was still broken was one conversion:

    case b.Info&IsInteger != 0:
        iv := cvInt(v)
        ...
        return MaskConstToBasic(&ConstInt{V:iv, Unsigned:unsigned}, b)

`ConvertConstLocal` rebuilt the constant with its 64-bit slot and **dropped
`Exact`**. That is the declaration path - `localizeConstVal` calls it to store a
named constant at its declared type - and it is the ONLY path that goes through
it, which is exactly why the same expression was exact inline and zero when
named. `MaskConstToBasic` already had the right shape: an untyped target passes
the value through unchanged, a sized one is rebuilt without `Exact` (correct -
a sized value really is truncated).

The fix carries `Exact` into that constructed value. Measured before and after,
on the same probe:

| expression | before | after |
| --- | --- | --- |
| `const big = 1<<70; big >> 64` | 0 | **64** |
| `const big = 1<<70; big >> 70` | 0 | **1** |
| `const m = (1<<64-1)<<8; m >> 64` | 0 | **255** |
| `const hi = (1<<26-1) << 'A'; hi >> 64` | 0 | **134217726** (= the inline value) |
| `(((1<<26-1) << 'A') >> 64)` inline | 134217726 | 134217726 (unchanged) |

Note the doc's old "want 33554430" for `hi >> 64` was itself wrong: `'A'` is 65,
so `(2^26-1) << 65 >> 64` is `(2^26-1) << 1`. The oracle that needs no hand
arithmetic is named-versus-inline, and that is what the new fixture asserts.

**Verified by running** (`const_fold_exact`, new phase-0 regression, also checked
for wasm): before the fix it panics with `named hi>>64 = 0, want 134217726`; after
it prints `untyped wide-constant folding ok`. Typed overflow is unchanged - `var
a int32 = 1 << 40` is still refused. Bootstrap fixpoint r123b == r123c.

A worked-around source in the tree (the textproto bitmap, split into two 64-bit
halves) is no longer required for correctness; the bitmap shape is one of the
assertions in the fixture.

## Round 124: what is actually left of the goal's list, measured

Swept the objective's own item list against the tree and the gates. Resolved
since they were written, with the evidence:

- **`makeStrongFromWeak`**: no occurrence anywhere in the tree (`*.mx`, `*.go`,
  vendor excepted). Removed rather than stubbed, which is the standing rule.
- **`regexp` and `text/template`**: deliberately absent. The sweep's own comment
  records the reasoning - neither is reflect-free, and every in-tree importer was
  dead code removed in the same round; with the loader strict, naming a package
  that does not exist now fails the build, so the list is self-verifying.
- **the relative-path entry point**: CORRECTED and VERIFIED (round 78) - it is
  Go's own import-path rule, not a bug.
- **the embedded tzdata registration**: VERIFIED in round 78, including in a
  browser.
- **`debug/gosym`, `syscall/js` compilation, `internal/coverage/*`,
  `internal/trace/internal/tracev1`, `compress/flate`, `crypto/ecdsa`,
  `crypto/ed25519`, the `src/net` public types, net/http's `context.Value`**:
  see the round-124 reconciliation above; all are done, and the packages are
  green phase-1b/1c sweep entries.

Still open, and this is now the whole of it:

1. **The rule-9 arrays in `syscall`/`os.Pipe`** - `exec_unix.mx:141`'s
   `var p [2]int32` and the `*[2]Timeval`/`*[2]Timespec` //sys directives. They
   work; they need a decision (keep and pin with a run-verified fixture, or
   convert), not a patch.
2. **`src/syscall/js` and `net/http/roundtrip_js` rule debt** inside their
   `js && wasm` constraint (value receivers, `any`, mutated globals). They
   compile; nothing runs them. Either convert them or pin them with a wasm
   fixture through the gojs stubs.
3. **The cgo-gated `src/os/user/cgo_*.mx` files are dead references.** They carry
   no build constraint of their own, `os/user` compiles and is a green sweep
   entry without them, and their job is done by the non-cgo implementations
   beside them. The standing rule is to remove dead references, not keep them.
4. **`any` in generic signatures** (`iter`, `weak`,
   `crypto/internal/fips140cache`, and generic functions across
   `maps`/`slices`/`bytes`/`internal/trace`): the compiler still SKIPS any file
   importing those three packages (`discoverPkg`'s skip-list), which silently
   drops symbols. The real fix is the concrete-constraint refactor plus deleting
   the skip-list; the skip-list's removal is the completion condition.

## FIXED in round 125: the cgo-gated `os/user` files were dead references, and are gone

The goal's list carried "the cgo-gated os/user files" as an open item. What they
actually were, measured with the compiler's own file list:

    discover os/user [lookup_unix.mx, listgroups_unix.mx, user.mx, lookup.mx,
                      lookup_stubs.mx]

Five files are live. Four were dead and could never compile:

    cgo_lookup_cgo.mx        imports "C"          - the loader refuses it
    getgrouplist_unix.mx     imports "C"          - the loader refuses it
    cgo_lookup_unix.mx       references _C_*      - defined ONLY by the file above
    cgo_listgroups_unix.mx   references _C_*      - defined ONLY by the file above

So the pair that looked like a fallback was not one: the `_C_*` type aliases and
accessors live exclusively in `cgo_lookup_cgo.mx`, which is itself excluded, so
the other two could not resolve a single one of the C-shaped types they are
written against. `getGroupList` and `groupRetry`, the only functions they
contributed, have no reference anywhere in the tree. Moxie has no cgo and no
`osusergo` build tag, so no configuration selects any of the four; keeping them
is a dead reference, which is what the standing rule says to remove rather than
keep "in case".

Removed, and verified by running after the removal:

- the `os_user_api` regression prints `os/user ok` (it asserts `Current`,
  `Lookup("root")`'s uid/gid/home, and the group lookups through the real
  `/etc/passwd` and `/etc/group`);
- `archive/tar`, the stdlib package that imports `os/user`, still builds and runs;
- the fixture's header no longer describes the cgo route as present, and records
  the measurement above instead.

The pure implementation was never the backup - it is the only implementation, and
it is the one the fixture has been running all along.

## NEW OPEN ITEM (round 125): a generic named function type is not substituted in the instantiated body

Found while trying to run the iterator APIs (`maps.Keys`/`Values`/`All`,
`slices.Values`/`Collect`) to prove the compiler's `any`-in-generics skip-list is
gone. `slices.Collect` turned out to be `iter_wasm.mx` (wasm-only, my error), but
reducing that away left a real defect. Whole repro, one file:

    type Seq[V comparable] func(yield func(V) bool)

    func gen[V comparable](v V) (s Seq[V]) {
        return func(yield func(V) bool) {     // line 6
            yield(v)
        }
    }

    var got int32
    func yieldIt(v int32) (cont bool) { got = v; return false }

    func main() {
        s := gen[int32](7)
        s(yieldIt)                            // line 20
    }

    main.mx:6:  cannot use value of type func(yield func(int32) bool) as main.Seq in return statement
    main.mx:20: cannot use value of type func(v int32) (cont bool) as func(V) bool in argument to function

Both diagnostics print the UNSUBSTITUTED type - `main.Seq` with no type argument,
and `func(V) bool` with the type parameter still in it - while the values being
rejected carry the correctly substituted `func(yield func(int32) bool)` and
`func(v int32) (cont bool)`. So the substitution reaches the func literal, the
call argument and the closure's parameter, and does NOT reach the instantiated
function's own signature: its result type is still `Seq[V]` and its parameter is
still `func(V) bool`. That is why instantiating `maps.Values[map[string]int32,
string, int32]` fails inside its own body

    cannot use value of type func(yield func(string, int32) bool) as iter.Seq2 in return statement
    cannot use value of type func(k string, v int32) (cont bool) as func(K, V) bool in argument to function

- the same two shapes, one level down in the stdlib.

**Where to look next (measured by reading, not guessed).** The SSA path DOES
substitute the signature - `monomorphizeAndCall` builds it with
`resolveSignatureWithSubst(fd.Type, typeSubst, srcScope)`
(`ssa_builder.mx:9772`) and sets `subFb.typeSubst` for the body. The failing
diagnostics are not from there; they are typechecker diagnostics
("cannot use value of type ... in return statement"), and the comment at
`ir_typecheck.mx:627` notes that monomorphized bodies substitute via
`fb.typeSubst` - i.e. the TYPECHECK's own visit of an instantiated body is the
side still holding the unsubstituted signature (`main.Seq` for the result,
`func(V) bool` for the parameter) while the func literal and the call argument
beside it carry the substituted `int32` forms. Fixing it means making the
typecheck's instantiated-body check substitute the same way the builder's
signature resolution already does.

**Why no gate caught it: `maps`, `slices` and `iter` are NOT phase-1b sweep
entries.** The sweep's list has 42 packages and none of them is the iterator
surface, so the whole `iter.Seq`/`Seq2` API is un-compiled and un-run by the
suite. Adding `iter`, `maps` and `slices` to the sweep is part of closing this.

## HAZARD (round 125, same root cause as the validator one): `moxie build -a` clears the SHARED cache

Running `./moxie build -a ...` while `tests/run.sh --phase 0/1b` is in flight
kills those runs the same way `tests/wasm/validate.sh` does: the log simply stops
mid-fixture, twice in a row, with no error. `-a` clears the `.bc` build cache that
the phases are reading from. Anything that clears the cache - `-a`, the wasm
validator - must run when no phase is running.

## DECIDED (round 127): closures do NOT capture, anywhere - the compiler and the stdlib get refactored to obey the rule

The design question was re-opened and settled by the owner: a function value in
Moxie carries a code pointer and no environment, a frame arena dies at the
creating call's return (FnArenaPop + ArenaRelease per return; munmap only per
domain exit), and there is no escape analysis to separate the closures that stay
inside their frame from the ones returned, stored in a global, stored in a
receiver field, or run by `defer` at return time. Capturing in-scope variables is
therefore forbidden, and the answer is NOT to relax the rule: it is to refactor
everywhere - compiler first, then the stdlib, then drop the `/pkg/` carve-out.

**Compiler status, measured with `-report-closure-captures` on a full
self-build: ONE capture left.**

    capture: ssaRecvRooted__anon1 [seen, recvParam, walk, localStore]

`pkg/syntax` is clean - the sixteen (seven `__mval` bound-method-value wrappers,
`appendGroup`, `trace`, `argList`, `complitexpr`, `structType`, `interfaceType`,
`listSep`'s callers, `paramList`, and the `init`/`initBytes` scanner callbacks)
were removed in the earlier rounds recorded above, the last two by giving
`Scanner.Init`/`InitBytes` and `Source.init`/`initBytes` the owning `*Parser`.
The one left is the recursive `walk` closure in `_mxc_stage4/ir_deepcopy.mx`,
now rewritten as an iterative worklist (`recvWalkItem` + `mxutil.Ensure`/`push`,
`seen` map, depth cap) so the compiler obeys the rule it enforces. Build and
report verification follow this entry.

**Stdlib status: 185 capturing functions** across the 42-package sweep (round-111
measurement, top packages `(*ClientTrace)` 16, `(*http2serverConn)` 11,
`(*Logger)` 10, `(*Builder)` 10, `(*Transport)` 8, `(*persistConn)` 8). That is
the bulk of the refactor and it is what blocks dropping `/pkg/` from
`isUserPackagePath`.

## FIXED in round 126: a generic instantiation is now bound into the types it is compared against

The doc's top item. `resolveTypeExprWithSubst`'s IndexExpr case builds a shallow
instantiated Named (`TArgs` set) and deliberately leaves `Under` generic - which
is right, `llvmTypeInner` binds the parameters itself for LAYOUT. Nothing bound
them for COMPARISON, so a monomorphized body compared a substituted value against
an unsubstituted shape:

    main.mx:6:  cannot use value of type func(yield func(int32) bool) as main.Seq in return statement
    main.mx:20: cannot use value of type func(v int32) (cont bool) as func(V) bool in argument to function

`checkAssignable` now binds both sides before reporting
(`bindInstantiation`: the Named's own TArgs, then the enclosing function's
`fb.typeSubst`), and takes the bound types for every check below so diagnostics
name the real types; `buildCall` derives the callee signature from the bound
type, which fixes the argument and variadic paths together. Verified: the exact
repro compiles and prints `got=7`; `Take(Counter)` with a parameter of the
instantiated named func type passes; `iter_apis` (new fixture) instantiates
`maps.Values`, `maps.All`, `maps.Keys` and a generic `Seq[V]` and prints
`iterators ok`. `iter`, `maps` and `slices` are now phase-1b sweep entries.
Fixpoint: r126b == r126c == r126d.

**Still open, narrower:** passing a function SYMBOL where a FOREIGN instantiated
generic named func type is expected -

    func (l *lib) ... ; main: lib.Collect(lib.Counter)     // Collect(seq iter.Seq[int32])
    cannot use value of type func(func(int32) bool) as iter.Seq in argument to function

The target prints as the bare `iter.Seq`, so the parameter type lost its
instantiation args on that path; a value already of the parameter's type works,
which is why the fixture returns `iter.Seq[int32]` from a function instead.

## Round 128: closure-capture elimination, batch 1 - and two build hazards found doing it

Inventories, all with `-report-closure-captures`:

- 31 small packages (`compress/*`, `crypto/*` small, `encoding/*`, `hash/*`,
  `internal/coverage/*`, `mime*`, `net/textproto`, `net/url`, `os/user`,
  `runtime/trace`, `text/tabwriter`, `unicode/utf8`) -> **3 captures**, all in
  `(*Hooks).Run`.
- `bytes`, `context`, `fmt`, `sort`, `time` -> **0 captures**. Their closure
  literals already take everything through parameters.
- `log` -> the `(*Logger)` cluster: EVERY logging path handed `output` a
  formatting callback closed over the caller's frame.

**FIXED - `src/log/log.mx`.** `output(pc, calldepth, appendOutput func([]byte)
[]byte)` is now `output(pc, calldepth, msg []byte)`. Twenty call sites collapsed
(`fmt.Append*(b, ...)` -> `fmt.Append*(nil, ...)`, `push(b, s...)` -> `s`), and
the body appends the message itself:

    *buf = mxutil.Ensure(*buf, len(msg))
    *buf = push(*buf, msg...)

The message is now built before the lock instead of inside it; the header is
still formatted under the lock, so ordering is unchanged.

**FIXED - `src/internal/runtime/exithook/hooks.mx`.** `Run`'s three deferred
closures are now deferred CALLS to free functions that take the receiver
(`hooksUnlock`, `hooksClearRunGoid`, `hooksThrowOnPanic`) - a direct call's
arguments live in the defer frame, which is the point. `recover` still works
because the deferred function is the one calling it.

Verified: the `log`-only report build is rc=0 with **0 captures**, and the
31-package batch rebuild is rc=0 with **0 captures**.

**HAZARD 1 - the compiler auto-fetches from the network.** An unresolved import
runs `git clone https://golang.org` (`main.mx:841`, "auto-fetch golang.org"),
and when the clone fails it returns "" and the build CONTINUES. Offline this
hangs for as long as the network makes it hang, and it created
`$HOME/moxie/golang.org` as a side effect. A build must not reach the network,
and an import it cannot resolve must fail loudly.

**HAZARD 2 - the repo lock is not reclaimable.** The clone holds
`$HOME/moxie/<repo>.lock` (content: a PID) with no timeout and no stale-PID
check. The killed clone left `golang.org.lock` behind, and the next build sat in
`waiting for lock: golang.org` forever - a build that will never finish and never
say why. Removing the lock unblocked it.

## Round 128: the net/http inventory (161 closures), and one part of it that the rule cannot mechanically fix

`net/http` + `httptrace` + their closure account for **161 capturing closures in
~120 distinct functions**. Top clusters, measured:

    16  (*ClientTrace).Compose
     5  setRequestCancel
     4  (*persistConn).readLoop
     3  (*Transport).dialConn          3  http2configureTransports
     3  http2ConfigureServer           3  EncodeHeaders
     2  (*socksDialer).connect         2  (*Request).write
     2  (*persistConn).addTLS          2  NewRequestWithContext
     2  (*http2ClientConn).roundTrip   2  (*Decoder).DecodeFull
    ...then a long tail of 1s: (*sysListener).listen*, (*transferWriter).write*,
    (*timeoutHandler).ServeHTTP, (*Transport).{roundTrip,getConn,
    startDialConnForLocked,dialConn__mval2,tryPutIdleConn__mval1}, ...

Two shapes in there are NOT ordinary closures:

**1. Bound method values (`__mvalN`)** - `(*Transport).tryPutIdleConn__mval1`,
`dialConn__mval2` and friends. The compiler emits these itself; the receiver IS
the environment. The doc's earlier `pkg/syntax` round removed sixteen of exactly
this kind by calling the method directly instead of materialising a value, and
net/http needs the same treatment.

**2. `(*ClientTrace).Compose` - 16 closures that the rule cannot remove without an
API decision.** Every composed hook is, by construction, a closure over the two
hooks it composes:

    if prev, cur := old.GetConn, t.GetConn; cur != nil {
        t.GetConn = func(hostPort string) { cur(hostPort); prev(hostPort) }
    } else {
        t.GetConn = prev
    }

Sixteen hooks, sixteen closures. A free function cannot stand in (a Moxie
function value carries no environment), a bound method cannot either (the
receiver is a capture by definition), and the hook signature has no room for a
holder - `func(hostPort string)` is the public API. `Compose` is LIVE: it is
called from `src/net/http/request.mx:413`.

So this needs a decision, not a patch, and the options are:

  (a) redesign the hook signatures to carry a holder (wide public API change);
  (b) drop `Compose` and change `request.mx:413` to consult both traces directly
      at the point of notification (loses the single-trace contract);
  (c) accept a narrow, documented exception for hook COMPOSITION - the one shape
      where a function value must carry an environment - while everything else
      (the wrapper closures, the `__mval`s) is refactored.

Everything else in the 161 is ordinary work: turn scheduling closures into direct
calls, give the work a helper with explicit parameters, and drop the `__mval`
bound-method values.

## FIXED in round 129: vendored packages no longer reach for the network, and a failed clone is fatal

Both parts came out of the same measurement. With a failed clone made FATAL (as
asked - a quiet "carry on" was how this stayed invisible), the suite went red
immediately and named the reason:

    mxc: auto-fetch failed: git clone https://golang.org for
         golang.org/x/crypto/cryptobyte - the package is not vendored and the
         network did not answer
    mxc: auto-fetch failed: git clone https://golang.org for
         golang.org/x/net/dns/dnsmessage

Both of those packages ARE in the tree, under `src/vendor/`. The bug was the
ORDER in `resolveRemotePkg`: for a `golang.org/...` path it called
`autoFetchRepo` first and only fell back to `src/<pkg>` / `src/vendor/<pkg>`
afterwards (main.mx:2872). So every build cloned a repository it already had -
slow, network-dependent, and fatal once a failure stopped being ignored. A
MANIFEST pin still resolves first: a pinned build must read exactly that ref.

Order is now: manifest pin -> `src/vendor/<pkg>` -> fetch (fatal on failure).

Verified with the network taken away on purpose (`PATH` pointing at a `git` that
exits 1): a COLD (`-a`) build of the 31-package probe succeeds, produces its
binary, and reports **0 auto-fetch attempts** - the vendored tree answers every
`golang.org/...` import. The fatal path itself is what the pre-fix run above
demonstrates; it now only fires for a package that is genuinely absent.

Gates: phase 0 123 passed / 0 failed / 0 skipped, phase 1b 45 packages, bootstrap
fixpoint r129d == r129e == r129f.

STILL OPEN from this family: the repo lock has no stale-PID reclaim
(`waitRepoLock` waits forever on `$HOME/moxie/<repo>.lock`), which is how a killed
clone blocked the next build indefinitely.

## Round 130: the httptrace hooks are WIDENED, so Compose needs no closures (16 gone)

Owner decision: option (a), widen the hook signatures. Done, and the whole
composition mechanism is now environment-free.

**The shape.** Every hook takes the ClientTrace it belongs to as its LAST
parameter, so a hook can reach trace state through the parameter instead of
closing over it:

    GetConn func(hostPort string, ct *ClientTrace)
    GotConn func(info GotConnInfo, ct *ClientTrace)
    Got1xxResponse func(code int32, header textproto.MIMEHeader, ct *ClientTrace) error
    ...16 in all

**Compose is a chain, not a nest.** It used to build sixteen closures, each
closing over the two hooks it composed. Now it links the older trace into a
`prev` field and the DISPATCH helpers walk the chain:

    func (t *ClientTrace) Compose(old *ClientTrace) { t.prev = old }

    func GetConn(t *ClientTrace, hostPort string) {
        for c := t; c != nil; c = c.prev {
            if c.GetConn != nil { c.GetConn(hostPort, c) }
        }
    }

Sixteen such helpers, which is also what call sites use now - so a call site
needs neither a nil guard nor a bound method value.

**Call sites and guards.** 30 hook calls in five files became helper calls
(h2_bundle 7, header 1, request 5, transfer 4, transport 13). The 26
`trace.X == nil` / `trace.X != nil` guards were reduced to their structural part
(`trace != nil`, or the enclosing conditions) by LINE-LEVEL edits - only the
hook-identity conjunct went away, because a guard on the FIRST trace's hook would
skip a composed one. An earlier attempt that dedented whole blocks with a regex
broke transport.mx; those five files were reverted and redone this way.

**The 1xx plumbing collapsed.** `get1xxTraceFunc`/`http2traceGot1xxResponseFunc`
existed only to hand the hook out as a plain `func(int32, MIMEHeader) error`
value; with the hook widened there is nowhere for the trace to live in that type.
The one call site now uses the helper directly, plus a chain-aware predicate
`HasGot1xxResponse` because the HTTP/2 layer uses the hook's PRESENCE (not just
its result) to decide who limits 1xx response size.

**Measured:** the net/http+httptrace closure went from **161 to 145** capturing
closures - the 16 `(*ClientTrace).Compose` ones are gone - the package builds, and
the probe runs (`http`). The remaining 145 are the ordinary shapes: wrapper
closures to turn into direct calls, and `__mvalN` bound method values.

Note for the public API: a user hook now takes the trace as its last argument
(`func(hostPort string, ct *httptrace.ClientTrace)`). That is the cost of (a).

## Round 131: seven immediately-invoked literals inlined (net/http 145 -> 138)

`func() { ... }()` blocks in net/http capture the enclosing frame for no
structural reason - they are blocks, not values. Seven had no locals escaping and
no defer/return inside, so they are now plain statements: `filetransport.mx`'s
response-writer fill, `h2_bundle`'s closeNotifier wait and reqBody close,
`transport`'s startDialConnForLocked, `socks_bundle`'s deadline select,
`transport`'s TLS handshake block (its `err` renamed `hsErr` to avoid shadowing),
and `h2_bundle`'s client-preface read (`buf` -> `prefaceBuf`).

Five IIFEs remain and each needs more than inlining: `client.mx`'s
`setRequestCancel` and `h2_bundle`'s ping writer RETURN or assign through
closures, and `server.mx`'s wraps a `defer func(){ recover() }` whose panic channel
is a local parameter - those want the same "widen the signature, pass what is
modified" treatment the httptrace hooks got.

Verified: net/http+httptrace builds, the probe runs, and the closure report drops
from 145 to 138. Gates: phase 0 123 passed / 0 failed / 0 skipped, phase 1b 45
packages.

## Round 132: setRequestCancel is a holder with methods, not three closures (net/http 138 -> 132)

The cluster's shape: `setRequestCancel` returned `stopTimer func()` and
`didTimeout func() bool`, built a `doCancel` closure, and ran an
immediately-invoked closure over all of them. Six captures, and no way to fix
them one at a time - the state has to live somewhere a function value cannot
carry.

It is now a `cancelState` struct with methods: `doCancel`, `stop`, `didTimeout`
and `run(initialReqCancel)`; `setRequestCancel` returns the state. The widening
cascaded exactly as the httptrace hooks did: `send` and `Client.send` hand the
state back instead of a `func() bool`, `do` holds it, and `cancelTimerBody`'s two
function fields (`stop func()`, `reqDidTimeout func() bool`) became one
`cs *cancelState`. `nop` and `alwaysFalse` became dead and were removed.

Two self-inflicted wounds worth recording: the cleanup that removed those dead
helpers also swallowed `ErrUseLastResponse` and `(*Client).checkRedirect` (the
comment-block boundary search walked too far), and `run` first took `chan
struct{}` where `req.Cancel` is receive-only. Both were caught by the compiler
and fixed - the declarations restored from a copy of the file, the parameter
widened to `<-chan struct{}` - before anything was committed.

Measured: net/http+httptrace 138 -> 132 capturing closures, with no
`setRequestCancel`/`doCancel`/`cancelState` entry left in the report. Gates:
phase 0 123 passed / 0 failed / 0 skipped, phase 1b 45 packages.

## Round 133: the last four IIFEs in net/http are gone, and the REST of the sweep is measured

Four immediately-invoked closures, each converted with explicit parameters:

- `server.mx`'s timeout-handler scope wrapped a `defer func(){ recover() }` and
  signalled a panic on a local channel. It is now `serveWithPanic(handler, tw, r,
  done, panicChan)` plus `sendPanic(panicChan)`, which must be the function
  deferred DIRECTLY because that is what `recover` applies to.
- `fs.mx`'s multipart range writer captured pw, mw, ranges, content, ctype and
  size; it is `writeMultiRange(...)` with all six as parameters.
- `h2_bundle.mx`'s ping writer assigned pingError and returned out of the IIFE
  twice; it needs no function at all - `WritePing`, then `Flush` only if that
  succeeded, then close errc if either failed.
- `transport.mx`'s CONNECT write+read captured resp, erC, connectReq and con5; it
  is `writeConnectRequest(connectReq, c)` and the caller closes didReadResponse
  itself (the only exits between write and close are its returns). Its first
  parameter was named `conn`, which shadows net/http's package-level `conn` type
  - the compiler caught it, and it is `c` now.

Measured: net/http+httptrace 132 -> **127**, and **zero `func() { ... }()`
immediately-invoked literals left** in the package.

**The rest of the sweep, measured in the same pass: 29 captures, ALL of them in
`net`** - the resolver (`internetAddrList`, `lookupIPAddr`), the dialers
(`dialIP`, `dialUDP`, `dialUnix`, `doDialTCPProto`), the listeners (`listenIP`,
`listenTCPProto`, `listenUDP`, `listenUnix`, `listenUnixgram`,
`listenMulticastUDP`), `pipeDeadline.set`, `resolverConf.addrLookupOrder`/
`hostLookupOrder`, `sendFile`. Everything else the sweep covers - crypto/x509,
crypto/ecdsa/rsa/elliptic, math/big, os/exec, internal/trace(+tracev1),
internal/profile, index/suffixarray, mime/multipart, compress/*, encoding/*,
debug/gosym, runtime/trace - reports ZERO.

## Round 134: net's control adapters and debug closures (net 29 -> 5)

**The ten control adapters were dead code around a deprecated field.** Every
dialer and listener built a closure whose only job was to adapt the deprecated
`Control` (no context) to `ControlContext`:

    ctrlCtxFn := sd.Dialer.ControlContext
    if ctrlCtxFn == nil && sd.Dialer.Control != nil {
        ctrlCtxFn = func(ctx, network, address, c) error { return sd.Dialer.Control(...) }
    }

`Dialer.Control` is never SET anywhere in the tree, and `ListenConfig` had only
the non-context form. So, per the standing rule (remove dead references rather
than keep them): `Dialer.Control` is gone, `ListenConfig.Control` is
`ControlContext` with the context-carrying signature (matching `Dialer`), and all
ten adapters collapse to `ctrlCtxFn := <config>.ControlContext`. Ten captures,
zero behaviour change - nothing ever populated the old field.

**Then four more:**

- `nss.mx`'s `parseCriteria` handed `foreachField` a closure that pushed onto its
  local slice. `foreachField` now takes a `fieldSink` interface
  (`add(field string) error`) and calls it directly; the accumulating state is a
  `criteriaBuilder` whose `add` is the body that closure had. That is the
  "add the parameters that actually get modified" shape, and it keeps parse.mx
  generic instead of coupling it to nss types.
- `conf.mx`'s three deferred DEBUG prints (addrLookupOrder, hostLookupOrder,
  initConfVal) are deferred CALLS to free functions now. `debugLookupOrder` takes
  `ret *hostLookupOrder` because deferred arguments are evaluated at defer time -
  the value being printed is set by the return, so it must be read through a
  pointer when the defer runs.

Measured: `net` 29 -> 5 capturing closures, and the package builds.

**The five left in net are structural, and need decisions rather than patches:**

1. `(*Resolver).lookupIPAddr__mval1` - a bound method value (`recv` is the
   environment); the fix is to call the method directly, as `pkg/syntax` did.
2. `(*Resolver).lookupIPAddr__anon2` and `(*Resolver).internetAddrList__anon1` -
   resolver fan-out over several lookup paths; the state (resolverFunc,
   lookupGroupCtx, network, host, net, portnum) wants a holder the fan-out drives.
3. `(*pipeDeadline).set` - the callback goes to `time.AfterFunc`, whose signature
   has no room for the deadline. This is the timer-service question: either the
   deadline state moves into a spawn'd timer worker (the documented manager/worker
   timer pattern) or the callback stays a capture.
4. `sendFile` - the callback goes to `syscall.RawConn.Write`, same problem, and
   the syscall layer owns that signature.

## Round 135: every remaining net/http capture is named, with its target

The report names the ENCLOSING function for a compiler-generated bound method
value (`setRequestCancel__mval1`), not the method, which is why grepping for the
method names found nothing. A throwaway instrumented compiler (two lines in
`buildMethodValue`, gated on `-report-closure-captures`, reverted immediately
after) printed the pair instead. All 25 remaining `__mval`s in net/http, exact:

    (*chunkWriter).writeHeader                 -> declareTrailer
    (*Config).ProxyFunc                        -> proxyForURL
    (*conn).serve                              -> startBackgroundRead
    (*http2ClientConn).closeConn               -> forceCloseConn
    (*http2clientConnReadLoop).endStream       -> copyTrailers
    (*http2clientConnReadLoop).run             -> healthCheck
    http2ConfigureServer                       -> startGracefulShutdown
    (*http2responseWriter).SetReadDeadline__anon1  -> onReadTimeout
    (*http2responseWriter).SetWriteDeadline__anon1 -> onWriteTimeout
    (*http2responseWriterState).writeChunk     -> declareTrailer
    (*http2serverConn).newStream               -> onWriteTimeout
    (*http2serverConn).processHeaders          -> onReadTimeout
    (*http2serverConn).processSettings         -> processSetting
    (*http2serverConn).serve                   -> onIdleTimer, onReadIdleTimer, onSettingsTimer
    (*http2serverConn).shutDownIn              -> onShutdownTimer
    (*http2Server).serveConn                   -> processSetting
    (*http2stream).endStream                   -> copyTrailersToHandlerRequest
    (*http2Transport).newClientConn            -> onIdleTimeout
    (*http2writePushPromise).writeFrame        -> writeHeaderBlock
    (*http2writeResHeaders).writeFrame         -> writeHeaderBlock
    (*Transport).dialConn                      -> Authenticate
    (*Transport).tryPutIdleConn                -> closeConnIfStillIdle

(`(*Resolver).lookupIPAddr -> lookupIP` is the net one; `setRequestCancel__mval1`
was removed in round 132.)

**Family A - convertible by widening the in-tree helper that receives the method
value into a sink, exactly as `nss.foreachField` was:**

| helper | callers | sites it fixes |
| --- | --- | --- |
| `foreachHeaderElement` / `http2foreachHeaderElement` | 4 / 2 | declareTrailer x2 |
| `registerOnHitEOF` | 3 | startBackgroundRead |
| `closeWithErrorAndCode` | 2 | copyTrailers, copyTrailersToHandlerRequest |
| `ForeachSetting` | 4 | processSetting x2 |
| `http2splitHeaderBlock` | 2 | writeHeaderBlock x2 |
| `http2ConfigureServer`'s `RegisterOnShutdown` | 1 | startGracefulShutdown |
| `(*Config).ProxyFunc` and `(*Transport).dialConn`'s `Authenticate` | - | need a look; ProxyFunc RETURNS a func, so it may be structural |

~14 call sites in all, so it is one batch, not fourteen commits.

**Family B - the timer callbacks, and the design that fixes them.** Twelve of the
25 are methods handed to a timer:

    sc.idleTimer       = sc.srv.afterFunc(sc.srv.IdleTimeout, sc.onIdleTimer)
    cc.idleTimer       = time.AfterFunc(d, cc.onIdleTimeout)
    pconn.idleTimer    = time.AfterFunc(t.IdleConnTimeout, pconn.closeConnIfStillIdle)
    ... onReadTimeout, onWriteTimeout, onReadIdleTimer, onSettingsTimer,
        onShutdownTimer, healthCheck, forceCloseConn

`afterFunc`/`time.AfterFunc` take `func()`, which has no room for the receiver -
the same wall as `pipeDeadline.set` and `sendFile`'s `syscall.RawConn.Write`.
`http2Server.afterFunc` is however OUR field (net/http sets it through
`http2.ConfigureServer`), so it can carry a receiver: replace
`afterFunc func(time.Duration, func()) *Timer` with a form that takes the owner
and a KIND -

    afterFunc func(d time.Duration, owner timerSink, kind int32) *Timer
    type timerSink interface { onTimer(kind int32) }

- and the owner switches on the kind. That is the "simpler structure to enable
  capturing the parameters that actually get modified" shape, applied to timers,
  and it needs a decision because it changes an exported configuration field.

## Round 136: family A converted - the helpers take the OWNER (net/http 127 -> 106)

The insight that made this one batch: the helper that receives a method value does
not need a func at all. `foreachHeaderElement(v, rws.declareTrailer)` becomes
`foreachHeaderElement(v, rws)` with

    type headerSink interface{ declareTrailer(key string) }

which removes the bound method value AND turns the other callers' closures into
owners as well - so the count drops by more than the number of `__mval`s.

Converted, with every caller:

- `foreachHeaderElement` and `http2foreachHeaderElement` now take a
  `headerSink`. `(*response)` and `(*http2responseWriterState)` already have
  `declareTrailer`, so two call sites pass the receiver; the two callers that had
  closures got owners: transfer.mx's `trailerSink` (it captured AND modified the
  trailer map and the error, both now fields) and h2's `headerKeySink` (captured
  the trailer map).
- `registerOnHitEOF` and `(*body).registerOnHitEOF` take an `eofSink`
  (`startBackgroundRead()`, the only implementation being `*connReader`), and
  `body.onHitEOF` is that interface instead of `func()`.
- `http2splitHeaderBlock` takes a `headerBlockSink`; both
  `http2writeResHeaders.writeHeaderBlock` and
  `http2writePushPromise.writeHeaderBlock` match it exactly, so both call sites
  pass the writer.

Measured: net/http+httptrace **127 -> 106** capturing closures, package compiles.

Still family A, next pass: `ForeachSetting` (4 callers, 2 of them closures wanting
owners), `closeWithErrorAndCode`/`closeWithError` (whose two receivers have
different method names, so it needs a common `onPipeEOF` method on each), and the
two lookups (`(*Config).ProxyFunc`, `(*Transport).dialConn`'s `Authenticate`).
Family B (the twelve timer callbacks) still wants the `afterFunc` decision.

## Round 137: family A finished and family B done - timers take an OWNER (net/http 106 -> 87)

**Family A remainder.** `ForeachSetting` now takes a `settingSink`
(`processSetting(s http2Setting) error`): `(*http2serverConn)` already has that
method, so its two call sites pass the receiver, and the two closure callers got
owners - `settingsDebugSink` (captured and modified a buffer and a count) and
`clientSettingsSink` (captured cc and set `seenMaxConcurrentStreams`, both fields
now). The two `(*Config).ProxyFunc` / `(*Transport).dialConn -> Authenticate`
sites turned out to be in VENDORED code (`golang.org/x/net/http/httpproxy` and the
socks bundle), so they are left alone and noted.

**Family B: `time.TimerSink` + `time.AfterFuncOwner`.** `afterFunc`/`time.AfterFunc`
take `func()`, which has no room for the receiver, so the closure could not be
removed at the call site - and widening the in-tree `afterFunc` alone would only
have MOVED the closure into it. The fix is one level down:

    type TimerSink interface{ onTimer(kind int32) }
    func AfterFuncOwner(d Duration, owner TimerSink, kind int32) *Timer

which boxes `{owner, kind}` in the ROOT arena exactly as `AfterFunc` boxes the
func (so the box outlives the timer) and trampolines through `goTimerSink`. No
closure anywhere. `time.AfterFunc` is untouched - it still serves everyone else.

On top of it: `http2Server.afterFunc`/`http2Transport.afterFunc` and the
test-only group interface take `(d, owner, kind)`; `http2serverConn`,
`http2stream` and `http2ClientConn` each gained an `onTimer(kind)` dispatcher
switching to the methods that were previously referenced as values; all ELEVEN
h2 timer sites pass the owner and a kind, including the two stream-deadline
re-arms and the mark-dead timer. `net/http`'s own timers use
`time.AfterFuncOwner` too (`persistConn.closeConnIfStillIdle`, and the TLS
handshake timeout whose `errc` is now a `handshakeTimerSink` field).

`net/pipe.mx` is the same story and shows why the design is right: `pipeDeadline.set`
carried a comment about creating its callback with the ROOT arena current, because
"a closure environment is allocated in the arena current where its func literal is
written". `AfterFuncOwner` boxes in the root arena itself, so that dance - and the
closure - are gone; `(*pipeDeadline).onTimer` closes the channel.

Measured: net/http+httptrace 106 -> 87, net 5 -> 4 (only `pipeDeadline` moved;
`sendFile` and the resolver remain). The package builds and links.

Self-inflicted errors on the way, all caught by the compiler before any commit:
an index-based splice mangled `processSettings` and the file was reverted and
re-patched with exact-text replacements; `s.Val`/`s.ID` in the moved sink body had
to become `setting.Val`/`setting.ID` (the closure's parameter was named `s`);
`onReadTimeout`/`onWriteTimeout` live on `*http2stream`, not
`*http2clientStream`; and a named `err` result shadowed the body's `if err :=`.

## Round 138: the two recipes applied at scale (net/http 87 -> 52)

A cold (`-a`) report run gave the authoritative remainder: 87 captures across ~50
functions, and they are almost all ONE of two shapes. Both now have a worked
recipe, applied to the biggest clusters in this pass:

**Deferred closures become deferred CALLS with the state as an argument.** A
deferred closure reads the value it captured when the defer was REGISTERED, which
is why the ones that print or act on a named result or a mutated local needed a
pointer:

    defer func() { pc.close(closeErr); pc.t.removeIdleConn(pc) }()
      ->  defer pc.cleanupAfterReadLoop(&closeErr)
    defer func() { if err != nil { cancel.Cancel(err) } }()
      ->  defer cancelRoundTripOnError(cancel, &err)
    defer func() { if err != nil { w.cancel(t) } }()
      ->  defer cancelWantOnError(t, w, &err)

**Local closure helpers become methods.** `readLoop`'s `tryPutIdleConn` closure
(wrote the enclosing `closeErr` too) is `(*persistConn).putIdleConnAfterRead(treq,
closeErr *error)`; `(*persistConn).roundTrip`'s `handleResponse` closure is
`(*persistConn).handleResponseFor(req *transportRequest, startBytesWritten,
debugRoundTrip bool, re)`; `send`'s `forkReq` (which ASSIGNED its enclosing `req`)
is `forkRequest(reqp **Request, ireq)`.

Removing those three closures took 35 captures with them, because each contained
nested closures of its own - the count is not the number of edits.

Three compile errors on the way, all mine, all fixed: `handleResponseFor`'s first
parameter is a `*transportRequest` (not `*Request`), and the extracted method
could not see `roundTrip`'s LOCAL `const debugRoundTrip`, so it is a parameter now.

Still to convert, same two recipes: the remaining 52 across net/http (readLoop's
inner closures, `(*Transport).dialConn`, `http2ConfigureServer`,
`http2configureTransports`, `(*Request).write`, `NewRequestWithContext`,
`(*http2ClientConn).roundTrip`, `EncodeHeaders`/`DecodeFull` in net/textproto,
`serveFile`/`ServeContent`/`readRequest`/`(*Server).Shutdown`/the routing index,
and the rest of the singles), and net's four (`sendFile` - whose callback goes to
`syscall.RawConn.Write`, the resolver fan-out, and one bound method value).

## Round 139: the deferred-closure family (net/http 52 -> 43) and what the last 43 want

Nine deferred closures became deferred CALLS - and the important detail is that a
deferred closure reads what it captured when the defer was REGISTERED, so every
one that acts on a named result or a mutated local takes a pointer:

    request.mx   httptrace.WroteRequest over `err`      -> reportWroteRequest(trace, &err)
                 the closeBody guard over `closed`/`err` -> closeBodyUnlessClosed(r, &closed, &err)
    request.mx   readRequest's EOF normalisation         -> normalizeEOFError(&err)
    server.mx    conn.readRequest's write deadline       -> setWriteDeadlineAfter(c.rwc, d)
    server.mx    conn.serve's recover/cleanup            -> (*conn).finishServe(&inFlightResponse, runHooks)
    transfer.mx  writeBody's BodyCloser close            -> closeBodyIfNeeded(t, &closed, &err)
    pattern.mx   parsePattern's offset annotation        -> annotateParseError(&err, off)
    socks        connect's finish                        -> finishSocksConnect(done, &ctxErr, errCh)
    h2_bundle    runHandler's recover/cleanup            -> (*http2serverConn).finishHandler(&rw, req, &didPanic)

Two self-inflicted wounds, both from the same mistake and both caught by the
compiler: my helper insertions carried the function signature AND were prepended
to the anchor, so five files got a doubled signature (`func (t *transferWriter)
writeBody(func (t *transferWriter) writeBody(w io.Writer) (err error) {`), and
`finishServe`'s first parameter name `runHooks` shadows net/http's package-level
`runHooks`, so the parameter is `doHooks` and the call site passes the local.

**The last 43, by kind - two of the three are mechanical:**

1. MECHANICAL, the two recipes (about 36 sites): `(*Server).Shutdown`'s
   `nextPollInterval` helper, `(*Client).do`, `(*chunkWriter).writeHeader`,
   `serveFile`, `ServeContent`, `(*ioFS).Open` (widen `mapOpenError` to take the
   FS), `(*transferWriter).probeRequestBody`, `(*Transport).dialConn` (cm, pa,
   Authenticate), `(*persistConn).readLoop`'s two, `waitForContinue`,
   `(*http2serverConn).startPush`/`readFrames`, `http2ConfigureServer` (2 anon +
   1 mval), `http2configureTransports` (3), `(*http2ClientConn).roundTrip` (2),
   `(*http2clientStream).encodeAndWriteHeaders`, `(*http2Framer).readMetaFrame`,
   `http2NewFramer`, `http2new400Handler`, `(*http2priorityWriteScheduler).Pop`,
   `(*http2responseWriter).Set{Read,Write}Deadline`,
   `(*Client).makeHeadersCopier`, `(*routingIndex).possiblyConflictingPatterns`,
   `(*routingNode).matchingMethodsPath`, `(*ServeMux).registerErr`,
   `appendSorted`, `adjustNextProtos`, and `closeWithErrorAndCode` (whose two
   receivers need a shared `onPipeEOF`).
2. NEEDS THE PUBLIC FIELD WIDENED, exactly as `afterFunc` got: `ProxyURL` and
   `defaultTransportDialContext` both return a `func(...)` that is stored in a
   public field, and `NewRequestWithContext` builds `Request.GetBody` that way.
   The closure is unavoidable while the field's type is a bare func.
3. net's four: `sendFile` (the callback belongs to `syscall.RawConn.Write`),
   `(*Resolver).lookupIPAddr`'s mval and anon, `internetAddrList`'s anon.

## Round 140: the last 47 captures, and the compiler bug that was eating one of them

Every capture in the tracked remainder is gone: the 43 in `net/http`, the 4 in
`net`, and three more the same sweep turned up in
`net/http/internal/httpcommon` (that package only entered the census once the
whole closure was rebuilt with the new compiler). The two recipes did the work
again - a deferred closure becomes a deferred CALL whose state is an argument,
a local helper or callback becomes a METHOD on an owner type - and the public
fields that could not carry state as a bare func were widened the way
`afterFunc` already was:

    Request.GetBody        -> GetBodyFunc, with bufferGetBody / readerGetBody /
                              emptyGetBody owners (NewRequestWithContext)
    Transport.Proxy        -> ProxyFunc (envProxy for ProxyFromEnvironment,
                              fixedProxy for ProxyURL)
    Transport.DialContext  -> DialContextFunc (dialerDialContext carries the
                              *net.Dialer)
    Server.TLSNextProto    -> map[string]TLSNextProtoHandler (h2TLSNextProto,
    Transport.TLSNextProto -> map[string]TransportNextProtoHandler
                              h2UnencryptedTLSNextProto, h2TLSUpgrade,
                              h2UnencryptedUpgrade)
    syscall.RawConn        -> Control(RawConnControlAction), Read/Write(
                              RawConnAction), forwarded through internal/poll
                              and net.rawConn; net.sendFile's syscall now runs
                              as sendFileAction
    Server.onShutdown      -> []ShutdownHook (h2's server state is the hook)
    singleflight.Do/DoChan -> Call (lookupIPCall carries the *Resolver)
    hpack.Decoder.emit     -> FieldSink (http2metaHeaderSink, DiscardFields)
    httpcommon.EncodeHeaders -> HeaderSink (http2ClientConn is the sink)
    http2serveConnFunc     -> http2serveConnMsg (http2setDeadlineMsg)
    http2pipe.readFn       -> http2pipeEOF (onPipeEOF copies trailers)
    http2writePushPromise  -> http2promisedIDAllocator
    walkReadyInOrder       -> http2priorityVisitor (http2popVisitor)
    bodyEOFSignal          -> bodyEOFFunc / bodyEarlyCloser
    mapping.eachPair       -> deleted; its only caller iterates the mapping
                              directly (a concrete visitor could not be named
                              through the generic type)
    appendSorted           -> sort.Search inlined, and the insert rebuilt in one
                              pass: copy(es[i+1:], es[i:]) was a NO-OP, because
                              a Moxie subslice is a copy
    adjustNextProtos       -> slices.DeleteFunc inlined as a presized pass

### The compiler bug underneath `matchingMethodsPath`

`(*routingNode).matchingMethodsPath` refused to compile:

    ; cannot resolve the type of p in (*routingNode).matchingMethodsPath -
      the right-hand side is not a value

The construct was `for method, c := range n.children.m { if p, _ :=
c.matchPath(pat7, nil); p != nil { ... } }` and the range value's type was the
problem. Instrumenting the compiler (four bootstraps) printed the whole story:

    DBG range (*routingNode).matchingMethodsPath iter=map[K]V val=V
    DBG field kind=named targs=2 tparams=0 t=main.mapping idx=1 ft=map[K]V
    DBG instfail base=mapping argKind=name:K

`routingNode.children` is `mapping[string, *routingNode]`, but the Named that
reached the SSA builder had **TArgs and no TParams**. The instantiation copy is
a shallow `&Named{..., TParams: named.TParams, TArgs: args}` made where the
instantiation is written; a copy made in another file - before that file's own
pass attached the parameters - froze an empty TParams, and with no parameters
there is nothing to bind the arguments to. So the field type stayed `map[K]V`,
the range value's type stayed the type parameter `V`, method resolution on `V`
found nothing, and `buildExpr` returned nil WITHOUT registering an error: the
call was silently dropped. The fix is `namedTParams` (ir_types.mx): the
declaration object the copy points at is the authority for the parameters, and
every reader of `TParams` now goes through it (`llvmTypeInner`, `fieldType`,
`instantiatedUnder`, `substTypeIn`, the descriptor copy). `varObject` in
ssa_builder.mx is the same rule for the variable binding: a range/short-var
object is registered with the type the SSA layer allocated for it, not with the
type the front end recorded.

### The second finding: a bogus type assertion compiled silently

While instrumenting, `n.Obj.(*TypeName)` - where `Named.Obj` is ALREADY
`*TypeName` - compiled and answered ok=false, so the fallback branch ran and
the real bug stayed hidden for hours. go/types rejects that ("invalid type
assertion ... (non-interface type ... on left)"); stage4 accepted it. It no
longer does: `checkAssertOperand` reports it for both the single and the
comma-ok form, verified by running:

    $ tmp/mxc-r144 build -o /tmp/at1/bin /tmp/at1
    ; invalid type assertion: main.T is not an interface

### Wood law

No legacy change is needed for either fix, and the check is positive rather
than assumed: the legacy compiler consumes `go/types` + `golang.org/x/tools/go/ssa`
(legacy/header.go, `loader.Load(config, pkgName, types.Config{})`), so it
substitutes instantiated field types and rejects non-interface assertions
already. Stage4 was the deviating side; these two changes move it to the same
behaviour.

### The complete census, and what is left

With the whole closure rebuilt by the fixed compiler, the report is now a census
of the entire build - not just net/http. It found net/http and net at ZERO, and
three more sites in `net/http/internal/httpcommon` (EncodeHeaders' local
`enumerateHeaders` helper and its two callbacks, now `enumerateRequestHeaders` +
`HeaderEmitter` with `headerSizeCounter` and `requestHeaderWriter`). What
remains is 20 sites, all in vendored packages:

    golang.org/x/crypto/cryptobyte   10  (*Builder).AddASN1* bodies
    golang.org/x/net/idna             7  Option-returning closures
    golang.org/x/text/unicode/bidi    1  DefaultDirection
    golang.org/x/text/unicode/norm    1  (*Form).IsNormalString
    golang.org/x/net/http/httpproxy   1  (*Config).ProxyFunc's method value

They are the same two recipes (`Option func(*options)` and
`AddASN1(tag, func(*Builder))` are both bare func fields), but they are a
separate tranche: they are vendored copies, and this round's scope was the
47-capture remainder plus whatever the compiler bug underneath it exposed.

### Verified by running

    gt3 fixture (routing_tree.mx + mapping.mx, isolated):
      before: "cannot resolve the type of p" x2, did not compile
      after:  compiles, emits, links
    net/http + net probe, whole closure rebuilt by the fixed compiler:
      probe143 (r143): 43 -> 0 for net/http and net; 23 elsewhere (httpcommon 3,
        vendored 20) - that run is what turned the report into a census
      probe147 (r144, after every conversion): 0 errors, 0 captures, for the
        whole closure
    at1 fixture: invalid type assertion fails the build
    FIXPOINT: `tmp/mxc-r144 build -o tmp/mxc-r145 ./_mxc_stage4` produces a
      byte-identical binary (sha256 50a3a16f...), so the compiler with both
      fixes reproduces itself in one generation

### The vendored tranche, converted too

The census listed 20 sites in five vendored packages; they are converted, each
with the recipe its shape called for:

    x/crypto/cryptobyte  AddASN1(tag, func(*Builder)) bodies rewritten to the
                         callback-free BeginASN1/EndASN1 the tree already had
                         (10 sites); callContinuation's deferred closure became
                         the deferred CALL (*Builder).finishContinuation
    x/net/idna           Option func(*options) became an Option INTERFACE;
                         optionBool carries the boolean factories, and
                         optionValidateLabels / optionCheckJoiners /
                         optionBidiRule / optionValidateForRegistration /
                         optionMapForLookup carry the multi-statement ones
                         (in BOTH idna9.0.0.mx and idna10.0.0.mx)
    x/text/unicode/bidi  optionDefaultDirection
    x/text/unicode/norm  flushFunc interface with plainFlush adapting the three
                         stateless flushes and normalStringFlush carrying
                         (*int32) bp - a POINTER because the caller keeps
                         walking from where the flush left off, which is what
                         the closure mutated
    x/net/http/httpproxy ProxyFuncValue interface; (*Config).ProxyFunc returns
                         the preprocessed *config, and net/http's
                         envProxyFuncValue is that interface now

Two Moxie rules bit during the conversion and are worth recording, because both
are the compiler telling the truth:

  - a method that writes through a pointer FIELD counts as self-mutating, so
    `:=` inside a loop is refused ("move the loop scratch into a helper
    function"). normalStringFlush.flush hoists info/p/pe, and cryptobyte's
    addASN1Signed/AddASN1Uint64 are the same shape.
  - with that, the tree-wide census is ZERO: the compiler, net/http and its
    internal packages, net, log, and every vendored package the build closure
    reaches.


### Open item found this round: `AddASN1`'s continuation SEGVs

Writing the cryptobyte coverage turned up a runtime crash that is NOT the
closure rule and NOT this round's rewrite:

    // /tmp/fx2/main.mx
    func addSeven(c *cryptobyte.Builder) { c.AddUint8(7) }

    b := cryptobyte.NewBuilder(nil)
    b.AddASN1(asn1.INTEGER, addSeven)
    got, err := b.Bytes()          // -> panic: caught signal SIGSEGV

A plain NAMED function crashes exactly like a func literal, so it is not the
closure rule. It is the recover() bug above, and the chain is short:
`AddASN1` -> `addLengthPrefixed` -> `callContinuation`, whose deferred
`(*Builder).finishContinuation` calls recover() and writes state - the exact
shape the matrix shows dying. The pre-round-140 code had the same recover() in
a deferred CLOSURE with the same writes, so the callback API was crashing
before this round too; it went unnoticed because nothing in the tree calls it
any more. Every in-tree caller of the `AddASN1*` family now uses the inline
`BeginASN1`/`EndASN1` path, and
`tests/regressions/should_compile/asn1_builder_der.mx` covers that path byte
for byte (18 encodings, each compared against the DER the rules require).

So the continuation check comes back to that fixture once recover() is fixed,
and it is one more reason the recover() item is first.

### TOP OPEN ITEM: `recover()` corrupts the domain and it dies with SIGSEGV

Found while giving the cryptobyte builder the coverage it never had. It is NOT
the closure rule and NOT this round's rewrite - and it is the most serious thing
in this file, because it means **the HTTP server's handler-panic recovery does
not work**:

    recover() inside a deferred func literal that does nothing else:
        works
    recover() in a deferred function that carries or uses ANY state
    (arguments, a captured receiver, a call after it, a write):
        the deferred work runs, then the domain dies with
        "panic: runtime error at 0x0000000000000004: caught signal SIGSEGV"
        at exit - with no panic anywhere in the program, too.

The matrix, one binary per case, all built by mxc-r144 (native):

    defer func() { freefin(t) }()          no panic  -> SIGSEGV at exit
    defer func() { freefin(t) }()          panic     -> recovers, then SIGSEGV
    defer func(x *T) { x.fin() }(t)        no panic  -> SIGSEGV at exit
    defer func(x *T) { x.fin() }(t)        panic     -> recovers, then SIGSEGV
    defer t.fin()                          no panic  -> SIGSEGV at exit
    defer t.fin()                          panic     -> recovers, then SIGSEGV
    defer freefin(t)                       no panic  -> SIGSEGV at exit
    defer freefin(t)                       panic     -> recovers, then SIGSEGV
    defer func() { recover(); ... }()      no panic  -> clean, exit 0

(`fin`/`freefin` here only call recover() and print; no writes.)

The faulting address is always 4 or 5 - a jump through a pointer holding a
small integer - which points at `moxie_longjmp` resuming from a clobbered
`deferFrame.JumpPC`. The thing to look at first is the setjmp-like checkpoint
`ir_emit.mx` emits before every call in a function that has a defer frame
("leaq 1f(%rip), %rax; movq %rax, 8($1); xorq %rax, %rax; 1:") together with
the frame contract it writes into (`deferFrameLLVMType` in ir_defer.mx versus
the runtime's own GEP offsets in setupDeferFrame/destroyDeferFrame). The
`recover` call's second `ptr null` argument is the hidden closure-environment
parameter every Moxie function carries, not a mismatch.

Why it matters here: `(*conn).finishServe` (server.mx) and
`(*http2serverConn).finishHandler` (h2_bundle.mx) are the deferred calls that
recover a panicking handler, and round 139 moved recover() into those methods
(it used to sit in a deferred closure). Round 139 did not create the defect -
the state-carrying closure form is in the same matrix and dies the same way -
but it is the code path a panicking handler takes, so nothing recovers a
handler panic today. No test panics inside a handler, which is why the suite is
green.

Next round, in order:
  1. reduce to the smallest IR-level repro (a defer frame + one call + recover),
     dumping the emitted .ll;
  2. fix the checkpoint/frame contract in stage4 (and check `_recover`'s arity
     against the runtime's own signature - they disagree right now);
  3. mirror in legacy/ if the legacy path has the same shape;
  4. add a regression fixture that panics in a handler and asserts the server
     answers 500 instead of dying.

### Coverage gaps left by this round

- The idna/norm conversions are COMPILE-verified (phase 1b builds net/http, which
  pulls both) but not behaviour-verified: no fixture in the tree exercises idna
  profiles or norm.IsNormalString. A fixture is drafted and parked at
  `tmp/pending/idna_norm_hooks.mx` (Lookup vs Punycode vs Transitional, the
  leading-hyphen rejection, NFC/NFD IsNormalString both ways, NFC/NFD.Bytes).
  It was pulled from `tests/regressions/should_compile/` because its build did
  not finish before the gate chain reached phase 5, and an unverified fixture
  there can turn phase 5 red. It goes back in the moment it is seen to pass.
- cryptobyte's builder path IS behaviour-verified now
  (`tests/regressions/should_compile/asn1_builder_der.mx`, 18 encodings), but
  the callback API (AddASN1 with a continuation) is not - it dies in recover()
  (see the top item).

# The web app framework belongs in the STDLIB (owner decision, after round 140)

Stated plainly by the owner: *the web app framework is part of the stdlib, and
everything imports it, so there is one codebase that does the things for
everything.* Today it is copied per app, which is the wrong shape:

    smesh/web/static/          index.html, favicon, manifest.json, $runtime/
                               (a COPY of the shim modules), $sw/, and twelve
                               <bundle>-wasm-host.mjs loaders
    smesh/web/common/jsbridge  the Moxie side of the DOM/IDB/WS/SW bridge
    musiquay/web/common/jsbridge   the same thing, forked
    moxie/jsruntime/*.mjs      the JS side, already one copy - but served by
                               hand from each app's web/static/

So the target is:

1. The Moxie-side bridge becomes stdlib packages, one per browser surface, next
   to the other `src/...` packages: `dom`, `idb`, `ws`, `sw`, `store`,
   `crypto`/`subtle`, `webgl`, plus an `app` package that owns the entry point,
   routing and asset metadata. Import path shape: whatever the tree settles on
   for stdlib (`git.smesh.lol/moxie/...`).
2. The JS side stays `jsruntime/*.mjs` (it is already single-copy) and gains the
   host scaffolding that every app currently re-copies: the loader, the service
   worker, the `$runtime`/`$sw` staging, and the wasm-host module template.
   Those should be emitted/served BY the build (`moxie build -target wasm`)
   rather than assembled per app.
3. `smesh` and `musiquay` delete their copies and import the stdlib. The proof
   is smesh: it must build and run with no local `web/common/jsbridge` and no
   local `$runtime`/`$sw`/`*-wasm-host.mjs`.
4. A stdlib-hosted fixture in the suite (not an app): a wasm program that uses
   the stdlib bridge, built by `moxie build -target wasm`, run in Chromium by
   phase 5 through the stdlib-emitted host - the framework's own end-to-end test
   rather than "an app somewhere runs".

Order of work, with the recover removal first because it is a prerequisite in
the same area and it is small:

  a. Remove recover() tree-wide (19 call sites, listed in the round-140 entry),
     make the builtin a COMPILE ERROR so it cannot come back, delete the
     runtime's `_recover` and the `Recover` landing-pad branch, and replace the
     handler-panic tests with one that asserts a panicking handler kills the
     domain loudly. The SIGSEGV in the round-140 top item dies with the feature.
  b. Inventory smesh's `web/common/jsbridge` and `web/wasm/*` imports; that list
     IS the framework's package set, measured rather than invented.
  c. Move it into `src/`, keep the JS modules where they are, and add the
     host-scaffolding emission to `moxie build -target wasm`.
  d. Point smesh at the stdlib, delete its copies, run it in a browser.
  e. Then musiquay, then the fixture in step 4.

# Round 141: recover() is gone from the language

Owner decision, stated as a rule: *panic is death. the end. no way back. no
closure scope capture, no recover. exceptions are the devil.* Nobody writes
recover() in real Go code - error values are what recoverable failure is for,
and a panic means the code is broken and must be fixed. So the feature is
removed rather than repaired, and the SIGSEGV in the round-140 top item goes
with it: there is no path left to it.

**19 call sites removed**, every one of them a place where a failure was being
turned into a silent success path:

    net/http/server.mx        handler panic (finishServe) and the timeout
                              handler's panic channel - both plumbing branches
                              deleted, not just the recover
    net/http/h2_bundle.mx     runHandler's panic RST + stack report, and
                              registerHTTPSProtocol's panic-to-error; the dead
                              http2handlerPanicRST frame and
                              http2errHandlerPanicked error went with them
    fmt/print.mx              catchPanic around Stringer/Formatter/Error methods
                              (and the `panicking` recursion flag)
    fmt/scan.mx, internal/dag/parse.mx   errorHandler: a parse panic is a bug
    debug/gosym/pclntab.mx    four swallow-and-reset helpers plus the
                              disableRecover flag: a corrupt pclntab now dies
                              loudly instead of parsing to nonsense
    iter/iter.mx              Pull/Pull2 no longer carry a panic value between
                              the coroutine and next()
    text/tabwriter, internal/runtime/exithook, testing, hmac   handlers removed
    vendor cryptobyte         finishContinuation no longer converts a panicking
                              continuation into a builder error
    pkg/syntax                Parse/parseWithRecover/traceEnd (the compiler's
                              OWN parser used recover to turn panics into parse
                              errors - the compiler build caught this, the
                              linker named runtime._recover first)

**The language side, so it cannot come back:**

  - `_mxc_stage4/ssa_builder.mx`: the `recover` builtin reports
    "recover() is not available in Moxie: a panic is fatal" and fails the build
    (ir_call.mx's emission branch and ir_defer.mx's `defer recover()` case are
    deleted with it; the recover doc block is gone from src/builtin/builtin.mx).
  - `legacy/compiler/compiler.go`: the same message from the same place
    (createBuiltin's "recover" case), and goroutine.go's silent `go recover()`
    exemption is gone. Wood law: both sides. NOTE: the legacy side cannot be
    COMPILED here (no Go toolchain, no libLLVM-22), so that mirror is written
    but unverified - record it, do not claim it.
  - `src/runtime/panic.mx`: `_recover` and `supportsRecover()` are deleted, and
    `panicOrGoexit` no longer longjmps: panic prints and aborts, and deferred
    calls do NOT run on the way out (they run only when their function returns
    normally). The defer frames, `_defer` records and `destroyDeferFrame`
    re-raise stay - ordinary deferred calls need the frames.

**The bake drift, found by doing this.** `sysroot/runtime_go.bc` is what APP
builds link, and it still carried `runtime._recover`/`supportsRecover` from an
older source: that is why fixtures could `recover()` while src/runtime's own
`supportsRecover()` said false. `mxc-r145 build-runtime` re-baked it (rc=0),
and the symbol is gone from the blob now. Any round that changes src/runtime
must re-bake in the same commit or apps keep the old runtime.

**Verified by running** (mxc-r145, native, against the re-baked runtime):

    recover() in a program        -> compile error: "recover() is not available
                                     in Moxie: a panic is fatal"
    panic("boom") with a defer    -> "body before panic", "panic: boom",
                                     Aborted (exit 134); the deferred call did
                                     NOT run
    same defer, normal return     -> body, deferred ran, exit 0
    compiler self-build           -> rc=0 with every pkg/syntax site removed

**Next, and it is the honest tail of this feature:** the compiler still emits
the setjmp-style checkpoint before every call in a frame-bearing function
(ir_emit.mx) and a landing pad that branches to the fn.Recover block
(ir_defer/ir_emit). With no recover and no longjmp nothing can ever arrive
there, so that asm and those blocks are dead weight - removing them is its own
change with its own verification, and it also removes the last consumer of
`moxie_longjmp`.

## Round 141, part 2: the framework inventory, measured

The Moxie side of the web bridge is a two-file split per package: a native stub
(`//:build !wasm`, every function `panic("jsbridge")`) and a wasm body
(`//:build wasm`) that declares `//:wasmimport bridge <fn>` and calls into the
matching `jsruntime/*.mjs` module. That shape is what moves into `src/web/`:

    smesh/web/common/jsbridge/dom          929 lines (288 stub + 638 wasm)
    smesh/web/common/jsbridge/callback     512   the callback registry: this is
                                                the owner-type pattern DOM
                                                events need, generic
    smesh/web/common/jsbridge/sw           486   service worker lifecycle
    smesh/web/common/jsbridge/idb          193   IndexedDB
    smesh/web/common/jsbridge/subtle       122   WebCrypto
    smesh/web/common/jsbridge/ws            73   WebSocket
    smesh/web/common/jsbridge/node          75
    smesh/web/common/jsbridge/localstorage  46
    smesh/web/common/jsbridge/markdown      24   rendering, not a browser API -
                                                it can stay downstream
    app-specific, stay downstream: feed, mlsw, notif, profile, relayproxy,
                                   schnorr, signer, store, verify, x25519, ext

The JS half is ALREADY single-copy in `moxie/jsruntime/*.mjs`; what is duplicated
is (a) these Moxie packages and (b) the `web/static/` host scaffolding
(`$runtime/`, `$sw/`, `index.html`, `<bundle>-wasm-host.mjs`, twelve of them in
smesh). So the move is: packages into `src/web/<name>/`, and the scaffolding
emitted by `moxie build -target wasm` (or a `moxie web scaffold` subcommand)
instead of copied per app.

Order that keeps the tree green:
  1. move `callback` first (dom depends on it), then `dom`, `idb`, `localstorage`,
     `ws`, `subtle`, `sw`, `node` - one commit, `src/web/...` import paths,
     native stubs and wasm bodies verbatim;
  2. a stdlib fixture that uses them, built for wasm and run by phase 5 through
     the stdlib host page;
  3. point smesh at `git.smesh.lol/moxie/web/...`, delete its copies, run it in a
     browser (its Makefile already takes `JSRUNTIME ?= ../moxie/jsruntime`);
  4. then musiquay;
  5. then the scaffolding emission, so `$runtime/` and `*-wasm-host.mjs` stop
     being per-app files.

# Round 142: the web framework's generic half is stdlib now

First slice of the owner decision (round 141, part 2). The Moxie side of the
browser bridge moved out of the app and into the stdlib, verbatim except for
import paths:

    src/web/callback     512 lines  the callback registry (wasm) + a native stub
    src/web/dom          929        elements, events, clipboard, files, history
    src/web/idb          193        IndexedDB
    src/web/sw           486        service worker lifecycle
    src/web/ws            73        WebSocket
    src/web/subtle       122        WebCrypto
    src/web/node          75
    src/web/localstorage  46
    src/crypto/aes256gcm  40        generic AES-256-GCM wrapper (was app-local
                                    under web/common/crypto)

Each keeps the tree's stub/wasm split (`//:build !wasm` stub, `//:build wasm`
body with `//:wasmimport bridge ...`). The JS half was already single-copy in
`jsruntime/*.mjs`, so nothing there moved.

Two things this found in the app-local copies:

  - `web/idb` had `limit int` in QueryDMs - `int` is UNDEFINED in Moxie, so
    smesh's own idb package cannot have been compiling for wasm. Fixed to int32
    in the stdlib copy; smesh gets the fix when it switches over.
  - `callback` was wasm-only (no native file at all), which would break any
    native build that imports the framework. It has a stub now
    (callback_native.mx), like every other package in the set.

Verified by running, both targets, with mxc-r145:

    wasm:  crypto/aes256gcm web/callback web/dom web/idb web/localstorage
           web/ws web/subtle web/sw web/node -> rc=0 (linked .wasm)
    native: the same set compiles with 0 compile errors; rc=1 is the linker's
           missing main.main, which is what a library package does

Next in this workstream: a stdlib fixture that uses the packages and is run in
Chromium by phase 5, then smesh switched over (delete its jsbridge copies), then
musiquay, then the host scaffolding emission ($runtime/, $sw/,
<bundle>-wasm-host.mjs) so those stop being per-app files.

## Round 142, part 2: what is still MISSING from the stdlib (the real gap)

Moving the packages exposed the actual shape of the framework, and the missing
piece is not the Moxie packages - it is the **page<->worker relay**.

`smesh/web/static/bridge-common.mjs` (358 lines) is a WORKER bootstrap: it
builds the wasm import object, and its own comment says the host "merges these
with its own specific functions (ext_*, dom_* ...)". The page holds
`jsruntime/dom.mjs` (direct `document.*`) and friends.

`moxie/jsruntime/host.mjs` implements `dom_body`, `dom_create_element`, ... by
calling `dom.*` DIRECTLY, and `jsruntime/loader.mjs` starts the root domain in a
Worker (`worker-host.mjs`). In a Worker there is no `document`, so a DOM call
from a Moxie module running under the stdlib loader cannot work as written. The
DOM path only works when the two sides are split and the calls are RELAYED:
worker posts a dom_* request, page executes it against `jsruntime/dom.mjs` and
posts the handle back.

So the next piece of this workstream is the relay, in the stdlib:

  1. `jsruntime/host.mjs`'s `dom_*` entries become postMessage relays when
     `is_worker_context()`, and direct calls on the page;
  2. the page side gains the matching request handler (a new
     `jsruntime/page-dom.mjs` or an extension of `jsruntime/app.mjs`), so a
     worker module's DOM calls reach the real document;
  3. `tests/wasm/` gains a page-side host that a fixture can use, which is what
     lets phase 5 run a DOM fixture at all (today's host.html is worker mode).

Evidence for each step is the parked fixture
`tmp/pending/web_dom_roundtrip.mx`: it builds for wasm today (rc=0) and drives
create/attribute/text/append/query/remove through web/dom. It was pulled from
tests/regressions/should_compile because the worker-mode test host cannot serve
it yet, and an unverified fixture there can turn phase 5 red.

## Round 143: the page <-> Worker DOM relay, in the stdlib

Round 142 named the missing half and guessed at its shape. This round built it,
and the shape is smaller than the guess: no new page-side file, no page setup at
all. Two new modules and one delegated variable.

**jsruntime/dom-ops.mjs** is the one description of what a DOM operation IS: a
table of 82 entries (the complete set of `//:wasmimport bridge dom_*` in
src/web/dom/dom_wasm.mx), each `{ kind, run }` where `run(a)` is the page
performing the operation against the real jsruntime/dom.mjs and `kind` is what
the answer carries (void / int32 / string / int64-as-decimal-text). Arguments
travel in wire order as one array, so the page executes exactly what the Worker
sent, and both halves of the boundary are built from this table.

**jsruntime/relay.mjs** is the boundary itself. A Worker holding the document's
port allocates one `SharedArrayBuffer` (16-byte header + 1 MiB payload), posts
`{relay, seq, op, args, sab}`, and `Atomics.wait`s on the gate. The page runs
`domOps[op].run(args)`, writes the answer, stores the gate and notifies. Every
request is answered - including the ones with no value - so a call that returns
has already happened on the page and DOM order is exact. A failed operation is
NOT a zero the module might mistake for an answer: the message goes in the
buffer and the Worker throws, which kills the domain loudly. The same for a
relayed operation whose answer overflows the buffer, for an op the two halves
disagree about, and for a Worker that waits `WAIT_MS` with no answer.

**host.mjs** picks the implementation once: `const D = opts.dom || dom`. On the
page D is jsruntime/dom.mjs; in a Worker it is the relay proxy, whose method
names are generated from the table. All 56 relayable `dom_*` bridge entries now
call `D.<Method>(plain values)`, so the bridge has one shape for both hosts, and
`startDomain` creates a `MessageChannel`, serves one end on the page and
transfers the other to the root Worker. The bridge needed the port threaded
through: worker-host's `instantiate()` passes `makeDomProxy(relayPort)` to
`buildWorkerBridge`.

**The 8 imports that made every web/dom module unloadable.** Host coverage was
measured, not assumed: `grep -o 'wasmimport bridge dom_[a-z_0-9]*'` lists 82,
and host.mjs had 74. A wasm module imports every symbol of a package it links,
not just the ones it calls, so `web/dom` could not INSTANTIATE at all:
`copy_image_to_clipboard encode_uri_component intercept_internal_links
location_assign release_all release_children window_open
write_primary_selection` had no bridge entry. All nine missing pieces (those
eight plus `encode_uri_component`, which was in neither list) are implemented
now, and the mismatch cannot come back silently: tests/wasm/validate.sh checks
the two lists against each other before the sweep, and checks that the relay
carries no operation the stdlib does not import. Measured after the change:
82 stdlib imports, 82 bridge entries, 82 relay ops, 0 stale, 0 missing.

**Three functions joined dom.mjs** because the stdlib declares them and nothing
had them: `EncodeURIComponent` (a value, not a URL - the browser owns the
rules), `WindowOpen` (always `noopener,noreferrer`: a module cannot hold a
window handle), and `InterceptInternalLinks` (same-origin anchors become
pushState + a callback; the version already in smesh's runtime copy).

**What the relay does NOT carry: the other direction.** 26 of the 82 ops need
the page to call *back* into the module (event listeners, rAF, timers, resize,
fetch, IDB reads, clipboard, file pickers, popstate, keydown). They are listed
in the table as `PENDING` rather than omitted, the proxy throws their names, and
the bridge states the same reason through `pageOnly(name)`. A page->module path
is the next slice, and its shape is already visible: the page must be able to
reach module exports (`__cb0`, `__cbs`, `__cb6i`) the way it now reaches
dom.mjs, and the Worker must be idle in its event loop to receive it - which is
the same "module returns from _start, the page drives it" model smesh already
runs (`wasm-app-worker-host.mjs` dispatches `__cb0` by postMessage).

**A spawned domain has no relay** - `spawnWorker` starts a Worker inside a
Worker, and the page is not a party to that, so the child cannot be given a port
to the page. DOM calls there fail by name ("this domain has no DOM relay")
instead of running against a document that does not exist. The fix is for the
page to hand out a port per spawned domain, or for the root domain to answer
requests on the page's behalf while it is not itself running - the second is
unsound (a parent blocked in `channel_recv` cannot answer), so it is the first.

**Proof status.** The page half and the Worker half were first run against each
other in Node (main thread serves, worker_thread holds only the port:
tmp/relay_preflight.mjs), which is where the Atomics protocol, the int/str/i64
decoding and all three error paths were checked before any wasm build, including
the fixture's own op sequence. The committed proof is the fixture moved back
into tests/regressions/should_compile/ (`web_dom_roundtrip.mx`): it drives
create / attribute / text / append / query / child / remove / encode /
now_seconds / viewport / path / release through `web/dom`, and every step
asserts what the document holds - a wrong answer panics, so a green browser run
is a positive claim about the round trip, not the absence of an error.

Measured, in Chromium, in the phase 5 sweep of this round's tree:

    browser stdout (tests/wasm/tmp/web_dom_roundtrip/wasm.clean):
      ok: body is handle 0
      ok: create_element returns a handle
      ok: attached element is in the document
      ok: attribute round trip
      ok: property round trip
      ok: selector finds the element
      ok: appended child is the first element child
      ok: child text round trip
      ok: removed child is detached
      ok: encode_uri_component round trip
      ok: now_seconds comes back as a positive int64
      ok: viewport width is readable
      ok: page path is readable
      ok: removed element left the document
      PASS: stdlib web framework round trip

    native.out:  panic: jsbridge        (the wasm-only package's stub, by design)

**One harness change was needed to place it there.** Natively `web/dom` is a
panic stub, so the native run cannot be part of a gate that requires a native
reference - but phase 0's `compile_and_run` requires the binary to RUN, and
`tests/regressions/should_compile` is also the phase 5 target list. A fixture
can now say `//:wasmtest browser-only` in its header:

  - phase 0 still builds it natively (that is what keeps the stubs compiling
    into an app) and skips only the run, reporting "native build; browser-only";
  - phase 5 runs the wasm module in Chromium, requires that run to exit clean,
    and takes it as the result instead of diffing it against a panic. It reports
    `web_dom_roundtrip PASS (browser only)`, verified by a focused re-run of the
    same harness.

The marker is opt-in and explicit: a fixture that claims it and then fails in
the browser still fails the phase, and nothing pattern-matches a real failure
away. `tests/phase5_wasm.sh` reads per-target results with
`^[a-z_0-9]+ +(PASS|FAIL|SKIP)`, which accepts a name only while it is
lowercase (every target is today, and all 76 were reported, so nothing was
being lost); the class is widened to `^[A-Za-z_0-9]+` so a future target with
an uppercase letter cannot vanish from the report while the phase still fails
or passes on the exit code underneath.

Gate for the whole tree at this point, `MOXIE=tmp/mxc-r145`:
phase 0 129/129, 0b restrictions ok, 1 packages ok, 1b 45 stdlib packages ok,
1c 3 wasm packages ok, 3 IR validate 4/4, 4 smoke 1/1, 6 SKIP (legacy compiler
older than its source - no Go toolchain here), 5 wasm 76 passed / 0 failed /
0 skipped.

Correction to round 142's plan: step 2 there proposed a new page-side host file
(`jsruntime/page-dom.mjs`) and step 3 a new test host page. Neither was needed.
The page handler lives in relay.mjs and is installed by `startDomain`, so a page
that loads an app gets the relay for free, and tests/wasm/host.html already
loads through loader.mjs -> startDomain, so it was always Worker mode.

### Round 143, the next slice: the other direction of the boundary

The 26 `PENDING` ops need the page to call into the module, and the two
lifetimes that have to change are already visible:

1. **The root Worker must outlive `_start`.** `worker-host.runRoot` posts
   `done` and `startDomain` then `terminate()`s the Worker, so after the module
   returns there is nobody left to receive an event. smesh's model is the one to
   copy: the module returns from `_start`, and from then on the page drives it
   through exports (`wasm-app-worker-host.mjs` dispatches `__cb0`, `__cbs`,
   `__cbbi` from its message handler). So `done` becomes "loaded", the Worker
   stays alive, and only an error or page teardown ends it. Phase 5 is
   unaffected: `loader.mjs` resolves on `done`, and each fixture run gets a
   fresh page.
2. **The relay port is the callback path too.** The page already holds `port1`
   for requests; the same port carries `{cb, kind, id, args}` back, and
   worker-host dispatches it to `instance.exports.__cb0/__cbs/__cbbi/__cb6i`
   (queueMicrotask, as the page-side bridge already does). Then the 26 ops move
   from `PENDING` to real entries: the page registers the listener/observer/
   fetch/timer with a page-side id, keeps a map page-callback -> module cbID,
   and forwards each fire over the port.
3. **Spawned domains** still need a page port of their own. The page cannot be
   reached from inside `spawnWorker` (the parent Worker creates the child), so
   the page must hand out one port per spawned domain, either at boot for a
   fixed pool or on request while the root domain is idle. Until then a spawned
   domain's DOM calls fail by name, which is what the relay does today.

## Round 144: the other direction - the page calls into the module

Round 143 carried requests one way. This round carries the answers back, which
is what the 26 `PENDING` operations were waiting for, and it took three changes
in the hosts plus one in the fixture model.

**The root Worker now outlives `_start`.** `worker-host.runRoot` still posts
`done` when the module returns, and `loader.mjs` still resolves on it, but
`startDomain` no longer terminates the root Worker: from that moment the page
owns the module and drives it through the relay. A spawned domain still
terminates when its entry function returns - it holds no port, so nothing can
call into it afterwards.

**The relay port carries callbacks back.** `serveRelay` builds the forwarders
the table asks for (`cb.void`, `cb.str`, `cb.bool`, `cb.i6`, `cb.ss`) and posts
`{cb, id, ...}` over the same port the requests arrive on; `worker-host` listens
on its end and calls `dispatchCallback`, which maps the kind onto the module's
own `web/callback` handle and queues the call (`__cb0`, `__cbs`, `__cbb`,
`__cb6i`, `__cbss`). The ids are the module's handles, so the code that
registered the callback is the code that runs, and each callback is a fresh
entry into wasm from a clean stack rather than from inside the Worker's message
handler.

**25 of the 26 operations are now real entries in the table**: timers, event
listeners (plain, self, enter-key, removal), resize observers, animation
frames, bounding rect, fetch text / relay info / blob PUT, IDB get and get-all,
service-worker messages, popstate, internal-link interception, clipboard read
and write, file pickers, paste/drop images, pull-to-refresh, copy-image. The
table also lost its second implementation: `host.mjs`'s 120 lines of hand-written
async bridge bodies and the `pageOnly` half-measure are gone, the page-side DOM
path (D = dom.mjs) is deleted because a module always runs in a Worker, and
`buildBridge` now says once, by name, that it needs a relay proxy instead of
failing per call with a TypeError. The 82 bridge entries are still the wasm ABI
and nothing more: pointers and lengths in, plain values to D.

**One operation has no answer path anywhere: `on_keydown`.** The module must
decide before the page can `preventDefault`, so the page would have to call into
the Worker and WAIT, and the page thread cannot block on a Worker. It fails by
name with that reason. smesh's own worker host has the same hole: it calls
`__hook_keydown` and drops the return value, so its OnKeydown cannot prevent
anything either. A real fix is an API change, not a host change - the module
would register a key policy up front and the page would apply it synchronously -
and it is not needed by anything in the tree today.

**Late failures are no longer swallowed.** With the root Worker alive after
`done`, a callback that panics has no caller left: `onSettled` belongs to a
promise that settled long ago. A domain error now goes to `console.error` as
well as to the caller, so a failing callback cannot look like success.

**Evidence, both directions, before any wasm build.** The Node pre-flight
(tmp/relay_preflight.mjs) runs the real table on a page-side thread and the real
proxy on a `worker_thread` over a real MessagePort and SharedArrayBuffer: 27
checks, including the fixture's exact sync sequence and eight callback kinds
produced by the page - a fired timer, a real `fetch` of a `data:` URL, an
animation frame, an element click, a bounding box, a fired ResizeObserver, a
dispatched popstate - each one arriving at the Worker with the right id and
payload, plus the two error paths (the named `on_keydown` refusal and an unknown
op).

**Then in Chromium, and the fixture now proves the callbacks too.** main ends by
asking for a timer; from there the chain is page-driven: timer -> `fetch_text`
of /jsruntime/dom-ops.mjs -> `get_bounding_rect` -> observe a resize after a
relayed style change -> `request_animation_frame` -> the PASS line. A callback
that never arrives leaves the output short, and the fixture's expected output is
now a file the gate compares against, because "the run was clean" cannot tell a
missing callback from a working one. Measured:

    web_dom_roundtrip                PASS  (browser only)
    diff tests/wasm/expected/web_dom_roundtrip.txt \
         tests/wasm/tmp/web_dom_roundtrip/wasm.clean   -> identical
      ... ok: set_timeout callback
      ok: fetch_text callback carries the file
      ok: get_bounding_rect callback reports a real box
      ok: observe_resize callback
      PASS: stdlib web framework round trip

**Three harness changes came with it**, each because the old shape could not see
the new thing:

  - a browser-only fixture states its expected output in
    `tests/wasm/expected/<name>.txt` and the gate diffs the browser run against
    it (a missing file is a FAIL, not a skip);
  - `tests/wasm/host.html` flushes complete stdout lines as they arrive. It used
    to accumulate one blob and push it when `load()` resolved, so every line a
    page-driven callback printed after that was written into a buffer nobody
    ever read;
  - `run.py`'s grace after `__done` went from 200ms to 600ms, because the last
    output of a passive module now arrives after the load promise settles.

**A finding from placing the fixture in phase 0's path: the `+`-on-text rule is
location-dependent.** The fixture's first version wrote
`panic("dom round trip: " + what)`, which phase 5 built and ran happily - and
phase 0 rejected with `'+' is not allowed for text concatenation, use |
operator`. Both are right, for different reasons:

  - the rule is a MAIN-MODULE rule. In the compiler
    (_mxc_stage4/ssa_builder.mx, the Add case, mirroring legacy's
    restrict.go/CheckPlusOnText) it fires only when `targetIsMainModule()` and
    the function belongs to the target package and that path is a user package
    path. A manifest-less target and a dependency package inside the moxie
    checkout both keep `+`;
  - phase 0 copies a fixture to `$TMPROOT/reg_<name>/pkg`
    (`$HOME/moxie-tests-$$/...`), which is outside the checkout, so the fixture
    IS the manifest-less target and the rule fires;
  - phase 5 copies it to `$ROOT/tests/wasm/tmp/<name>/pkg`, inside the moxie
    module, so it is a package of that module and the rule does not fire.

Measured, same 5-line program (`s := "a"; println(s + "x")`):

    inside the checkout   tests/wasm/tmp/probe144/pkg      -> rc=0
    checkout tmp/ dir     tmp/probe144/pkg                 -> rc=0
    sibling of checkout   ../probe144/pkg                  -> rc=1  (+ error)
    /tmp                  /tmp/probe144/pkg                -> rc=1  (+ error)
    both cache roots cleared, inside the checkout          -> rc=0  (not a cache)

So a fixture can pass phase 5 and fail phase 0 for a reason that has nothing to
do with the target: the two harnesses compile it in different places, and only
one of them is manifest-less. The rule itself is deliberate, but the
consequence is not: the language's own text-concatenation ban is enforced on
in-tree fixtures only by accident of where `compile_and_run` copies them. The
fixture now uses `|`, which is the language's operator in both places, and the
probe above is the evidence for the asymmetry. Worth deciding deliberately:
either build fixtures where the user-code rules apply (a manifest-less dir)
in BOTH harnesses, or accept that in-tree fixtures are exempt.

**Gate, measured, MOXIE=tmp/mxc-r145:**

    phase 0 (--quick)         130 passed / 0 failed / 0 skipped, rc=0
      regression:web_dom_roundtrip  PASS (native build; browser-only fixture
                                    runs in phase 5)
    phase 5, focused target   web_dom_roundtrip PASS (browser only)
      diff tests/wasm/expected/web_dom_roundtrip.txt \
           tests/wasm/tmp/web_dom_roundtrip/wasm.clean   -> identical
    phase 5, full sweep       76 passed / 0 failed / 0 skipped, rc=0
      wasm:web_dom_roundtrip   PASS ((browser only))

The full sweep is also the evidence for the three host changes that touch every
fixture rather than just this one: the root Worker outliving `_start`, the page
flushing stdout lines as they arrive, and the longer grace in run.py. All 76
targets still build, load, run and match their native reference.

The fixture's native build in phase 0 is the strict one: phase 0 copies to a
manifest-less temp dir, so the tree's `+` leniency does not apply there and the
fixture is compiled under the rules a user app gets.

## Round 145: the build stages the bundle's other half

The framework's JS half lives in jsruntime/ and is one copy, but until now only
a hand-copied app could use it: smesh keeps `$runtime/` (9 modules it picked),
`$sw/`, `bridge-common.mjs` and a `*-wasm-host.mjs` per bundle in its own tree,
and musiquay does the same. That is the drift (b) is about, and this round
removes it for the runtime and the page bootstrap.

**`moxie build -target wasm -o dir/app.wasm` now writes, beside the module:**

    dir/$runtime/*.mjs          the whole JS runtime, byte for byte
    dir/app-wasm-host.mjs       the page bootstrap for this bundle

The bootstrap is deliberately small - it pins the module URL and the loader
path, and everything else is the shared runtime:

    import { load } from './$runtime/loader.mjs';
    export const wasm = new URL('./app.wasm', import.meta.url).href;
    export function start() { return load(wasm); }

So a page needs one import and one call, and an app has no host files of its own
to keep in sync with the compiler that built it. The staging is done by
`emitWasmHost` in the stage4 compiler and mirrored in
`legacy/builder/wasmhost.go`, called from the same point in both link paths, so
a bundle built by either compiler is loadable by the same page. An output of
`/dev/null` stages nothing (that is a build that asked for no artifacts).

**Measured, in Chromium, through the emitted files only:**

    $ mxc-r146 build -target wasm -o tests/wasm/tmp/emit/web_dom_roundtrip.wasm ...
      host tests/wasm/tmp/emit/web_dom_roundtrip-wasm-host.mjs ($runtime: 35 modules)
    $ python3 tests/wasm/run.py -q --host /tmp/emit/web_dom_roundtrip-wasm-host.mjs
      ... all 14 sync assertions, then
      ok: set_timeout callback
      ok: fetch_text callback carries the file
      ok: get_bounding_rect callback reports a real box
      ok: observe_resize callback
      PASS: stdlib web framework round trip

The page imported only the emitted bootstrap; the loader, the Worker host, the
relay and every API module it used came from the emitted `$runtime/` copy, and
`tests/wasm/run.py` grew `--host <url>` (with `host.html?host=`) so the harness
can load a bundle's own bootstrap instead of the checkout's loader.

**The check is in the gate, not in this paragraph.** `tests/wasm/emit_check.sh`
builds the fixture, compares every staged module against `jsruntime/` with
`cmp`, and loads the emitted bootstrap in Chromium; `tests/phase5_wasm.sh`
reports it as `wasm:emitted-host`. It exists because validate.sh builds through
the checkout's own jsruntime/, so it cannot see whether the copy beside the
module is complete or loadable - a bundle that works in the checkout and 404s in
a deployed directory is exactly the failure this closes.

Still open in (b): the service worker staging. smesh's `$sw/wasm-host-sw.mjs` is
generic (it imports `$runtime/sw.mjs` and `$runtime/idb.mjs` and defines only the
service-worker lifecycle bridge), so it becomes `jsruntime/sw-host.mjs` plus the
registration glue, emitted to `$sw/` beside the runtime. It is deliberately left
for the round that switches smesh over, because that is where the emitted SW
gets exercised for real rather than merely written - and an emitted file with no
proof is the thing this workstream is trying to delete.

### Round 145, the legacy side, and a toolchain correction

The mirror is `legacy/builder/wasmhost.go`, called from the same place in the
legacy link job (after `link()`, before the size report) with the same
`/dev/null` rule. Wood law says legacy first: this was written second because
the stage4 version defined the emitted shape, and the ordering is recorded here
rather than glossed over.

**Earlier rounds recorded "no Go toolchain here". That was wrong.** Go 1.25.3 is
installed at `/home/mleku/go/bin` and is simply not on `PATH`:

    $ export PATH=$PATH:/home/mleku/go/bin
    $ go version
    go version go1.25.3 linux/amd64
    $ cd legacy && go build ./builder/
    (compiles; only LLVM deprecation warnings from cgo)

So a legacy change CAN be checked here: `gofmt -l` (clean for the new file, and
build.go's only formatting complaint is an import-order nit that predates this
round) and `go build ./builder/` type-check and compile the package the mirror
lives in. What still cannot be done is the legacy BINARY: the stack is pinned to
LLVM 22, `legacy/build.sh` refuses anything else, and this machine has LLVM 23
headers with `/usr/lib/llvm-20` and no `/usr/lib/llvm-22` - `go vet .` fails
inside vendored go-llvm on `C.LLVMBr`, a header mismatch unrelated to this
change. Phase 6 therefore still SKIPs, and that SKIP now means "the legacy
binary cannot be linked here", not "the legacy source is unchecked".

The emitted bytes are identical on both sides by construction: the same module
text (`wasmHostModule`) is a string literal in stage4's Moxie and in legacy's
Go. There is no test that compares them, and that is a real gap: the two
compilers can drift in wording without any gate noticing. Comparing the two
string literals is cheap and belongs with the next round's work.

**Known wart, fix with the next emitter change.** The bootstrap's module URL is
built from the output name with `.wasm` stripped and re-appended, so
`-o dir/app.wasm` is right but `-o dir/app` (no extension) points the page at
`dir/app.wasm`, which is not where the linker wrote the module. Every harness
here and smesh's Makefile pass `.wasm`, so nothing in the tree hits it, and the
page fails loudly on the fetch rather than silently - but the fix is two lines:
keep the output basename verbatim for the URL and strip the extension only for
the bootstrap's own name. It is deferred because it changes both emitters and
therefore needs a compiler rebuild plus a fresh gate, which is the same cycle
the service-worker staging needs; doing both in one cycle is cheaper than two.

**Gate, measured, MOXIE=tmp/mxc-r146 (the compiler rebuilt with the emission):**

    phase 0 (--quick)   130 passed / 0 failed / 0 skipped, rc=0
      regression:web_dom_roundtrip   PASS (native build; browser-only)
    phase 5              77 passed / 0 failed / 0 skipped, rc=0
      wasm:web_dom_roundtrip         PASS ((browser only))
      wasm:emitted-host              PASS (the build staged 35 runtime modules
                                     and a loadable bootstrap)

    $ mxc-r146 build -target wasm -o /dev/null <pkg>   -> rc=0, stages nothing

r146 was built by r145 with `-a` (bootstrap protocol). What is NOT done here:
the fixpoint check (r146 building an identical r147). It belongs in the round
that also carries the next emitter change, so one pair of rebuilds covers both;
until then the claim is "r146 compiles, runs both gates, and emits a loadable
bundle", not "the compiler reproduces itself at this revision".

## Round 146: the service worker's half, and three more missing entries

**(b) is nearly done: the build now stages the whole bundle.** Beside the module
and `$runtime/`, `moxie build -target wasm` writes:

    dir/$sw/wasm-host-sw.mjs   the service worker's host (jsruntime/sw-host.mjs)
    dir/sw-register.js         page-side registration glue

`jsruntime/sw-host.mjs` is the third execution context's half: no document, no
Worker, its own lifecycle. It is the Worker host plus the `sw_*` entries - the
WASI/env import object, the channel and spawn bridge, the string and callback
marshalling and the IndexedDB imports are all imported from host.mjs rather than
duplicated, and the DOM relay is passed a portless proxy so a DOM call from a
service worker fails by name. It reaches the runtime through `../$runtime/…`,
which resolves correctly both from `$sw/` and from inside `$runtime/` itself,
where the same file also lands (one staging rule, no special cases).

`jsruntime/sw-register.js` differs from the glue apps wrote by hand in one way
that matters: it derives the bundle's directory from `document.currentScript.src`
and registers with that as the scope, so a site at the root and a bundle under a
subdirectory both work. The hand-written version hardcodes `/$sw/…` and scope
`/`, which only ever works at a root.

**The `-o` wart from round 145 is fixed**: the module URL uses the output name
verbatim and only the bootstrap's own name drops `.wasm`, so `-o dir/app` now
points the page at `dir/app` instead of a file that was never written.

**web/idb was missing three entries in the host, exactly like web/dom was.**
`src/web/idb/idb_wasm.mx` declares 17 `idb_*` imports, including
`idb_kv_get`/`idb_kv_put`/`idb_kv_get_all`, and jsruntime/host.mjs provided 14:
a module linking web/idb could not instantiate at all. The three are implemented
now (`jsruntime/idb.mjs` gained the KV functions and the kv_profiles/kv_settings/
kv_cache stores the package documents; the entries live in a new
`buildIdbBridge()` so the service-worker host spreads the same object instead of
keeping a second copy).

**The coverage invariant is now checked for every namespace**, not just the DOM
one where it first bit: tests/wasm/validate.sh walks `dom`, `idb` and `sw`,
extracting the imports from each package's wasm half and the entries from the
host that serves it (host.mjs for dom and idb, sw-host.mjs for sw). Measured:
82/82, 17 imports against 21 entries (the extras are the dom package's own
idb wrappers), 39/39.

**The service worker's boot is proven, not assumed.** A fixture
(`web_sw_boot.mx`) registers an install hook and logs; emit_check.sh builds it to
`$sw/sw.wasm`, and then a page loads the emitted bundle through the emitted
bootstrap, registers the worker through the emitted `sw-register.js`, and the
run must end with the worker `activated` AND the server having seen the worker
fetch `$sw/sw.wasm`. Run.py reports that on stderr so `-q` still prints only the
module's stdout, and it exits non-zero otherwise: a worker whose host or module
is broken must not be able to "activate successfully" by doing nothing.

**A framework bug the proof found, in the service-worker lifecycle.** The first
boot attempt logged `sw fixture: hooks registered` and then sat in `activating`
until the page gave up. sw.mjs's eager listeners (they must be registered during
script evaluation, for Firefox) create a `waitUntil` promise for any install or
activate event that arrives before the module registers a handler, and hand the
resolver to `WaitUntil` when it does. Nothing ever resolved that promise if the
module had no hook for that event, so:

    [swdbg] install event handler=false      <- queued with an unresolved waitUntil
    sw fixture: hooks registered             <- module ran and registered OnInstall
    [swdbg] ModuleReady installQ=0 activateQ=0
    [swdbg] activate event handler=false     <- arrived AFTER the module was ready
    state=activating ... forever

Measured, not guessed: the logging above is what the service worker's own
console said. Two changes fix it, and they are the correct semantics rather than
a timeout: an event handed to the module is released on the next turn unless the
module claimed it by calling `WaitUntil` (`handOver`), and once the host has
called `sw.ModuleReady()` an event with no handler is simply not extended - a
worker whose module only serves fetch and message is normal, and the browser
must be allowed to finish install and activate. Measured after: `state=activated`
with `module_fetched=true`.

The page-side wait had the same kind of error and got the same treatment:
`navigator.serviceWorker.ready` is scoped to the *document*, and in this harness
the page lives outside the worker's scope, so it can never resolve no matter how
healthy the worker is. The check polls `getRegistrations()` against a deadline
and reports the state it reached.

**The idb entries are proven by running them, not by grepping.** A grep can see
that host.mjs mentions `idb_kv_get`; it cannot see whether the object the bridge
actually returns contains it, which is exactly what a mis-placed spread would
break. `web_idb_kv.mx` opens the database, puts a value, reads it back, walks the
store with get-all and re-reads a key, all through callbacks, and the gate diffs
the browser output against tests/wasm/expected/web_idb_kv.txt. Measured:

    web_idb_kv                       PASS  (browser only)
      ok: idb opened
      ok: kv get returns what kv put stored
      ok: kv pair greeting = hello kv
      ok: kv pair second = two
      ok: second key survived the walk
      PASS: stdlib idb kv round trip

The get-all walk is what makes this more than a put/get smoke test: its two pair
lines come from the each-callback, so a `cbss` that silently did nothing would
leave them out and fail the diff.

**Gate, measured, MOXIE=tmp/mxc-r147 (the compiler that also stages $sw/), with
nothing else building at the same time:**

    phase 0 (--quick)   132 passed / 0 failed / 0 skipped, rc=0
      regression:web_dom_roundtrip   PASS (native build; browser-only)
      regression:web_idb_kv          PASS (native build; browser-only)
      regression:web_sw_boot         PASS (native build; browser-only; phase 5
                                     skips it - a service-worker module has no
                                     meaning in the page/Worker host)
    phase 5              78 passed / 0 failed / 0 skipped, rc=0
      wasm:web_dom_roundtrip         PASS ((browser only))
      wasm:web_idb_kv                PASS ((browser only))
      wasm:emitted-host              PASS (the build staged 36 runtime modules
                                     and a loadable bootstrap, and the emitted
                                     service worker booted)

    fixpoint: mxc-r147 == mxc-r148 (sha256 615ee4a9...) - a generation that
    reproduces itself byte for byte, which is the strongest bootstrap claim and
    the one round 145 deferred.

    $ mxc-r147 build -target wasm -o dir/app <pkg>    -> module at dir/app and
      the bootstrap says new URL('./app', import.meta.url): the -o wart is gone,
      checked with an output that has no .wasm extension.

**A process note with evidence, because it cost a full sweep.** Two focused
validate.sh runs were started while a full phase-5 sweep was already running.
validate.sh wipes the shared build cache at its start, so the sweep's later
targets lost their cache mid-flight and `array_elem_addr` failed with "wasm
build failed" - a target that had passed in every earlier sweep and passed again
in the clean one. The rule recorded in round 140 ("no other builds while a gate
runs") is not only about edits: a focused run IS a build, and it carries the
cache wipe with it. The clean re-run above is the one to believe; the failed
sweep is not evidence of a regression, and this paragraph is why that is
knowable rather than guessed.

## Round 147: what a real app bundle needs that the framework did not have

The switchover started where it has to start: point smesh at the stdlib and
build it. The import rewrite is mechanical and done (36 files, 46 import lines,
for the eight packages that moved: callback, dom, idb, localstorage, node,
subtle, sw, ws). Then the compiler answered a different question than expected.

**Measured, every bundle, with mxc-r147:**

    bundle        rc   first blocker
    store          0   -
    sw             1   main: package-level var with initializer is not allowed
    notif/profile/feed/relay-proxy/verify/app
                   1   git.smesh.lol/smesh/web/common/nostr:
                       parseStrArray: unnamed return values are not allowed
    signer         1   web/common/jsbridge/schnorr:
                       PubKeyFromSecKey: unnamed return values are not allowed

So `store.wasm` - a real smesh bundle - builds against the stdlib today, and the
build staged its whole bundle beside it ($runtime/, $sw/, sw-register.js,
store-wasm-host.mjs). Everything else is blocked on APP-side debt, not on the
framework: unnamed return values, named slice types (`type Tag []string`,
`type Tags []Tag`), and a package-level var with an initializer. Those are rules
the current compiler enforces and the app predates, and they are the app's to
port - the same debt the app's own Makefile already declares for closure
captures.

**The framework gap the store bundle exposed, and the fix.** `store.wasm` links
`web/common/jsbridge/store`, which imports five bridge symbols the stdlib host
has never heard of: `store_worker_on_message`, `store_worker_post`,
`store_worker_set_timeout`, `store_worker_clear_timeout`,
`store_worker_now_seconds`. A web application always needs some of its own - a
store worker's message loop, an extension's storage, a protocol shim - and they
cannot be handed from the page to the Worker, because a function is not
structured cloneable. Until now the answer was a hand-written per-bundle host
file, which is exactly the copy this workstream exists to delete.

The fix is one option on the loader and one import in the Worker host:

    load('./app.wasm', { appBridge: './app-bridge.mjs' })

The Worker imports that module (same origin, so the URL is all that crosses) and
merges its default export over the stdlib bridge. Hosts stay framework; bridges
stay app; a bundle with no extras calls load() exactly as before. Proven with a
fixture that imports a symbol no stdlib host provides and prints what the bridge
computed - the value crosses wasm -> bridge module -> wasm:

    the app bridge answered an import the host does not provide: probe: 42

**What the store bundle still needs, and it is the next framework piece.** A
bundle that is a *worker server* - smesh's store answers JSON-array requests from
the page - needs the page to hold that Worker: to post it messages and receive
its replies. Today `startDomain` creates the worker for the root domain and keeps
the handle to itself (the page only sees stdout/stderr/done/error), so an app
cannot talk to a second bundle at all. smesh's 1379-line page host exists largely
to do this by hand for eight bundles. The shape of the fix is clear and belongs
with the routing package the goal names: a page-side `startBundle(wasm, opts)`
that returns the Worker (or a port pair), with the same relay and appBridge
plumbing the root domain already has. That is the next round's work, and the
store port rides on it so the port can be proven end to end rather than written
and hoped for.

**Where that fixture lives, and why it is not a regression.** The app-bridge
proof imports a symbol no host provides, so it cannot LINK natively - a wasm
import has nothing to resolve against off target - and the regression directory
requires a native build (its markers can skip a native RUN, not a native build).
It sits in tests/wasm/fixtures/app_bridge/ instead, where only the harness that
can host it looks at it: emit_check.sh builds it for wasm, runs it with
`--app /app-bridge-probe.mjs`, and diffs the output. The lesson is the marker
pair's: `//:wasmtest browser-only` plus `//:wasmtest skip` still left phase 0
compiling a file that cannot compile, which is what the first phase-0 run after
adding it reported (`ld.lld-22: error: undefined symbol: main.wasmProbe`).

## Round 148: the page's handle on a second bundle

Round 147's app bridge gave a bundle a way to answer imports only its app has.
It was not enough for a bundle that is a *server*: smesh's store worker answers
JSON-array requests from the page, and the page had no way to reach it, because
`startDomain` keeps the Worker handle to itself (the page sees stdout, stderr,
done and error, nothing else). That is most of what smesh's 1379-line page host
does by hand for eight bundles.

    const store = startBundle('./store.wasm', { appBridge: './store-bridge.mjs' });
    store.port.onmessage = (e) => { ... };
    store.port.postMessage('...');
    await store.ready;

`startBundle` creates a MessageChannel, starts the bundle as its own Worker with
the same relay and appBridge plumbing a root bundle gets, and returns the Worker,
the page's port, and a ready promise. The bundle's bridge factory receives the
port (`factory({ port })`), which is how a page message becomes a module
callback: the bridge may import the host's own marshalling helpers
(readStr/cbs/writeStr) because the host answers the module's imports and the
bridge decides what they mean.

**Proven in both directions, in Chromium, through the runtime staged beside the
bundle.** The fixture posts first so the page knows it is up; the page answers
that message; the module prints what came back:

    bundle: got="hello-from-module"          (what the page received)
    expected/stdout for the module:
      module got: hello-from-page
      PASS: bundle port round trip

A missing port, a dropped message, or a callback that never fires all leave the
expected output short, so this is a positive claim like the other three checks
in emit_check.sh - which is now four: the emitted bootstrap runs the DOM fixture,
the emitted service worker activates having fetched its module, the app bridge
answers an import no host provides, and a second bundle answers the page.

**Gate, measured, MOXIE=tmp/mxc-r147:**

    phase 5   78 passed / 0 failed / 0 skipped, rc=0
      wasm:emitted-host   PASS (all four checks: emitted bootstrap + DOM relay,
                          service worker activate + module fetch, app bridge,
                          second bundle answering the page)

The sweep matters here because round 148 changed the shared host path every
fixture loads through (`startDomain`'s boot message now can carry an application
port, and the Worker host takes one more argument). All 77 targets still build,
load, run and match their native reference, and the check that exercises the new
path passes inside the same run.

## Round 149: the store bundle answers the page

The first real app bundle runs on the stdlib framework. smesh's store worker -
which owns the app's IndexedDB, answers JSON-array requests and is what the feed
and profile bundles talk to - was built against the stdlib (round 147) and now
talks to a page through `startBundle`:

    store bundle said: {"replies": ["SR_READY","SR_KV_GET","SR_KV_GETALL_ITEM",
                        "SR_KV_GETALL_DONE"],
                        "kv": "hello-from-page",
                        "kvAll": [["probe","hello-from-page"]], "ok": true}
    PASS: the store bundle answers the page through the stdlib framework

Both directions are proven: the page received the worker's ready reply and its
KV answers, and the module received the page's requests. The app's half is now
`web/static/store-bridge.mjs` (the five `store_worker_*` imports and nothing
else, ~40 lines) plus `scripts/store-bundle-test.py` in the app repo; the host
that used to carry them, `store-wasm-host.mjs`, is no longer needed by this
bundle.

**Three fixes came out of making it real**, each found by running:

  - a relative `appBridge` (and a relative module path) given to `startBundle`
    was resolved by the WORKER, so it asked for `/$runtime/store-bridge.mjs` and
    then for `/$runtime/store.wasm` (the second failing as `expected magic word
    00 61 73 6d, found 3c 21 44 4f` - it had fetched the 404 page). Both are
    resolved against the page now, where the caller's relative path means what it
    says: beside the bundle;
  - the store opens IndexedDB asynchronously and posts `SR_READY` when it is
    ready, which is LATER than `startBundle`'s ready promise (that resolves when
    main returns). The proof now waits for the app's own ready signal - the first
    version sent a put before it and the value vanished, which is the app's
    contract being raced rather than a framework bug;
  - **the DOM fixture was flaky, and the gate would have failed intermittently.**
    Its ResizeObserver callback is registered for good, so a second layout
    notification ran the whole callback tail again and printed the PASS line
    twice - measured in the emitted-bundle check as duplicated output. The
    callback now unobserves first: one notification, one chain, one PASS line.
    That is a gate bug fixed before it could waste a sweep, and it is only
    visible because the expected-output comparison exists.

Gate: wasm:emitted-host green with all four checks after the fix; the full phase
5 sweep for this tree is the next round's first action, and the store proof is
smesh's own script (`python3 scripts/store-bundle-test.py --dir <built bundle>`).

**Round 149's gate, measured** (MOXIE=tmp/mxc-r147): phase 5 **78 passed / 0
failed / 0 skipped, rc=0**, with `wasm:emitted-host` covering all four checks -
the emitted bootstrap and DOM relay, the service worker activating having fetched
its module, the app bridge, and the second bundle answering the page. The sweep
is what closes round 149: the path-resolution change touches the shared loader,
and the DOM fixture's callback chain had just been made single-shot after the
expected-output comparison caught it printing its PASS line twice.

### Round 150: the app-side port, measured (handoff state)

The framework work is done and gated; what remains of (c) is smesh's own port,
and three rounds of measuring it produced these exact facts, so the next round
starts from numbers rather than a guess:

  - `web/common/nostr/parse.mx`: the five unnamed returns are fixed
    (parseStrArray, parseKindArray, parseString, parseInt, parseTags), which took
    the package from 7 compiler errors to 2. Committed in the app repo.
  - the two that remain are `type Tag []string` and `type Tags []Tag`, and their
    removal is NOT local: 253 references across 44 files, including
    web/wasm/app/main.mx, feed, mls, notif and donation. It needs to be done in
    slices that each compile, smallest first (nostr's own signatures, then notif,
    then outward), committing each slice with its measured error count.
  - `web/wasm/sw/main.mx` has one `var staticFiles = []string{...}` with 7
    references, and BOTH obvious shapes are rejected: `init()` is refused in
    package main ("use main() for program entry"), and assigning the package
    global from main() is refused ("package globals are immutable outside init:
    mutate through the methods of a sovereign self-mutating type"). A regex
    rewrite to a function did not parse, so it has to be done by hand against the
    seven call sites - the sovereign-holder shape the compiler names is the one
    to use. The file was reverted after the attempt, so the bundle fails only on
    its original rule and nothing is half-converted.
  - the store pattern is the template for the rest: an app bridge module for the
    module's own imports plus `startBundle` for the page port, proven by
    smesh/scripts/store-bundle-test.py (round 149).

Order for the port: nostr's Tag/Tags -> notif (smallest dependent bundle) ->
schnorr's unnamed returns -> sw/main.mx's staticFiles -> feed/profile/verify/app
on the store pattern -> smesh's page (which is where wasm-host.mjs,
*-wasm-host.mjs, $runtime and $sw finally leave the app tree) -> musiquay.

**Round 151 additions to the port list.** `jsbridge/ext`'s package-level
initializer is fixed with zero-value + `init()` (a LIBRARY package may have
`init()`; package main may not, which is why `sw/main.mx` needed a different
shape for the same rule). `nip44`'s three unnamed returns are named. `sw/main.mx`
now uses a self-mutating holder for its precache list and **./web/wasm/sw builds
(rc=0)** - the second bundle against the stdlib after store, and the first whose
runtime use depends on the emitted `$sw/`.

The signer bundle now walks all the way to `web/common/nostr` and stops only on
`type Tag []string` / `type Tags []Tag`: those 253 references across 44 files are
the last thing between three bundles and compiling.

**A finding that explains why the app's port is app-shaped.** `grep '^type X []'`
finds named slice types in the STDLIB too (`src/builtin/builtin.mx`,
`src/net/ipsock.mx`) and the stdlib compiles - phase 1b builds 45 packages with
mxc-r147. The rule that rejects smesh's `type Tag []string` therefore does not
reach stdlib packages, which is the same location-dependence round 145 measured
for the `+` operator: the strict rules are a MAIN-MODULE rule, so a dependency
package inside the checkout keeps what a manifest-less or main-module target is
refused. It does not change the port - smesh IS a main module, so it must comply -
but it does mean the compiler's rule boundary is worth stating plainly somewhere:
"what the compiler accepts" depends on where the package lives, and that is why
the same tree can hold both a rejected `type Tag []string` and an accepted
`type ... []...` in the standard library.

## CORRECTION (owner decision): the Tag plan is structs, not holders-around-slices

The previous rounds' plan said "struct holders carrying `list []string` /
`list [][]string`" and left the singular `Tag` as an open question. That is now
decided, and the plan for `web/common/nostr/tags.mx` is:

    type Tag struct {
        Parts []string      // was: type Tag []string
    }

    type Tags struct {
        List []Tag          // was: type Tags []Tag
    }

Field names are EXPORTED (`Parts`, `List`), so the rest of the app reads and
writes them directly instead of going through accessors.

`[]string` is the spelling to use, not `[][]byte`: in Moxie they are the same
type, and `[]string` reads as what a tag is - a sequence of text parts.

What follows from it, for the sweep of the 194 non-test references:

  - the eight methods (`Tag.Key/Value/Relay/Marker`,
    `Tags.GetFirst/GetAll/GetD/ContainsValue`) move onto the structs unchanged in
    signature, which keeps the 91 `Tags.` call sites intact - that is the point of
    the shape;
  - literals change: `Tag{"p", "abc"}` becomes `Tag{Parts: []string{"p", "abc"}}`,
    so add small constructors (`NewTag("p", "abc")`, `NewTags(...)`) for the
    sites that read better that way and spell the literal where it is already
    clear; no accessors are needed, the fields are exported;
  - **check first, do not assume:** 37 sites compare with `==` / `!=`. Moxie lets
    slices compare with `==`, but whether a struct CONTAINING a slice is
    comparable is not something this session measured. The first slice of the
    work is to find out - if the struct is not comparable, those sites need an
    `Equal` method (or compare the `List` field), and that is a piece of the
    sweep, not a surprise in the middle of it;
  - then rebuild ./web/wasm/notif as the checkpoint and commit with the measured
    error count, as rounds 12/16/17/18 did.
