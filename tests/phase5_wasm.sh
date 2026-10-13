#!/bin/bash
# Phase 5: wasm target validation.
#
# Every program with a main() is compiled twice - natively and for -target wasm
# - and both are run, the wasm one in a real browser. Native is the reference:
# the module must produce identical output. Linking is not the bar; matching
# output is. See tests/wasm/validate.sh for the comparison itself.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$SCRIPT_DIR/util.sh"

echo
echo "--- phase 5: wasm target ---"

# The browser run needs Playwright's Python package and a Chromium build.
# Without them the phase cannot produce evidence, so it skips rather than
# reporting a pass it did not earn.
if ! python3 -c 'import playwright' >/dev/null 2>&1; then
	log_skip "wasm:validate" "playwright python package not installed"
	exit 0
fi
if ! python3 -c '
from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    p.chromium.launch().close()
' >/dev/null 2>&1; then
	log_skip "wasm:validate" "chromium not available to playwright"
	exit 0
fi

LOG="$TMPROOT/wasm-validate.log"
mkdir -p "$TMPROOT"

if MOXIE="$MOXIE" bash "$ROOT/tests/wasm/validate.sh" >"$LOG" 2>&1; then
	rc=0
else
	rc=$?
fi

# Report one result per target so the summary names what failed rather than
# collapsing the whole phase into a single line.
while read -r name verdict rest; do
	case "$verdict" in
	PASS) log_pass "wasm:$name" "${rest:-output matches native}" ;;
	FAIL) log_fail "wasm:$name" "$rest" ;;
	SKIP) log_skip "wasm:$name" "$rest" ;;
	esac
done < <(grep -E '^[A-Za-z_0-9]+ +(PASS|FAIL|SKIP)' "$LOG")

if ! grep -qE '^[A-Za-z_0-9]+ +(PASS|FAIL|SKIP)' "$LOG"; then
	log_fail "wasm:validate" "harness produced no per-target results"
	tail -10 "$LOG"
fi

# The bundle a build PRODUCES is checked too: validate.sh builds through the
# checkout's jsruntime/, so it cannot see whether the staged copy beside the
# module is complete or loadable. This builds a fixture, compares the staged
# runtime against jsruntime/ file by file, and runs the emitted bootstrap in
# Chromium.
EMITLOG="$TMPROOT/wasm-emit.log"
if bash "$ROOT/tests/wasm/emit_check.sh" >"$EMITLOG" 2>&1; then
	log_pass "wasm:emitted-host" "$(tail -1 "$EMITLOG" | sed 's/^ *//')"
else
	log_fail "wasm:emitted-host" "$(grep -m1 'FAIL:' "$EMITLOG" | sed 's/^ *FAIL: *//')"
	tail -4 "$EMITLOG"
	rc=1
fi

echo "  (full log: $LOG, emitted bundle: $EMITLOG)"
exit "$rc"
