# Remaining work: execution plan

Written after round 251. Everything below assumes the reader starts with no
memory of the session that produced it; any claim that matters has a command next
to it.

## 0. What you inherit

Repository: `/home/mleku/moxie/git.smesh.lol/moxie`, branch `dev`, level with
`origin/dev`. Confirm with `git status --porcelain` (empty) and
`git rev-list --left-right --count origin/dev...HEAD` (`0 0`).

The compiler is release-candidate. The browser of record is
`docs/OPEN_WORK.md`; rounds 239-251 are the copy rule and its bill.

**The gate is `tests/quickcheck.sh`, not the full sweep.**

```
bash tests/quickcheck.sh          bootstrap (phase 2) + 13 fixtures   ~5 min
bash tests/quickcheck.sh --fast   14 fixtures only                    ~1 min warm
bash tests/unitrun.sh --phases 0a,0b,2 --force   the full sweep, 170 units, ~2.5 h
```

Run quickcheck after each batch of commits. Run the full sweep once, at the end
of the array campaign (item W7) and before any release tag.

### Process rules (from CLAUDE.md, non-negotiable)

- **Wood law**: a compiler change lands in `legacy/` first, then
  `_mxc_stage4/`, in the same or immediately adjacent commit. `legacy/` is the
  bootstrap path.
- **One logical change per commit.** Docs go in the same commit (a paragraph in
  `docs/OPEN_WORK.md`, numbered round) or in an adjacent docs commit.
- Never weaken, skip or delete a test to make a change pass; adapt plumbing only.
- Push with `git push origin HEAD:dev` after each commit that passes quickcheck.
- **Batch by class**: make every edit a class needs, then test once, then fix.
  Do not test after each edit; the gate is cheap but not free.
- `moxie build -o /dev/null ./src/<pkg>` on a *library* ends with
  `linker failed: undefined symbol: main.main`. That is **success**: it compiled.
  Only `compile error`, `undefined:` or a `;` diagnostic line means failure.

### Bootstrap protocol (only when a compiler file changed)

```
cd legacy && GOROOT=/opt/go1.25.3 bash build.sh
cd .. && export MOXIEROOT=$PWD
./legacy/moxie build -a -o /tmp/s4a ./_mxc_stage4
/tmp/s4a build-runtime
/tmp/s4a build -a -o /tmp/s4b ./_mxc_stage4
/tmp/s4b build -a -o /tmp/s4c ./_mxc_stage4
sha256sum /tmp/s4b /tmp/s4c      # must match: that pair is the fixpoint
cp /tmp/s4c ./moxie
```

### Traps that cost real time

- `pkill -f "<pattern>"` kills your own shell when the pattern appears in the
  command you are running. Kill by PID from `pgrep -fa`, never by pattern string
  you just typed.
- A sub-slice is a **copy**. Never use `x[a:b]` as a write destination, and never
  re-slice a cursor in a loop (`x = x[n:]` copies the whole remainder). The
  offset forms that already exist: `resize`, `mxutil.CopyAt/CopyRange`,
  `byteorder.*At`, `binary.*PutUint*At`, `Int.FillBytesAt`, `bytealg.IndexFrom`,
  `bytealg.IndexByteFrom`, `utf8.EncodeRuneAt`, `io.ReadFullIntoN`.
- An array base is a header with a **negative capacity**, and `arr[:]` is that
  header with the cap sign flipped: same base, now length-changeable. That is not
  an aliasing exception (round 281 corrected round 262's framing) - a sub-slice
  still copies, and `arr[a:b]` is a sub-slice. `[16]byte{...}` written as
  `[]byte{...}` keeps only the bytes spelled out - round 248 fell into that.
- `resize(x, n)` keeps the buffer and the capacity and does **not** zero.
- `bash tests/unitrun.sh --units a,b,c` selects exactly those fixture names;
  a unit it did not select keeps its old verdict and no longer affects the exit
  code (fixed in round 251).

---

## W1. TLS handshake under the copy rule (functional gap) — DONE, round 253

The fixture exists (`tests/regressions/should_compile/tls_handshake.mx`), is in
`tests/quickcheck.sh`, and passes: TLS 1.3, a 1 KiB echo, wrong CA rejected. It
found five bugs on the way - cryptobyte's cursor handing back dead frames, the
asn1 tagged encoder pointing into itself, nine slices written by index with no
length, and two record-cipher type switches that could never match. All five are
fixed; `docs/OPEN_WORK.md` round 253 has the detail. One bug is left: the
X25519MLKEM768 share (W9), which the fixture pins X25519 to route around.

The original brief, for the record:

**Why.** `crypto/tls` is linked into every server in the tree and **no fixture
exercises the record layer**. Rounds 240-246 converted `tls/conn.mx` blind
(`sliceForAppend`, the CBC/stream paths, `mgf1XORAt` neighbours). A handshake is
the single strongest "builds usable code" signal available.

**Where.** New fixture `tests/regressions/should_compile/tls_handshake.mx`.
`crypto/tls` APIs in this tree: `tls.Server`, `tls.Client`, `tls.Config`, and
notes from round 240: `rand.Reader()` is a func, `tls.Config.Certificates` takes
`[]tls.Certificate` with `Certificate`/`PrivateKey` fields.

**Shape.** A handshake needs two domains: in one domain `net.Pipe()` deadlocks.
Use `spawn` with a worker that owns the server end, or two TCP sockets over
127.0.0.1. Pass the connection's fd across the spawn boundary, or open the
listener in the parent and `spawn` the accept side. Follow
`tests/netsmoke/server` for the manager/worker shape and `src/evloop/conn.mx`
for the accept loop.

**Acceptance.**

1. Server handshake and client handshake both return nil error.
2. `ConnectionState().CipherSuite` and `.Version` agree on both sides.
3. 1 KiB of application data round trips byte-identically in both directions.
4. A client configured with the wrong CA (or no CA) is rejected, and the server
   sees an error rather than a hang.
5. It is a `should_compile` fixture, so the process exits non-zero on failure and
   `tests/quickcheck.sh` picks it up automatically once added to `FIXTURES` there.

**If it fails**, the failure names the window: check `tls/conn.mx`
(`encrypt`, `decrypt`, `readRecord`, `writeRecord`) for a sub-slice used as a
destination, in the shape `dst := buf[off:]` followed by writes.

---

## W2. x509 certificate path warns "codec arena escape"

**Why.** `x509.CreateCertificate` now works (round 251) but every call prints:

```
moxie: codec arena escape: pointer into a transient frame arena preserved instead of copied
(unknown-extent pointee); that frame arena is leaked for the pointer's lifetime
```

A leak per certificate is survivable; a warning nobody can act on is not, and the
same codec path is what returns every pointer-carrying value in the program.

**Where.** Emitted at `src/runtime/codec.mx:306`. The condition is "a pointer into
a transient frame arena is being preserved, and the pointee's extent is unknown",
i.e. the value was encoded/relocated as a pointer without a type descriptor that
says how many bytes it owns.

**Reproduce** (write it as `/tmp/xc/main.mx`, build and run):

```moxie
package main

import "crypto/ecdsa"
import "crypto/elliptic"
import "crypto/rand"
import "crypto/x509"
import "crypto/x509/pkix"
import "math/big"
import "time"

func main() {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader())
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1234),
		Subject:      pkix.Name{CommonName: "moxie test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, cerr := x509.CreateCertificate(rand.Reader(), tmpl, tmpl, &key.PublicKey, key)
	if cerr != nil {
		println("create failed:", cerr.Error())
		return
	}
	cert, perr := x509.ParseCertificate(der)
	if perr != nil {
		println("parse failed:", perr.Error())
		return
	}
	println("len", int32(len(der)).String(), "cn", cert.Subject.CommonName)
}
```

`MOXIEROOT=$PWD ./moxie build -o /tmp/xcp /tmp/xc && /tmp/xcp` prints
`len 302 cn moxie test` on stdout and the warning on stderr.

**Shape.** Two candidate fixes, in order of preference:

1. Find the value whose extent is unknown and give it a known one - a
   `*T` where `T` is a `Named` struct should already be relocatable; a pointer to
   an incomplete/parametrized type (`publicKeyInfo`, `pkix.AlgorithmIdentifier`
   with a type parameter, `asn1.RawValue`) is the likely culprit. Register the
   type descriptor at the site that boxes it (see `e.collectTypeDesc` in the
   emitter and how `unsafe.TypeOf` is used in `src/crypto/x509/x509.mx`).
2. If the pointee is genuinely extra-large or recursive, copy it into the
   destination arena in the codec (`codecEncodeValue`'s pointer case) so the
   "preserved" branch is not taken.

Do not silence the warning; it is the only instrument that shows this class.

**Acceptance.** The probe above prints no warning, `cert.Subject.CommonName`
round trips, and calling it 100 times does not grow the root arena (compare
`runtime.ReadMemStats` or `mstats` before and after if a number is wanted).

**Round 291 state**: the warning is gone (round 288's fast path), and the leak
was two bugs, both found with the trace-gated ledger now in `arena_scope.mx`
(`fnarena`/`sovhome` lines):

1. `sovReleaseArena` cleared the group list instead of running
   `arenaReleaseGroups`, so every group whose receiver lived inside another
   group's data arena (7,109 of 7,915 per certificate) stayed in the receiver
   map forever. Fixed: live groups 7,112 -> 3.
2. `sovGroupFreeMax` was 32 while a call builds ~800 groups, so the root-arena
   free list was smaller than the peak. Raised to 4096.

Measured after both: 100 certificates grow the root arena 35.7 MB (was 103 MB).
The remainder was `sovGrowRecs` re-growing the biggest receiver's record array
(64 KB and 128 KB `sovRootAlloc`s) once per call; a third fix parks that array
between groups (`sovParkedRecs`), which takes it to **13.3 MB per 100 calls**
(133 KB per call). **Still open**: the last 133 KB per call is spread over small
root-lifetime bookkeeping and record arrays below the park threshold, and the
size histogram no longer accounts for all of it - the next instrument is
call-site attribution (a PC per root allocation), not another size table.

---

## W3. bufio.Scanner passes the buffer by window

**Why.** Measured: 4 MiB of 64-byte lines through `bufio.Scanner` takes **711 ms**
(Go: ~20 ms), because `Scanner.Scan` calls `s.split(s.buf[s.start:s.end], ...)`
and that window is a copy - up to 64 KiB per token. `mime/type_unix.mx` scans
`/etc/mime.types` with it, so it is on the HTTP path.

**Where.** `src/bufio/scan.mx`: the `SplitFunc` type, `ScanBytes` (line ~307),
`ScanRunes` (~322), `ScanLines` (~368), `ScanWords` (~413), and the `Scan` method
that calls the split function.

**Shape.** Change the contract to offsets:

```moxie
type SplitFunc func(data []byte, start, end int32, atEOF bool) (advance int32, token []byte, err error)
```

`data[start:end]` is the live region. Inside the four functions replace
`bytes.IndexByte(data, '\n')` with `bytealg.IndexByteFrom(data, start, '\n')`
(bounded by `end`), and for runes add `utf8.DecodeRuneAt(p []byte, off int32)`
next to the existing `utf8.EncodeRuneAt`. `token` stays a slice - the token
itself legitimately becomes a copy the caller keeps, and it is short.

Callers: `src/internal/trace/raw/textreader.mx:32`, `src/mime/type_unix.mx:40`
and `:85` use the default split, so they need no change. No custom `SplitFunc` is
defined anywhere in the tree; `grep -rn "SplitFunc" --include=*.mx src/` proves it
before you change the signature.

**Acceptance.** A probe that scans 4 MiB of 64-byte lines measures < 50 ms
(711 ms before), and a corpus comparison against Go produces the same token
bytes (write the corpus to a file, scan it in both, checksum the lines).
`tests/quickcheck.sh --fast` stays green.

---

## W4. container/ring does not compile - DONE (commit 703be634)

`src/container/ring/ring.mx` declared `Value any` and `Do(f func(any))`, and
Moxie has no `any`: the package failed with `undefined: any`. Nothing in the
tree imported it (`grep -rn "container/ring" src/ pkg/ tests/` found only
itself), and it is deleted - a heterogeneous container has no shape in a
language without an empty interface. OPEN_WORK records it as the tree's last
`any`.

---

## W5. Read-side windows that cost allocations (low priority)

- `src/crypto/tls/conn.mx:390,418`: `payload = payload[explicitNonceLen:]` per
  record. Correct (a copy of the payload is what gets decrypted), but it
  allocates per record. Convert to an offset if W1 shows record-path allocation
  growth; otherwise leave and record why.
- `src/debug/gosym/pclntab.mx`: `return b[pos:]`, `t.Data[8+t.ptrsize:]`,
  `t.functab[functabsize:]`, `t.filetab[fileoff:]` and the two
  `bytes.IndexByte(t.funcnametab[off:], 0)` calls. These run per function in a
  symbol table, so a big binary pays O(n²). Convert the per-symbol ones to
  offsets; the one-shot table base slices can stay.
- `src/unicode/norm/normalize.mx:615` `rb.out[:p]` per flush; `iter.mx` cursors.

**Acceptance.** `0a/gosym_pclntab` passes, and a probe that dumps the pclntab of
a large binary shows no per-symbol growth in arena bytes.

---

## W6. float64 Stringer prints 15 digits and an exponent

`println(1.5)` prints `+1.500000e+000`. `fmt.Sprintf("%v", 1.5)` prints `1.5`
(fmt has its own shortest-round-trip formatter), so this is cosmetic - it shows
up in debug output and in any `String()` that uses the runtime's formatter.

**Where.** `src/runtime/numfmt.mx`: `Float32Text` (line ~85, 7 digits) and
`Float64Text` (~145, 15 digits).

**DONE (round 293).** The premise was wrong: the runtime CAN import `strconv`.
Nothing in strconv's imports (math, mxutil) imports the runtime, so there is no
cycle, and the bootstrap self-hosts with the dependency in place. `Float64Text`
and `Float32Text` are now one `strconv.FormatFloat(v, 'g', -1, bits)` call each,
which is the same formatter `fmt` uses - one float formatter in the tree
instead of two, and the Ryu port this item proposed would have duplicated it.

**Acceptance.** A table of ~40 values (`0.1`, `1.5`, `1/3`, `1e21`, `1e-7`,
`123456789.123456789`, `math.MaxFloat64`, `math.SmallestNonzeroFloat64`,
integers, negatives, denormals) prints identically to
`strconv.FormatFloat(v, 'g', -1, 64)` in Go 1.25.3.

---

## W7. The array campaign: one header, negative capacity

Round 262 rewrote this item around three groups it called "load-bearing
exceptions" - the ABI structs, the fiat/nistec field elements, and the array
view. That was wrong, and this replaces it: none of the three is an exception,
because an array's accounting does not have to be invented: it is the slice
header's. An array is a slice header - (base, len, cap) - carrying a **negative
cap**, and the negative capacity is the whole mechanism:

- `cap(arr)` is negative, so nothing can grow it, and `push`, `pop`, `resize` and
  `append` on it are **compile errors**, not runtime checks.
- `len(arr)` is fixed exactly as the type says; indexing is base + i*elemsize
  bounded by len, the same code a slice indexes with.
- The header travels anywhere a value travels: a parameter, a struct field, a
  return value, a channel element. There is no separate array ABI to build,
  convert, or account for.

What that says about the three groups round 262 called exceptions:

**The ABI.** A kernel struct mirror does not get a size change it cannot account
for - it gets the accounting spelled out. The 24-byte header is the mirror's
field where Go had `[16]byte`, and the `//sys` boundary reads `base` and `len`
for the buffer it needs, with the negative cap meaning "this extent is fixed".
Where the kernel fills bytes *in place* in a passed struct, the storage the
header points at is what is passed, and the mirror records that offset - which is
what a mirror is for. 99 sites of bookkeeping, no exception.

**The fiat/nistec field element.** A header is not an allocation. The element is
`[4]uint64` of storage with a header whose cap is negative; the *owner* (the
element's containing struct or the caller) allocates the 32 bytes once, as it
does today inline, and the limbs are loads through that base. There is no
per-limb allocation and no per-element allocation, and the bounds check is the
same one a slice index already costs. 24 sites of accounting.

**The view.** `arr[:]` stops being an aliasing exception and becomes the cap-sign
transition: the same base, len = len(arr), cap = +len(arr). It is a genuine slice
of the same storage, which is why the 329 `Read(fd, b[:])` sites keep working -
the callee writes into the same bytes because there was never anything to copy.
`arr[a:b]`, in contrast, is a sub-slice and copies, exactly like on a slice. The
copy rule then reads: *a sub-slice copies; a fixed-extent header's whole range
yields the same storage with a length-changeable cap.*

Inventory for W7a, counted rather than guessed (the campaign has a map now):

    stage4 (both the representation and every probing site)
      ssa_builder.mx    24   buildArrayLit, emitAlloc of an array, index
      ir_emit.mx        13   indexing, len/cap, array-to-slice
      ir_descriptors.mx  8   KindArray in the runtime type descriptor
      ir_iter.mx         4   range over an array
      ir_call.mx         4   array parameters/returns
      ssa_types.mx       3   Array type itself
      ir_binop.mx        3   comparison/copy of array values
      ir_typecheck.mx    2   the `[...]T` length rule
      ir_types.mx        1   llvmType's `[N x T]` branch (already returns the
                             slice for Len < 0, which is the shape to keep)
      ir_deepcopy.mx, ir_chan.mx, main.mx   1 each
    legacy
      compiler.go 24, interface.go 11, restrict.go 10, llvm.go 8, map.go 5,
      channel.go 4, wasmspawn.go 3, sizes.go 2, interp/memory.go 9
    runtime
      KindArray in 6 files (alloc, codec, hashmap, interface, typekind,
      type_render), 15 sites

Order of work, all bilateral (legacy first, then stage4):

- **W7a - representation.** `llvmType([N]T)` becomes the slice header (ptr to
  N*sizeof(T) bytes, len = N, cap = -N); array literals allocate their storage in
  the current arena and build the header; array-typed struct fields become header
  fields. This is the change everything else follows from, and it is where the
  two compilers must agree byte for byte.
- **W7b - indexing, len, cap.** Indexing through base+offset with the len bound;
  `len(arr)` reads the header; `cap(arr)` returns the negative value.
- **W7c - the refusals.** DONE in stage4 (round 294): `push`, `pop` and
  `resize` on an array are compile errors naming the negative capacity. Legacy
  already refused all three through go/types ("invalid push: argument must be a
  slice"), so only stage4 had the hole - and it was a silent one: `push(arr, v)`
  compiled and dropped `v`. Still open here: `append` does not exist in the
  language at all, and assignment to `len(arr)` (not expressible today) would
  need the same message if it ever becomes one.
- **W7d - the whole-range transition.** `arr[:]` produces the same base with a
  positive cap; `arr[a:b]` copies. Remove "an array base is still a view" from
  the copy rule's documentation, because it is no longer an exception - it is the
  same header.
- **W7e - the two accounting groups.** syscall mirrors (99) and fiat/nistec (24):
  spell the header out, pass base+len at the boundaries, keep the storage
  owner-allocated. No group is exempt.
- **W7f - the rest.** The remaining array sites convert to slices during the same
  pass, since an array literal and a slice literal now have the same
  representation; only the cap sign differs.

**Acceptance.** An array is a header in both compilers; `push`/`pop`/`resize` on
one is a compile error from either; `arr[:]` and a slice of the same bytes are
indistinguishable at runtime; the array fixtures (`abi_arrays_pipe_times`,
`array_elem_addr`, `array_of_slices_layout`, `nested_array_field_addr`,
`ptr_recv_array_elem`, `ptr_to_array_pun`) pass or become reject fixtures with the
named error; quickcheck green; full sweep green.

## W22. The -O2 pipeline's cost bound (rounds 288-289)

stage4 optimizes each package's bitcode with `opt -passes=default<O2>` and
compiles the object with `-O2` (round 288). That is what the legacy compiler
always did, and it is a 3x runtime win. It also has one pathological shape and
one tuning question, and both were measured on `x/text/unicode/norm` because
that package is the worst case in the tree:

- **Pathological shape.** A function that *carries* a table-sized array by
  value (>= 512 elements) is emitted `noinline optnone` (round 289). SROA
  scalarizes the whole-aggregate load and the following passes work on a
  38,000-instruction block: without the attribute that one package needed
  10 GB and minutes; with it, 304 MB and 26 s. Everything else in the tree is
  optimized normally. Do not remove the attribute without re-measuring that
  package.
- **Module budget.** `optBc` runs the O2 middle-end only on modules under
  6 MB (`optBcBudget`); above that it runs `mem2reg`, which is what the pre-288
  tree did. Round 290's numbers: `opt` on net/http (18 MB) reached 10.7 GB and
  never finished, while the runtime and bufio - every per-call win comes from
  them - are under the budget.
- **Object stage is llc, and only llc.** clang -c -O2 re-runs the middle-end
  (38 s per module, 97 min for the netsmoke server), clang -c -O0 costs the
  win (1.5x on every bench). `llc -O2` is the same backend without the
  optimizer: clang -O2 quality, 2.2 s for the runtime module. build.sh checks
  llc's major beside clang's.
- **Parallelism is blocked by a correctness bug, not the memory budget.** `-j 4`
  on the netsmoke server fails in 39 s with `cannot use value of type
  crypto/internal/fips140/ecdh.PublicKey as crypto/internal/fips140.PublicKey`
  and `parallel build failed` (peak RSS 4.0 GB, so not resource exhaustion).
  Serial builds of the same tree are green, so the parallel package path gives
  the same named type two identities when packages are checked concurrently.
  Until that is fixed the build is serial, and a cold netsmoke server build is
  25.7 min (peak 3.67 GB); a warm one is 0.6 s, so the cost is per package
  content, not per build.

**Accept:** a cold `bash tests/unitrun.sh --phases 8 -j 1 --force` completes
inside its timeouts with peak RSS under 2 GB per worker, and the bench numbers
of round 288 hold (W13, W14, W17).

## W23. Parameter immutability is documented but not enforced (round 292)

The memory model says parameters are immutable borrows and that writing through
a parameter pointer is a compile-time error. Measured: it is not. Both
compilers accept `p.x = 7` through a `*T` parameter and `n = 5` on an `int64`
parameter, and neither `legacy/compiler/restrict.go` nor stage4's checks have a
parameter rule at all. The tree depends on the current behaviour - W10 records
570 pointer-parameter field writes - so this is a migration, not a bug fix.

**Decision needed**: either implement the rule and migrate those 570 sites (a
campaign of its own, and one that would make every callee-that-fills-a-struct
return the value instead), or correct CLAUDE.md to describe the model as it is
and stop deriving arena arguments from immutability. Until one of the two
happens, no arena reasoning may assume a callee cannot write through a
parameter.

## W8. Opportunistic cleanups (do while in the neighbourhood)

- `src/math/big/decimal.mx` `rsh(x *decimal, s)`: writes `x.mant` through a
  pointer parameter and can allocate there. Safe today because the capacity
  suffices; harden by writing into the caller's buffer by index only, or by
  returning the new length.
- Re-run phase 8 (`tests/unitrun.sh -j 1 --phases 8 --force`, ~1.5 h of builds):
  it found the RFC 6724 literal bug in round 248, which is fixed, so it should
  now pass and becomes a regression net for the socket/spawn path.
- `docs/OPEN_WORK.md` still says the `any` campaign covers `iter`, `weak`,
  `fips140cache`, `maps`, `slices`, `bytes`, `strings`, `trace`. Measured: all of
  them compile; the only real remaining `any` is `container/ring` (W4). Correct
  that page.

---

**W9 — the post-quantum hybrid key share** (found by W1, round 253). With the default `CurvePreferences` the client's 1216-byte share is 1184 bytes of ML-KEM encapsulation key followed by zeros where the 32-byte X25519 share belongs, and the server answers `tls: invalid client key share`. The bytes are correct immediately before `push(mlkemBytes, ecdhShare...)` and zero after it, with `len == cap == 1184` on the destination (the growth path). Round 261 measured arena ownership at the push: both the 1184-byte destination and the 32-byte source are in the SAME frame arena as the push (where=7, top=7), the source's first byte is live immediately before, and the appended 32 bytes are zeros after. So neither the dead-frame aliasing of W10 nor a plain growth-copy failure explains it, and the same shape copies correctly in isolation (both with a parameter source and a local one). The remaining difference: the destination comes from an interface method chain (`priv.mlkem.Encapsulator().Bytes()`). Start there. Accept: the fixture with `CurvePreferences` unset passes.

**W3b — where the scanner's remaining 3.8 us per call goes** (found by W3, round 255). The window copies are gone and 4 MiB of 64-byte lines went 711 ms -> 356 ms, but the search-and-copy work is 29 ms, so ~327 ms is per-`Scan` overhead that does not scale with token size. Ruled out by probe: the reader, method calls, nested store-backs, function values, buffer size. Left: the sovereign-arena acquire and the store-back walk inside `Scan`. The benchmark recipe is in round 255. Accept: a repeat of that benchmark under 50 ms.

**W10 — a callee that writes a slice through a pointer parameter leaves it in its own frame — DONE (round 259)** (found by W1's verification case, round 256). `x509.processExtensions(out *Certificate)` builds `ExtKeyUsage`, `Policies`, the key ids and the name constraints with `push` and stores them through the parameter; nothing relocates them at its return, so the outer encode sees a pointer into a released arena, classifies it as an ancestor's, and aliases it. `x509.Verify` then reads it and dies. Reproducer: parse a leaf with an EKU extension and verify it against its CA in ONE domain (`/tmp/xcv` shape in round 256). Fixed by making the callee work on a copy it owns and return it: the codec emits an ancestor-arena pointer as stable and never walks the fields the callee wrote into it, so the writes have to live in the callee's own arena and come back as a return value. `parseNameConstraintsExtension` returns a struct for the same reason. Accept met: 20 verifications, chains=1 len=2, and the TLS fixture's wrong-CA case is back (round 259). Still open as a class: 570 pointer-parameter field writes exist and only the two a test could see are fixed.

**W11 — the routing tree loses its patterns** (round 260). phase 8 is red; `tests/netsmoke/muxmatch` fails 7 of 10 shapes with `pattern=[]` while the handler resolves, so matching works and the stored pattern reads empty. Three frame-lifetime hypotheses (root-owned child keys, root-owned pattern struct, rebinding into a new local) were each built cold and changed nothing; they are reverted. What is now measured: registration stores the child, `findChild` finds it at match time (`hit=true`), and reading the matched node's `pattern.str` faults - so the leaf's pattern pointer is into released memory. Four candidate fixes (root-owned keys, root-owned pattern, rebinding into a new local) were each built cold and changed nothing, and one further build that printed `n.pattern.str` in `matchPath` segfaulted at that read. Next: print node/pattern/str at `set` time and again in `matchPath`, and check `ServeMux.register` for its own copy of the pattern. Accept: `muxmatch` prints `ok` for all ten shapes, then phase 8 green.

**W12 — a fixture the compiler rejects was built and passed** (rounds 260-263). Established, with the SAME `./moxie` binary (mtime 08:13, unchanged all session) and the SAME source:

    tests/netsmoke/muxmatch as phase 8 built it     build OK, binary linked, fixture ran
    the same source in a fresh directory, cold      "; probe: package globals are immutable
                                                    outside init ..." -> compile error, exit 1

So the compiler rejects the source, and a phase built it anyway. The cache dir name carries `compilerHash()` (a hash of the compiler's own sources under MOXIEROOT) and `compilerBuildID()` (a hash of the running binary), so "a stale object from an older compiler" does not explain it either - unless the per-package content key, not the directory, is what went stale. Next experiment: build the canonical path cold (`-a`), then in a copy with `-a`, and diff the two logs and the resolved cache dirs; `pkgCacheDir()` and `buildScratchDir("./tmp", "mxc-build")` are the two places to look. Accept: a fixture the compiler rejects fails the phase that builds it, from any directory, with a cold cache.

Round 263 added a second sighting of the same *shape* and no reproducer: one incremental build produced a binary whose slice header was read at the old offset after a `Digest` field was added, `-a` was correct, and a clean rebuild of the same change recompiled the dependents properly and ran correctly. The state that caused it was cleared before it could be repeated. Keep both sightings in view.

**Round 272 closed the one place a dependent's key could lose its dependency**: `pkgHash` dropped an import the memo did not hold instead of computing it, and not every call site hashes in dependency order. The key now resolves the import through `pkgByPath` and recurses, with `"|?" + path` only for an import that is no package at all. That does not *explain* either sighting (no key was captured), so the state to look for next is a stale object whose key came from somewhere other than `pkgHash`: `pkgCacheKey` hashes the package's own files alone, and `compilerHash()`/`compilerBuildID()` only name the directory.

## W13. time.Now is 4.3 us (found by round 269 while chasing W3b)

`time.Now()` costs 4.3 us; the same-shape codec boundary costs 105 ns and a
vDSO clock read costs tens of ns. Round 269 has the measurement table, the
disassembly (FnArenaPush(64 KiB) + codecEncodeValue/codecDecodeValue per call)
and the ruled-out list. Every timing instrument in this tree pays it, and it
made W3b's earlier probes measure the clock instead of the code.

**Accept**: `time.Now()` under 200 ns and the W3b scanner benchmark (4 MiB of
64-byte lines, 65536 tokens) under 50 ms, both measured with the recipe in round
269. Round 288 met the first (191-197 ns) and missed the second (83 ms): the
scanner's floor is now the package boundary, not un-inlined local code. Profiling needs an in-tree counter: ptrace is blocked in this environment.

**Round 275 split it into three costs** (see W17): the return-value protocol on
every pointer-containing return (640 ns for `time.Local()` returning one stable
pointer), the per-call sovereign group when a receiver is created in the
callee's frame (1-2 mmap+munmap per call), and an unexplained ~3.5 us in the
protocol for a `Time` specifically. `time.Now()` under 200 ns needs all three.

## W14. bufio.Scanner is 5.6 us per token regardless of token size (W3b, unresolved)

See W3b above and round 269: the measurement instrument is now known to be
4.3 us per call, so the earlier attribution to the sovereign arena is void.
Re-measure first, then bisect with counters rather than timers.

**Round 288 re-measured**: 83-84 ms for the 4 MiB fixture (1.27 us/token,
was 5.4) after stage4 stopped emitting -O0 code. The 50 ms target is unmet and
what is left is the per-call work that crosses a package boundary (the runtime
is a separate object, so its helpers are still real calls).

**Round 275 rules out the mmap churn for the scanner**: 65536 tokens with 66
mmaps in the whole run, so the 5.6 us per token is CPU on the per-call path
(sovereign borrow, store-back, return protocol), not syscalls. W17 is the list
of per-call costs to cut.

## W15. The compiler needs 7.5 GB to build the net/http closure (found by round 270)

`moxie build ./src/net/http` (library target) panics "arena: region table full"
at the root arena's cap (128 regions x 64 MiB = 8 GB), with peak RSS 7.2 GB; the
same closure as a main package peaks at 7.66 GB and passes. Round 270 has the
numbers and the bisect (not the round-268 fix, not the round-271 hoists).

**Accept**: `moxie build -o /dev/null ./src/net/http` completes (the
`undefined symbol: main.main` linker error is the success shape) on a 16 GB
machine with room to spare - which means per-package discovery data is released,
not that the cap was raised.

**DONE (round 274)**: the residency was one function.
`dispFingerprintForMethods` built the text it hashes by string concatenation -
once per invoked method and once per impl - and each `|` allocated a fresh copy
of everything accumulated so far, in the root arena, for every package. 8.7 GB
of the 10.2 GB peak was those abandoned intermediates. It now hashes as it
produces (`fnvState`), with the same byte sequence and therefore the same
fingerprint value.

    tests/netsmoke closure   root arena 10163MB / 157 regions (panic)
                             ->            2599MB /  39 regions
    the 45-package sweep     panicked -> links, and the binary runs
    moxie build -o /dev/null ./src/net/http
                             10163MB and a panic -> 2687MB, linker says only
                             `undefined symbol: main.main`, which is success

Rejected on the way there: giving the root arena a 1024-entry region registry.
It lets the build finish (10163 MB over 157 regions) but retains the same bytes,
which is the ceiling this section already called "not a fix".

**Still residential**, now small enough not to block: `cached-bc` adds ~800 MB
(mem2reg reads every compiled package's bitcode into a frame that outlives the
loop) and the fingerprint phase keeps ~1.1 GB of per-impl identity strings.

## W16. A warm build still re-compiles every object, which is the whole wall time - DONE (round 273)

Build 2 of round 272 had 147 of 147 packages cached, wrote no `.bc` at all, and
still took 1218 s. The time is in `compile objects`: `clangCompileToObj`
(`_mxc_stage4/main.mx`) execs `clang -c <hash>.bc -o <hash>.bc.o` for every
module on every build, and the `<hash>.bc.o` it overwrites is already sitting
next to the `.bc` from the previous build. Nothing checks it, so 147 LLVM
modules - including the generated Unicode tables, which are minutes of clang
each - are recompiled to identical objects.

This is why a one-line change to a fixture costs twenty minutes: the package
cache is fine, the object cache does not exist. It also makes W15 worse: the
compiler holds the whole build's IR while it does the work.

**Accept**: a second build of the same source with nothing changed completes in
under two minutes, i.e. the object for a `.bc` whose hash is unchanged is
reused. The `.o` name must carry what can invalidate it (the clang that produced
it), and `-a` must still discard it. This is a build-cache detail of stage4's
driver, not language semantics, so there is no legacy mirror: legacy's build
cache is cmd/go's and already keys objects on the compiler and its inputs.

**Done, round 273**: object reused when `<hash>.bc.o.stamp` matches the clang
version line, the triple and the flags; `-a` rebuilds and re-stamps; objects are
published by rename; an object is built from the cached copy of the bitcode so
it outlives the scratch directory. `tests/netsmoke/muxmatch`, 148 packages:
cold 1532 s, warm **42 s**, byte-identical binaries. The next warm-build item is
that the root package is never cached (1 of 148 compiled every time), which is
the 42 s.

## W17. A method call on a frame-local receiver, and the return-value protocol (found by round 275)

Round 275's table is the evidence; the three costs are independent and each has
its own fix.

0. **Round 288 measured the whole item again at O2** (stage4 had been emitting
   -O0 objects; the legacy compiler never did): `t.Add(0)` 436-453 ns (was
   2640), `t.UTC()` 525-583 (was 2990), and `strace -f -c` shows six mmaps for
   the whole program - the per-call mmap churn is gone. The 300 ns accept is
   still missed, and the remainder is the package boundary (see round 288).
1. **Sovereign group per call when the receiver lives in the callee's frame.**
   `codecSovereignArena` keys groups by receiver address; a receiver created
   inside the callee has a new address every call, so every call runs the create
   path: `sovNewArena(8192)` mmaps an 8 KB arena, registers its range, inserts
   into the root hashmap, and munmaps it when the frame dies. Measured with
   strace: `t.Add(0)` 200,000 mmap + 200,000 munmap for 200,000 calls (11 us
   each), `t.UTC()` 400,000 of each (16.8 us), `time.FixedZone` 30 us. The same
   mutating method on a receiver that outlives the call is 10 ns. **Accept**:
   `t.Add(0)` under 300 ns and no mmap in `strace -f -c`. The fix has to keep the
   model's guarantee (the arena lives as long as the receiver) while not paying
   an mmap for a receiver whose whole life is one call - the group's data arena
   comes from the arena pool, and the group is recycled rather than leaked when
   the frame dies.

2. **The return-value protocol is entered for every pointer-containing return**
   even when the value needs no relocation: `time.Local()` returns one stable
   pointer and costs 640 ns, of which every step is visible in its disassembly
   (FnArenaPush, codecBufInit, codecSeenInit, codecSetRetTransient x2,
   codecEncodeValue, codecDecodeSeenInit, codecDecodeValue, codecEscapeOccurred,
   FnArenaPop, ArenaRelease). A local struct with a pointer to a scalar global
   costs 15 ns, so the difference is the protocol, not the pointer.
   **Accept**: a function returning one stable pointer under 50 ns, and
   `time.Local()` under 100 ns. This is codegen in both compilers (wood law): a
   shallow "does any pointer field point into the current frame" test, with the
   codec kept for the case that answers yes.

3. **`time.Now()`'s own 3.5 us is not yet explained.** It has no mmap and
   nothing to copy, and with `loc` set to nil it is still 3.3 us, so it is in
   the protocol for a `Time` specifically - five times the same protocol for a
   bare pointer. **Accept**: explained, then folded into (2).

Order: (1) first, because it is contained in the runtime and it is 10-30 us per
call on every mutating method that builds its receiver; then (2) with the
bilateral codegen change; then re-measure W14's scanner bench, which round 275
showed is CPU on this same path.

**First attempt at (2), parked with what it learned**: the branch belongs in
`emitRetWithCodec` in `_mxc_stage4/ir_deepcopy.mx`, after the `needsCodec` test,
guarded by a runtime `codecRetStable(value, typeDesc)` that answers "every pointer
this value holds is nil or stable by identity" - mirroring
`codecEncodeValue`'s own per-field decisions in return mode, including the rule
that a slice whose elements can hold pointers is walked even when its backing is
stable. Written that way it emitted the check for interface results (12 sites in
`time`) but NOT for `time.Local`'s named single pointer result, and the bench was
unchanged (640 -> 665 ns), so the work is unverified and was reverted rather than
committed half-done. The next attempt should start by dumping the emitted IR for
a function with one NAMED pointer result and finding which of `emitReturn`'s
branches produced its epilogue - the block is reachable, so it is a routing
question, not a design question.

## W18. The net package's IP layer (found by round 277, mostly fixed by round 278)

Round 277 fixed `parseFile.readLine`, which is why /etc/hosts, /etc/resolv.conf,
/etc/nsswitch.conf and /etc/services parse at all now. Behind it:

1. **`net.IP.String()` returns the raw 16-byte form.** Not fixable, and now
   documented: `IP` is an alias for `[]byte` because the language has no named
   slice types and an alias cannot carry methods, so the method set is free
   functions (`net.IPString`, `IPIsLoopback`, ...). `ip.String()` compiles and
   calls the *builtin* `[]byte` Stringer instead. Ported code must call
   `net.IPString(ip)`. Anything that can warn about it (a lint pass) is worth
   more than a fix here.
2. ~~**`Dial` on a hosts name answers `no suitable address found`**~~ - fixed by
   round 278: it reaches the connect now.
3. ~~**`LookupIP`/`Dial` on a DNS name die with SIGSEGV**~~ - fixed by round 278:
   `lookupIPCall.Call` returned the address of its own local, and singleflight
   dereferenced it from another frame after the arena was released. The value is
   published in the root arena now, which also means a resolved name's addresses
   are never reclaimed - the remaining item here.

**Open**: the root-arena residency of resolved addresses (bounded per distinct
name, unbounded over a long-running process resolving many names - W2's leak
class), and phase 8 re-run to confirm the whole netsmoke passes.

## W19. Phase 8's spawned-mode handlers answer nothing - DONE (round 279)

Fixed, and the cause was worth the hunt. The queue boundary is a MAP_SHARED ring
of preallocated slots, and both sides named one slot with a bounded sub-slice
(`chanSlotBuf[base : base+slotBytes]`), which **allocates and copies** in this
language. The shell wrote the request into its private copy, the worker decoded
an untouched ring, rejected the frame and wrote an abort the shell discarded, so
the client saw a connection closed with no response. `chanSlotWindow` now builds
a real window with `unsafe.Add`.

Phase 8 after the fix: 14 of 18 pass in both modes, including `client-spawned`
(the whole Moxie client stack against a worker in another domain). The four
that remain are W20.

## W20. The sync path's response body reads back as zeros - DONE (round 280)

`io.LimitedReader.Read` truncated the caller's buffer with `p = p[0:l.N]`, and a
bounded sub-slice ALLOCATES AND COPIES here: the reader wrote the body into the
copy, the caller's buffer kept its zeros, and the count was still right because
it came from the copy's length. `SectionReader.Read`/`ReadAt` had the same three
lines. `io.readWindow` returns a VIEW (`unsafe.Slice(unsafe.SliceData(p), n)`)
and all three use it. Phase 8 is 18 of 18: sync, Transport, and both modes'
no-accumulation units.

## W21. Read destinations passed as bounded sub-slices - DONE (round 280)

The compiler refuses `copy(x[a:b], src)` by name, but it cannot refuse
`f(x[a:b])` for a callee that writes through its parameter, and that is the shape
that cost two rounds of phase 8. Measured semantics: `array[a:b]` is a VIEW,
`slice[a:b]` is a COPY, bounded or not.

The sweep found exactly one live site: `archive/tar/reader.mx`'s `tryReadFull`
read a tar stream into `b[n:]` (a `[]byte` parameter's sub-slice). It uses
`io.ReadIntoN` now. Everything else is a full range or an array slice -
`crypto/internal/fips140/drbg`'s `additionalInput[:16]` is a pointer to an array.
A grep for `Read(|ReadFull(|ReadAt(|copy(` with a bounded sub-slice argument finds
only comments.

Not covered by a fixture: tar's partial-read path is not in phase 0a/8, so the
accept here is the grep plus the compile. A tar fixture would be the honest
completion.

## Suggested order

| # | item | why now | cost |
|---|---|---|---|
| 1 | ~~W4 ring~~ | done (703be634): the package is gone | — |
| 2 | ~~W1 TLS handshake~~ | done (rounds 253, 259): record layer, echo, and the wrong-CA case | — |
| 3 | ~~W16 object cache~~ | done (round 273): warm builds 36x faster, which makes every later item cheaper | — |
| 4 | ~~W15 compiler memory~~ | done (round 274): a concatenation built the fingerprint, not a data structure | — |
| 5 | ~~W18 net IP layer~~ | done (round 278): DNS/Dial crash fixed, hosts and services parse; the leak and the `IP.String` trap remain | — |
| 5b | ~~W19 spawned dispatch~~ | done (round 279): a sub-slice of the shared ring was a copy | — |
| 5c | ~~W20 response body zeros~~ | done (round 280): a destination sub-slice was a copy; phase 8 is 18/18 | — |
| 5d | ~~W21 destination sub-slices~~ | done (round 280): one live site (tar), swept and fixed | — |
| 6 | W17 per-call costs | 10-30 us per mutating call that builds its receiver; W13, W14 and W3b are all this | 1-2 sessions |
| 7 | W13 time.Now | needs W17 (1) and (2); the remaining 3.5 us is unexplained | after W17 |
| 8 | W2 codec escape warning | leaks per certificate, instrument for a class of bugs | 1 session |
| 9 | W3/W14 scanner | 5.6 us per token, re-measure with counters first | half session |
| 10 | W5 read windows | allocation only | half session |
| 11 | W6 float Stringer | cosmetic, self-contained | half session |
| 12 | W7 array campaign | the last big one; representation first (W7a), one header with a negative cap, no exceptions | multi-session |
| 13 | W8 cleanups | opportunistic | - |

After each item: `bash tests/quickcheck.sh`, commit, push. After W7c: the full
sweep, then a release tag.
