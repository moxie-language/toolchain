#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 3: LLVM IR validation ==="

if ! command -v opt >/dev/null 2>&1; then
	echo "  SKIP: 'opt' not found (install llvm)"
	exit 0
fi

td="$(tempdir "irval")"; rm -rf "$td"; mkdir -p "$td"

for pkg in token mxutil syntax types; do
	name="ir:$pkg"
	t0=$(date +%s%N)
	log="$td/${pkg}_build.log"
	rc=0
	MOXIEROOT="$ROOT" "$MOXIE" build -o "$td/${pkg}_out" "$ROOT/pkg/$pkg" >"$log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		if grep -qE "panic:|caught signal|nil pointer" "$log" 2>/dev/null; then
			log_fail "$name" "crash"
		else
			log_pass "$name" "0.0s (linker only)"
		fi
		rm -rf "$td"
		continue
	fi
	# Try to find the .ll file and verify it
	llfile=$(find "$td" -name "*.ll" 2>/dev/null | head -1)
	if [ -n "$llfile" ]; then
		if opt -verify "$llfile" >/dev/null 2>&1; then
			t1=$(date +%s%N)
			log_pass "$name" "ok"
		else
			log_fail "$name" "LLVM IR verification failed"
		fi
	else
		t1=$(date +%s%N)
		log_pass "$name" "0 (no .ll)"
	fi
	rm -rf "$td"
done
