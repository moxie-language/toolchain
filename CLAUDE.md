# CLAUDE.md

COMPACTION: ;always preserve: moxie is not Go, no stubs - remove dead references instead, no adding GC models to work around bugs, assembler pass required for .S files; slices replace arrays (an array type is leftover Go slice bypass), string and []byte are one MUTABLE type, and there is no aliasing - a subslice is a copy

## WOOD LAW: Bilateral Compiler Sync

The legacy Go compiler (`legacy/`) and the stage4 compiler (`_mxc_stage4/`) are the SAME compiler in two languages. They must remain feature-identical at all times. Both sides or neither.

Every compiler feature change — arena relocation, type system, code generation, parameter rules, sovereign model — must be implemented in BOTH compilers, in the same commit or immediately adjacent commits. Legacy first, stage4 second, always. This is wood law: bilateral in time.

A feature implemented in only one compiler is a lie waiting to surface at bootstrap. The legacy compiler IS the bootstrap path — it produces every first binary. Changing stage4 without the legacy mirror means the next bootstrap from legacy will lose the change, silently corrupting the binary.

Never merge a stage4 change without checking: "does legacy/compiler/compiler.go implement this same behavior?" If not, the merge is incomplete.

### Runtime Codec is Part of the Stack

`src/runtime/codec.mx` is consumed by both compilers. The legacy compiler interprets runtime functions via `interp.Run` during compilation; the stage4 binary links them at runtime. Both compilers must generate correct codec calls such that the runtime codec functions (`codecEncodeValue`, `codecDecodeValue`, etc.) produce identical output for identical input, regardless of which compiler built the binary. A codec bug that corrupts decode output in one compiler but not the other violates the bilateral invariant. Fixes to codec.mx apply to both sides automatically (single source file), but codec behavior must be verified with both compilers before any change is considered complete.

### Sovereign Arena

A self-mutating type (a named struct with at least one method that stores into
a receiver field, directly or through a method called on a receiver field) owns
a **sovereign arena**, held in a root-arena hashmap keyed by receiver address
(no struct layout change). Only methods that actually mutate the receiver
borrow it: the prologue lazily acquires the arena if needed, pushes it as the
current arena (marked borrowed, so the codec treats pointers into it as
stable/identity), and the epilogue pops it. The arena is NEVER freed - it
lives as long as the receiver.

**Non-mutating (read-only) methods of a sovereign type do NOT borrow.** They
get a regular per-function arena so their scratch is cleared at return; they
register the receiver's arena as "stable" so slices/pointers they return are
views into receiver data, not deep copies.

**Authoring rule: helper functions for scratch.** A mutating method's direct
allocations land in the sovereign arena and persist. When a mutating method
needs scratch, push the heavy work into a regular helper function (which gets
its own freed-at-return fn arena), and only write the product back into the
receiver (or return it to the method so it lands in the caller arena). This
keeps the sovereign arena bounded to actual object state, not per-call
garbage.

Runtime entries: `codecSovereignArena(recv)` lazily acquires/registers,
`SovereignSetArena`/`SovereignRestoreArena` push/pop the borrow,
`codecRegStable(recv)` registers a read-only frame's stable arena,
`SovereignRegStable`/`SovereignRegRestore` manage the stable-arena LIFO.

### Codec Dispatch Policy

The structural codec is used in two places, with this rule:

- **Spawn channels always use the builtin structural codec.** Cross-domain
  boundaries serialize values structurally (the full `codecEncodeValue` /
  `codecDecodeValue` graph walk with back-references). No exceptions - the
  wire format between domains is fixed.
- **Direct codec calls check for a custom encode/decode method first.** When
  serialization is invoked directly on a type (e.g. to a buffer), dispatch to
  the type's own `MarshalBinary`/`UnmarshalBinary`-style method if one is
  defined (`encoding.BinaryMarshaler` / `encoding.BinaryUnmarshaler`); only
  fall back to the structural codec when no custom method exists.

## Moxie Memory Model

NO HEAP. Heap is shared memory, moxie has no shared memory. Stack and arena only.

per-function arena stack. compiler emits FnArenaPush() at function entry, relocates return values and receiver writes into caller's arena via three-pass mark-compact (RelocInit/RelocMark/RelocForward/RelocCompact/RelocFixupAll), then FnArenaPop + ArenaRelease at function exit. no escape analysis, no scope chains, no individual free. all reclamation is bulk: arena release per function return (pooled), munmap per domain exit. there is no free() builtin. DomainRoot holds fnStack[512]*Arena with fnTop index.

No global heap allocator, no startup bump allocator, no growHeap. Every allocation goes through FnArenaPush or the root arena's ArenaAlloc. domainRoot MUST be initialized before any allocation - there is no fallback path. The root arena is mmap'd once in initDomain; further regions are added via ArenaAlloc when a region fills. NEVER use a "heap" or "startup allocator" that bypasses the arena system.

## Buffer Growth and Arena Discipline

An arena is never reused within a frame: `FnArenaPop` reclaims the position for
the *next callee*, not for you. Every buffer a growth step abandons therefore
stays live for the rest of the frame, and peak arena is the **sum** of every
intermediate, not the last one. Doubling to N costs roughly 2-3x N of arena;
allocating N costs N. Growth is the exception, and where it is unavoidable it
belongs somewhere its garbage dies early.

`push` does not grow. From a zero-capacity slice it lazy-inits to exactly the
first batch (floor 4) in one allocation, so that first push already *is* "one
buffer, sized once". Once cap is non-zero it is a bounded store that fails loud
with "push past end of slice capacity". The growth regime is **entered**, not
inherent, and the goal is never to enter it.

Choose by how the final size is known:

| how the size is known | technique |
| --- | --- |
| one scan, or a `len()` of the input | presize exactly, cursor write |
| a second pass over a cheap predicate | presize exactly, cursor write |
| a second pass over a user-supplied func (`FieldsFunc`, `Map`) | grow, but inside a free function |
| genuinely streaming, input consumed as read | grow, but inside a free function |

- **Presize and cursor-write.** `b := []T{:n}`, write `b[i] = v; i++`, return
  `b[:i]`. No capacity checks in the loop, no guards, no dead buffers.
- **Grow inside a free function.** A free function gets its own frame arena, so
  every intermediate a growth step abandons is reclaimed when it returns, and
  the value returned is relocated into the caller at exactly its length. Never
  let a buffer enter the doubling regime in an arena that outlives the call - in
  a sovereign arena every doubling step is permanent.
- **Never hand-write the capacity guard.** `if len(x) >= cap(x) { x = GrowCap(x) }`
  reserves ONE element and is a latent panic before a spread push, because
  `push(x, src...)` needs `len(src)`. Use `mxutil.Ensure(x, len(src))` for a
  spread, and `mxutil.Reserve(x, total)` when the total is known.

One function, one arena, one buffer, one pass is the target shape: read the
sources, write the output through a cursor. There is no concurrency, so a
transform whose output size is a function of its input needs no growth at all.

## Parameter and Receiver Rules

Function parameters are IMMUTABLE BORROWS - writing through a parameter pointer is a compile-time error enforced by the compiler, not by MMU page protection. This is a type-system-level guarantee that is "expensive now" (compile-time enforcement) but "free at runtime" (no runtime checks). Because parameters cannot be mutated, they never need deep copying across arena boundaries.

Only METHOD RECEIVERS can be written to. Changes to receiver fields are tracked and serialized at return along with return values. The serialization format includes pointer-to-previously-serialized references to enable full closure of cyclic graph data structures.

Return values are serialized at return along with values written into the receiver, then decoded into the caller's arena with pointers rewritten, and the function arena can then be fully released.

## Closures Do Not Capture Outer Scope

Function scope = function scope. A closure (function value / anonymous function) is a STANDALONE function that receives everything it needs through its parameters. It does NOT capture variables from the enclosing function scope - there is no closure environment carrying outer locals, and this is not a compiler limitation to be fixed. The isolation is deliberate: a closure that pulled state from its enclosing frame would create an ownership tangle between the caller's arena (which dies at return) and the closure's own lifetime.

Consequences:

- Ported Go code that writes `func(x T) R { return usesOuterVar(x) }` inside a function is WRONG in Moxie and must be rewritten to pass `usesOuterVar`'s inputs (or a pointer to a sovereign holder) as explicit parameters or receivers.
- A closure may read globals and its own parameters only. If it needs mutable state, that state must live in a sovereign self-mutating type passed as a parameter or receiver - not be captured from the enclosing scope.
- Function values still exist and can be passed/called, but a function value carries only its code pointer, never an environment.
- `spawn` already refuses to transit closure captures; the same rule applies everywhere, not just across spawn boundaries.

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

## Guards Are A Smell: Find The Root Cause

The moment you catch yourself adding a nil check, a default, a defensive branch, or a special case to stop a piece of code misbehaving, **stop and read that as evidence about something upstream**. A guard makes a symptom disappear; it does not tell you why the wrong state ever arrived. The wrong state is the bug.

The diagnostic is the repetition. One guard is a boundary condition. The **second** guard on the same call path is proof you are treating symptoms - because you have just watched the failure move a few lines further along instead of going away. That is the signal to abandon the local fix and search upstream for what is producing the bad input.

Symptoms that are guards in disguise:

- A check for a nil value that "should never be nil here."
- Falling back to a zero value or an empty result when something is missing.
- A branch that exists only to keep a specific caller or package working.
- Catching an error and continuing with a default.
- Widening a condition until the failing case slips past it.

What "upstream" means depends on the layer, and the search is always the same shape: **who produced this value, and under what assumption?** In this codebase it is usually one of:

- The type checker or SSA builder handing a later phase a nil type, a nil Scope, or a `Signature` whose slots were never filled - state that should have been established before the phase ran.
- A pass running against state it was never meant to see, e.g. type inference executing before the universe is installed.
- A struct whose layout or field offsets differ between the compiler that emitted it and the runtime that reads it.
- One compiler of the pair (legacy, stage4) having a feature the other lacks, so the "wrong" value is simply never written on one side.
- An arena lifetime mistake: state allocated in an arena that is bulk-freed before the reader runs.

A guard is acceptable in exactly these cases, and you should say which one applies in the comment:

1. The input is genuinely untrusted and can be hostile or malformed at a real boundary.
2. The nil/absent case is a **defined, expected** state that the algorithm handles - not an impossible one being papered over.
3. The guard is a *temporary* fail-loud stop that changes a silent wrong answer into a loud, named one, and the real fix is recorded as the next step.

If none of those hold, you have not fixed anything. Delete the guard and go upstream.

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

## Slices Replace Arrays; string Is []byte

Arrays are eliminated from Moxie. An array type still in this tree is leftover
Go slice-bypass code and should be rewritten as a slice. A slice already knows
its own length and there is no heap, so an array buys no stack layout and no
speed - it exists in Go only to dodge the runtime's slice bookkeeping, and
Moxie has no such bookkeeping to dodge.

- **No `append`.** `push` and `pop` are the slice mutators. `push` is bounded:
  from a zero-capacity slice it lazy-inits to the first batch (floor 4), and
  past the end of capacity it fails loud. Presize with `[]T{:n}` and write
  through a cursor.
- **Slices compare with `==` / `!=`.** With no reflect and no empty interface,
  that is the comparison there is.
- **`string` and `[]byte` are one type.** `len`, indexing, slicing and
  assignment work the same on both spellings.
- **Strings are mutable.** There is no immutable-string contract to preserve,
  so a function may take a `string` parameter and write through it exactly like
  any other byte slice.
- **There is no aliasing: a subslice is a copy.** `s[a:b]` allocates and copies.
  A write through a subslice does NOT reach the parent, so Go code that relied
  on overlapping views - in-place XOR, "read src fully before writing dst",
  `copy` overlap reasoning - is wrong here. Take the value you mean to change
  and write it back, or build the result in a fresh slice.

For the compiler this means: element loads and stores are bounds-checked for
slices and strings (forming an address from an index is not), and there is no
slice-to-array or array-to-slice conversion machinery to lean on - code that
needs one works in slices.

## Type System: No int/uint, Builtin Stringers

`int` and `uint` are **undefined**, not restricted - they are not predeclared
in Moxie, so `var x int` resolves as `undefined: int` and a user may declare
their own `int` type. Always use explicit widths (`int32`, `int64`, `uint32`,
`uint64`). Slice/array indexes and `len()`/`cap()` return `int32`.

The same model applies to other removed Go names: `make`, `new`, `append`,
`complex`, `real`, `imag` and the types `complex64`/`complex128` are undefined
(not restricted) - the loader lowers `[]T{:n}` / `chan T{}` to the internal
`__slicealloc`/`__ptralloc` builtins, and `push` replaces `append`. `uintptr`
is a normal predeclared type (the pointer-sized integer for `unsafe.Pointer`
arithmetic). The `strings` package is dropped - `bytes` covers it under the
string=[]byte unification.

All builtin types (`int32`, `int64`, `uint32`, `uint64`, `int8`, `int16`,
`uint8`, `uint16`, `float32`, `float64`, `bool`) have `String() string` methods,
satisfying `fmt.Stringer`. This is how `fmt` works without reflection or empty
interfaces - `fmt` variadics use `...Stringer` not `...any`/`...interface{}`.
Each argument's `.String()` method is called directly:

```moxie
n := int32(42)
msg := "count: " | n.String() | " items"
```

## No Reflect, No Empty Interface (any)

`reflect` and the empty interface (`any` / `interface{}`) are **removed** from
Moxie. They are not merely unimplemented - they are forbidden. The empty
interface leads to bad code; its absence is tied to the same design decision
that removed `reflect`. In Moxie everything you can print has a `Stringer`
(builtin stringers exist for every builtin type), and everything that crosses
a boundary has a builtin codec (required for function returns and spawn
channels). The empty interface is the seam where that machinery would be
needed and is deliberately absent.

**Generic type constraints must be concrete and non-empty.** A type parameter
may be constrained by:
- a type-set constraint, e.g. `cmp.Ordered` (`~int32 | ~int8 | ~int16 | ~int64 | ~uint32 | ~uint8 | ~uint16 | ~uint64 | ~uintptr | ~float32 | ~float64 | ~string`), or
- a method interface with at least one method.

Never `any` / `interface{}` as a constraint. Never `any` / `interface{}` as a
field, parameter, or return type.

**Open work (not yet done):** the stdlib still contains `any` in generic
signatures and must be refactored to concrete constraints. Known offenders:
`iter` (`Seq[V any]`, `Seq2[K,V any]`, `Pull`/`Pull2`, plus plain `any`
panic-value storage), `weak` (`Pointer[T any]`, `Make[T any]`),
`crypto/internal/fips140cache` (`Cache[K,V any]`), and generic functions across
`maps`/`slices`/`bytes`/`strings`/`internal/trace` (`maps.Clone[V any]`,
`maps.Keys[V any]`, etc.). Iteration element types take `ordered` (`cmp.Ordered`),
same as slices/sort. Reuse or expand an existing stdlib interface where one
fits; otherwise define the minimal concrete constraint the use case demands.

The compiler currently papers over this with a **workaround, not a fix**: in
`_mxc_stage4/main.mx` (discoverPkg, ~line 1722) any file importing `iter`,
`weak`, or `crypto/internal/fips140cache` is skipped wholesale. This silently
drops those files (and their symbols) from the build. The real fix is the
refactor above; removing that skip-list is the completion condition, not the
starting point.

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

## Spawn Architecture: Managers and Workers

Spawn is heavyweight (fork + socketpair + ring pairs). Goroutine conventions do
not apply. Every domain in a spawn tree takes exactly ONE role:

- **Manager**: owns an event loop. It spawns workers, owns the channels, and
  multiplexes signals. The loop is tight - poll, identify the channel that
  fired (and whether it was a close or a signal), dispatch - for low latency.
- **Worker**: a long-lived server. Spawned once, then serves messages
  (queries, commands) over its inbox channel and watches its role breakers
  (cancel, deadline, timeout). It never spawns on demand and never manages
  other domains.

A domain is never both at the same time. The spawn tree therefore has
distinct element types: manager nodes and worker nodes.

**Channel handles are fixed at spawn time.** Spawn does not allow passing
channel handles to a domain after it has started. Everything the spawned
domain will ever receive must be among the channels passed in the spawn
call. Everything it will ever send goes over those same bound channels.
Post-spawn communication is messages over already-bound channels, never new
handles.

Therefore the timer pattern: the function that needs timer-driven signals
creates the signal channels **in its own function scope** and keeps them.
It spawns the timer service, passing those channels in at spawn. When a
timer fires, the service multiplexes the event back over the spawn channel
(a message identifying which channel and close-vs-signal); the caller's
event loop consults its channel list and does the processing - closing the
matching channel in its own scope and forwarding the signal to the workers
it spawned to listen. The service is a worker; the caller is a manager.
Timer registrations are messages; timeouts are never per-wait spawns.

**Workers are servers.** Per-job spawns are invalid. A worker runs a select
loop over {inbox, role breakers} and replies over the pipe-bound ring pair
correlated by id. Queries and commands are the only inbound traffic.

**Share by communicating, not communicate by sharing.** There is no shared
memory between domains; channels (ring buffers in MAP_SHARED memory) are the
only inter-domain state. Function values and closure captures cannot transit
a spawn boundary - only code pointers, codec-serialized values, and channels.
A closure lives in its function arena and may only receive data read-only
via parameters; if mutation is required, use a sovereign self-mutating type.

**Breakers.** Cancellation is a `chan struct{}` breaker: `close(breaker)`
propagates to every domain holding it (pipe-bound close signal fan-out).
A nil breaker means never canceled.

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
  3. Bake the runtime from source with the compiler just built: `MOXIEROOT=$PWD ./mxc-stage4 build-runtime`
  4. Compile stage4 with the first bootstrap: `MOXIEROOT=$PWD ./mxc-stage4 build -a -o mxc-stage4-2 ./_mxc_stage4`
  5. Compile stage4 with the second bootstrap: `MOXIEROOT=$PWD ./mxc-stage4-2 build -a -o mxc-stage4-3 ./_mxc_stage4`
  6. Verify a generation reproduces itself: `sha256sum mxc-stage4-2 mxc-stage4-3` (should match)

`sysroot/runtime_go.bc` is an INPUT to application links, not a build product:
it is only read when the runtime is not itself in the build closure (i.e. for
apps, not for the compiler, which compiles the runtime from source). Nothing
regenerates it automatically, so it drifts from `src/runtime` and apps then
fail to link with undefined symbols. Step 3 exists so the bake is a product
of the bootstrap rather than a distributed blob - no prebuilt runtime needs
to be shipped.

The bake is compiler-specific: the legacy compiler writes a different blob
(different symbol set and linkage) which will NOT link against stage4's
expectations. Always bake with the compiler that will consume it.

LLVM is pinned to major version 22 across the stack: legacy links
libLLVM-22, and stage4 invokes clang-22/llvm-link-22/ld.lld-22 plus
mem2reg via opt. build.sh verifies the major version of each tool rather
than trusting the name.

The fixpoint is bootstrap-against-bootstrap: some generation must emit the
identical binary that emitted it. The legacy compiler is a reference
implementation and a way to build Moxie from scratch, not a codegen oracle -
`mxc-stage4` (legacy-built) is NOT expected to equal `mxc-stage4-2`.

Convergence may take one generation longer than the minimum, because the
legacy pipeline differs (it runs interp + LowerInterfaces + globaldce) and
that difference is inherited by the first bootstrap. Measured with a
relative-path chain: s1 != s2 but s2 == s3 == s4. So `stage2 != stage3` is
not by itself a failure; a generation failing to reproduce itself
(`stageN != stageN+1` for all N) is. tests/phase2_selfhost.sh accepts either
`stage2 == stage3` or `stage3 == stage4`.

Do not use `sha256sum mxc-stage4 mxc-stage4-2` as the fixpoint check. A
mismatch there is expected and carries no signal.

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
