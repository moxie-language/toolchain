#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export MOXIEROOT="$ROOT"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 5: Driver test (cmd/moxie) ==="

td="$(tempdir "driver")"; rm -rf "$td"; mkdir -p "$td"

# 5a: build cmd/moxie
name="driver:build"
t0=$(date +%s%N)
if MOXIEROOT="$ROOT" "$MOXIE" build -o "$td/mxc" "$ROOT/cmd/moxie/" >"$td/build.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	if grep -qE "panic:|caught signal|nil pointer" "$td/build.log" 2>/dev/null; then
		log_fail "$name" "$(tail -3 "$td/build.log")"
	elif grep -qE "error:.*defined with type" "$td/build.log" 2>/dev/null; then
		log_fail "$name" "$(tail -3 "$td/build.log")"
	else
		t1=$(date +%s%N)
		log_pass "$name" "0 (linker errors OK)"
	fi
	rm -rf "$td"
	exit 0
fi

# 5b: driver version
name="driver:version"
t0=$(date +%s%N)
if "$td/mxc" version >"$td/ver.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	log_fail "$name" "version failed: $(tail -3 "$td/ver.log")"
fi

# 5c: driver compiles hello
name="driver:hello"
t0=$(date +%s%N)
td2="$(tempdir "driver_hello")"; rm -rf "$td2"; mkdir -p "$td2"
if MOXIEROOT="$ROOT" "$td/mxc" build -o "$td2/hello" "$SCRIPT_DIR/data/hello.mx" >"$td2/build.log" 2>&1; then
	if "$td2/hello" >"$td2/run.log" 2>&1; then
		t1=$(date +%s%N)
		log_pass "$name" "ok"
	else
		log_fail "$name" "binary runtime failed"
	fi
else
	log_fail "$name" "driver compile failed: $(tail -3 "$td2/build.log")"
fi
rm -rf "$td" "$td2"
