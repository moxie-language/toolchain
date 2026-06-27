#!/bin/bash
# Restriction test runner: compiles each .mx, checks it fails cleanly.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$SCRIPT_DIR/../util.sh"

echo ""
echo "=== Phase 0b: Restrictions ==="

for f in "$SCRIPT_DIR"/*.mx; do
	[ -f "$f" ] || continue
	name="restrict:$(basename "$f" .mx)"
	t0=$(date +%s%N)
	td="$(tempdir "rst_$(basename "$f" .mx)")"
	rm -rf "$td"; mkdir -p "$td"
	mkdir -p "$td/pkg"
	cp "$f" "$td/pkg/main.mx"

	expected=$(grep "^// expected:" "$f" | head -1 | sed 's/^\/\/ expected: *//')
	if [ -z "$expected" ]; then
		log_fail "$name" "no '// expected:' header"
		rm -rf "$td"
		continue
	fi

	log="$td/build.log"
	rc=0
	MOXIEROOT="$ROOT" "$MOXIE" build -o /dev/null "$td/pkg" >"$log" 2>&1 || rc=$?

	if [ $rc -eq 0 ]; then
		log_fail "$name" "compiler did not reject (exit 0, expected error: '$expected')"
		rm -rf "$td"
		continue
	fi

	if grep -qE "panic:|caught signal|nil pointer dereference" "$log" 2>/dev/null; then
		log_fail "$name" "compiler crashed instead of producing clean error"
		rm -rf "$td"
		continue
	fi

	if ! grep -qiF "$expected" "$log" 2>/dev/null; then
		log_fail "$name" "expected error '$expected' not found in output"
		tail -5 "$log"
		rm -rf "$td"
		continue
	fi

	t1=$(date +%s%N)
	log_pass "$name" "ok"
	rm -rf "$td"
done
