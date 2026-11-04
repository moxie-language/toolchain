# Moxie Changelog

## v1.0.0-rc.0

The first release on the public toolchain repository (`moxielang.dev`, staged
at `github.com/moxie-language/toolchain`). Everything below the tag is the
compiler as it self-hosts to a reproducible binary.

- **The import root is `moxielang.dev`** - the toolchain's own packages import
  under the vanity domain, and the loader resolves it, so a toolchain module
  states its dependency by name rather than by checkout path.
- **A spawn target's result is the breaker, `chan []byte`** - `spawn(fn, ...)`
  returns the target's one declared result, and the reason a domain stopped
  (a panic message, empty on a clean stop) travels on that channel before it
  closes. The result is matched as Moxie text, so a signature read back from
  another package's export data (`[]byte` arrives as `string`) is the same
  contract.
- **`sigint` is the domain's termination channel** - `main` selects on
  `sigint`, a spawn target on `closed`. The word lowers to `__sigint()`, a
  builtin typed `chan []byte`; the runtime creates the channel once, in the
  root arena, and the signal handler only raises a flag. Every point where the
  domain waits closes it from normal context, so a select arm on it fires like
  an arm on any closed channel. A program that wants its own cleanup can still
  install the immediate handler.
- **Spawn channels are mailboxes with a declared message depth** - `chan T{}`
  is one slot, `chan T{:N}` is N outstanding messages, and `chan T{:count:bytes}`
  states the per-message frame budget (both fields may be run-time expressions,
  so relay policy read from config sizes its own rings). A frame that encodes
  past its budget panics with the actual size, the budget, and the
  compiler-generated list of `cap` tags that set it. `:0` is a compile error.
- **Frame budgets come from `cap` struct tags** - one number per
  variable-length dimension, counted in elements (bytes at a `string`/`[]byte`
  leaf), with one 4096-byte page for an unstated dimension.
- **Breaker lifecycle** - a spawn's breaker is created before the fork, tracked
  on the domain, closed at every return of the spawning function, on a panic,
  and by the kernel reaper when a parent dies without one.
- **The termination select is enforced** - both compilers refuse a `main` that
  does not consume `sigint` and a spawn target that does not consume its
  `closed` breaker, so a program that cannot be told to stop is a compile
  error rather than a kill. A receive counts as well as a select arm, because
  a single-arm select is lowered into one.
- **Value parameters are copied across spawn** - the whole value graph is
  encoded against the cap-tag budget, checked, and decoded into the root arena
  at the spawn call, so a child's slices and pointers never point into the
  parent's frames. Overrunning the budget panics naming the fields and their
  `cap` tags.
- **`moxie get` resolves import paths** through their own `?go-get=1`
  discovery document, so `moxie get moxielang.dev/pkg/mxutil` finds the module
  root and the repository with no knowledge of the host. `moxielang.dev` serves
  the discovery document, a browser 301 and git's smart-HTTP endpoints.
- **A domain is a thread, not a process** - `spawn` starts a pthread with its
  own stack, TLS domain root, arenas and package globals, instead of forking.
  There are no PIDs, no `pdeathsig`, no `waitpid` and no reaper; a closed
  breaker reaches the waiting loops, and the process ending takes every domain
  with it. Package variables are per-domain storage in both compilers, so no two
  domains share mutable state by accident, and the isolation is the capability
  contract rather than an MMU boundary.
- **A spawn channel skips the codec for a pointer-free element** - both ends are
  threads in one address space running one binary, so an element that holds no
  pointer, slice, string, map or interface crosses as its own bytes: a memcpy
  into the ring is the message. A type that DOES hold references stays on its
  codec (the decode is the relocation) or on the structural codec when it has
  none. Measured on a 12-byte struct, one-slot round trip: 95us -> 16us. The
  element predicate is checked first and counts a `string` as a reference, so a
  `chan string` across a spawn boundary is now encoded instead of handing the
  receiver a `{ptr,len}` pair into the sender's frame.
- **A `select` arm on a spawn channel carries its frame format** - the arm's
  state names the element's codec or the structural type descriptor, so the
  runtime encodes at the moment the arm fires. Before this the select path moved
  raw element bytes while a direct send encoded them, so a select receive of a
  struct carrying a `[]byte` arrived zeroed and a select send of one corrupted
  the ring ("codec expected struct tag", core dump). Encode and decode are now
  one shared pair (`codecFrameEncode`/`codecFrameDecode`) that every channel
  entry point - extent, codec, structural, select, native and wasm - routes
  through.
- **The package cache is bounded** - generations untouched for two hours are
  pruned on every build (`removeAll` had never deleted a non-empty tree, so
  they accumulated: 368 generations, 90 GB).
- **A cache key never depends on measurement state** - export data is named by
  its sources and its imports' export keys (`pkgExportHash`), while the object
  key keeps the dispatch fingerprint, because dispatch arms are baked into the
  object. Folding the fingerprint into the export name gave one blob two names
  inside one build, so the first build after any compiler upgrade - a cold
  cache generation - lost the export blob, and the interface-dispatch table it
  emitted was missing every impl from the root package: `fmt` printed `?` for
  every `Stringer`. Warm builds were correct, which is why every gate was
  green; `tests/phase0c_coldcache.sh` now runs cold on purpose.
- **A bare host bootstraps from one versioned script** -
  `scripts/provision-build-host.sh` provisions the pinned Go 1.25.3 and LLVM 22
  (with the unversioned tool names the driver asks for), supplies the
  compiler-rt sources and the llvm-glue library the repository does not carry,
  and runs `s1 -> s2/s3 (or s3/s4) -> bake`. `--check` reports what a host is
  missing and changes nothing. The bootstrap pins `legacy/moxie` as s1's
  compiler instead of trusting whatever `./moxie` happens to be in the tree.

Versions are tagged with `vX.Y.Z` and capture user-visible changes - compiler
features, language rules, runtime additions, and fixes that change observable
behaviour. Point-releases under the same minor typically group related
work; the headline of each entry matches the tag's commit subject.

## v1.8.1

- **Self-hosting fixpoint reached** - stage4 compiler builds itself to a
  reproducible binary. No Go toolchain required.
- **Float hex encoding fix** - float literals emitted via source text
  (`cf.lit`) instead of `Float64bits(cf.v)`, eliminating bootstrap poison
  where corrupted float constants in the compiler binary produced wrong
  IR for the next generation.
- **Rune/string concatenation is now a compile error** - `"s" | 'r'` and
  `'r' | "s"` produce `cannot concatenate rune with string - convert to
  UTF-8 first`. Use `r.String()` for explicit conversion.
- **Builtin `.String()` methods on all primitive types** - `int32`, `int64`,
  `uint32`, `uint64`, `int8`, `int16`, `uint8`, `uint16`, `float32`,
  `float64`, `bool`, `string`, `byte` (hex), `rune` (hex), `uintptr` (hex).
  `fmt` variadics use `...Stringer` - no reflection, no empty interfaces.
- **compileErrors separation** - deliberate compile errors (rune concat)
  separated from benign parser warnings (scanner UTF-8 noise), preventing
  false-positive build failures on stdlib packages.
- **Versioned binary installs** - `release.sh` and `install.sh` install
  `moxie-X.Y.Z` to `~/.local/bin/` with `moxie` as symlink to latest.
- **LLVM 21 required** - build scripts and README updated.
- **`moxie fetch`/`moxie install`** - network-free builds with `.mxh`
  protocol headers for cross-binary spawn type checking.
- **`moxie update --all`** - rebuilds cached packages after compiler upgrade,
  cleans stale version caches.
- **Forward-port compile/ from stage4** - compile library converged with
  stage4 compiler source.

## v1.2.19

- **WASM GC reverted to leaking** - conservative GC (v1.2.18 commits
  a182665..96e2ab4) caused stack overflows in the recursive mark phase on
  large heap graphs. Leaking GC is correct for short-lived WASM modules
  with explicit cleanup paths. `NeedsStackObjects` disabled, `__heap_base`
  / `__data_end` exports removed, stack size override removed.
- **`trap()` / `moxie_longjmp()` use direct LLVM exports** instead of
  arch-specific wrapper functions (`trap_arch`, `moxie_longjmp_arch`).
- **`llvm_sponentry` intrinsic** added (`llvm.sponentry.p0`) for stack
  pointer on function entry.
- **`gc_stack_portable.mx`** reverted to goroutine-aware stack scanning
  with `swapStackChain` support.
- **Fork error handling** fixed in `os/osexec.mx` - errno checked
  correctly for non-zero pid returns.
- **Pipe stall diagnostic** reduced from 60s to 3s for faster detection
  of stuck spawn channels.
- **Removed `llvm.used` for WASM imports** - not needed without
  conservative GC's LTO preservation requirements.
- **README** corrected: spawn channel retry is indefinite (not 100ms
  timeout), WASM GC column updated.

## v1.2.1

- **Latent v1.1.2 regression fixed** — native stdlib builds now succeed
  end-to-end. Four-stage fix:
  1. `splitConstBlocks` cascade-demotes const specs that depend on
     already-demoted `[]byte` vars (fixed `crc32` + similar patterns).
  2. New `RewriteAddToPipe` AST pass converts `+` to `|` in non-main
     packages so `dnsmessage`-style `s := "a" + "b" + "c"` on `[]byte`
     vars typechecks.
  3. `patch-gotypes.go` patch 12: `matchTypes.mayConvert` allows
     untyped-string ↔ `[]byte` conversion.
  4. `patch-gotypes.go` patch 7 widened: `|` accepted when either
     operand is a slice *or* a string (covers `[]byte(lit | var)`
     where the constant path converts the literal to typed `String`).
  5. `CheckPlusOnText` and `FindPipeConcat` now handle `[]byte(...)`
     wraps correctly — skip pure-literal chains (Go folds them);
     descend into mixed chains (rewrite to `__moxie_concat`).
- **Docs**: CHANGELOG.md added; REFERENCE.md v1.2.0 stdlib / JS runtime
  shim entries; `CLAUDE.md` WASM directive narrowed (native targets
  remain first-class).

## v1.2.0

- **Bitcoin EC crypto** migrated into moxie stdlib (`crypto/secp256k1`,
  `crypto/schnorr`, `base58`) so downstream projects stop carrying the
  BIP340/secp256k1 implementation.
- **p256 runtime** — `crypto/ec/p256` with the host `crypto.subtle.*`
  bridge for `js/wasm` and a pure-Go path for native.
- **AES-128-GCM** runtime — cipher suite support for MLS 0x0001.
- **`node`/`aead`** stdlib additions.
- Runtime/syscall refinements around the p256 and AEAD additions.

## v1.1.4

- **XChaCha20-Poly1305 AEAD** runtime for IndexedDB at-rest encryption.

## v1.1.3

- `dom.Remove` bridge.
- Diagnostic overlay removal in moxiejs runtime.

## v1.1.2

- **Const pipe rewrite**: `|` folded to `+` in const string expressions so
  `const X = "a" | "b"` typechecks as a constant. (Note: a four-stage
  interaction with `splitConstBlocks`, `FindPipeConcat`, and the patched
  `matchTypes.mayConvert` was identified and fixed in the following
  development cycle — native builds of stdlib packages with mixed
  `[]byte`/literal `|` chains now work end-to-end.)

## v1.1.1

- **Enforce `|` for text concat.** `+` on text types is now a compile
  error in user/main-module packages; use `|`. Stdlib and vendor packages
  still accept `+` syntactically (rewritten to `|` by the loader before
  the first typecheck) so pre-v1.1.1 vendored code continues to build.

## v1.1.0

- **`__moxie_secalloc` native landing** — secure-allocation runtime for
  key material, wiped on free.
- **moxiejs**: regabi stack layout, `types.Info.Implicits` tracking, and
  a `copy()` fix that lost slice lengths on same-type-reference copies.

## v1.0.16

- Netpoll fix: `EPOLLOUT` no longer busy-spins when a socket's writable
  side is already drained.
- `PUBLISH_TO` relay routing for targeted publishes.
- Platform-adaptive `ext.mjs` — one signer shim auto-detects
  extension-vs-in-page environment.

## v1.0.15

- `signer.mjs`: check `error` and `sig` fields on `signEvent` resolution.

## v1.0.14

- `signer.mjs` error reporting improved.
- `sw.mjs` `Log` / `SetTimeout` helpers.

## v1.0.13

- `dom.OnPullRefresh` — scroll-to-refresh gesture support.

## v1.0.12

- JS backend: fix string-equality op using `cap` instead of `len`.
- Service worker fallback routing.
- Signer bridge coercion for boolean fields.

## v1.0.11

- `dom.mjs` guards for non-Element nodes.
- `dom.FirstElementChild`.

## v1.0.10

- **moxiejs pipe concat** — `|` lowered to `__moxie_concat` in the JS
  backend.
- Extract `moxie/mxtext` so rewrite logic is shareable between the
  native compiler (`moxie build`) and `moxiejs`.

## v1.0.9

- `makeSlice` deep-clones object zero values so default element state
  doesn't alias across slice slots.

## v1.0.8

- Service-worker hot-update fixes.
- Build stamp embedded in the entry module.
- Closure call dispatch in the JS backend.

## v1.0.7

- `signer.ResetExtension` bridge.

## v1.0.6

- `dom.QuerySelector` / `GetElementById`: return `0` (valid ref) instead
  of `-1` (invalid) when an element is found.
- BigInt runtime fixes.

## v1.0.5

- BigInt `uint64` correctness.
- wNAF schnorr signing (15× faster than naive scalar-mult).
- `js/wasm` build context exposed.

## v1.0.4

- `Sprintf` pool-aliasing fix (output buffer was shared with earlier
  results under `string = []byte` unification).
- moxiejs literal-syntax preprocessor (`[]T{:n}`, `chan T{n}`, etc.).

## v1.0 and earlier

- **v1.0.0–v1.0.3**: JS backend groundwork, Moxie language rules
  (no `goroutines`, no `make`, no `new`, no `+` on text), spawn as
  a builtin, `moxie.Codec` IPC boundary, `secalloc` for key material.
- **v0.x** — initial language (Moxie forks TinyGo), stdlib purification
  (117+ packages compile under Moxie restrictions), `int=int32` on all
  targets, `string=[]byte` unification via patched `go/types`, `spawn`
  for inter-domain concurrency replacing `go`.
