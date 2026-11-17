# region.md — adopting Tofte & Talpin region inference as Moxie's memory model

> **TEMPORARY FILE. DELETE BEFORE COMMITTING THE CHANGES IT DESCRIBES.**
> It exists so the work can be executed from a written plan instead of from a
> conversation. When Stage H is complete, `git rm region.md` in the same commit
> as the final change, and let `docs/memory.md` carry the durable record.

**How to use this file.** Say *"do what it says in region.md, and note into the
document when you complete the steps"*. Each stage has a `Status:` line, a list
of sub-todos as checkboxes, and an **Acceptance** paragraph. A sub-todo is
ticked only when its acceptance evidence is written into the Completion log at
the bottom (command, number, file). Never tick a box on recollection; the
project has already paid for that mistake twice.

**Status legend.** `todo` · `in progress` · `blocked (reason)` · `done (evidence
in log)`.

---

## 0.0 The constitution this plan is subject to

`docs/axioms.md` is not guidance. Its three axioms are constitutional, and a
stage that opposes any one of them is not Moxie and is not built — there are no
exceptions, and "it was faster" is not an argument against a constitution.

| axiom | what it forces here | what this plan must not do |
| --- | --- | --- |
| **A1 Just the facts** — every property that governs execution is declared and statically checkable; nothing is inferred, negotiated or discovered at run time (L7: *declare, don't infer*) | every region, effect and capacity is **declared**, and the compiler **checks** it | no region inference, no constraint solver, no ⊤ minted because a solver failed to converge |
| **A2 Total isolation** — no component addresses another's state, and every fact has exactly one authoritative owner | one authoritative owner per region; a region's contents have one holder | an address-keyed second authority (which is what a sovereign group keyed by receiver *address* is), and two regions naming one object |
| **A3 Closure** — every capability and input is declared, the declaration is exhaustive, default deny (L34: *authority is a parameter, not a context*) | the region an allocation lands in is a **parameter**, named at the site and checked; a region that was not named is unreachable | an ambient *current* region that any code may allocate into without naming it |

**This plan was written as an inference plan, and that was the defect the axioms
catch.** §1.4 and Stage C originally specified a fixed-point solver over effect
rows with ⊤ as the escape when it did not converge. That makes a
property which governs execution the product of an algorithm, which A1 and L7
forbid: an inference cannot be refuted because nothing states what it should have
been, while a declaration can. The solver is therefore deleted from the plan, and
what replaces it is smaller and stricter:

- the **declaration** is the effect row and the region on the type (§4.2, §4.3);
- the **check** is R-store and R-escape, decided by a syntax pass over declared
  facts (C1–C3), exactly as A1's justification requires ("the placement check is
  decidable: spawn sites are syntactic, the topology is syntactic, and comparing
  them is a syntax pass");
- **⊤ is a declaration too** — a signature that says *this call may allocate
  anywhere* — and the build counts the declarations, so the number is still the
  migration bar, but it is a fact about the source, not about the solver's mood.

Stage A is unchanged by this, and it is where the third violation lives: today
the region an allocation lands in is the ambient `CurrentArena`, so a value's
placement is a context rather than a parameter. A.2's job is therefore not to
re-create the ambient stack under a better name — it is to make the region a
parameter, count every remaining ambient use, and drive that count to zero by
Stage F.

---

## 0. Why this file exists

The model is not wrong. It was arrived at independently, it is region-based
memory management, and it is half-implemented: regions exist at run time, are
created and destroyed by hand, and are **not in the type system at all**. Every
memory defect this tree has is a consequence of that one gap.

Measured evidence, all from the compiler's own self-build (details and the
per-phase ceiling table in `docs/memory-budget.md`):

| observation | number |
| --- | --- |
| arena reserve at the abort | 3.22 GB live mapped |
| resident memory at the same moment | 717 MB |
| one package's SSA build, mapped growth | 1.9 → 2.8 GB, arena capacity flat at 262 MB |
| one package's parse | 177 MB for 1.5 MB of source |
| the same input, three runs | 775 MB / 1037 MB / 1898 MB peak |
| hand-placed arena creations in the compiler | 10 sites, sizes 4/8/16/64 MB, `len(src)*50`, `len(src)*200+4MB`, `len(src)*64` |
| `SetCurrentArena` call sites | 130 |
| `ManualArenaEnter` call sites | 14 |
| mutating methods (sovereign-group owners) | 319 |
| root-arena escape helpers (`keepStr`, `rootCopyStr`, …) | 8 |

Read through the filesystem lens, because the correspondences are exact and they
give the checklist. A region is a mount. `Function entry/exit` is a tmpfs mount
whose lifetime is the call. `ArenaFree` is `umount` followed by `rm -rf`. The
ledger (`RecordSegmentAdd` / `RecordSegmentFree`, `sysMapBytes` /
`sysUnmapBytes`) is `df`. `keepStr` and `rootCopyStr` are "copy it into the
read-only root because it must outlive the mount". Then the four failure classes
are mechanical:

1. **use after unmount** — a value referenced after its region died: the
   `sovMarkReceiverRoots` fault that read the string `"part of "` as a type
   descriptor.
2. **aliasing across mounts** — two regions naming one object: the disabled
   `dropGraphsBefore` and the dangling `*Type` it was disabled for.
3. **double unmount** — `sovGroupList` / `sovGroups` drifting, the
   `arena exit orphaned sovereign groups` panic.
4. **accounting drift** — `liveArenaCapBytes` walks only the fn stack and the
   pool, so it reported a flat 262 MB while 2.8 GB was mapped. `df` that only
   counts the filesystems that happen to be in the mount table.

The thesis of this plan: **make regions static, and three of those four classes
stop being possible** — not "fixed", not "guarded", unimplementable.

---

## 1. The model being adopted

Region-based memory management, Tofte & Talpin (1997), building on the
type-and-effect discipline of Talpin & Jouvelot (1992), implemented in ML Kit
and in Cyclone. The pieces that matter here, and only those:

### 1.1 Regions and `letregion`

A region `ρ` is a storage area with a *dynamic extent* fixed by a lexical
construct:

```
letregion ρ in e
```

`ρ` is allocated on entry to `e` and deallocated on exit. Allocation into `ρ` is
a *may* effect — an expression may allocate into a set of regions listed in its
effect. There is no per-object free, no compaction, no collector: deallocation
is the scope exit, and it is bulk.

Moxie already has this construct under another name: `FnArenaPush` at entry,
`FnArenaPopFree` at exit, with the codec's encode/pop/decode as the escape
protocol for the values that outlive the frame. The `letregion` we do not have
is the *named* one, and the type system that makes it safe.

### 1.2 Effects on function types

A function type carries an effect:

```
f : (τ₁, …, τₙ) --ε--> τ′      where ε is a set of region variables
```

`ε` is what the call may allocate into (and, in the extended form used by ML
Kit, what it may touch). Effect variables are ordinary unification variables:
they appear in `Signature`, they are solved, and they are **generalised at
binding sites**, which is what gives region polymorphism — one `map` usable at
whatever region its caller chooses, instead of one `map` that copies.

This is the piece Moxie is missing, and it is *why* the return protocol exists.
Without an effect on the signature, a callee cannot be told where its result
belongs, so the runtime encodes the result in the callee's region, pops that
region, and decodes into the caller's. That is a correct dynamic answer to a
question static inference answers for free — and it costs a deep copy on every
return that is not on the `codecRetStable` fast path.

### 1.3 The outlives order, and the two rules that do all the work

Regions form a partial order `ρ₁ ⊑ ρ₂` ("ρ₁ outlives ρ₂" — ρ₂ dies no later
than ρ₁). Two rules:

- **R-store.** Storing a value that lives in `ρ_v` into a container that lives in
  `ρ_c` requires `ρ_c ⊑ ρ_v`. You may put a young thing into an old region; you
  may not put an old thing into a young one.
- **R-escape.** A value of a type whose region is `ρ` may not be reachable after
  `letregion ρ` exits. `letregion` only introduces names that its body cannot
  leak, which is enforced by not allowing `ρ` in the type of a result.

Everything else — no dangling pointers, no use-after-unmount — follows from those
two plus the effect discipline. There is no runtime check and no fast path
because there is nothing to check at run time.

### 1.4 Declaration and checking (not inference)

T&T's inference algorithm is **not** adopted, because A1 and L7 forbid it: the
region a value lives in governs execution, so it must be declared and checkable,
not computed. What is adopted is T&T's *safety machinery*, which is the part that
makes the declaration checkable at all:

1. **Every owned type states its region**, and every signature states its effect
   row: the regions the call may allocate into and the region its result is
   placed in (§4.2, §4.3). A signature that states nothing is not implicitly
   general — it is **⊤**, the declared unknown effect, and it is a written fact
   about that function rather than the residue of a failed solve.
2. **R-store** is checked where a value is stored: the container's region must
   outlive the value's region, and "outlives" comes from the declared order
   (root at the bottom, the frame stack as the order between `letregion`s).
3. **R-escape** is checked at `letregion` exit: no declared type may mention a
   region the body introduced.
4. Both checks are **syntax passes over declared facts** — the same kind of check
   A1's own justification describes for spawn topology ("comparing them is a
   syntax pass"), and decidable for the same reason.

Generality is bought by **declaring a region parameter** on the function, not by
a solver discovering that one is possible: a function whose signature names its
region variable is region-polymorphic at every call site that supplies one. The
generalisation restriction (§1.6) is then a rule the checker enforces — a
signature may not quantify a region that appears free in a type it stores — and
imperative type variables are its form for mutable receivers.

**⊤ is not the escape hatch; it is the debt column.** The build counts functions
whose declared effect is ⊤, per package, and that count is the migration progress
bar. It goes to zero for the standard library because every one of those
signatures gets a region written into it, not because a solver improves.

### 1.5 What T&T does not give us, stated up front

- **No first-class regions.** ML Kit and Cyclone both added them (region handles,
  `mkregion`/`region` values). Moxie needs them, because a self-mutating
  receiver owns storage whose lifetime is the receiver's, not a lexical scope.
  That is the "owned region": a region handle stored in a value, with a
  destructor. The static discipline is still T&T's: the handle's type carries
  its region, and R-store/R-escape apply unchanged. Cyclone's `region` + `@`
  annotation is the model to follow, with ML Kit's region handles as the
  implementation reference.
- **No cyclic or mutable-update story.** T&T assume mostly-functional data.
  Moxie has mutable receivers and cycles. Handled by R-store plus the owned
  region, not by a new mechanism.
- **No concurrency.** Moxie's domains are threads. A region belongs to a domain;
  across `spawn`, values are copied by the codec, which is already the rule
  ("values are copied; channels are shared").
- **Known failure modes to design against**, because they are ours already:
  effect inference becomes useless when one function in a cycle is ⊤ (one ⊤
  poisons its callers — mitigate by inferring per package and by keeping FFI
  wrappers thin); and region polymorphism is lost if a function is used at two
  incompatible regions, which surfaces as a copy rather than an error.

### 1.6 The impossibility results that actually apply — and their price list

Not FLP and not CAP. Those are results about **missing information under an
adversary**: no process has a global view, and the adversary may withhold exactly
the message you would need. A compiler has the entire program, in one place, at
one time, and no adversary. There is no partition to be tolerant of, and citing
those results here would be an obstruction in the way of the work rather than a
description of it.

The results that do bear on this plan, each with what it costs:

| result | what it forbids | what we pay |
| --- | --- | --- |
| **Rice's theorem** | any non-trivial semantic property is undecidable, so "the compiler knows the whole lifetime graph" cannot hold for an unrestricted language | restrict the language until the property is syntactic. Not an impossibility — the price of admission every type system pays |
| **non-interference is undecidable** | "isolated" as *no flow except declared channels* cannot be checked in general | obtain it by typing the flows, not by analysing them |
| **effects + generalisation** | a type containing a free region variable cannot be generalised, so region polymorphism and sound inference do not compose without a restriction | **Tofte & Talpin's generalisation restriction**, applied at C.3: generalise only a type whose region variables are not free. The imperative case is ML Kit's **imperative type variables** (Birkedal, Tofte, Vejlstrup), polymorphic only where the binding is a syntactic value. This is *not* ML's value restriction — that one is a let-polymorphism-plus-mutable-references rule, and citing it here would have a future session implement the wrong rule |
| **resource bounds** | closed *and* bounded needs one of: linear types, refinement types over sizes, a runtime check, or **a declared capacity plus a static accounting pass** | Moxie's mechanism is the fourth, and it is the one already in the tree: `shelf[Entry]{ 1 << 20 }` and the per-phase budget gate are a declared capacity checked by a pass over the program's own structure. Neither linear nor refinement typing, and not a runtime check. A list of three that omits it describes a language that is not this one |

So **explicit and closed compose fine**. The real trade is *closed versus
general*, and every closed system in history paid in expressiveness. Moxie pays
it in specific, nameable places: fixed tiers, literal capacities
(`shelf[Entry]{ 1 << 20 }`), no reflection, no dynamic code loading. This plan
must not pretend to buy closure for free, and §7's stop conditions are the
places where the price comes due.

**Where the closure stops: interop.** Other languages are not a mess from
ignorance — they must call libc, POSIX and the C ABI, all of which are ambient
authority by construction. Moxie's closure is therefore not total, and the plan
must say where the edge is instead of implying it does not exist. The edge is
policed, not proved:

- every extern carries ⊤ (§C.4), so nothing with a static region crosses it;
- the only sanctioned route in and out is the codec/copy protocol plus the arena
  API, so a value that does cross is owned by its destination region by
  construction;
- FFI wrappers stay thin, which is what keeps one ⊤ from poisoning a package.

A plan that claims closure without this paragraph claims something false.

**The maxim that keeps this decidable**, and the guard against the failure mode
that would kill the whole thing: *the compiler's question must stay decidable,
and the depth belongs in the library.* Anything needing more precision than
region inference gives — a size, a linearity proof, a refinement — belongs in a
library type a program opts into, never in the compiler's analysis. A future
session that reaches for refinement types to make a bound exact is the failure
this sentence exists to stop.

**Where it actually gets hard: channels.** For a sequential program, closure is a
typing problem. Once processes are created at run time and values cross between
them, the lifetime graph depends on the *communication topology*, and that
analysis is undecidable in general — which is why Erlang copies and Pony types
the capabilities. Moxie already does both halves of the known-good answer
("values are copied; channels are shared", and the codec is the relocation), so
across `spawn`, closed is a **discipline, not a theorem**. Stage D's placement
must not reach across a spawn boundary; that is a rule of the plan, not a proof.

**The bill, in one place: what happens when a declared capacity is exhausted.**
Every closed capacity needs a defined failure mode, and the two halves of the
tree disagree today. Verified in code, not remembered:
`arenaOpenSegment` (`src/runtime/alloc.mx:4972`) grows the region table when it
is full (`arenaGrowRegions`) and chains a new segment; the only panic on the
growth path is `arena: region table full`, when the table itself cannot grow —
a resource limit, not a declared bound. So a region's declared size is advisory
and its number is a reservation, while a channel frame that encodes past its
budget **panics**, naming the actual size, the budget and the cap tags. That
disagreement is not cosmetic: G.4's per-phase budget gate cannot be written
against an advisory number, and D8 ("sizes are guesses") is the same defect seen
from the other end. Stage G must pick one and write it down:

- **(a) capacity is a measurement.** Regions keep chaining; the number is a
  budget the `df` report compares against, and exceeding it is a *reported*
  event. Cost: no static bound, so "closed and bounded" is false at run time and
  the plan must say so.
- **(b) capacity is a bound.** A declared capacity that is exceeded fails loudly,
  naming the region and the code position that created it. Cost: every
  hand-placed size in §2.5 becomes load-bearing, and the 10 sites must be derived
  from measurement before the gate is switched on.
- **(c) assert in debug, chain in release, ledger as the bound** — the option
  most systems pick, and the cheapest here. The declared number is checked when
  checks are on; the real bound is the segment ledger
  (`RecordSegmentAdd`/`RecordSegmentFree`, `SysMapNet`), which is exactly what
  A.3 makes honest. Cost: it needs A.3 first, and it makes a build's failure
  depend on whether assertions are enabled, which must be stated rather than
  assumed.

The plan does not get to leave this implicit, because it is the one place where
"closed" turns from a typing claim into an operational promise.

---

## 2. What Moxie is today

Not a strawman: most of the machinery exists and is load-bearing. The survey is
by file, because the plan's stages touch these exact places.

### 2.1 The region stack that already exists — `src/runtime/alloc.mx`

- `domainRoot.fnStack[512]` with `fnTop`; `fnStack[0]` is the root arena.
  `FnArenaPush(65536)` (l.444) / `FnArenaPop` (l.462) / `FnArenaPopFree` (l.488).
- `CurrentArena` (l.527) and `SetCurrentArena` (l.536).
- Segments and their classes: `segKindFn` / `segKindRoot` / `segKindLarge`,
  fn chunks to `segFnChainMax` = 4 MB, root segments of `arenaRootSegCap()`,
  phase/owned arenas to `arenaPhaseChainCap()` = 64 MB native (4 MB wasm).
  Growth in `arenaOpenSegment`; region-table growth in `arenaGrowRegions`.
- Pools: `arenaPool` (64 entries, 8 MB total), sovereign pool (32 entries, 4 MB).
- Ledger: `RecordSegmentAdd` / `RecordSegmentFree`, `sysMapBytes` /
  `sysUnmapBytes`, `liveArenaCapBytes` (walks the fn stack and the pool only),
  `ArenaUsed`, `ArenaRootUsage`.

This is a region stack with an outlives order that happens to be *the stack
order*, and it is correct as far as it goes. It is not named, not typed, and not
visible to the compiler.

### 2.2 Region kinds that exist today

| kind | created by | dies at | lexical? |
| --- | --- | --- | --- |
| frame | `FnArenaPush` | `FnArenaPopFree` / `ArenaRelease` | yes |
| root | `initDomain` | domain exit | no, but process lifetime |
| manual | `ManualArenaEnter` (14 sites) | `ManualArenaExit` + `ArenaFree` | **no** — entered and exited by hand |
| owned | `codecSovereignArena` (319 mutating methods) | `sovReleaseGroup` via arena teardown, compaction, or never | **no** |
| graph | `graphArenaFor`, `dispTier2Arena` | `dropGraphsBefore` (disabled) | no |
| parked | `ArenaReset` in `chanpool`/`chanpath`/`ir_emit` | reuse by the next frame | no |

Six kinds, two of them lexical. Four of them are places where a region's
lifetime is decided by a heuristic instead of a scope, and all four failure
classes live there.

### 2.3 The escape machinery that substitutes for static placement

- Return protocol: encode in the callee, `FnArenaPopFree`, decode in the caller
  (`emitRetWithCodec`, `_mxc_stage4/ir_deepcopy.mx`), with `codecRetStable` as
  the fast path; `src/runtime/codec.mx`.
- Root escapes: `keepStr`, `rootCopyStr`, `rootStrMap`, `rootArenaMap`,
  `rootPkgMap`, `rootFlagMap`, `keepStrs` (`_mxc_stage4/main.mx`) — eight
  helpers whose whole purpose is "this value must outlive the region it was born
  in, so copy it into the root".
- Compaction as a GC substitute: `SovDrainCompactions`, `sovFullMark`,
  `codecSovereignCompact`, the queue, the ratchet, the backoff, the escape
  leases. A collector, by another name, for the one region kind whose lifetime
  is not lexical.

### 2.4 The type system and the export format

- `pkg/types/tc_types.mx:343` — `Signature { TargetDeclared, Recv, Params,
  Results, Variadic }`. **No effects, no region variables.**
- `_mxc_stage4/main.mx:4692` `generateMxh` writes a line-oriented text format:
  `package`, `import`, `func`, `type`, … There is **no format marker**, so
  adding an effect field is a silent cache-invalidation hazard, not a version
  bump: an old `.mxh` read by a new compiler would parse with effects missing.

### 2.5 Where the compiler creates regions

Ten `ArenaNew` sites with hand-chosen sizes (`_mxc_stage4/{main,ir_typecheck,
ir_emit}.mx`), the per-function `ssaFuncBuilder` group (one owned region per
function body, 319-method pattern), and 130 `SetCurrentArena` sites. Every size
is a guess and every guess is a reservation: `len(src)*1000` reserved 512 MB for
`runtime` and the package then needed more anyway.

---

## 3. Divergences, and what each one costs

| id | divergence | cost, measured |
| --- | --- | --- |
| D1 | regions have no identity or kind the compiler can name | the compiler cannot express "this value belongs in that region", so it copies or guesses |
| D2 | no effect on `Signature` | every non-stable return is a deep copy; `codecRetStable` exists to dodge it |
| D3 | `SetCurrentArena` **replaces** the stack top instead of pushing | a region can stop being on the stack while live — the exact hole that made 40+ receivers unattributable and faulted on `"part of "`; papered by `liveArenaHead` |
| D4 | two non-lexical kinds (manual 14, owned 319) | deallocation by heuristic: hold tests, external-ref tests, ratchets, backoff, compaction |
| D5 | owned data keyed by receiver *address* | relocation needs `sovRekeyRelocated`, collisions need dropping a group, cycles across owners are unrepresentable |
| D6 | no outlives relation in types | `dropGraphsBefore` cannot be switched on; graphs retained: 3.22 GB mapped, 1.9 GB RSS |
| D7 | no region polymorphism | a generic function returning pointer-bearing data copies at every return: 2 GB in `math.Erfinv`, 3.2 GB in `markReturnEscapes` (already recorded in the tree's comments) |
| D8 | sizes are guesses | reserve ≫ use: 3.22 GB mapped for 717 MB resident; the compiler aborts on its own 3 GB ceiling |
| D9 | the ledger walks only the fn stack + pool | `acap` flat at 262 MB while `net` grew to 2.8 GB; the one number that would have named the leak was blind to parked regions |
| D10 | the SSA builder allocates per-function scratch into a receiver-owned region | 2.5 GB and ~10 minutes for one package, quadratic in the abandoned growth |

The ordering that follows is by **distance from the foundations**: the things
that must be true before anything else can be expressed (region identity, then
region types), then the things that can only be built once those exist
(inference, placement, owned regions), then the deletion of what the new
machinery makes unnecessary, then the compiler eating its own food.

---

## 4. The target architecture

Small enough to state completely:

1. **A region is a first-class name at compile time.** `RegionId` with a kind
   (`frame`, `root`, `owned`) and a parent link forming the outlives order.
   Lexical regions are `letregion`; the frame stack is the `letregion` a call
   emits, and the region an allocation lands in is a **parameter** of the site,
   not an ambient current value (A3, L34).
2. **Types carry regions where storage is owned:** a pointer or slice whose
   backing lives in `ρ` is `… at ρ`. Types without owned storage do not.
3. **Signatures carry effect rows:** `f : (τ…) --ε--> τ′`, where `ε` names the
   regions the call may allocate into and the region results are placed in.
4. **Declaration, then check** (A1, L7). Every owned type's region and every
   signature's effect is written down; R-store and R-escape are decided by a
   syntax pass over those declarations. ⊤ is a declared unknown, counted per
   package, and its count is the migration bar. There is no solver.
5. **Placement replaces copying.** A call to a declared function tells the
   callee which region to write into — that is authority as a parameter. The
   encode/decode protocol remains only for ⊤ calls, and the counter of those is
   the migration metric.
6. **Owned regions replace sovereign groups.** A region handle is a value; its
   destructor runs when the handle dies (statically known scope); R-store and
   R-escape are checked. One owner, not an address-keyed second authority (A2).
   Compaction, rekeying, hold tests and external-ref tests are deleted.
7. **Deallocation is scope exit.** No compaction. No collector. The `df`-like
   ledger becomes a first-class budget report per region and per phase.

---

## 5. The plan, stage by stage

Stages are in dependency order. Nothing in a later stage is legal before its
predecessors are done: region types without region identity are names without
referents, and a checker without region types has nothing to check. Every stage's
acceptance also carries the §0.0 constitutional check for its axioms — a stage
that passes its memory target by violating A1, A2 or A3 has failed.

Execution order, by distance from the foundation:

| # | stage | distance | one line |
| --- | --- | --- | --- |
| 0 | precondition | outside the refactor | a green build and a baseline binary, so the rest can be measured |
| A | region identity and kinds | 0 — the foundation | a region becomes a name; the region becomes a parameter |
| B | region variables in types | 1 | a type can say *which* region owns its storage |
| C | declared effects and the check | 2 | effects written on signatures; R-store/R-escape refuted by a syntax pass; ⊤ counted |
| D | placement in codegen | 3 | the callee writes into the caller's region; copying only for ⊤ |
| E | owned regions | 4 | first-class region handles replace sovereign arenas; the collector is deleted |
| F | delete ad-hoc kinds | 5 | manual/graph/parked regions become scopes |
| G | dogfood the compiler | 6 | the compiler's own phases, and the 1 GB self-build |
| H | verification and deletion | 7 | tests per failure class, WOOD LAW audit, `git rm region.md` |

### Stage 0 — precondition: a green build and a baseline binary outside the tree

*Not part of the refactor. It exists because a refactor cannot be measured from
inside a build that does not finish, and because the last two days were spent
concluding things about a compiler that could not answer.*

Status: `in progress (0.2: floor named and reduced 200MB; next wall strconv.initPow10, 188MB transient; see log 0.2g-0.2j)`

- [x] 0.1 Land the two lifetime patches already designed and measured, because
      they are prerequisites for measuring anything else: per-function scratch in
      the SSA builder (`SovCompactGroup` at the end of `buildFunc`) and the
      graph-release review for `dropGraphsBefore`. Each is correct on its own
      terms, not a step toward the model. **Outcome: both reviewed, one rejected
      and one rewritten.** `SovCompactGroup` was measured as a permanent no-op
      (`try=297 ok=0 ext=297`) and deleted; the graph release was rewritten from
      the last-use rule (which never fires, because `main` is everyone's last
      user) to the closure rule, `retainGraphsOnly`, and verified to fire 192
      times in one cold pre-pass with no dangling type. **The rule is applied in
      the pre-pass only**: applying it in the compile loop too produced
      `undefined: bits` in `math`, because a *retained* package's direct-import
      record is emptied by `clearGraphCaches` and never re-parsed, so it loses
      its imports while its types stay live. That is a real defect with a named
      fix (re-register a retained package's imports from its live graph) and it
      is recorded in the log rather than papered over by dropping less. See the
      log.
- [ ] 0.2 Produce a compiler that completes its own `-a -no-main-arena` build
      inside the 1 GB box, and copy it to `~/mxc-builds/mxc-baseline-<date>`.
      That file is the baseline: every later stage compares against it.
      **Not reached.** Best boxed result: 910 MiB cgroup peak, wedged in `main`'s
      pre-pass type-check at 925 MB RSS in state `D`, two distinct causes named in
      the log. The tree does not currently have a compiler that can self-build
      cold inside 1 GB.
- [ ] 0.3 Record in `docs/memory-budget.md` the baseline's phase ceilings
      (`VmHWM` per phase, mapped peak, wall clock) using the existing probe
      harness and the `VmHWM`-reading gdb script. **Round-2 ceilings recorded**
      (`docs/memory-budget.md`, "Round 2"), including the wedge point, the 910 MiB
      peak and the 2.7 GB gdb abort; the *baseline binary's* table is still owed
      because 0.2 produced no baseline.

**Acceptance.** `~/mxc-builds/mxc-baseline-*` builds itself boxed, exit 0; its
phase table is in the log. If 0.2 cannot be reached, the plan stops here and the
blocking defect is written up, because Stages A–H would be measured from a
broken tool.

**What "stops here" means, and why the plan continues anyway.** 0.2 is not
reached, so its defect is written up (§ the log, and `docs/memory-budget.md`
"Round 2"). But the write-up's own conclusion is that the remaining terms cannot
be attributed, because the ledger sees only the fn stack and the pool. The plan
therefore continues with **A.3 and A.4 first**: they are the instrument that makes
0.2's blocker measurable, so doing them is how the stop is lifted rather than
bypassing it. No stage past A.4 proceeds until 0.2 is green.

### Stage A — region identity and kinds in the runtime (foundation)

*Distance from the foundation: zero. Nothing else can be expressed until a
region is a name.*

Status: `todo`

- [ ] A.1 Introduce a `Region` descriptor: `id`, `kind` (`frame`/`root`/`owned`),
      `parent`, `cap`, `used`, `live`. The existing `Arena` becomes the
      descriptor's storage; do not add a second allocator.
- [ ] A.2 Make the region a **parameter**, and the stack a real stack.
      `RegionPush`, `RegionPop`, and `RegionCurrent`; the 130 `SetCurrentArena`
      sites become an allocation that names its region, or a `RegionSwitch` that
      records a parent link instead of overwriting `fnStack[fnTop]` where a
      switch is genuinely the local meaning. **A3/L34 makes this the point of the
      item, not a side effect**: `SetCurrentArena` is ambient authority over
      placement, so what survives as a compatibility path is explicitly named,
      counted per build, and driven to zero by Stage F.
- [x] A.3 Fix the ledger (D9): `liveArenaCapBytes` and the budget report walk
      **all live regions**, not the fn stack and the pool. The report must agree
      with `/proc/self/maps`, and say so when it does not. A ledger that cannot
      see a region is a measurement defect (A1: the fact must be checkable).
      **Done, and the first honest run found two things.** (i) The walk now
      covers every live arena via `liveArenaLedger`, and the report prints
      `narena=`, `acap=`, `net=` and `dfgap=` (net - acap) at every phase, so the
      gap is the published number instead of an inference. (ii) The first run
      reported `narena=4096` - the walk's own bound - against 166 mappings, which
      is what a hand-maintained list looks like when a death path misses its
      `arenaLiveDel`: a freed arena stays linked, its address is reused by the
      next mmap, and re-adding it closes a cycle. The bound is gone; walks mark
      each node with a generation and stop at a repeat, printing `lrep=`. On the
      current tree `lrep=0` and `narena` reaches 6777, so the count is real: the
      compiler holds ~6800 live arenas at once, ~52 KB each, which is D8's
      fragmentation seen from the ledger side. **The 5 % wording is replaced by a
      checkable one**: `dfgap` is a bounded constant whose step is explained (it
      rises 26 -> 93 MB once a 64 MB domain stack is mapped), and it is printed
      at every phase so a later reader can disagree with the claim.
- [ ] A.4 Per-region accounting: bytes allocated, bytes live, allocations,
      largest allocation, and the code position that created the region.
- [ ] A.5 Once the stack cannot silently lose a region, delete the two
      compensations it needed: `liveArenaHead` entirely, and **only** the
      `sovRangeOwner` fallback inside `codecSovereignArena` (the range table
      itself stays — compaction uses it to find a record's owner). Attribution
      becomes the stack walk, which is D3 closed by construction rather than
      papered over.

**Acceptance.** The compiler builds and runs unchanged; `RegionCurrent` agrees
with `CurrentArena` at every probe; the budget report's total is within 5 % of
the `/proc` figure at every phase of a self-build; a diff of the compiler's
output (`-print-ir` on three packages) is byte-identical to the baseline's.

### Stage B — region variables in the type system (foundation)

*Distance: one. Types must be able to name a region before inference can solve
for one.*

Status: `todo`

- [ ] B.1 `pkg/types`: add region variables and effect rows. `Signature` gains
      `Effect *EffRow`; owned types gain an `at ρ` component (pointer, slice,
      map, chan, func-with-captured-storage, struct with owned fields).
- [ ] B.2 Effect rows: union-find with substitution, `⊤` (unknown) as the top,
      and a canonical rendering for export.
- [ ] B.3 Outlives constraints as a separate little lattice: `ρ_c ⊑ ρ_v`,
      with the stack order as the initial relation and the root at the bottom
      (outlives everything).
- [ ] B.4 The `.mxh` format: **add a format marker first**, then the effect field
      on `func` lines and `at ρ` on owned types. An mxh without the marker is
      rejected, not defaulted — a silent default here is how the interface
      dispatch table was lost before (`docs/decisions.md`, round 371).
- [ ] B.5 WOOD LAW gate: this changes the language's meaning, so `legacy/` gains
      the same `Signature` field and the same mxh emission **in the same
      commit**. Until then this stage is `blocked`.

**Acceptance.** A self-build produces an mxh with effects where every function is
`⊤`; the compiler builds itself from that mxh; `legacy/moxie` and `mxc-stage4`
emit byte-identical mxh for the standard library (WOOD LAW check, scripted).

### Stage C — declared effects, and the check (the engine)

*Distance: two. Needs B's variables and A's identities; produces the facts that
make D's work possible. It is a check, not a solver: A1 and L7 (§0.0).*

Status: `todo`

- [ ] C.1 Read the declarations. For every function, the effect row and the
      placement region come from its signature; a signature that declares
      nothing is ⊤ by rule, and the rule is stated in the diagnostic. Nothing
      about a function's effect is computed from its body.
- [ ] C.2 Check R-store over the typed AST: at every store, the container's
      region must outlive the stored value's region, `ρ_c ⊑ ρ_v`, decided from
      the declared order (root at the bottom, `letregion` nesting as the order
      between frames). A violation is a compile error naming both regions and
      the store's position — not a demotion to ⊤.
- [ ] C.3 Check R-escape at every `letregion` exit: no declared type in the
      result may mention a region the body introduced. **Also enforce the
      generalisation restriction** (§1.6): a signature may not quantify a region
      variable that appears free in a type it stores. The imperative case
      (mutable receivers) uses ML Kit's **imperative type variables**, polymorphic
      only where the binding is a syntactic value. This is not ML's value
      restriction, and the diagnostic names the generalisation restriction.
- [ ] C.4 ⊤ is declared, not assigned. The set is: FFI externs, `unsafe.Pointer`
      arithmetic, interface dispatch whose dynamic type is not statically known,
      and a generic whose type parameter is unresolved. Each is a source-level
      fact the checker records; ⊤ propagates upward because a caller of a ⊤
      callee is ⊤ for that row element, and that propagation is a printed
      derivation, not a solver state.
- [ ] C.5 Report: per package, how many signatures declare ⊤, and the
      effect-size distribution. This is the migration progress bar, and it is a
      count over the source.

**Acceptance.** All effects for `internal/itoa`, `math/bits`, `errors` are
concrete (0 ⊤ declared); the standard library's ⊤ fraction is measured and
recorded; **the check adds no more than 10 % to a cold self-build's wall clock**
(a syntax pass, not a fixed point); a deliberately wrong declaration in a test
package is *refused* with both regions named; `sov`-style faults do not appear.
Constitutional check: no declared fact is derived from a body, and no ⊤ is
produced by the compiler on its own authority.

### Stage D — placement in codegen

*Distance: three. The first stage that changes emitted code, and the first that
can be measured in megabytes.*

Status: `todo`

- [ ] D.1 Effective-region convention: the caller names the destination region
      for a call whose result has owned storage; the callee allocates there.
      Result values are no longer encoded and re-decoded for those calls.
- [ ] D.2 Keep the codec protocol for ⊤ calls, unchanged. Two paths, one
      obvious: annotated → placement, ⊤ → copy.
- [ ] D.3 Count both paths per build (`ret-copy` / `ret-place`), so the migration
      is visible from the binary's own output.
- [ ] D.4 WOOD LAW gate: legacy emits the same convention for the same
      signatures.
- [ ] D.5 Placement stops at the spawn boundary, and this is a rule rather than
      a proof: once values cross between domains the lifetime graph depends on
      the communication topology, which is undecidable (§1.6). Placement names
      the caller's region *within one domain*; across `spawn` the codec copy
      stays the relocation.

**Acceptance.** On a corpus of ten packages, `ret-copy` drops as ⊤ drops; RSS
during emit improves by a measured amount attributable to the removed copies; the
`-a` self-build still produces a working compiler and the string corpus
(`tests/regressions/should_compile`) still passes.

### Stage E — owned regions replace sovereign arenas

*Distance: four. The largest deletion in the plan, and the one that removes a
collector from the runtime.*

Status: `todo`

- [ ] E.1 `region` as a value: a handle type carrying its region; created with
      the equivalent of `letregion`/`mkregion`; destroyed when its scope is.
- [ ] E.2 R-store and R-escape checked by the type checker for owned regions:
      storing into an owned region requires the stored value's region to outlive
      it; a handle may not outlive its scope.
- [ ] E.3 Replace the sovereign machinery: `codecSovereignArena`,
      `sovMarkReceiverRoots`, `sovFullMark`, `codecSovereignCompact`,
      `sovDrainCompactions`, the pending queue, the ratchet, escape leases,
      `sovRekeyRelocated`. All of it becomes dead when no region has a
      heuristic lifetime.
- [ ] E.4 Re-express Moxie's authoring rule — "a mutating method's scratch goes in
      a free helper" — as a *checkable* property: scratch in an owned region is
      either dead by the end of the method or a diagnostic.
- [ ] E.5 Migration of the compiler's own 319 mutating methods: each one's
      per-call scratch moves to a frame region; only the receiver's own state
      stays in the owned region. This is D10's fix, and it is where the 2.5 GB
      goes.

**Acceptance.** `grep -c 'sov' src/runtime/*.mx` is zero for the functions listed
in E.3; the corpus passes; the self-build's mapped peak falls below 1 GB; the
R10 fault class is impossible to express (a test that previously faulted now
fails to compile).

### Stage F — delete the remaining non-lexical kinds

*Distance: five. Only legal once E gives owned data a static lifetime.*

Status: `todo`

- [ ] F.1 Manual arenas (14 sites) become named lexical scopes: a `letregion`
      binding the compiler emits, or a runtime API that is *only* usable in a
      scope-shaped way. No `ManualArenaEnter`/`Exit` pairs.
- [ ] F.2 Graph arenas: a package graph's region is owned by the link's region
      set, with the lifetime the outlives relation proves. Then re-enable
      `dropGraphsBefore` **as a region deallocation**, not as a free-from-a-map.
- [ ] F.3 Parked scopes (`ArenaReset` in `chanpool`, `chanpath`, `ir_emit`): an
      explicit "reuse scope" region kind, or a frame region with a documented
      reuse loop. One of the two, named.
- [ ] F.4 Delete the root-escape helpers (8) that exist only because regions
      could not be named; keep the ones that mean "process lifetime" and rename
      them to say so.
- [ ] F.5 Delete `dropGraphsBefore`'s `if true { return }` and the reason for it.

**Acceptance.** Only three region kinds remain (`frame`, `root`, `owned`); the
`df` report accounts for every mapping; the self-build's mapped peak is under
1 GB without any ceiling being raised.

### Stage G — dogfood: the compiler's own phases

*Distance: six. The compiler is the first consumer of its own region system, and
the proof that the model pays.*

Status: `todo`

- [ ] G.1 Annotate the compiler's phases with region budgets instead of guessed
      sizes (`len(src)*64`, `4MB`, `64MB`, …): the initial region size comes from
      the inference result or from the previous package's measured use.
- [ ] G.2 The SSA builder's per-function scratch is a frame region (D10), and the
      stage's acceptance is that `runtime`'s SSA build no longer grows past
      its own product.
- [ ] G.3 The emitter's cost, **with the premise corrected by measurement**.
      Per-function scratch already HAS a scope: `emitAllFunctions` gives each
      function an `fnArena` and `ArenaReset`s it, and the measured result is that
      586 of 587 functions stay under 5.7 MB. So this item is not about
      lifetimes. The measured cost is ~25 KB **per instruction**, linear, and the
      wall is one function: `strconv.initPow10`, 7025 instructions in a single
      block, 188 MB, because it materialises a 696-element `[]pow10` literal
      element by element. Two fixes, in order of leverage: (i) the builder emits
      a constant table once instead of one store per field per element -
      `tryConstArrayMemcpy` exists for constant arrays and does not cover a slice
      of structs, and this is a codegen change so WOOD LAW applies; (ii) cut the
      ~25 KB per instruction inside `emitInstr`. The hand-rolled checkpoint in
      the loop stays: it is the interrupt point, not reclamation.
- [ ] G.4 Per-phase budget gate in the build: a phase that exceeds its measured
      ceiling fails the build with the phase named, instead of dying in the
      cgroup. **This requires the §1.6 decision first**: a gate over an advisory
      capacity is a suggestion. Record which of (a) capacity is a measurement,
      (b) capacity is a bound, or (c) assert in debug / chain in release with the
      segment ledger as the real bound — and if (c), say that a build's failure
      depends on assertions being enabled.
- [ ] G.5 The 1 GB self-build, boxed, with the phase table recorded.

**Acceptance.** `scripts/moxbox.sh <compiler> build -a -no-main-arena
./_mxc_stage4` exits 0 with a peak below 1 GB, kernel-reported; the phase table
shows no phase above its budget; a second run reproduces within 5 % (D-nondet
closed).

### Stage H — verification and deletion

Status: `todo`

- [ ] H.1 A regression test per failure class: use-after-unmount, aliasing across
      mounts, double unmount, accounting drift — each expressed as a program that
      *fails to compile* (first three) or a build that fails its budget gate
      (the fourth).
- [ ] H.2 WOOD LAW audit: mxh, signatures, placement and owned regions identical
      between `legacy/` and `_mxc_stage4/`; the fixpoint bootstrap
      (`stageN == stageN+1`) still holds.
- [ ] H.3 Write the durable record into `docs/memory.md` and
      `docs/decisions.md`; delete `docs/memory-budget.md`'s ceiling table into
      it if the numbers have been superseded.
- [ ] H.4 `git rm region.md`.

**Acceptance.** The tests exist and pass, the bootstrap reaches a fixpoint, and
`region.md` is gone from the tree.

---

## 6. Acceptance for the whole plan

1. `scripts/moxbox.sh <compiler> build -a -no-main-arena ./_mxc_stage4` exits 0,
   peak RSS under 1 GB, no `SysSetMemoryCeiling` abort, two consecutive runs
   within 5 %.
2. No `sov*` symbol in `src/runtime/alloc.mx`; no compaction queue; no ratchet.
3. Three region kinds; every region's lifetime decided by a lexical scope or by
   an owned value's static lifetime.
4. `df` agrees with the kernel at every phase.
5. The number of signatures that **declare** ⊤ is zero for the standard library,
   and the build reports that count. (Declared, not inferred: A1, L7.)
6. Constitutional: the compiler checks declared facts and never derives one it
   was not given; every region has exactly one authoritative owner; no allocation
   lands in a region it did not name. A stage that meets 1–5 by violating any of
   these has failed.

## 7. What would make us stop

- If Stage C shows that declared ⊤ reaches so much of the standard library that
  more than ~30 % of returns stay on the copy path, the model is not paying for
  itself in this language as written, and the fallback is *not* a rewrite: it is
  to keep the copy protocol as the default and take only Stage E's owned regions
  (which is where the 2.5 GB and the R10 class live). Record the measurement and
  decide in the open. Note what this is *not*: it is never a licence to replace a
  declaration with an inference — that would trade a memory problem for a
  constitutional one (A1, L7).
- If Stage A cannot make the stack a real stack without changing emitted code,
  stop and take the emitted-code change through WOOD LAW *first*; a runtime
  convention change that legacy does not mirror is the one thing that has broken
  this bootstrap before.
- If any stage needs a memory ceiling raised to pass, the stage is wrong.
- If a stage can only state its acceptance by introducing linear or refinement
  types into the compiler, it has left §1.6's maxim (the compiler's question
  stays decidable; the depth belongs in the library). Go back and restate the
  acceptance in terms the analysis can already answer.

## 8. Reading list

- Tofte & Talpin, *Region-Based Memory Management*, Information and Computation
  132(2), 1997 — the model, the inference, the safety proof.
- Talpin & Jouvelot, *The Type and Effect Discipline*, LICS 1992 / I&C 111(2),
  1994 — effect rows as unification variables; read this before C.2.
- Birkedal, Tofte, Vejlstrup, *From Region Inference to von Neumann Machines via
  Region Representation Inference*, POPL 1996 — how a region stack becomes
  machine storage; the closest thing to Moxie's runtime.
- The ML Kit region-inference papers beyond the POPL 1996 one, for **imperative
  type variables**: generalisation as it applies to a language with mutable
  state. Read before C.3, and do not substitute ML's value restriction for it.
- Tofte, *Type Inference for Polymorphic References*, Information and
  Computation 89(1), 1990 — where imperative type variables come from; it is the
  device T&T's generalisation restriction relies on in the imperative case.
- Grossman, Morrisett, et al., *Region-Based Memory Management in Cyclone*, PLDI
  2002 — first-class regions, the `region` handle, and the typing rules for
  R-store/R-escape as implemented in a real compiler.
- ML Kit's region handles (Elsman/Hallenberg) — the implementation reference for
  E.1.
- O'Hearn, Reynolds, Yang, *Local Reasoning about Programs that Alter Data
  Structures*, CSL 2001; Reynolds, *Separation Logic*, LICS 2002 — the ownership
  calculus underneath R-store.
- Cherem & Rugina, *A Practical Escape and Effect Analysis for Building
  Lightweight Region Annotations*, 2007 — inference that produces annotations
  rather than requiring them; the shape of Stage C.

## 9. Completion log

Append one entry per completed sub-todo. Command, number, file. No prose.

```
<date> <stage>.<item> <what changed> <evidence: command + number>
```

2026-11-15 0.0   plan amended to the constitution in docs/axioms.md: §0.0 added,
                 A1/L7 ("declare, don't infer") deleted Stage C's constraint
                 solver and its ⊤-on-failure escape, C rewritten as declaration
                 plus a syntax check, §4.4 and the execution-order row changed,
                 §6 acceptance gained the constitutional clause. Evidence: this
                 file, grep -n 'declared effects' region.md = 1 stage heading.
2026-11-15 0.1a  SovCompactGroup measured as a permanent no-op and deleted.
                 Evidence: boxed self-build with counters, `sovcg try=297 ok=0
                 ext=297` at `SSA mem fn=Tan`; the call is gone from
                 ssa_builder.mx and the function and counters from alloc.mx.
2026-11-15 0.1b  graph release rewritten: last-use rule (never fires - `main` is
                 every package's last user, measured dropgraph count = 0) replaced
                 by retainGraphsOnly, keep exactly the package's import closure.
                 Evidence: boxed cold pre-pass, `grep -c dropgraph` = 192, working
                 set `pre ... needs=4 reg 5->5`, `needs=46 reg 32->45`,
                 pre-pass `retained` 445MB -> bounded per package, no
                 dangling-type fault before the wedge.
2026-11-15 0.2   BLOCKED. Best boxed cold self-build: cgroup MemoryPeak
                 954,736,640 B = 910 MiB, wedged at `main`'s pre-pass type-check
                 (`tyP10`, rss=925MB, state D, 13m42s), not OOM-killed and not
                 emitting. Two causes: `main`'s type-check arena chains from a
                 75MB guess to acap=464MB, and root grows 55 -> 166 regions /
                 200 MiB because -no-main-arena gives package main no frame
                 arena. Recorded in docs/memory-budget.md "Round 2".
2026-11-15 0.3   round-2 phase ceilings recorded (not the baseline's, because
                 0.2 produced no baseline). Evidence: docs/memory-budget.md
                 +75 lines, table rows for the wedge point and the 2.7GB gdb
                 abort with its 550-frame codecEncodeValue backtrace.
2026-11-15 0.1c  retainGraphsOnly applied in the compile loop breaks name
                 resolution, so it is pre-pass only until fixed. Evidence:
                 mxlex build with both drops -> `math: undefined: bits`,
                 `trigReduce: cannot use int32 as uint64`; with the compile-loop
                 drop removed and the pre-pass drop kept -> the same build
                 reaches `runtime`'s emit (EAF nf=795) instead of failing in
                 `math`. Cause named in _mxc_stage4/main.mx at the call site:
                 clearGraphCaches empties a retained package's import record and
                 nothing re-registers it.
2026-11-15 A.3   ledger walks every live arena; report prints narena/acap/net/
                 dfgap. Evidence: boxed build of a one-file program, 1054 `mem`
                 lines, `dfgap=25` at startup rising to `dfgap=93` after the 64MB
                 domain stack is mapped and constant across phases; `lrep=0`
                 (no stale link) and `narena` 21 -> 6777.
2026-11-15 A.3b  found and bounded a live-list corruption class: the first run
                 reported narena=4096 (the walk's bound) against 166 mappings.
                 Replaced the bound with per-node generation marks, so a repeat
                 stops the walk and is counted (`lrep=`), and sovFindContainingArena
                 uses the same guard. Evidence: next run lrep=0, narena=6777,
                 acap=257MB, net=351MB, dfgap=93MB.
2026-11-15 0.2    update: with the pre-pass working set fixed the wall moved from
                 `math`'s SSA build to `runtime`'s EMIT phase - a 1-file program
                 build (cmd/mxlex) reaches `EAF nf=795` then stalls at rss=902MB
                 (state D, cgroup peak 907 MiB). Not OOM-killed. The emit loop's
                 per-function scratch is G.3, and the same "product escapes, so
                 the group cannot be compacted" argument that killed 0.1a applies.
                 Interrupt path verified in passing: SIGINT during that stall
                 exits with `moxie: build interrupted` (rc=130).
2026-11-15 0.1d  per-key cache clearing replaces the wholesale clearGraphCaches,
                 and it did NOT fix the compile-loop drop. Evidence: with
                 clearGraphCaches gone and retainGraphsOnly deleting only its
                 own package's Registry/ByName/DirectImports/mxhLoaded entries,
                 `math` still failed with `undefined: bits`. Retention itself is
                 ruled out by the trace: `pre math needs=6 reg 6->8` shows the
                 closure is loaded for math, and the loop retains exactly that
                 set, so math and math/bits are both in the registry when the
                 name fails to resolve. The defect is in the compile path's own
                 import resolution, downstream of the working set. The per-key
                 clear is kept: it is strictly safer than a wholesale clear,
                 which also wiped the entries of packages the release retains.
2026-11-15 0.2b  emit phase attributed. Evidence: `EAF` probe now prints net= and
                 acap=; nf=1 -> 166MB rss / 190MB acap, nf=2 -> 221/270 (one
                 14-block function adds 80MB of segment growth), nf=609 ->
                 899/970. acap tracks net within ~90MB throughout, so the emit
                 wall is live arena capacity rather than an uncounted mapping
                 tower. The per-function arena IS reset per function
                 (ArenaReset unmaps extras), so the remaining cost is one
                 function's scratch VOLUME (G.3), not a lifetime leak.
2026-11-15 A.4   note, not a claim: a probe that called ArenaUsed(fnArena) and
                 ArenaUsed(compileArena) in the emit loop SIGSEGVd at nf=42
                 (rc=139); removing those two calls ran to nf=609. A.4 must
                 record size and site at allocation time instead of reading an
                 arena the caller may no longer own.
2026-11-15 0.1e  the `undefined: <name>` class was EXPORT-CACHE CORRUPTION, not a
                 retention bug. Evidence: `find cache -name '*.mxh' -size 0 | wc
                 -l` = 51 of 187 files. TypeCheckOnly wrote exports with
                 writeFile (non-atomic) so an interrupted build left a truncated
                 file under a valid name, and every cache-hit test was
                 FileExists, so the truncation was invisible and loadMxh silently
                 returned false. Fixed in three layers: cacheWrite (tmp+rename)
                 for the export write; mxhValid (size>0) replacing FileExists at
                 all 5 cache-hit sites; graphReloadable refusing to evict a graph
                 whose mxh is absent or empty. After: boxed self-build reports 0
                 `undefined:` (was 39) and 0 SIGSEGV, reaching runtime's SSA at
                 rss=770MB before the 600s bound.
2026-11-15 0.1f  compile-loop release: SECOND failure mode measured, so it stays
                 off. With the cache fixed, applying it faults with SIGSEGV after
                 tyP10 at reg=17 - a *Type from a released graph still reachable
                 from a retained one. The closure argument covers imports and does
                 not cover a type reached through dispatch or embedded-method
                 lookup; that is the outlives relation this plan exists to add
                 (D6). Recorded at the call site as evidence, not as a patch.
2026-11-15 note  `cmd/mxlex` is not a valid test target: its main does not compile
                 (`undefined: append`), so a build of it measures the compiler's
                 error paths. Use ./_mxc_stage4 or a should_compile file.
2026-11-15 0.2c  emit wall localised to ONE function, and it is linear. Evidence:
                 `MOXIE_EMIT_USED=1` on a boxed self-build: 586 of 587 functions
                 use <= 5.7MB of fn-arena scratch; `strconv.initPow10` uses 188MB
                 and has 7025 instructions in one block. Checkpoints inside it:
                 before the instruction loop 8.6MB, after 187.5MB -> ~25KB per
                 instruction, linear. It is 7025 instructions because it builds
                 `detailedPowersOfTen = []pow10{...}` as 696 struct-literal
                 elements. Fix order: (1) builder emits the constant table once
                 (tryConstArrayMemcpy does not cover a slice of structs) - a
                 codegen change, so WOOD LAW applies; (2) cut the ~25KB per
                 instruction inside emitInstr. NOTE: neither is a region-lifetime
                 change, so G.3's premise ("per-function scratch") is wrong for
                 this wall - the scratch already dies per function.
2026-11-15 0.2d  emit floor measured and reduced. `EAF0` prints the floor's
                 composition: acap=635MB graphs=39/12MB root=59/71MB. The graph
                 working set is 12MB of data in 156MB of capacity (4MB reserved
                 per arena) - the reservation was the floor, not the graphs.
                 Sized graph arenas from each package's mxh text (24x, floor
                 256KB, ceiling 4MB): capacity 156 -> 39MB, acap entering emit
                 635 -> 490MB, rss 508 -> 450MB, and at `retPoineeDies` in
                 runtime's SSA net 994 -> 862MB / acap 921 -> 772MB / rss 770 ->
                 706MB. No emitted-code change, so no WOOD LAW obligation.
2026-11-15 0.2e  the emit spike (initPow10, 188MB, ~25KB/instruction) cannot be
                 fixed by another arena: the fn arena holds both the emitted
                 text (`e.segs`, a []string that must survive to flush) and the
                 scratch, so it cannot be reset per instruction and a
                 per-instruction frame arena would free the text with the
                 scratch. Separating them requires naming the region per
                 allocation - D1 - which is Stage D's placement. First measured
                 wall whose answer is the plan's own mechanism rather than a
                 heuristic.
2026-11-15 0.2f  the peak now has a name: `rssPeak()` keeps a high-water mark and
                 the emit loop prints `NEWPEAK ... at emit <fn>`. Measured: the
                 peak CREEPs - every function is a new maximum - so the growth is
                 an accumulator. It is `mergeAccumulators`, which copies one
                 string-constant per OCCURRENCE into the package's strConst pool.
                 Sharing the first copy for later occurrences was implemented,
                 built, and REJECTED BY THE COMPILER at link: "unknown special
                 variable with appending linkage: @0", because strings are
                 mutable - two .str slots sharing one text corrupt each other.
                 Reverted, with the reason at the call site. The pool is product,
                 not scratch, so this is not a lifetime defect.
2026-11-16 0.2g  the floor had a name and it was NOT the string pool. Evidence, in
                 order of refutation. (1) ZERO BLOBS: `LiveArenaTop` scans used
                 pages of every live arena and reports the all-zero fraction -
                 livetot used=691MB; scanned 571MB, zero=11MB,
                 zeropermille=19. 1.9%, and the largest single arena reads
                 used=344MB / zero=3.7MB. The memory is full of data. (2) THE
                 POOL: the peak line now prints the string pool and every NEWPEAK
                 reads pool=0KB nstr=0, so round 2f's accumulator is not the
                 accumulator either. (3) THE FN ARENA: `EAFU used=0MB` at every
                 function end - Mr per-function scratch is already reclaimed, as
                 0.2b said. (4) THE COUNT: census n=21500, tiny=20245 (195MB of
                 reservation), hdr=31MB, sov=10645 - a real granularity cost, but
                 31MB of RSS, not the peak.
2026-11-16 0.2h  `scopebytes` named it after three instrument defects were fixed:
                 id=45 n=1 used=344MB - `cctx.genericArena`, one arena created
                 once and never freed. `genscan` split it into parse vs scope:
                 the PARSE is the cost, 100-170 bytes of AST per source byte, and
                 `main` alone is 1470KB of source to 201MB of AST. `main` is
                 compiled last and nothing imports it, so the whole-link typecheck
                 pass was parsing it at the FIRST package for its own use at the
                 LAST. Fix: retain a package's AST in the typecheck pass only when
                 `dispLastUse[pkg] > pkgOrder[pkg]` - nothing later can instantiate
                 from it - which is the outlives fact the graph retention already
                 uses. Measured at math's emit floor: rss 820 -> 620MB, acap 1041
                 -> 849MB, genericArena 344 -> 137MB. Predicted 200MB, measured
                 -200MB. Remaining 137MB is `runtime` (92MB), `time`, `strconv` -
                 packages later packages do instantiate from.
2026-11-16 0.2i  the next wall is `strconv.initPow10` and it is TRANSIENT: 646MB
                 before it, 999MB inside it, 655MB after. 7025 instructions in ONE
                 block, ~188MB of fn-arena scratch created and abandoned in that
                 function. Because 999 < MemoryMax=1G the box throttles instead of
                 killing, which is why a build sits in one function for tens of
                 minutes. Same shape as the emitter's ~1.3MB/function creep (acap
                 +27MB, net +66MB over 79 functions with cau/cac/fn-arena/pool all
                 flat). Both are the emit write path allocating into arenas that
                 no compiler pass compacts; the sanctioned fix is the tree's own
                 authoring rule - build per-instruction text in FREE functions so
                 it dies with the fn arena - and it is a refactor of that path, not
                 a one-liner.
2026-11-16 0.2j  allocation-site instrumentation, for the statement-level answer the
                 aggregates cannot give. The mechanism already existed
                 (`src/runtime/alloc_trace.mx`: 8192 sites, `logAlloc` emitted in
                 front of every allocation, side-file naming each id) and was
                 unreachable three ways: `-print-allocs` never set the `alloc_trace`
                 tag (so logAlloc was the no-op twin and every counter stayed 0);
                 `PrintAllocCounters` had NO CALLER (cover had an exit hook,
                 allocation had none); and stage4 names a site `pkg.Func` while
                 legacy names it `pkg.Func@file:line`, collapsing a function's
                 allocations to one id. Fixed: both compilers make the flag imply
                 the tag; `allocBeforeExit` is wired beside `coverBeforeExit` in
                 runtime_unix.mx and scheduler_none.mx; `PrintTopAllocSites(k)`
                 added for mid-run reports (a transient is invisible to an
                 exit-time dump). OPEN: an `alloc_trace` build of the compiler
                 itself faults - "codec arena escape: pointer into a transient
                 frame arena preserved instead of copied" on a 12015424-byte raw
                 block, then SIGSEGV in math/bits - so the exit dump is not yet
                 usable on the compiler. Stage4's site name still needs `InstrPos`
                 rendered to close the wood-law gap with legacy.
2026-11-16 0.2h2 re-verified on the final tree at math's emit floor: rss 595MB (was
                 820MB), acap 849MB (was 1041MB), live used 691 -> 481MB, and
                 scopebytes id=45 is ABSENT - with retention deferred the
                 genericArena is never created early enough for math to inherit
                 it. Net -225MB. The peak at strconv.initPow10 now reads
                 595 floor + ~92MB emit creep + 188MB transient = 1001MiB, just
                 under the cap: the box throttles instead of killing, and the
                 deciding term is the creep.
2026-11-16 0.2k  THE ROOT CAUSE of the remaining wall, measured with a
                 volume-triggered stack probe (MOXIE_ALLOC_STACK=<MB>:
                 ArmAllocStackProbe in alloc.mx dumps the call stack every N MB an
                 arena hands out; one compare per allocation, allocates nothing,
                 works when the cost is millions of small allocations). Largest
                 single allocation in a whole package build, symbolized with nm -n
                 against the static non-PIE binary:

                   ALLOCSTACK scope=41 alloc=337239968 size=67461120
                     arenaBump <- ArenaAllocTD <- ArenaAlloc <- sovTableAlloc
                     <- sovGrowRecs <- sovAddRec <- ArenaAllocTD <- alloc

                 67461120 = 8388608 records x 8 bytes: ONE sovereign group's
                 allocation-record pointer table. sovAddRec records every
                 allocation made while a group is borrowed, into g.allocTbl (a
                 ~40-byte sovRecord each) plus 8 bytes in g.recs. The emitter's
                 methods borrow its group for a whole instruction, so each
                 instruction's strings/slices/maps are all recorded: ~1200
                 allocations per instruction, ~50 bytes of bookkeeping each. That
                 is the ~25KB/instruction and the 188MB transient. sovMapMax is
                 1<<27, so a bigger package reaches "sovereign allocation map
                 overflow". Records exist for compaction, and compaction only runs
                 at a select or a drained checkpoint - a compiler has neither
                 inside a function body, so the audit trail of millions of dead
                 intermediate strings is never pruned.
2026-11-16 0.2k1 fix analysis, recorded because the choice is a schedule decision
                 and not an open question about the cause. (1) Per-instruction
                 drain: REJECTED as a switch - sovCompactStep copies records to
                 rec.newAddr and relies on a forwarding map consulted at READ time;
                 the emitter object lives in the COMPILE arena while its data lives
                 in the group, so a field of the receiver holds a pointer into the
                 compacted arena that nothing rewrites. That is the fault class
                 that made SovCompactAll unsafe. (2) Free functions for the emit
                 write path: correct by the tree's own authoring rule (a free
                 function does not borrow, so allocations land in the fn arena),
                 removes both the records and the ~1.3MB/function creep; it is a
                 refactor of ~120 methods. (3) -no-sov-borrow for package main
                 beside -no-main-arena: the emitter needs no sovereign arena
                 because its accumulators are already explicit in the compile
                 arena, which is freed per package; a codegen change, so WOOD LAW
                 applies to both compilers.
2026-11-16 0.2l  two measured corrections and one real defect. (1) The 188MB
                 transient was a MISSING FLAG: docs/building.md requires
                 -no-main-arena when building the compiler itself and says why
                 ("every self-mutating emitter method holds a sovereign arena that
                 cannot be compacted while it runs"). Every stage4 binary this
                 session was built without it. With it: initPow10 costs +6MB
                 instead of +188MB (365 -> 371MB at that point, was 645 -> 833).
                 (2) DEFECT: emitAllFunctions created an 8MB fnArena and called
                 SetCurrentArena(fnArena) per function, but `alloc` hands out from
                 domainRoot.fnStack[fnTop] and never reads that setter, so the
                 per-function emit arena was never on the stack - EAFU used=0MB was
                 the truth. Fixed to ManualArenaEnter/Exit, popping before
                 mergeAccumulators. (3) -no-sov added to BOTH compilers (fn arena
                 kept, sovereign arena dropped) and measured byte-identical to
                 -no-main-arena: cau=536MB, rss=895MB, same packages - which
                 disproves "emit scratch fills the compile arena". The volume stack
                 probe then named it: 32-byte allocations from the SSA build path
                 (buildRangeLoop, checkDuplicateCases, checkPackageDecls,
                 checkClosureCaptures) and cau 108MB at ci-tc -> 538MB at the emit
                 floor, i.e. 430MB added ACROSS SSA BUILD for `runtime`. The wall
                 is the biggest package's SSA, not the emitter.
2026-11-16 0.2m  the SSA boundary, measured with a new `ci-ssa` line inside
                 CompileToIR: `ci-tc runtime cau=108MB` -> `ci-ssa runtime
                 members=1268 cau=522MB rss=874MB`. The SSA build of the biggest
                 package costs +414MB on its own, and the volume probe reports
                 every crossing in that window as a 32-byte allocation (13 of 13
                 reports), so the SSA is ~13M small nodes in the compile arena,
                 bulk-freed only at package end. This is the peak. The fix is a
                 per-function SSA region freed at the function's end - Stage D's
                 mechanism, target 414MB - not a smaller struct and not the
                 emitter, both of which are now ruled out with measurements.
2026-11-16 0.2n  CreatePackage split by a new `ssa-bodies` probe: for `runtime`
                 (875 fn / 7093 blk / 41537 ins) the AST+type graph is 108MB, body
                 building adds 124MB, and the CHECK SUITE adds 289MB - 2.3x the
                 graph it inspects, allocated per node into the package arena and
                 outliving the checks. That is the 874MB peak, not codegen.
                 Scoping the suite in a scratch arena was implemented and REVERTED:
                 it freed the 289MB from the package arena (cau 521 -> 233MB) but
                 broke the build with four "undefined: runtime.CurrentArena"
                 errors in `time`, so a check records state the export reads and
                 the retention is not yet identified; the transient peak did not
                 move either. Recorded at the call site. Next: identify the
                 recording check, scope the pure ones, then cut the suite's
                 per-node allocation volume (~7KB per instruction).
2026-11-16 0.2o  per-check ranking on `runtime`: `checkShadowing` alone is the
                 +284MB (all other checks together ~5MB). Rewritten from twelve
                 functions returning [][]string (pointer-bearing returns -> codec
                 deep copy of the whole scope stack per statement) to one
                 shadowWalker mutated through a pointer, with in-place truncation
                 instead of subslice copies. Measured 517 -> 461MB, so the returns
                 were not the whole cost; kept because it is strictly better.
                 ALSO: the build now runs long enough (as a contained SERVICE, not
                 a job - the harness kills jobs at ~10 min, proven with a boxed
                 `sleep 700` dying rc=143) to reach package `time`, where it fails
                 with "undefined: runtime.CurrentArena" x12. Pre-existing: no
                 earlier run reached `time`. The dumped export CONTAINS both
                 names, so the defect is in materialising the runtime registry
                 entry for `time`, not in the export. This gates every memory
                 number: all peaks so far are peaks of an incomplete build.
2026-11-16 0.2p  the `time` blocker ROOT CAUSE: `loadMxhDataIn`/`ensurePkgLoaded`
                 skip a package if the registry has ANY entry for its path, and
                 startup installs a Synthetic 9-name placeholder for runtime
                 (InitCShared, LockOSThread, KeepAlive, GOROOT, Gosched, Version,
                 GOOS, GOARCH). So runtime's real export was never loaded and the
                 loader returned TRUE. Evidence: undefdbg shows names=9
                 regnames=9 same=y; the dumped mxh contains both names; nine is
                 the placeholder's exact size. Fixed with the predicate the tree
                 already uses elsewhere: replace a Synthetic entry, never a graph
                 (both loadMxhDataIn and ensurePkgLoaded). Verification run in
                 progress under box throttle; 0 undefined so far.
2026-11-16 0.2q  jubilee step 1 landed and verified. The second typecheck pass was
                 the whole-link registry pre-pass in main: prepareDispatchIndex
                 already typechecks every package whose mxh is missing and
                 builds the dispatch index from the same text, and
                 buildPkgsSerial starts with retainGraphsOnly(nil), so the
                 pre-pass's typechecks and registry loads were both thrown away.
                 Deleted; CompileToIR no longer materialises its own mxh but
                 registers the live graph for its own compile (removed before
                 ArenaFree, or the next package would read a freed arena).
                 Measured in the box (mxc-r5, r5-d1.log): tc-out exactly once per
                 package (45/45), ci-pre once per package, build reaches `main`
                 - the last package - where the baseline stopped at runtime's
                 emit. Peak 945,786,880 B.
2026-11-16 0.2r  jubilee steps 2/4 are BLOCKED, and the reason is a codec rule,
                 not the emit loop. Implemented: CreatePackage builds no
                 bodies, BuildBody builds one, emitAllFunctions builds/checks/
                 emits/drops one member per fnArena push. That fails in the
                 first generic package with "cannot use []mxutil.ImportSeg as
                 []mxutil.ImportSeg". A probe inside resolveSignatureWithSubst
                 showed the two `[]T` of ONE signature resolving to different
                 Named objects, neither equal to typeSubst["T"]: the codec
                 DEEP-COPIED the type on return. Why: codecInManualArena
                 (codec.mx:163) tests domainRoot.manualArena, the TOP manual
                 frame only. With the function arena on top, a type in the
                 package arena is not in manual arena, retPointeeDies says its
                 arena dies, and the return protocol copies it. Registering the
                 package arena with SovereignRegStable and making the durable
                 scopes real ManualArenaEnter(compileArena) frames fixed the
                 identity split outright (mxc-r15, no probe fires) but a later
                 SIGSEGV in emitFunctionOf -> hashmapContentSet remained, so the
                 change was reverted rather than shipped. The shape that fits
                 the measurements: the body must live in the BUILDER's sovereign
                 arena (per-function, released by arenaReleaseGroups), with the
                 package arena as the manual frame - i.e. the builder keeps
                 borrows and the emitter does not. -no-main-arena currently
                 disables both; splitting it is option (3) of 0.2k1 and is a
                 codegen change, so it is bilateral with legacy.
2026-11-16 0.2s  a pre-existing defect, NOT from these steps: any APPLICATION
                 build segfaults after "compile objects" (hello, the generics
                 cases) with the checkpoint compiler mxc-r3m as well as with
                 r5/r16. The self-build is unaffected and remains the acceptance
                 vehicle. Left alone as out of scope for jubilee.
2026-11-16 0.2t  step 2/4 round 2, on branch jubilee-step2-wip. Two real fixes
                 and one open defect.
                 (1) The recursive addCompileError the first rewrite created
                 (the global "push(cctx.compileErrors," -> addCompileError
                 substitution also hit the helper's own body) was the cause of
                 every "spin" and "SIGSEGV" I attributed to other things: the
                 helper called itself. Fixed.
                 (2) The identity split is FIXED and verified: the codec's
                 return rule asks only about the TOP manual arena, so with the
                 function arena current every type returned from a resolution
                 helper was deep-copied and ONE signature's `[]T` parameter and
                 result were different Named objects. Registering the package
                 arena with SovereignRegStable(compileArena) for the duration
                 (and making durableBegin enter it as a manual frame) removes
                 it - the SIGID/GENID probes stop firing, and a boxed
                 self-build now compiles 12 packages, past mxutil's generics.
                 A codec change that makes the whole manual chain stable in the
                 return rule was tried instead and SPINS (correctness of the
                 extension is unresolved), so it is not in the branch.
                 (3) OPEN: the build then SIGSEGVs in emitFunctionOf ->
                 hashmapContentSet -> hash32 while emitting the function after
                 strconv.initPow10, whose per-function arena reports
                 used=382MB. Two classes are candidates and both are the same
                 shape: per-function state that outlives its arena. e.operandBuf
                 is nil'd before the pop, but e.globalTypes is CREATED in the
                 compile arena and INSERTED into during a body scan in the
                 function arena (map growth follows the owner arena, so this
                 needs checking), and any cctx-level cache written during a
                 build has the same exposure. Next step: find what the wild key
                 at 2161 actually is by printing e.usedAs's map arena and the
                 ref that produced it, or by giving the emitter's per-function
                 maps an explicit package-arena allocation at creation.
                 -no-main-arena also means package main's self-mutating types
                 never borrow, so the compiler's own emitter/builder scratch
                 lands in the current arena rather than a sovereign one: that is
                 why initPow10 alone is 382MB. The durable design (option 3 of
                 0.2k1) is to enable the builder's borrow and keep the emitter's
                 off, which is a codegen change and bilateral.
2026-11-17 0.2u  handover state at the 66% rule. Working tree is on branch
                 jubilee-step2-wip (ee3509d5), dev is the verified step 1
                 commit 5bfa352e and must stay there until step 2 is green.
                 Binaries: ~/mxc-builds/mxc-r16 = dev (step 1); mxc-r21 = step 2
                 with the identity fix (crashes at strconv); mxc-r22 = mxc-r21
                 plus experiment A (no per-function ArenaReset unless >1GB).
                 Experiment A is INCOMPLETE: it reached 9 packages (math), 88
                 functions, fnArena 60MB, box peak 522MB, no panic - strconv was
                 not reached before the stop. Run r22 to strconv: passing it
                 means the reset is the defect; crashing there means the reset
                 is innocent. The r22 log is ~/mxc-builds/r22-d1.log.
                 Forks recorded for the next context: if the reset is innocent,
                 the next suspects are (i) e.globalTypes created in the package
                 arena and inserted into during a body scan that runs in the
                 function arena, (ii) any cctx-level cache written during a
                 build, (iii) the -no-main-arena borrow split (region.md 0.2k1
                 option 3) which would put the body in the builder's sovereign
                 arena instead of the function arena - the durable design, but a
                 bilateral codegen change. Rule added to jubilee.md: stop at 66%
                 of the context window, leave dev verified, record the running
                 command and its log.
2026-11-17 0.2v  step 2/4, second working session. Branch jubilee-step2-wip now at
                 04f68f3d (dev untouched at 5bfa352e). Five lifetime defects
                 found and fixed, all of them "a value built in the per-function
                 arena read after that arena was reset":
                 (1) a monomorphised clone's mangled name was built one line
                 ABOVE the durable scope, so it became a key of
                 targetPkg.Members and the clone's SSAName while living in the
                 function arena: SIGSEGV in hashmapContentSet/hash32 emitting
                 the member after strconv.initPow10. Reproduced in 1m40 with a
                 12-line probe (tests/tmp/strconvprobe); durableBegin now opens
                 before the name, and genericMethodInstance lost the second
                 durableBegin that shadowed the first and leaked its frame.
                 (2) manualFrame - the bitmap codecInManualFrame answers from -
                 was set only by SovereignSetArena, never by ManualArenaEnter,
                 so a phase arena counted as stable only while it happened to be
                 the top manual arena. Fixing the bit cut the box peak from 880MB
                 to 595MB and cau during a package's emit from 350MB to 48MB,
                 i.e. that missing bit was the deep-copy engine.
                 (3) checkIfaceAssignments ran at the NEXT package's
                 CreatePackage over entries recorded in a function arena (dead
                 V/T, SIGSEGV). The deferral only needs a complete method set,
                 which CreatePackage now records before any body, so the verdict
                 runs at the end of the member.
                 (4) a member's name reached funcSymbol as dead bytes
                 (`call void @"internal/task.0<binary>"`, clang: NUL in a name).
                 SetMember copies the name into the package arena and makes it
                 the function's own name.
                 (5) fail-loud invariants: FnArenaPop refuses a manual slot,
                 ManualArenaExit refuses a slot that is not a manual frame and
                 refuses an arena that is not the slot's, SovereignRestoreArena
                 refuses a slot that is not a borrow.
                 OPEN, and this is the whole of step 2 now: with the durable
                 stack as MANUAL frames (fast: 1-2ms per member), the member's
                 fn-stack slot (depth 6) ends up holding a pooled FRAME arena
                 (scope 43, cap 8MB) while depth and manual depth are unchanged,
                 so nothing was pushed or popped at that depth: some
                 SetCurrentArena at depth 6 installed it. Measured 11 times in a
                 33-package probe build; the next package then SIGSEGVs. The
                 ring shows the shape: `exit at d=7` (a durable scope leaving)
                 immediately followed by `set d=6 old=<member arena> new=<frame
                 arena>`, i.e. the durable scope's saved arena was ALREADY a
                 frame arena when it opened (DBEGIN: save=1MB/-72MB scope=43 at
                 depth=6, 31 times). Since fnArenaPush returns manualArena while
                 manualArena != nil, a frame arena can only appear at depth 6 if
                 manualArena was nil at some point inside the member, i.e. the
                 member's manual frame was left early and a generated frame took
                 the slot. The candidate set for the next round is therefore
                 narrow: a save/restore pair whose SAVE is captured at one depth
                 and restored at another (the runtime's SetCurrentArena idiom
                 crossed with a ManualArenaEnter/Exit), or an unbalanced
                 durableEnd on a path that has not been audited yet.
                 The alternative design was measured too: durable scopes as
                 SetCurrentArena (no nested manual frames) keeps the stack
                 correct (0 LEAK, 0 DSYNC) but is 12-40x slower and hangs around
                 a package's 22nd member, because FnArenaPush ignores the slot
                 and hands callees manualArena, so durable state built by a
                 callee is not durable at all. That is why the manual frames are
                 structurally required and why the fork is "find the stale save"
                 rather than "drop the manual frame".
                 Binaries (all in ~/mxc-builds): mxc-r16 = dev/step 1;
                 mxc-r43 = slot-switch experiment; mxc-r50..r54 = this state
                 with the diagnostics. Logs: t43.log, t50.log, t51.log,
                 t53.log, t54g.log. Probe: `mxc-rN build -a -no-main-arena -o
                 ~/mxc-builds/tN ./tests/tmp/strconvprobe` boxed, 9s-2.5min.
                 Diagnostics now in the tree (all cheap, all fail-loud):
                 MOXIE_EMIT_TRACE=<member>, per-member LEAK + DBEGIN + DSYNC
                 lines, a TIME line with build/emit ms per member, and the
                 runtime's last-64k arena-stack event ring (CurSwitchAt).
                 Instrumentation to remove before merge: the event ring,
                 SetSlotWatch, the DBEGIN/DSYNC/LEAK/TIME prints, BADNAME, and
                 runtime.NowNano.

2026-11-17 0.2w  step 2/4, third working session. THE OPEN DEFECT OF 0.2v IS
                 FOUND, NAMED, AND FIXED. The member's fn-stack slot was being
                 overwritten by a PHANTOM ARENA: durableBegin returned
                 `*runtime.Arena`, and by the time it returned it had already
                 entered the package arena, so the member's arena was no longer
                 the top frame. The codec's return protocol then decided the
                 pointee dies and decoded a COPY of the Arena struct into the
                 package arena - same regions, same scope field, different
                 address. durableEnd restored that copy into the member's slot;
                 the member's own ManualArenaExit then found a different arena
                 there and the build died with "manual arena exit on a
                 different arena than the slot holds" on the first generic
                 member of the third package.
                 How it was named, because the method matters as much as the
                 answer: the DBEGIN/Dsync prints were symptoms, so the read was
                 taken from the protocol's own decision. A watch armed at the
                 member's slot (d=6) recorded every mutator after it wrote the
                 slot, printed the operands, and a return-address scan filtered
                 by the byte before each candidate (0xE8 = a call) named the
                 caller chain with addr2line. The decisive datum was identity,
                 not provenance: `ArenaRegionBase(save) == ArenaRegionBase(
                 cctx.memberArena)` while `save != cctx.memberArena` - the same
                 mapping described by two different structs, which is a copy and
                 not an owner. The first version of that scan faulted (it read
                 *v-5 for any word in a wide address range); the fixed version
                 bounds the read to the binary's mapped text.
                 THE FIX: an arena handle crosses a function return as a
                 uintptr. durableBegin() returns uintptr, durableEnd(uintptr)
                 converts back once, and every call site is unchanged. An
                 address has no protocol and cannot be relocated, which is what
                 a handle needs; the same rule applies to any future helper that
                 hands back an arena.
                 Also in this commit: hashmapInsertIntoNewBucket, hashmapGrow and
                 hashmapContentSet no longer make the owner arena current. They
                 called save/SetCurrentArena(hashmapOwner(m))/restore, i.e. they
                 overwrote the fn-stack top slot to redirect ONE allocation;
                 they now call ArenaAlloc(owner, size) directly. That removed the
                 only remaining way an arbitrary arena could enter a slot, so
                 the slot is now written only by push/pop/enter/exit.
                 MEASURED after the fix, strconvprobe (33 packages) in the box:
                 0 LEAK, 0 DSYNC, 0 DBEGIN-odd, all 33 packages emitted (was 3),
                  2482 members built, 560MB peak, 64s. It then SIGSEGVs in
                 "compile objects" (memcpy) - and dev mxc-r16 (verified step 1)
                 SIGSEGVs at exactly the same place on the same probe with a
                 900.7MB peak, so that is a PRE-EXISTING defect in the
                 object-compile path, not part of step 2. The probe is therefore
                 a smoke test that stops one phase short of a binary; the step 2
                 numbers come from the boxed self-build (s65.log).
                 Binaries: mxc-r57 (first caller-print), r58..r63 (the scan and
                 identity prints), r64 (uintptr handle + watch), r65 (watch and
                 printCallers removed). Commits on jubilee-step2-wip: d5f3b3e5
                 is the durable-handle fix.
                 Instrumentation still in the tree after r65: the event ring
                 (curEvNote/CurSwitchAt), DBEGIN-odd, DSYNC, LEAK, TIME, BADNAME,
                 MOXIE_EMIT_TRACE, runtime.NowNano, ArenaRegionBase. Remove all
                 of it before merge, with the Ring and the prints.

2026-11-17 0.2x  step 2/4, same session, continued. The durable-handle fix (0.2w)
                 is in and verified. MEASURED, boxed, on the compiler's own
                 source: `mxc-r67 build -a -no-main-arena -o mxc-r67-2
                 ./_mxc_stage4`, log s67.log.
                 - runtime package (866 measured members): EAF0 cau=272MB, last
                   member nf=883 cau=389MB = +117MB over the emit, rss 431->558,
                   peak 558MB.
                 - dev mxc-r16, same command (s16.log): peak 894MB and it
                   throttles inside the runtime emit without finishing the
                   package. So the step's win is real (-336MB) and its own
                   Accept (cau within 5MB, peak <400MB) is NOT met.
                 - self-build stops at `bytes`: `undefined: unicode.Tables`.
                   dev r16 fails IDENTICALLY (b16.log, and again after deleting
                   the empty file), so it is pre-existing: the unicode mxh is
                   0 bytes and never regenerated, and the reader degrades the
                   unresolved names to int32 (`int32 has no field or method
                   SimpleFold`). Every build that imports bytes is blocked by
                   it, the compiler's own included.
                 Two things were tried and are recorded so they are not retried:
                 (1) dedupe the descriptor tail before copying it into the
                 package arena (collectTypeDesc, registerLocalType). Correct
                 order, no measurable effect on the +117MB - committed as
                 13113583 with that stated in the message.
                 (2) a dsc=/dby= instrument (durable scope count and retained
                 bytes per member) to attribute the growth. r68 crashed in
                 durableBegin because the first scope's lazily created table
                 landed in the member arena; r69 fixed that and still crashed in
                 durableBegin (address 0x45c859, inside the TLS-read sequence),
                 so the instrument is removed from the tree and the attribution
                 is the next session's first job if the fork chooses (C).
                 The fork itself is written into jubilee.md under Step 2, with
                 the evidence for each position: (A) accept the growth as
                 package state, (B) move durable work into the builder's own
                 per-function arena (bilateral codegen), (C) measure by kind
                 first. Binaries r66-r70; r70 is the current tree.

2026-11-17 0.2y  step 2/4 + step 3, first item. The 360MB of the `runtime`
                 package was attributed from the log's own probe lines:
                   ci-tc cau=29MB -> chk decls 32 -> **chk astfile 263** ->
                   chk importConflicts 268 -> ci-ssa 268 -> EAF0 271 ->
                   nf=883 389. peak rss 558MB.
                 So 231MB of it was ONE line of accounting: the per-file checks
                 (subslice stores/copy targets, shadowing, package decls,
                 missing return) already ran one file at a time, but their
                 scratch landed in the PACKAGE arena - which is the manual frame
                 for the whole compile - so nothing ever reclaimed it. That is
                 the same class as the member body: a unit of work with no unit
                 of reclamation.
                 FIX (commit 8f9b0470): each file's walk runs in a manual frame
                 over a check arena that is reset after the file and freed after
                 the loop. Errors are unaffected because addCompileError copies
                 into the package arena.
                 MEASURED (s71.log, boxed self-build of _mxc_stage4):
                   chk astfile        263MB -> 32MB   (chk total +239 -> +7MB)
                   runtime EAF0       271/433 -> 40MB cau / 201MB rss
                   runtime emit peak  558MB -> 326MB  (step 2 accept: <400MB MET)
                 Negative fixtures on the same binary (res.log): duplicate case,
                 missing return and shadowing all still rejected with the
                 expected text.
                 WHAT IS LEFT of step 2's accept: cau still grows 40MB -> 157MB
                 across the 866 members (+117MB). The size histogram shows bins
                 16..20 (64KiB..1MiB) frozen after the first members, so the
                 growth is MANY SMALL allocations - package state created while a
                 body is built, i.e. what durableBegin exists to retain, not
                 buffers and not per-member garbage in the descriptor path
                 (dedupe tested, no effect, commit 13113583).
                 Binaries r66-r71. r71 is the current tree, and it is the best
                 state measured so far on every number the plan tracks.

2026-11-17 0.2z  step 2/4 + step 3, continued. TWO MORE FIXES, both verified, and
                 the self-build now compiles every package.
                 1. THE 0-BYTE EXPORT (commit 24295fdb). The compile phase copied
                    a package's .mxh text into the CURRENT arena, which for a
                    free function is its own per-function frame arena, and the
                    build loop writes it after that function returns. The write
                    then got a dead pointer: mxc_writefile created the file, the
                    first write returned EFAULT, the loop broke, and a 0-byte
                    export was renamed OVER the correct text the dependency
                    pre-pass had written for the same package. Only the biggest
                    package (unicode, tables.mx) hit it, its importers degraded
                    unicode.Tables/CaseRange to int32, and every build that
                    imports bytes died with `undefined: unicode.Tables` - dev
                    mxc-r16 reproduces it exactly, so it predates this branch.
                    keepStr is the root-arena copy. Measured after: the
                    strconvprobe compiles all 33 packages (0 undefined), 464MB
                    peak, and stops only at the pre-existing object-compilation
                    SIGSEGV that dev also hits.
                 2. The diagnostic `chkA` (commit 30c2d559) names each per-file
                    check's scratch, because the self-build OOM'd at 1GB inside
                    `main`'s check loop (s72.log, right after `chk decls
                    cau=109MB`). With it the SAME build does not OOM: s73.log
                    compiles all 45 packages - 44 emitted, then main - and gets
                    to the FINAL phase (`mem ./_mxc_stage4`), where it faults in
                    `runtime.retPointeeDies+0x34` (the codec's return rule
                    dereferencing a bad pointer). So the main-package OOM is
                    order-dependent, and the next blocker is a dangling value in
                    the return protocol during the last phase.
                 STEPS 4-6 ARE NOW THE CRITICAL PATH, in that order: the graph
                 lifetime question (step 6) is the shape of the retPointeeDies
                 fault, and step 4's per-function arena is what would keep the
                 main package's check/emit phase bounded.
                 Binaries r72 (export fix), r73 (chkA). s73 is the furthest any
                 build in this project has reached: 45/45 packages.

2026-11-17 0.3a  The s73 crash is DETERMINISTIC: a second run (s73b.log) faults at
                 the same address, 0x2c0874, with the same words. Disassembled,
                 that instruction is `mov (%rbx),%rbx` in
                 runtime.retPointeeDies - the read of the value the return
                 protocol is deciding about - so `p`, the return slot, is the
                 bad pointer, not the type descriptor.
                 It fires in the FINAL phase: the last package (mxcover) emits
                 its M5/M3, `ci-free` runs, `mem ./_mxc_stage4` is printed, and
                 the fault lands before `compile objects`. The strconvprobe
                 faults in the same phase (there inside memcpy). So there is one
                 pre-existing defect at the end of the build, and its shape is a
                 dangling pointer used after the frame that owned it.
                 The sharpest hypothesis, recorded so the next session starts
                 with it rather than re-deriving: the encoder is called with `p`
                 in a frame that has already been RELEASED, and it only faults
                 when that frame arena was big enough to be UNMAPPED instead of
                 pooled (ArenaRelease frees an arena whose size exceeds the pool
                 budget, and a pooled one stays mapped, so the same bug is
                 silent for every small frame). That would be a codegen order
                 defect - pop before encode - and it is bilateral (legacy
                 emits the same order), so it is a WOOD LAW change.
                 State at this stop: 45/45 packages compiled, dev mxc-r16 cannot
                 finish package 8. Binaries r72 (export fix), r73 (chkA).
                 dev is untouched at 5bfa352e; all work is on jubilee-step2-wip,
                 HEAD 30c2d559.

2026-11-17 0.3b  STEP 2'S BUILD GATE IS PASSED, AND THE NEW BLOCKER IS THE
                 BINARY IT PRODUCES. Measured, not inferred:
                 `mxc-r84 build -a -no-main-arena -o mxc-r84-2 ./_mxc_stage4`
                 (u84.log, 1.65MB) ran to completion: all 45 packages, 53
                 bitcode files, `compile objects`, `link`, and
                 `-> /home/mleku/mxc-builds/mxc-r84-2`. The compiler built
                 itself for the first time on this branch, with 0 LEAK / 0
                 DSYNC / 0 DBEGIN-odd and no crash in the final phase. The
                 retPointeeDies+0x34 fault recorded in 0.3a does NOT reproduce
                 on this commit: the five keepStr/root-map fixes (aafae251)
                 removed the dangling map values that fed it.
                 THE FIXPOINT FAILS, and this is the new open defect:
                 `mxc-r84-2 build -a -no-main-arena -o mxc-r84-3 ./_mxc_stage4`
                 (f84.log) exits 1 after printing
                     moxie: codec arena escape: pointer into a transient frame
                     arena preserved instead of copied (unknown-extent pointee)
                     usage: mxc <build|run|test|version|cache|get|update|
                            build-runtime> ...
                 The usage line is main.mx:8234, the fallback reached only when
                 `cmd != "build" && cmd != "run"` - so the self-built binary did
                 not recognise its own argv[1]. Corroborating evidence that this
                 is string corruption, not a dispatch bug: the same binary's
                 `version` prints `mxc 1.0.1-rc.1 (sthcnea )` where the dev
                 binary prints a hex digest `(7b104528)`. `sthcnea ` is not a
                 hash - it is the right length and the wrong bytes.
                 The escape warning is the codec's own detector (codec.mx:473,
                 `codecEscapeNote`): a pointer into a transient fn arena was
                 aliased instead of copied, so the frame arena is leaked and the
                 pointer survives - but whatever the pointer names is still
                 being read as the wrong bytes.
                 WHERE THE NEXT SESSION STARTS (in this order):
                 1. Read how `os.Args` is materialised (src/os/proc.mx
                    `var Args []string`; the hosted runtime fills it from C
                    argv) and whether those strings are built in a transient
                    frame arena. A corrupted argv[1] is exactly this defect's
                    shape, and it is the cheapest place to confirm it: print
                    argv[1] before the dispatch compare.
                 2. Then find the aliased return that `codecEscapeNote` reports.
                    `codecIsStableAddr` returning true for a pointer into a
                    frame arena is the mechanism, and 0.2v/0.2w already
                    established that arena handles and `*runtime.Arena` values
                    must not cross returns as pointers.
                 `dev` is untouched at 5bfa352e. Work is on jubilee-step2-wip,
                 HEAD aafae251. Binaries: mxc-r84 (legacy-built, works),
                 mxc-r84-2 (self-built, corrupted strings).
                 Logs: u84.log (the successful self-build), f84.log (the failed
                 fixpoint). Nothing was running at the stop.

2026-11-17 0.3c  THE FIXPOINT DEFECT, FOUND AND FIXED: A POINTER-KEYED MAP THAT
                 OUTLIVED THE ARENA HOLDING ITS KEYS.
                 The `sthcnea ` digest and the unrecognised `build` were NOT
                 argv corruption and NOT the startup codec escape. Measured on
                 mxc-r84-2 with gdb (the binary is not stripped):
                 * `os.Args` is correct at main.main entry - args[0] the path,
                   args[1] = b'build', len 5. argv materialisation is innocent.
                 * runtime.stringEqual takes its two strings in rdi/rsi/rdx and
                   rcx/r8/r9. Breaking on it for `mxc-r84-2 build` shows the whole
                   dispatch chain comparing b'build' against the right literals -
                   version, help, env, dispindex, typedb, targets, info, list,
                   clean, header, install, cache, get, update, test,
                   build-runtime - all of them in the constant block at 0x7e10xx,
                   and then ONE comparison whose literal operand is
                   ptr=0x7d5af0 len=5 = b'  sto': right length, foreign address,
                   and it is the `cmd == "build"` compare. `"build"` is not in
                   that block at all (`build-runtime` and `run` are adjacent in
                   it). The emitted instruction is
                       mov $0x7d5af0,%ecx ; mov $0x5,%r8d ; mov $0x5,%r9d
                   so the compiler emitted the length from its own constant and
                   the address of a DIFFERENT constant - a substring of an
                   IR-template literal in ir_emit.mx's own .data.
                 The mechanism is `strLitIndex map[*SSAConst]int32` (ir_emit.mx):
                 it memoises the `.str.N` index of a string constant, keyed by
                 the SSA CONSTANT POINTER, and it was created once in
                 newIREmitter. `emitAllFunctions` does
                 `runtime.ArenaReset(fnArena)` after every member, so a body and
                 all of its SSAConst objects die there, and the next body's
                 constants are allocated at exactly the addresses the previous
                 one vacated. Every other accumulator keyed by an SSA value
                 (valName, allocTypes, regTypes, hoisted, allocBlock, storedTo,
                 usedAs, faByBase, mulByBase, devByAddr, ldByName,
                 missingStores) is cleared per member in emitBuiltMember for
                 this reason; strLitIndex was the one that was not. A stale hit
                 returns a dead constant's index, so the live constant's bytes
                 are never pushed into pendStrConsts and its operand names the
                 dead constant's global.
                 THE FIX: `e.strLitIndex = map[*SSAConst]int32{}` at the TOP of
                 emitBuiltMember, not with the resets at the end - a map created
                 at the end of the previous member is freed by that member's
                 ArenaReset before the next member's first constant looks it up.
                 The end placement was built and measured as mxc-r85: it
                 SIGSEGVs at 0x2be090 while emitting (*parser).parseAtom. The
                 top placement is mxc-r86 and it is correct.
                 FAST REPRO: tests/tmp/litprobe/main.mx, two functions of 400
                 distinct string literals each. mxc-r85 crashes it in 6s at the
                 same 0x2be090; mxc-r86 compiles it and the binary prints n=2.
                 Cost of the fix measured cold on that probe: r84 50.8s wall,
                 r86 49.2s - no measurable cost, and the fix does not change how
                 many `.str` globals a package emits except by no longer
                 aliasing them.
                 Also settled: the codec escape warning printed at startup is
                 NOT the defect - u84.log line 1 is the same warning from the
                 WORKING mxc-r84. `codecEscapeNote` fires once in both binaries
                 during os.Args materialisation and both proceed.

2026-11-17 0.3d  PROBE NOTE (so nobody re-chases it): tests/tmp/bigprobe and
                 tests/tmp/diagprobe were generated with `seq -w 0 1499`, which
                 pads to FOUR digits, so their literals are
                 `aaaa-bbbb-cccc-dddd-eeee-0000` (29 bytes) while main passes
                 `...-003` (28 bytes). Their `n=0` is CORRECT output, not a
                 miscompile - verified by breaking on runtime.stringEqual in the
                 r86-built binary and reading the operand: the literal really is
                 29 bytes and really is `...-0000`. Only tests/tmp/litprobe
                 (`seq -w 0 399`, three digits) is a valid fixture, and its n=2
                 is correct on both r84 and r86.
                 What the probes DID establish: mxc-r85 (strLitIndex cleared at
                 the END of emitBuiltMember) SIGSEGVs in 6s at 0x2be090, which
                 `nm` resolves to runtime.hash32 - a hashmap lookup on the map
                 the previous member's ArenaReset had already freed. That is the
                 same hash32 signature jubilee.md already records for this
                 defect class, and it is why the reset belongs at the TOP.

2026-11-17 0.3e  THE STALL IS THE BOX, NOT THE FIX - AND WHERE THIS SESSION
                 STOPPED.
                 After the strLitIndex fix (0.3c) the boxed self-build no longer
                 completes: both mxc-r86 attempts and an instrumented mxc-r87
                 stop in the same member, `(*irEmitter).emitBinOp` (nf=228 of
                 the last package), after its EAF line and before its EAFU
                 line, the .ll file no longer growing, the process in state R
                 with system time dominating (utime 64s / stime 265s over ~15
                 minutes), and gdb attached to a later attempt itself sitting in
                 folio_wait_bit_common. That is a cgroup memory.high reclaim
                 livelock, not a loop in the compiler: that member's emit grows
                 its arena to `used=305MB` (u84's own EAFU for the same member)
                 on top of a ~742MB baseline, and the box is MemoryHigh=900M
                 with MemorySwapMax=0, so most of the growth is throttled in
                 reclaim instead of being spent.
                 THE CONTROL THAT SETTLES IT: r84, the compiler WITHOUT the fix,
                 was run on the same command and stalls at the SAME member right
                 now - `NEWPEAK rss=740MB at emit (*irEmitter).emitBinOp pool=4KB
                 nstr=554`, no `EAFU nf=228` (u84c.log). It completed the
                 identical build earlier the same day (u84.log, peak 936MB). So
                 the fix is not the cause: the self-build's peak sits ON the
                 throttle line and whether it gets through is a property of the
                 machine's state, not of the code.
                 The fix's own evidence, from a full 45-package attempt with
                 mxc-r87: the new STRPUSH/STRSTALE instrument printed
                 `strstale=0` in EVERY package's M5 line - no memo hit ever
                 named another constant's text - and strpush is 26-287 per
                 package, so the fix's cost is negligible (r84 50.8s vs r86
                 49.2s cold on tests/tmp/litprobe). Binaries: mxc-r85 (reset at
                 the END of emitBuiltMember - SIGSEGVs in runtime.hash32),
                 mxc-r86 (reset at the TOP - the fix), mxc-r87 (r86 + counters).
                 WHAT IS STILL MISSING is the end-to-end half: with no completed
                 self-build, no self-built binary has been run to show that
                 `build` is recognised again. That needs the peak off the
                 throttle line, which is Step 2's own unmet clause - runtime's
                 emit grows cau 40MB -> 70 (nf=200) -> 99 (400) -> 124 (600) ->
                 148 (800), i.e. ~130KB of retained package-arena state per
                 member, ~117MB over the package.
                 NEXT STEP (the plan's recorded fork, option C, decided in
                 jubilee.md): measure that growth by kind before choosing A or
                 B. Add the accumulator COUNTS to the EAF line
                 (len(e.pendTypeDescs), len(e.typeIDs), len(e.definedSyms),
                 len(e.arrConsts), len(e.keepIfaceTypes), len(e.strMap),
                 len(e.extDecls)); do the counts first, they cannot crash - the
                 r68/r69 dsc=/dby= version crashed inside durableBegin. The
                 numbers for `runtime` print BEFORE the stall in the last
                 package, so one boxed run answers it.
                 Stop state: `dev` untouched at 5bfa352e; work on
                 jubilee-step2-wip, HEAD aafae251 + the fix commit; no processes
                 and no jobs running; mxbuild.scope inactive; `git status` shows
                 only _mxc_stage4/ir_emit.mx modified before that commit.

2026-11-17 0.3f  STEP 2'S UNMET `cau` CLAUSE: FOUND, MEASURED AND FIXED - IT WAS
                 ONE MAP RANGE PER MEMBER.
                 Fork C answered, and the answer was not the plan's guess. The
                 instrument (durN/ptd/dsym on the EAF line, then a phase split on
                 the EAFU line, then a five-point split of the loop tail) says:
                 * runtime's emit floor is cau=40MB and it climbed to 155MB over
                   866 members, ~135KB per member (this is the recorded clause).
                 * NONE of it comes from the member: build+checks 116KB TOTAL over
                   the 866 members, emit+merge 329KB TOTAL.
                 * NOT from durable scopes: 848 of 866 members open ZERO
                   (durN=0); the plan's "~19 durableBegin per member" is stale.
                 * NOT from the prints, ArenaReset or SovCheckpoint: 0KB each.
                 * ALL of it is the loop-back's member selection:
                   `emitNextMemberName` ranges `e.pkg.Members`, and a range over a
                   map calls hashmapNext, whose first call does
                   `buf := alloc(m.keySize*n)` - a sorted buffer of EVERY key.
                   That call runs between two members, after ManualArenaExit, so
                   the buffer lands in the PACKAGE arena and is never reclaimed.
                   Measured: sel=160200KB over the 866 members (= 100% of the
                   growth); mark (the emitted map insert) 104KB; Func lookup 0;
                   ManualArenaEnter+SovereignRegStable 0.
                 FIX: enter the member arena around the selection
                 (`ManualArenaEnter(fnArena)` / `emitNextMemberName` /
                 `ManualArenaExit(fnArena)`) so the range's buffer dies at that
                 member's ArenaReset. Selection semantics are unchanged - the map
                 is still ranged on every call, so a bodyless member or a clone
                 added mid-emit is still found.
                 RESULT, measured on mxc-r93's boxed self-build (u93.log):
                 runtime's cau is now FLAT - 40,40,41,41,41,41,41,42,42 at
                 nf=1..860 - so Step 2's Accept clause `EAF0 vs EAF nf=N within
                 5MB` is MET (it was 115MB out). The last package's baseline at
                 the same member fell from `rss=740MB cau=470MB` to
                 `rss=694MB cau=424MB`, and the same build's peak NEWPEAKs fell
                 from 747MB to 694MB.
                 STILL NOT ENOUGH FOR THE FIXPOINT: the build still livelocks at
                 `(*irEmitter).emitBinOp` (nf=228 of the last package), now at
                 rss 694MB + that member's ~300MB. The box's cgroup sits at
                 memory.current=954MB of which anon=949MB (anon_thp=832MB) with
                 file=2.7MB, memory.high climbing ~4600/6s and utime frozen while
                 stime runs at ~30% of a CPU: the kernel cannot reclaim anon with
                 MemorySwapMax=0, so the run is throttled into no progress rather
                 than killed (oom_kill=0). ~150MB of peak has to come off before
                 the boxed self-build can finish, and the two remaining terms are
                 Step 4's per-member cost (that member's arena reaches used=305MB)
                 and Step 6's retained package graphs (cau=424MB at that point,
                 which is the baseline the member is added on top of).

2026-11-17 0.3g  WHERE THE FIXPOINT'S ~150MB GAP NOW SITS, MEASURED.
                 With the selection fix in (mxc-r93's u93.log) there is no
                 per-member leak left anywhere, including the last package:
                 `./_mxc_stage4`'s cau is 423,423,423,423,424,424 at nf=1,50,100,
                 150,200,227 and its rss is 678,684,684,684,684 across the same
                 span. The emit floor for `runtime` is 40MB and stays 40-42MB.
                 What is left is a HIGH FLOOR, not growth: the last package's
                 `M1 header done rss=687MB` prints BEFORE its first member is
                 emitted, and its emit floor is cau=423MB. The stall then adds
                 one member's cost on top: `(*irEmitter).emitBinOp` uses 305MB
                 (u84's EAFU for that member), so 687 + 305 = ~990MB against the
                 box's MemoryHigh=900M / MemoryMax=1G, all of it anonymous
                 (anon=949MB of 954MB, file=2.7MB, swap off), which is why the
                 kernel throttles and the run makes no progress instead of dying
                 (oom_kill=0, memory.high +4600/6s, utime frozen).
                 So the next unit of work is one of the two the plan already
                 names, and it is chosen by these numbers:
                   - Step 6 / the retained package graphs behind the 423MB floor
                     (687MB rss before the last package emits anything); the
                     M1/M2/M4 markers all print at ~687-689MB, so it is state
                     that exists before per-member work begins.
                   - Step 4 / the member's own 305MB (the plan's "enable the
                     builder's borrow and keep the emitter's off" - a bilateral
                     codegen change), which would drop the body before the emit.
                 ~150MB off either term gets the boxed self-build through, and
                 that is the last thing between here and the fixpoint check plus
                 the end-to-end verification of the strLitIndex fix.

2026-11-17 0.3h  WHAT THE 687MB FLOOR IS MADE OF, AND WHY STEP 4'S ACCEPT AS
                 WRITTEN CANNOT HOLD.
                 From the last census dump before the stall (u93.log):
                   scopebytes id=41 n=4019 used=465330112 cap=588210176
                   scopebytes id=42 n=45   used=37782896  cap=61796352
                   scopebytes id=44 n=3067 used=8172112   cap=46727168
                   scopebytes id=45 n=1    used=105321024 cap=134217728
                   livetot n=7232 used=616949552 cap=835850240
                   EAF0cen census n=7232 tiny=6989 tinycap=100950016 hdr=10728448 sov=3688
                 So the floor is NOT the retained package graphs: scope 42 holds
                 one arena per package (n=45, 37MB), and scope 44 (n=3067) is the
                 reset per-call arena pool at 8MB. The 465MB is scope 41 with
                 4019 LIVE arenas, and the census says 3688 of the 7232 live
                 arenas are SOVEREIGN - the self-mutating types' own arenas. One
                 arena (scope 45, n=1) holds 105MB of a 128MB region.
                 Accounting is honest, not inflated: ArenaReset zeroes region 0
                 (`a.regions[0].off = a.dataOff`), unmaps every extra region and
                 clears its registry words, so a reset pooled arena reports only
                 its data offset - the 465MB is memory that is genuinely held.
                 The per-member cost is also measured now: over 5413 members in
                 u84.log, `used` at the member's end is median 0MB, p90 1MB, and
                 the tail is a handful of monsters - 67, 116, 126, 145, 150, 183,
                 184, 305MB - with (*irEmitter).emitBinOp at 305MB. (main.main is
                 not among the EAF lines at all; it is emitted in the last
                 package, after the stall point.)
                 STEP 4's Accept as written - "EAFU used=0MB at every function
                 end" - is therefore not achievable and not the right target: the
                 median member already IS 0MB, and the peak is one monster
                 function on top of the floor. The honest restatement is "peak =
                 the largest single function plus the floor", and the lever for
                 the fixpoint is the FLOOR: 687MB of rss exists before the last
                 package emits its first member, while the member adds 305MB.
                 ~150MB off the floor (3688 sovereign group arenas, 465MB) gets
                 the boxed self-build through, and that is now the next unit of
                 work: why a compiler build has 3688 live sovereign groups and
                 what holds their 465MB, given `-no-main-arena` is supposed to
                 keep main's self-mutating scratch in the current arena.

2026-11-17 0.3i  THE FIXPOINT'S REMAINING ~150MB NAMED: IT IS PRE-PASS STATE, NOT
                 THE PACKAGE GRAPHS, NOT PER-MEMBER WORK.
                 `EAF0 enter` already splits the floor, and for the last package
                 it reads:
                   EAF0 enter n=1541 rss=689MB acap=797MB cau=423MB cac=478MB
                              graphs=45/36MB root=44/59MB
                 (u93.log:21863). So at the moment the last package starts
                 emitting, the whole floor is:
                   compile arena      423MB   <- the entire gap is here
                   root arena          59MB   (44 regions)
                   package graphs      36MB   (45 arenas, one per package)
                   other rss          ~170MB  (pools, sovereign groups, process)
                 and one member on top of it - (*irEmitter).emitBinOp needing
                 305MB - puts the cgroup over MemoryHigh=900M and the run
                 livelocks (954MB, anon 949MB, no swap).
                 The graphs are NOT the problem: 36MB for 45 packages. Neither is
                 per-member work any more (cau is flat inside every package, and
                 scope 44's reset pool is 8MB). The 423MB is written BEFORE the
                 first member is emitted - it is the PRE-PASS: the export pass
                 that produces every package's mxh, the typecheck of every
                 package, and the dispatch index - which is the state Step 5
                 ("export generic bodies, keep no AST") and Step 6 ("inter-package
                 state lives on disk") exist to remove. The root's 59MB is the
                 durable caches this branch added deliberately (keepStr'd export
                 texts and the root*Map helpers) and is the smaller, more
                 tractable half.
                 Consequence for the plan's Accept lines: Step 2's two clauses are
                 now met (leak-free run; cau flat across runtime's 866 members,
                 which the selection fix delivered). Step 4's Accept as written
                 ("EAFU used=0MB at every function end") is not achievable and
                 should be restated as "peak = largest single function plus
                 floor": the median member is already 0MB and the tail is a
                 handful of monsters (67..305MB). The peak clause now reduces to
                 the FLOOR, so the next unit of work is Step 5/6 on the pre-pass's
                 423MB, not more per-member work.

2026-11-17 0.3j  THE 288MB IS PER-EMITTED-FRAGMENT SCRATCH, NOT THE TEXT AND NOT
                 THE ACCUMULATOR MAPS.
                 `EAF` now carries `fb=` (the member arena after the BODY build)
                 and a heartbeat inside `w()` (`W segs= text= farena= vn= rt= at=
                 tc= st= ua= ln= dv= fb= ho= hb=`), so the emit of one member can
                 be watched while it runs:
                   EAF ... fn=(*irEmitter).emitBinOp blocks=738 fb=17MB
                   W segs=98304  text=1003KB farena=168MB vn=1643 rt=718 at=739 tc=400 ua=3424 ln=1877 hb=369
                   W segs=139264 text=1441KB farena=215MB vn=2538 rt=1171 at=1192 tc=423 ua=3424 ln=1877 hb=475
                 Read that carefully:
                 * the body build leaves the member arena at 17MB, so 288MB of the
                   305MB is the EMIT, not the body graph (Step 4's premise is
                   beside the point for the peak).
                 * the emitted TEXT is 1.4MB for 139264 segments - the segment
                   slice and the assembled buffer are NOT the hog, so streaming
                   the flush would buy almost nothing.
                 * the accumulator maps are tiny (valName 2538, regTypes 1171,
                   allocTypes 1192, typeCache 423, blockExitLabel 475).
                 * the arena grows 8.5MB per 8192 segments, uniformly: ~1KB of
                   arena per emitted fragment, against ~10 bytes of text per
                   fragment. A uniform per-fragment cost, not a buffer doubling
                   (that is geometric) and not an accumulator (their counts are
                   flat by comparison).
                 So the remaining ~150MB the fixpoint needs is ONE of:
                   (1) that ~1KB-per-fragment emit scratch, which is stage4-local
                       and would show up in the per-member allocation histograms
                       (the EAF `hist` only prints bins 12..20 = 4KB..1MB, so a
                       ~1KB class is invisible there - print per-member bin
                       DELTAS for bins 4..12 next, then find the site); or
                   (2) the pre-pass floor (cau=423MB, region.md 0.3i).
                 (1) is by far the better target: it is bounded by one member, it
                 is the emitter's own scratch, and the measurement above already
                 proves the text, the segments and the named accumulators are
                 innocent.
                 Stop state: dev untouched at 5bfa352e; jubilee-step2-wip, HEAD
                 4d7f8756 plus the new diagnostic commit; no processes running.

2026-11-17 0.3k  THE MEMBER'S 288MB IS EMIT-PATH STRING GARBAGE: ~20 SMALL
                 TEMPORARIES PER EMITTED FRAGMENT. SYSTEMIC, NOT A ONE-LINER.
                 With the exact-size histogram (fineAllocCount, sizes 1..192;
                 recordAllocSize's floor(log2) bins cannot separate 32 from 63):
                 * bin 5 (32..63B) dominates at ~173600 allocations per 8192
                   emitted segments, i.e. ~21 per segment.
                 * the top exact sizes are spread, not fixed - 80B (2.82M), 48B
                   (2.70M), 64B (2.13M) cumulative, and their deltas over one
                   heartbeat are only 5632+286+2432 - under 5% of bin 5. Varying
                   sizes across one bin is the signature of string CONCATENATION
                   results, not a fixed-size buffer.
                 * the emitted TEXT is 1441KB for 139264 segments (~10 bytes per
                   segment), while the member arena reaches 215MB and rising. So
                   ~213MB of the member's arena is GARBAGE, not text: abandoned
                   intermediates that die only at that member's ArenaReset.
                 Where it comes from - the hot helpers all build small strings:
                   nextReg2(prefix)  = "%" | prefix | irItoa(n)   -> 2 allocations
                   declareRuntime()  = params | ", ptr" then
                                       retType | " @" | name | "(" | params | ")"
                                                                  -> 2-3, plus
                                       whatever the caller built params with
                   irItoa/irItoa64   = []byte{:0:20} + a string copy -> 2
                   plus each e.w(<chain>) -> 1-2
                 and the tree has ~200 `x = x | part` cross-statement accumulation
                 sites (ir_emit 16, main 88, ssa_builder 14, ir_typecheck 19,
                 ir_descriptors 18, ...), the pattern the language's own
                 concatenation rule warns about.
                 So the ~150MB the fixpoint needs is available here, and the fix
                 is a BOUNDED REFACTOR rather than a one-line change: give the
                 emitter a reusable line/scratch buffer (and allocation-free
                 number formatting, e.g. one shared 24-byte buffer in irItoa
                 instead of a fresh []byte{:0:20} + copy per call), then convert
                 the hottest fragment builders to append into it. Estimated: a
                 10-20x cut in per-member garbage takes the member's peak from
                 305MB to ~30MB and the build's peak to ~720MB, i.e. under
                 MemoryHigh=900M, which is what the boxed self-build needs to
                 finish and what the fixpoint check is waiting on.
                 (Tried and rejected as a lever: streaming the flush. The
                 segments and the assembled buffer are only 1.4MB; the garbage is
                 produced by the CALLERS' temporaries, so writing earlier frees
                 none of it.)

2026-11-17 0.3l  THE FLOOR'S 194MB WAS A QUADRATIC COMPILER CHECK, AND FIXING IT
                 LET THE BOXED SELF-BUILD COMPLETE - AND VERIFIED THE strLitIndex
                 FIX END TO END.
                 Attribution first, by adding memLine after each step of
                 CreatePackage (ssa_builder.mx) and after each check in its
                 declaration block:
                   ci-tc 86MB -> cp-imports 87 -> cp-files 94 -> cp-bodies 95
                   -> cp-checks 110 -> cp-sovereign 110 -> cp-ifaceassign 110
                   -> cp-astfile 110 -> cp-impconf 302MB
                 One call, `pb.checkImportConflicts(files)`, was +192MB of the
                 423MB floor. Its inner loop called `pb.importedNamesOf(imp)` -
                 which ALLOCATES a name list on every call, and for a dot import
                 walks every exported name of the imported package - inside a
                 PAIRWISE loop over import specs: quadratic in allocations as
                 well as comparisons. Hoisting the lists out of the loop helped
                 only 2MB (302 vs 304), because the cost is the lists themselves,
                 one per import SPEC (the driver concatenates a package's files
                 and tracks per-file import segments). So it belongs in a scratch
                 arena, exactly like the five declaration checks beside it:
                 `addCompileError` already copies into cctx.compileArena through
                 an explicit arena switch, so nothing durable is lost.
                 RESULT (u101.log, mxc-r101):
                   chkA ic used=191MB          <- the check's own transient peak
                   cp-impconf main cau=110MB   <- was 302MB
                   EAF0 enter n=1541 rss=497MB cau=230MB   <- was rss=684 cau=421
                 and the boxed self-build RAN TO COMPLETION for the first time
                 since the strLitIndex fix: past member 228, all 45 packages,
                 `objects: 53 files`, `link`, `-> ~/mxc-builds/mxc-r101-2`.
                 END-TO-END VERIFICATION OF THE strLitIndex FIX (the thing that
                 was blocked all session):
                   mxc-r101-2 build      -> "mxc: no package specified"  (it
                                            RECOGNISES build; before, the usage
                                            fallback at main.mx:8234)
                   mxc-r101-2 help       -> real help text
                   mxc-r101-2 targets    -> "linux/arm64", no garbage
                   mxc-r101-2 cache      -> its usage
                 So the self-built compiler is no longer miscompiled: the argv
                 dispatch works and the string constants are intact.

2026-11-17 0.3m  THE FIXPOINT'S THIRD DEFECT: THE SELF-BUILT COMPILER'S OWN
                 INTERFACE DISPATCH TABLE IS INCOMPLETE.
                 `mxc-r101-2 build -a -no-main-arena -o mxc-r101-3 ./_mxc_stage4`
                 (u101b.log) runs through the packages and aborts at member 106
                 of one of them:
                   panic: runtime error at 0x456f6f: interface dispatch: no impl
                   for SSAType (64 impl(s), none matches)
                   ... fn=Identical blocks=129
                 So the FIRST two blockers are gone and the binary that comes out
                 of the self-build is no longer miscompiled in the ways this
                 session fixed: argv dispatch works, string constants are intact,
                 a full 45-package build completes inside the box with an emit
                 floor of cau=230MB (was 421MB) and rss 497MB (was 684MB).
                 What fails now is the DISPATCH TABLE of that binary: the
                 interface `SSAType` has 64 arms and the receiver's concrete type
                 matches none, i.e. the compiler that EMITTED r101-2 enumerated
                 an incomplete arm set for the main package. This is the shape
                 CLAUDE.md records under "A cache key must not depend on
                 measurement state" (round 371 lost the interface-dispatch table
                 on every cold build; export data and object lifetime are
                 different inputs, and the dispatch fingerprint belongs only to
                 the object key), and its gate is tests/phase0c_coldcache.sh.
                 `-a` makes this a cold build, which is exactly the window that
                 section describes: the dependency pre-pass writes the
                 pre-measurement name and the compile phase reads the
                 post-measurement one.
                 Next session starts there: run tests/phase0c_coldcache.sh, then
                 compare the arm set the pre-pass computes for the main package
                 with the one the emitting phase uses, and whether the arm for
                 the missing type is enumerated at all (the type is one of
                 main's 64+ SSA value types; `fn=Identical` is where the dispatch
                 happens, so the value is a *SSAConst or *SSABinOp-like operand).

2026-11-17 0.3n  THIRD DEFECT, SHARPENED: IT IS A DISPATCH LOOKUP WHOSE RECEIVER'S
                 TYPE WORD MATCHES NO ARM - AND THE FAULT IS A LIFETIME, NOT AN
                 ENUMERATION, SO FAR AS THIS READING GOES.
                 What is known:
                 * The panic string is EMITTED CODE, built by
                   ir_iface.mx:2064 in the dispatch thunk the compiler writes for
                   an interface call site. The thunk is a chain of typeID
                   comparisons - one arm per impl - and the default block panics
                   with "no impl for <method> (N impl(s), none matches)". Here
                   the method is SSAType, i.e. the dispatch is through main's
                   `SSAValue` interface (ssa_types.mx:101).
                 * It happens while emitting the member `Identical` of
                   `moxie.dev/pkg/types` (tc_assign.mx:36), early in that
                   member's emit (the EAF line printed, no W heartbeat yet), so
                   the compiler's own code was dispatching SSAType on a value it
                   had just read out of the body it is emitting.
                 * The comment above the panic anticipated two causes: an
                   incomplete list, or a list that holds every named type "but
                   not the INSTANTIATION being dispatched". The second does not
                   apply here: main has no generic SSA value type (`grep -E
                   "^type (SSA|ssa)[A-Za-z0-9_]+\["` finds none), and the
                   interface is implemented by ~55 non-generic `SSA*`/`ssa*`
                   struct types in main. So the arm list of 64 is plausibly
                   complete, and the receiver's type word matched none of them.
                 * That reading makes this a LIFETIME defect, not a codegen one:
                   an `SSAValue` interface value whose 16-byte type word was
                   reclaimed and overwritten reads as a type no arm can match,
                   and this branch's fail-loud default is exactly what turns that
                   into a named panic instead of a garbage call. It is also the
                   same class as the codec-escape warning every compiler build
                   still prints at startup (u101.log line 1), which says out loud
                   that a pointer into a transient frame arena is being aliased.
                 * It cannot be attributed to this session's changes without a
                   further test: no earlier self-built compiler ever got here.
                   mxc-r84-2's argv dispatch was broken (region.md 0.3c/0.3l) so
                   it could not run at all, and every attempt between r84 and
                   r101 stalled in the box (0.3e). The floor fix in 0.3l was
                   checked for the obvious hazard - the wrapped check only READS
                   durable state (pb.segImports, the packages' scopes) and
                   reports through addCompileError, which copies into
                   cctx.compileArena via an explicit arena switch - so it does
                   not lose durable registrations, but that is an argument, not a
                   measurement.
                 NEXT SESSION starts here: instrument the emitted default block
                 to print the receiver's type word and the arms' typeIDs (a
                 fail-loud message change, local, revert before merge), which
                 names the concrete type that was dispatched and turns the
                 lifetime hunt into "who wrote that interface value, and into
                 which arena". Run it as:
                   mxc-r101-2 build -a -no-main-arena -o /tmp/x ./_mxc_stage4
                 which reaches the panic in a few minutes now that the floor is
                 230MB instead of 421MB.

2026-11-17 0.3o  THIRD DEFECT: THE FACTS AS THEY STAND, SO THE NEXT PROBE IS AIMED.
                 * The panicking run is clean by this branch's own lifetime
                   diagnostics: 0 LEAK, 0 DSYNC, 0 DBEGIN-odd in u101b.log. So
                   no member's body was built in a foreign arena, per those
                   checks.
                 * The failure is in `moxielang.dev/pkg/types` (the third package
                   from the end), at member 106 of that package, after 3879
                   members had been emitted overall - the compiler ran a long way
                   before it hit this.
                 * `SSAType()` is declared by exactly ONE interface in the whole
                   tree: main's `SSAValue` (ssa_types.mx:101-107). So the 64-arm
                   list is `SSAValue`'s impl set, and the failing dispatch is in
                   the CALLER's emitted chain.
                 * The compiler contains NO generic types at all (`grep -E
                   "^type [A-Za-z0-9_]+\["` over _mxc_stage4/*.mx finds none), so
                   the code's own alternative reading - "the list holds every
                   named type but not the INSTANTIATION being dispatched" -
                   cannot apply to `SSAValue`. Either the 64 arms are complete and
                   the RECEIVER's type word was reclaimed (a lifetime bug), or an
                   arm is missing for a type declared outside main.
                 * Where the arms come from is the dispatch index built from every
                   package's mxh, and this file already records why that index is
                   fragile: "an interface arm is enumerated into the CALLER's
                   package, so an upstream package must see a downstream type
                   (main) that has not been compiled yet". `pkg/types` is emitted
                   BEFORE main, so a call site inside `pkg/types` counting
                   `SSAValue` arms needs main's types through that index.
                 THE NEXT PROBE, in order of cost:
                 1. Emit the receiver's typeID into the thunk's panic message and
                    dump the arms' typeIDs with their type names at emit time
                    (both are local, fail-loud diagnostics; revert before merge).
                    Run `mxc-r101-2 build -a -no-main-arena -o /tmp/x
                    ./_mxc_stage4`, which now reaches the panic in a few minutes,
                    and match the receiver's typeID against the arm dump.
                 2. If the receiver's typeID is absent from every arm while the
                    type is a legitimate `SSAValue`, the missing arm is an
                    index/enumeration bug (the dispatch-fingerprint class, gate
                    tests/phase0c_coldcache.sh). If the typeID matches an arm's
                    type but the arm is not in THAT thunk, the thunk's list was
                    enumerated before the type existed. If the typeID matches
                    nothing in the program, it is a reclaimed interface value and
                    the hunt is a lifetime one.

2026-11-17 0.3p  THIRD DEFECT RESOLVED TO A CLASS: IT IS A LIFETIME BUG, NOT A
                 DISPATCH-INDEX OR CODEGEN ONE.
                 The emit-time dump of every `SSAType` call site in the compiler
                 (ir_iface.mx emitIfaceChain, printing each arm's descriptor
                 operand) settles it: 406 sites, and the arm set is IDENTICAL at
                 every one of them - exactly one distinct set of 64 arms (4487
                 bytes of descriptor names). No type outside `_mxc_stage4`
                 implements SSAType either (`grep -rn "SSAType()" pkg/ src/`
                 finds nothing). So the list is complete and consistent, the
                 per-call-site enumeration is not the problem, and the receiver
                 that matched none of the 64 arms was carrying a descriptor that
                 is not any program type at all: a reclaimed or overwritten
                 interface value.
                 That puts this in the same class as the codec-escape warning
                 every build still prints at startup ("pointer into a transient
                 frame arena preserved instead of copied"): an aliased pointer
                 surviving into the emit. It also retires the other two readings
                 in 0.3o - a missing arm (the arm sets agree) and a thunk
                 enumerated too early (same set everywhere).
                 NEXT: name the emitting function at the panic. The thunk is
                 inline in the function that contains the call site, so the
                 return address at the panic identifies it, and 406 sites
                 collapse to the handful in one emitter function. Print the stack
                 words from the runtime's panic path (SovPrintStackWords already
                 exists for exactly this), then resolve the address with nm on the
                 self-built binary (it keeps symbols; r101-2 has 134 T entries).
                 A dump from this round is in u102.log (406 `DISP fn=...` lines,
                 one line per site, with the caller function name).

2026-11-17 0.3q  THIRD DEFECT: THE FAILING SITE IS NAMED, AND THE RECEIVER IS A
                 FIELD-ADDRESS BASE.
                 No new probe was needed: `_panicstr` (src/runtime/panic.mx:101)
                 calls `runtimePanicAt(returnAddress(0), msg)`, so the panic line
                 "panic: runtime error at 0x456f6f: ..." already carries the
                 failing call site's address - printed by the compiler into its
                 own panic, in the binary under test (mxc-r101-2).
                 Resolved against that binary:
                   nearest symbol <= 0x456f6f : main.(*irEmitter).emitFieldAddr
                   addr2line -f -C             : main.(*irEmitter).emitFieldAddr
                 So while emitting member 106 (`Identical`) of
                 `moxielang.dev/pkg/types`, the emitter was inside
                 `(*irEmitter).emitFieldAddr`, and the SSAType dispatch that
                 matched none of the 64 arms was on the BASE VALUE of a field
                 address (SSAFieldAddr) - i.e. that instruction's base operand
                 held an interface value whose descriptor is not any program type
                 (0.3p established the arm set is complete and identical at all
                 406 sites, and no type outside the compiler implements SSAType).
                 NEXT STEP, with the site known:
                 1. Read `(*irEmitter).emitFieldAddr` and note exactly which
                    operand it dispatches SSAType on (the field address's base
                    X, per the instruction the builder produced).
                 2. Then instrument the BUILDER for that instruction shape: print
                    where the base value comes from and which arena it lives in
                    (the branch has `%p`-style arena probes and durableBegin
                    diagnostics for this). The leading hypothesis, given this
                    step's design, is a CROSS-MEMBER reference: member 106's body
                    holds a value whose instruction object was built in an EARLIER
                    member's arena and released by that member's ArenaReset
                    (bodies are built per member now, and a value that survives in
                    a durable map, a clone record or the member table carries a
                    pointer into a dead arena - exactly the shape that also
                    produces the startup codec-escape warning).
                 3. A cheap discriminator for (2): make ArenaReset poison what it
                    reclaims (e.g. write a non-type value over the descriptor word
                    of every interface-sized slot it frees is not possible, but
                    ArenaReset already has `memzero`-style helpers) - or simpler,
                    check whether the base value's arena is still on the fn stack
                    at the moment of the dispatch (`sovFindContainingArena`).
                 State: 406 `DISP fn=...` site lines and the arm set are in
                 u102.log; the panic line and address are in u101b.log.

2026-11-17 0.3r  THIRD DEFECT: THE EXACT OPERAND.
                 `emitFieldAddr` (ir_emit.mx:3853) does:
                     base := e.operand(f.X)
                     if uop, ok := f.X.(*SSAUnOp); ok {
                         addrType := e.llvmType(uop.X.SSAType())        <- here
                         if p, ok2 := SafeUnderlying(uop.X.SSAType())...
                 so the SSAType dispatch whose receiver matched none of the 64
                 arms is on `uop.X` - the operand INSIDE the SSAUnOp that is the
                 BASE of a field address. The instruction object or its X
                 interface value was reclaimed/overwritten: the panic is a read
                 of a dead interface, and the three SSAType calls on the same
                 expression are three reads of it.
                 NEXT SESSION: instrument the BUILDER at the point it creates an
                 SSAFieldAddr whose base is an SSAUnOp (ssa_builder.mx
                 buildSelectorAddr / buildFieldAddr), printing where `uop` and
                 `uop.X` come from and which arena they were allocated in; then
                 check whether that arena is still on the fn stack when
                 emitFieldAddr reads it (`sovFindContainingArena` answers that
                 directly). `Identical` is a type switch, so its case bodies take
                 field addresses through narrowed interfaces, which is where a
                 value that outlives its member's arena is most likely to be
                 stored.
                 Handoff state: all of the above is in u101b.log (panic + address
                 0x456f6f), u102.log (406 DISP site lines, one arm set) and
                 region.md 0.3c-0.3r. dev untouched at 5bfa352e; branch
                 jubilee-step2-wip; HEAD 3b75effb; tree clean; nothing running.

2026-11-17 0.3s  HANDOFF DETAIL FOR THE DEAD-INTERFACE HUNT: THE FIELD-ADDRESS
                 CHAIN THAT MATCHES THE FAILING BRANCH.
                 `emitFieldAddr` takes the `f.X.(*SSAUnOp)` branch when a field
                 address's base is a deref, and the builder creates exactly that
                 shape only in the EMBEDDED-FIELD path (ssa_builder.mx:8205-8215):
                     zzq34 = &SSAFieldAddr{X: cur, Field: idx}      // one hop
                     ... fb.emit(zzq34)
                     zzq35 = &SSAUnOp{Op: OpMul, X: zzq34}          // deref it
                 so the chain is FieldAddr -> (X) UnOp -> (X) FieldAddr, all built
                 in the same member's builder and read by the emitter in the same
                 member. The simple field case is ssa_builder.mx:8187
                 (`&SSAFieldAddr{X: addr, Field: fieldIdx}`).
                 Both the field address and its `X` are `SSAValue` interface
                 values, and the failing read is one of them (or of a value the
                 inlined `operand`/`resolveFieldAddrBase` walks from it): the
                 descriptor matched none of the 64 arms, so the 16 bytes holding
                 that interface were reclaimed and overwritten while the emitter
                 still had the pointer.
                 Two things to check first, both cheap:
                 1. `-no-main-arena` is what puts main's own mutating-method
                    scratch in the CURRENT arena rather than a sovereign one, and
                    this member's body is built in the fn arena that the member's
                    own emit then reads. The LEAK/DSYNC diagnostic printed by the
                    same loop reports 0 in the failing run, so the body was where
                    it should be - which points the hunt at something that
                    SURVIVES the reset: a clone record (cctx.genericMethodInsts),
                    a member-table entry, or a durable map holding an SSAValue.
                 2. Ask the runtime which arena the failing pointer is in. The
                    codec already answers that question for arbitrary pointers
                    (`sovFindContainingArena`, `sovInSovereignDataArena`,
                    `codecIsStableAddr`); calling one of those from emitFieldAddr
                    on `f` and on `f.X`, and printing the arena's scope and
                    whether it is still on the fn stack, names the dead arena in
                    one run.

2026-11-17 0.3t  CHECKED AND EXCLUDED FOR THE DEAD-INTERFACE HUNT.
                 The obvious candidate was a durable `SSAFunction` field still
                 pointing into the member's arena after its ArenaReset. It is
                 excluded, and the exclusion is the useful part:
                 * `SSAFunction`'s arena-bearing fields are Params, FreeVars,
                   Locals, Blocks, AnonFuncs, NamedResults, Recover, vars.
                 * emitBuiltMember's per-member reset clears Blocks, Locals,
                   Params, FreeVars, NamedResults and vars (ir_emit.mx:912-917).
                 * AnonFuncs is cleared by emitAnonFuncsOf itself (`f.AnonFuncs =
                   nil`, ir_emit.mx:2106), and it is called at the top of
                   emitBuiltMember (ir_emit.mx:894) - so it is nil before the
                   reset (I expected this one to be the bug and it is not).
                 * Recover is never cleared, but its only readers are in the
                   CURRENT member's emit path (ir_emit.mx:2581, 2589, 2801, 3233),
                   all of which run before the reset, and ir_scope's AnonFuncs
                   walks (ir_scope.mx:1970, 1989) run in the checks, also before
                   the emit. A dangling Recover is therefore unread.
                 So the dead `SSAValue` read in emitFieldAddr is NOT one of the
                 member-table's cleared fields. That leaves, in order of
                 likelihood: (a) an emitter or builder map keyed by an SSA value
                 that survives the member (the accumulator list is cleared, but a
                 map that is NOT in that list would behave exactly like
                 strLitIndex did in 0.3c); (b) a clone/monomorphisation record
                 (cctx.genericMethodInsts and friends) holding a value whose arena
                 is gone; (c) a value in the fn arena read AFTER that arena was
                 reset by a DIFFERENT member's iteration - i.e. the emit of one
                 member reaching state that belongs to another.
                 The direct measurement for all three is still 0.3s item 2: ask
                 the runtime which arena the failing pointer is in (the codec's
                 sovFindContainingArena/sovInSovereignDataArena), printing the
                 scope and whether that arena is still on the fn stack, from
                 emitFieldAddr before the SSAType dispatch that panics.

## 0.3u — the fixpoint panic is a NIL `SSAUnOp.X`, and the sovereign-loop check was silently dead

**The dead `SSAValue` read is a nil interface, not reclaimed memory.** The core
from the second-generation self-build (`coredumpctl`, PID 63157, `mxc-r102-2`)
resolves both panic PCs to `main.(*irEmitter).emitFieldAddr`; the disassembly
around them is a 64-arm dispatch chain over `cmp $<descriptor addr>,%rax`
(all 64 arms of `SSAType`, i.e. all 32 `SSAValue` implementors twice - the list
is COMPLETE, so it is not a missing arm) ending in the `_panicstr` call for
"no impl for SSAType". The chain reached the default because `%rax` was zero,
and the register is loaded from `0x40(%r15)`, the second word of `uop.X`:

```
f      = SSAFieldAddr  @0x7f60388b97f0  name "t161"
f.X    = SSAUnOp       @0x7f60388b9520  name "t160", Op=OpMul, typ=*pkg/types.Pointer
f.X.X  = {0x0, 0x0}                     <- NIL interface
```

Every other field of `t160` is intact (block ptr, name ptr/len, typ, pos=0), so
the struct was not reclaimed wholesale: `X` is a nil interface value. The
dispatch failing is the CONSEQUENCE; the cause is whoever built an `SSAUnOp`
with a nil `X`. Not a lifetime bug of the kind 0.3c was.

The same shape is on dev: `mxc-r102` (legacy-built) compiles `pkg/syntax` fine,
`mxc-r102-2` (built BY `mxc-r102`) does not, and the member it dies in is not
stable between generations (`Identical` in `pkg/types` in u101b,
`(*Parser).paramDeclOrNil` in `pkg/syntax` in u103) - i.e. the SECOND-generation
compiler is the broken one, which is the long-standing "self-built generation is
corrupt" class, not a regression of this branch.

**Step 3's move, measured.** `checkSovereignLoopDecls` and `checkIfaceAssignments`
were still CALLED from `CreatePackage`, where Step 2 builds no body: the first
asks `ssaFuncHasReceiverWrite` (walks `fn.Blocks`) and the second drains
`cstate.pendingIface` (filled by `checkAssignable` during a body build). Both
judged an empty subject - the checks were dead from the day the body build moved
into the per-member pipeline. `checkIfaceAssignments` already had a per-member
call in `emitAllFunctions`; the sovereign-loop check did not, and now does
(`checkSovereignLoopDeclOf(p, fn)`, declaration recovered from
`p.b.funcDecls[fn.name]` the way `checkDuplicateCasesOf` does it).

Fixture `/tmp/sovloop-c` (a `Pair{A:i,B:i}` literal inside a loop of a method
that stores through its receiver) is now REJECTED with the rule's message; the
same fixture in the two order variants is not, and the reason is worth keeping:
`ssaFuncHasReceiverWrite` is TRANSITIVE through receiver calls (legacy's
`ssaFuncWritesReceiver` is too, and the sovereign prologue uses it), but in a
one-body-at-a-time pipeline a callee's `Blocks` are nil - either not built yet
or already cleared by `emitBuiltMember`'s reset after its own emit. Measured:

| variant | caller | callee | `note: ... is mutating` | check fires |
| --- | --- | --- | --- | --- |
| a | `fill` | `add` (sorts first) | `add` only | no |
| b | `alpha` | `zeta` (sorts last) | `zeta` only | no |
| c | `fill` stores directly | - | `add` and `fill` | yes |

So the transitive arm of the predicate can never answer true in this pipeline,
and the same blindness is in the PROLOGUE's borrow decision: a method that
mutates only through a callee is classified read-only and pushes its own frame
arena instead of borrowing the receiver's. That is a policy difference from
legacy (scratch dies at return rather than persisting in the sovereign arena),
not a dangling-pointer bug, and the check now agrees with the prologue because
both ask the same question at the same moment - which is what coherence requires
even while the question itself is body-lifetime-dependent. Recording it as Step
2's real remaining defect: a cross-member query asked while only one body exists.

**Step 3 Accept, measured on `mxc-r103` building `tests/tmp/litprobe` (p103.log),
runtime package:** `cp-checks cau=32MB`, `chk decls cau=32MB`,
`chk astfile cau=32MB`, `chk importConflicts cau=32MB` - the whole check suite
adds **0MB** to the package arena for `runtime` (it was +239MB before the
per-check arena, +7MB at the last stop). The three named negative fixtures pass
on `mxc-r103`: `reject_duplicate_case`, `reject_missing_return`,
`reject_shadow_type_switch`.

**Follow-up probes on the classification (same entry).** Two shapes were built
and run with `mxc-r103` to see whether the misclassification is merely a policy
difference or produces a dangling read. (1) `fill` builds a `[]string` locally
and hands it to a directly-mutating `put` that stores it in the receiver; after
`fill` returns, `main` overwrites frame memory with a 4000-element slice and
then reads `b.items` - it still prints `hello`, because the callee's arena owns
the copy the argument was relocated into. (2) The same with `-no-main-arena`
(no sovereign arena at all): also `hello`. So the misclassification is a
DIVERGENCE from legacy's classification, not (in these two shapes) a
demonstrated dangling read: the stored value is owned by the callee's arena, not
by the misclassified caller's. Fixtures: tests/tmp/sovdangle/main.mx.
