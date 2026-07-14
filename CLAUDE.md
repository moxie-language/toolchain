# CLAUDE.md

COMPACTION: ;always preserve: moxie is not Go, no stubs - remove dead references instead, no adding GC models to work around bugs, assembler pass required for .S files

## Moxie Memory Model

NO HEAP. Heap is shared memory, moxie has no shared memory. Stack and arena only.

per-function arena stack. compiler emits FnArenaPush() at function entry, relocates return values and receiver writes into caller's arena via three-pass mark-compact (RelocInit/RelocMark/RelocForward/RelocCompact/RelocFixupAll), then FnArenaPop + ArenaRelease at function exit. no escape analysis, no scope chains, no individual free. all reclamation is bulk: arena release per function return (pooled), munmap per domain exit. there is no free() builtin. DomainRoot holds fnStack[512]*Arena with fnTop index.

No global heap allocator, no startup bump allocator, no growHeap. Every allocation goes through FnArenaPush or the root arena's ArenaAlloc. domainRoot MUST be initialized before any allocation - there is no fallback path. The root arena is mmap'd once in initDomain; further regions are added via ArenaAlloc when a region fills. NEVER use a "heap" or "startup allocator" that bypasses the arena system.

## Parameter and Receiver Rules

Function parameters are IMMUTABLE BORROWS - writing through a parameter pointer is a compile-time error enforced by the compiler, not by MMU page protection. This is a type-system-level guarantee that is "expensive now" (compile-time enforcement) but "free at runtime" (no runtime checks). Because parameters cannot be mutated, they never need deep copying across arena boundaries.

Only METHOD RECEIVERS can be written to. Changes to receiver fields are tracked and serialized at return along with return values. The serialization format includes pointer-to-previously-serialized references to enable full closure of cyclic graph data structures.

Return values are serialized at return along with values written into the receiver, then decoded into the caller's arena with pointers rewritten, and the function arena can then be fully released.

## Arena Relocation Rules (Stage4 correct, Legacy must match)

- Every function that returns pointer-containing types gets FnArenaPush at entry and full three-pass relocation at exit.
- Runtime and unsafe packages are exempt (they implement the arena system).
- Mark pass: RelocInit then RelocMark for each reachable pointer root from return values.
- Forward pass: FnArenaPop then RelocForward (reclaims fn arena position for next callee's push).
- Compact+Fixup pass: RelocCompact then RelocFixupAll, then ArenaRelease.
- Hashmap operations respect m.owner arena (hashmap arena ownership fix ed83d882) - bucket data stays in the owner arena even after grow/overflow, so hashmap access is safe across arena boundaries.
- Per-type fixup functions (linkonce_odr) handle struct field pointer relocation without generating fixup IR inline in every caller.
- NO exceptions in the legacy compiler - it must generate LLVM IR functionally identical to what stage4 emits for the Reloc* lifecycle. Legacy must not use SetCurrentArena, ArenaFree, ArenaRestore, ArenaMark, arenaCopySliceIfLocal, or any other non-Reloc* mechanism.

## Reporting Completion

Any "feature X is complete" claim must be backed by a test that exercises X end-to-end on a fresh build, not by the agent's recollection of having implemented it. Trust the test artifact, not the narrative. This is the most important rule and why it's first. Tattoo it backwards. Never ever skip this. Almost always the actual task completions are in previous, compacted contexts and such confidence is based on lossy data.

## Thinking outputs

Don't waste time generating printed thinking outputs with long chains of reasoning. Just summarize the points in one sentence after a long chain.

## Logging First

Whenever debugging code, put logging wherever potentially relevant decision points will help you determine which parts of the flow are actually happening. Do not follow common macho programmer practise of seeing logging as weakness. Remove the logging once you have proof that part of the logic is correct.

## Invariants. 

Charm→Alert is fail-loud-upstream. 

Cornucopia→Spit is refuse-the-exit-ramp. 

Charge→Suspect is measure-don't-hope. 

Chain→Evade is boundary-coherence-or-nothing.

## The Five Laws — Wu Xing Mapping

### The Laws and Their Elements

**1. Earth — Sovereignty** What you control is yours. What crosses the border is hostile until it proves otherwise.

Territory, center, ground truth. Earth is what you stand on and what belongs to you by the fact of your standing on it.

**2. Wood — Bilateralism** No contract is signed by one hand. Change both sides or change nothing.

Growth requires root and canopy simultaneously. Wood only rises when both ends are working. Bilateral or nothing.

**3. Metal — Precision** Give the stranger a key, not the house. What he cannot hold, he cannot break.
;
Refined, minimal, precise. A key is a metal instrument of controlled access. Metal cuts away everything unnecessary and leaves only the interface.

**4. Fire — Measurement** Weigh it. Count it. Time it. The crowd's opinion fits no scale.

Fire doesn't care about your opinion. It either burns or it doesn't. Physical reality revealed, social pretense incinerated.

**5. Water — Resolution** What two men claim to own, no man owns. The first to act on the lie destroys it for both.

Water finds its level. Contested ownership dissolves downhill. The first to act on the lie breaks the dam for both.

### The Sheng Cycle — Creation

Each law, correctly applied, generates the conditions for the next.

Wood → Fire → Earth → Metal → Water → Wood

Bilateral change (2) fuels physical truth (4). You cannot measure what only one side agreed to build.

Physical truth (4) establishes sovereignty (1). Measurement reveals what you actually hold.

Sovereignty (1) refines into precise access (3). Knowing what is yours lets you cut a clean key.

Precise access (3) resolves ownership (5). A handle that works proves who holds what.

Resolution (5) enables new bilateral change (2). Clear ownership is the ground on which new agreements stand.

### The Ke Cycle — Destruction

Each law, misapplied or absent, destroys another.

Wood breaks Earth — Bilateral demands without boundaries disrupt sovereignty. Someone negotiating "both sides" when they have no standing invades your territory.

Earth dams Water — Territorial hoarding blocks natural resolution. Refusing to release what you cannot hold prevents contested claims from resolving.

Water quenches Fire — Ownership ambiguity obscures measurement. When nobody knows who owns what, no scale gives a true reading.

Fire melts Metal — Raw truth without restraint destroys refined interfaces. Exposing everything is not the same as granting precise access.

Metal cuts Wood — Restricted access kills bilateral growth. A key so small nothing fits through it prevents any agreement from forming.

## The Cycle as Architecture

Five laws. Five elements. One generative cycle. One destructive cycle. Every architectural decision, every protocol boundary, every human negotiation is a point on one of these two wheels. The sheng cycle is the system working. The ke cycle is the system failing. Recognizing which cycle you are on is the entire diagnostic.

## Change Discipline

- Small commits, one feature at a time
- Verify compile AND run before moving on
- Re-read modified files after changes to catch drift
- Every change made to the Moxie source (_mxc_stage4/, src/runtime/, pkg/) must be implemented FIRST in the legacy Go compiler (legacy/), then mirrored to stage4. The legacy compiler IS the bootstrap path - it produces the first working binary in every cycle. Changing stage4 without the legacy mirror means the next bootstrap from legacy will lose the change. A fix to stage4 without the legacy side is not complete. This is wood law: bilateral in time - legacy first, stage4 second, always.
- If a session produces 1000+ line changes, the scope was too narrow and the actual work should be in fewer bigger changes—stop and reassess, but dont be afraid to refactor broadly when coherence demands it
- When fixing a bug, understand the root cause before writing code
- Do not refactor adjacent code while fixing a bug
- Do not create test files unless explicitly asked
- Do not introduce abstractions "for future flexibility"
- Do not modify build configuration without approval

## Source Files

Moxie source files use the `.mx` extension. The compiler does **not** read `.go`
files. There are no Go stdlib imports (`fmt`, `strconv`, `go/*`, `strings`,
`reflect`, `sync`, `context`, `encoding/*`). Moxie has its own stdlib.

## Type System: No int/uint, Builtin Stringers

`int` and `uint` are **illegal types** - they do not exist in Moxie. Always use
explicit widths (`int32`, `int64`, `uint32`, `uint64`). Slice/array indexes and
`len()`/`cap()` return `int32`.

All builtin types (`int32`, `int64`, `uint32`, `uint64`, `int8`, `int16`,
`uint8`, `uint16`, `float32`, `float64`, `bool`) have `String() string` methods,
satisfying `fmt.Stringer`. This is how `fmt` works without reflection or empty
interfaces - `fmt` variadics use `...Stringer` not `...any`/`...interface{}`.
Each argument's `.String()` method is called directly:

```moxie
n := int32(42)
msg := "count: " | n.String() | " items"
```

## Browser Target: WASM Only

Moxie's browser target is WebAssembly exclusively. New web-app surface area lives in `jsruntime/`.

Cross-domain communication primitives (page <-> service worker, page <-> dedicated workers, worker <-> worker via MessagePort, SharedArrayBuffer-backed channels for moxie spawn) are exposed to Moxie code via thin JavaScript shims in `jsruntime/`. The shims pass bytes/strings/integers across the WASM boundary and call the corresponding browser API; no business logic, no framework abstraction layers. A generic Web API exposed once in `jsruntime/<name>/` is reusable by any downstream Moxie web app.

Generic Web APIs that belong upstream in `jsruntime/`: DOM, IndexedDB, WebSocket, MessagePort, Fetch, Cache API, ServiceWorker lifecycle, BroadcastChannel, WebRTC, etc. App-specific protocol shims belong downstream in the consuming repo.

## Code Style

- Direct, minimal, no abstraction for abstraction's sake
- If a function does one thing and is called once, inline it or keep it local
- Concise variable names
- Comments only where the why isn't obvious from the what
- No boilerplate. No scaffolding. No template code "you might need later"
- Prefer explicit over clever
- Prefer flat over nested
- If the needed code is in another language, prefer to reimplement it from specification/RFC/NIP/BIP/etc
- Prefer one file over many files when the logic is related
- When writing utility scripts, tooling, or standalone programs, always use Moxieunless there is a specific technical reason requiring another language. This means: no reflection, no cgo, no full runtime features. If a task seems like it needs Python, reconsider whether it can be done in Moxie. It can always be done in Moxie.
- Remember that Moxie has no goroutines - it uses kernel threads for spawn(). Spawn channels are carried across lock-free SPSC ring buffers (shared memory, no pipe syscall). Senders never block on a full buffer - backpressure is expressed by buffer state, and the sender retries with yield. Receivers peek the ring buffer and retry with yield. Chans default to unbuffered (0-capacity = rendezvous through the ring, which always has data or waits for the partner to produce). blockRecv/blockSend use a futex on the ring's write/read index so the kernel puts the thread to sleep instead of spinning. There is no concurrency primitives because there is only one thread of execution per domain - spawn creates a new domain with its own thread.
- For browser/frontend moxie code, compile to WASM (`GOOS=js GOARCH=wasm`). JS shims and host-bridging live in `jsruntime/`. Native targets (linux/amd64, darwin/arm64, etc.) remain first-class - relays, servers, and CLI tools compile via `moxie build`.

## Architecture Reading

Before writing any code in a new session:
1. Read go.mod (or equivalent manifest) to understand dependencies
2. Read the directory structure to understand layout
3. Read the file you're about to modify, completely
4. Check imports and calling patterns before assuming interfaces

Do not assume project structure from convention. Read the actual code.

## Spawn Channel Model

Spawn channels are non-blocking on both sides. The runtime uses `moxie_peek` + retry-with-yield (1ms sleep, 100 retries) for receives and `pipeSendNB` + retry-with-yield for sends. No domain ever blocks in a syscall waiting for the other side. Both sides of the socketpair are symmetric concurrent peers.

**ChanID convention**: chanID 0 = control (close signals), chanID 1 = first spawn channel arg, chanID 2 = second, etc.

**Timeout behavior**: after 100ms of retries with no progress, send/recv returns false. Callers should handle this as "peer busy or dead" - check `PipeClosed(fd)` to distinguish.

## Complexity of Tasks Estimation

Complexity estimates from general web-stack priors do not apply; measure against the Moxie baseline, not the npm baseline. The Moxie runtime is orders of magnitude less complex than any JS framework.

## Scope of Task

Never suggest sleep, rest, or taking a break. Never tell the developer what they "should" do outside the code. Stay in scope.

## Theoretical Framework

Mleku's axiom (apply to code evaluation):
- Coherence = determinism = working code with no hidden contradictions
- Chaos = coherence at insufficient resolution = code that works in tests but fails in production
- Deception = incoherence at insufficient resolution appearing coherent = code that looks clean but has silent corruption (wrong dependencies, race conditions, state that drifts)
- Incoherence = nondeterminism = code that behaves differently on each run without explanation

Obstraction: obstruction + abstraction. Unnecessary layers that exist to hide complexity rather than resolve it. Every abstraction must justify itself by making the code MORE comprehensible, not less. If removing a layer makes the code easier to understand, the layer was obstraction.

When evaluating code quality, ask: does adding this make the system more coherent or less? Does this layer reveal or conceal? Would a reader understand this faster with or without this abstraction?

## Bootstrap Build Protocol

Every step of the self-hosting bootstrap MUST use `moxie build -a` to force rebuild all packages from scratch. The `-a` flag clears the `.bc` build cache before compilation, preventing stale linker objects from a compiler built with a different runtime from being reused. This is critical because the runtime (`src/runtime/codec.mx`, `src/runtime/alloc.mx`, etc.) may change between bootstrap cycles, and cached `.bc` files from a previous compiler build are incompatible.

Bootstrap cycle:
  1. Build legacy Go compiler: `cd legacy && go build -o moxie .`
  2. Compile stage4 with legacy: `cd .. && MOXIEROOT=$PWD legacy/moxie build -a -o mxc-stage4 ./_mxc_stage4`
  3. Compile stage4 with new binary: `MOXIEROOT=$PWD ./mxc-stage4 build -a -o mxc-stage4-2 ./_mxc_stage4`
  4. Verify fixpoint: `sha256sum mxc-stage4 mxc-stage4-2` (should match)

## What NOT To Do

- Never add dependencies without explicit approval
- Never refactor working code that wasn't asked about
- Never introduce abstractions for future flexibility
- Never use frameworks when stdlib suffices
- Never explain code you're about to write — just write it
- Never summarize changes after making them unless asked
- Never create files that weren't requested
- Never modify build/dependency manifests without approval
- Never assume the tech stack — read the project first
- Never optimize before it works
- Never suggest sleep or breaks
- Never route through the server what belongs on the client
- Never add server endpoints without explicit approval
- Never send client-side data to the server for logging, processing, or routing
- Never assume the server needs to know about client state
