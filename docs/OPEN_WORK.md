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
   stage4 does not (table below). Implement them in stage4, one commit each,
   each with a fixture pair under `tests/data/` and a `phase6` check, so both
   compilers reject the same programs. This is a bootstrap hazard, not a
   cosmetic gap: legacy builds stage4 first, so stage4-only shapes break the
   next bootstrap.
3. **`moxie test` never runs the package under test's `init()`.** Proven:
   a package whose `init()` sets a scalar global reads 0 in its own test
   (`scalar global = 0, want 42`). Fix the harness so init runs, or document
   the lazy-ensure workaround as the rule.
4. **Remove modules** (design below), **with versioning carried on the import
   path itself** (`import "git.smesh.lol/nostr/pkg/event@v1.2.3"`). Delete
   `moxie.mod` handling from both compilers, resolve imports only through
   `MOXIEPATH` + `$MOXIEROOT/src` (+ vendor), and replace version pinning with
   git refs plus per-ref worktrees and a waiting compile lock. Delete the
   `moxie.mod` files from every repo and the `replace`/`require` lines from the
   Makefiles. **`moxie get <path>[@<ref>]`** is the companion command: fetch,
   place at `$MOXIEPATH/<repo-path>`, check out the tag/branch/commit, pin into
   `./moxie.lock`, and take a real (waiting) lock instead of dying on
   contention. It can land before the removal, since it is just today's
   auto-fetch with a ref argument and an exposed front door.
5. **VS Code / VSCodium: full Moxie language support.** Build the extension,
   install it into the local Codium, and keep the source in a folder in the
   moxie repo. Scope: TextMate grammar + `language-configuration.json`, the
   `pkg/token`/`pkg/syntax` keyword and operator sets as the source of truth,
   snippets for the shapes Moxie requires (named returns, pointer receivers,
   `init()` + zero-value globals, root-arena ensure), and a language server
   (diagnostics from `moxie build`'s stderr, completion, hover, go-to-definition
   over `pkg/syntax`). No compiler subcommand exists for it yet; `moxie help`
   lists build/run/test/clean/targets/info/list/header/install/version/env/
   fetch/cache/update/build-runtime.
6. **Wire `musiquay/pkg/protocol` into the running system.** Relay side:
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

Same program, ordinary user module (`example.com/app`), built with both:

| program | legacy | stage4 |
|---|---|---|
| `var Version = []string{"1"}` | reject | **accept** |
| `type Names []string` | reject | **accept** |
| `type M map[string]string` | reject | **accept** |
| `println("a" + "b")` | reject | **accept** |
| unnamed return values | reject | **accept** |
| `(f)()` parenthesised call target | reject | **accept** |
| closure capture without `-allow-closure-captures` | reject | reject |
| `init()` in a main package | reject | reject |

Legacy's messages: `moxie: package-level var with initializer is not allowed:
use zero-value declaration and init()`, `... named slice type 'Names' is not
allowed: use []T directly`, `... named map type 'M' is not allowed: use map[K]V
directly`, `... '+' is not allowed for text concatenation, use | operator`,
`... unnamed return values are not allowed: name all return values`,
`... parenthesized call target is not allowed: write f(x), not (f)(x)`.

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
3. **Version selection** needs a home once `require` is gone. It is the
   **import path** — see "Versioning on imports" below — with a flat pin file
   at the build root (`moxie.lock`) recording the commit each ref resolved to.
   Absent a ref and a pin, a repo is "whatever is checked out".
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
the `replace` lines in the Makefiles. `tests/data/badreplace` and phase6 `6s`
go with it; the other fixtures (`maplit`, `untypedvar`, `pushnil`, `aeaddep`)
do not use manifests.

### Versioning on imports

Without modules there is no manifest to hold a version, so the **version lives
on the import path**, the way the old tooling did it:

```
import "git.smesh.lol/nostr/pkg/event@v1.2.3"
```

- The **last** `@` splits the ref (same rule `moxie get` uses). The ref may be a
  semver tag, a branch, or a commit hash.
- **Resolution**: strip `@ref` to get the repo (`git.smesh.lol/nostr`) and the
  package (`pkg/event`); materialise that ref at
  `$MOXIEPATH/.refs/<repo>/<ref>` with `git worktree add` from the main
  checkout, which doubles as the mirror; load `<worktree>/pkg/event`. No ref:
  `$MOXIEPATH/<repo-path>`, today's rule. `moxie get <path>@<ref>` performs
  exactly this materialisation, so a build and a hand-fetch cannot disagree.
- **The ref is part of package identity.** `event@v1.2.3` and `event@v2.0.0`
  are two packages in one build — distinct import path, distinct registry key,
  distinct symbols. Two versions coexisting is the entire point of putting the
  version on the import; dropping the ref from identity would make the second
  import silently alias the first. Because the discriminator is the `@ref`
  suffix, there is no Go-style `/v2` path element to maintain.
- **Per import site, no graph.** Each import names its own ref. There is no
  minimum-version selection, no constraint solving, no transitive requirement:
  a repo imported at two refs simply gets two worktrees and links both. `moxie
  env` (or `moxie list`) reports each repo's refs and their worktree paths, so
  "why is it building that version" has an answer; a two-ref build is legal and
  reported, not an error.
- **The lock pins commits, not refs.** A tag can be moved, so `moxie.lock`
  records `<repo> <ref> <commit>`; `-offline` and a fresh `MOXIEPATH` reproduce
  the tagged commit from the pin. A ref with no tag (a branch or a commit)
  records the commit it resolved to at fetch time.
- **Toolchain imports stay unversioned.** `git.smesh.lol/moxie/pkg/...`
  resolves once against `$MOXIEROOT`: the compiler cannot import a revision of
  itself other than the one it is built from.
- **Repo boundary is unchanged**: the first two components of a dotted import
  path (`git.smesh.lol/nostr` from `git.smesh.lol/nostr/pkg/event@v1.2.3`),
  found by walking up to `.git` once the ref is stripped.

Consequence for the resolution order: the ref is stripped before every step,
and the stripped path is looked up at the worktree for that ref when one is
pinned or materialised. A build never checks out a repo in place for a
versioned import — that would make two refs in one build impossible.

### `moxie get`

One command that fetches, places and checks out a repository, so nobody has to
know where the tree lives or clone it by hand. It is the explicit front door to
the same resolution step a build uses.

```
moxie get <import-path>[@<ref>] [<import-path>[@<ref>] ...]
moxie get                       # materialise every entry in ./moxie.lock
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
- **Pinning**: `-pin` records `<repo> <ref> <commit>` in `./moxie.lock` (current
  HEAD when no ref is given) — the commit is recorded so a moved tag still
  rebuilds the same bytes; `moxie get` with no arguments reads that file and
  materialises every entry. The file is a flat pin list, not a manifest: no
  module graph, no version solver. The same syntax is what an import path
  carries (`pkg/event@v1.2.3`), which is what makes the pin and the source
  agree by construction.
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
