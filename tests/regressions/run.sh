#!/bin/bash
# Phase 0a: regression fixtures. Each fixture is its own unit: compiled in its
# own temp dir and run, with the build log kept when it fails.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$SCRIPT_DIR/../util.sh"
source "$SCRIPT_DIR/../split-util.sh"

# A browser-only fixture says so in its header. Its packages are panic stubs
# natively, so the run cannot succeed by design; phase 5 runs the wasm module in
# Chromium and that is the evidence. The native build still runs here, because
# importing those packages into a native binary is exactly what has to keep
# compiling.
browser_only() { grep -q '//:wasmtest browser-only' "$1" 2>/dev/null; }

run_one() {
	local fixture="$1" td skip_run=0
	unit_begin
	td="$UNIT_WORK/pkg"
	mkdir -p "$td"
	if [ -f "$fixture" ]; then
		cp "$fixture" "$td/main.mx"
	else
		cp "$fixture"/*.mx "$td/" 2>/dev/null
	fi
	browser_only "$fixture" && skip_run=1
	# A directory fixture with a lib/ subpackage: the library must compile on
	# its own too, so a cross-package break names the library rather than only
	# the main package that calls it.
	if [ -d "$fixture/lib" ] && [ -f "$fixture/lib/lib.mx" ]; then
		MOXIEROOT="$ROOT" "$MOXIE" build -o /dev/null "$fixture/lib" >"$UNIT_WORK/lib_build.log" 2>&1 || true
	fi
	if SKIP_RUN="$skip_run" compile_and_run "$td" "$UNIT_WORK" testbin; then
		if [ "$skip_run" = 1 ]; then
			unit_ok "$UNIT_NAME" "ok (native build; browser-only fixture runs in phase 5)"
		else
			unit_ok "$UNIT_NAME" "ok"
		fi
	elif grep -qE "undefined symbol|linker (failed|could not find symbol)" "$UNIT_WORK/build.log" 2>/dev/null; then
		# A fixture must RUN, so this is a FAIL - but a link failure is a
		# different defect from the fixture disagreeing with itself, and the
		# summary line has to say which one it was.
		unit_no "$UNIT_NAME" "link: $(first_error "$UNIT_WORK/build.log")"
	elif [ -f "$UNIT_WORK/run.log" ] && [ ! -s "$UNIT_WORK/run.log" ]; then
		unit_no "$UNIT_NAME" "ran but produced no output"
	else
		unit_no "$UNIT_NAME" "$(first_error "$UNIT_WORK/build.log" 'panic:|caught signal|Assertion|defined with type')"
	fi
	unit_exit
}

UNITS=()
while IFS= read -r f; do
	UNITS+=("$(basename "$f" .mx)")
done < <(ls "$SCRIPT_DIR/should_compile/"*.mx 2>/dev/null | sort)
while IFS= read -r d; do
	[ -f "$d/main.mx" ] || continue
	UNITS+=("$(basename "$d")")
done < <(ls -d "$SCRIPT_DIR/should_compile/"*/ 2>/dev/null | sort)

unit_args "$@"

echo ""
echo "=== Phase 0a: Regressions ==="

# One unit: the named fixture, chosen by name so the two shapes (.mx file and
# directory) stay a detail of this script rather than of the driver.
if [ -n "$UNIT_NAME" ]; then
	if [ -f "$SCRIPT_DIR/should_compile/$UNIT_NAME.mx" ]; then
		run_one "$SCRIPT_DIR/should_compile/$UNIT_NAME.mx"
	elif [ -d "$SCRIPT_DIR/should_compile/$UNIT_NAME" ]; then
		run_one "$SCRIPT_DIR/should_compile/$UNIT_NAME"
	else
		echo "unknown regression fixture: $UNIT_NAME" >&2
		exit 1
	fi
	exit $?
fi

# No unit selected - the legacy whole-phase mode, run in order.
for u in "${UNITS[@]}"; do
	if [ -f "$SCRIPT_DIR/should_compile/$u.mx" ]; then
		run_one "$SCRIPT_DIR/should_compile/$u.mx"
	else
		run_one "$SCRIPT_DIR/should_compile/$u"
	fi
done
