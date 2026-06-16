# CLAUDE.md

COMPACTION: ;always preserve: moxie is not Go, no stubs - remove dead references instead, no adding GC models to work around bugs, assembler pass required for .S files

## Moxie Memory Model

scope dealloc is compile-time code generation, not runtime tracking. emit free() at scope exit like a C programmer. no escape analysis, no
runtime scope chains, no escaped bits. all the free code does at scope boundaries is run through the list, created by the generated code that went into a conditional block that assigned the ownership of a
given variable to an outer (preceding) scope declared variable, and thus is now freed by that scope boundary.

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
- If a session produces 1000+ line changes, the scope was too broad — stop and reassess
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
- Remember that Moxie has no parallelism (no goroutines), no reflection, no arbitrary string/bytes distinction (remember to treat these as encodings and preserve UTF-8 byte sequences instead of breaking them up), spawn() boundary channels are always unbuffered (the network connection is a buffer) and use buffered channels as a convenient notation for FIFO queues. There is no concurrency primitives because there is only one thread of execution.
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

## Memory Management in WASM (Critical)

The Moxie WASM target uses the Boehm conservative GC inherited from TinyGo. Its mark phase is a **recursive depth-first scan** of the live heap graph. The WASM call stack is fixed (typically ~1MB = ~10k-20k frames). When the live heap graph grows large, the mark-phase recursion overflows the stack and the WASM module dies with `InternalError: too much recursion`. This is a GC stack overflow, not an infinite loop.

**Do not write Go-style "allocate freely, GC will clean up" code in long-lived WASM workers.** The GC is not fast enough or deep enough for that pattern. Every allocation in a long-lived worker must have an explicit free path.

### Mandatory Rules

1. **`delete(map, key)` is mandatory when an entry is done.** `map[k] = nil` keeps the slot reachable; only `delete` removes the reference. Apply at the exact lifecycle boundary where the entry is no longer needed - same timeout, same close handler.

2. **Every add path needs a corresponding remove path.** Subscriptions, callbacks, routing table entries, buffered messages - each one added must be explicitly deleted when its scope ends. "Works for 10 connections, dies at 100" is the symptom of a missing remove path.

3. **Clean ALL containers at the same boundary.** If a subscription is tracked in three maps, delete from all three in the same cleanup function. Missing one keeps the object graph reachable, defeating the cleanup.

4. **Use `AllConns()` for cleanup, not `URLs()` + `Get()`.** `Get()` returns nil for non-open (reconnecting) conns. Their internal `subs` maps still hold entries the GC traverses. Cleanup must reach all conns regardless of state.

5. **Slice semantics: `slice = nil` releases backing array.** `slice = slice[:0]` retains the allocation. Use nil assignment for cleanup, not re-slicing.

6. **Long-lived workers need rotation or explicit caps.** Any map that grows monotonically over a session (caches, relay tables, event metadata) must either have a delete path or a two-generation rotation scheme that bounds its size. App-level caches use `rotateEventCaches()`/`rotateAuthorCaches()` - add new maps to the right rotation function.

### Pattern: subscription cleanup

```moxie
// When a subscription ends, clean every map that references it.
for rSubID := range info.remoteIDs {
    delete(remoteToProxy, rSubID)      // routing map
    for _, c := range rpool.AllConns() { // ALL conns, not just open
        c.CloseSubscription(rSubID)    // cleans c.subs unconditionally
    }
}
delete(proxySubs, proxyID)   // primary map
delete(clientSubs, proxyID)  // secondary map
```

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
