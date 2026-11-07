# The wasm domain hub: one protocol, two planes

Status: protocol fixed and implemented (round 386). The hub lives in
`jsruntime/supervisor.mjs`; the host supplies `start`, `stop`, `log`, `fault`
and `exited`, and the protocol runs under `node --test` with fake ports. The
step order that got it there is at the bottom.

## Why there is a hub at all

A wasm domain is a Worker. A Worker started by a Worker does not begin to run
while its creator is parked in `Atomics.wait` - the startup task needs the
creator's event loop - and a domain that spawns and then waits for the child is
exactly that shape. The page is the only realm that is never parked in wasm, so
the page starts every domain. That is not a convenience: it is the reason wasm
spawn can work at all.

The page is therefore already a switchboard. One protocol carries everything a
domain says to the page; the data plane does not go through the page.

## The three planes

| plane | path | what crosses | who reads it |
| --- | --- | --- | --- |
| control | domain -> hub (port) | spawn, exit, panic, rebind, heartbeat | the hub, by id |
| log | domain -> hub (port) | stdout/stderr text | the hub, then the page's console |
| data | domain -> domain (`SharedArrayBuffer`) | message bytes | the two domains only |

The first two share a port and an id scheme; they are still separate planes in
the hub (separate handlers, and logs have their own queue). Keeping them apart
is what lets a log be printed when its id is stale without that tolerance
leaking into control.

**Logs star to the page.** A domain posts its log to the hub; it is never
forwarded up the tree. A parent that forwards its child's logs is a parent
parked in a receive, which cannot pump anything, so a blocked parent would
silence its children - the same defect shape as a blocking select arm. The page
can pump on a frame callback and is never in `Atomics.wait`, so it is the one
place a log queue may sit.

**Data stays direct.** A spawn channel is a `SharedArrayBuffer` ring created by
the spawning domain. The hub forwards the handles (and the SABs) into the
child's boot message and never reads or writes a byte of the ring.

**The log queue is bounded, and a drop is reported.** A domain that prints
without a reader (a trace loop, a busy worker) must not grow the page without
limit, so the hub keeps at most `queueMax` entries (4096 by default). A log
line that arrives with the queue full is dropped, counted on that domain, and
the count is flushed when the domain exits as one stderr line:
`[hub] N log lines dropped for <id>`. The summary itself bypasses the queue if
it is still full - a summary of lost diagnostics is the one line that may not
be lost, or "no output" would be ambiguous again. The hub's `stats().dropped`
and `stats().unattributedDropped` carry the same numbers.

A channel that already sits on a boundary keeps its ring when it crosses
another spawn, on both targets: wasm's `runtime.SpawnChannelHandle` reuses the
existing SAB handle, and native's `spawnDomain` reuses the channel's existing
ring pair instead of creating a new one. Creating a second ring for the same
channel detaches it from the holder that passed it on - the
manager-hands-a-worker's-pipe-on case - and the two ends then wait on different
rings.

Native's direction is decided **per channel**, by the child-domain token in the
channel (`ch.childDomain`), not by a per-domain flag. Both domains hold the same
channel struct (they are threads), and a domain can be a child of one spawn and
the parent of another: a single "am I a child" flag picks the wrong ring for one
of the two, which is exactly how a spawned domain that spawns and then receives
used to hang.

## Addressing

- A domain's id is the **domain id**: one page-wide sequence, assigned at spawn,
  never reused. The root takes 1; its first child takes 2 and its lineage is
  `1.2`; that child's first child takes 3 and is `1.2.3`. The LAST component is
  the domain id itself - the same number the native runtime's `ch.childDomain`
  carries - so a log line, a hub node and a channel's role all name one number,
  and a debugging session never has to map one id scheme onto another.
- The lineage is the chain of those ids, which is what the hub builds from
  `parent`. A domain cannot choose or change its own id.
- **parent** is the id of the domain that started it; a root's parent is `0`,
  the hub.
- The hub builds the tree from `parent` alone. It does not parse `args`,
  `chans`, `wasm` or `fnIdx`; those fields are payload and are handed to the
  child's boot verbatim.

## Control messages (domain -> hub)

Every control message carries `kind` and `id`. `parent` is carried on `spawn`
(the sender's own parent, for the record) and set by the hub on the child.

| kind | fields | hub action |
| --- | --- | --- |
| `spawn` | `id, parent, wasm, fnIdx, args, chans, breaker` | allocate child id `id.N`; call the host's `start`; register the child under `id`; the payload goes into the boot untouched |
| `exit` | `id` | the domain returned normally: stop its Worker (a root's Worker is kept - the page drives it through the relay), adopt its children to its parent (a `rebind` to each), count it |
| `panic` | `id, text` | report a fault; the page stops every domain (native parity: a panic aborts the process) |
| `rebind` | `parent` | **hub -> domain only**: your parent id changed (the intermediate above you exited cleanly). A `rebind` in the other direction is a protocol error. |
| `heartbeat` | `id` | record `lastSeen`, nothing else |

`exit` and `panic` are different on purpose. A clean exit is not a failure: an
intermediate that returns leaves a live subtree behind, and the hub re-parents
it rather than killing it. A panic is the process dying, so the tree goes with
it.

## Log messages (domain -> hub)

`{kind:'log', id, fd, text}` where `fd` is 1 (stdout) or 2 (stderr). The hub
queues them and `drain()` hands them to the page in arrival order, per domain.

## Missing or mismatched id

This is where the next defects will live, so the table is part of the protocol:

| case | action | counter |
| --- | --- | --- |
| `spawn` from an unknown id | **refuse**: do not start a domain that the tree cannot place or reap. Reply `{kind:'refused', id, reason:'unknown-sender'}` to the sender's port. | `refusedSpawn` |
| `spawn` from a known id but on the wrong port | refuse, same reply, `reason:'port-mismatch'` | `portMismatch` |
| `log` from an unknown id | **print it** attributed to `?` - a log must never be dropped for a stale lineage, or the evidence for the next defect disappears | `unattributedLog` |
| `exit` / `panic` from an unknown id | drop (the node is already gone); count | `unknownExit` / `unknownPanic` |
| `heartbeat` from an unknown id | drop; count | `unknownHeartbeat` |
| `rebind` addressed to the hub | drop; count | `unknownRebind` |
| any control field with a non-string id | drop; count | counter per case |

A refused spawn is loud at the sender: the domain that asked sees `refused`
and reports it as an error. A hub that starts the domain anyway would create a
Worker no `exit` can match and no `stop` can find - an unkillable orphan, which
is worse than a refusal.

## Heartbeat is advisory, never a reaper

A domain parked in `Atomics.wait` cannot send a heartbeat: its thread is inside
the wasm call and its event loop is not running. A liveness timer that reaped
silent domains would therefore kill exactly the domains that are waiting for a
message - the defect this whole design exists to avoid. Heartbeats may inform a
diagnostic or a human; they must not decide liveness.

## Reaping

- **Clean exit**: children are adopted by the exiting domain's parent and told
  with `rebind`; they keep logging to the page and exit cleanly. This is the
  invariant test: A spawns B, A exits, B keeps logging and exits cleanly.
- **Clean exit of a root is not a process exit on wasm**: `_start` returning
  resolves the loader and the page keeps the root's Worker for callbacks, so
  the hub adopts the root's children at the page (`0`) and they keep running.
  The app stops them the way it always could: it holds their breakers. A page
  teardown (`stopAll`) is what ends a tree.
- **Panic**: the page stops every domain, as an abort ends every thread in a
  native process.
- **Subtree stop** is available to the hub (`stopSubtree(id)`) and is what the
  lattice will use once reaping is scoped per subtree.

## Divergence to keep visible: native needs no broker

Native spawn is a fork: the child is a process, the kernel owns the tree, and
nothing has to route anything. Wasm spawn requires the page's supervisor for
the lifetime of the domain - not just at spawn. So:

- the check is at **init**, not at spawn: a module built with spawn support
  (`__spawn_entry` present) that is booted without a supervisor fails at boot
  with a named message, not at the first `spawn()`;
- every harness path supplies one (`loader.mjs`, `startBundle`, the emitted
  host), and `sw-host.mjs` - which runs a module in a service worker with no
  page - is the one path that does not, so a spawn-capable module there is
  refused at init.

## Step order (from the design discussion)

1. This document.
2. Extract the hub into `jsruntime/supervisor.mjs` with a real log queue and a
   node-side test that feeds spawn/log/exit sequences with no browser.
3. Browser fixtures: the orphan invariant, and a parent parked in a receive
   while its child spawns (the shape that broke last time).
4. The lattice: hubs per arena, subtree survival policy, and fan-out - deferred
   until 1-3 are real.
