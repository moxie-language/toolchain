#!/bin/bash
# Regression test runner: compiles each should_compile test and runs the binary.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$SCRIPT_DIR/../util.sh"

echo ""
echo "=== Phase 0: Regressions ==="

for f in "$SCRIPT_DIR/should_compile/"*.mx; do
	[ -f "$f" ] || continue
	name="regression:$(basename "$f" .mx)"
	td="$(tempdir "reg_$(basename "$f" .mx)")"
	rm -rf "$td"; mkdir -p "$td"
	# A browser-only fixture says so in its header. Its packages are panic stubs
	# natively, so the run cannot succeed by design; phase 5 runs the wasm
	# module in Chromium and that is the evidence. The native build still runs
	# here, because importing those packages into a native binary is exactly
	# what has to keep compiling.
	skip_run=0
	grep -q '//:wasmtest browser-only' "$f" 2>/dev/null && skip_run=1
	if SKIP_RUN="$skip_run" compile_and_run "$f" "$td" "testbin"; then
		if [ "$skip_run" = "1" ]; then
			log_pass "$name" "ok (native build; browser-only fixture runs in phase 5)"
		else
			log_pass "$name" "ok"
		fi
	else
		tail -5 "$td/build.log" 2>/dev/null > "$td/err.txt"
		log_fail "$name" "$(cat "$td/err.txt" 2>/dev/null || echo 'crash')"
	fi
	rm -rf "$td"
done

for d in "$SCRIPT_DIR/should_compile/"*/; do
	[ -d "$d" ] || continue
	[ -f "$d/main.mx" ] || continue
	name="regression:$(basename "$d")"
	td="$(tempdir "reg_$(basename "$d")")"
	rm -rf "$td"; mkdir -p "$td"

	lib_dir="$d/lib"
	if [ -d "$lib_dir" ] && [ -f "$lib_dir/lib.mx" ]; then
		MOXIEROOT="$ROOT" "$MOXIE" build -o /dev/null "$lib_dir" >"$td/lib_build.log" 2>&1 || true
	fi

	if compile_and_run "$d" "$td" "testbin"; then
		log_pass "$name" "ok"
	else
		tail -5 "$td/build.log" 2>/dev/null > "$td/err.txt"
		log_fail "$name" "$(cat "$td/err.txt" 2>/dev/null || echo 'crash')"
	fi
	rm -rf "$td"
done

failed=0
if [ -f "$STATUSFILE" ]; then
	failed=$(grep -c FAIL "$STATUSFILE" 2>/dev/null) || failed=0
fi
if [ "$failed" -gt 0 ]; then
	echo ""
	echo "Regression suite: $failed failure(s)"
	exit 1
fi
