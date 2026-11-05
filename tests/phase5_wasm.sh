#!/bin/bash
# Phase 5: wasm target validation. One unit per fixture.
#
# Every program with a main() is compiled twice - natively and for -target wasm
# - and both are run, the wasm one in a real browser. Native is the reference:
# the module must produce identical output. Linking is not the bar; matching
# output is. See tests/wasm/validate.sh for the comparison itself.
#
# The units are the fixtures, not "the phase". As a single unit this phase
# re-ran the whole 78-target sweep once per driver job - measured: 80 launches,
# each rebuilding every fixture, and the 900s default timeout killed the first
# one mid-sweep while it was still on target 36. A unit is one fixture, so a
# target that hangs or fails costs that target.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

source "$SCRIPT_DIR/util.sh"
source "$SCRIPT_DIR/split-util.sh"

# The browser run needs Playwright's Python package and a Chromium build.
# Without them the phase cannot produce evidence, so it skips rather than
# reporting a pass it did not earn.
browser_ok() {
	python3 -c 'import playwright' >/dev/null 2>&1 || return 1
	python3 -c '
from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    p.chromium.launch().close()
' >/dev/null 2>&1
}

# The fixture set is the one validate.sh derives for itself: programs with a
# main(), minus those that declare themselves native-only, plus directory
# fixtures. Kept in step with validate.sh by construction - both read the same
# two headers.
UNITS=()
while IFS= read -r f; do
	grep -q 'func main' "$f" || continue
	grep -q '//:wasmtest skip' "$f" && continue
	UNITS+=("$(basename "$f" .mx)")
done < <(ls "$ROOT"/tests/regressions/should_compile/*.mx 2>/dev/null | sort)
while IFS= read -r d; do
	[ -f "$d/main.mx" ] || continue
	UNITS+=("$(basename "$d")")
done < <(ls -d "$ROOT"/tests/regressions/should_compile/*/ 2>/dev/null | sort)

# The emitted bundle is one more unit: whether a build stages a complete,
# loadable host beside the module is a property of the emitted output, not of
# any one program, and it must be re-checked when the runtime or the loader
# changes.
UNITS+=(emitted-host)
# A panic in a spawned domain is a browser contract no output-diff fixture can
# express: the module is SUPPOSED to die, and the page is supposed to stop the
# tree. It has its own harness (tests/wasm/panic_check.sh).
UNITS+=(panic-tree)
# The hub protocol is a Node test with fake ports: no browser, no wasm, no
# compiler. It is the fast half of this phase's evidence.
UNITS+=(hub-protocol)

target_for() {
	local name="$1"
	if [ -f "$ROOT/tests/regressions/should_compile/$name.mx" ]; then
		echo "$ROOT/tests/regressions/should_compile/$name.mx"
	elif [ -d "$ROOT/tests/regressions/should_compile/$name" ]; then
		echo "$ROOT/tests/regressions/should_compile/$name"
	fi
}

run_one() {
	local name="$1" target log
	unit_begin
	if [ "$name" = "hub-protocol" ]; then
		log="$UNIT_WORK/hub.log"
		if ! command -v node >/dev/null 2>&1; then
			unit_skipped "hub:protocol" "node is not available"
			unit_exit
			return
		fi
		if node --test "$ROOT/tests/supervisor/hub.test.mjs" >"$log" 2>&1; then
			unit_ok "hub:protocol" "$(grep -c '^ok ' "$log") protocol tests passed"
		else
			unit_no "hub:protocol" "$(grep -m1 '^not ok' "$log" | sed 's/^not ok [0-9]* - //')"
		fi
		unit_exit
		return
	fi
	if [ "$name" = "panic-tree" ]; then
		log="$UNIT_WORK/panic.log"
		if MOXIE="$MOXIE" bash "$ROOT/tests/wasm/panic_check.sh" >"$log" 2>&1; then
			unit_ok "wasm:panic-tree" "$(tail -1 "$log" | sed 's/^ *//')"
		else
			unit_no "wasm:panic-tree" "$(grep -m1 'FAIL:' "$log" | sed 's/^ *FAIL: *//')"
		fi
		unit_exit
		return
	fi
	if [ "$name" = "emitted-host" ]; then
		log="$UNIT_WORK/emit.log"
		if bash "$ROOT/tests/wasm/emit_check.sh" >"$log" 2>&1; then
			unit_ok "wasm:emitted-host" "$(tail -1 "$log" | sed 's/^ *//')"
		else
			unit_no "wasm:emitted-host" "$(grep -m1 'FAIL:' "$log" | sed 's/^ *FAIL: *//')"
		fi
		unit_exit
		return
	fi
	target="$(target_for "$name")"
	if [ -z "$target" ]; then
		unit_no "wasm:$name" "no such fixture"
		unit_exit
		return
	fi
	if ! browser_ok; then
		unit_skipped "wasm:$name" "chromium not available to playwright"
		unit_exit
		return
	fi
	log="$UNIT_WORK/validate.log"
	# `|| true` is not tolerance: util.sh runs under `set -e`, and validate.sh
	# exits nonzero exactly when a fixture FAILS, so without this the phase
	# script died on the first failure before printing the verdict it had
	# already written to the log - the whole phase reported "no output".
	MOXIE="$MOXIE" bash "$ROOT/tests/wasm/validate.sh" "$target" >"$log" 2>&1 || true
	# One verdict per target, taken from the harness's own line for it.
	local line
	line="$(grep -E "^$(basename "$name") +(PASS|FAIL|SKIP)" "$log" | head -1)"
	case "$line" in
	*PASS*) unit_ok "wasm:$name" "output matches native" ;;
	*SKIP*) unit_skipped "wasm:$name" "$(echo "$line" | sed 's/.*SKIP *//')" ;;
	*FAIL*) unit_no "wasm:$name" "$(echo "$line" | sed 's/.*FAIL *//')" ;;
	*) unit_no "wasm:$name" "harness produced no verdict: $(tail -1 "$log")" ;;
	esac
	unit_exit
}

unit_args "$@"

echo
echo "--- phase 5: wasm target (${#UNITS[@]} fixtures) ---"

if [ -n "$UNIT_NAME" ]; then
	if [ "$UNIT_NAME" = "whole-phase" ]; then
		# The driver's monolithic mode still runs the whole sweep, including
		# the emitted-bundle check, exactly as before.
		LOG="$TMPROOT/wasm-validate.log"
		mkdir -p "$TMPROOT"
		rc=0
		MOXIE="$MOXIE" bash "$ROOT/tests/wasm/validate.sh" >"$LOG" 2>&1 || rc=$?
		while read -r tname verdict rest; do
			case "$verdict" in
			PASS) log_pass "wasm:$tname" "${rest:-output matches native}" ;;
			FAIL) log_fail "wasm:$tname" "$rest" ;;
			SKIP) log_skip "wasm:$tname" "$rest" ;;
			esac
		done < <(grep -E '^[A-Za-z_0-9]+ +(PASS|FAIL|SKIP)' "$LOG")
		EMITLOG="$TMPROOT/wasm-emit.log"
		if bash "$ROOT/tests/wasm/emit_check.sh" >"$EMITLOG" 2>&1; then
			log_pass "wasm:emitted-host" "$(tail -1 "$EMITLOG" | sed 's/^ *//')"
		else
			log_fail "wasm:emitted-host" "$(grep -m1 'FAIL:' "$EMITLOG" | sed 's/^ *FAIL: *//')"
			rc=1
		fi
		exit "$rc"
	fi
	run_one "$UNIT_NAME"
	exit $?
fi

# No unit selected: the whole sweep, in order, as one process.
rc=0
for u in "${UNITS[@]}"; do
	run_one "$u" || rc=1
done

# The bundle a build PRODUCES is checked here rather than per fixture: it is a
# property of the emitted host, not of any one program.
LOG="$TMPROOT/wasm-emit.log"
mkdir -p "$TMPROOT"
if bash "$ROOT/tests/wasm/emit_check.sh" >"$LOG" 2>&1; then
	log_pass "wasm:emitted-host" "$(tail -1 "$LOG" | sed 's/^ *//')"
else
	log_fail "wasm:emitted-host" "$(grep -m1 'FAIL:' "$LOG" | sed 's/^ *FAIL: *//')"
	rc=1
fi
exit "$rc"
