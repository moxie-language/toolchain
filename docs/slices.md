# Slices: the complete survey

A slice is Moxie's only sequence type. This document collects every builtin
that touches one, every stdlib package that operates on one, the lifetime rules
that make the design coherent, and an evaluation of the one place the model is
weak: a *reader* that consumes from the front. It exists so the design can be
judged on its costs rather than on its intent.

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
| `pop` | `pop(s)` | statement: removes and returns the last element; expression: returns it without shrinking | O(1) |
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

`recover` was listed in the builtin table until this survey removed it; it is a
compile error: *"recover() is not available in Moxie: a panic is fatal"*. `panic`
is death; error values are the recoverable path.

A slice bound with `//:linkdata` (the all-ones capacity) cannot change length:
`push`, `pop` and `resize` all refuse, because writing past the external symbol
would corrupt it.

### Named slices

A name over a slice or map exists for its method set. The receiver is a pointer
like every Moxie receiver; a **value** receiver on a named slice or map is a
*reference* receiver (the value is the header for storage the caller owns), so
`func (n Names) Add(s string) { n = push(n, s) }` writes the caller's variable.
A value receiver on a struct remains a compile error.

## 2. Lifetime, arenas and compaction

Every allocation lands in the current function arena and is released when the
frame returns (or in a receiver's sovereign arena, which lives as long as the
receiver). The consequence that shapes every slice API:

- **Growth garbage stays.** Pushing past capacity doubles, and the abandoned
  backing remains live in the arena that allocated it until that arena dies.
  Peak arena is the sum of every intermediate, so presizing (`[]T{:n}`) is the
  fast shape and growth belongs in a free function whose frame dies early
  (`mxutil.Ensure`, `mxutil.Reserve`, `mxutil.GrowCap`).
- **Sovereign arenas are compacted when idle.** A self-mutating named type owns
  an arena; when it reaches its pressure ratchet, `SovDrainCompactions` (called
  at every `select`, one group per drain) moves the live data to a fresh, smaller
  region (`codecSovereignCompact`). While parked, the channel select's park loop
  also trims unused tail pages (`idleTrimPages`), so a later burst reuses them.
- **Compaction is only safe because there are no aliases.** Moving a group's
  bytes is a raw relocation: nothing outside the group may hold a pointer into
  it. Copy semantics are what guarantee that: a slice handed out is a copy (or a
  value relocated into the caller's arena), never a view into another frame's or
  another group's storage. The one escape hatch is explicit: once a pointer
  crossed a codec boundary the group is marked `escaped` and is never compacted.
  With Go-style aliasing, every view into the group would have to be found and
  fixed up - or the group could never move - which is exactly the pointer-map
  machinery Moxie does not have.
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

The third shape is the trap. It is the direct Go port and it is wrong under
copy semantics; it appears in the tree:

- `src/bufio/bufio.mx`, `(*Writer).WriteString`: `s = s[n:]` inside the
  large-write loop - a 1 MB write through a 4 KB buffer reslices ~256 times and
  copies ~128 MB in total;
- `src/time/format_rfc3339.mx`: `s = s[19:]` then `s = s[n:]` while parsing a
  timestamp - small strings, but the same O(n²) shape.

The correct stdlib counterpart already exists:

- `bytes.IndexByteFrom`, `bytes.IndexAnyFrom`, `bytes.IndexRuneFrom` take a
  start offset;
- `encoding/binary` has both `Uvarint(buf)` (returns the count) and
  `UvarintFrom(buf, pos)` (cursor in, cursor out);
- `bufio.Reader` keeps `r`/`w` and only compacts when it must.

The segment-extraction API is `bytes.Split`/`Fields`/`Cut`, and all of them
copy: `Split` returns `[][]byte` whose elements are independent copies, `Cut`
returns two copies. That is the honest price of no aliasing and should be
stated wherever the API is documented.

### The stale aliasing contract

Four stdlib comments still describe Go's aliasing and are false here:

- `bytes.(*Buffer).Bytes`: *"The slice aliases the buffer content ... immediate
  changes to the slice will affect the result of future reads"* - it returns a
  copy; changes do not affect the buffer.
- `bufio.(*Reader).ReadSlice`: *"returning a slice pointing at the bytes in the
  buffer ... will be overwritten by the next I/O operation"* - it returns a copy
  that stays valid.
- `bytes.(*Buffer).Next`: returns `b.buf[b.off:b.off+n]`, a copy (Go's returns
  a view);
- `bufio.(*Scanner).Bytes`: *"may point to data that will be overwritten by a
  subsequent call to Scan. It does no allocation"* - the token is a subslice
  copy, so it is stable and the allocation is the copy.

Ported code that relies on mutation-through-a-view silently loses the write
instead of failing loudly. The comments should say "a copy", and this document
is the place that says why.

### The disabled streaming layer

`src/bytes/iter_wasm.mx` and `src/slices/iter_wasm.mx` are
`//:build moxie.never`: Moxie has no coroutines (`runtime.coroswitch`,
`runtime.newcoro` do not exist), so `bytes.Lines`, `bytes.SplitSeq`,
`slices.All/Values/Collect/Chunk/AppendSeq` are not available in any build.
`src/iter` declares the `Seq`/`Seq2` types but its `Pull` needs the same
coroutine machinery. The language has no `for range` over a function, and no
iterator protocol; a reader is a cursor the caller loops over by hand.

This is the gap the LIFO focus leaves: the writer has a language primitive
(`push`/`pop`), the reader has a convention spread over three types and two
idioms, and the layer that would have unified it is disabled. That is a
documentation and API problem, not an argument for aliasing.

## 5. The stdlib survey

| package | slice surface | notes |
|---|---|---|
| `bytes` | `Equal`, `Compare`, `Index*`/`Index*From`, `Contains*`, `Count`, `Split`/`SplitN`/`SplitAfter`, `Fields`, `Join`, `Cut`, `Trim*`, `Replace`, `Map`, `ToUpper`/`ToLower`, `Runes`, `Clone`, `Buffer`, `Reader` | `Split`/`Cut`/`Trim*`/`Replace` return fresh slices; `Buffer` is the FIFO buffer (off/end, compaction on grow, `Bytes()` copies); `Reader` is the memory cursor; the `Lines`/`SplitSeq` iterators are disabled |
| `bufio` | `Reader`, `Writer`, `Scanner` | `Reader` is the front-consuming stream reader (`r`/`w` cursor, `fill`/compaction, `ReadSlice`/`ReadLine`/`ReadBytes` copy the segment); `Writer.WriteString` still uses the `s = s[n:]` idiom |
| `slices` | `Equal`, `Compare`, `Index`, `Contains`, `Insert`, `Delete`, `Replace`, `Compact`, `Reverse`, `Clone`, `Grow`, `Clip`, `Concat`, `Repeat`, `Sort*`, `BinarySearch*`, `Min`/`Max` | the generic Go-shaped API, minus the iterator functions (disabled); `Insert`/`Delete`/`Replace`/`Concat` return new slices (copies); `Reverse`/`Compact` work in place through indexes |
| `sort` | `Sort(Interface)`, `Search`, `Find`, `SearchInts`, `SearchFloat64s`, `SearchStrings`, `Reverse` | interface-based; closures do not capture, so comparators take their data through parameters or a sovereign receiver |
| `mxutil` | `Ensure`, `Reserve`, `GrowCap`, `CopyFrom`, `CopyAt`, `CopyRange`, plus string helpers (`SplitLines`, `JoinStrings`, `TrimSpace`, ...) | the growth helpers that keep doubling garbage in a dying frame; the copy helpers avoid materializing `src[off:]` as an extra copy before `copy` |
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
| reader: `s = s[n:]` | O(remaining) - never in a loop |
| buffer refill / compaction | O(unread), amortized O(1) per byte written |
| sovereign compaction (idle) | O(live bytes) of the group, one group per select |
| arena release at return | O(pages) |

## 7. Is the design correct?

**For memory management: yes.** Copy semantics is what makes bulk reclamation
and idle compaction possible at all. Every pointer has exactly one owner, so an
arena can be released at frame exit, a sovereign group can be relocated when
idle, and a reader can hold a segment without pinning the buffer. The
alternative - views - would need per-pointer fixups on every move, which is a
tracing GC's job (stop-the-world, pointer maps), not an arena's.

**For writers: yes.** `push`/`pop`/`resize` over a known capacity is the stack
discipline the arena wants; the presize-and-cursor shape covers fixed values;
growth is explicit and its garbage is visible.

**For readers: no, not yet - and the fix is not aliasing.** The language gives
the reader no primitive and no single type; the stdlib compensates with three
cursor types (each with its own offset field and its own copy semantics), a
family of `*From` index helpers, and two O(n²) reslice loops that are Go
transplants. The disabled `iter` layer is where Go would have put the streaming
answer, and Moxie has deliberately no coroutines, so that layer will not come
back.

What follows from the evaluation, none of it requiring a language change:

1. **Name the reader.** Add one documented cursor type to `slices` (or make
   `bytes.Reader`/`bufio.Reader` the canonical pair and say so): `off int32`
   over a slice, `Next(n) []T` (a copy), `Skip(n)` (no copy), `Remaining()`.
   That is a stdlib addition, not a builtin.
2. **Delete the reslice idiom.** `s = s[n:]` inside a loop is always a bug in
   Moxie; rewrite `bufio.WriteString` and RFC3339 with an offset. Optionally
   have the compiler warn on a reslice whose result immediately rebinds the
   same variable (a lint, not a semantic rule).
3. **Say "copy" in the docs.** `Buffer.Bytes`, `Buffer.Next`, `ReadSlice`,
   `ReadLine`, `Split`, `Cut`: every one returns independent storage, and the
   Go-shaped comments must say so.
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

## 8. Where the rules live

- `docs/REFERENCE.md` - the builtin table and the push/pop/resize rationale
  (corrected by this survey: `push` grows, `recover` is not callable);
- `tests/restrictions/reject_subslice_write.mx`,
  `tests/restrictions/reject_copy_into_subslice.mx` - the two copy rules;
- `src/runtime/alloc.mx` - sovereign arenas, `SovDrainCompactions`,
  `codecSovereignCompact`, the `escaped` flag;
- `src/runtime/chan.mx` - the select-time compaction drain and `idleTrimPages`;
- `src/bufio/bufio.mx`, `src/bytes/buffer.mx`, `src/bytes/reader.mx` - the
  three reader shapes;
- `src/mxutil/mxutil.mx` - `Ensure`/`Reserve`/`GrowCap`, `CopyFrom`/`CopyAt`;
- `src/bytes/iter_wasm.mx`, `src/slices/iter_wasm.mx` - the disabled iterator
  layer and its reason.
