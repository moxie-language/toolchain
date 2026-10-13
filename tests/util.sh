#!/bin/bash
# Shared utilities for the moxie test suite.

set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
# The suite tests the tree it lives in. An inherited MOXIEROOT (an installed
# tree from another revision) would make the compiler read that tree's stdlib
# and report its sources against these, which reads as a compiler bug in the
# very tree under test. run.sh already pins this; a phase script invoked
# directly gets the same root here.
MOXIEROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export MOXIEROOT

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

# Same policy as run.sh: artifacts in the tree's ./tmp, never /tmp, which is
# wiped between sessions here.
STATUSFILE="${STATUSFILE:-$MOXIEROOT/tmp/moxie-test-status-$$}"
# Outside the tree on purpose: a fixture inside the tree is not a user package,
# so the restriction rules would not fire (see run.sh).
TMPROOT="${TMPROOT:-${HOME:-/tmp}/moxie-tests-$$}"

log_pass() {
	local name="$1" extra="$2"
	printf "  %-45s ${GREEN}PASS${NC} (%s)\n" "$name" "$extra"
	echo "PASS" >> "$STATUSFILE"
}

log_fail() {
	local name="$1" msg="$2"
	printf "  %-45s ${RED}FAIL${NC}\n" "$name"
	echo -e "    ${RED}${msg}${NC}" | tail -5
	echo "FAIL" >> "$STATUSFILE"
}

log_skip() {
	local name="$1" reason="$2"
	printf "  %-45s ${YELLOW}SKIP${NC} (%s)\n" "$name" "$reason"
	echo "SKIP" >> "$STATUSFILE"
}

tempdir() {
	local name="$1"
	mkdir -p "$TMPROOT/$name"
	echo "$TMPROOT/$name"
}

compile_and_run() {
	local src="$1" out="$2" name="$3"
	local log="$out/build.log"
	local rc=0

	if [ -f "$src" ]; then
		local pkgdir="$out/pkg"
		mkdir -p "$pkgdir"
		cp "$src" "$pkgdir/main.mx"
		src="$pkgdir"
	fi

	MOXIEROOT="$MOXIEROOT" "$MOXIE" build -o "$out/$name" "$src" >"$log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		if grep -qE "panic:|caught signal|nil pointer dereference" "$log" 2>/dev/null; then
			return 1
		fi
		if grep -qE "error:.*defined with type.*but expected" "$log" 2>/dev/null; then
			return 1
		fi
		# A fixture whose link fails must FAIL. These two guards returned 0 for
		# "undefined symbol: main." and for any linker failure, so a fixture
		# that never linked was reported as a PASS - which is how a broken
		# entry point stayed invisible. The root cause was fixed; a guard that
		# turns a non-linking binary into a pass is a smell, not a tolerance.
		return 1
	fi
	# A fixture can declare that its native run is meaningless (a browser-only
	# fixture: the wasm-only packages are panic stubs off-target, so calling one
	# natively panics by design). The native BUILD still has to succeed - that
	# is compile coverage for the packages the fixture imports - but the run is
	# the caller's to skip, and only the caller knows the fixture said so.
	[ "${SKIP_RUN:-0}" = "1" ] && return 0

	"$out/$name" >"$out/run.log" 2>&1 || return 1
	return 0
}

print_summary() {
	local total=0 passed=0 failed=0 skipped=0
	if [ -f "$STATUSFILE" ]; then
		total=$(wc -l < "$STATUSFILE" 2>/dev/null || echo 0)
		passed=$(grep -c PASS "$STATUSFILE" 2>/dev/null) || passed=0
		failed=$(grep -c FAIL "$STATUSFILE" 2>/dev/null) || failed=0
		skipped=$(grep -c SKIP "$STATUSFILE" 2>/dev/null) || skipped=0
	fi
	echo ""
	echo "=== Summary ==="
	printf "total: %d  passed: %d  failed: %d  skipped: %d\n" "$total" "$passed" "$failed" "$skipped"
	rm -f "$STATUSFILE"
	rm -rf "$TMPROOT"
	return "$failed"
}
