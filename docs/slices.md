# Slices: the complete survey

A slice is Moxie's only sequence type. This document collects every builtin
that touches one, every stdlib package that operates on one, the lifetime rules
that make the design coherent, and an evaluation of the one place the model is
weak: a *reader* that consumes from the front. It exists so the design can be
judged on its costs rather than on its intent.

Streaming is possible today in its pull form (`io.Reader`, `bufio.Reader`,
`Scanner`, a cursor); only Go's function-iterator spelling is disabled, for two
specific reasons that §9 states. "Bring streaming back" is therefore an API
addition, not a language change.

## 1. What a slice is

A slice is a three-word header for arena storage: a pointer, a length and a
capacity. There is no heap and no ownership object; the storage lives in the
arena of the frame that allocated it (or in a receiver's sovereign arena) and is
reclaimed in bulk when that arena is released. `len` and `cap` return `int32`,
and there is no `int`/`uint` in the language.

`string` and `[]byte` are one mutable type: `len`, indexing, slicing and
assignment work identically on both spellings. The `string` keyword is a
representation pragma for the wasm target (native JS string vs byte array), not
a distinct type. String constants are per-occurrence: two literals with the same
text are two objects, and a literal is mutable through a variable that names it
(equality is by content, so the difference only shows when one site writes).

### Allocation

```moxie
var s []int32            // nil: len 0, cap 0
a := []int32{:1024}      // length 1024, capacity 1024, zeroed
b := []int32{:0:100}     // length 0, capacity 100
c := []int32{1, 2, 3}    // composite literal: length 3, capacity 3
```

`[]T{:n}` is the only way to allocate by size. The size literal allocates the
storage **and zeroes it** (verified: `[]int32{:4}` and `[]byte{:8}` read as 0).
A composite literal allocates and fills. `make`, `new` and `append` are undefined
names.

### The builtins

| builtin | signature | what it does | cost |
|---|---|---|---|
| `len` | `len(s) int32` | element count | O(1) |
| `cap` | `cap(s) int32` | capacity | O(1) |
| `s[i]` | read/write | bounds-checked element access | O(1) |
| `push` | `push(s, v...)` | appends; grows when capacity is short (lazy-init floor 4, then doubling); store-back to `s` is implicit | amortized O(1) per element |
| `pop` | `pop(s)` | statement: removes and returns the last element; expression: returns it without shrinking (the two behaviours are a footgun; the decided change is two names - `pop` always removes, `top` reads the last element - pending implementation) | O(1) |
| `resize` | `resize(s, n)` | sets the length; panics if `n > cap(s)` or `n < 0` | O(1) |
| `copy` | `copy(dst, src) int32` | element copy; the destination may not be a subslice | O(k) |
| `clear` | `clear(s)` | zeroes the elements, **length unchanged** | O(len) |
| `s[a:b]` | subslice | **allocates and copies** the range | O(k) |
| `a \| b` | concatenation | allocates a new slice | O(n+m) |
| `a == b` | equality | element-wise; slices of comparable elements also work as map keys | O(n) |

Two restrictions enforce the copy model:

- writing through a subslice (`s[a:b][i] = v`, or passing `s[a:b]` to a
  parameter the callee writes) is a compile error in both compilers -
  `tests/restrictions/reject_subslice_write.mx`;
- `copy` into a subslice (`copy(x[a:b], ...)`) is a compile error -
  `tests/restrictions/reject_copy_into_subslice.mx`. The fix is always
  `resize`/`push`, never a view.

A slice bound with `//:linkdata` has foreign storage. `cap` reports its `len`,
so `0 <= len <= cap` holds for every consumer - `s[:cap(s)]`, `cap-len`
arithmetic and any library that assumes the invariant. The fixed-length
sentinel is internal to the length-changing builtins: `push`, `pop` and `resize`
all **panic** with "linkdata slice: cap is foreign (copy it first)"
(`sliceFixedPanic`). Reading, indexing and copying out are unaffected.
Subslicing or cloning a binding copies it out into normal storage whose cap is
its length, so the copy is an ordinary slice and push/pop work on it. A
linkdata slice is for reading and copying out, not for streaming: do not hand it
to a reader (a cursor or `bufio.Reader`), which is built to advance over storage
it may re-slice and retain - the fixed extent and foreign ownership are the
wrong contract for a reader.

### Named slices

A name over a slice or map exists for its method set. The receiver is a pointer
like every Moxie receiver; a **value** receiver on a named slice or map is a
*reference* receiver (the value is the header for storage the caller owns), so
`func (n Names) Add(s string) { n = push(n, s) }` writes the caller's variable.
A value receiver on a struct remains a compile error.

### Copy semantics, precisely

`subslice` copies; a **header copy does not**. `x := s; y := s` gives two
headers over one storage, and a write through either is visible through the
other - that is not aliasing in the sense this document means, and it is how a
struct field, a slice parameter and a returned slice share state. What the
language guarantees is that a *range* handed out (`s[a:b]`) is independent
storage. Every sentence below about "no aliasing" means that guarantee.

## 2. Lifetime, arenas and compaction

Every allocation lands in the current function arena and is released when the
frame returns (or in a receiver's sovereign arena, which lives as long as the
receiver). The consequences that shape every slice API:

- **Growth garbage stays until the frame dies; only a sovereign group can be
  compacted away.** A bump arena has no free list: when a push doubles and
  abandons a backing, that region sits *below* the live data and no tail trim
  can reach it. Reclaiming it means moving the live data down over it - a
  compaction. That exists for sovereign groups because their pointers are
  registered (`codecSovereignCompact`), and it is deliberately absent for a
  function frame: the frame's live roots are registers and stack slots the
  runtime cannot enumerate, so a mid-frame move would have to rewrite pointers
  it cannot see. The practical rules are therefore: growth belongs in a free
  function (its garbage dies at return), presize when the size is knowable, and
  put a long-lived builder behind a sovereign receiver. A future step that
  would let frames compact too is a per-frame pointer map (the `Reloc*` marks at
  return are the seed) and a compaction at a safe point such as a select; that
  is a compiler feature, and it is the one change that would make "former
  backing stores are reclaimed" true in general.
- **Every domain grooms its own arenas when idle.** `idleTrimPages` releases up
  to a budget of pages of *unused tail* - the domain's root arena first, then
  its active frame arena - and the channel select's park loop calls it on every
  waiting turn, re-checking between pages so an arriving message is delayed by
  at most one page. That covers `main` and every spawned domain, which are event
  loops that select. It is a tail trim (`madvise(MADV_DONTNEED)` beyond the bump
  offset): it stops the committed set ratcheting up across idle turns, and it
  never touches live data, so it does not lower the RSS of live data. A tight
compute loop that never selects does not groom - by construction it is not
  idle. While a compaction cycle owns the region's tail, the trim is suspended
  (§10): the compactor releases the pages itself as they empty, and a trim over
  the same tail would be a use-after-madvise.
- **Why subslicing must copy: reclamation.** If `s[a:b]` were a view, any frame,
  receiver or arena could hold a pointer into storage it does not own. Then an
  arena could not be released at frame exit (something outside might still point
  into it), a sovereign group could not be moved when idle (the move would have
  to find and rewrite every outside pointer), and the tail trim would have to
  keep the pages a view still uses. Copy semantics gives every slice's storage
  exactly one owner, which is what makes reclamation O(pages) at return and a
  compaction a plain memmove with no fixups. The price is paid at the reader
  (one copy per segment) and, in the writer, only where it reslices to consume -
  the bug this revision removed (§4).
- **A reader's segment copy is the price of a movable arena.** Because
  `s[a:b]` copies, the segment a reader hands out survives the next read, the
  next write and a compaction of the source; the cost is one copy per segment,
  paid at extraction.

## 3. The three shapes, and what LIFO is for

Slices are used three ways, and only one of them is LIFO:

1. **Fixed-size value** - presize (`[]T{:n}`) and write through a cursor index.
   This is the dominant shape in the tree: the size is known or counted first,
   no growth, no reads from the front. LIFO is irrelevant here.
2. **Builder** - `push`/`pop`/`resize`: build up, inspect, take down. This is
   the LIFO shape, and it matches the arena stack: last in, first out, with the
   whole frame released at return. A stack is also the natural *reverse*
   reader: when a format is parsed back-to-front, `pop` consumes it in order.
3. **Reader / stream** - consume *from the front*: a framing parser taking
   length-prefixed segments, a line reader, a protocol decoder. This is FIFO by
   nature and is **not** what LIFO provides. The rest of this document is about
   that shape.

## 4. The reader: what exists, what it costs, what is missing

Without aliasing there are exactly three ways to consume a slice from the
front, and they differ only in where the copy is paid:

| shape | idiom | cost |
|---|---|---|
| index cursor | `off`/`pos` int, read `s[off:off+n]` per segment, `off += n` to skip | one copy per segment; **skips are O(1) and copy nothing** |
| cursor type | `bytes.Reader` (`i int64`), `bufio.Reader` (`r`,`w`), `bytes.Buffer` (`off`,`n`) | one copy per segment; buffer refill/compaction copies the unread remainder, amortized |
| reslice | `s = s[n:]` | **copies the whole remaining slice on every step: O(n) per step, O(n²) for n steps** |

The third shape is a bug, not a preference, and the two instances in the tree
are gone:

- `bufio.Writer.Write` and `WriteString` consumed with `p = p[n:]` /
  `s = s[n:]` in their buffer loops; both now carry an `off` cursor and copy
  with `mxutil.CopyRange(buf, n, src, off, ...)`. A 1 MB write through a 4 KB
  buffer now copies each byte once instead of reslicing ~256 times (~128 MB of
  copying). Verified end to end: 1 MB written and flushed through a 4 KB
  `bufio.Writer` round-trips byte for byte, for `Write` and for `WriteString`.
- `time.parseRFC3339` consumed with `s = s[19:]` and `s = s[n:]`; it now uses a
  `pos` cursor. Verified against `Z`, `.123456789`, `+04:00` and `-07:00`, and
  a missing zone is still rejected.

The correct stdlib counterpart already exists: `bytes.Index*From`,
`encoding/binary.UvarintFrom`, and `bufio.Reader`'s `r`/`w` cursor. The rule is:
**advance an index; reslice only to cut one segment you mean to copy** - and
never reslice to consume inside a loop (§12).

The segment-extraction API is `bytes.Split`/`Fields`/`Cut`, and all of them
copy: `Split` returns `[][]byte` whose elements are independent copies, `Cut`
returns two copies. That is the honest price of no aliasing and should be
stated wherever the API is documented.

### The aliasing-shaped comments

Go's comments on the ported APIs promised views. They now say "a copy":

- `bytes.(*Buffer).Bytes`, `bytes.(*Buffer).Next`,
  `bufio.(*Reader).ReadSlice`, `bufio.(*Reader).ReadLine`,
  `bufio.(*Scanner).Bytes`;
- `net/http/internal.(*chunkedReader).readChunkLine`,
  `internal/trace/tracev1.(*parser).readRawEvent`, `evloop.(*Conn).Read`.

`encoding/csv.(*Reader).readLine` is the one mixed case: a line that fit in a
single `ReadSlice` is a copy, while a line assembled across `ErrBufferFull`
fragments is `r.rawBuffer`, whose *header* is reused by the next call. That is
a shared header, not a subslice, and it is the only place where the result does
not outlive the next read.

Two APIs could not be fixed, only removed: `bytes.Buffer.AvailableBuffer` and
`bufio.Writer.AvailableBuffer` returned `b.buf[n:][:0]`, which under copy
semantics is a **zero-capacity copy** - appending to it wrote to throwaway
storage and the write to the buffer was lost. They had no in-tree callers and
are gone. The replacement is `Writer.Available` + `Write`, or a caller-owned
buffer.

`evloop.(*Conn).Read` deserves its own sentence: it is correctly a copy
(`c.rbuf[c.rpos:]`), but a hot loop that calls `Read` and then `Consume(n)`
copies the whole unread remainder on every iteration. The cursor path is
`Consume` alone; `Read` is for handing a segment to code that keeps it.

### The disabled streaming layer

`src/bytes/iter_wasm.mx` and `src/slices/iter_wasm.mx` are
`//:build moxie.never`: `bytes.Lines`, `bytes.SplitSeq`,
`slices.All/Values/Collect/Chunk/AppendSeq` are not available in any build.
§9 explains why, and what streaming looks like instead.

## 5. The stdlib survey

| package | slice surface | notes |
|---|---|---|
| `bytes` | `Equal`, `Compare`, `Index*`/`Index*From`, `Contains*`, `Count`, `Split`/`SplitN`/`SplitAfter`, `Fields`, `Join`, `Cut`, `Trim*`, `Replace`, `Map`, `ToUpper`/`ToLower`, `Runes`, `Clone`, `Buffer`, `Reader` | `Split`/`Cut`/`Trim*`/`Replace` return fresh slices; `Buffer` is the FIFO buffer (off/end, compaction on grow, `Bytes()` copies); `Reader` is the memory cursor; `Lines`/`SplitSeq` are disabled; `AvailableBuffer` is removed |
| `bufio` | `Reader`, `Writer`, `Scanner` | `Reader` is the front-consuming stream reader (`r`/`w` cursor, `fill`/compaction, `ReadSlice`/`ReadLine`/`ReadBytes` copy the segment); `Writer.Write`/`WriteString` now consume their input with an offset; `AvailableBuffer` is removed |
| `slices` | `Equal`, `Compare`, `Index`, `Contains`, `Insert`, `Delete`, `Replace`, `Compact`, `Reverse`, `Clone`, `Grow`, `Clip`, `Concat`, `Repeat`, `Sort*`, `BinarySearch*`, `Min`/`Max` | the generic Go-shaped API, minus the iterator functions (disabled); `Insert`/`Delete`/`Replace`/`Concat` return new slices (copies); `Reverse`/`Compact` work in place through indexes |
| `sort` | `Sort(Interface)`, `Search`, `Find`, `SearchInts`, `SearchFloat64s`, `SearchStrings`, `Reverse` | interface-based; closures do not capture, so comparators take their data through parameters or a sovereign receiver |
| `mxutil` | `Ensure`, `Reserve`, `GrowCap`, `CopyFrom`, `CopyAt`, `CopyRange`, plus string helpers (`SplitLines`, `JoinStrings`, `TrimSpace`, ...) | the growth helpers that keep doubling garbage in a dying frame; `CopyRange` is the offset-aware copy the cursor idiom needs |
| `encoding/binary` | `Read`, `Uvarint`, `UvarintFrom`, `Varint`, `PutUvarint`, ... | the `*From` variants are the cursor convention for parsers |
| `strconv`, `fmt` | `AppendInt`/`AppendFloat`/`FormatInt`/`FormatFloat`, `fmt.Sprintf`/`Fprintf` | the `Append*` family writes into a caller slice; `fmt` uses `...Stringer` variadics, not slices of interfaces |
| `encoding/*`, `crypto/*`, `hash/*`, `compress/*` | most codecs take `[]byte` and return `[]byte` or write into a caller slice | the copy model shows up as "caller provides the destination" APIs |
| spawn channels (`src/runtime/chan.mx`, codec) | a slice in a channel element | element with references: structural codec (relocation); without: raw memcpy. Variable-length dimensions declare `cap:"..."`; unstated is one page (4096B); overrun panics and names the field |

## 6. The cost model, in one table

| operation | cost |
|---|---|
| index, `len`, `cap`, `resize`, skip by index | O(1) |
| `push` (amortized), `pop` | O(1) |
| subslice `s[a:b]`, `copy`, `clear` | O(k) / O(k) / O(len) |
| `a \| b`, `==`, map-key hash/lookup | O(n) / O(n) / O(n) |
| reader: skip/advance by cursor | O(1) |
| reader: extract one segment | O(segment length) - one copy |
| reader: `s = s[n:]` | O(remaining) - never in a loop (fixed sites, §4) |
| `bufio.Writer.Write` of n bytes | O(n) total: each byte copied once (offset + `CopyRange`) |
| buffer refill / compaction | O(unread), amortized O(1) per byte written |
| sovereign compaction (idle) | O(live bytes) of the group, one group per select |
| arena tail trim (idle) | O(pages) `madvise`, never touches live data |
| arena release at return | O(pages) |

## 7. Is the design correct?

**For memory management: yes.** Copy semantics is what makes bulk reclamation
and idle compaction possible at all. Every slice's storage has one owner, so an
arena can be released at frame exit, a sovereign group can be relocated when
idle, and a reader can hold a segment without pinning the buffer. The
alternative - views - would need per-pointer fixups on every move, which is a
tracing GC's job (stop-the-world, pointer maps), not an arena's.

**For writers: yes.** `push`/`pop`/`resize` over a known capacity is the stack
discipline the arena wants; the presize-and-cursor shape covers fixed values;
growth is explicit and its garbage is visible.

**For readers: no, not yet - and the fix is not aliasing.** The language gives
the reader no primitive and no single type; the stdlib has three cursor types
(each with its own offset field and its own copy semantics), a family of `*From`
index helpers, and had two O(n²) reslice loops (now fixed, §4). The remaining
work is naming the reader and giving it one home.

What follows from the evaluation, none of it requiring a language change:

1. **Name the reader.** Add one documented cursor type to `slices` (or make
   `bytes.Reader`/`bufio.Reader` the canonical pair and say so): `off int32`
   over a slice, `Next(n) []T` (a copy), `Skip(n)` (no copy), `Remaining()`,
   `Peek(n)`. It must be a **pointer-receiver** type - a value receiver on a
   struct is a compile error - so callers pass `*Reader`; passing it by value
   would fork the offset. That is a stdlib addition, not a builtin.
2. **Keep the reslice rule.** `s = s[n:]` inside a loop is a bug; the compiler
   could warn on a reslice whose result immediately rebinds the same variable,
   as a lint rather than a semantic rule.
3. **Say "copy" in the docs.** The aliasing-shaped comments are fixed; new ones
   must state the copy.
4. **Keep LIFO where it belongs.** The stack discipline is the builder's, not
   the reader's; do not add a FIFO header field or a `shift` builtin. A
   front-consuming reader is a cursor, and the FIFO *buffer* case is
   `bytes.Buffer`/the runtime ring, which already exist.

What would falsify this: a workload where per-segment copies dominate - a
protocol parser that must retain most of its input as segments, not inspect it.
Then the right answer is still not aliasing; it is a reader that hands out
*offsets into one retained buffer* (`(base []byte, off, len)`) and lets the
consumer copy only what it keeps. That is a data-structure decision at the call
site, and this document should be updated with the measurement that shows it.

## 8. Why streaming is disabled, and how it comes back

Streaming is not what is missing. The pull model — `io.Reader`, `bufio.Reader`,
`Scanner`, a cursor over a slice — *is* streaming, and it works today. What was
disabled is Go's **push-iterator spelling** (`bytes.Lines`, `slices.All`,
`iter.Seq`), by two independent facts:

1. **Closures do not capture.** `for x := range seq` lowers to
   `seq(func(x) bool { body })`, and the body refers to the loop's surroundings
   (`sum += x`), which needs a closure environment. Moxie has none by design, so
   the consumer cannot be written as a callback at all.
2. **There are no coroutines.** `iter.Pull` inverts push to pull, and that needs
   a second stack to suspend the producer mid-loop (`runtime.coroswitch`,
   `runtime.newcoro` — deliberately absent). A domain has one stack, the arena
   stack assumes one execution point, and `sigint`/`closed` polling is written
   against it.

Neither blocker is about streams, and neither has to be solved to get streaming
back:

- **The first-class piece is a cursor, a data type rather than syntax:**
`slices.Reader[T] { buf []T; off int32 }` with `Next(n)` (copy), `Skip(n)`
  (no copy), `Peek`, `Remaining`. It is pointer-receiver (a struct value
  receiver is a compile error), so pass `*Reader` or the offset forks.
  `bytes.Reader` and `bufio.Reader` are already the byte and stream versions;
  the gap is the generic slice one and a line in the docs saying "this is the
  reader"
- **Segmenters stay as they are:** `bufio.Scanner` copies each token,
  `bytes.Split` copies each field. A lattice node's inbound stream is frames
  over a channel plus a cursor over the frame body — both exist.

If function-iterator syntax is genuinely wanted later, the price is explicit:
capture (a closure environment with its own lifetime and arena) and coroutines
(a second stack per domain, suspension points, and the arena stack switching
with it). That is the language-level change the arena model was designed to
avoid; the cursor gives the same streaming with none of it. This is why "the
disabled `iter` layer will not come back" — the *spelling* will not, the
capability is already here.

## 9. Where the rules live

- `docs/REFERENCE.md` — the builtin table and the push/pop/resize rationale
  (corrected by this survey: `push` grows, `recover` is not callable);
- `tests/restrictions/reject_subslice_write.mx`,
  `tests/restrictions/reject_copy_into_subslice.mx` — the two copy rules;
- `src/runtime/alloc.mx` — sovereign arenas, `SovDrainCompactions`,
  `codecSovereignCompact`, the `escaped` flag;
- `src/runtime/chan.mx`, `src/runtime/idle_trim_linux.mx` — the select-time
  compaction drain and `idleTrimPages` (root arena, then active frame arena);
- `src/bufio/bufio.mx`, `src/bytes/buffer.mx`, `src/bytes/reader.mx` — the
  three reader shapes, and the offset-consuming writer;
- `src/mxutil/mxutil.mx` — `Ensure`/`Reserve`/`GrowCap`,
  `CopyFrom`/`CopyAt`/`CopyRange`;
- `src/bytes/iter_wasm.mx`, `src/slices/iter_wasm.mx` — the disabled iterator
  layer and its reason.

## 10. Planned: compaction of long-lived arenas (sovereign, main, spawn)

Status: design, not implemented. The prerequisite - per-frame pointer maps - does
not exist yet. This section is the specification to build against, and it
separates the two areas the problem really has: **the data** (bytes in the
arena) and **the metadata** (what is where, what is live, and who points at it).

**Why it is needed.** A frame arena is released when its frame returns. The
`main` and spawn bodies are event loops that never return, so everything the
loop body itself allocates (not a callee's frame, which does die) accumulates in
that one arena for the process lifetime: the classic event-loop ratchet. The
same is true of a sovereign receiver's arena, except that one already has a
precise model and a compaction queue. The fix is the same mechanism for all
three, run **during idle**, which for a Moxie domain means "inside the select
park loop", next to `idleTrimPages` and `SovDrainCompactions`.

**The parameters, as specified:**

- work **in reverse from the end**, so the pages freed are the last ones; each
  page that empties is `madvise`d away and the region's allocated length
  shrinks - the arena ratchets *down*, page at a time;
- a **progress tracker**: the offset from the end that the scan reached last
  time, so a cycle continues across idle turns instead of restarting;
- a **flag** that records "an allocation happened since the last select". An
  allocation invalidates the plan (it may have taken a hole the compactor
  intended to use, or extended the region), so the tracker resets - the user's
  rule is "reset to zero at every allocation after a select is exited to run a
  branch", i.e. an allocation cancels the cycle and the next park starts a fresh
  one;
- every time the scan **crosses a page boundary** it returns to the select to
  dispatch channel events (Variant B, below);
- **the trim yields to the compactor.** `idleTrimPages` and the copy phase both
  own the region's tail, so while `compactRemaining` is set the trim is
  suspended (its budget belongs to the compactor). A trim's `madvise` beyond the
  bump offset could free a page the compactor has not copied from yet; the
  compactor releases the pages itself as they empty, and only then does the trim
  resume.
- when data is squashed downwards, **reference pointers are rewritten**.

**The two areas.**

*Data*: the arena's bytes, in regions of pages, with a bump offset. Nothing new
is needed here beyond the move itself.

*Metadata*, per region: an **object table** of
`{start, size, typeDesc, flags}` written at allocation time (the object header,
or a side array in the metadata area), where `typeDesc` is the type's pointer
map - the same information the compiler already emits for return relocation
(`RelocMark`/`RelocFixup` walk a value's pointers; a per-type "visit every
pointer field" function is the reusable form). Liveness is not stored in the
table; it is computed by a mark from the roots. `flags` carries `dead` (a
relocation or an overwrite left this object without a reachable reference - a
hint the mark confirms) and the Variant B edge records. **Nothing is pinned**:
an arena is closed (below), so every object in it can move.

**The roots.** Three sources, all needed together:

1. **Package variables**: every package variable is a TLS slot whose type is
   known statically, so each package emits a static root map (TLS offset, type
   descriptor). The flattened package block is the arena's first allocation and
   never moves - the slide starts above it - but the pointers *inside* the block
   must still be rewritten when the objects they point to move. The map is there
   for that fixup, not to relocate the block.
2. **The current frame**: a per-safepoint map of live pointer slots and their
   types (the next section). Compaction happens in the frame that is running
   the select, so one frame's map is enough - no stack walk, provided we compact
   only that frame's arena (and the domain's root arena when that frame is the
   top one).
3. **The sovereign registry**: already precise (`sovGroup`), and its compaction
   already exists.

**The cycle, in phases.**

1. **Mark** (read-only, resumable at any boundary): walk from the roots, mark
   live objects in the object table, and record the pointer edges if the
   dispatch-safe variant is chosen (see below).
2. **Forward** (read-only): assign every live object a new address by sliding
   live objects down in address order. The forwarding map is `old -> new`,
   complete before any byte moves.
3. **Copy + fixup** (page-bounded): process pages from the **top down**. For
   each live object in the page, copy its bytes to its destination and rewrite
   its outbound pointer fields through the forwarding map as they are copied,
   then mark the vacated range dead. When every live object in a page has
   moved, `madvise` the page and shrink the region's allocated length. Every
   page boundary saves the tracker and returns to the select to poll.
4. **Root fixup** (small): rewrite the roots (frame slots, package variables)
   through the forwarding map.

**The consistency rule (Variant B, the design).** Idle grooming must be able to
dispatch between pages, so every pointer must be valid at every instant. The
mark phase therefore records the arena's internal edge set as
`(source object, field index) -> target object` - object identity, not raw
addresses, because a source's field address changes when the source moves. A
move is then atomic: copy the object, rewrite every inbound edge through the
graph, mark the old range dead; only then may the select dispatch. The price is
the edge list in the metadata area, one record per pointer in the arena. Without
it the copy phase would have to run to completion before any branch - the
poll-only fallback - which is kept in reserve if the graph's memory proves
unacceptable, not as the plan.

**What the tracker means here.** `compactRemaining` is the scan cursor as an
offset from the end of the region, set to the region's allocated length when a
cycle starts and saved at every page crossing. `allocatedSinceSelect` is the
cancellation flag: any allocation after the select returns clears
`compactRemaining` (the plan is stale) and the next park re-marks. A cycle that
is aborted in the read-only phases costs nothing; the copy phase cannot be
aborted partway (it either has not started or runs to the root fixup), which is
why A and B differ in what "returning to the select" may do.

**No pinning: the arena is closed.** The scope that owns a reference lives
*outside* the arena being written to. A function's arena is that function's
working space; a self-mutating type's sovereign arena belongs to the receiver;
anything that must outlive the scope crosses the boundary by the call protocol -
parameters are copied in, return values are relocated out, and a store into a
receiver relocates into the receiver's arena. So an arena's objects are pointed
at only by (a) pointer slots in the executing frame, (b) other objects in the
same arena, and (c) the receiver's own fields for a sovereign group. There is no
external interior reference to pin, and the compactor needs no `pinned` flag.
The one fixed floor is the package-variable block: package variables are
allocated once at init into a flattened block that is the arena's first
allocation, and since the slide starts above it, it never moves - it is the
base, not a pin.

**Dead space is produced by relocation, and by overwrite as a hint.** When the
return protocol (or a receiver store) copies a value out of an arena, the source
object's bytes are dead as soon as the copy completes; the object table marks
them dead and the next idle compaction reclaims them. The same is true of a
variable overwrite: `s = s[n:]` stores a new header into the slot, and the old
backing may have lost its last reference. The compiler can emit that store as a
*dead hint*, but the authoritative decision is the mark from the roots, because
a copied header (`x := s`) still references the old backing. That distinction
also bounds the damage of an unwitting reslice loop: the O(n^2) *copying* remains,
but the *live* set is only the current remainder, not every abandoned tail,
because each abandoned tail is unreachable as soon as the only header that named
it is overwritten.

**Cost, honestly.** The object table costs bytes per allocation (an 8-byte header
plus a type pointer, or a side entry); the mark is O(live objects + edges); the
copy is O(live bytes); the fixup is O(edges + roots); Variant B adds the edge
list to peak memory. The payoff is that a long-lived arena stops growing without
bound while keeping O(pages) release at frame exit unchanged.

### Per-frame pointer maps (the prerequisite)

Because an arena is closed, the map only has to name the pointers of ONE frame:
the frame that owns the arena being compacted, which is by construction the
frame running the select. No stack walk is needed.

The mechanism that needs no LLVM stackmap support is a compiler-owned **root
array** in that frame:

- at each select, the compiler stores the current value of every pointer-typed
  variable in scope (slice header, string header, `*T`, map pointer) into a
  fixed-size array it allocates for that select, and passes `(arrayPtr,
  mapPtr)` to the runtime select entry;
- after the select returns, it loads the values back into the variables, so a
  pointer the runtime rewrote takes effect for the rest of the frame;
- the map is a static table emitted per function and per select: `count`, then
  for each root its kind (slice / string / pointer / map / interface / handle)
  and the type descriptor the compactor needs to follow it.

Two choices are open and cheap to change:

- **Root set**: all pointer-typed variables in scope (assigned at least once) is
  the conservative choice and needs no liveness analysis; a precise live set is
  smaller and cheaper per select but needs the liveness the compiler already
  computes for return relocation. Start conservative, tighten later.
- **Interface-typed roots**: an interface value's data pointer has a dynamic
  type, so following it needs the itab's type descriptor. The first version can
  refuse to compact a frame that has an interface root (the map says so); the
  alternative is to teach the walker to descend through the itab.

Work items, in order: (1) the root array + map at the select in both compilers
(wood law), with the runtime entry taking the pair and keeping it in TLS for
the duration of the park; (2) the object table and the type "visit pointers"
function in the runtime; (3) Variant B compaction at the select, with the
tracker and the page budget; (4) the frame arena and the domain root arena as
the first targets (sovereign already has its own path); (5) the dead-marking
hints from relocation and overwrite.

## 11. Streaming: channels, comma-ok, and the cursor

The missing streaming layer is not the *concept*; it is the Go spelling. A FIFO
stream is already expressible with a channel and comma-ok:

```moxie
for {
    buf, ok := <-in          // FIFO: the ring is a queue
    if !ok {
        return closed        // closed channel: the stream ended
    }
    for len(buf) > 0 {       // parse the buffer with a cursor
        seg, rest, found := cut(buf)
        ...
        buf = rest           // one cut, not a loop of reslices
    }
}
```

What a channel gives, and what it does not:

- **FIFO**: yes, per channel. The ring is a queue; the depth is the declared
  outstanding count, and the sender parks when it is full. That is backpressure
  for free - the producer cannot outrun the consumer by more than the declared
  depth.
- **Close as end-of-stream**: yes, comma-ok is exactly the `ok == false` that
  says "no more".
- **Ordering across producers**: per channel, not global. Two producers
  interleave; a total order needs one producer or a sequence number in the
  element.
- **Peeking**: no. A receive consumes. This is why `slices.Reader[T]` matters:
  a parser needs to look at the next N bytes before deciding to consume them.
  The workable composition is *buffer at a time*: pull a buffer from the
  channel, wrap it in a `Reader` (or `bytes.Reader`/`bufio.Reader`), parse with
  `Peek`/`Next` inside it, and pull the next buffer when it is exhausted. That
  is exactly what `bufio.Scanner` does for byte streams, and it is the shape the
  lattice's framing wants.
- **Copy cost**: crossing a channel copies the message into the ring, and the
  receiver gets its own copy in its arena (or a raw memcpy when the element
  holds no references). That copy is the contract for cross-domain data. Within
  one domain, a cursor over one buffer copies only the segments that are
  retained. So: cross-domain stream = channel; in-buffer parsing = cursor;
  usually both, as above.

This is why "streaming back" does not need `iter.Seq` or coroutines: the
channel is the FIFO and the close signal, the cursor is the lookahead, and
`bufio.Scanner` is the existing byte-stream segmenter. The only thing missing
from the language surface is the generic `slices.Reader[T]`.

## 12. The reslice lint (proposed)

"Never reslice to consume" is too broad: a reader that takes one segment from
the front *must* cut the slice, and the cut is a copy of that segment (plus the
remainder, in the `s = s[n:]` spelling) - that is the model working, not a bug.

The cost only turns quadratic when the same variable is resliced **in a loop**,
each iteration copying what is left. So the lint should target the shape, not
the operation:

- fire when a slice variable is rebound by a reslice inside a loop (the message:
  "reslice in a loop copies the remainder every iteration; advance an index
  instead");
- do **not** fire on a single cut (`seg := s[n:]`, `buf = rest` where the loop
  then parses with a cursor), which is how a reader takes a segment;
- for the genuine case of a loop over a small bounded buffer where the copy is
  deliberate, a `//:nolint reslice` directive with the reason - documented, not
  silent.

The two sites this survey fixed were write loops over unbounded input, which is
precisely the shape the lint targets. Reader code is not excluded as a
category; it is excluded by *shape* (single cuts pass, remainder-copy loops
fail, and a documented suppression exists).
