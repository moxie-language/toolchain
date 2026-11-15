# Moxie documentation

Moxie is a compiled systems language: a tree of domains, each a thread with its
own arena stack and event loop, connected only by declared channels. No heap, no
garbage collector, no goroutines, no shared mutable state, no ambient authority.

This folder is the whole documentation set. If a document is not in it, it is not
a rule.

## Read in this order

| # | document | what it settles |
|---|---|---|
| 1 | [axioms.md](axioms.md) | the three axioms, the pentalogue and octalogue, the 49 lemmas, the residue, the tooling corollaries |
| 2 | [architecture.md](architecture.md) | domains, the compilation pipeline, coupling, where state lives |
| 3 | [language.md](language.md) | the complete language reference |
| 4 | [restrictions.md](restrictions.md) | what is removed and what replaces it (all compile errors) |
| 5 | [memory.md](memory.md) | arenas, ownership, the return protocol, sovereign types, growth |
| 6 | [slices.md](slices.md) | slice discipline, the reader/writer shapes, the cost model |
| 7 | [concurrency.md](concurrency.md) | spawn, channels, select, breakers, managers and workers, wasm hub |
| 8 | [patterns.md](patterns.md) | CSP/actor/DDD patterns that follow from the axioms |
| 9 | [security.md](security.md) | what the isolation buys, stated so it can be attacked |
| 10 | [compiler.md](compiler.md) | compiler internals: per-package arenas, the disk cache, the type database, measurements |
| 11 | [building.md](building.md) | toolchain, the 4 GB box, the bootstrap protocol |
| 12 | [style.md](style.md) | formatting and code style |
| 13 | [tutorial.md](tutorial.md) | a guided introduction with worked programs |
| 14 | [decisions.md](decisions.md) | implemented vs proposed, known-red list, measurements of record |
| 15 | [agent-skill.md](agent-skill.md) | the condensed rule sheet for an AI agent working in this tree |
| 16 | [wasm-hub.md](wasm-hub.md) | the wasm domain hub protocol: three planes, id scheme, reaping |
| 17 | [memory-budget.md](memory-budget.md) | where the compiler's bytes go: the per-phase ceiling table, measured, biggest path first |

`claude.md` at the repository root is the same normative content as 1-6 in rule
form, for an agent that must act without reading the set. Where it disagrees
with 2-6, the docs are the record and `claude.md` is the bug.

## What the language enforces, and what it hands back

Three of the pentalogue's five elements are the compiler's: **metal** (explicit),
**wood** (isolation), **water** (closure). Moxie enforces its side, and the
failures are loud - a compile error, a type error, a panic naming the region.

The other two are not the language's. **Earth** is the specification, and it
belongs to the client: one canonical form, stated once, so two implementations
agree. **Fire** is the code, and it belongs to the programmer: it terminates,
and it yields the same answer from the same input. Fire is the mechanism of
precision - the client says what they want, the programmer makes it exact. No
type system can check either, and both fail silently - earth as a disagreement
between two correct nodes, fire as a fork. Every silent divergence in a
distributed system is one of them.

The line between the two groups is not abstract versus concrete. It is that a
law with a free parameter cannot be a type rule: you must name the tier, you
must declare the channel, the tier set is fixed, but which algorithm and which
encoding are choices with no fact of the matter. Isolation and bilateralism are
one law, and sovereignty is arbitrary - json or cbor or protobuf, so long as
there is exactly one.

See [axioms.md](axioms.md#the-enforced-three-and-the-residue-two).

## The one-paragraph version

A domain is a thread in one process with its own arena tree and one event loop.
It cannot reach another domain's memory; the only handles between domains are
channels, whose rings live in shared memory but whose payloads cross by value -
as their own bytes when the element holds no references, through the codec when
it does. Every allocation belongs to an arena and every arena has one structural
release point: function return, phase exit, or the death of the domain. Nothing
is inferred: the topology, the capacities, the lifetimes and the frame budgets
are declared, and the compiler refutes what contradicts them.

## Conventions in these documents

- **MUST / never / compile error** means the compiler enforces it.
- **Authoring rule** means the compiler cannot see it and the shape is the fix.
- Anything marked TARGET is not implemented. See [decisions.md](decisions.md).
- File references are repository-relative (`src/runtime/alloc.mx`,
  `_mxc_stage4/ir_deepcopy.mx`) so a claim can be checked against the code.
