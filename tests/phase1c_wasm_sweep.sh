#!/bin/bash
# Phase 1c: js/wasm compile sweep. Each package is its own unit.
#
# The wasm target has its own build tags (`//:build js && wasm`, `//:build unix
# || (js && wasm)`), so a package can compile natively and not for js - and
# phase 5 only builds fixtures with a main(), which never pulls in syscall/js or
# the js arms of internal/poll. Nothing compiled src/syscall/js at all until
# round 77, which is why its ten uses of `any` (a type Moxie does not have) sat
# unnoticed.
#
# A wasm library links without a main.main, so rc==0 is the pass condition.
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"
source "$SCRIPT_DIR/split-util.sh"

# Packages that must compile for -target wasm.
UNITS=(
	net/http
	path/filepath
	syscall/js
)

# Packages known not to compile for wasm, with the reason. SKIP, never FAIL:
# the suite must not encode today's breakage as acceptable, but it must not go
# red for a defect that is already recorded either.
SWEEP_KNOWN=(
)

run_one() {
	local pkg="$1" rc=0
	unit_begin
	MOXIEROOT="$ROOT" "$MOXIE" build -target wasm -o "$UNIT_WORK/out.wasm" "$ROOT/src/$pkg" \
		>"$UNIT_WORK/build.log" 2>&1 || rc=$?
	if [ "$rc" -eq 0 ]; then
		unit_ok "wasm:$pkg" "compiles"
	else
		unit_no "wasm:$pkg" "$(first_error "$UNIT_WORK/build.log")"
	fi
	unit_exit
}

unit_args "$@"

echo ""
echo "=== Phase 1c: js/wasm compile sweep ==="

if [ -n "$UNIT_NAME" ]; then
	run_one "$UNIT_NAME"
	exit $?
fi

for u in "${UNITS[@]}"; do
	run_one "$u"
done

if [ "${#SWEEP_KNOWN[@]}" -gt 0 ]; then
	for entry in "${SWEEP_KNOWN[@]}"; do
		pkg="${entry%%|*}"
		reason="${entry#*|}"
		log_skip "wasm:$pkg" "$reason"
	done
fi
