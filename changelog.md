# Moxie Changelog

## v1.0.0

The first stable release, tagged on a full gate rather than a targeted set:

- **Native phases: 214/214 units pass** (0a, 0b, 0c, 1, 1b, 1c, 2, 3, 4, 6, 7,
  8), including the self-hosting fixpoint (stage2 == stage3), the cold package
  cache, and the sibling app's 9 wasm bundles plus 11 browser smoke tests.
- **Wasm phase: 124 pass, 0 fail, 2 declared native-only skips.**
- **The relay check is real:** morly, built with this compiler, publishes an
  event over WebSocket, stores it, and delivers it back to a query and to a live
  subscription (its own integration suite, throwaway relay and store).

Last fixes on the way to the tag:

- **Unsigned constant ranges are judged on the exact value.** `uint64(1) << 63`
  was rejected as an overflow because `ConstInt.V` holds the low 64 bits and
  2^63 looks negative there; the range test now uses the exact value when it
  has one, and a negative bit pattern from an unsigned shift is not an
  overflow. `uint64(0) - 1` is refused again in the declaration form, in both
  compilers.
- **`crypto/ed25519.PublicKeyOf`** - the constructor the mls code calls.
- **`jsruntime/bridge-common.mjs`** (the second, app-supplied host) provides
  `channel_try_recv`; without it every app bundle failed to instantiate.
- **`ip_api.mx`** calls the named-type methods (`ip.String()`, ...) instead of
  the removed alias free functions.
- **`first_error`** is back in `tests/util.sh` (phase 0b and 0c showed an empty
  diagnostic without it).
- **The test server never binds a port Chromium refuses** (ERR_UNSAFE_PORT took
  one random wasm fixture down per sweep), and the wasm host page lets the hub's
  log drain run before reporting done.

Known next work is recorded in `docs/open-work.md`: one domain-id space for
`ch.childDomain` and the hub's lineage ids, and the lattice.

## v1.0.0-rc.4

- **The native intermediate-spawn hang is fixed, with its mechanism.** Native
  chose a spawn channel's ring direction from the per-domain
  `childSpawnTarget` flag; a domain that is both a child (of the root) and a
  parent (of its own child) picked the wrong ring for one of the two and waited
  on a ring nobody wrote. Direction is now keyed on the channel
  (`ch.childDomain`, the spawn handoff's unique token), so each domain resolves
  its side from the same shared struct.
- **Native reuses a channel's rings on re-spawn**, as wasm already did:
  `spawnDomain` keeps the existing pair instead of creating one per spawn slot.
  The struct is shared, so a fresh pair made every holder silently follow the
  newest rings and split a channel a manager passed on.
- **The hub log queue is bounded with a reported drop.** 4096 entries by
  default; overflow drops the line, counts it per domain, and flushes
  `[hub] N log lines dropped for <id>` at exit (bypassing the queue if it is
  still full). `stats().dropped` and `stats().unattributedDropped` expose the
  same numbers.
- **The new tests were seen to fail.** Adoption disabled -> `spawn_orphan` red;
  the spawn request queued on the spawning domain's event loop ->
  `spawn_root_parked_parent` red. Both mutations reverted.
- **Fixture renamed:** `spawn_root_parked_parent` says root-level, and the
  intermediate shape is its own fixture `spawn_parked_intermediate`.

## v1.0.0-rc.3

- **One hub, two planes on wasm.** Control (spawn, exit, panic, rebind,
  heartbeat) and logs share one port and one lineage id (`1.3.2`, hub-assigned,
  never reused); the data plane does not go through the page - a spawn channel
  is a ring the spawning domain created, whose handles the page moves without
  reading a byte. The contract is `docs/wasm-hub.md`.
- **The hub is its own module with a Node test.** `jsruntime/supervisor.mjs`
  has no DOM, no Worker and no page, so the protocol runs under `node --test`
  (`5/hub-protocol`, 13 cases): lineage, refused spawns, missing ids, the
  orphan invariant, the parked-parent shape, line assembly, panic, bounded
  drains.
- **Logs star to the page.** A domain posts what it writes to the hub; a parent
  never forwards a child's output (that would be a parent parked in a receive).
  Lines are assembled per domain, so two domains printing at once can no longer
  splice each other's lines.
- **Missing ids are a protocol decision, not an accident.** A spawn from an
  unknown id or from the wrong port is refused and answered; a log from an
  unknown id still prints, as `?`; other unknown-id control messages are
  counted and dropped.
- **A root's exit is not a process exit on wasm.** `_start` returning resolves
  the loader and the page keeps the root Worker for callbacks, so its children
  are adopted by the page and keep running; the app stops them with their
  breakers. The first cut terminated the root, which silently killed every
  post-`_start` callback.
- **A channel keeps its ring through a second spawn.**
  `runtime.SpawnChannelHandle` reuses the existing SAB handle for a channel
  already on a boundary, so a manager can pass a worker's channel on without
  splitting it into two rings.
- **Init-time supervisor check.** A module built with spawn support that boots
  without a supervisor now fails at boot, not at its first `spawn()`; the
  service-worker host refuses it with a named message.
- **New fixtures:** `spawn_orphan` (A spawns B, A exits, B keeps logging and
  exits cleanly), `spawn_root_parked_parent` (the ROOT parks in a receive right
  after spawning) and `spawn_parked_intermediate` (a spawned domain does), all
  green as wasm-output-matches-native.
- **Recorded, open:** natively, a spawned domain that spawns a child and then
  blocks in a receive on that child's channel never wakes.

## v1.0.0-rc.2

- **Wasm spawn works, and it is page-supervised.** A Worker started by a
  Worker never runs while its creator is parked in `Atomics.wait` - the startup
  task needs the creator's event loop - so a domain that spawned and then
  waited for its child deadlocked with no diagnostic. Every domain now holds a
  supervisor port to the page, `spawn_domain` posts a spawn request over it,
  and the page starts the Worker and hands it the same SharedArrayBuffers. The
  page is never parked in wasm.
- **The breaker is threaded.** The parent creates the breaker ring before the
  domain starts, `__spawn_entry` receives its handle and binds it as the
  domain's `closed` (`WasmBreakerSet`), and the child's exit closes it
  (`WasmBreakerClose`). `spawn` returns the parent's end of that ring, so
  `close(done)` stops a worker parked on `<-closed` and `<-done` returns when
  the child ends.
- **`tryPipeRecv` no longer blocks.** A select over a spawn-bound arm runs on
  the domain's only thread, so a blocking probe stopped every other arm: a
  worker selecting on its breaker never looked at its inbox. `channel_try_recv`
  reports empty (-2) versus closed (-1); the select path uses it and a direct
  `<-ch` still blocks.
- **The compiler no longer special-cases spawn-bound channels.** The
  `sabChannels` bypass is gone from both compilers: the runtime routes a
  spawn-bound channel, and the parent's channels are tagged at run time by
  `MarkChannelSAB` (legacy now emits that call too).
- **stage4: a nil `Signature.Params` is a function with no parameters.**
  Treating it as "not a spawn target" made `spawn(worker)` with zero arguments
  emit a nil result and start nothing. Fixed in the call emitter and in
  `__spawn_entry`'s dispatch. Legacy was already correct (go/types returns an
  empty tuple).
- **The wasm spawn refusal is withdrawn** and the fixture's `//:wasmtest skip`
  with it: `wasm_spawn_struct` runs in Chromium under both compilers and phase 5
  reports "output matches native".

## v1.0.0-rc.0

The first release on the public toolchain repository (`moxielang.dev`, staged
at `github.com/moxie-language/toolchain`). Everything below the tag is the
compiler as it self-hosts to a reproducible binary.

- **A missing diagnostic is not evidence** - three rounds read a decoder that
  rejected every frame as one that was never entered, because `printFD` stayed 0
  in a spawned domain and every worker `println` went to fd 0 and vanished.
  `initDomain` seeds the print descriptor, a spawned worker prints a canary, and
  every probe asserts its control line before its result counts.
- **Spawn channel frames carry an offset representation for self-contained
  elements** - a struct of scalars and `string`/`[]byte`, or a bare
  `string`/`[]byte`, crosses as one memory image whose pointer slots are
  `uintptr` offsets from the frame base (a 4-byte magic; total and root are
  derived). Decode is one memcpy plus a rebase. It is LOCAL-ONLY by
  construction (it carries the machine's slice header, padding and pointer
  width); the structural codec remains the portable wire format for pipes and
  sockets. Rings are sized by a sizer next to the codec - the max of the
  structural and offset maxima - implemented once per compiler.
- **The import root is `moxielang.dev`** - the toolchain's own packages import
  under the vanity domain, and the loader resolves it, so a toolchain module
  states its dependency by name rather than by checkout path.
- **A spawn target's result is the breaker, `chan struct{}`** - `spawn(fn, ...)`
  returns the target's one declared result. The breaker is a signal, not a
  message: nothing is ever sent on it, and a close is the whole event. A panic
  does not send a reason through it - the process aborting is the message, and
  the diagnostic is on stderr (see below).
- **`shutdown` is the domain's termination channel** - `main` selects on
  `shutdown`, a spawn target on `closed`. The word lowers to `__shutdown()`, a
  builtin typed `chan struct{}`; the runtime creates the channel once, in the
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
- **A spawn target's result must be NAMED `closed`** - the name is part of the
  contract, not only the type: a function returning a `chan struct{}` under
  another name is ordinary and may be called directly. The name crosses package
  boundaries as `func (closed) <name> <sig>` on the func line of the export
  data, because result names are not carried there and a name inside the
  signature descriptor would split interface-method matching. The marker is
  parenthesised so it can never be read as a function identifier.
- **A spawned wasm domain is a real domain** - `__spawn_entry` initializes the
  runtime (`WasmSpawnInit`: the root arena and package initialisers that
  `_start` does for the root), stages its packed arguments in a static
  `__spawn_scratch` area (there is no arena before initialization), and builds
  a real channel object around each SAB handle. Spawn-bound sends and receives
  no longer route to stubs that returned false: the element bytes cross the
  bridge, and the child wrapper is sized from the element type. A panic in a
  spawned domain reaches the page as one stderr line (line-buffered), and the
  page stops every domain it spawned; `tests/wasm/panic_tree` gates that with a
  sibling that would print after 1.5 s.
- **A panic prints to stderr before it dies** - the diagnostic is the first
  thing the panic path writes, on fd 2, and the process then aborts (status
  134). It does not signal through breakers: native has one process, so no
  domain could read a reason after the abort. On wasm the same text reaches the
  page as stderr and the page stops every domain it spawned, which is the
  browser counterpart of the process ending. A spawn's breaker is closed by its
  owner with `close(breaker)`; `main` returning ends every domain with the
  process.
- **The termination select is enforced** - both compilers refuse a `main` that
  does not consume `shutdown` and a spawn target that does not consume its
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
- **A spawn's breaker is closed only by its owner** - the compilers no longer
  close the breakers a function created when that function returns. A pool
  constructor hands the breaker to its caller, and closing it there killed the
  worker microseconds after its thread started; a returning frame is not the
  owner deciding a domain is finished. `close(breaker)` ends a domain, `main`
  returning ends every domain with the process, and a panic aborts the process
  (status 134) rather than leaving a domain to be restarted.
- **Interface dispatch only enumerates implementers of the interface** - an
  invoke arm is emitted only for a type whose method set covers every method of
  the static interface, the same filter the legacy lowering already applied. A
  name match alone enumerated `http2FrameHeader` into `sockaddr.String()`
  chains: `net`'s wasm bitcode was 15 MB with two functions over 100k IR lines,
  and `llc` was killed at ~30 GB. The filter took the same module to 7.5 MB and
  the largest function to 54k lines, and `1c/net/http` compiles.
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
- **Docs**: changelog.md added; REFERENCE.md v1.2.0 stdlib / JS runtime
  shim entries; `claude.md` WASM directive narrowed (native targets
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
