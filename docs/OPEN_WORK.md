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
2. **Close the legacy/stage4 restriction gap.** Legacy enforces six rules
   stage4 does not (table below). Four are done — unnamed returns,
   parenthesised call targets and `+` on text are implemented, verified with
   both compilers and pinned by phase6 `6v`–`6x` (commit `ea7bb392`); value
   receivers were already there. **Two remain: package-level var initializers
   and named slice/map types.** Legacy gates those with `isUserPackageByPath`,
   which has no `/pkg/` clause, so enforcing them in stage4 rejects 39 sites
   across `nostr`/`morly`/`musiquay` (6+13+20 var initializers, 3+7+6 named
   types). They need the explicit exemption declaration (item 5) first, or a
   migration; until then stage4 keeps the `/pkg/`-exempting gate for these two.
   This is a bootstrap hazard, not a cosmetic gap: legacy builds stage4 first,
   so stage4-only shapes break the next bootstrap.
3. **`moxie test` never runs the package under test's `init()`.** Proven:
   a package whose `init()` sets a scalar global reads 0 in its own test
   (`scalar global = 0, want 42`). Fix the harness so init runs, or document
   the lazy-ensure workaround as the rule.
4. **Remove modules** (design below). Delete `moxie.mod` handling from both
   compilers, resolve imports only through `MOXIEPATH` + `$MOXIEROOT/src`
   (+ vendor), and move version pinning into a single mod-style **`MANIFEST`**
   file at the build root. Delete the `moxie.mod` files from every repo and the
   `replace`/`require` lines from the Makefiles. **`moxie get <path>[@<ref>]`**
   is the companion command: fetch, place at `$MOXIEPATH/<repo-path>`, check
   out the tag/branch/commit, record it in `MANIFEST`, and take a real
   (waiting) lock instead of dying on contention. It can land before the
   removal, since it is just today's auto-fetch with a ref argument and an
   exposed front door.
5. **Make the model rules universal; make the conflict rule strict.** The
   immutability guarantee is a model invariant, not a style preference, so it
   belongs on the package root everywhere — with the **runtime/unsafe carve-out
   only** (measured: 69 global stores in `src/runtime`, the layer that builds
   sovereign arenas; see the finding below). Concretely:
   (a) apply global immutability and the two decl rules to every package above
   the runtime, deleting the `/pkg/` carve-out — costs the compiler tree
   nothing, ~77 stdlib sites and 39 app-repo sites;
   (b) forbid **dot imports** (all 20 are the compiler's
   `. "git.smesh.lol/moxie/pkg/types"`; 3087 identifiers to qualify) or, if
   that price is refused, make a dot import's names participate in the conflict
   check instead of being invisible;
   (c) make import conflicts hard errors: two imports binding one name, and an
   import colliding with any declaration — currently import-vs-import keeps the
   first binding silently and stage4 dedupes the names so the second is not
   even seen.
6. **VS Code / VSCodium: full Moxie language support.** Build the extension,
   install it into the local Codium, and keep the source in a folder in the
   moxie repo. Scope: TextMate grammar + `language-configuration.json`, the
   `pkg/token`/`pkg/syntax` keyword and operator sets as the source of truth,
   snippets for the shapes Moxie requires (named returns, pointer receivers,
   `init()` + zero-value globals, root-arena ensure), and a language server
   (diagnostics from `moxie build`'s stderr, completion, hover, go-to-definition
   over `pkg/syntax`). No compiler subcommand exists for it yet; `moxie help`
   lists build/run/test/clean/targets/info/list/header/install/version/env/
   fetch/cache/update/build-runtime.
7. **Wire `musiquay/pkg/protocol` into the running system.** Relay side:
   storage/index/validation policy per kind, the `#x` blob -> asset index hosts
   need to gate, and the replaceable-range exception for 32218-32220. App side:
   adapters between these structs and `event.E` (native) / `core.Event` (wasm),
   plus the publish paths.

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
  one `DeclList` for a one-file package like `pkg/mxutil`. A conflict must
  therefore be keyed by **(name, path)** — two different paths binding one name
  — not by name alone, which reported "unsafe is imported twice" for a file
  that imports it once.
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
- Concurrent compiles collide on `~/.moxie/*.lock`-style repo locks and fail
  rather than wait; serialise builds that touch the same module cache.
- The relay serves the app with `ORLY_STATIC_DIR=<musiquay>/web/static`; the
  smoke suite starts its own relay, so a stray listener on the test port fails
  the fixture on purpose.
