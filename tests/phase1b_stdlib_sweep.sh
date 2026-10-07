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
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 1b: Stdlib compile sweep ==="

# Packages that must compile. net/http carries the crypto/tls closure, which is
# what reaches crypto/ecdsa and its fips140 generics.
SWEEP=(
	compress/flate
	compress/gzip
	compress/zlib
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
	encoding/json
	encoding/xml
	hash/crc32
	hash/fnv
	internal/coverage/cformat
	internal/coverage/encodecounter
	internal/coverage/encodemeta
	math
	math/big
	mime
	mime/multipart
	net
	net/http
	net/textproto
	net/url
	os/user
	regexp
	text/tabwriter
	text/template
	time
	unicode/utf8
)

# Packages known not to compile, with the reason. SKIP, never FAIL: the suite
# must not encode today's breakage as acceptable, but it must not go red for a
# defect that is already recorded either.
SWEEP_KNOWN=(
	"internal/trace/internal/tracev1|X86 DAG->DAG crash (SelectionDAG::ReplaceAllUsesWith) on the 32 MiB aggregate load+store that relocating an escaped &[524288]Event{} emits"
)

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
	echo "}"
} >"$td/pkg/main.mx"

log="$td/build.log"
rc=0
MOXIEROOT="$ROOT" "$MOXIE" build -o "$td/sweep" "$td/pkg" >"$log" 2>&1 || rc=$?
if [ $rc -eq 0 ]; then
	# It compiled AND linked: run it, so a bad entry point cannot pass unrun.
	if "$td/sweep" >"$td/run.log" 2>&1; then
		log_pass "stdlib:sweep" "${#SWEEP[@]} packages ok"
	else
		log_fail "stdlib:sweep" "built but failed to run: $(tail -2 "$td/run.log")"
	fi
else
	# Name the package the compiler blamed, not the whole set.
	first="$(grep -E '^mxc:|error:' "$log" | head -1 || true)"
	if [ -z "$first" ]; then
		first="$(tail -3 "$log" | head -1)"
	fi
	log_fail "stdlib:sweep" "$first"
	echo "    (full log: $log)"
	cp "$log" "$TMPROOT/stdlib_sweep.log" 2>/dev/null || true
fi
rm -rf "$td"

for entry in "${SWEEP_KNOWN[@]}"; do
	pkg="${entry%%|*}"
	reason="${entry#*|}"
	log_skip "stdlib:$pkg" "$reason"
done
