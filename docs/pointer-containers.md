# Containers of pointers: survey and migration plan

Companion to `issues.md` C-11. That entry states the defect; this one scopes it,
states the rule that removes it, and lists every site found in the three trees
(`moxie`, `smesh`, `morly`).

## The defect, as measured

A method that stores a pointer into a container and then returns that same
pointer hands back a copy, not the stored object:

    func (p *pool) connect(k string) (c *box) {
        c = &box{n: 1}
        p.conns[k] = c     // c is relocated into the map's storage
        return c           // this is the PRE-store local
    }

    storeAndReturn aliases: 1     <- broken: the caller's write is lost
    returnOnly     aliases: 8     <- correct

The same program built with plain values instead of pointers is correct:

    map[string]box + method returning box   -> both writes land

And these pointer shapes are all **correct** (measured, `/tmp/lab2`-`/tmp/lab6`):

| shape | result |
| --- | --- |
| `fresh()` returning a newly allocated pointer | correct |
| `ident(b *T)` returning a pointer it was given | correct |
| `fromMap(m, k)` — free function returning `m[k]` | correct |
| method returning `m[k]` directly | correct |
| method returning `m[k]` via a local, no store | correct |
| method that ranges a map and returns the element | correct |
| **method that stores a pointer into a container, then returns it** | **broken** |

So the rule is narrow, and it is not "maps hold values" in general — that part
already works exactly as in Go. It is: **a pointer that has been stored into a
container must not also be returned from the same frame.** Either return the
container lookup (`return p.conns[k]`, which reads the stored object) or do not
return it at all.

That is why the relay failed exactly once and for one reason: `Pool.Connect`
stores into `p.conns` and returns the local. Every `Subscribe` after the first
was applied to a copy, the socket callbacks kept the original, and every live
event for those subscriptions was delivered to a `Conn` that no longer held
them.

## The rule for authors

1. **Prefer values in containers.** `map[K]T` and `[]T` are correct for every
   shape measured, and they are what Go does. A container of pointers is only
   justified when the element is genuinely shared and mutated through more than
   one holder.
2. **When a container holds pointers, never return the pointer you just
   stored.** Return the lookup instead, so the caller receives the stored
   object rather than the pre-store local.
3. **A long-lived object that is reachable from more than one holder should not
   be handed out at all.** This is the architectural point behind the relay
   fix: subscriptions belong in a queue owned by a worker, addressed by key, so
   no pointer to a shared object crosses a frame.

## Survey

Method: walk every `.mx` file in the three trees, find a pointer-typed local
that is assigned into a container and then returned from the same function,
then keep only the hits where the enclosing function is a METHOD returning a
pointer — the one shape measured as broken.

    raw hits:                     16
    of those, methods returning a pointer:  9
    verified false positives:     2 (Subscribe returns Sub by value;
                                    cordnNewGroup is a free function)

Seven candidates, all in `moxie`; none in `smesh` or `morly` beyond the relay,
which this pass fixed:

| file | line | function | note |
| --- | --- | --- | --- |
| `src/net/http/h2_bundle.mx` | 843 | `(*http2clientConnPool).getStartDialLocked` | **live**: `p.dialing[addr] = call; return call` |
| `src/net/http/h2_bundle.mx` | 6263 | `(*http2serverConn).newStream` | `s.streams[id] = st; return st` |
| `_mxc_stage4/ssa_builder.mx` | 49 | `(*SSAProgram).CreatePackage` | stored in `prog.packages` and returned |
| `_mxc_stage4/ssa_builder.mx` | 7377 | `(*ssaFuncBuilder).ensureImportedSSAPackage` | |
| `src/internal/profile/merge.mx` | 136, 226, 369 | `mapSample`, `mapLocation`, `mapFunction` | |
| `src/internal/trace/summary.mx` | 585 | `(*Summarizer).getOrAddTask` | name says "get or add" — exactly the shape |

Each needs its call sites checked before it is called a bug: the defect only
bites when the caller mutates through the returned pointer and expects the
container to see it. The list is the work queue, not a verdict — two of the
sixteen raw hits were already false positives, so every entry gets read.

**Not surveyed, and worth a separate pass:** the same shape reached through a
struct field (`x.y = p; return p`), through `push(s, p)` (growth relocates, so
it should be equivalent), and returned into a caller's struct field. The relay
defect was found by reading a fault, not by this survey, so the survey's recall
is unproven.

## Do we change the call sites or the compiler?

Both, in this order.

**Step 1 — the seven candidate methods above (call-site fixes).** Cheap, local, and
they make the trees correct on today's compiler. Each is either "return the
lookup" or "stop returning the pointer". This is the work that unblocks the
cordn interop: `Pool.Connect` is the one that matters there.

**Step 2 — a regression test in the compiler's own suite.** The 20-line
reproduction from `issues.md` C-11 belongs in `tests/restrictions/` so that the
shape cannot silently return. It must fail today (proving it tests the right
thing) and pass after step 3.

**Step 3 — the codegen fix, both compilers (wood law).** The return protocol
relocates return values into the caller's arena; it should relocate the value
as it stands at the `return`, which for a stored pointer means the stored
object. `legacy/compiler/` first, `_mxc_stage4/` second. Until this lands, every
`map[K]*T` in the tree stays a trap and a new one can be written any day.

Doing step 3 first would be the "fix it properly" instinct, but it changes
return handling for every function in the tree, and there is no way to tell a
correct result from a silently different one without the call sites and the
test in place. Steps 1 and 2 are what make step 3 verifiable.

## Acceptance

- The C-11 reproduction compiles and prints `a==b true` with the write visible
  in the map.
- `moxie test ./pkg/types` and the stage4 self-build are unchanged.
- The cordn interop test passes with `Pool.Connect` fixed and nothing else
  changed in the relay client — that isolates the claim.
- The survey script is re-run over the three trees and reports zero
  store-then-return sites.

## Note on what this is not

This is not a mandate to convert every `map[K]*T` to `map[K]T`. Values are
preferred where the element is just data, but a container of pointers is
legitimate for shared mutable objects — which is exactly why the relay fix
should go further and move subscription state into a worker-owned queue keyed
by id. Pointer containers need the rule above, not blanket conversion.

## 2026-11-11 - what happened when the first fix was applied

`Pool.Connect` now returns `p.conns[url]` after the store instead of the local.
The relay proxy builds, and this is a correct fix by the measured rule.

The cordn interop test still fails: cordn's peer polls `requests=0` for 150
rounds and the app stays at "waiting to be added to the group". So the pool
defect was **one** of the reasons the subscription table was empty, and not the
only one. The remaining candidates, in the order the evidence supports:

1. `Conn.subs` is still written on an object the callbacks do not reach —
   the relay proxy's `getConn` returns through `Pool.Connect`, and the method
   call `c.Subscribe(...)` binds the result to a local in `handleMLSSub` before
   calling. If the pointer is relocated again at that call site, the fix moved
   the problem one frame rather than removing it.
2. The worker's own `cordnGroups` map in `web/wasm/mls/cordn.mx` has the same
   shape (`cordnNewGroup` stores and returns) and is used by `cordnAnswer` to
   find the pending request.
3. Something outside the pointer question: the answer may not be reaching the
   proxy at all.

The next measurement is the same one that worked before — the relay client's
frame trace, kept small — but the honest note is that each hypothesis so far
has been disproved by the next run, and the interop does not work yet.

## What was committed

- `moxie`: runtime diagnostics and the fault report, the spawn retirement fix,
  the loop-rule classification.
- `morly`: the transport close shape.
- `smesh`: the worker log binding, the cordn wiring, subscribe-before-publish,
  the no-op re-subscription guard, and `Pool.Connect`.
