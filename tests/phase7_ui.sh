#!/bin/bash
# Phase 7: the sample UI app, from a served build.
#
# The stdlib web framework's only real consumer in this workspace is the smesh
# app, and until this phase nothing in the gate ran it: phases 0a/0b/1/3/5 test
# the compiler, and 5 drives wasm fixtures through a browser but never the app.
# So a change that broke the framework's contract with a real UI would have left
# every gate green. This phase builds BOTH the relay and the app bundles from
# source with the compiler under test, serves the app from the relay it just
# built, and runs the app's own smoke suite (test/test_app.py) which covers its
# main flows.
#
# FAIL-CLOSED. Every precondition below is a FAIL, not a SKIP:
#
#   * the app repo is missing
#   * the compiler under test is missing
#   * the relay will not build, or the wasm bundle will not build
#   * the relay does not come up, or pytest/selenium is absent
#   * any smoke test fails
#
# A skip would report "not verified" as though it were "nothing to verify", and
# the whole point of the phase is that the UI is verified on every default run.
# This host has the browser stack (selenium + a chromium), so there is no
# exception to make here.
#
# The relay goes to a temp dir, so the app checkout keeps its own binary. The
# wasm bundles overwrite web/static/*.wasm, which the app TRACKS as build
# products; that is the app's arrangement, and the phase leaves them rebuilt
# from the compiler under test rather than restoring them.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 7: UI app smoke (served build) ==="

# The app lives beside this checkout in the workspace. MOXIE_UI_APP overrides it
# for a different layout; the default is the workspace's own arrangement, not a
# guess about an installed tree.
APP="${MOXIE_UI_APP:-$(cd "$ROOT/.." && pwd)/smesh}"
MOXIE="${MOXIE:-$ROOT/moxie}"
SUITE_TIMEOUT="${UI_SUITE_TIMEOUT:-600}"

fail() {
	log_fail "ui:${1}" "$2"
	exit 1
}

[ -d "$APP" ] || fail "preconditions" "app repo not found at $APP (set MOXIE_UI_APP)"
[ -f "$APP/test/test_app.py" ] || fail "preconditions" "no smoke suite at $APP/test/test_app.py"
[ -x "$MOXIE" ] || fail "preconditions" "compiler under test not executable: $MOXIE"
command -v python3 >/dev/null 2>&1 || fail "preconditions" "python3 not found"
python3 -c 'import selenium, pytest, websocket' >/dev/null 2>&1 \
	|| fail "preconditions" "python3 is missing selenium/pytest/websocket-client"
python3 -c 'import playwright' >/dev/null 2>&1 && : # playwright is for other phases; not required here

# ── build the relay and the app bundle from source with the compiler under test
BUILD_LOG="$(tempdir ui)/build.log"
rm -rf "$BUILD_LOG"

# The relay is built from source and the app serves THAT binary. The port is
# complete (see the relay port entries in docs/OPEN_WORK.md), so a relay build
# failure is a FAIL like any other: the earlier arrangement tried the build,
# reported the outcome, and quietly served a prebuilt binary when it failed,
# which is exactly the state a broken compiler would keep green.
RELAY_BIN="$(tempdir ui)/smesh"
if ! (cd "$APP" && MOXIEROOT="$ROOT" "$MOXIE" build -o "$RELAY_BIN" . >"$BUILD_LOG" 2>&1); then
	fail "relay-build" "$(grep -m1 -oE '^; .*|mxc: .*' "$BUILD_LOG" | tail -1 | cut -c1-160)"
fi
[ -x "$RELAY_BIN" ] || fail "relay-build" "the relay build produced no binary"
relay_note="relay built from source"

bundles=0
build_failed=""
for b in app signer relay-proxy store verify profile notif feed mls; do
	[ -d "$APP/web/wasm/$b" ] || continue
	if ! (cd "$APP" && MOXIEROOT="$ROOT" "$MOXIE" build -allow-closure-captures \
			-target wasm -o "$APP/web/static/$b.wasm" "./web/wasm/$b/" \
			>>"$BUILD_LOG" 2>&1); then
		build_failed="bundle $b: $(grep -m1 -oE '^; .*|mxc: .*' "$BUILD_LOG" | tail -1)"
		break
	fi
	bundles=$((bundles+1))
done

if [ -n "$build_failed" ]; then
	fail "build" "$build_failed"
fi

# A served build needs the module AND the staged runtime beside it; a build that
# emitted the module but not $runtime/bridge-common.mjs would serve a page that
# never boots, and this is the phase that should catch that.
[ -s "$APP/web/static/app.wasm" ] || fail "build" "app.wasm was not produced"
[ -d "$APP/web/static/\$runtime" ] || fail "build" "the runtime staging (\$runtime) is missing"
[ -s "$APP/web/static/bridge-common.mjs" ] || fail "build" "bridge-common.mjs is missing"
log_pass "ui:build" "$bundles bundles + runtime staging; $relay_note"

# ── run the app's own smoke suite; it starts and stops the relay itself
PY_LOG="$(tempdir ui)/pytest.log"
rm -rf "$PY_LOG"
(cd "$APP" && SMESH_RELAY_BIN="$RELAY_BIN" timeout "$SUITE_TIMEOUT" \
	python3 -m pytest test/test_app.py -q) >"$PY_LOG" 2>&1
rc=$?

summary="$(grep -E '^[0-9]+ (passed|failed)|passed|failed' "$PY_LOG" | tail -1)"
if [ "$rc" = 0 ]; then
	log_pass "ui:smoke" "${summary:-passed}"
else
	log_fail "ui:smoke" "$(tail -6 "$PY_LOG" | tr '\n' ' ')"
fi

exit "$rc"
