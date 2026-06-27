#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export MOXIEROOT="$ROOT"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 4: Binary smoke tests ==="

td="$(tempdir "smoke")"; rm -rf "$td"; mkdir -p "$td"

# 4a: compile and run hello.mx
name="smoke:hello"
t0=$(date +%s%N)
if compile_and_run "$SCRIPT_DIR/data/hello.mx" "$td" "hello"; then
	t1=$(date +%s%N)
	out=$(cat "$td/run.log" 2>/dev/null)
	if echo "$out" | grep -q "hello"; then
		log_pass "$name" "ok"
	else
		log_fail "$name" "wrong output: $out"
	fi
else
	log_fail "$name" "$(tail -3 "$td/build.log" 2>/dev/null)"
fi

rm -rf "$td"
