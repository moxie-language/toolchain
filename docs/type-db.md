# The type-resolution database

Status: the engine and the site population are implemented and committed; the
loader uses the engine for release, and the emitter still dispatches on the
method-name potential set. This is the fix for the compiler's whole-link
residency: the cold `http_header_api` build held the whole closure's type graphs
at once (root arena ~3.1GB at `objs-pre`, ~7GB peak RSS); after the release
work the same build peaks at ~2.4GB and the compiler self-build at 964MB.

## Why this is the final boss, and why it is structural

Interfaces are the only relation in Moxie that is not local. Every other thing
the compiler touches is a package or a function: a call has a callee, a field
has a type, an import has a path. "Does `T` implement `I`" is a question about
the whole program. Compiling the closure in one process - which buys speed by
not forking and not re-reading export data - pays for that speed with the one
thing the process boundary used to give for free: the closure never had to be
resident.

A type database does not shrink the closure by itself. It makes the *query*
cheap and, more importantly, it lets the compiler evict a package as soon as no
live question needs it. The precision comes from the question we ask, not from
the store. This note therefore states the population rule first, because a
missed conversion context means a package released too early, and that is a
link-time crash, not a compile error.

## Four pins before reusing a graph store

1. **Type nodes are keyed nominally.** If the table identifies a node by a
   content hash, two packages with the same shape collapse into one node - type
   confusion again, now in the database. The node key is the declaration path;
   the body is the payload and the structural hash is a secondary index.
2. **The reverse index is per edge kind.** `declares`, `implements`,
   `field-of`, `alias` and `embed` are different relations. One reverse index
   over all of them makes an "implements" query scan every edge in the store.
   Each kind gets its own forward and reverse table, so a query scans only its
   own relation.
3. **`exists` and `count` are first-class.** The release test is an
   intersection: does this interface's actual-implementer set meet the
   unreleased set? If the API hands the caller a list, the caller has
   re-materialised the closure and the memory win is gone. The store answers
   membership (`exists`), cardinality (`count`), and "does this set intersect"
   internally; a list is a diagnostic escape hatch, not the release path.
4. **Namespace it separately from the event log.** Events are retained forever;
   type facts live for one build. Sharing a store means retention is decided by
   the wrong policy, and an event store's fsync-per-put is wrong for a compiler
   doing thousands of small puts per package. The type index is a per-build,
   non-durable index over the `.mxh` fact files (which are already
   content-addressed on disk); writes are batched, there is no fsync per put.

## What the database stores

An entry is a value, so it is immutable and content-addressed:

- **Identity key: nominal.** `pkgPath + "." + TypeName` (plus the generic args
  when instantiated). Two packages can define the same layout under the same
  name and they are different types; a content-addressed store would dedup them
  into one entry and that is silent type confusion. The structural hash is a
  secondary index for sharing fact bodies, never the identity.
- **Fact body**: kind, size, alignment, underlying declaration text, struct
  fields (name, offset, type key), method set (name, signature text, pointer
  receiver), type parameters, defining package, version hash.
- **Versioned path index**: `pkgPath -> fact-set hash`. Entries are immutable;
  the index is mutable and checkpointed. A new body is a new entry; the index
  moves. An old entry is dropped only when no live index version references it
  (reference count from the index roots, or a rule that a build version pins
  its reachable entries and a sweep drops the rest).
- **Two ordered edge tables per relation**, each keyed so a prefix scan answers
  one direction in one iteration:

  The engine has exactly six indexes; they are the schema:

  | index | shape | why |
  | --- | --- | --- |
  | type -> methods | `typeKey -> [methodName]` sorted | satisfaction is a subset test; never loads another package |
  | package -> interface contributions | `pkg -> [(ifaceKey, typeKey)]` | release is O(contributions), not a scan of every interface |
  | (package, name) -> record | `pkg|name -> fact` | nominal identity lookup |
  | hash -> record | `structHash -> [fact]` | interning bodies; never identity |
  | package -> types | `pkg -> [typeKey]` | release and versioning are per package |
  | alias/underlying -> named types | `target -> [aliasKey]` | `Underlying()` and field offsets |

  There is deliberately **no `method name -> types` index**. That is the exact
  reverse walk that started this: with site-based population nobody needs to
  ask "who could implement `String`", so storing the answer would be storing
  the bug. `implements` is stored forward (`iface -> actual types`) plus the
  package-contribution index; there is no method-name reverse table.

  Every index has a cheap delete, because release *is* delete. An index that
  only appends turns the memory win into a slower one, and the profile finds it
  only after the DB is bigger than the graphs it replaced. `deletePkg` removes
  the package's facts, its contributions and every edge incident to them in
  time proportional to that package's own entries.

The fact files already exist: a package's `.mxh` is its serialized export
facts. The DB indexes the file, and a lookup decodes only the requested fact
text. Materialising a `TCPackage` is an *optional cache*, never the storage.

## The population rule: conversion contexts

The interface association is populated from **actual conversion sites**, not
from "types that could implement it". The distinction is the whole point: every
package in a real link has a `String` or `Error` method, so the potential set is
the closure; the traffic set is a handful per interface.

A conversion to interface `I` is any place where a value of concrete type `T`
becomes an `I` value. Enumerate all of them:

1. **Assignment** - `var i I; i = v`, and short declarations whose right side is
   concrete and left side interface-typed.
2. **Argument** - a call argument, including a variadic argument and the
   element passed to a spawn/channel send whose declared element type is `I`.
3. **Return** - `return v` where the result type is `I` (named result stores
   included).
4. **Struct field** - storing `v` into a field of interface type, through a
   value, a pointer, or a composite literal.
5. **Slice element** - storing `v` into an element of `[]I`, including
   `push`, a composite literal, and an index assignment.
6. **Map element** - a value of interface type in `map[K]I`, including the
   literal and `m[k] = v`; the key side is not a conversion to `I`.
7. **Interface-to-interface conversion** - `j = i` where `i` is `I1` and `j` is
   `I2`; the dynamic type is unchanged, but the association `(I2, T)` becomes
   real for every `T` already associated with `I1`. The site must be recorded
   as an edge between the two interface keys so the closure of this relation is
   available.
8. **Method value** - `f := x.String` where `f` is `func() string`: the value
   is placed into a function value and dispatched later; it is a conversion
   site for the method it names. A method expression `T.String` is the same
   fact without a receiver.
9. **Type switch / type assertion** on an `I` does not create an `I` from a
   `T`; it reads one. It is a *use* of the interface and keeps `I` live, but it
   adds no `(I, T)` edge. A type switch that binds `case T` and then stores the
   bound value into an `I` is covered by (1).
10. **Interface-typed package variable / global initialiser** and **const-like
    zero values**: the initialiser is an assignment (1) at init.

The site record is `(ifaceKey, typeKey, sitePkg, position)` and is counted in
the forward/reverse tables above. The **counter is traffic**: when a package is
a candidate for release, the question is not "could anyone convert to it" but
"does any live interface's actual-implementer set intersect the package's
types". If not, the package's graph can go.

This is **points-to analysis in disguise**: the dynamic types that can flow
into an interface are exactly the concrete types converted at these sites. The
precision is bought by the site list; the database only stops the compiler
paying for the same answer twice.

Failure mode to guard: a missed context (e.g. method values) releases a package
whose type still flows into a live interface dispatch, and the link crashes or
silently dispatches through a missing arm. The compiler must record sites at
the SSA builder (which sees the operands and the declared types), not from a
text scan.

## What the fixture already shows

The cold `typedb_nominal` build records **201 conversion sites** and collapses
them to **48 distinct `(interface, type)` edges**, with 3 actual `fmt.Stringer`
implementations. The old method-name potential set for the same link is the
whole closure. This is a fixture, so it is a ratio rather than a scale, but it
is the empirical answer to the objection that traffic and potential are the
same thing: they are not, and the site-based population is what makes the
counter small.

## Mirror scope: emitted code, not layout

The bilateral rule is about **emitted code**. A compiler-internal change that
produces byte-identical output - arena scoping, releasing a graph at last use,
building/emitting/dropping one function body at a time - is stage4-only: the
legacy compiler is a reference for what the program means, not for how the
stage4 process arranges its memory. Mirror when the arms, the codegen or the
language behaviour change; until then the population hook can wait.

The scope needs an assertion, because nothing stops the next feature from
accumulating on the build arena under a different name. `mxc build` with
`MOXIE_ARENA_CHECK=1` fails loudly when one package leaves more than 32 MB on the build arena
(the observed baseline is a few MB per package); the build arena is
build-lifetime, not per-package. Run it on a cold bulky link (the `http_header_api` build) when
touching the loader or the fingerprint pass.

## What is implemented

- **Engine**: nominal fact keys, structural hash as a secondary index, the six
  indexes, `exists`/`count`/`intersects`, and `typeDBDeletePkg` (release is
  delete). `mxc typedb <file.mxh>...` prints the shape; the legacy compiler has
  the same engine and diagnostic.
- **Prepass**: the fact index is built from the `.mxh` files without
  materialising a `TCPackage`.
- **SSA**: the three `MakeInterface` sites (assignment/coercion, return,
  argument) record the implements edge, the package contribution, and the
  target interface's method set. The nominal fixture records 201 sites and 48
  distinct edges.
- **Release**: a package's graph lives in a per-package arena and is freed at
  its last-need index; the dispatch pre-pass's whole-link graphs are dropped
  before the per-package compile loop; the fingerprint measurement has its own
  freed arena.
- **Assertion**: `MOXIE_ARENA_CHECK=1` fails loudly when one package leaves
  more than 32MB on the build arena.

## What is not implemented yet

- **Typecheck from facts**: the typechecker still materialises each package's
  graph whole, into its arena. The facts are the index, not yet the lazy
  resolution path.
- **Arm narrowing**: `findIfaceImpls` still enumerates the method-name potential
  set. Using the actual implementer set for arms requires the arm facts (type
  IDs, symbols, signatures) to be frozen, and the site pass to run before the
  fingerprints so the cache key and the arms agree.
- **Persisted sites**: a warm build reuses object files and has no site data
  for untouched packages, so the potential set remains the fallback.
- **Per-function phase**: the peak is still the package's `ci-tc` SSA body set,
  not the largest single function.

The next bound is therefore "largest function, not package": typecheck the
declarations once, keep a compact per-function summary, and build/emit/drop one
body at a time.
