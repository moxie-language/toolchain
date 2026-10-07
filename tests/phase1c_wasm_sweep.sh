#!/bin/bash
# Phase 1c: js/wasm compile sweep.
#
# The wasm target has its own build tags (`//:build js && wasm`, `//:build unix
# || (js && wasm)`), so a package can compile natively and not for js - and
# phase 5 only builds fixtures with a main(), which never pulls in
# syscall/js or the js arms of internal/poll. Nothing compiled
# `src/syscall/js` at all until round 77, which is why its ten uses of `any`
# (a type Moxie does not have) sat unnoticed.
#
# A wasm library links without a main.main, so rc==0 is the pass condition.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 1c: js/wasm compile sweep ==="

# Packages that must compile for -target wasm.
SWEEP=(
	path/filepath
	syscall/js
)

# Packages known not to compile for wasm, with the reason. SKIP, never FAIL:
# the suite must not encode today's breakage as acceptable, but it must not go
# red for a defect that is already recorded either.
SWEEP_KNOWN=(
	"net/http|internal/poll's js accept/ReadFrom do not resolve a type (rhs=nil)"
)

for pkg in "${SWEEP[@]}"; do
	td="$(tempdir "wasmsweep_$(echo "$pkg" | tr / _)")"
	rm -rf "$td"; mkdir -p "$td"
	log="$td/build.log"
	rc=0
	MOXIEROOT="$ROOT" "$MOXIE" build -target wasm -o "$td/out.wasm" "$ROOT/src/$pkg" >"$log" 2>&1 || rc=$?
	if [ $rc -eq 0 ]; then
		log_pass "wasm:$pkg" "compiles"
	else
		first="$(grep -E '^; |^mxc:|error:' "$log" | grep -v '^  compile errors$' | head -1 || true)"
		[ -n "$first" ] || first="$(tail -3 "$log" | head -1)"
		log_fail "wasm:$pkg" "$first"
		echo "    (full log: $log)"
	fi
	rm -rf "$td"
done

if [ "${#SWEEP_KNOWN[@]}" -gt 0 ]; then
	for entry in "${SWEEP_KNOWN[@]}"; do
		pkg="${entry%%|*}"
		reason="${entry#*|}"
		log_skip "wasm:$pkg" "$reason"
	done
fi
