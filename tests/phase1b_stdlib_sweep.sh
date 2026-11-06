#!/bin/bash
# Phase 1b: stdlib compile sweep.
#
# No other phase compiles a stdlib package: phase 1 builds only the five
# compiler-support packages (token mxutil syntax types rewrite) and the other
# phases build fixtures and the compiler itself. That gap hid real breakage -
# src/internal/coverage/{encodecounter,encodemeta}, src/text/tabwriter and
# src/net/http did not compile at all while the suite was green, because the
# only check anyone ran was `moxie build -a ./_mxc_stage4` returning 0, which
# says nothing about any other package.
#
# One build, not one per package: a blank import pulls a package into the
# compile closure, so a single main importing all of them compiles every one of
# them in one pass. Building the packages separately costs a full closure each
# (net/http alone is ~20 minutes) and bought nothing - the compiler names the
# package in the error either way.
#
# A package that is known broken today is listed in SWEEP_KNOWN and SKIPs with
# its reason, so the set of broken packages stays visible and shrinking it is a
# deliberate act: move the name to SWEEP when it is fixed.
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"
source "$SCRIPT_DIR/split-util.sh"

# Packages that must compile. net/http carries the crypto/tls closure, which is
# what reaches crypto/ecdsa and its fips140 generics.
#
# Every entry here must RESOLVE. It did not always: encoding/json, encoding/xml,
# regexp and text/template were on this list while no such package existed in
# the tree, and the sweep reported "41 packages ok" because the compiler skipped
# an import it could not resolve instead of failing (round 103). encoding/json
# and encoding/xml went with the reflect/any exodus (3b6e25e8, 4da18fb0);
# regexp and text/template are not reflect-free either, and every in-tree
# importer of them was dead code, removed in the same round. With the loader
# strict, an entry naming a package that does not exist fails the build - so
# this list is now self-verifying and a missing package can never pass again.
SWEEP=(
	compress/flate
	compress/gzip
	compress/zlib
	context
	crypto/aes
	crypto/cipher
	crypto/ecdh
	crypto/ecdsa
	crypto/ed25519
	crypto/elliptic
	crypto/hmac
	crypto/rsa
	crypto/sha256
	crypto/x509
	debug/gosym
	encoding/base64
	encoding/binary
	encoding/hex
	hash/crc32
	hash/fnv
	index/suffixarray
	internal/coverage/cfile
	internal/coverage/cformat
	internal/coverage/encodecounter
	internal/coverage/encodemeta
	internal/profile
	internal/trace
	internal/trace/internal/tracev1
	iter
	maps
	slices
	math
	math/big
	mime
	mime/multipart
	net
	net/http
	net/textproto
	net/url
	os/exec
	os/user
	runtime/trace
	text/tabwriter
	time
	unicode/utf8
)

# Packages known not to compile, with the reason. SKIP, never FAIL: the suite
# must not encode today's breakage as acceptable, but it must not go red for a
# defect that is already recorded either. Empty is the goal - every entry here
# is a package that should be fixed and then moved back into SWEEP.
SWEEP_KNOWN=(
)

# One unit, and it stays one unit - see the note above `unit_args` below for why
# the sweep is not split per package. Without an entry here the driver sees a
# phase with no units and reports success without running anything, which is
# exactly the failure this line exists to prevent.
UNITS=(stdlib_sweep)

td="$(tempdir stdlib_sweep)"
rm -rf "$td"; mkdir -p "$td/pkg"
{
	echo "package main"
	echo ""
	echo "import ("
	for pkg in "${SWEEP[@]}"; do
		echo "	_ \"$pkg\""
	done
	echo ")"
	echo ""
	echo "func main() {"
	echo "	// A sweep binary is a batch program: stoppable at entry, then it runs"
	echo "	// its imports' initialisers to completion."
	echo "	select {"
	echo "	case <-shutdown:"
	echo "		return"
	echo "	default:"
	echo "	}"
	echo "}"
} >"$td/pkg/main.mx"

# One unit, and it stays one unit. The sweep is one build so that the 45
# packages share the closure they have in common (net/http, crypto/tls, math/big
# are most of the cost of every package that touches them); splitting it into 45
# builds would compile that closure 45 times instead of once. What the unit
# layer buys here is what it buys every phase: its own log, its own timeout, and
# a result that survives the rest of the run.
unit_args "$@"

echo ""
echo "=== Phase 1b: Stdlib compile sweep (${#SWEEP[@]} packages) ==="

log="$td/build.log"
rc=0
MOXIEROOT="$ROOT" "$MOXIE" build -o "$td/sweep" "$td/pkg" >"$log" 2>&1 || rc=$?
if [ $rc -eq 0 ]; then
	# It compiled AND linked: run it, so a bad entry point cannot pass unrun.
	if "$td/sweep" >"$td/run.log" 2>&1; then
		unit_ok "stdlib:sweep" "${#SWEEP[@]} packages ok"
	else
		unit_no "stdlib:sweep" "built but failed to run: $(tail -2 "$td/run.log")"
	fi
else
	# Name the package the compiler blamed, not the whole set.
	first="$(grep -E '^mxc:|error:' "$log" | head -1 || true)"
	if [ -z "$first" ]; then
		first="$(tail -3 "$log" | head -1)"
	fi
	unit_no "stdlib:sweep" "$first"
	echo "    (full log: $log)"
	cp "$log" "$TMPROOT/stdlib_sweep.log" 2>/dev/null || true
fi

if [ "${#SWEEP_KNOWN[@]}" -gt 0 ]; then
	for entry in "${SWEEP_KNOWN[@]}"; do
		pkg="${entry%%|*}"
		reason="${entry#*|}"
		log_skip "stdlib:$pkg" "$reason"
	done
fi
exit $?
