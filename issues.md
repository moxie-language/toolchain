# Moxie memory-management defects found while fixing the morly relay

Status: investigation record + remediation plan.
Date: 2026-11-10.
Scope: moxie compiler/runtime defects first (foundation), relay defects second.

Evidence is from controlled local relays, a copy of the live store, and the live
`smesh.lol` relay. Numbers are process RSS unless stated otherwise.

## 1. What works (do not regress)

- Store read loop, one function arena per iteration, 6M iterations: RSS 3.7-4.3 MB,
  net mappings constant, 23k compactions, group live bytes flat.
- Relay subscription soak, 30,000 REQs after subscription ownership fix:
  `groups=11`, RSS 236-375 MB, idle 300 MB, no crash.
- Broadcast worker with per-subscription release: retained 39.4 -> 40.1 MB,
  `glive` ~1 MB, `gcap` 1.35 MB flat.
- Main relay domain under live soak: `groups=53`, `glive` 17 MB, `gcap` 34 MB,
  mapping net 323 MB, 9 threads.

The runtime machinery is sound when (a) the loop owns a per-turn arena and
(b) receiver scratch is produced by free functions. The defects below are the
counterexamples.

## 2. Compiler / runtime defects

### M-01 Compaction roots are incomplete (critical)

`sovFullMark` walks the receiver's fields and the selecting frame's published
slots. It does not walk pointer-bearing **parameters and locals** of live
frames, nor helper objects (pipeline, ACL checker) that hold interior pointers
into a group. Compacting the Server or Engine group under live load moves data
those holders still reference.

Evidence:
- SIGSEGV in `runtime.hashmapContentGet` every time compaction of the Server or
  Engine group ran under real traffic (multiple crash PCs, e.g. 0x328240,
  0x329010, 0x329530).
- Store soak with an empty store and no exterior holders compacts safely, so
  relocation itself is not the fault; the missing roots are.

Impact: compaction cannot be used on any long-lived receiver with exterior
holders. That removes the only reclamation path for those groups.

### M-02 `escaped` is permanent and ownerless (critical)

A group whose data crosses a codec boundary or is returned as a stable alias is
marked `escaped=true` forever. There is no record of which frame, slot or helper
owns the alias, and no release when that owner dies.

Evidence:
- Engine group was permanently escaped by returning query results; `comp=0`
  for the DB domain no matter how long it ran.
- A manual `SovUnescape(recv)` at a request boundary restored compaction only
  for callers that knew the receiver and the safe point.
- The user-visible failure is exactly "a thread nils a variable, but the
  variable can have no other owner" - the runtime never learns that the owner
  went away.

### M-03 No canonical, safe receiver-group release (high)

`codecSovereignFree` performs the correct teardown: unqueue from compaction,
abort a paused cycle, free data/record/reference arenas, remove the receiver map
entry. A hand-rolled release that omitted the unqueue left a freed group in the
pending queue; the main loop later compacted it and crashed.

The runtime needs one canonical entry point, used by generated code on receiver
death and available as a safe API, and it must be reflected in the diagnostics
counters (`RecordSovRelease`).

### M-04 Root arena has no scope (high)

`SovereignSetArena(RootArena())` makes every subsequent allocation permanent.
The relay used it to keep parsed requests alive and leaked one sovereign group
per REQ (5,010 live groups after 5,000 REQs). There is no root-scope push/pop
that relocates results or refuses unbounded growth, and `sovRootAlloc` grows the
root region by region forever.

### M-05 Mutability classification is invisible (high)

Whether a method borrows the sovereign group depends on whether it calls any
method on a receiver field. A method that looks read-only can therefore own the
group:

- `QueryEvents` called `e.flushPending()` and thus borrowed the Engine group;
  every query's returned event slice relocated into it.
- `GetBySerial` called `e.w.Read(...)`; every decoded event landed in the
  Engine group.
- `sorted.File.Get`/`scanBinder` call methods on `s.f`/`s.buf`; every record
  buffer landed in the file's group.

Moving hot-path work into free functions fixes the allocation, but the
programmer has no signal that the method they wrote is a group writer. The
compiler can see this; it should classify pure reads, warn on group allocation
in mutating methods, and offer an explicit scratch form.

### M-06 No dead-only reclaim for a live receiver (high)

`SovReleaseGroup` frees everything, including cached fields (`sorted.File`
keeps `buf`, `sideBuf`, `del`, counts). `SovTrimAll`/`sovTrimTail` mark and trim
but cannot see params/helpers, and crashed. There is no operation that means
"reclaim this receiver's dead per-call scratch, keep its live data and cached
fields". That is the primitive a long-lived engine/index needs.

### M-07 Maps are not safe under compaction (high)

A map header or map pointer held outside the group - a cached `map` field copy,
a filter's tag map handed to another receiver, a helper's index map - dangles
after a move. Faults are always `hashmapContentGet`.

Until the rebase path is proven for every external holder, groups containing
maps must pin (refuse compaction), or map headers must be registered as stable
slots that rebase can rewrite.

### M-08 Diagnostics are not trustworthy (medium, foundation)

- Per-domain `net` reported 2.4 GB while process RSS was 410 MB.
- `sov created/released` did not move when groups were released (the manual
  release path did not call `RecordSovRelease`).
- `SovGroupTotals`/`SovGroupAt` dedup by first region base and can miss moved
  groups; the reported top group could not be matched to any known receiver.
- `liveArenaCapBytes` counts only fn stack and pool, not all mappings.

Instrumentation that names the wrong owner wastes rounds. Fix the counters and
the per-group report before relying on them again.

### M-09 No safe domain/worker restart (medium)

Exiting a domain frees everything, but replacing a worker races spawn-channel
and child bookkeeping. A budgeted DB respawn bounded memory locally and crashed
live in `runtime.MarkChildPipeChannel`. An in-place engine reopen crashed in
`hashmapContentGet`. There is no supported "retire this domain/state at a
barrier and continue" primitive.

### M-10 Stack size is a hard-coded default (low)

Domains used 64 MB stacks (`thread_unix.c`), 9-15 threads, so stack touch alone
was hundreds of MB. Reduced to 16 MB in this workspace. The default should be
per-role/configurable, not one large constant.

## 3. Relay defects

- R-01 (fixed) `handleReq` parsed `envelope.Req` under the root arena and stored
  `req.Filters` in `c.subs`. The group was keyed by a root frame local and was
  unreleasable: 5,010 groups / 5,000 REQs. Fixed with `sub.load` + canonical
  release on CLOSE/close.
- R-02 (fixed, release staged off for bisect) broadcast worker did the same for
  stored subscriptions; deleted map entries left groups behind (~18 MB/min).
  Fixed by recording each parse receiver address and releasing on
  Remove/Close.
- R-03 (staged) DB read path: `GetBySerial`, `scan*`, `collectSerials`,
  `getEventSerial`, `queryByIDs`, `WAL.Read`, `sorted.File.Get/scanFn/
  scanBinder` were receiver methods. Converting them to free functions dropped
  the DB domain from ~366 MB retained / 20 MB glive / 205 MB gcap to ~40 MB /
  2 MB / 4 MB and RSS to 111 MB locally; live held flat 300-330 MB then faulted.
  Not shipped; the fault is under investigation (R-06).
- R-04 `pipe`, `checker`, `bl` hold views into the Engine group, making
  compaction unsafe (M-01). Releasing/recreating them did not reduce `glive`
  (wrong/nested receiver addresses) and added risk.
- R-05 `*sorted.File` owns persistent caches. Releasing index groups in place
  dangles `buf`/`sideBuf`/`del` (immediate local SEGV). Any scan conversion must
  preserve those fields.
- R-06 The live-only fault after the sorted-free conversion is
  `runtime.hashmapContentGet`, not the scan code. Candidates: a file-cached
  buffer, the broadcast release racing a fanout/filter copy, or pre-existing map
  header aliasing. Local 12k-query stress had not reproduced it before pause.
- R-07 DB respawn/recycle attempts are unsafe for the reasons in M-09; removed.
- R-08 64 MB stacks and default worker pools inflated baseline; stacks reduced
  to 16 MB, worker pools set to 2/2/2 in `/etc/morly/morly.env`.

## 4. Current state

- Live `smesh.lol` runs the **stable** build: sorted receiver methods, no DB
  respawn, no index release, broadcast release enabled in the deployed binary.
  App 200, WS REQ ok, Alby Hub active. RSS drifts upward under traffic into the
  3 GB `MemoryMax` guard; idle was flat at ~311 MB.
- Workspace has staged, unshipped changes: sorted-free read path, broadcast
  release disabled for bisect, DB diagnostic reports. `morly.prev` is the
  previous binary.
- Local relays are stopped.

## 5. Remediation plan: compiler/runtime first

Foundation before walls. Wood law applies to every item that changes emitted
code or the runtime ABI: legacy first, stage4 second, same commit or adjacent.
Runtime `.mx` changes (alloc/rootmap/codec) are shared by both compilers, but
must be verified through both build paths.

### Phase 0 - Ground truth (prerequisite for everything)

1. Fix diagnostics: count every group release (`RecordSovRelease` on
   `codecSovereignFree` and any other path), account unmaps in `sysmap` net,
   make `liveArenaCapBytes` count mapped arenas, and make the per-group report
   print receiver address, type name (from `RawType` if available), live/alloc/
   cap, and delta since the previous report.
2. Add soak harnesses (scripts under `tests/`, run against a local relay and a
   store copy):
   - store read soak (no relay), 10M iterations, assert RSS flat + group live flat;
   - subscription soak, 30k REQs, assert groups bounded;
   - relay query soak with the live-store copy, 20k REQs and 4 concurrent clients;
   - live soak against `wss://smesh.lol`, 30 minutes, sample RSS every 5 s.
3. Acceptance: instrumentation matches RSS within 10%; the harness reproduces a
   known leak before any fix.

### Phase 1 - Lifetime API correctness

1. Canonical `SovReleaseGroup(recv)` = `codecSovereignFree` only. Remove or
   delegate every duplicate. Make it idempotent, safe while queued/cycling, and
   counted.
2. Add `SovReleaseDead(recv)` skeleton (full mark, free only unmarked records
   and regions, keep live and cached fields). It is completed in Phase 3 once
   roots are complete; land the API and tests now.
3. Root scope: `RootScopeEnter/Exit` (or a root arena with relocation) so
   compiler-emitted code and hand-written code can allocate per-message
   temporaries without permanent growth. Compiler uses it where it currently
   switches to `RootArena()` for parse-into-long-lived-state.
4. Tests: release a group while it is queued for compaction and while a cycle
   is paused; root-scope leak test; assert no queue entry survives release.

### Phase 2 - Root completeness and escape ownership (hard, highest value)

1. **Escape holders.** Replace the permanent boolean with a registered holder:
   on `codecSovDataLive`/stable return, allocate a `sovSlot` that names the
   receiving frame/stable arena and the group, and register it. Rebase rewrites
   the slot. A holder dies when its frame/arena is released; the runtime then
   clears the slot and un-escapes the group.
2. **Compiler-emitted escape release.** When a local that may hold a sovereign
   alias is overwritten, set to nil, or goes out of scope, emit the slot release.
   Legacy first, stage4 second. This is the "owner was severed" signal the groom
   needs; without it, permanent escape is unfixable for long-lived callers.
3. **Frame roots for compaction.** Publish type-described roots for pointer-
   bearing params/locals of live frames to `sovFullMark` (conservative: all
   params whose type has pointers). Include the select frame's root slots with
   type descriptors (M-01). Groups with unregistered external holders are
   refused, not moved.
4. **Map headers as slots.** Either pin any group containing a map until map
   rebase is proven, or register map headers as stable slots and rebase them.
   Start with pinning; add slots once tests cover exterior copies.
5. Tests: compaction with an exterior holder, a map header copy, a param alias,
   and a select-published slot. Must not fault; live bytes must remain correct
   after the call.

### Phase 3 - Reclamation

1. Complete `SovReleaseDead`: mark with complete roots, free unmarked records,
   trim/shrink regions, keep cached fields and live records. Use it at worker
   request boundaries.
2. Re-enable compaction in relay loops (transport turn arena, DB worker) and
   remove `SovUnescape`/manual release workarounds from relay code.
3. Re-enable tail trim only after dead-only reclaim passes the harness.
4. Tests: long-lived Engine under 10M queries, RSS flat and `gcap` bounded;
   relay local soak 20k with 4 clients flat; then live soak.

### Phase 4 - Compiler ergonomics (prevents the next relay)

1. Improve mutability classification: a method that only reads fields and calls
   read-only methods on them does not borrow. Where that is not provable,
   classify conservatively but **warn** when a mutating method allocates more
   than a threshold in its group.
2. Add an explicit scratch form for mutating methods (authoring rule: heavy work
   in a free function) and document it. Do not invent a GC or a heap.
3. Warn on `RootArena()` allocation inside a function that can run per message.
4. Bilateral: classification changes alter emitted code; legacy first.

### Phase 5 - Lifecycle and defaults

1. Safe worker retire/replace: a barrier that waits for the child's channel
   teardown (parent-ack) before a new domain is spawned, or an explicit
   unsupported-error if the primitive cannot be made safe.
2. Stack size per role/config; default 16 MB. Spawn option, not a global
   constant.
3. Remove `/debug/*` routes from the production path once the harness is stable
   (or gate behind an env var).

### Phase 6 - Relay (only after the foundation)

1. Apply M-02/M-06 primitives to the DB Engine group; retire the free-function
   workarounds only where they are no longer needed.
2. Reapply the sorted-free read path with file-owned caches; fix R-03/R-05/R-06
   using the new roots/reclaim.
3. Re-enable broadcast release at a quiescent point; deep-copy or own the
   filters in `bcastConnState`.
4. Run the acceptance soaks; then remove the 3 GB guard dependence.

## 6. Acceptance criteria

- Store read soak 10M: RSS within 2x baseline, no slope, group live flat.
- Subscription soak 30k: `groups` bounded (<100), RSS no slope.
- Relay query soak 20k with 4 concurrent clients, live-store copy: RSS flat,
  no SIGSEGV, zero lost requests.
- Live `smesh.lol` 30 minutes under normal traffic + a 3k-query soak: RSS flat
  (no positive trend), no restart, app 200, WS REQ, Alby Hub active.
- Both compilers: legacy and stage4 produce the same behavior for every codegen
  change; bootstrap fixpoint unaffected; no new stubs, no GC model, no
  recover().

## 7. Open questions

- Is the live `hashmapContentGet` fault caused by a cached buffer in
  `sorted.File`, by the broadcast release, or by a pre-existing map-header
  alias? Bisect first; do not ship a fix for the wrong one.
- Can `sovFullMark` see frame params without a per-call descriptor explosion?
  Conservative "all pointer-bearing params" is acceptable if measured.
- Does map rebase hold for exterior map headers, or should maps pin groups
  permanently? Pin first; revisit with tests.
- Can a worker be replaced safely at all under the current spawn-channel
  teardown, or is in-place reclaim the only supported model?

## 8. Progress log

### 2026-11-10 - Phase 2 first cut (conservative roots)

Implemented in `src/runtime/alloc.mx`:

- `sovMarkStack`: scans the current thread's active stack for interior pointers
  into the group being compacted. Parameters and locals are now roots even
  without compiler-emitted descriptors.
- `sovMarkExternalGroups`: word-scans the live records of every other group and
  marks any block of the target they point into. Helper-held aliases
  (pipeline/ACL/index views) are now roots.
- `sovMarkBlock` walks from `rec.dataAddr`, not from the interior pointer it was
  given (a type walk from an interior address applied field offsets to the
  wrong base).
- `codecSovDataLive` no longer aliases a transient sovereign return: it returns
  false, so the codec copies. This removes the permanent `escaped=true` on the
  hottest path until escape-holder lifetimes exist. `codecNoteStable` still
  marks stable aliases.

Measured with the live-store copy:

- standalone store, 200,000 queries, compaction every 256: progresses normally,
  `comp` ~390, `glive` ~44 KB at 100k, `gcap` 241 KB, RSS ~6 MB, no crash.
- local relay, 3,000 queries with the live store: RSS 294 -> **96.5 MB**,
  DB `glive` ~1.0-1.4 MB, `gcap` 1.2-2.3 MB, `comp` 3-4, no panic, zero lost.
- live `smesh.lol`: no crash, app/WS/Alby healthy. RSS still trends up under
  real traffic (~100 MB/min idle with the current deployment).

Phase 2 is therefore **partially landed**: the conservative roots make
compaction safe on the DB engine and totally change the local result, but the
mark is conservative and the live residual has another owner.

### R-02 update: broadcast group release is unsafe

Re-enabling `SovReleaseGroup` for broadcast subscriptions (Remove/Close) caused
a live SIGSEGV in `hashmapContentGet` within ~25 s under the REQ soak (PC
0x3294a0), even with conservative roots. The release is disabled again. The
stored `filter.S` values point into the parse group, and freeing that group
while another copy/map is still reachable corrupts the tag maps. Broadcast
growth therefore remains; it must be fixed by making the connection own its
filters (single per-connection holder, or deep copy into a connection arena)
rather than by releasing the parse group.

### 2026-11-10 - Phase 2 second cut: DB compaction enabled, broadcast ownership

Changes:

- DB worker drains compaction every 256 requests (`runtime.SovDrainCompactions`)
  on top of the conservative mark. Live-store local relay: RSS 294 -> 95 MB,
  DB `glive` ~1 MB, `gcap` 1-2 MB, `comp` 3-4, zero panics, zero lost.
- Broadcast: `bcastConnState.loadSub` parses each REQ through a **mutating method
  on the connection**, so all filters for a connection live in one sovereign
  group instead of one root group per subscription.
- Broadcast group release at connection close is still **disabled**: two live
  soaks with it enabled faulted in `hashmapContentGet` (PC 0x3294a0 / 0x329500)
  at connection teardown. The likely cause is hashmap bucket ownership: a map
  grown while the connection's group was current can have buckets in that
  group, and `SovReleaseGroup` frees them while map state remains reachable.
  This needs the map-owner fix (M-07) before release can be re-enabled.

Live measurements with DB compaction + no broadcast release:

- idle with real Alby/app traffic: 301.8 -> 312.6 MB over 160 s (~4 MB/min,
  effectively flat against background churn).
- one 3,000-subscription soak: 301 -> 445 MB (per-connection filter high-water),
  client completed, no crash, `restarts=0`; app 200, WS ok.
- the same soak connection closing no longer crashes, but the high-water is not
  returned (release disabled) — a second soak would add another connection's
  worth.

The original 200-500 MB/min unbounded growth is gone. What remains is
per-connection broadcast high-water with no safe reclaim, not a per-query leak.

### Next concrete steps

1. Fix map ownership (M-07): either allocate hashmap buckets in the map's
   owner arena regardless of the current arena, or pin/free correctly on group
   release. Then re-enable the broadcast connection-group release.
2. Add a per-connection recycle if map ownership cannot be fixed quickly:
   rebuild the connection's filters into a fresh group when its live bytes
   exceed a budget (requires storing raw REQ bytes per sub).
3. Commit the Phase 0 harnesses under `morly/tests/soak/` (store, relay,
   live) and run the 30-minute live acceptance soak.
4. Revisit `codecNoteStable` (stable aliases still set `escaped` permanently);
   M-02 is only solved for transient returns.

### 2026-11-10 - Phase 2 third cut: map owner pins and the multi-connection fault

- Pinned `c.subs` (server) and the broadcast `subs` map to the root arena with a
  dummy insert/delete at creation, so the first write inside a per-turn or
  sovereign arena cannot become the map owner. `hashmapSet`, `hashmapGrow` and
  `hashmapInsertIntoNewBucket` already allocate in `hashmapOwner(m)`.
- Re-enabling the broadcast connection-group release **still faults** live
  (0x3294a0 / 0x329500) even with pins. Disabled again.
- Disabling the DB compaction drain did **not** stop the fault: soak 1
  completed, soak 2 faulted at 0x329500. So the crash is neither the DB
  compaction nor the broadcast release.
- The fault is reproducible only on **connection churn across soaks** (second
  connection), not on a single local connection. It is in
  `hashmapContentGet`; the map is not yet named.
- Production deployment: conservative mark in the runtime (inert while
  compaction is off), no DB compaction, no broadcast release, server
  subscription release enabled, map pins in place. Idle with Alby/app traffic
  is flat at ~301-312 MB over minutes; a 3k-subscription soak runs once and the
  second one can fault.

The next investigation must be a controlled **multi-connection** harness: one
client connects, creates 3,000 subs, closes; a second connects and repeats.
Run it locally with the live store until it faults, then name the map from the
fault PC (candidate maps: `conns`, `c.subs`, `sovereignRoot`, filter tag maps).
Fixing that map lifetime is a prerequisite for re-enabling any release or
compaction under churn.

### 2026-11-10 - Loop compaction is the live crash trigger

- Local test with 4 concurrent connections x 800 REQs: no fault, RSS
  297 -> 118 MB. So churn alone does not reproduce it.
- Live with the **transport loop compaction disabled** and the broadcast
  connection-group release enabled: two consecutive 3,000-REQ soaks completed
  with **no crash** (`NRestarts=0`), app 200, WS ok. RSS: 306 -> 799 -> 1224 MB,
  settling only ~30 MB after each close.
- The same release with loop compaction **enabled** faulted in
  `hashmapContentGet` at close. Conclusion: the map is corrupted by compaction
  under live map states (`sovRebaseMap`/external headers), not by the release
  itself. **Production now runs with loop compaction and DB compaction off.**
- Per-domain reports after the soaks: main `groups=8`, `glive=8 KB`; DB
  `retained` 69 MB, `net` 382 MB; bcast `retained` 85 MB, `net` 450 MB. The
  per-domain `net` values sum to the process RSS, but they overcount resident
  memory (unmap accounting still incomplete). The per-soak growth is
  per-connection high-water that the release does not return, so either the
  release is not freeing the bulk (the connection's filters are in a nested
  group created by `loadSub`) or the growth is in another domain's root/pool.

Production is stable at ~305 MB idle (real Alby/app traffic) and survives two
soaks without crashing. Remaining leak is per-connection/subscription
high-water under synthetic churn, plus the map-rebase bug that blocks
compaction.

### 2026-11-10 - External-reference pin; compaction safe again

- Added `sovExternalRef(g)`: before compaction, scan the stack, the live fn
  arenas and the other groups' live records. If any word names g's data, the
  group is **pinned** (`escaped=true`) instead of moved. This is the missing
  rule: marking an external holder keeps its bytes, but relocation changes the
  address the holder still names.
- With that pin, loop compaction, DB compaction and the broadcast
  connection-group release can all stay enabled: two consecutive 3,000-REQ live
  soaks completed with `NRestarts=0`, app 200, WS ok. RSS still rises per soak
  (307 -> 819 -> 1317 MB) because that synthetic connection keeps 3,000
  subscriptions; the runtime no longer faults.
- Fixed a startup arena-stack bug: `SpawnDB` restored with
  `SovereignRestoreArena(RootArena())` instead of the saved arena. It now saves
  and restores `prev`.
- Measurement caveat: `/debug/sov` on a relay with a populated store reports
  ~9,000 groups at startup and `glive` ~120 MB, because `store.Open` builds
  index/file receivers (each a sovereign group). A fresh empty store reports
  ~11. Group counts on a live-store relay are therefore not a subscription
  count; the debug response itself also allocates into the Server group.
- One transient `MarkChildPipeChannel` fault occurred once, ~60 s after a
  restart with no soak; it did not repeat over the next 90 s. Suspect worker
  respawn/pipe teardown (M-09); watch for it.

Current production: compaction on both loops, broadcast release on, external
pin active. Live over 90 s real traffic: 308 -> 349 MB, no crash, app/WS/Alby
healthy.

### 2026-11-10 - The ext=2 pin was stale arena metadata, not a holder

- Instrumented `sovExternalRef`: it now reports which arena slot and which word
  offset pinned the group (`exta`, `exto`), whether the arena is the current
  frame (`extft`), its data offset (`extao`) and whether the target record is
  still live (`extrsz`, `extrl`).
- First reading settled it: `exta=3 exto=3152 extft=3 extao=3984 extrsz=32
  extrl=0` - arena slot 3 is the per-turn fn arena, its data starts at 3984,
  and the pinning word sits BEFORE that, inside the region registry. The
  registry holds `arenaMaxRegions` entries; `ArenaReset` unmaps the overflow
  regions but left their `base`/`cap` in the table, and the process later
  recycled one of those addresses as a sovereign group's data arena. A stale
  registry word therefore looked like a pointer into live data, and the top
  group was pinned forever (`comp` never advanced past 1 while `glive` grew
  ~8 KB/s on the relay).
- Fixes:
  1. `sovExternalRef` never scans an arena's header: region 0 is scanned from
     `base + arenaDataOffset()`.
  2. `ArenaReset` clears each unmapped region's registry entry, so the table
     never names an address the process handed back.
  3. Liveness now precedes the external test: `SovDrainCompactions` calls
     `sovFullMark(g)` first, then `sovExternalRef` only treats a word as a
     holder when it names a **live** record (a word naming a dropped block is
     recycled bytes and must not pin). `codecSovereignCompact` takes a `marked`
     flag so the walk is not repeated.
- Live after the fix: `ext=0`, `comp` 15 -> 31 -> 47 and climbing, the 28 MB top
  group compacted away (glive 135 KB -> 120 KB and falling), retained flat at
  34 MB. The relay stays at 200.
- Lesson for the remaining investigations: a "holder" must be a word that names
  live data. Any test that only checks an address range will find the arena's
  own bookkeeping and stale recycled words.

### 2026-11-10 - The churn crash: a dangling string key, and the map owner it exposed

- Reproduction: 3,000 REQ + CLOSE on one live connection crashed the relay with
  `SIGSEGV` inside `runtime.hashmapContentGet`. `addr2line` on the fault PC
  showed the faulting instruction was the key-byte loop (`movzbl (%r15)`), not
  the map dereference: the KEY string, not the map, was dead.
- Diagnostics added (temporary, fail-loud):
  - `hashmapContentDeadCheck` reports `HASHMAPDEAD` (dead map header) or
    `HASHMAPDEADKEY` (dead key) with the map address, key length, key pointer.
  - `SovPrintStackWords` prints return addresses only (a word whose fifth
    preceding byte is `0xE8`), because an unfiltered stack word scan prints
    recycled garbage - the same false-positive class that pinning taught us.
  - `sovExternalRef` identity fields (`exta`, `exto`, `extft`, `extao`).
- Two real defects fell out:
  1. **Map owner is not rebased by compaction.** `hashmapOwner(m)` returns
     `m.owner`, the arena the map was born in. Compaction retires that arena, so
     after a compaction every `Set` copied its key bytes into freed (then
     recycled) memory and the stored key dangled; a later lookup faulted while
     hashing it. Fixed in `sovRebaseMap` (`m.owner == g.data -> nd`) and
     additionally in `hashmapOwner`, which now validates the recorded owner
     without dereferencing it (`sovFindContainingArena` / registered sovereign
     range) and otherwise derives the arena from the header address.
  2. `ArenaReset` left the region registry naming unmapped regions (see the
     `ext=2` entry), which both pinned compaction and could mis-attribute
     storage. Clear on unmap.
- Live after the compaction fixes: `ext=0`, `comp` climbing, `glive` ~150-800 KB,
  RSS flat at ~300 MB, app 200.

### 2026-11-10 - Churn crash root cause: the X-Forwarded-For string outlived its turn

- The 3,000 REQ + CLOSE soak crashed exactly when the WebSocket connection
  closed, in `hashmapContentGet`, hashing the key of the `ipConns` decrement.
  The key was the client IP taken from X-Forwarded-For.
- `processHTTP` did this:

      if c.remoteIP == "127.0.0.1" {
          if xff := req.headers["x-forwarded-for"]; xff != "" {
              realIP := FirstXFF(xff)          // copies into the CURRENT arena
              prevR := runtime.CurrentArena()
              runtime.SovereignSetArena(runtime.RootArena())
              c.remoteIP = realIP              // stores a pointer that dies this turn
              runtime.SovereignRestoreArena(prevR)

  The root borrow only directs NEW allocations; it does not move `realIP`,
  which `FirstXFF`/`makeCopy` had already placed in the turn (or Server-group)
  arena. The connection outlives the turn, so `c.remoteIP` dangled, and every
  later use - `ConnIP`, the write limiter key, and `ipConns[remoteIP]` at close
  - hashed released memory. The crash needed a real XFF header, which is why the
  local relay (remote address 127.0.0.1, no header) never reproduced it and the
  live one did every time.
- Fix: build the copy inside the root borrow (`ipc := []byte{:len(realIP)}` +
  `copy`, then assign). Verified: the full 3,000-REQ live soak now completes with
  `NRestarts=0`, app 200, WS ok.
- Diagnostic support added while chasing it: `HASHMAPDEAD` / `HASHMAPDEADKEY`
  reports (map address, key length, key pointer) and `SovPrintStackWords`
  (return-address-only stack dump). These stay until the map/arena invariants
  have a regression test; they cost one arena-range lookup per map operation.
- `hashmapOwner` also no longer trusts a recorded owner arena blindly: a retired
  sovereign arena is unmapped, so the test must not dereference it (it uses the
  function-stack/registered-range test instead).

### Open after this pass

1. `ext=3` pins: after the soak the Server group keeps ~17 MB because another
   group holds a live 416-byte record naming its data. Name that holder group
   (`extrecv`) and the record's type, then remove the cross-group reference or
   make it rebaseable. Until then a group referenced from a connection-scoped
   group cannot compact.
2. Remove `hashmapLivenessCheck`/`hashmapContentDeadCheck` from the hot paths
   once the above is settled.
3. The 30-minute live acceptance soak, then drop `MemoryMax=3G`.

### 2026-11-10 - Root arena is a holder too; live-only test keeps compaction

- Measured with `roothits`/`rootfield`/`rootother`: the root arena holds words
  that point into the top group's data (13 at idle, 1093 under churn), and none
  of them are inside the receiver struct's own bytes. Root is not a sovereign
  group, so the external-reference test never saw them: root-allocated
  connection records carry map headers and buffers that were allocated while a
  receiver method had the group borrowed.
- `sovExternalRef` now checks the root arena as well (`sovRootLiveRef`), but
  only for a group over 1 MB and at most once per 512 attempts, because the walk
  covers the whole root region. The test is **live-only** (the word must name a
  live block), which matters: the raw hits are mostly stale words left by
  released connections and by arenas that compaction has already retired, so a
  range test would pin the group forever. A live hit sets `rootRef`, which holds
  the pin until the next walk says otherwise.
- With the live-only test, live behaviour is unchanged where it matters:
  `ext=0`, `comp` advancing (18 -> 34 -> 50), `glive` shrinking (196 -> 180 KB),
  `retained` flat at 34 MB. The stale root words cost nothing.
- Also confirmed at the loop's safe point: `fntop=3 borrows=0` - root, main, the
  loop frame and the per-turn arena, with no leaked frame or arena borrow.
- Interpretation: the relay's ownership is sound enough for compaction (nothing
  live is held only from root), and the remaining retention under churn comes
  from the *live* subscription state itself, not from unreclaimed garbage.

### 2026-11-10 - Where the remaining retention comes from

- Two consecutive 3,000-subscription soaks on the current build: both complete,
  `NRestarts=0`, app 200. RSS peaks ~450-890 MB while 3,000 subscriptions are
  live; after they close the top (Server) group keeps ~17 MB per soak, because
  compaction of that group is pinned by another group's live record.
- The pinned target names itself now (`exttd=<kind>:<size>:<first field>`): it is
  a bytes/slice record (`KindBytes`, descriptor size 24) inside the Server
  group, pointed at from a connection-scoped group. Moving it would leave that
  group naming retired memory, so the pin is correct; the fix is ownership, not
  the pin: connection-scoped state must not hold pointers into the Server's
  sovereign arena. Two concrete candidates in the relay:
  - the per-connection state arena (`c.arena`) borrowing the Server group while
    `s` is the receiver of the running method (a method on `*Server` that writes
    through a `*tconn` allocates into the Server group);
  - `dispatchToWorker` / worker reply staging that keeps a decoded view alive
    across turns.
- Planned fix (next task): move the connection-scoped objects (`tconn`, `sub`,
  their maps and buffers) out of the root arena and into the connection's own
  group, so everything that references them is in that group, released at close.
  Then the Server group has no foreign holders and compacts under churn. The
  `c.subs[""] = nil` root pin in `OnWSConnected` exists only to avoid the fault
  that this ownership move removes.

### 2026-11-11 - Byte-buffer holders were pinning compaction; and the soak slowness was the link

- `ext=3` pins came from **untyped or pointer-free records**: a word inside a
  serialized frame, a key copy or a byte buffer that happened to land in the
  Server group's data was treated as a reference. `sovExternalRef` now skips a
  holder record whose descriptor proves it holds no pointers
  (`sovTypeHasPtrs` false), keeping the conservative path for a nil descriptor.
  Effect under a 3,000-subscription soak: `ext=0`, `comp` advancing
  (236 -> 256 -> 279), `glive` 1.8-2.2 MB instead of 12-47 MB, RSS ~360 MB
  instead of 450-890 MB.
- Typed holders still pin when they exist: `exthold=22:32:1` (a 32-byte slice of
  pointers) pointing at a live 16-byte block in the Server group. That is a real
  cross-group reference from a connection-scoped group, and it is the remaining
  ownership item - not a false positive.
- Two tuning changes, both about not paying for work that cannot pay off:
  `dirtyBytes` pressure only queues a groom when the garbage is a meaningful
  share of the group (`allocBytes > liveBytes + liveBytes/4`, because the groom
  copies the whole live set); and a pin sets `sovBackoffUntil = tick + 64`, since
  the holder walk costs a sweep of every other group's live records and a pinned
  group stays pinned for a while.
- Removed the `HASHMAPDEAD`/`HASHMAPDEADKEY` per-operation checks and
  `SovPrintStackWords`: they named the crash they were built for (the XFF
  lifetime bug) and cost an arena lookup on every map operation.
- The long soaks (900 s timeouts) were **network**, not the relay: `ping` to the
  VPS measured 327-428 ms RTT at the time (it had been ~40 ms), and a REQ/EOSE
  round trip is one RTT plus work. Local relay latency on the same build: 45 ms
  per REQ against the copied live store.

### 2026-11-11 - The remaining growth is in the DB domain, and it is counted live

- The main domain is now healthy under churn (`ext=0`, `comp` advancing,
  `glive` 1-2 MB during a 300-subscription churn). The memory that climbs sits in
  the **db domain**:

      SOVDOMAIN db retained=110MB glive=72MB comp=0 pend=0 topq=0
        toplive=65640240 topalloc=66602448 topbatch=288

  `toplive` and `topalloc` differ by about 1 MB, so the Engine's group believes
  everything it ever allocated is still reachable: no dead-marking is happening
  for that receiver, `allocBytes > liveBytes` is therefore never true, and the
  pressure ratchet never queues it. `comp=0` for the life of the domain.
- Two changes landed for the DB loop and did remove a real pin (the sweep's own
  frame was holding a response into the Engine group):
  - `serveAndSend` sends the response from a void helper, so the loop frame never
    holds a returned `tree.Response` (whose query buffers point into the group);
  - `runtime.SovScanCeil()` before the drain declares the safe point, and
    `sovExternalRef` no longer scans frames above it - the sweep's own record
    tables and slices are not holders.
  With both, the db domain reports `ext=0` (it used to be `ext=2` and pinned).
- Next question for the DB leak: which receiver accumulates ~200 KB per query
  with `live == alloc`? Candidates are store fields written on the query path
  (a cache map keyed per query, a reader table, a WAL/index view). The answer is
  in `pkg/store`, not the runtime: a group whose mark finds everything live is
  either caching deliberately or never dead-marks because its methods are not
  bracketed by `codecSovereignEnter/EndCall`.

### 2026-11-11 - Why compaction cannot help the DB group

`toplive` tracks `topalloc` within ~1 MB, so the Engine's 65-275 MB is *live by
the mark*: the receiver's fields still name it. The store's `Engine` struct holds
only index `*sorted.File` handles and scalars, so the accumulating fields are in
`sorted.File` itself: `buf []byte` (the write buffer, one record per write until
the merge threshold), `del [][]byte` (deleted keys held until a merge) and
`sideBuf`. Those are genuinely reachable, so the groom is right to keep them and
wrong to be expected to free them. The next question is a write/merge policy
question in `pkg/store/sorted/sorted.mx` (when does `buf` flush, when does `del`
drain), plus whether the DB loop's `Flush` cadence matches the write rate.
A local relay on a copy of the live store, driven by 300 queries, stays at
`glive=234KB` with the group queued - the growth needs real event writes, which
is consistent with `buf` being the accumulator.

### 2026-11-11 - DB domain at 437 MB with live==alloc and skipf=0

Root cause hypothesis of the day is ruled out: `skipf=0` (no bytes field of the
Engine group references foreign memory), `ext=0`, `pend=1`, `topbatch=288`, yet
`glive` 172-177 MB equals `topalloc` within ~1 MB. The mark finds every record
reachable from the receiver, so the group is accumulating state the receiver
genuinely points at, at ~1 MB per turn (an EVENT or REQ delivery).

The relay now reports the DB domain every 64 turns instead of 256 (which never
fired under real load, so the domain grew invisibly until the OOM killer named
it). The next step is to attribute the growth to a receiver field rather than to
the group as a whole: the candidates, in order, are
- `filter.S`/`filter.F` retained anywhere reachable from the Engine or the
  Pipeline (a filter keeps ids/authors/kinds/tags slices), and
- `sorted.File.buf` / `.del` retained through the 20 index handles.

Until this is closed the service keeps `MemoryMax=3G` and restarts on OOM every
40-70 minutes.

### 2026-11-11 - What IS fixed, what is staged, what is unfixed (runtime audit)

Audit against the M-list, after the week's work. Honest status, not narrative.

**Fixed (measured on a live relay or a local build):**
- M-03 canonical release: `SovReleaseGroup` delegates to `codecSovereignFree`,
  which unqueues, aborts a cycle, frees data/record/ref arenas and removes the
  map entry. Counted by `RecordSovRelease`.
- M-04 root scope (partial): the relay no longer parses REQs under a root borrow.
  There is still no `RootScopeEnter/Exit` API, and `sovRootAlloc` still grows
  the root region forever - so a leak into root is still permanent. The relay
  needs no more than a couple of hundred KB there now, but the defect stands.
- M-07 maps under compaction: `hashmapOwner` no longer trusts a retired owner
  arena (it tests live membership without dereferencing), `sovRebaseMap` rebases
  `m.owner`, and the crash that this defect produced (`hashmapContentGet` on a
  dead key) was traced to its real cause (an X-Forwarded-For string that outlived
  its turn in the relay, fixed in transport.mx). Two consecutive 3,000-REQ soaks
  and every live soak since ran with compaction on and no fault.
- M-01 incomplete roots (largely): the mark now covers the stack, live fn arenas
  (below the safe-point ceiling), other groups' records, the root arena (live
  targets only), and compaction skips an arena's own header. `sovExternalRef`
  only pins on a word that names a LIVE block, and only when the holder record's
  descriptor says it holds pointers - so byte buffers no longer pin. Two false
  pin classes were found and removed this way: stale arena-registry entries and
  the sweep's own scratch frames (`SovScanCeil`).
- M-08 diagnostics: `extwhere` now names the holder (kind, arena slot, offset,
  target descriptor, holder descriptor, `skipf`/`roothits`/`fntop`/`borrows`),
  `toplive`/`topalloc`/`topbatch` make "we reclaim nothing" and "everything is
  live" tell apart, and the DB domain reports every 64 turns instead of 256
  (which never fired under real load).
- M-10 stack size: 16 MB (was 64 MB), still a constant at 9-15 threads.
- Reclaim primitive (M-06 groundwork): `sovTrimTail` now runs on a pinned group
  and releases empty trailing regions. It moves no live byte, so an exterior
  holder cannot be invalidated.

**Staged / not done:**
- M-02 `escaped` is still permanent and ownerless. No code sets it on the
  current paths (the stable-alias and codec-boundary sets were removed), so it
  is dead rather than fixed; `SovUnescape` exists but no caller needs it.
- M-05 mutability classification: unchanged. A method that touches a receiver
  field still owns the group, and the compiler still says nothing.
- M-06 `SovReleaseDead` does not exist. `SovTrimAll` + `sovTrimTail` is the free
  proxy: it reclaims the tail, never the interior.
- M-09 domain restart: unchanged; no safe retire-at-barrier primitive.
- M-01 residual: the checks are conservative, not complete. A live holder the
  scans do not model (a stack slot below the ceiling, an untyped record holding
  a pointer) is still a use-after-move.

**The one live defect left, and it is NOT the runtime:**
On the DB domain the receiver keeps an UNBOUNDED list of pointers.
`toplive`/`topalloc` differ by <1 MB while the group grows ~1 MB per turn, and
`glive` went 1.81 -> 2.08 -> 2.36 -> 2.63 GB on one instance (then the OOM
killer ended it) and restarts at 229 MB with `ext=0 skipf=0 comp=0`. A mark
that finds everything live on a graph this large is a container field that is
never cleared, and the only candidates are in the store (`queryEvents`,
`queryByIDs`, `resolveBlob`, `collectSerials`) or the pipeline. The runtime's
job here is to name it, which the per-field breakdown will do.

Conclusion: the runtime defects that produced crashes and permanent pins are
fixed and verified under load. The remaining runtime gaps are design gaps
(M-02/M-05/M-06/M-09 and the root-scope API), and the memory growth still killing
the relay is in morly's DB domain, not the arena system.

### 2026-11-11 - M-06, M-09, M-10 closed; the live leak is a delete backlog

**M-06 `SovReleaseDead` is implemented and running.** Three crashing first cuts
taught the constraints the hard way, and each is now part of the design:
1. a group's own arena must never be dropped (`Arena.regions` is carved inside
   it), so region 0 always survives;
2. record structs live in `g.allocTbl`, which the release retires, so released
   extents are CLONED into root-lifetime memory before anything is unmapped;
3. the walk must not allocate in the arena under test, so each record carries
   its `region` index and the region table is rebuilt from plain arrays.

The final shape reclaims in place: dead regions unmapped, dead blocks that
reach a region top released exactly, interior dead blocks zeroed - with every
window a live block, a stable slot or a pin points into left alone - and
released extents kept as root-lifetime `extra` records so a stale pointer still
answers with bounds (`sovFind` skips them, `sovMarkBlock` still marks them).

**M-09:** the `MarkChildPipeChannel` SIGSEGV was a NIL channel argument (the
runtime wrote `ch.childDomain` unconditionally, and `MarkParentPipeChannel` did
the same). Both are boundary-checked now. `releaseLastSpawn()` frees the
previous spawn's rings, control block and breaker at the start of the next
spawn, and only when that child has closed its breaker - the safe form of
"retire at a barrier".

**M-10:** `MOXIE_STACK_BYTES`, `MOXIE_STACK_BYTES_WORKER` and
`MOXIE_STACK_BYTES_MAIN` set domain stacks per role (256 KB..512 MB, refused
outside that range rather than clamped); the default stays 16 MB.

**The live leak, named.** `SovReleaseDead` reclaims nothing because the DB
group's memory is all LIVE by the mark (`toplive` = `topalloc`). The reachable
owner is `sorted.File.del`: `DeleteEvent` tombstones keys in that slice, it is
only drained by `Flush`, and `Flush` ran on a query-facing flush or a merge
threshold that mental traffic never reaches. Meanwhile `isDeleted` is a linear
scan of `s.del` per record on every scan, so the backlog made queries take
seconds - and every slow query pinned its buffers in the Engine group, which is
exactly the growth and the `slow db request ... took=2.29s` lines in both
relays' logs. Fixed by draining at `sorted.File.DelOverflow()` (32 tombstones)
in `saveEvent`, which bounds both the slice and the scan cost.

### 2026-11-11 - Incident: worker respawn-at-startup crashed the relay; rolled back

A first attempt at the worker retire-at-barrier respawned ingest workers ON
STARTUP (the age counter started past its threshold) and the process died
before it was listening, at `codecEncodeMap`, on every restart:

    DBADDR eng=...
    panic: runtime error at 0x326d9f: caught signal SIGSEGV

Four threads instead of nine: the worker domains were dying in their prologue.
The failure did not reproduce locally on the same build (local relay was
healthy), so the difference is the live worker configuration: 2 ingest, 2
blossom and 2 proxy workers, 9 domains total, all spawned through the same
`spawnRingSet` bookkeeping. That is exactly the class M-09 describes, and it is
NOT fixed yet.

What is in place now:
- `server.workerMaxRequests = 0` (retirement OFF, the counter and barrier kept
  behind a knob a test can raise);
- the nil-channel boundary checks in `MarkChildPipeChannel` and
  `MarkParentPipeChannel`;
- `releaseLastSpawn()`, which frees the previous spawn's rings only once that
  child has closed its breaker.

The live relay is back on that build: 9 threads, 200 OK, no crash since restart.
The earlier 15 restarts are this incident, not the memory leak.

Lesson for the next attempt: a respawn must be exercised against the SAME
multi-worker configuration the relay uses (2/2/2), not a single-worker local
harness, and the first respawn must happen at a barrier a test can force - a
threshold of thousands of events hides the startup case entirely.

### 2026-11-11 - The DB growth is many buffers, not one list; event cap added

The size histogram of the DB group's live records (new `bins=` field on the
SOVDOMAIN report) settles the shape:

    bins(<=16,<=32,<=64,<=128,<=512,<=4K,<=64K,>64K)
    16636 33272 0 0 0 0 49908 0        ~1.29 GB live, topalloc = toplive

So the Engine's graph holds ~50k buffers of 4 KB-64 KB (~1.1 GB) plus 50k small
records. That is per-EVENT accumulation of mid-sized buffers, all reachable -
see `issues` companion finding below - and NOT a single giant list.

Also fixed while measuring: the store had no event-size policy. The pipeline's
`MaxContent` was a DefaultConfig constant (70 KB of `content`) that no relay
ever set, and nothing bounded the whole frame. Added `ORLY_MAX_EVENT_BYTES`
(default 262144 = 256 KiB) through LimitsCfg -> dbengine.Config -> the
pipeline's content limit. That bounds what ONE request can add to the Engine
group; it runs at ~200-250 MB/min on the live relay, whose traffic is
real-world signed events, so a per-event accumulation is still taking it down.

Next: name the field. The histogram proved it is buffers of <=64 KB, so the
candidates are the index-merge/decode buffers in `sorted.File` (buf, sideBuf)
and any per-entry decode staged reslice in `wal` or the checkpoint's event
view - and the answer has to come from the source, not another histogram.

### 2026-11-11 - LEAK FIXED: `sorted.File.sideBuf` was a read-path buffer kept as a receiver field

The DB domain's growth is gone. Root cause, proven at unit level and confirmed
live:

    pkg/store/sorted/sorted.mx  type File { ... sideBuf []byte }

`readBufFile` tried to be clever - "allocate once and reuse" - but:

1. the buffer must grow with the sidecar, and `mxutil.Ensure` allocates the new,
   larger buffer while the field drops the old one;
2. the field (`sideBuf`) is a receiver field, and its old buffer is a live record
   of the File's sovereign group as long as anything in the group names it -
   which the successor does, because the superseded buffer is what the previous
   `ReadAt` wrote into;
3. nothing ever resets `sideBuf`. `Flush` sets `bufCount = 0` (so `readBufFile`
   returns early next time) but leaves the field; `Clear` and `SkipFlush` leave
   it too.

So every merge left one more multi-KB buffer alive with no reset to bound it.
Measured in a package test: `cap(sideBuf) == len == bufCount*recLen` grew by
exactly one record per event, 640 -> 8320 over 260 events, and `toplive` stayed
equal to `topalloc` - which is why `SovReleaseDead` freed nothing: the memory was
not garbage, it was the field naming it.

Fix: `readBufFile` is a free function that sizes exactly and returns a frame
buffer; the receiver field is gone. A frame buffer cannot leak.

Live result (same traffic that had reached 1.29 GB):

    before: glive=1287162432  bins(<=16..>64K)=16636 33272 0 0 0 0 49908 0
    after:  glive=148016      bins=947 1146 474 0 0 0 1 0   topalloc=93456
    RSS 330 MB flat, NRestarts=0, app 200

The 4-64 KB bucket (28,041 records -> 1) was the sideBuf copies; the 16-32 B
tail was the runtime's `extra` records for them.

Also from the same investigation: `grapevine.mx:65-69` and `GetFollows`
recompute `len(out)` inside their push loop, so the work is quadratic in the
number of pubkeys. Ordinary defect, not the leak; recorded for the relay queue.

### 2026-11-11 - `pkg/relay/server` tests crash before any of this work

`moxie test ./pkg/relay/server` dies in `TestNewWiresPools` with a SIGSEGV
before any test body matters, and it does so with `pkg/relay/server` stashed
(i.e. against the untouched tree). The faulting address lands in
`moxie.(*BigInt32).EncodeTo` / `(*BigUint64).EncodeTo` depending on layout, so
the report is the trampoline, not the culprit. This blocks M-09's in-package
test harness and is a separate defect: the server package's tests cannot run at
all, so every "tests pass" claim about that package this week was unverifiable.
Not fixed here.

### 2026-11-11 - PENDING: the sovereign loop-declaration rule fires on helper-shaped code

CORRECTION of an earlier entry in this file that claimed "the stage4 classifier
treats e.curFunc as sovereign when it is not". That claim was wrong and is
withdrawn. The gate reads:

    checkSovereignLoopDecls (ir_scope.mx:514)
      only examines functions with a receiver (fd.Recv == nil -> continue)
      and uses the same predicate as the sovereign prologue

`(*irEmitter).noteSovereignMutating` has receiver `e *irEmitter`, and irEmitter
holds seqs/buf/operandBuf/spawnWrappers that e.w() writes throughout: it IS the
kind of type the rule exists for. The classifier is behaving as designed.

The real defect is narrower:
1. The rule forbids a `:=` (or a var decl) anywhere inside a loop body of ANY
   sovereign method, including a one-line helper doing `x := vec[:]` - an
   idiomatic, harmless scalar/slice binding that allocates one header per
   iteration. The ban on struct literals and slice literals per iteration is
   justified (they land in the sovereign arena); a `:=` of a computed scalar is
   not.
2. Its message ("move the loop scratch into a helper function") is unactionable
   when the offending function IS the helper.
3. Consequence seen in practice: a legacy-built gen1 cannot compile the tree
   (gen2 fails with hundreds of these across ir_*.mx), so the stage4 self-host
   (wood law check) is unverifiable on the current sources.

To do:
- Decide the rule precisely: reject aggregate literals and pointer-bearing
  composite allocations inside a sovereign loop; allow `:=` whose initialiser is
  a call or a scalar expression (the value can still be hoisted, and the
  interp/codegen already handle per-iteration scalars).
- Mirror the decision in legacy/ (wood law) and add a regression test with a
  sovereign method that has `x := f()` inside a loop (must compile) and one that
  has `s := &T{}` inside a loop (must still fail).
- Then run the bootstrap chain and require stage2 == stage3 (or stage3 ==
  stage4).

### 2026-11-11 - Acceptance soak: 30 minutes, zero restarts, zero faults

Removed the crashing call first: `runtime.SovReleaseDead(engine)` is gone from
the DB loop (it faulted live in `sovWalkValue1` and reclaims nothing there - the
Engine's live set is what the store actually holds). Also hardened the one
unchecked dereference in that walk: `sovWalkValue1`'s slot lookup now verifies the
slot really lies in `g.refTbl` before reading it, instead of trusting a slot
pointer the release pass may have retired.

Soak (`/tmp/accept.sh`, one sample every ~33s, 30 minutes, live relay under real
traffic):

    samples            53, one every 33s across 13:12-13:41
    app status         200 on every sample (the one "non-200" line is the final
                       SOAK DONE marker, not a failure)
    NRestarts          0 for the whole run (single value in the sample column)
    panics / SIGSEGVs  0 in the journal for the whole window
    RSS                312 MB -> 452 MB (330-490 MB band today; the climb is
                       real traffic on a relay that also serves the app, not a
                       leak: the DB domain's live set stayed in the 1-2 MB band)
    db domain glive    ~0.97-1.8 MB, oscillating, no trend
    threads            9, unchanged

That is the acceptance criterion the `MemoryMax=3G` guard was installed for. It
was NOT removed in this pass: with the crash fixed and memory bounded, removing
it is a policy decision for the operator, and the next 24h of RSS is the evidence
that should back it.

Still open and unrelated to this run: M-09 (retirement disabled), the stage4
sovereign-classifier bug recorded above, and the pre-existing `pkg/relay/server`
test crash.

## 9. Closure plan (2026-11-11)

Everything still open in this document, ordered so each item unblocks the next.
Each item states the problem, what will be attempted, and its acceptance
criteria. Conclusions are appended here as the work proceeds: either `DONE`
(with the measurement that proves it), or `STALLED` (with the concrete
condition, so the next pass does not re-derive it).

### P-01 Fault reports name the right address

**Problem.** `signal_handler` derives the faulting instruction pointer and the
runtime prints it, but the C side declared and called the handler with a single
address and the comment labelled it with `si_addr`'s meaning. Diagnostics are
the whole evidence base here, and every "the fault is in X" conclusion in this
file rests on that number resolving to a function (M-08: instrumentation that
names the wrong owner wastes rounds).

**Attempt.** Pass both: instruction pointer (resolves to a frame) and
`si_addr` (the operand), print them distinctly, rebuild.

**Acceptance.** A SIGSEGV triggered deliberately in a compiled binary prints an
instruction pointer whose `addr2line` lands in the function that faulted and on
the line that faulted, and the `si_addr` value is separately visible.

### P-02 The stage4/legacy sovereign loop-declaration rule is wrong, and the bootstrap is unverifiable until it is not

**Problem.** The rule rejects any `:=` inside a loop of a sovereign method,
including a harmless per-iteration scalar, so a legacy-built gen1 cannot
compile the tree (gen2 fails with hundreds of these across `ir_*.mx`). The
stage4 self-host - the wood-law check - has therefore not been verified on the
current sources. This is a compiler defect of the "incoherence at insufficient
resolution" kind: it looks like a safety rule and behaves like a wall.

**Attempt.** Classify the initialiser: reject aggregate literals (`T{}`, `[]T{}`,
`&T{}`) and pointer-bearing composite allocations inside a sovereign loop body
(those land in the sovereign arena per iteration and persist); allow `:=`
whose initialiser is a call or a scalar/binding expression. Classify the
same way in legacy and stage4; make the message name the actual construct.
Then run the bootstrap chain.

**Acceptance.** A sovereign method with `x := f()` inside a loop compiles; one
with `s := &T{}` or `l := []T{}` inside a loop still fails, with a message that
names the construct and the rule; and `stage2 == stage3` (or `stage3 == stage4`)
on the current sources.

### P-03 `pkg/relay/server` tests run

**Problem.** `moxie test ./pkg/relay/server` dies in `TestNewWiresPools` before
any test body runs, with and without the working tree changes; the reported
symbol changes every build because the fault address was mis-attributed
(P-01). Every "tests pass" claim about that package is unverifiable, and it
blocks any in-package harness for M-09.

**Attempt.** Use the P-01 report to get a real frame, then follow it upstream
(no guard: the test binary spawns real domains from a harness thread, so the
candidates are domain init, spawn bookkeeping, or a test-harness-only path).
Run the spawn path with one domain at a time to find the first count that
faults.

**Acceptance.** `moxie test ./pkg/relay/server` runs the whole package suite to
completion with zero crashes.

### P-04 M-09: a worker can be retired and replaced

**Problem.** Retirement is disabled (`workerMaxRequests = 0`) because the first
attempt respawned workers at startup and died in the new domain's prologue
(`codecEncodeMap`) on the live 9-domain configuration. The primitive -
retire at a barrier, renew, continue - does not exist.

**Attempt.** With P-02/P-03 giving a buildable, testable tree: reproduce in the
package test at the 2/2/2 configuration the relay uses, with the first
retirement at a barrier a test forces (not after thousands of events, which
hides the startup case). Fix what that shows; then re-enable with a budget and
run the acceptance soak.

**Acceptance.** A package test retires and replaces every worker role
(ingest/proxy/blossom/broadcast) at a forced barrier, repeatedly, with zero
faults; then the live relay with retirement enabled keeps `NRestarts=0` and
app 200 through the 30-minute soak with worker generations advancing.

### P-05 M-02/M-05: the group writer is visible, and escape is released

**Problem.** M-05: a method that calls anything on a receiver field silently
owns the group (`QueryEvents` -> `e.flushPending()`, `sorted.File.Get` ->
`s.f.Read`, and the `sideBuf` leak that this document's biggest entry traces).
There is no signal to the author. M-02: `escaped` is a permanent, ownerless
boolean; `SovUnescape` exists but nothing needs it because nothing sets it -
dead, not fixed.

**Attempt.** M-05 first, since it is the compiler-side defect with a proven
cost: emit a note per method classification and a warning where a mutating
method allocates in its group (behind the `-no-sov-notes` flag already added,
legacy first, stage4 mirrored). M-02 second: do not invent an escape-holder
registry for a flag with no setter - either delete it and `SovUnescape`
(the dead-reference rule) or keep it with a test that proves the invariant.
Record which, with evidence.

**Acceptance.** M-05: the classification note appears for a method that borrows
through a field call and not for a read-only one; both compilers emit it for
the same sources. M-02: `escaped` is either gone or has a passing test that
sets, observes and clears it - no flag exists that no code can set.

### P-06 M-04 root scope: prove or bound it

**Problem.** `SovereignSetArena(RootArena())` makes every following allocation
permanent; `sovRootAlloc` grows the root region forever. `RootScopeEnter/Exit`
was tried and removed because the exit-time trim crashed under the pipe-channel
mark. The relay no longer needs root for request state, so the defect is
latent, not absent.

**Attempt.** Measure the root arena's live bytes on a 30-minute soak. If it is
bounded (the relay allocates nothing unbounded into root), record the evidence
and define the contract: root is for domain-lifetime state only, and the scope
API is refused rather than half-implemented. If it grows, make the scope trim
queue for compaction at a safe point instead of trimming in place.

**Acceptance.** A stated root contract backed by numbers (root live bytes over
the soak), and either a working `RootScopeEnter/Exit` test or a recorded
decision with its evidence.

### P-07 M-08: the remaining untrustworthy counters

**Problem.** Two specific M-08 items are still open: per-domain `net` overcounts
resident memory (it summed to more than RSS), and per-group totals dedup by
first region base so a partially moved group can be missed. The rest of M-08
(holder naming, `toplive`/`topalloc`, report cadence) is done.

**Attempt.** Account unmaps in `sysmap` net; key per-group totals by receiver
address only. Remove the temporary diagnostics that have served their purpose
(`bins=`, `big=`) so the report stays readable.

**Acceptance.** On a running relay, the sum of per-domain `net` is within 10% of
process RSS at idle and under load, and the top group reported can be matched
to a live receiver address on request.

### P-08 Hygiene: no scratch left in the tree

**Problem.** `morly/spawnprobe/` is a scratch probe with wrong budgets; a
probe left in the tree becomes a false reference for the next pass.

**Attempt.** Delete it; confirm the module still builds.

**Acceptance.** `morly` builds with no probe package present, and no file under
`morly/` exists only to reproduce a closed investigation.

### P-09 Acceptance: a fresh build, both compilers, the live relay

**Problem.** Reporting completion requires a test on a fresh build (CLAUDE.md),
and the last acceptance soak predates this pass's changes.

**Attempt.** Bake the runtime, rebuild `morly`, run the bootstrap fixpoint
(stage2 == stage3 or stage3 == stage4), deploy, run the 30-minute soak.

**Acceptance.** Fixpoint holds; live relay `NRestarts=0`, app 200, 0 SIGSEGVs,
RSS with no positive trend, for 30 minutes, on a binary built in this pass.

### 2026-11-11 - `pkg/relay/server` tests: crash confirmed pre-existing, cause narrowed

Re-confirmed on a stashed working tree (only the committed sources):
`moxie test ./pkg/relay/server` dies in `TestNewWiresPools` at 0x323927,
reported inside `moxie.(*BigUint16).EncodeTo` - like the earlier sightings
(`BigInt32.EncodeTo`, `BigUint64.EncodeTo`, `io.(*teeReader).Read`), which are
different functions each build. The variance is the signature of a symbolication
artifact, not of several bugs: the panic handler's fault PC is not resolving to
the real frame.

What is established:
- the crash happens DURING TestNewWiresPools, which calls `New(db, c)` -
  i.e. the test server spawns real domains (ingest 1, proxy 1, blossom 1,
  broadcast) from a TEST BINARY;
- it happens with and without the working-tree changes to pkg/relay/server;
- the package's other tests never run, so the suite is unusable.

Leading hypothesis: spawning worker domains from the test harness is what
fails (the test process has a different init/stack state than `morly relay`,
which spawns the same nine domains and runs fine in production - and now in the
30-minute soak). Next step is to run the spawn path under the test binary with
one domain at a time to find the first count that faults, and to confirm whether
the test runner's own domain (the harness thread) is the difference.

NOT fixed. It blocks M-09's in-package test, so it is the gate for that work.

### P-01 DONE - the fault report prints the instruction, not the operand

**What was wrong.** `signal_handler` read the instruction pointer into a local
called `addr`, declared and called `moxie_handle_fatal_signal(sig, addr)`, and
the Moxie side printed it after "panic: runtime error at ". Every reader of the
file - including three notes in this document - took that number to be the
instruction pointer and fed it to `addr2line`; the C comment next to the read
said "TODO: print the invalid address", so the code itself was ambiguous about
which of the two numbers it was passing.

**Test that settles it.** A deliberate nil dereference (`readPtr` in a hand
written `.S`, called from `main`), built with this runtime:

    panic: runtime error at 0x000000000023c478: caught signal SIGSEGV
    $ addr2line -f -e segv2 0x23c478
    main.readPtr
    $ gdb -batch -ex run -ex bt segv2
    #0  0x000000000023c478 in main[readPtr] ()

Printed address and gdb's frame agree. That is the acceptance criterion.

**What was tried and abandoned, and why** (so the next pass does not repeat it):

1. Passing `si_addr` alongside. `si_addr` never resolves; printing it next to a
   code address is exactly how this confusion spread. Reverted.
2. A frame-pointer walk. This runtime emits no frame pointers on amd64
   (measured: RBP holds a program value, `0x1`, on the faulting thread, and
   `-fomit-frame-pointer` is the default at -O2), so there is no chain to walk.
3. A conservative stack scan for return addresses. It works (it found
   `main.main`'s call site in the probe) but it is pattern matching: the
   "immediately after a call opcode" test matches data as often as code in
   these binaries, and the crash report showed why that is fragile - a dead
   byte pattern in rodata (`0x200200`) passed the range test, while the real
   return address was rejected by the count guard. Not worth a heuristic in the
   one code path that must never fail.
4. Printed from the handler straight to fd 2 to survive the print-lock the
   faulting thread may hold. Raw writes from inside the fatal handler stopped
   the handler from reaching the runtime at all on this build, which is the
   opposite of the goal. Reverted; the runtime's own report is the report.

**Kept: the alternate signal stack.** `signal_handler` now runs on a static
1 MB `sigaltstack` (`SA_ONSTACK`), so a stack-overflow SIGSEGV is reported
instead of silently killing the process, and the report never runs on the
faulting stack. This also means a nested fault inside the handler aborts the
kernel immediately - which is what our own re-raise depends on.

**Remaining limit, stated plainly.** When the faulting function is a leaf that
-O2 inlined into its caller, the instruction pointer lands inside the inlined
caller and `addr2line` names the caller - which is the right answer at frame
granularity. Symbol-level identity is now trustworthy; line-level is not.

### P-02 (part 1) - the rule now tests construction, and the tree fails it for real

**The finding.** Narrowing the rule did not remove the flags; it made them
true. With the rule keyed on constructed aggregates, `_mxc_stage4` still fails to
compile with 56 of them (55 in `ssa_builder.mx`, 1 in `ir_iface.mx`), and they
are genuine sovereign-arena writes: a struct or map node built per loop pass and
stored in the receiver (`pb.pkg.Members[...]`, `fb.blocks`, a filter's seen-set
map). The old blanket rule had been refusing those too - along with everything
else - which is why the message said "move the loop scratch into a helper
function" to functions that WERE the helper.

**Legacy, verified both ways** (fixture `/tmp/sovfix`, built with
`legacy/moxie-purego`):

    allowed: x := i * 2            (scalar binding)
             s.a = i; w.n = s.a     (struct without literal)
             w.buf[:]               (view of own data)
             g := &Node{}; push(list, g)   (a node being built)
    refused: b := []byte{byte(i)}  -> "':=' of a slice literal ..."
             s := point{x: i}       -> "':=' of a struct literal ..."
             m := map[string]bool{} -> "':=' of a map literal ..."

That last allowed case is the one this document's own PENDING entry was about:
a helper-shaped loop that constructs the things the method returns.

**Stage4 mirror** is written (`sovLoopAllocates` + `sovLoopLiteralKind` +
`sovAggregateKind` in `ir_scope.mx`), matching messages included. It cannot be
compiled until `_mxc_stage4` itself compiles under the rule, and that needs the
56 sites converted - one per loop, hoisting the node out or splitting the
algorithm into a free function. That work is large and mechanical; it is the
remaining half of P-02, and the bootstrap fixpoint cannot be checked until it is
done.

### P-02 (part 2) STALLED at the tree conversion - conclusion and what blocks the rest

**Progress this pass, verified on a fresh legacy build** (`legacy/moxie-purego`,
rebuilt by `legacy/build-purego.sh`):

- The rule now names what it refuses: `':=' of a slice literal ...`,
  `':=' of a map literal ...`, `':=' of a struct literal ...`, and the same for
  a `var` declaration. The message states the construct, not a rule id.
- Allowed: scalars, structs without a literal, a view of data the frame already
  owns (`v := w.buf[:]`), and a node built and pushed into the receiver. So the
  helper-shaped code this
  document's PENDING entry was written about - a loop that constructs the nodes
  the method returns - now compiles and runs (fixture `/tmp/sovmk`,
  `n := mkNode(nm, i)` pushed into a slice: prints `2 1`).
- Refused, verified: `[]byte{...}`, `point{x: i}`, `map[string]bool{}`.

**Where it stops.** `_mxc_stage4` fails its own rule at 56 loop bodies and
always has: 1 map literal in `ir_iface.mx`, 55 struct-literal nodes in
`ssa_builder.mx` (`SSAGlobal`, `SSAParameter`, `SSAExtract`, `SSAConst`,
`SSABinOp`, `SSATypeAssert`, `SSAFieldAddr`, `SSAUnOp`, `SSAMakeInterface`,
`SSAIndexAddr`, `SSAFreeVar`, `SSAInvoke`, `SSAIf`, `SSARootLoad`, ...). The
previous blanket rule refused them along with everything else, which hid the
fact that the tail of the problem is real.

**The fix pattern works, and is two lines:**

    func newSSAExtract(tuple SSAValue, index int32) (e *SSAExtract) {
        return &SSAExtract{Tuple: tuple, Index: index}
    }

    ext := newSSAExtract(val, i)   // node built in the helper's frame, returned

The return protocol relocates it into the caller, so it lands in the same arena
a hand-written literal does. Applied to `registerVar` (`newSSAGlobal`) and the
parameter list (`newSSAParameter`) this pass; both patterns verified in a
standalone fixture that compiles and runs.

**Why it is not finished here.** 55 sites remain, each needing its field set
read and threaded through a constructor - and `_mxc_stage4` cannot be compiled
to test the result until all 55 are converted (the compiler rejects its own
source). That is a full pass of its own; doing it blind in this one would leave
the compiler unbuildable with no verification, which is worse than an honest
stop.

**What this blocks.** The bootstrap fixpoint (`stage2 == stage3`) cannot run
until `_mxc_stage4` builds, and P-03 (the `pkg/relay/server` test crash) and
P-04 (M-09 worker retirement) both need a buildable stage4 to be tested
through. The legacy half of the rule is verified and correct; the mirror is
written but not yet exercised.

**Next step, precisely.** Convert `ssa_builder.mx`'s node literals to
constructors (15 distinct types, the pattern above), convert
`ir_iface.mx:1432` (done: hoisted and cleared), rebuild stage4 with legacy,
confirm zero rule errors, then run `tests/phase2_selfhost.sh` for the
fixpoint.

### P-08 DONE - the scratch probe is gone

`morly/spawnprobe/` deleted (it was the 9-domain probe with the wrong 512 KiB
budgets, kept past its investigation). Nothing referenced it; `make build` in
morly completes on the current toolchain (`-> morly`, 8.3 MB). No file under
`morly/` now exists only to reproduce a closed investigation.

### P-03/P-04 (part 1) - the live fault is start-up spawn, and the deferred release is in the way

**The live relay has been crashing all morning, and the report was being read
wrong.** Journal, 06:28-10:02 (UTC), five SIGSEGVs:

    08:44:30  listening on 127.0.0.1:3335
    08:44:31  panic: runtime error at 0x3411ff: caught signal SIGSEGV
    10:02:31  panic: runtime error at 0x315ff5: caught signal SIGSEGV

    0x3411ff -> runtime.spawnDomain
    0x315ff5 -> runtime.ArenaAllocTD

`0x3411ff` lands in `spawnDomain`, and the fault follows "listening" by one
second: the relay dies WHILE SPAWNING ITS WORKER DOMAINS, not under load. That
is the same start-up path that killed the retirement experiment on 10-11 and the
one `pkg/relay/server`'s `TestNewWiresPools` dies on - one defect, three
reports.

**Cause found in the runtime.** `spawnDomain` called `releaseLastSpawn()`
mid-function to retire the previous spawn's rings. That function reads
`spawnCtlRing`, `spawnRingSet` and `spawnBreaker` - which are PROCESS-WIDE
statics - so:
- after `spawnCtlRing = ringCreate(4096)` on the 2nd spawn, it sees the NEW
  control ring, concludes the previous child is gone, and destroys the ring the
  new domain is about to be handed;
- the 3rd spawn would pass the same test using the 2nd spawn's state.

It also waited for "is the previous child's breaker still open", which from a
manager that has just closed a worker is a synchronous wait on another domain's
teardown from inside a spawn - the deadlock the 10-11 "retire at barrier"
experiment was really about.

**Changes, verified on a fresh runtime bake:**

1. The retirement record is now its own state (`lastSpawnCtlRing`,
   `lastSpawnRings`, `lastSpawnBreaker`, `lastSpawnRingCount`), written by
   `recordLastSpawn` - it cannot alias live spawn state.
2. `releaseLastSpawn` is no longer called from `spawnDomain`. A fixture that
   spawns 200 worker domains in sequence (`/tmp/spawntest`) DEADLOCKED with the
   automatic call (prints `total 6` instead of `total 80800`) and completes with
   it removed. The lesson is the barrier's real shape: a manager cannot await a
   child's teardown from inside a spawn; the release is the OWNER's call, after
   that worker's breaker closed.
3. After the 200-spawn run: `Threads: 1`, `VmRSS: 6.9 MB`. Spawn threads and
   rings are not leaking.

**What this means for M-09.** The start-up crash and the `pkg/relay/server`
test crash are the same event and are now expected to disappear with the runtime
bake. Verification of both needs a fresh `morly` build and a package test run,
which needs the runtime change deployed; the runtime is baked but the relay
running on the VPS is the older binary. Nothing was deployed from this pass.

### P-03 (part 2) - the `pkg/relay/server` crash survives the runtime fix; STALLED on symbolisation

Rebuilt `morly` against the freshly baked runtime and ran the package suite:

    moxie test ./pkg/relay/server
    === RUN   TestNewWiresPools
    panic: runtime error at 0x000000000031052c: caught signal SIGSEGV

So it is NOT the `releaseLastSpawn` defect (that one is fixed and no longer
runs inside `spawnDomain`). The report is now trustworthy as a NUMBER but the
number cannot be named: `moxie test` builds into a temp directory and deletes it
on VC and on exit (`cleanupScratch`), the test binary is not kept by `-o` (the
`test` command does not take the global `-o`), and `-work` did not leave a
directory this pass. Five attempts (temp-dir retention, `TMPDIR`, a `/proc`
watcher, `-o`, `-c`) produced no symbol.

**Next step, precise:** make `moxie test -work` keep the temp directory (it
should - the flag exists for exactly this), then
`addr2line -f -e <dir>/.../server.test 0x31052c`. One line of output names the
function, and the M-09 test gate opens.

### P-04 / P-09 - what changed, what did not

Changed and baked into `sysroot/runtime_go.bc` this pass: the spawn retirement
record (P-03 part 1), the fault reporter (P-01), the alternate signal stack.
`morly` rebuilt against that runtime builds and links.

NOT done: the bootstrap fixpoint (blocked by P-02's 55 remaining stage4 loop
sites - `_mxc_stage4` does not compile under its own rule, so `stage2 == stage3`
cannot be measured), the deployed relay is unchanged, and no live soak ran.
The live relay's own start-up crash (`spawnDomain`, five times this morning)
is expected to be fixed by the spawn change, but that is a claim for the next
pass to verify on a deployed build - not something this pass measured.

### P-05 / P-06 / P-07 - status at the end of this pass

**P-05 (M-05, M-02): not attempted beyond what already exists.** The
classification note (`noteSovereignMutating`, `-no-sov-notes`) has been in both
compilers since the earlier pass and is visible in every build this pass ran:
`_mxc_stage4` emits a note per mutating method. The "warn where a mutating
method allocates in its group" half was not written. M-02 (`escaped`) was not
touched: nothing sets it on the current paths, so it is dead code, and the
decision to delete it or test it is still open.

**P-06 (M-04, root scope): measured evidence recorded, no API written.** The
live reports show root is not growing: `roothits` moves (4844 -> 4976 over ten
minutes) while `rootfield=0`, and `retained` in the main domain sits at 43-45 MB
with `glive` 1.0-1.3 MB throughout. The contract that fits that evidence is:
root holds domain-lifetime state only, the relay allocates nothing per-request
into it, and a scope API would be a primitive with no caller. Implementing one
would be an abstraction without a use; the defect stands as a latent one and the
contract is recorded here instead.

**P-07 (M-08): not attempted.** `net` still overcounts (the live report shows
`net=281-375 MB` against a process RSS of 530 MB, summed over all domains), and
the per-group dedup by first region base is unchanged. The diagnostics that
served their purpose are still there: `bins=`, `big=`, `skipf=` cost nothing at
info level and were the tool that found the `sideBuf` leak, so removing them is
a separate decision, not a cleanup this pass should force.

### Where the closure plan stands

| item | status |
| --- | --- |
| P-01 reporter | DONE - printed address resolves to gdb's frame |
| P-02 loop rule | PARTIAL - rule fixed and verified in legacy; stage4 mirror written; 55 tree sites unconverted, so `_mxc_stage4` does not compile under it |
| P-03 server tests | STALLED - crash confirmed on the new runtime at a real address, not symbolisable because the test binary is deleted |
| P-04 M-09 | PARTIAL - the start-up spawn defect is found and fixed (record separated, no auto-release inside spawn, 200-spawn fixture passes); retirement/replacement itself is untested |
| P-05 M-05/M-02 | NOT DONE - note exists from the earlier pass; no allocator warning; M-02 decision open |
| P-06 M-04 | NOT DONE - root measured as bounded; contract recorded, no API |
| P-07 M-08 | NOT DONE |
| P-08 hygiene | DONE - `morly/spawnprobe` removed, morly builds |
| P-09 acceptance | NOT DONE - fixpoint blocked by P-02; nothing deployed; no soak |

Nothing was deployed this pass. The live relay still runs the 10:02 UTC binary.

### 2026-11-11 - M-01 measured: the descriptor-less hole exists, is armed in a fixture, and is NOT armed on the relay

The hypothesis was that compaction roots are incomplete because a pointer and
the value it names are allocated into different areas. Measured rather than
argued, with two new report fields.

**The mechanism, confirmed in code.** `sovMarkBlock` marks the record it lands
in and follows its children ONLY when `rec.typeDesc != nil`. `sovAddRec` gets
that descriptor from the caller of `ArenaAllocTD`/`alloc`, and where it is nil
the record is a black hole in the mark: everything reachable only through it is
counted dead and the copy pass frees it.

**The hole is real and it is armed.** `nodesc=` (live records of the group with
no descriptor) and `nodescptr=` (of those, the ones holding a word naming LIVE
data of their OWN group - the armed form) were added. Fixture `/tmp/sovarm`: a
`rook` built by a free constructor whose `child *pawn` was built the same way,
so no typed record names the pawn:

    nodesc=1 nodescb=4096 nodescptr=1      and  RESULT bad 0 of 300

The pawn survives because the mark's OTHER root paths (the external scan, the
borrow, the frame map) reach it, not because the walk followed it. That is why
this hole has never produced the crash on its own: it needs a pawn nothing else
names.

**The relay does not have it.** Local relay, idle and under 30 accepted events
plus queries, both domains, every group that compacted (comp 16 -> 31):

    SOVDOMAIN main ... comp=31 ... nodesc=0 nodescb=0 nodescptr=0

Generated store-backs are typed. So M-01's remaining risk on the relay is NOT
descriptor-less records; it is the root paths (frames, other groups, root) that
the scans do not model, exactly as the audit entry of 2026-11-11 says.

**What the fixtures also show.** 400 turns of dead scratch on a sovereign
receiver, 22 compactions: every pointer link, every field pointer and every
slice reached through a pointer stayed correct and `glive` stayed flat at
40 KB (`/tmp/sovmark`). Compaction on a well-typed graph is not losing data.

Conclusion: M-01 stays open, narrowed - the descriptor-less hole is a latent
defect with a counter that names it and a fixture that arms it, and the relay
needs neither today. The next M-01 step is a fixture whose pawn is named ONLY
by an untyped record (no typed parent slice), which is the case that should
fail today and must be made to pass by making the compiler pass a descriptor
for constructor-built aggregates.

### 2026-11-11 - C-11 A pointer taken from a container and returned across a method boundary does not alias the stored object

Found while chasing why the cordn messaging interop never delivered a
coordinator's answer. Minimal reproduction, no relay, no browser:

    type box struct{ subs map[string]int32 }
    func (b *box) add(k string, v int32) { b.subs[k] = v }
    type pool struct{ conns map[string]*box }

    func (p *pool) connect(url string) (b *box) {
        if existing, ok := p.conns[url]; ok { return existing }
        b = &box{subs: map[string]int32{}}
        p.conns[url] = b
        return b
    }

    a := p.connect("u"); a.add("a", 1)
    b := p.connect("u")
    // a == b   is FALSE
    // p.conns["u"].subs["a"]  is 0   - the write went to a copy
    // b.add("b", 2); p.conns["u"].subs["b"] is 0 - also lost

Measured variants, same program shape:

    map[string]T  + method returns T   -> CORRECT (both writes land)
    map[string]*T + method returns *T  -> BROKEN  (a==b false, writes lost)
    map[string]*T + method call ON the map expression (no local) -> correct
    map[string]*T + read into a local, then call on the local      -> broken

So maps and slices hold values correctly, exactly as in Go. The defect is
narrow and it is the pointer: a `*T` obtained from a container and carried
across a method (return value, or a local that outlives the read) is
relocated, and a method called through it mutates the relocated copy.

Every variant that failed involved a pointer to a struct type; the value
variants are unaffected.

**What it broke.** `web/common/relay`'s `Pool.conns` is `map[string]*Conn`
and `Connect` returns the pointer. Every `Subscribe` after the first was
therefore applied to a copy of the connection, so `Conn.subs` on the object
the socket callbacks reach never grew past its first entry. The relay proxy
sent correct REQs, the relay accepted them and sent EOSE, and every live
event for those subscriptions was dispatched to a Conn that did not hold
them. For the cordn interop that meant: the app publishes `kp_publish`, the
coordinator answers on kind 25910, the answer is delivered to the browser and
dropped, the worker waits for a reply that never reaches it, and cordn's
admin sees `requests=0` because `join_store` is only sent after that reply.

**The architectural answer (from mleku).** Subscriptions are not the right
type: they should be items in a queue owned by a worker or worker group,
addressed by key, so no pointer to a sovereign receiver ever crosses a frame
boundary. That is also the pattern the measured variants say is safe.

**Scope.** `web/wasm/relay-proxy/main.mx` is the only consumer of the pool
(`Connect` once, `AllConns` seven times), so the restructure is contained.

Still open: the underlying codegen defect itself. Until it is fixed, a
`map[string]*T` plus a method that returns `*T` is a trap anywhere in the
tree, and the reproduction above is the regression test it needs.

## 10. C-12: an object holding a callback is deep-copied on every return

**Resolved 2026-11-11. This was the cordn interop blocker.**

**Symptom.** No cordn tool call was ever answered. The relay proxy published
nothing at all: `Publish` saw `state=0` while the socket reported `ready=1`, so
every EVENT was queued into `pendingSend` and never written, and the app sat on
"waiting to be added to the group" while cordn polled `requests=0` for two and a
half minutes.

**Measurement.** Pointer identity, printed from the proxy itself:

    Publish ws://127.0.0.1:23334 me=67510896 map=67288448 kind=25910 state=0
    OPEN    resolved=67334640     map=67288448 same=0

Two different objects behind one registry entry: `wsOnOpen` wrote `state=1` to
one, `Publish` read `0` from the other. The map's own entry never changed
address.

**Root cause, measured.** `retPointeeDies` walks a pointee's fields to decide
whether `codecRetStable` may alias a returned pointer. It grouped `KindFunc`
with the `KindMap`/`KindChan` heads and asked `codecIsStableAddr` about the
field's first word - but a Moxie function value is a CODE address, the text
segment, never an arena, so the answer was always "not stable" and therefore
"dies". A struct holding any non-nil callback was declared dying, the fast path
refused, and the whole object was encoded and decoded - deep-copied - on every
return. `Conn` carries `onEvent`/`onEOSE`/`ScheduleReconnect`, so once
`wireConn` installed them every `lookup` and every `Pool.Connect` handed back a
fresh copy.

Diagnosed by naming the dying field rather than guessing:

    codec-die] field=11 kind=22      (40x, the only field)

Field 11 is `pendingSend []string`; kind 22 is slice.

**The code fixes - all three on the caller side, none in the runtime.**
1. `web/common/relay/conn.mx` `queueSend`: the `push` that grows `pendingSend`
   ran AFTER the root-arena switch was restored, so the slice backing array
   landed in the caller's frame and died with it. It now grows inside the root
   arena. **This was the trigger**: the diagnostic named it (field 11, slice)
   and fixing it alone made every `Conn` return alias again.
2. `web/common/relay/conn.mx` `Dial`: `URL: url` stored the caller's frame
   string in a root-arena object; it is now copied into the root arena
   (`keepString`). Field 0, same class of defect, found while narrowing.
3. `wsRegistry.lookup` was a MUTATING method - `r.pending = nil` inside it - and
   a mutating method always returns through the full encode/decode protocol, so
   the result was a copy regardless of how stable the pointee was. Adoption
   moved to `adopt`, which returns nothing; the callbacks do lookup, then adopt
   and re-read on a miss. `Pool.Connect` was already fixed the same way.

**A fourth hypothesis, measured and rejected.** `retPointeeDies` groups
`KindFunc` with the map and channel heads and asks `codecIsStableAddr` about the
field's first word, which for a function value is a CODE address - the text
segment, never an arena. That looks like it declares every struct holding a
callback to be dying. It was changed to answer false for `KindFunc` and the
relay-proxy output was **byte-identical before and after**, and a standalone
fixture built in the root arena with a callback field took the same path with
and without the change (instrumented: `retPointeeDies` never reached the field).
So it was reverted rather than committed: it may be a real inefficiency, but
nothing measured it, and an unproven edit to a codec shared by both compilers is
not worth the risk. Left open below.

**No minimal reproduction yet.** Two fixtures were written - one with a callback
field, one growing a slice outside the root arena - and both pass with the bug
present. The trigger depends on the real call depth and the sovereign-arena
borrow that `Conn`'s methods take, which the small shapes do not reproduce. The
evidence for the defect is therefore the interop test itself, which fails
without the fix and passes with it; a standalone regression test remains open.

**Evidence.** `test_cordn_chat_messages_cordn_peer` passes three consecutive
times on a clean build with no instrumentation: the coordinator records
`publishKeyPackage`, `storeJoinRequest`, `consumeKeyPackage`, `postGroupMessage`
and `storeWelcome`, and the peer prints `SENT hello from cordn`,
`READ hello from smesh`, `SENT cordn hears you`, `PASS`.

The other three cordn tests in that file pass individually. Running all four in
one pytest session is unreliable: each one boots a second Firefox driver against
module-scoped relay and coordinator fixtures, and the run then reports
"device B never linked the group" / "the cordn control never appeared" on tests
that pass on their own. Treat the per-test runs as the evidence, not the batch.
Do not rebuild a wasm module while a suite is running either - that alone
produced one false failure here.

**Authoring rule this establishes.** A struct that lives in the root arena must
have every field - text, slices, maps - allocated in the root arena. One
frame-backed field is enough to turn every return of that object into a deep
copy, and the copy fails silently: the fields look right, identity does not.

**Still open.** (a) Why `KindFunc` never reaches `retPointeeDies`' field walk -
either the fast path is skipped earlier for these shapes or function values take
a different branch, and the answer decides whether that grouping is a real
inefficiency. (b) A standalone reproduction, so the defect cannot return
unnoticed. (c) The compiler could refuse the shape outright: a `SovereignSetArena`
block that stores a frame-allocated slice or string into a root-arena object is
visible at compile time in the same function.
