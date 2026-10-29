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
- An array *base* is still a view; arrays are the one carve-out left, and W7
  removes them. `[16]byte{...}` written as `[]byte{...}` keeps only the bytes
  spelled out - round 248 fell into that.
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

## W4. container/ring does not compile (trivial)

`src/container/ring/ring.mx` declares `Value any` and `Do(f func(any))`, and
Moxie has no `any`: the package fails with `undefined: any`. Nothing in the tree
imports it (`grep -rn "container/ring" src/ pkg/ tests/` finds only itself).

**Decision.** Delete it: `git rm -r src/container/ring`, and note in
`docs/OPEN_WORK.md` that a heterogeneous container has no shape in a language
without an empty interface - a Ring of T needs `Ring[T C]` with a concrete
constraint, and there is no caller to choose C for.

**Acceptance.** Package gone, no references, quickcheck green.

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

**Shape.** The runtime cannot import `strconv` (cycle). Port the shortest
algorithm: `src/strconv/ftoaryu.mx` already has `ryuFtoaShortest` and its
support tables; a self-contained copy in `numfmt.mx` needs only integer ops on
the mantissa/exponent and a small powers-of-five table. Keep the special cases
(NaN, ±Inf, zero, negative zero) as they are.

**Acceptance.** A table of ~40 values (`0.1`, `1.5`, `1/3`, `1e21`, `1e-7`,
`123456789.123456789`, `math.MaxFloat64`, `math.SmallestNonzeroFloat64`,
integers, negatives, denormals) prints identically to
`strconv.FormatFloat(v, 'g', -1, 64)` in Go 1.25.3.

---

## W7. The array campaign - revised in round 262 after measuring it

The round-251 version of this item said "convert the 823 sites, then ban the
array type". Measured, that cannot be done and should not be:

    array-type sites, src/ without vendor           522
      package-level vars                             83
      in a function signature                       135
      composite literals                            221
      other                                          83
    sites that slice an array base (`arr[:]`)       329
    syscall ABI structs (ztypes_linux_amd64/arm64)   99
    fiat/nistec field elements (inline math)         24

Three of those groups are load-bearing:

1. **The ABI.** `src/syscall/ztypes_linux_*.mx` mirrors the kernel's struct
   layout. `[16]byte` in a `RawSockaddr` is the ABI; a slice field would change
   the struct's size and every `//sys` call with it. 99 sites.
2. **Inline arithmetic.** A `[4]uint64` field element in fiat/nistec is inline
   storage with no allocation. As a slice field it needs an allocation per
   element and a bounds check per limb, in the hottest code the tree has.
3. **The view.** `arr[:]` is a *write-through view* - measured on a global, a
   struct field and a local array, each `writeFirst(a[:])` leaving 0xAB in the
   array. That is the carve-out in "an array base is still a view", and 329 call
   sites depend on it, most of them the `Read(fd, b[:])` shape where the callee
   writes into a fixed buffer. (`copy(arr[a:b], ...)` is already a **compile
   error**: the compiler refuses a copy destination that is a sub-slice.)

So the array *type* is not the defect; **the view is**. Revised items:

**W7a - done (round 258).** Both compilers already reject an array where a slice
is expected, naming both types, in argument position and in assignment. There is
no compiler-driven checklist for this campaign.

**W7b - convert data holders only.** Arrays that are pure data (coverage
counters, error-code tables, metadata), where the value is never sliced as a
view and never part of a wire/ABI layout. Allocate once in the owner (a
constructor or `init`), not per call. Batch by package, ~50-100 sites per
commit, `tests/quickcheck.sh` after each. Do **not** touch `syscall/ztypes_*`,
`crypto/internal/fips140/nistec/fiat`, or anything whose layout is read by a
syscall.

**W7c - remove the view, not the type.** Make `arr[:]` on an array base a
**copy** (or a named compile error) in both compilers, then fix the 329 sites by
giving each buffer an owner that allocates it once - `[]byte{:N}` in the struct,
or a `resize`-preserved field. This is the change that removes the last carve-out
from the copy rule: every slice expression then copies, and an array is only ever
a value you index. It is also where the fixtures live: the six array fixtures
(`abi_arrays_pipe_times`, `array_elem_addr`, `array_of_slices_layout`,
`nested_array_field_addr`, `ptr_recv_array_elem`, `ptr_to_array_pun`) are the
evidence for how the view is used today, and they become the reject fixtures for
whatever the new rule forbids.

**Acceptance, revised.** No `arr[:]`/`arr[a:b]` on an array base remains in `src/`,
`pkg/`, `_mxc_stage4/`; both compilers reject it with a named error; the two
exceptions (ABI structs, inline field elements) are documented in one place with
the reason; the full sweep is green; quickcheck green. The array *type* count is
allowed to stay above zero, and the reason for each remaining group is written
down.

---

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

Round 263 added a second sighting of the same *shape* and no reproducer: one incremental build produced a binary whose slice header was read at the old offset after a `Digest` field was added, `-a` was correct, and a clean rebuild of the same change recompiled the dependents properly and ran correctly. The state that caused it was cleared before it could be repeated. Keep both sightings in view; the cache-key code path is `pkgHash`/`pkgCacheKey`, and the dependency-hash lookup there skips a dep it has not memoized, which is the one place a dependent's key can lose its dependency.

## Suggested order

| # | item | why now | cost |
|---|---|---|---|
| 1 | W4 ring | 5 minutes, unblocks "everything compiles" | trivial |
| 2 | ~~W1 TLS handshake~~ | done (rounds 253, 259): record layer, echo, and the wrong-CA case | — |
| 3 | W2 codec escape warning | leaks per certificate, instrument for a class of bugs | 1 session |
| 4 | W3 scanner offsets | 14x on a hot path, contained API change | half session |
| 5 | W5 read windows | allocation only | half session |
| 6 | W6 float Stringer | cosmetic, self-contained | half session |
| 7 | W7 array campaign | the last big one; W7a first | multi-session |
| 8 | W8 cleanups | opportunistic | - |

After each item: `bash tests/quickcheck.sh`, commit, push. After W7c: the full
sweep, then a release tag.
