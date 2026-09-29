# Axioms of Moxie

Two axioms generate this language. Everything else in it is a derivative, not a decision. Nothing in Moxie is a preference, a convention, or a matter of taste, and the reason there is nothing to argue about is that the axioms are strong enough and few enough that the design closes.

The operational test that keeps this honest: a proposed feature must name the axiom that forces it. If neither axiom forces it, it is an opinion, and it is deleted.

---

## A1 - Just the facts

**Every property that governs execution is declared and statically checkable. Nothing that determines behaviour is inferred, negotiated, or discovered at runtime.**

The declared set is small and closed:

- the topology (which component is the parent of which)
- for each product, its consumers
- for each product, its required lifetime
- for each pooled task, its expected rate
- every message type that crosses every edge

## A2 - Total isolation

**No component can address another component's state, and every fact has exactly one authoritative owner.**

Two clauses, and both are load-bearing:

- **mechanism:** no shared address space. Memory is per-domain. Nothing addressable is common between components except explicitly constructed transfer regions.
- **property:** single authority. No fact has two canonical holders.

The mechanism clause alone is insufficient. Pure isolation of access still permits two components to hold copies of the same fact and both treat them as canonical. That is replication, and replication is the contested-claim failure that the topology rules exist to eliminate. Isolation supplies the mechanism; single authority supplies the property that matters.

---

## Justification

### Why A1

Undeclared topology has a bill, and the bill is paid in exactly the failure modes of undeclared topology:

- data races, because shared mutation is permitted and every program therefore carries a convention that fails silently
- unbounded tail latency, because collector behaviour is not a function of program structure
- allocation surprises, because escape analysis decides at compile time what the programmer thought they were deciding
- delivery assumptions built on peers that never started

The absence of A1 admits only two treatments: conservative over-approximation, or runtime detection. Over-approximation means promoting every fact toward the root, which degenerates to a star and is precisely the centralized shape this architecture refuses. Runtime detection means a decoder at every boundary watching for illegal traffic, which makes the observer a co-owner of every type, which violates A2. There is no free repair. So the declaration is the only path, and once declared, the placement check is decidable: spawn sites are syntactic, the topology is syntactic, and comparing them is a syntax pass.

### Why A2

**Coherence exists only because state is shared.** Delete the shared address space and the coherence protocol goes with it, and the memory consistency model with it. No invalidation traffic, no directories, no acquire and release discipline in user code, no SC-versus-TSO argument. Memory ordering bugs are the hardest class in the field, and this is the only architecture class that pays none of that cost.

**Containment is by construction, not by trust.** A wild pointer cannot leave its arena, so there is no capability check on every access, no software sandbox, no reference counting to get right.

**Effects become enumerable.** A component cannot be perturbed by anything except its inputs, so the effects it has are exactly the messages it emits. Nothing else is reachable.

**Purity at the useful granularity.** A component's observable behaviour is a function of its declared inputs and its own arena. That is the kind of purity a type system can enforce, and the kind that makes a component replaceable, testable in isolation, and restartable cold.

### Why exactly two

- **Independent.** A1 does not imply A2: a fully declared shared-memory language is possible. A2 does not imply A1: an isolated system with runtime negotiation is possible.
- **Auditable.** Two axioms fit in a head, which means they can be checked for consistency and redundancy. Every other language also has an axiom set; it is unwritten, larger, partly contradictory, and owned by nobody. Anything unauditable survives by default as an opinion, because you cannot delete what has never been stated.
- **Absorbing.** Two strong axioms with a mutual intersection close the design. What looks like freedom in other languages is the gap an axiom failed to close.

---

## Lemmas

Derivations are marked with the axiom they come from. Some are forced by A1 alone (checkability costs), some by A2 alone (isolation costs), some by their intersection.

### From A1 alone

**L1. No preemption of user code within a domain.**
Preemption makes execution order a runtime property, so no static check can bound it. The domain runs until it blocks or finishes. Corollary: work must be bounded by construction, and the scheduler is cooperative.

**L2. No garbage collector.**
A collector makes the release point a runtime property. Under A1 the release point must be derivable from structure, and the only structural release point available is function return.

**L3. No exceptions.**
The set of raise points and the set of handlers is a dynamic property. Failure is represented as a value that crosses the boundary, and translation happens at the boundary, in the context that can diagnose it.

**L4. Bounded reach: the uncle bound.**
Reach must be checkable, and an unbounded reach graph is not. The maximum legal path is up two and down one (see L23, L24 for the tree semantics).

**L5. No channel mobility.**
A channel inside a message is a dynamically created role, which is not checkable in the local fragment. A channel capability is non-delegable by construction.

**L6. Binary sessions only.**
Multiparty sessions require a global type held by a choreography authority, and projections into local types can deadlock even when the global type is well formed. The binary fragment is the one where liveness is local and decidable per pair.

**L7. Declare, don't infer.**
An inference cannot be refuted, because nothing states what it should have been. A declaration can. Tooling falsifies placements; it does not generate intent.

**L8. No unbounded mailboxes.**
An unbounded queue makes memory a runtime property of arrival rate. Capacity is declared, and the overflow policy is declared with it.

**L9. Memory ordering is confined to the channel.**
Ordering is a runtime property, so it may appear in exactly one place: the channel implementation. No user code contains a fence, an atomic, or an ordering annotation.

### From A2 alone

**L10. No shared address space.**
Memory is per-domain. There is no global heap and no shared allocator.

**L11. No locks, atomics, or read-modify-write in user code.**
Mutual exclusion is not needed because no mutable state is shared between components.

**L12. Exclusion by partitioning: one writer and one reader per direction.**
The write index has exactly one writer and the read index has exactly one writer. Exclusion is obtained by dividing ownership of the state, not by serialising access to it. No CAS on the hot path, and producer and consumer are concurrently active in the same buffer rather than taking turns.

**L13. A channel is an entity, not a memory layout.**
The invariant is role-based: per direction exactly one writer and one reader, and the pair antisymmetric, so both directions always exist. A ring buffer with its four indices is the reference lowering, not the definition. A one-way link is a channel whose reverse direction carries zero message types, so a return path is structural and there is never an unpaired send.

**L14. The channel is the only two-owner object.**
Every other owned thing has one owner. The channel is the interface at a sovereignty boundary, and interfaces are the one category that is necessarily common. Ownership is divided along a line rather than shared: each index has one writer, and each endpoint may read the other's index only as a value. State has one owner; interfaces have two.

**L15. Every boundary decodes.**
No pointer, function, or channel survives a crossing. Each boundary is a serialization point, and the decode is mandatory rather than a transport choice. Cost unit: the number of times the payload's live state is copied and rewritten.

**L16. No forwarding.**
A relay holds a payload it does not own, which is either a copy (duplication, breaking single authority) or an alias (breaking the decode rule). A decoding node that owns nothing relevant is not a bottleneck to optimise; it is a node to delete.

**L17. Single authority per fact.**
One canonical holder per fact. Two holders is a contested claim, which is unresolvable by construction.

**L18. Placement is canonical once the declarations exist.**
For a product, the owner is the lowest node that is within reach of, and outlives, every consumer. Reach is the spatial constraint, lifetime is the temporal one. Uniqueness and minimality coincide, so given the consumer set and the lifetimes there is exactly one correct tree. The design freedom lives entirely in the declarations, not in the shape.

### From A1 and A2 together

**L19. The arena is the only allocation model.**
A2 forces per-component memory; A1 forces the release point to be structural. The intersection admits exactly one possibility: a per-function arena released at return, with relocation and pointer rewriting at the boundary. There is no startup bump allocator and no heap, because either would reintroduce a release point that structure does not determine.

**L20. The topology declaration is the only global artifact.**
A1 needs exactly one thing to check, and A2 guarantees nothing else is global. It therefore plays three roles at once: the intent record, the compiler input, and the review surface. It should diff as a shape rather than as control flow, because it is the only place a non-local bug can live.

**L21. Signal is the operation.**
The index word is simultaneously the state and the notification. Advancing the write index is both the data-availability state and the wake for a parked reader; advancing the read index is both the space-availability state and the wake for a parked writer. There is no separate control message and no notification ordering question, because there is only one write. The parking primitive is a futex on that word, and the expected-value check in FUTEX_WAIT is what makes the check-then-sleep sequence atomic in the kernel.

**L22. Termination is reported, not hidden.**
A worker's death is an event delivered to its supervisor. It is never silent, never auto-recovered, and never inferred from a timeout alone.

**L23. Depth is bought only by throughput.**
A low-labour task is a leaf and costs zero extra depth. A high-labour, throughput-bound task gets one manager owning a queue and N workers, which is exactly one level. Fan-out never reduces single-item latency; it increases it. So the pool is justified only by item rate, and the expected rate is a declaration. Corollary: no nesting of pools. Nesting puts consumers at cousin distance and interposes a manager that owns nothing relevant. Granularity is set at the worker.

**L24. The uncle path is the shape of consuming a pooled product.**
A worker two levels below the shared ancestor consuming the parallelised result of its parent's sibling is three hops, terminating at the pool manager, which answers from the aggregate it owns. Depth 3 is the maximum and it is exactly one level of fan-out. Cousin distance (four or more) does not indicate a routing problem; it indicates that the shared fact is owned one level too low. The repair is promotion to the shared ancestor, and promotion must be derivation rather than relocation: relocating an existing fact creates a second authority, while a derived fact has exactly one owner and carries its own invalidation obligation. Derivation permanently adds an owner, so it is a cost, not a free repair.

**L25. Move the worker, not the manager.**
A worker is short-lived and owns no state, so it can be re-parented freely. A manager owns the boundary and cannot be moved without a negotiation. When the placement condition is violated, the cheap repair is the worker, and moving a product between managers is reserved for the case where the product genuinely sits at the wrong level.

**L26. The latency tax is the sovereignty budget.**
The cost of a boundary is the price of non-interference, paid per hop, visibly, on the profiler. A boundary that costs more than the isolation it buys is a merge candidate. This makes componentisation economically self-correcting rather than stylistically policed, and it makes minimality an economic property rather than a guideline.

**L27. Resilience by respawn and replay.**
Because a leaf is a function of its declared inputs, a dead worker restarts cold. Because the manager owns the queue, in-flight work survives the worker's death and can be re-dispatched. The queue is the recovery log. No checkpointing is required, and recovery needs no protocol beyond respawn and replay. Corollary: the escalation path for an orphaned child (parent died mid-fan-out) is a declared property, and the adopting ancestor takes on the dead child's queue.

**L28. Topology is semantic; transport is mechanical.**
Because the channel is an entity, the same declared tree lowers to in-process rings, to hardware queues on a network-on-chip, or to sockets across machines, without the topology changing. Topology is invariant across deployment scale, and transport is a lowering decision.

**L29. Coarse-grained purity.**
Fine-grained purity is given up (a leaf mutates its own arena) in exchange for a component whose observable behaviour is fully determined by its declared inputs. This is the purity that a type system can enforce at this granularity, and the only kind that makes replacement and cold restart sound.

**L30. The axioms in silicon delete coherence and the memory model.**
A hardware architecture that enforces A2 has no shared address space and therefore no coherence protocol, no directories, no invalidation traffic, and no memory consistency model. The interconnect becomes point-to-point channels with buffering, which removes snoop traffic and scales where a bus does not. Fault containment is by construction. Historical instances of this architecture class (the transputer, the Cell BE local stores) were simpler and more robust than their coherent contemporaries and lost commercially because no compiler made the topology a first-class reviewable artifact, not because of the silicon. The safety-critical domain, where certification demands provable isolation, independently converged on these axioms (ARINC 653 partitioned domains with no shared memory). What hardware cannot remove is the cost of the transfer itself, and the one place the axiom is violated by physics is DMA and MMIO, where the correct response is to minimise the shared window, which is what an IOMMU and SR-IOV already are.

---

## The residue

Two things neither axiom settles. They are acknowledged costs, not derived decisions, and denying them would make the axiom claim brittle.

**The spin-versus-futex boundary.** A hardware tradeoff present in any design. It is the single place where the language's latency characteristics rest on kernel API semantics rather than on its own type system, which is why it is a tuning parameter and cannot be ruled on by the compiler.

**The universal wire format.** A chosen tax. A bespoke per-pair encoding would be cheaper per byte. Universality of interface is bought against O(n squared) protocol-pair coupling. Note that universality does not require runtime reflection, because the schema is declared once and known statically: the tax is a fixed-rate copy, not a negotiation or a discovery.

Also acknowledged, from A2: the expressiveness that is given up. No in-place mutation of a shared structure, so no lock-free algorithms, no zero-copy shared buffers, no DMA-style sharing, and no cross-component in-place numerics. The single escape is at the physical edge, where the hardware itself shares memory.

---

## Corollaries for tooling

**C1. The tree is recoverable syntactically.** Spawn is the only construct that creates a peer, so spawn sites are the tree edges and a depth-first scan recovers the instantiated tree with no analysis. The condition that preserves this is that the parent context is lexical and not first-class: spawn takes its parent by construction, and spawning authority is never a passable value. Cardinality on the edges is dynamic (a pool size is an expression); role is static.

**C2. Placement verification is decidable.** The expected tree is mechanical from products, consumers, lifetimes, and rates. The actual tree is mechanical from spawn sites. Comparing them is two cheap passes and a diff, which means the half of the discipline that cannot be inferred is declared and then verified. What remains undecidable is only whether the declared consumer set is complete, which is a declaration-completeness problem rather than a placement problem, with a different remedy.

**C3. The compiler refutes; it does not invent.** It can reject a placement that violates reach, lifetime, or the single-authority condition. It cannot supply intent.

**C4. The profiler produces two separable signals.** Semantic copy is the cost of isolation and cannot be removed by any transport, so it indicates the topology is wrong. Mechanical transfer is the cost of the lowering, so it indicates the transport is wrong. Being able to attribute a profile reading to one of the two is what makes the instrument load-bearing rather than decorative.

**C5. Errors are values at the border, terminated at the cause.** A failing boundary returns a value the other side can act on. The diagnosis stays in the context that produced it.

---

## Appendix: slogans mapped to lemmas

Slogans are useful only if each names something refutable. Mapped, so they can be checked rather than admired.

| Slogan | Lemma |
|---|---|
| Delete opinions. | the gate, section 1 |
| No heap, no hidden owner. | L19, L10 |
| Everything crossing a border is decoded. | L15 |
| A message, two threads, ever. | L12, L13 |
| No forwarding. A conduit is a bug. | L16 |
| Whoever decodes it, owns it. | L15, L17 |
| The tree is the requirement. | L18, L20 |
| Reach your uncle. | L4, L24 |
| Cousin distance means the fact sits one level too low. | L24 |
| Depth is bought by throughput. | L23 |
| Move the worker, not the manager. | L25 |
| You cannot move a fact, only give birth to one. | L24 |
| Declare, don't infer. | L7 |
| The compiler refutes placements; it does not invent them. | C3 |
| One declaration, one diff, one review. | L20 |
| Sovereignty is priced per hop. | L26 |
| The cost is conserved; only the location changes. | L26 |
| Backpressure is a buffer, not a block. | L12, L21 |
| Measure it or it is not yours. | C4 |
| The only free lunch is a leaf. | L23 |
