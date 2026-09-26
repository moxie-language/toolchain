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

echo ""
echo "=== Phase 6: build tags and allocation instrumentation ==="

if [ ! -x "$LEGACY" ]; then
	log_skip "phase6" "legacy compiler not built at $LEGACY"
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

failed=0
if [ -f "$STATUSFILE" ]; then
	failed=$(grep -c FAIL "$STATUSFILE" 2>/dev/null) || failed=0
fi
if [ "$failed" -gt 0 ]; then
	echo ""
	echo "Instrumentation suite: $failed failure(s)"
	exit 1
fi
