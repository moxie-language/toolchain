#!/bin/bash
# Phase 1: package compilation smoke. Each package is its own unit.
#
# These five are the compiler's own support packages. They are libraries, so a
# `build` of the package directory alone stops at the linker with "undefined
# symbol: main.main" - a library has no entry point, and reporting that as a
# failure would make every build of a library a red test. Each unit therefore
# builds a one-line probe main that blank-imports the package: the blank import
# pulls the whole closure into the build, and the probe's own main gives the
# link something to link to.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"
source "$SCRIPT_DIR/split-util.sh"

run_one() {
	local name="$1" rc=0
	unit_begin
	mkdir -p "$UNIT_WORK/pkg"
	{
		echo "package main"
		echo ""
		echo "import _ \"git.smesh.lol/moxie/pkg/$name\""
		echo ""
		echo "func main() {"
		echo "}"
	} >"$UNIT_WORK/pkg/main.mx"
	MOXIEROOT="$ROOT" "$MOXIE" build -o "$UNIT_WORK/out" "$UNIT_WORK/pkg" \
		>"$UNIT_WORK/build.log" 2>&1 || rc=$?
	if [ "$rc" -eq 0 ]; then
		if "$UNIT_WORK/out" >"$UNIT_WORK/run.log" 2>&1; then
			unit_ok "pkg/$name" "ok"
		else
			unit_no "pkg/$name" "built but failed to run: $(tail -1 "$UNIT_WORK/run.log")"
		fi
	elif grep -qE "panic:|caught signal|nil pointer dereference" "$UNIT_WORK/build.log" 2>/dev/null; then
		unit_no "pkg/$name" "compiler crashed: $(first_error "$UNIT_WORK/build.log" 'panic:|caught signal')"
	elif grep -qE "error:.*defined with type.*but expected" "$UNIT_WORK/build.log" 2>/dev/null; then
		unit_no "pkg/$name" "bad IR: $(first_error "$UNIT_WORK/build.log" 'defined with type')"
	else
		unit_no "pkg/$name" "$(first_error "$UNIT_WORK/build.log")"
	fi
	unit_exit
}

UNITS=(token mxutil syntax types)

unit_args "$@"

echo ""
echo "=== Phase 1: Package compilation smoke ==="

if [ -n "$UNIT_NAME" ]; then
	run_one "$UNIT_NAME"
	exit $?
fi

for u in "${UNITS[@]}"; do
	run_one "$u"
done
