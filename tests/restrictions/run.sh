#!/bin/bash
# Phase 0b: restriction fixtures. Each fixture is its own unit: the compiler
# must reject it, with the error named in its '// expected:' header.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$SCRIPT_DIR/../util.sh"
source "$SCRIPT_DIR/../split-util.sh"

run_one() {
	local fixture="$1" expected log rc=0
	unit_begin
	expected="$(grep "^// expected:" "$fixture" | head -1 | sed 's/^\/\/ expected: *//')"
	if [ -z "$expected" ]; then
		unit_no "$UNIT_NAME" "no '// expected:' header"
		unit_exit
		return
	fi
	mkdir -p "$UNIT_WORK/pkg"
	if [ -d "$fixture" ]; then
		# A directory fixture: its own sources are the package. A subpackage it
		# imports stays in the tree and resolves by module path, like a
		# should_compile directory fixture.
		cp "$fixture"/*.mx "$UNIT_WORK/pkg/"
	else
		cp "$fixture" "$UNIT_WORK/pkg/main.mx"
	fi
	log="$UNIT_WORK/build.log"
	MOXIEROOT="$ROOT" "$MOXIE" build -o /dev/null "$UNIT_WORK/pkg" >"$log" 2>&1 || rc=$?
	if [ "$rc" -eq 0 ]; then
		unit_no "$UNIT_NAME" "compiler did not reject (exit 0, expected '$expected')"
	elif grep -qE "panic:|caught signal|nil pointer dereference" "$log" 2>/dev/null; then
		unit_no "$UNIT_NAME" "compiler crashed instead of producing a clean error"
	elif ! grep -qiF "$expected" "$log" 2>/dev/null; then
		unit_no "$UNIT_NAME" "expected '$expected'; got: $(first_error "$log")"
	else
		unit_ok "$UNIT_NAME" "ok"
	fi
	unit_exit
}

UNITS=()
while IFS= read -r f; do
	UNITS+=("$(basename "$f" .mx)")
done < <(ls "$SCRIPT_DIR"/*.mx 2>/dev/null | sort)
while IFS= read -r d; do
	[ -f "$d/main.mx" ] || continue
	UNITS+=("$(basename "$d")")
done < <(ls -d "$SCRIPT_DIR"/*/ 2>/dev/null | sort)

unit_args "$@"

echo ""
echo "=== Phase 0b: Restrictions ==="

if [ -n "$UNIT_NAME" ]; then
	run_one "$SCRIPT_DIR/$UNIT_NAME.mx"
	exit $?
fi

for u in "${UNITS[@]}"; do
	run_one "$SCRIPT_DIR/$u.mx"
done
