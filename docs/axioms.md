# Axioms of Moxie

Three axioms generate everything in this language that a compiler can check. Everything else in it is a derivative, not a decision. Nothing in Moxie is a preference, a convention, or a matter of taste, and the reason there is nothing to argue about is that the axioms are strong enough and few enough that the design closes. The five structural laws of the pentalogue are the larger set the axioms project from; the law a type system cannot enforce is named there, and it is residue, not licence.

The operational test that keeps this checkable: a proposed feature must name the axiom that forces it. If no axiom forces it, it is an opinion, and it is deleted. A feature forced only by a pentalogue law that is not an axiom is doctrine, not language, and it belongs in the deployment rather than in the type system.

---

## A1 - Just the facts

**Every property that governs execution is declared and statically checkable. Nothing that determines behaviour is inferred, negotiated, or discovered at runtime.**

The declared set is small and explicit:

- the topology (which component is the parent of which)
- for each product, its consumers
- for each product, its required lifetime
- for each pooled task, its expected rate
- every message type that crosses every edge

A1 is the declaration requirement. It says what must be written down. It does not by itself say that the written set is exhaustive; that is A3.

## A2 - Total isolation

**No component can address another component's state, and every fact has exactly one authoritative owner.**

Two clauses, and both are load-bearing:

- **mechanism:** no shared address space. Memory is per-domain. Nothing addressable is common between components except explicitly constructed transfer regions.
- **property:** single authority. No fact has two canonical holders.

The mechanism clause alone is insufficient. Pure isolation of access still permits two components to hold copies of the same fact and both treat them as canonical. That is replication, and replication is the contested-claim failure that the topology rules exist to eliminate. Isolation supplies the mechanism; single authority supplies the property that matters.

A2 is the memory requirement. It says what cannot be reached in memory. It does not by itself say that the world outside memory is closed off; that is A3.

## A3 - Closure

**Every capability and every input is declared, and the declaration is exhaustive: anything not declared is unreachable. The default is deny.**

Two clauses, and both are load-bearing:

- **capability:** a component can reach only what its interface names. No ambient filesystem, network, clock, environment, signal, or global registry.
- **input:** every perturbation is a declared message or a declared value. A clock read is an input; if it is not declared, it is not allowed.

The capability clause alone is insufficient. A closed set of handles still permits a component to read the wall clock or an environment variable, which are inputs that govern behaviour without appearing in the interface. The input clause alone is insufficient: a component can be handed a declared value that is a handle to an undeclared resource. Closure requires both the set of reachable things and the set of perturbations to be exhaustive.

A3 is the authority requirement. It says what cannot be reached even though it is not another component's memory. A2 removes the shared address space; A3 removes the ambient world.

---

## The pentalogue

Three axioms are what a compiler can enforce. Five structural laws govern coherent systems, and the axioms are the static projection of them. The axioms are not a reduction of the pentalogue; they are its enforceable image. The gap between the two is the point of this section, because one of the five cannot be checked by a type system, and a design that pretends otherwise is coherent on paper and incoherent in use.

| law | element | requirement | projection |
|---|---|---|---|
| sovereignty | earth | what you control is yours; what crosses the border is hostile until proven otherwise | A2, A3 |
| bilateralism | wood | no contract is signed by one hand; change both sides or change nothing | none |
| precision | metal | give the stranger a key, not the house | A3 |
| measurement | fire | weigh it, count it, time it; the crowd's opinion fits no scale | A1 |
| resolution | water | what two claim to own, no man owns; the first to act on the lie destroys it for both | A2 |

The sheng and ke relations are consistency conditions between the laws, not additional laws. Sheng is the productive direction, and it runs wood to fire to earth to metal to water and back: bilateral change fuels measurement, measurement establishes sovereignty, sovereignty refines into precise access, precise access resolves ownership, resolution enables new bilateral change. Ke is the destructive direction, and each edge names a way a coherent system dies: bilateral demands without boundaries disrupt sovereignty, territorial hoarding blocks resolution, ownership ambiguity obscures measurement, raw truth without restraint destroys refined interfaces, restricted access kills bilateral growth.

The ke cycle is therefore the review checklist for everything the compiler cannot see. A design that hoards what it cannot hold, restricts access until nothing can pass, leaves ownership ambiguous, negotiates without standing, or exposes everything without granting anything has violated one of the five, and no static check will catch it, because none of those failures is a property of the code.

The repo's change discipline is the wood law applied to time: legacy first, stage4 second, because a change to one side alone is a contract signed by one hand.

The enforceable claim is narrower than "three axioms generate everything": the axioms generate everything the compiler can check. Bilateralism is the law the language serves and cannot enforce, because it is a property of how peers behave across a boundary, not of the boundary's type. That is the pentalogue's residue, and it is listed with the others.

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

**Effects become enumerable, once A3 closes the ambient world.** A component cannot be perturbed by anything except its declared inputs, so the effects it has are exactly the messages it emits. Nothing else is reachable. A2 supplies the private arena; A3 supplies the closed input set. Without A3, the component can still be perturbed by a file, a clock, or a signal, and the enumeration is false.

**Purity at the useful granularity.** A component's observable behaviour is a function of its declared inputs and its own arena. A2 supplies the arena; A3 supplies the closure of the inputs. That is the kind of purity a type system can enforce, and the kind that makes a component replaceable, testable in isolation, and restartable cold.

### Why A3

Undeclared authority has a bill, and the bill is paid in exactly the failure modes of ambient state:

- hidden inputs, because a clock, an environment variable, a file, a signal, or a random source changes behaviour without appearing in the interface
- hidden effects, because a log line, a cache write, a temporary file, or a metric changes the world without being a declared message
- hidden coupling, because a global name, a service registry, or a search path lets a component depend on a deployment it never declared
- hidden privilege, because an inherited descriptor, a credential, or a process-wide configuration grants authority the interface never named

The absence of A3 admits only two treatments: a runtime sandbox that intercepts every ambient access, or an audit that reconstructs the ambient set after the fact. The sandbox makes the observer a co-owner of every capability, which violates A2. The audit is not a static check and cannot bound behaviour, because the set of ambient facts is open. There is no free repair. So closure is the only path, and once declared, the capability check is decidable: calls are syntactic, names resolve at the boundary, and comparing the declared set with the actual set is a syntax pass.

The handler contract is the proof. The shell owns the socket, the framing, the accept loop, and the deadlines; the handler does no I/O and keeps no state between requests. That is A3 in practice. Naming it makes the purity structural rather than stylistic: the handler is closed because the shell is the only component with ambient authority.

The cost is expressiveness. No ambient clock, no ambient I/O, no ambient identity, no ambient randomness, no ambient configuration. The world is behind the shell. That is the price of enumerable effects, and it is paid at the boundary.

### Why exactly three

- **Independent.** A1 does not imply A2: a fully declared shared-memory language is possible. A1 does not imply A3: a declared topology with an ambient clock is possible. A2 does not imply A1: an isolated system with runtime negotiation is possible. A2 does not imply A3: per-domain memory with a global file is possible. A3 does not imply A1: a closed capability set with undeclared topology is possible. A3 does not imply A2: a closed capability set over shared memory is possible.
- **Auditable.** Three axioms still fit in a head, which means they can be checked for consistency and redundancy. Every other language also has an axiom set; it is unwritten, larger, partly contradictory, and owned by nobody. Anything unauditable survives by default as an opinion, because you cannot delete what has never been stated.
- **Absorbing.** Three strong axioms with a triple intersection close the design. A1 closes inference, A2 closes sharing, A3 closes ambient authority. What looks like freedom in other languages is the gap an axiom failed to close. The residue is what physics and the wire format refuse to close. The pentalogue's wood law sits outside the intersection by construction, which is why it appears in the residue rather than in the lemmas.

---

## The octalogue

Eight operational principles govern use of the system against a centralised adversary. They are not language semantics; they are the doctrine the semantics are built to survive. Some are forced by the axioms, and some are deployment discipline the axioms merely permit.

| principle | trigram | design consequence | status |
|---|---|---|---|
| flow around the centre | Kan, water | no runtime arbiter; make the centre irrelevant rather than capture it | forced by A2 |
| withdrawal as sovereignty | Gen, mountain | every capability is revocable by refusal; default deny is the exit | forced by A3 |
| invisible penetration | Xun, wind | do not announce until it works | doctrine |
| sovereign nodes | Qian, heaven | every node runs cut off; no coordination is required to survive | forced by L27, L28 |
| speed of small exchange | Zhen, thunder | binary sessions, small messages, no third-party gate | forced by L6, L23 |
| new ground | Kun, earth | build where the adversary has no claim | doctrine |
| bounded exchange | Dui, lake | every interface has a shore and a walk-away position | forced by L8, L26 |
| sovereign measurement | Li, fire | the operator defines the metric; the adversary's statistics are not evidence | forced by A1, C4 |

The two that are not enforceable are not optional. They are the parts of the doctrine that live in deployment and in the operator's discipline, and the language's contribution is to make the enforceable six cheap and the unenforceable two visible. A system that announces before it works, or that fights for ground the adversary already holds, has not violated a type rule; it has violated the doctrine, and no compiler will say so.

The anti-centralisation claim is structural rather than rhetorical. Centralisation is expensive here for three reasons the axioms supply. The topology declaration is a canon, not an authority (L20), so the one global artifact grants no runtime power. Every hop is priced (L26), so a centre that everything must cross pays the sovereignty budget on every message. A fact that sits too low is promoted rather than replicated (L24), so the repair for coupling is derivation, not a new hub. What the language cannot do is stop an operator from building a star. It can only make the star cost more than the mesh, which is the same thing the pentalogue says about hoarding.

---

## Lemmas

Derivations are marked with the axiom they come from. Some are forced by A1 alone (checkability costs), some by A2 alone (isolation costs), some by A3 alone (closure costs), some by their intersections.

The closure lemmas are numbered from L31, so that every reference already in the tree remains valid. L29 and L30 keep their original numbers but now derive from the triple intersection. The derivation label, not the number, is the authority.

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
A channel inside a message is a dynamically created role, which is not checkable in the local fragment. A channel capability is non-delegable by construction. Corollary: new peers are created by the parent, never by a peer. A delegated channel would be a contract signed by one hand, so bilateralism is preserved by the tree rather than by the channel.

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

**L20. The topology declaration is the only global artifact, and it is a canon, not an authority.**
A1 needs exactly one thing to check, and A2 guarantees nothing else is global. It therefore plays three roles at once: the intent record, the compiler input, and the review surface. It should diff as a shape rather than as control flow, because it is the only place a non-local bug can live. It is global because it is the measurement of the whole (fire), not because it holds power (earth). It grants nothing at runtime; the moment it becomes a runtime lookup it has become a service locator, which A3 forbids.

**L21. Signal is the operation.**
The index word is simultaneously the state and the notification. Advancing the write index is both the data-availability state and the wake for a parked reader; advancing the read index is both the space-availability state and the wake for a parked writer. There is no separate control message and no notification ordering question, because there is only one write. The parking primitive is a futex on that word, and the expected-value check in FUTEX_WAIT is what makes the check-then-sleep sequence atomic in the kernel.

**L22. Termination is reported, not hidden.**
A worker's death is an event delivered to its supervisor. It is never silent, never auto-recovered, and never inferred from a timeout alone.

**L23. Depth is bought only by throughput.**
A low-labour task is a leaf and costs zero extra depth. A high-labour, throughput-bound task gets one manager owning a queue and N workers, which is exactly one level. Fan-out never reduces single-item latency; it increases it. So the pool is justified only by item rate, and the expected rate is a declaration. Corollary: no nesting of pools. Nesting puts consumers at cousin distance and interposes a manager that owns nothing relevant. Granularity is set at the worker. The pool manager is a declared sovereign, not an ambient one: it owns the queue because the declaration says so, and its authority is bounded by the queue's capacity and the worker contract.

**L24. The uncle path is the shape of consuming a pooled product.**
A worker two levels below the shared ancestor consuming the parallelised result of its parent's sibling is three hops, terminating at the pool manager, which answers from the aggregate it owns. Depth 3 is the maximum and it is exactly one level of fan-out. Cousin distance (four or more) does not indicate a routing problem; it indicates that the shared fact is owned one level too low. The repair is promotion to the shared ancestor, and promotion must be derivation rather than relocation: relocating an existing fact creates a second authority, while a derived fact has exactly one owner and carries its own invalidation obligation. Derivation permanently adds an owner, so it is a cost, not a free repair.

**L25. Move the worker, not the manager.**
A worker is short-lived and owns no state, so it can be re-parented freely. A manager owns the boundary and cannot be moved without a negotiation. When the placement condition is violated, the cheap repair is the worker, and moving a product between managers is reserved for the case where the product genuinely sits at the wrong level.

**L26. The latency tax is the sovereignty budget.**
The cost of a boundary is the price of non-interference, paid per hop, visibly, on the profiler. A boundary that costs more than the isolation it buys is a merge candidate. This makes componentisation economically self-correcting rather than stylistically policed, and it makes minimality an economic property rather than a guideline.

**L27. Resilience by respawn and replay.**
Because a leaf is a function of its declared inputs, a dead worker restarts cold. Because the manager owns the queue, in-flight work survives the worker's death and can be re-dispatched. The queue is the recovery log. No checkpointing is required, and recovery needs no protocol beyond respawn and replay. Corollary: the escalation path for an orphaned child (parent died mid-fan-out) is a declared property, and the adopting ancestor takes on the dead child's queue. The manager's queue is the recovery log, and the manager is the declared owner of that log. Respawn is not a central service; it is the manager exercising the ownership the declaration gave it.

**L28. Topology is semantic; transport is mechanical.**
Because the channel is an entity, the same declared tree lowers to in-process rings, to hardware queues on a network-on-chip, or to sockets across machines, without the topology changing. Topology is invariant across deployment scale, and transport is a lowering decision.

### From A3 alone

**L31. No ambient input.**
Every perturbation is a declared message or a declared value. A clock read, a file change, a signal, an environment variable, a random source, or a locale is an input; if it is not in the interface, it is unreachable. Corollary: time, entropy, identity, locale, and configuration are capabilities, not globals. The runtime may provide a clock service, but the service is a declared input, not an ambient now().

**L32. No ambient capability.**
A component can reach only what its interface names. File descriptors, handles, registry entries, service names, and network addresses are granted, not inherited. Corollary: no getenv, no global registry, no service locator, no implicit DNS, no magic paths. Authority is a parameter, not a context.

**L33. The interface is a closed set.**
The declaration is exhaustive; the default is deny. A behaviour that cannot be attributed to a declared input is a bug in the declaration, not a runtime surprise. Corollary: the reviewer checks the absence, not just the presence. A component that passes every positive test can still violate A3 by reading something it never declared.

**L34. Authority is a parameter, not a context.**
A capability is passed explicitly across the boundary. It is not read from ambient state. Corollary: no ambient identity, no inherited credentials, no process-wide configuration, no getuid, no getcwd. The shell supplies identity and context as declared values.

**L35. Effects are the declared messages.**
A component's observable effects are exactly the messages it emits. A file write, a network call, a clock adjustment, a signal, or a log line is an effect and must be a capability. Corollary: no hidden side channels through the filesystem, network, environment, or process table.

**L36. Failure is an input.**
A crash, signal, timeout, or disconnect of another component reaches this one only as a declared message. Corollary: no ambient failure propagation. The supervisor relationship is declared, and a death is reported to the supervisor rather than observed through a global process table. A2 supplies containment; A3 supplies the input status of the failure.

**L37. Policy is declared, not ambient.**
Retries, timeouts, backoff, deadlines, and rate limits are declared policy, not hidden defaults. Corollary: the scheduler's clock is not visible to user code except through declared deadlines. A timeout that is not in the interface is an undeclared input.

**L38. Names resolve at the boundary.**
A component cannot resolve an undeclared name. Naming is a capability. Corollary: no ambient DNS, no global service registry, no magic paths, no implicit search path. The shell resolves names and passes the result as a declared capability.

**L39. Ambient authority is confined to the shell.**
The component that owns the OS interface is the one place where ambient authority is allowed, because physics puts the syscall boundary there. It exposes the world as declared capabilities; every other component is closed. Corollary: the handler path is pure because the shell is the only component with authority in that path. The syscall surface of the shell is the trust boundary, and its audit is the list of syscalls it can make.

### From A1, A2 and A3 together

**L29. Coarse-grained purity.**
Fine-grained purity is given up (a leaf mutates its own arena) in exchange for a component whose observable behaviour is fully determined by its declared inputs. A2 supplies the private arena; A3 supplies the closure of the declared inputs; A1 supplies the static check. This is the purity that a type system can enforce at this granularity, and the only kind that makes replacement and cold restart sound.

**L30. The axioms in silicon delete coherence and the memory model.**
A hardware architecture that enforces A2 has no shared address space and therefore no coherence protocol, no directories, no invalidation traffic, and no memory consistency model. The interconnect becomes point-to-point channels with buffering, which removes snoop traffic and scales where a bus does not. Fault containment is by construction. Historical instances of this architecture class (the transputer, the Cell BE local stores) were simpler and more robust than their coherent contemporaries and lost commercially because no compiler made the topology a first-class reviewable artifact, not because of the silicon. The safety-critical domain, where certification demands provable isolation, independently converged on these axioms (ARINC 653 partitioned domains with no shared memory). A3 extends the same deletion to ambient authority: a component with no ambient input cannot be perturbed by a device it did not declare. What hardware cannot remove is the cost of the transfer itself, and the one place the axiom is violated by physics is DMA and MMIO, where the correct response is to minimise the shared window, which is what an IOMMU and SR-IOV already are. The shell is the software analogue of that window. The pentalogue states the same deletion socially: sovereignty is what remains when nothing is shared, and resolution is what remains when no fact has two owners.

**L40. The capability graph is the topology.**
A1 declares the topology; A3 declares the capabilities. The two graphs must coincide. A capability that crosses a boundary the topology does not declare is an undeclared edge; a declared edge with no capability is a dead channel. Corollary: capability placement is checkable by the same syntactic diff as topology placement, and the review surface is the pair of declarations.

**L41. The boundary transfers capabilities, not just values.**
A message crossing a boundary carries values and, where declared, capabilities. The decode is the capability transfer: a handle that survives the decode is a declared capability, and a handle that does not decode is not a capability. Corollary: no ambient handle can be smuggled through a message, because the decode is the only way through.

**L42. A capability cannot be amplified.**
A component cannot acquire a capability it was not given. It can pass a capability it holds only where the interface declares the delegation. Corollary: no ambient privilege escalation, no forging a handle, no reading a descriptor it was not granted. A2's single authority supplies the ownership; A3's closure supplies the grant.

**L43. The closed interface is testable by absence.**
The compiler can compare the declared capability set with the actual calls, and the runtime can audit that no ambient handle is open and no undeclared clock or file access occurred. Corollary: the A3 instrument is a capability audit, not a memory report. The negative test is the one that matters: nothing undeclared was reached.

**L44. The world is a capability, not a context.**
The external world (files, sockets, clocks, devices, identity) enters the system only as a declared capability at the shell. Corollary: the same program lowers to a different world by changing the shell's capability set, not the component's code. This is the A3 analogue of L28: topology is semantic, transport is mechanical; capability is semantic, world is mechanical.

**L45. Ordering is the channel's, not the clock's.**
A component cannot observe a global clock to order events across boundaries. The only cross-boundary order is the order of declared messages. Corollary: no wall-clock timestamps in user code for ordering; a timestamp is an input, and the channel's sequence is the authority. This eliminates the clock-skew and time-of-check-to-time-of-use classes at the boundary.

**L46. Randomness is an input.**
A random source is a declared capability, not an ambient rand(). Corollary: a component's behaviour is reproducible from its declared inputs, including the entropy stream. This is what makes replay and cold restart sound. A1's static check supplies the declaration; A3's closure supplies the fact that no other entropy is reachable.

**L47. Configuration is an input, not an environment.**
Deployment configuration is a declared value, not an environment variable, a global file, or a command-line default. Corollary: the same binary runs in any deployment by changing the input, not the environment. A1's declaration supplies the shape; A3's closure supplies the fact that no other configuration is reachable.

**L48. The pentalogue is the closure condition the compiler cannot check.**
A design that satisfies A1, A2 and A3 can still violate a ke relation, and when it does it is coherent in the type system and incoherent as a system. Corollary: the ke cycle is the review checklist for what the compiler cannot see, and a violation is a design defect even when every test passes.

**L49. The octalogue is doctrine, not semantics.**
Six of its eight principles are forced by the axioms; two are deployment discipline. Corollary: the language can make centralisation expensive, not impossible. The residue is the operator's discipline, and no type system supplies it.

---

## The residue

Four things none of the axioms settles. They are acknowledged costs, not derived decisions, and denying them would make the axiom claim brittle.

**The spin-versus-futex boundary.** A hardware tradeoff present in any design. It is the single place where the language's latency characteristics rest on kernel API semantics rather than on its own type system, which is why it is a tuning parameter and cannot be ruled on by the compiler.

**The universal wire format.** A chosen tax. A bespoke per-pair encoding would be cheaper per byte. Universality of interface is bought against O(n squared) protocol-pair coupling. Note that universality does not require runtime reflection, because the schema is declared once and known statically: the tax is a fixed-rate copy, not a negotiation or a discovery.

**The syscall boundary.** A3 closes the component, not the machine. The shell must call the OS, and the OS is ambient by physics. The correct response is to minimise the window and audit the list, the same way L30 treats DMA and MMIO. This is a residue, not a violation: the axiom applies to components, and the shell is the designated exception.

**Bilateralism.** The language cannot enforce that both sides of a contract agree, only that both sides are declared. A peer can sign and then refuse, and the refusal is a declared message, not a type error. The pentalogue's wood law is therefore a residue: the tree supplies the two hands, but it cannot supply good faith. The repair is not a type; it is the operator's choice of peers, which is why the doctrine in the octalogue exists at all.

Also acknowledged, from A2: the expressiveness that is given up. No in-place mutation of a shared structure, so no lock-free algorithms, no zero-copy shared buffers, no DMA-style sharing, and no cross-component in-place numerics. The single escape is at the physical edge, where the hardware itself shares memory. From A3: the ambient world is given up. No ambient clock, no ambient I/O, no ambient identity, no ambient randomness, no ambient configuration. The single escape is the shell, which owns the world and exposes it as declared capabilities.

---

## Corollaries for tooling

**C1. The tree is recoverable syntactically.** Spawn is the only construct that creates a peer, so spawn sites are the tree edges and a depth-first scan recovers the instantiated tree with no analysis. The condition that preserves this is that the parent context is lexical and not first-class: spawn takes its parent by construction, and spawning authority is never a passable value. Cardinality on the edges is dynamic (a pool size is an expression); role is static.

**C2. Placement verification is decidable.** The expected tree is mechanical from products, consumers, lifetimes, and rates. The actual tree is mechanical from spawn sites. Comparing them is two cheap passes and a diff, which means the half of the discipline that cannot be inferred is declared and then verified. What remains undecidable is only whether the declared consumer set is complete, which is a declaration-completeness problem rather than a placement problem, with a different remedy.

**C3. The compiler refutes; it does not invent.** It can reject a placement that violates reach, lifetime, or the single-authority condition. It cannot supply intent.

**C4. The profiler produces two separable signals.** Semantic copy is the cost of isolation and cannot be removed by any transport, so it indicates the topology is wrong. Mechanical transfer is the cost of the lowering, so it indicates the transport is wrong. Being able to attribute a profile reading to one of the two is what makes the instrument load-bearing rather than decorative.

**C5. Errors are values at the border, terminated at the cause.** A failing boundary returns a value the other side can act on. The diagnosis stays in the context that produced it.

**C6. The capability set is checkable syntactically.** A3 makes calls and names syntactic. The compiler can compare the declared set with the actual set, and an undeclared ambient call is a compile error. The review surface is the topology declaration plus the capability declaration; both diff as shapes.

**C7. The absence is testable.** The runtime audit asserts that no ambient handle is open, no undeclared clock or file access occurred, and no capability was amplified. The A3 instrument is a capability audit, not a memory report. The negative test is the one that matters.

**C8. Ambient authority is audited at the shell.** Its syscall surface is the trust boundary; the audit is the list of syscalls it can make. Everything below the shell is closed.

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
| If it is not declared, it is not reachable. | A3 |
| The interface is a closed set. | L33, L43 |
| Authority is a parameter, not a context. | L34 |
| Time is an input. | L31, L45 |
| The shell owns the world. | L39, L44 |
| A capability is granted, not inherited. | L32, L42 |
| Effects are messages, or they are bugs. | L35 |
| Names resolve at the boundary. | L38 |
| Failure is an input. | L36 |
| Policy and configuration are inputs. | L37, L47 |
| Randomness is an input. | L46 |
| The capability graph is the topology. | L40 |
| What you control is yours. | A2, L10 |
| Give the stranger a key, not the house. | A3, L15 |
| What two claim, no one owns. | L17 |
| No contract is signed by one hand. | L5, L6 |
| Weigh it, count it, time it. | A1, C4 |
| Flow around the centre. | A2, L28 |
| Every node runs cut off. | L27 |
| The lake has shores. | L8, L26 |
