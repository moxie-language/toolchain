#!/bin/bash
# Phase 6: build tags and allocation instrumentation.
#
# -tags and -print-allocs are compiler features whose whole point is the
# binary they produce, so every check here builds AND runs the result. Both
# compilers must behave identically: legacy is the bootstrap reference and
# stage4 has to mirror it.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export MOXIEROOT="$ROOT"
source "$SCRIPT_DIR/util.sh"

LEGACY="${LEGACY:-$ROOT/legacy/moxie}"

# The legacy loader shells out to the Go toolchain for go/types, so the legacy
# side of every comparison needs a real GOROOT. This used to be the hardcoded
# /tmp/moxie-goroot, which is a scratch path: once it was gone every legacy case
# failed with "go: cannot find GOROOT directory", reading as eight compiler
# disagreements that were nothing of the sort. Resolve the toolchain that is
# actually installed, and if there is none, skip the legacy half loudly - a
# comparison with no reference is not evidence.
LEGACY_GOROOT="${LEGACY_GOROOT:-}"
if [ -z "$LEGACY_GOROOT" ]; then
	if command -v go >/dev/null 2>&1; then
		LEGACY_GOROOT="$(go env GOROOT 2>/dev/null)"
	elif [ -n "${GOROOT:-}" ] && [ -x "$GOROOT/bin/go" ]; then
		LEGACY_GOROOT="$GOROOT"
	fi
fi

echo ""
echo "=== Phase 6: build tags and allocation instrumentation ==="

if [ ! -x "$LEGACY" ]; then
	log_skip "phase6" "legacy compiler not built at $LEGACY"
	exit 0
fi

# The legacy compiler is a prebuilt binary and the suite never builds it. A
# stale one silently disagrees with its own source (a changed logAlloc
# signature showed up as an LLVM verifier failure), so say so instead of
# reporting a compiler bug that is not there.
if [ -n "$(find "$ROOT/legacy" -name '*.go' -newer "$LEGACY" -print -quit 2>/dev/null)" ]; then
	log_skip "phase6" "legacy compiler older than its source; run legacy/build.sh"
	exit 0
fi

# fixture <name> copies a fixture directory out of the tree. In-tree
# directories are built in place by the rest of the suite, but these tests
# want a scratch copy so a stray build product never lands in the repo.
fixture() {
	local name="$1"
	local d
	d="$(tempdir "fx_$name")"
	rm -rf "$d"; mkdir -p "$d"
	cp "$SCRIPT_DIR/data/$name"/*.mx "$d/"
	# A fixture that exercises module manifests carries its own.
	if [ -f "$SCRIPT_DIR/data/$name/moxie.mod" ]; then
		cp "$SCRIPT_DIR/data/$name/moxie.mod" "$d/"
	fi
	echo "$d"
}

# 6a: the stage4 regexp matcher must agree with the Go regexp legacy uses.
# tests/tools/regexp_ref is the reference (it must be Go: it calls Go's
# regexp). Cases are a shared file so the two sides cannot drift.
name="regexp:table"
t0=$(date +%s%N)
td="$(tempdir "regexp")"; rm -rf "$td"; mkdir -p "$td"
rxdir="$(fixture regexp)"
rc=0
MOXIEROOT="$MOXIEROOT" "$MOXIE" build -o "$td/rx_moxie" "$rxdir" >"$td/build.log" 2>&1 || rc=$?
if [ $rc -ne 0 ]; then
	log_fail "$name" "stage4 build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
else
	"$td/rx_moxie" "$SCRIPT_DIR/data/regexp/cases.txt" >"$td/moxie.txt" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "case program exited $rc"
	elif ! command -v go >/dev/null 2>&1; then
		log_skip "$name" "go not available for the regexp reference"
	else
		# shellcheck disable=SC2086
		go run "$SCRIPT_DIR/tools/regexp_ref/main.go" "$SCRIPT_DIR/data/regexp/cases.txt" >"$td/go.txt" 2>"$td/go.err" || rc=$?
		if [ $rc -ne 0 ]; then
			log_fail "$name" "reference failed: $(tail -2 "$td/go.err" | tr '\n' ' ')"
		elif cmp -s "$td/go.txt" "$td/moxie.txt"; then
			t1=$(date +%s%N)
			log_pass "$name" "$(wc -l < "$td/moxie.txt") cases in $(( (t1 - t0) / 1000000 ))ms"
		else
			log_fail "$name" "first difference: $(diff "$td/go.txt" "$td/moxie.txt" | head -3 | tr '\n' ' ')"
		fi
	fi
fi
rm -rf "$td" "$rxdir"

# 6b: -tags selects a //:build-gated file, in both compilers, including the
# comma-separated and negated forms.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	td="$(tempdir "tags_$label")"; rm -rf "$td"; mkdir -p "$td"
	tdir="$(fixture tags)"
	for spec in "none:plain" "probe_tag:tagged" "a,probe_tag:tagged" "!probe_tag:plain"; do
		tagset="${spec%%:*}"
		want="${spec##*:}"
		name="tags:$label:${tagset}"
		args=()
		[ "$tagset" = "none" ] || args=(-tags "$tagset")
		rc=0
		MOXIEROOT="$MOXIEROOT" "$cc" build "${args[@]}" -o "$td/bin" "$tdir" >"$td/build.log" 2>&1 || rc=$?
		if [ $rc -ne 0 ]; then
			log_fail "$name" "build failed: $(tail -2 "$td/build.log" | tr '\n' ' ')"
			continue
		fi
		got="$("$td/bin" 2>&1 | head -1)"
		if [ "$got" = "$want" ]; then
			log_pass "$name" "ok"
		else
			log_fail "$name" "got '$got', want '$want'"
		fi
	done
	rm -rf "$td" "$tdir"
done

# 6c/6d: allocation instrumentation. The site table must list the matched
# function, the runtime must count that site, and without the alloc_trace tag
# the same call must reach the no-op stub instead.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi

	# instrumented build: counters fire
	name="allocs:$label:matched"
	td="$(tempdir "allocs_$label")"; rm -rf "$td"; mkdir -p "$td"
	adir="$(fixture allocs)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -tags alloc_trace -print-allocs allocA \
		-o "$td/bin" "$adir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		msg=""
		[ -f "$td/bin.alloc_sites.json" ] || msg="no alloc_sites.json"
		if [ -z "$msg" ]; then
			sites=$(grep -o '"id":' "$td/bin.alloc_sites.json" | wc -l)
			[ "$sites" -eq 1 ] || msg="expected 1 site, got $sites"
		fi
		if [ -z "$msg" ] && ! grep -q '"name":"[^"]*allocA' "$td/bin.alloc_sites.json"; then
			msg="site table does not name allocA: $(cat "$td/bin.alloc_sites.json")"
		fi
		if [ -z "$msg" ]; then
			out="$("$td/bin" 2>&1)"
			got="$(echo "$out" | grep -c 'allocsite .* count=[1-9]')"
			[ "$got" -ge 1 ] || msg="no site counted: $(echo "$out" | tr '\n' ',')"
		fi
		if [ -z "$msg" ]; then
			log_pass "$name" "$(echo "$out" | grep allocsite | head -1)"
		else
			log_fail "$name" "$msg"
		fi
	fi

	# matched function is the only thing instrumented
	name="allocs:$label:selector"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -tags alloc_trace -print-allocs allocB \
		-o "$td/binB" "$adir" >"$td/buildB.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/buildB.log" | tr '\n' ' ')"
	elif ! grep -q '"name":"[^"]*allocB' "$td/binB.alloc_sites.json"; then
		log_fail "$name" "site table does not name allocB: $(cat "$td/binB.alloc_sites.json" 2>/dev/null)"
	else
		out="$("$td/binB" 2>&1)"
		line="$(echo "$out" | grep allocsite | head -1)"
		if [ -n "$line" ] && echo "$line" | grep -q 'bytes=32'; then
			log_pass "$name" "$line"
		else
			log_fail "$name" "expected a 32-byte site, got: $(echo "$out" | tr '\n' ',')"
		fi
	fi

	# untagged build: the same call reaches the stub, so nothing is counted
	name="allocs:$label:untagged"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -print-allocs allocA \
		-o "$td/binU" "$adir" >"$td/buildU.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/buildU.log" | tr '\n' ' ')"
	else
		out="$("$td/binU" 2>&1)"
		if echo "$out" | grep -q allocsite; then
			log_fail "$name" "untagged build still counted: $(echo "$out" | grep allocsite | head -1)"
		else
			log_pass "$name" "no counters without -tags alloc_trace"
		fi
	fi

	# an unusable pattern is rejected before anything is compiled
	name="allocs:$label:bad-pattern"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -print-allocs 'a(' \
		-o "$td/binBad" "$adir" >"$td/bad.log" 2>&1 || rc=$?
	if [ $rc -eq 0 ]; then
		log_fail "$name" "unbalanced pattern accepted"
	else
		log_pass "$name" "rejected"
	fi

	rm -rf "$td" "$adir"
done

# 6e: package-level float globals keep their initializer in both compilers.
# The legacy interp read a constant float with LLVMConstRealGetDouble, whose
# double return travels in XMM0 while the purego binding read RAX, so every
# package-level float32/float64 variable was silently initialized to zero
# (integers return in RAX and were fine).
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="globalinit:$label"
	td="$(tempdir "globalinit_$label")"; rm -rf "$td"; mkdir -p "$td"
	gdir="$(fixture globalinit)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$gdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "package-level float globals initialized"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$gdir"
done

# 6f: len()/cap() on a channel report the ring in both compilers. stage4's
# len/cap lowering had no channel arm and returned zero, while legacy called
# runtime.chanLen/chanCap.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="chanlen:$label"
	td="$(tempdir "chanlen_$label")"; rm -rf "$td"; mkdir -p "$td"
	cdir="$(fixture chanlen)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$cdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "channel len/cap report the ring"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$cdir"
done

# 6g: testing.T.Fatal/Skip abort the test body and the run continues with the
# next test. This is the cross-frame recover path: stage4 never armed the defer
# frame's JumpPC (call lines are emitted in fragments), and explicit
# panic("const") went to the abort-only runtime._panicstr.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="fatal:$label"
	td="$(tempdir "fatal_$label")"; rm -rf "$td"; mkdir -p "$td"
	fdir="$(fixture fatal)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$fdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		msg=""
		if echo "$out" | grep -q "unreachable after Fatal"; then msg="Fatal did not abort the test body"; fi
		if [ -z "$msg" ] && echo "$out" | grep -q "unreachable after Skip"; then msg="Skip did not abort the test body"; fi
		if [ -z "$msg" ] && ! echo "$out" | grep -q "second-ran"; then msg="the run did not continue to the next test"; fi
		if [ -z "$msg" ] && ! echo "$out" | grep -q -e "--- FAIL: TestStopsAtFatal"; then msg="the Fatal test was not reported as failed"; fi
		if [ -z "$msg" ] && ! echo "$out" | grep -q -e "--- SKIP: TestStopsAtSkip"; then msg="the Skip test was not reported as skipped"; fi
		if [ -z "$msg" ]; then
			log_pass "$name" "Fatal/Skip abort and the run continues"
		else
			log_fail "$name" "$msg"
		fi
	fi
	rm -rf "$td" "$fdir"
done

# 6h: the compiler's build identity is part of the cache directory, so a stale
# binary cannot seed the next compiler's cache with objects it compiled under
# the old sources but keyed on the new source hash. Two binaries from the same
# tree (same source hash) must use different cache dirs.
name="cacheid:stage4"
td="$(tempdir cacheid)"; rm -rf "$td"; mkdir -p "$td"
cp "$MOXIE" "$td/a"
cp "$MOXIE" "$td/b"
printf 'x' >> "$td/b"
chmod +x "$td/b"
ca="$(MOXIEROOT="$MOXIEROOT" "$td/a" env 2>/dev/null | sed -n 's/^CACHE="\(.*\)"$/\1/p' | head -1)"
cb="$(MOXIEROOT="$MOXIEROOT" "$td/b" env 2>/dev/null | sed -n 's/^CACHE="\(.*\)"$/\1/p' | head -1)"
if [ -z "$ca" ]; then
	log_fail "$name" "could not read CACHE from mxc env"
elif [ -z "$cb" ]; then
	log_skip "$name" "the modified binary did not run (signed binary?)"
elif [ "$ca" = "$cb" ]; then
	log_fail "$name" "cache dir does not carry the build identity: $ca"
else
	log_pass "$name" "cache dir differs with the binary"
fi
rm -rf "$td"

# 6i: untyped int/float constant promotion in both compilers. stage4's
# SSA-builder fold used the left (int) operand's type, so `1/2.0` folded to
# the int constant 0 and `1/Ln2` truncated - which also mis-sized math/big's
# base-10 conversion buffer.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="constpromo:$label"
	td="$(tempdir "constpromo_$label")"; rm -rf "$td"; mkdir -p "$td"
	pdir="$(fixture constpromo)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$pdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "untyped int/float promotion"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$pdir"
done

# 6j: clear() zeroes a slice or a map in both compilers. stage4's builtin
# emitter had no clear arm, so the call was dropped: math/big's basicMul
# starts with clear(z[0:len(x)+len(y)]) and accumulates into z, so a reused
# receiver kept its previous product's digits (Exp(10,20) = 10^20+10^5) and
# a 300-digit Text(10) divided by a stale zero and died with SIGFPE.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="clearzero:$label"
	td="$(tempdir "clearzero_$label")"; rm -rf "$td"; mkdir -p "$td"
	zdir="$(fixture clearzero)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$zdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "clear zeroes slices and maps"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$zdir"
done

# 6k: the spawn ring's index word is the parking word. A receiver parked on the
# write index must be woken by the publish (no lost wakeup), and a parent
# parked on a ring whose child died must unpark, report the closed channel and
# not hang. Before this both sides polled with a 1ms sleep.
#
# The same fixture also pins the discarded-receive ABI: `<-done` with no
# assignment passes a nil destination from the legacy compiler (stage4 passes a
# scratch), and the runtime wrote the delivered value through it - a
# legacy-built spawn program died in memcpy right after its first exchange.
# recvStore/recvZero in runtime.mx treat a nil destination as "discard", so
# both compilers run this now.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="futexpark:$label"
	td="$(tempdir "futexpark_$label")"; rm -rf "$td"; mkdir -p "$td"
	fpdir="$(fixture futexpark)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$fpdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		# A hang is the failure mode under test, so cap the run.
		out="$(timeout 60 "$td/bin" 2>&1 || true)"
		if echo "$out" | grep -q "^wake ok$" && echo "$out" | grep -q "^dead ok$"; then
			log_pass "$name" "park wakes on publish and unparks on peer death"
		else
			log_fail "$name" "got '$(echo "$out" | tr '\n' ' ')'"
		fi
	fi
	rm -rf "$td" "$fpdir"
done

# 6l: untyped float constants are exact rationals, in one package and across
# the .mxh boundary. stage4 compared the rounded float64 and exported a folded
# constant as its shortest decimal, so `math.MaxFloat64 ==
# 1.7976931348623157e308` was true where go/types (legacy) says false.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="exactfloat:$label"
	td="$(tempdir "exactfloat_$label")"; rm -rf "$td"; mkdir -p "$td"
	edir="$(fixture exactfloat)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$edir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "untyped float constants are exact"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$edir"
done

# 6m: the coverage join is one implementation (pkg/mxcover) shared by the
# mxcover command and `moxie test -cover`, so both print identical text. The
# same fixture and expected report are asserted by legacy/cover's Go test for
# the legacy harness (legacy's own `moxie test` cannot build these packages in
# this tree, so its report is locked by that unit test instead).
name="coverreport:stage4"
td="$(tempdir "coverreport")"; rm -rf "$td"; mkdir -p "$td"
rc=0
MOXIEROOT="$MOXIEROOT" "$MOXIE" build -o "$td/mxcover" "$ROOT/cmd/mxcover" >"$td/build.log" 2>&1 || rc=$?
if [ $rc -ne 0 ]; then
	log_fail "$name" "mxcover build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
else
	"$td/mxcover" "$SCRIPT_DIR/data/mxcover/sites.json" "$SCRIPT_DIR/data/mxcover/counts" "git.smesh.lol/smesh/" >"$td/got.txt" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "mxcover exited $rc: $(tail -2 "$td/got.txt" | tr '\n' ' ')"
	elif cmp -s "$td/got.txt" "$SCRIPT_DIR/data/mxcover/want.txt"; then
		log_pass "$name" "report matches the shared fixture"
	else
		log_fail "$name" "report differs: $(diff "$SCRIPT_DIR/data/mxcover/want.txt" "$td/got.txt" | head -3 | tr '\n' ' ')"
	fi
fi
rm -rf "$td"

# 6n: the reporter's parser and arithmetic units, run by the compiler that
# ships them (`moxie test ./pkg/mxcover`). pkg/mxcover is a package so the
# harness can print the same report the command does.
name="coverunit:stage4"
td="$(tempdir "coverunit")"; rm -rf "$td"; mkdir -p "$td"
rc=0
out="$(MOXIEROOT="$MOXIEROOT" timeout 900 "$MOXIE" test "$ROOT/pkg/mxcover" 2>&1)" || rc=$?
if [ $rc -eq 0 ] && echo "$out" | grep -q "^PASS: "; then
	log_pass "$name" "$(echo "$out" | grep '^PASS: ')"
else
	log_fail "$name" "$(echo "$out" | tail -3 | tr '\n' ' ')"
fi
rm -rf "$td"

# 6o: a shift whose left operand is an untyped constant is typed by its
# context, not by the count. stage4 typed `1 << c` (c a byte) as uint8, so the
# emitter's overshift select (Go: a count >= the width shifts to zero) zeroed
# every count >= 8 - `bytes.TrimRight(s, "\r\n")` never trimmed and the
# mediaproxy header loop read past the blank line. legacy was already right.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="shiftconst:$label"
	td="$(tempdir "shiftconst_$label")"; rm -rf "$td"; mkdir -p "$td"
	sdir="$(fixture shiftconst)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$sdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "untyped-constant shift takes the context type"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$sdir"
done

# 6p: a reslice is clamped to its parent instead of producing a negative
# length. `s[1:]` on an empty slice was len -1, so the tree's
# `for ; len(r) > 0; r = r[1:]` loops left `r` holding a negative length and
# the `if len(r) == 0` guards that follow never fired; a truncated REQ filter
# then read past the end and killed the relay.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="sliceres:$label"
	td="$(tempdir "sliceres_$label")"; rm -rf "$td"; mkdir -p "$td"
	srdir="$(fixture sliceres)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$srdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$(echo "$out" | head -1)" = "ok" ]; then
			log_pass "$name" "a reslice clamps to its parent"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok'"
		fi
	fi
	rm -rf "$td" "$srdir"
done

# 6q: the ChaCha20-Poly1305 AEAD must produce the RFC 8439 vector when it is a
# dependency of an application, not only when its own package is the build
# root. The ported sealGeneric split one allocation into a head and a tail view
# (the Go sliceForAppend idiom); Moxie relocates each returned slice on its
# own, so the tail came back as a copy and Seal answered all zeros - which is
# why Marmot DMs could not decrypt in an application build.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="aeaddep:$label"
	td="$(tempdir "aeaddep_$label")"; rm -rf "$td"; mkdir -p "$td"
	addir="$(fixture aeaddep)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$addir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if echo "$out" | grep -q "^ok d3 91$"; then
			log_pass "$name" "the AEAD known-answer vector holds as a dependency"
		else
			log_fail "$name" "got '$(echo "$out" | head -1)', want 'ok d3 91'"
		fi
	fi
	rm -rf "$td" "$addir"
done

# 6r: an untyped `{...}` in a map literal and a field select on a map index.
# Both compilers must build the same program and print the same values: stage4
# silently dropped the map entry when its value was an untyped composite
# literal (buildExpr answered nil), and it refused `m[k].Field` for a
# struct-valued map because a map value is not addressable - the key was live
# and the value was not there. Legacy builds both, so stage4 has to.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="maplit:$label"
	td="$(tempdir "maplit_$label")"; rm -rf "$td"; mkdir -p "$td"
	addir="$(fixture maplit)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$addir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		want="nested en = EN LOGIN
nested ja = JA LOGIN
slice = one,two
struct = one,two
array = first,second
ptr = five,six"
		if [ "$out" = "$want" ]; then
			log_pass "$name" "untyped map elements and map-index field selects hold"
		else
			log_fail "$name" "got '$(echo "$out" | tr '\n' '|')'"
		fi
	fi
	rm -rf "$td" "$addir"
done

# 6t: a variable declared from an untyped constant takes the constant's default
# type. Left untyped, `acc := 0` made the shift in `byte((acc >> bits) & 0x1f)`
# context-dependent, and the conversion retyped it to uint8 - an i8 shift on
# acc's low byte. Bech32 payloads came out wrong. Legacy types it int32 and is
# the reference here.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="untypedvar:$label"
	td="$(tempdir "untypedvar_$label")"; rm -rf "$td"; mkdir -p "$td"
	uvdir="$(fixture untypedvar)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$uvdir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		log_fail "$name" "build failed: $(tail -3 "$td/build.log" | tr '\n' ' ')"
	else
		out="$("$td/bin" 2>&1 || true)"
		if [ "$out" = "groups 1202140204021a13" ]; then
			log_pass "$name" "a declaration from an untyped constant is int32"
		else
			log_fail "$name" "got '$out', want 'groups 1202140204021a13'"
		fi
	fi
	rm -rf "$td" "$uvdir"
done

# 6u: push on an untyped nil must fail loudly in both compilers. Stage4 typed
# the call untyped nil, defaulted it to int32, and emitted an i32 where a slice
# header belongs - or, in a short declaration, silently built an empty slice.
# Legacy has always reported it; the fixture pins both sides to the same error.
for cc in "$MOXIE" "$LEGACY"; do
	label="stage4"
	if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
	name="pushnil:$label"
	td="$(tempdir "pushnil_$label")"; rm -rf "$td"; mkdir -p "$td"
	pndir="$(fixture pushnil)"
	rc=0
	MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$pndir" >"$td/build.log" 2>&1 || rc=$?
	if [ $rc -eq 0 ]; then
		log_fail "$name" "push on an untyped nil compiled"
	elif grep -q "must be a slice; have untyped nil" "$td/build.log"; then
		log_pass "$name" "push on an untyped nil is rejected by name"
	else
		log_fail "$name" "wrong error: $(tail -2 "$td/build.log" | tr '\n' ' ')"
	fi
	rm -rf "$td" "$pndir"
done

# 6v..6x: the three language rules legacy enforces and stage4 did not, each in
# its own fixture and inside a real module. Legacy applies user restrictions
# only to the main module (p.Module.Main), so a manifest-less directory version
# of these programs is accepted by legacy and would prove nothing. Both
# compilers must reject each one by name.
reject_fixture() {
	local fx="$1" want="$2"
	for cc in "$MOXIE" "$LEGACY"; do
		label="stage4"
		if [ "$cc" = "$LEGACY" ]; then label="legacy"; fi
		name="$fx:$label"
		td="$(tempdir "${fx}_$label")"; rm -rf "$td"; mkdir -p "$td"
		fxdir="$(fixture "$fx")"
		rc=0
		if [ "$cc" = "$LEGACY" ]; then
			if [ -z "$LEGACY_GOROOT" ]; then
				log_skip "$name" "no Go toolchain for the legacy loader"
				rm -rf "$td" "$fxdir"
				continue
			fi
			PATH="$LEGACY_GOROOT/bin:$PATH" GOROOT="$LEGACY_GOROOT" \
				MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$fxdir" >"$td/build.log" 2>&1 || rc=$?
		else
			MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$fxdir" >"$td/build.log" 2>&1 || rc=$?
		fi
		if [ $rc -eq 0 ]; then
			log_fail "$name" "the program compiled; want rejection"
		elif grep -q "$want" "$td/build.log"; then
			log_pass "$name" "rejected: $want"
		else
			log_fail "$name" "wrong error: $(tail -2 "$td/build.log" | tr '\n' ' ')"
		fi
		rm -rf "$td" "$fxdir"
	done
}

# 6v: unnamed return values.
reject_fixture unnamedret "unnamed return values are not allowed"
# 6w: a parenthesised call target.
reject_fixture parencall "parenthesized call target is not allowed"
# 6x: '+' for text concatenation.
reject_fixture plustext "'+' is not allowed for text concatenation"
# 6x.1: a package-level var with an initializer.
reject_fixture declvar "package-level var with initializer is not allowed"
# 6x.2: a named slice type.
reject_fixture namedslice "named slice type 'Names' is not allowed"

# 6y..6z: the no-conflict rule reaches imports. Legacy rejects both shapes
# through its own type checker (a redeclared import; a declaration already
# declared through the dot import), stage4 through the rule it gained here, so
# the two compilers reject for different reasons and the check pins each side's
# message rather than pretending they share one.
reject_conflict_fixture() {
	local fx="$1" want_s4="$2" want_legacy="$3"
	for cc in "$MOXIE" "$LEGACY"; do
		label="stage4"; want="$want_s4"
		if [ "$cc" = "$LEGACY" ]; then label="legacy"; want="$want_legacy"; fi
		name="$fx:$label"
		td="$(tempdir "${fx}_$label")"; rm -rf "$td"; mkdir -p "$td"
		fxdir="$(fixture "$fx")"
		rc=0
		if [ "$cc" = "$LEGACY" ]; then
			if [ -z "$LEGACY_GOROOT" ]; then
				log_skip "$name" "no Go toolchain for the legacy loader"
				rm -rf "$td" "$fxdir"
				continue
			fi
			PATH="$LEGACY_GOROOT/bin:$PATH" GOROOT="$LEGACY_GOROOT" \
				MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$fxdir" >"$td/build.log" 2>&1 || rc=$?
		else
			MOXIEROOT="$MOXIEROOT" "$cc" build -o "$td/bin" "$fxdir" >"$td/build.log" 2>&1 || rc=$?
		fi
		if [ $rc -eq 0 ]; then
			log_fail "$name" "the program compiled; want rejection"
		elif grep -q "$want" "$td/build.log"; then
			log_pass "$name" "rejected: $want"
		else
			log_fail "$name" "wrong error: $(tail -2 "$td/build.log" | tr '\n' ' ')"
		fi
		rm -rf "$td" "$fxdir"
	done
}

# 6y: two different imports binding one name in one file.
reject_conflict_fixture importconflict \
	"is imported twice in one file" \
	"redeclared in this block"
# 6z: a declaration colliding with a name a dot import injects.
reject_conflict_fixture dotconflict \
	"collides with an imported package name" \
	"already declared through dot-import of package math"

# 6aa: `moxie test` must run the package under test's init(). The harness
# concatenates the library and its test files into a synthetic main package and
# injects a __varinit call into that main, but it never called init() - so an
# init-set global read zero in the package's own test and nowhere else.
name="testinit:stage4"
td="$(tempdir "testinit")"; rm -rf "$td"; mkdir -p "$td"
itdir="$(fixture inittest)"
rc=0
out="$(MOXIEROOT="$MOXIEROOT" "$MOXIE" test "$itdir" 2>&1)" || rc=$?
if [ $rc -eq 0 ] && echo "$out" | grep -q "^PASS: "; then
	log_pass "$name" "$(echo "$out" | grep '^PASS: ')"
else
	log_fail "$name" "$(echo "$out" | grep -v '^\[mem\]' | tail -3 | tr '\n' ' ')"
fi
rm -rf "$td" "$itdir"

# 6ab: a named result without parentheses is a syntax error. The parser used to
# recover by swallowing the body, so the function's locals landed at package
# scope - every other function using one of those names then reported a
# shadowing error - and the only complaint was "unnamed return values", for a
# signature that names one. Two bugs behind it: the early file==nil branch
# collected the parse errors into an empty loop, and a recovered parse (file
# non-nil, errors recorded) was allowed to carry on with a partial tree.
reject_conflict_fixture badnamedresult \
	"named result values require parentheses" \
	"unexpected name"

failed=0
if [ -f "$STATUSFILE" ]; then
	failed=$(grep -c FAIL "$STATUSFILE" 2>/dev/null) || failed=0
fi
if [ "$failed" -gt 0 ]; then
	echo ""
	echo "Instrumentation suite: $failed failure(s)"
	exit 1
fi
