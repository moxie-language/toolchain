# Security properties of Moxie

Nothing in this language was added for security. The security is a consequence, and this document exists to state the consequence precisely enough that it can be attacked.

The thesis is one sentence. Reliability and security are the same property seen from two angles: both are about bounding what can influence what. The axioms bound influence. An adversary is an influence source you did not choose. So the isolation that makes a system testable is the isolation that makes it hard to attack, and a language that makes effects enumerable makes them auditable for the same reason.

That is what "without even trying" means, and it is also the limit of the claim. The axioms were chosen for parallel correctness. They happen to close a large set of vulnerability classes, and they close them structurally rather than by convention. They do not close all of them, and the residue section at the end names what is left.

---

## What is claimed

Claimed:

- component state cannot be read or written by another component except through declared transfer regions
- the set of inputs a component can be perturbed by is finite and written down
- the set of messages a component can receive is finite and written down
- the error channel and the value channel are distinct types, so a failure sentinel cannot collide with a valid value
- failure is fail-closed: the runtime panics rather than returning a value that might be mistaken for success
- the attack surface of a component is enumerable by reading its declaration, not by reading its code

Not claimed:

- availability under attack. Fail-closed trades availability for integrity, and an attacker can convert a bypass into an outage. That trade is correct for authentication and wrong for a pacemaker, and this document claims the former.
- correctness of application logic. The type system does not know what "authenticated" means.
- compiler trust. A compiler bug breaks every guarantee at once.
- safety inside `unsafe`.
- elimination of physical-layer interference. Rowhammer, controller contention and thermal effects are bounded by a declared partition map, not removed.

---

## Classes and their status

| class | mechanism | removed by | status |
|---|---|---|---|
| error/value sentinel confusion | failure returns a value of the same type as success | typed `error` channel | eliminated |
| silent error propagation | caller ignores the error | none | residual, convention |
| nil dereference as fail-open | null treated as a valid value | runtime panic | eliminated, fail-closed |
| out-of-bounds access | index past the end | runtime bounds check | eliminated, fail-closed |
| cross-domain memory corruption | wild pointer reaches another component | A2 arenas | contained |
| use-after-free | pointer outlives its allocation | arena lifetime checking | transformed |
| data race | two writers to one location | A2, no shared state | eliminated |
| ambient authority | undeclared file, clock, env, signal | A3 closure | eliminated |
| hidden input | behaviour changes without an interface change | A3 input clause | eliminated |
| confused deputy | component acts on authority it was handed | A3 capability clause | reduced |
| attack surface discovery | unknown reachable code | A1 declaration | enumerable |
| cross-domain cache timing | shared cache line observed | A2 separate scratchpads | eliminated across domains |
| speculative execution (Spectre class) | mistrained speculation reads shared state | in-order core, no speculation | eliminated by architecture |
| integer overflow | arithmetic wraps | none | residual |
| DRAM disturbance (Rowhammer class) | adjacent row flips | partition map, A1 extended | bounded by partition |
| memory controller contention | shared controller | partition map | bounded by partition |
| mesh traffic analysis | shared links | partition map | bounded by partition |
| resource exhaustion | unbounded work | A1 declared rates | bounded, not eliminated |
| supply chain | build or dependency compromise | none | residual |
| application logic | the auth check is simply wrong | none | residual |

---

## Error and value confusion

This is the strongest single claim, and it is worth stating in full because it is the class that most recently produced a decade-old authentication bypass.

In C, a function that can fail returns a pointer, and failure is signalled by a distinguished pointer value. Success and failure therefore live in the same type. `crypt()` returns `char *` for both, so a failure sentinel is a value that a caller may compare against a stored value that is also a sentinel. When musl changed a failed `crypt()` to return an unmatchable hash rather than `NULL`, it made the failure value a string. When OpenSSH borrowed a salt from another account for a `*` password field, it made the stored value the same string. The two decisions were ten years apart and each was correct alone. Together they were an authentication bypass, because both sides honoured their own contract and the contracts collided.

Moxie separates the channels by type. `type error interface` is declared in `src/builtin/builtin.mx`, and a fallible function returns `(T, error)`. A failure cannot be expressed as a value of `T`. The collision is not a logic error to be caught in review, it is a type error the compiler rejects.

The residual is real and must be stated: a single-return function can still return a sentinel, and a caller can still write `v, _ := f()`. The type system removes the collision, not the carelessness. Rust is stronger here, because `Result` is a sum type and the compiler forces handling. Moxie relies on convention for the handling and on types for the collision.

## Fail-closed

A nil dereference panics at `src/runtime/panic.mx`. An out-of-range index panics. There is no silent sentinel return in the runtime.

This matters because of the direction of the failure. The 2012 musl change moved from fail-closed to fail-open, explicitly because programs were not checking for `NULL`. A crash is a denial of service. A bypass is a compromise. Moxie chooses the crash everywhere, and the choice is not a policy that can be forgotten at a call site, it is the only behaviour the runtime has.

The cost is availability, and it is a real cost. A component that panics is a component that is down. The architecture's answer is that a component is restartable cold, because A2 gives it a private arena and A3 gives it a closed input set, so restart is a valid recovery. That is a design claim about the deployment, not a language guarantee.

## Memory containment

A2 gives each component a private arena and no shared address space. A pointer cannot leave its arena, so corruption is bounded to the component that caused it rather than reaching another component's data. There is no capability check on every access, no software sandbox, and no reference counting to get wrong.

Two honest qualifications.

First, containment is not immunity. Within an arena, a bug can still corrupt that component's own state. The guarantee is about blast radius, not about correctness.

Second, use-after-free is transformed rather than eliminated. There is no per-object free, so the classic double-free and free-then-use bugs do not exist in that form. What replaces them is arena lifetime escape: a pointer that outlives the arena it points into. That is the same hazard wearing different clothes, and the compiler's job is to reject it. The sovereign arena work in the stage4 compiler exists precisely because that check is load-bearing and was, at one point, wrong.

And `src/unsafe` exists. Anything that goes through it voids every claim in this document. That is not a defect, it is the escape hatch, and it should be treated as one.

## Ambient authority

A3 requires every capability and every input to be declared, with default deny. No ambient filesystem, network, clock, environment, signal, or global registry. A clock read is an input; if it is not declared, it is not allowed.

This eliminates the hidden-input class entirely. It is the class behind most privilege escalation through configuration, most reproducibility failures, and most "works on my machine" incidents. A component cannot be perturbed by anything except its declared inputs, so the effects it has are exactly the messages it emits.

The residual is that the declaration can be wrong. A3 says the declared set is exhaustive; it does not say the declaration is honest. A component that declares filesystem access and then reads a credential has declared the capability. Closure bounds what can happen, not what should.

## Enumerable attack surface

A1 requires the topology, the consumers of each product, the required lifetime, the expected rate, and every message type crossing every edge to be declared and statically checkable.

The security consequence is that the set of things a component can receive is finite and written down. Audit becomes a matter of reading a declaration rather than reconstructing behaviour. This is the property that makes the rest of the document checkable rather than aspirational: every claim above is a claim about a declared set, and a declared set can be compared against the actual set by a syntax pass.

## The von Neumann simulation

There is a class of vulnerability that exists only because hardware lies about its own structure, and it is worth naming because the architecture removes it rather than mitigating it.

The programming model presents memory as a flat array with uniform access. The hardware does not work that way: it has caches, store buffers, prefetchers, and speculative execution. Every one of those is state shared below the level of the abstraction, and every one of them is observable by an attacker who shares the machine. Flush and reload, prime and probe, and the entire Spectre family are not bugs in a program. They are the cost of simulating a machine that does not exist.

Cache coherence is the most expensive part of that simulation. It exists to maintain the fiction that all cores see one memory, and it does so with directories, invalidation traffic, and a memory consistency model that programmers are then expected to reason about correctly. It is a large protocol, it is a large attack surface, and it is there to support a model that A2 does not use.

Chairman Cache Coherence presides over a committee of cores that must all agree on the contents of a shared memory none of them owns. Moxie declines the meeting. There is no shared memory to agree about, so there is no protocol, no invalidation traffic, no consistency model, and no shared cache line for a timing attack to observe.

The general form of the argument: a security vulnerability baked into hardware is one where the abstraction hides state that the attacker can reach. A2 removes the shared state, so the hidden state has nothing to hide behind. An in-order core with no speculation removes the speculative state, so the Spectre class has no mechanism. Neither of these is a mitigation. Both are the absence of the thing that needed mitigating.

The residual is the memory controllers and the mesh, which are still shared. Rowhammer is a DRAM-level disturbance and A2 does not touch it.

## Physical partitions

A2 gives logical isolation. It does not give physical isolation, and the difference is where the remaining attacks live.

Rowhammer is the clearest case. DRAM cells leak charge into physically adjacent rows when a row is activated repeatedly. The attack does not go through the address space, it goes through the silicon. Two components with separate arenas, in different address ranges, with no pointer between them, can still interfere if their pages land in adjacent rows of the same device. Logical isolation is not insufficient here, it is irrelevant, because the mechanism never touches the abstraction A2 controls.

The same shape appears at every layer that aggregates a resource. Memory controller contention: one tenant saturating the controller delays another's accesses, which is both a timing channel and a denial of service. Row buffer conflicts: the same mechanism at finer grain. Mesh traffic: a tenant's message volume is observable in link timing. Power and thermal: sustained load by one tenant changes another's clock behaviour.

None of these are addressable by the type system, because none are properties of the code. They are properties of the deployment, which is the category the pentalogue calls residue. But that does not make them out of scope, because a language that claims isolation is making a claim about a partition, and a partition has to be declared. A1 already requires the topology to be declared. The same requirement has to extend down to the physical resources, or the isolation stops at the layer where the attack starts.

So the missing artifact is a partition map: which physical resources belong to which domain, declared and checkable. Memory by device or bank group, mesh bandwidth by link, and where possible power and thermal domains. The compiler can check that two domains' declared partitions do not overlap. It cannot check that the hardware honours the map, but it can refuse a deployment whose map is inconsistent, which is the same guarantee A1 gives everywhere else: the declaration is exhaustive and the comparison is a syntax pass.

For this architecture the map is cheap, because the topology already draws the boundaries. Scratchpad is on-die SRAM, which has no rowhammer mechanism, so per-core scratchpad is immune by construction. The DRAM lives in the external memory units, one per edge. So the natural partition is per memory unit, and the design decision is whether two tenants may share one. If they may not, rowhammer and controller contention are bounded to a single tenant and the attack becomes self-harm. If they may, the map should say so, because the guarantee is weaker and a reader is entitled to know which one is being claimed.

The residual: partitioning bounds the blast radius, it does not remove the mechanism. Within one tenant's partition rowhammer still works, and ECC reduces the success rate without eliminating it. The claim is tenant-against-tenant isolation, not device integrity.

## Comparison

C. No memory safety, no isolation, no declared authority, no error channel distinct from the value channel. Every class in the table is open. The musl and OpenSSH bug is a C bug in the precise sense that the type system permitted it.

Go. Memory safe, garbage collected, channels and goroutines. Closes memory corruption and, with the race detector, most data races at test time rather than compile time. Does not close ambient authority: `os.Getenv`, `time.Now`, and the filesystem are all reachable without declaration. Does not close shared state: a pointer passed to a goroutine is shared mutable state, and the language permits it. Error handling is `(T, error)` with the same ignore-the-error residual as moxie, and the same sentinel-collision protection, since `error` is a distinct type.

Rust. Closes memory unsafety and data races in the safe subset, and does it statically, with no collector and no borrow-check runtime cost, though bounds checks remain. This is a genuine and large achievement, and any comparison that pretends otherwise is not worth reading. What Rust does not close: ambient authority, since `std::env`, `std::fs`, and `SystemTime` are freely reachable; shared state, since `Arc<Mutex<T>>` is shared mutable state with a lock rather than an absence of sharing; topology, since there is no declaration of who may talk to whom; attack surface enumeration, since reachability is a property of the code rather than of a declaration; and cache side channels, since the hardware is unchanged. Rust is also stronger than moxie on error handling, because `Result` forces handling where `(T, error)` does not.

So the honest comparison is not "moxie closes more holes than Rust". It is that the two close different sets. Rust closes the memory-safety set with a sophisticated type system and no runtime cost. Moxie closes the memory-safety set plus the authority and sharing sets, with a much smaller conceptual surface and a runtime that panics instead of a borrow checker that reasons.

The conceptual surface is the adoption argument. Rust asks the programmer to hold ownership, borrowing, lifetimes, interior mutability, `Send` and `Sync`, and trait bounds simultaneously. Moxie asks for two things: memory comes from an arena with a declared lifetime, and components communicate by message. That is a smaller thing to learn, and it is a smaller thing to get wrong.

The other adoption argument is the cost model. Moxie forces the programmer to reason about how the machine actually runs: an arena is a real allocation scope, a message is a real transfer, there is no collector to hide the pause and no cache to hide the latency. That is more work per line and less work per incident. A programmer who knows where the bytes are does not write the bug where the bytes are not.

## The residue

The parts of the doctrine that no type system enforces, listed so that the document does not overstate itself.

Bilateralism. No contract is signed by one hand. This is a property of how peers behave across a boundary, not of the boundary's type, and it is the pentalogue's named residue. A component can honour its declared interface and still be a bad peer.

Integer overflow. Not checked. Arithmetic wraps.

DRAM disturbance. Rowhammer and its descendants operate below the isolation boundary.

Resource exhaustion. A1 declares expected rates, which bounds the buffers, but a declared rate is a promise and an attacker does not make promises.

Compiler trust. The compiler is the thing that enforces every claim here. A compiler bug is a universal bypass, and the only defence is that the compiler is small, is tested, and is bootstrapped from a chain that can be audited.

The declaration's honesty. A3 makes the declared set exhaustive. It does not make it true.

Application logic. The type system does not know what a password is.

## Corollaries for tooling

A security argument that cannot be checked is a marketing document. These are the checks that follow from the claims above, and each one is a syntax pass over a declaration rather than an analysis of behaviour.

- compare the declared input set against the actual input set, per component
- compare the declared message types per edge against the types actually sent
- reject a pointer whose arena lifetime exceeds the arena it points into
- reject an undeclared ambient access: clock, environment, filesystem, signal
- enumerate the reachable set from a component and compare it against its declaration
- flag every `(T, error)` whose error is discarded
- flag every single-return function that returns a distinguished sentinel

The last two are the ones that would have caught the Alpine bug in a codebase that had moxie's types but not moxie's discipline.

## Appendix: slogans

| slogan | claim |
|---|---|
| no shared memory, no shared fate | A2 bounds blast radius |
| the error is not a value | typed error channel removes sentinel collision |
| crash, do not guess | fail-closed is the only runtime behaviour |
| if it is not declared, it is not reachable | A3 closure |
| the attack surface is a declaration | A1 makes audit a syntax pass |
| the machine does not lie about its cost | arenas and messages are the real thing |
| Chairman Cache Coherence is not invited | no shared state, no coherence protocol, no shared cache line |
| isolation is a claim about a partition | A1 extends below the address space to the physical map |
