#!/bin/bash
# Phase 0b: restriction fixtures. Each fixture is its own unit: the compiler
# must reject it, with the error named in its '// expected:' header.
#
# A fixture that declares `main` must also select on shutdown. The entry rule is
# reported before the restriction under test - it is checked while main's SSA is
# built and it stops compilation - so a fixture without the arm fails with
# "main must select on shutdown" and the error it exists to pin is never reached.
# Measured on reject_plus_text and reject_plusassign_text, which passed on every
# compiler built before the entry rule landed.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$SCRIPT_DIR/../util.sh"
source "$SCRIPT_DIR/../split-util.sh"

run_one() {
	local fixture="$1" expected log rc=0 hdr="$1"
	unit_begin
	if [ -d "$fixture" ]; then
		hdr="$fixture/main.mx"
	fi
	expected="$(grep "^// expected:" "$hdr" | head -1 | sed 's/^\/\/ expected: *//')"
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
	if [ -f "$SCRIPT_DIR/$UNIT_NAME.mx" ]; then
		run_one "$SCRIPT_DIR/$UNIT_NAME.mx"
	elif [ -d "$SCRIPT_DIR/$UNIT_NAME" ]; then
		run_one "$SCRIPT_DIR/$UNIT_NAME"
	else
		echo "unknown restriction fixture: $UNIT_NAME" >&2
		exit 1
	fi
	exit $?
fi

for u in "${UNITS[@]}"; do
	if [ -f "$SCRIPT_DIR/$u.mx" ]; then
		run_one "$SCRIPT_DIR/$u.mx"
	else
		run_one "$SCRIPT_DIR/$u"
	fi
done
