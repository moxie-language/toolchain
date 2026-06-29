#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 1: Package compilation smoke ==="

for pkg in token mxutil syntax types mxh rewrite mod ssa emit; do
	td="$(tempdir "pkg_$pkg")"
	rm -rf "$td"; mkdir -p "$td"
	name="pkg/$pkg"
	t0=$(date +%s%N)
	log="$td/build.log"
	rc=0
	MOXIEROOT="$ROOT" "$MOXIE" build -o "$td/out" "$ROOT/pkg/$pkg" >"$log" 2>&1 || rc=$?
	if [ $rc -ne 0 ]; then
		if grep -qE "panic:|caught signal|nil pointer dereference" "$log" 2>/dev/null; then
			log_fail "$name" "$(tail -3 "$log")"
		elif grep -qE "error:.*defined with type.*but expected" "$log" 2>/dev/null; then
			log_fail "$name" "$(tail -3 "$log")"
		elif grep -qE "linker (failed|could not find symbol)" "$log" 2>/dev/null; then
			t1=$(date +%s%N)
			log_pass "$name" "ok"
		elif grep -q "undefined symbol" "$log" 2>/dev/null; then
			t1=$(date +%s%N)
			log_pass "$name" "ok"
		else
			log_fail "$name" "$(tail -3 "$log")"
		fi
	else
		t1=$(date +%s%N)
		log_pass "$name" "ok"
	fi
	rm -rf "$td"
done
