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
2. **Close the legacy/stage4 restriction gap.** Four of six done — unnamed
   returns, parenthesised call targets and `+` on text are implemented,
   verified with both compilers and pinned by phase6 `6v`–`6x` (`ea7bb392`);
   value receivers were already there. **Two remain: package-level var
   initializers and named slice/map types.**

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
4. **Clear the latent parse errors and make a recovered parse fatal.** Three
   known defects, each a real bug that has been silent for as long as the parse
   errors were swallowed: the concatenator leaving an import line in the body
   when a file's first line after the package clause is a comment (proven fix:
   skip blank lines and leading comments, but never `//:` pragmas), the eight
   vestigial blank imports in the stdlib, and one `//:generate` pragma before an
   import. Details and verification in the finding above. Once these are gone,
   `typeCheckPkg` can fail on `len(cctx.parseErrors) > 0` instead of only on
   `file == nil`, and a partial tree never reaches the SSA builder.
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

   **Still to do:** remove `moxie.mod` resolution from stage4 and legacy, delete
   the files and the Makefile `replace`/`require` lines, and key the build cache
   on repo + resolved commit.
7. **Make the model rules universal; make the conflict rule strict.** The
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
   plus the publish paths.

### Open: the `+` text rule's scope without a module file

Removing `moxie.mod` took away the thing the `+`-on-text rule was scoped by.
Legacy gated it on `p.Module.Main` (the input package and the input module's
packages, never a dependency); stage4 gated it on `rootModPrefix !=
"git.smesh.lol/moxie"`, which before the removal was the target's module and is
now just "not the toolchain".

Two tests pin the two sides of the gap, and both are red in the current tree
(the full suite is 166/168):

- `plustext:legacy` - a directory target outside every module is no longer
  rejected, because no module file sets `Module.Main` for it.
- `aeaddep:stage4` - the broad stage4 gate rejects a `+` inside a *dependency*,
  which legacy never did.

The fix is one notion of "the target's own code" implemented in both compilers:
the package the build was asked for, plus the packages under the repository it
lives in. stage4 has the pieces (`bst.targetPkgPath` from the resolver's entry
whose `path` equals the requested target, `rootModPrefix` for the repository);
legacy marks the input package `Module.Main` even with no module file. Both were
prototyped and are half-landed; the missing part is identifying the target
package reliably in stage4's resolver order, which is not "last".

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
- Concurrent compiles collide on `~/.moxie/*.lock`-style repo locks and fail
  rather than wait; serialise builds that touch the same module cache.
- The relay serves the app with `ORLY_STATIC_DIR=<musiquay>/web/static`; the
  smoke suite starts its own relay, so a stray listener on the test port fails
  the fixture on purpose.
