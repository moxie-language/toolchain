#!/bin/bash
# Phase 7: the sample UI app, from a served build.
#
# The stdlib web framework's only real consumer in this workspace is the smesh
# app, and until this phase nothing in the gate ran it: phases 0a/0b/1/3/5 test
# the compiler, and 5 drives wasm fixtures through a browser but never the app.
# So a change that broke the framework's contract with a real UI would have left
# every gate green. The relay and the app are separate repos now (../morly and
# ../smesh): this phase builds BOTH from source with the compiler under test,
# has the relay serve this app's static tree, and runs the app's own smoke suite
# (test/test_app.py) which covers its main flows.
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
# Both build products go to a temp dir, so the app checkout is never touched:
# the relay binary, and a staged COPY of web/static that the bundles are built
# into. The app TRACKS its bundles as build products, so building in place
# rewrote nine tracked binaries on every run and left the app repo dirty for a
# reason the app never asked for. Staging keeps the finding (did the bundles
# build and serve under this compiler?) without writing to a sibling repo.
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

# The app checks in its built bundles under web/static. Building them from the
# compiler under test is the point of the phase, so the whole static tree is
# copied first and every build output lands in the copy.
STAGE="$(tempdir ui)/static"
[ -d "$APP" ] || fail "preconditions" "app repo not found at $APP (set MOXIE_UI_APP)"
[ -f "$APP/test/test_app.py" ] || fail "preconditions" "no smoke suite at $APP/test/test_app.py"
[ -x "$MOXIE" ] || fail "preconditions" "compiler under test not executable: $MOXIE"
command -v python3 >/dev/null 2>&1 || fail "preconditions" "python3 not found"
python3 -c 'import selenium, pytest, websocket' >/dev/null 2>&1 \
	|| fail "preconditions" "python3 is missing selenium/pytest/websocket-client"
python3 -c 'import playwright' >/dev/null 2>&1 && : # playwright is for other phases; not required here

cp -a "$APP/web/static" "$STAGE" || fail "preconditions" "could not stage $APP/web/static"

# ── build the relay and the app bundle from source with the compiler under test
BUILD_LOG="$(tempdir ui)/build.log"
rm -rf "$BUILD_LOG"

# The relay is its OWN repo (RLY, default ../morly): the app is the front end and
# the relay lives elsewhere, so this phase builds the relay from THAT source and
# points it at this checkout's static tree. A relay build failure is a FAIL like
# any other: the earlier arrangement tried the build in the app repo, reported
# the outcome, and quietly served a prebuilt binary when it failed, which is
# exactly the state a broken compiler would keep green.
RLY="${MOXIE_RELAY_REPO:-$(cd "$ROOT/.." && pwd)/morly}"
[ -d "$RLY" ] || fail "preconditions" "relay repo not found at $RLY (set MOXIE_RELAY_REPO)"
RELAY_BIN="$(tempdir ui)/morly"
if ! (cd "$RLY" && MOXIEROOT="$ROOT" "$MOXIE" build -o "$RELAY_BIN" . >"$BUILD_LOG" 2>&1); then
	fail "relay-build" "$(grep -m1 -oE '^; .*|mxc: .*' "$BUILD_LOG" | tail -1 | cut -c1-160)"
fi
[ -x "$RELAY_BIN" ] || fail "relay-build" "the relay build produced no binary"
relay_note="relay built from source at $RLY"

bundles=0
build_failed=""
for b in app signer relay-proxy store verify profile notif feed mls; do
	[ -d "$APP/web/wasm/$b" ] || continue
	if ! (cd "$APP" && MOXIEROOT="$ROOT" "$MOXIE" build -allow-closure-captures \
			-target wasm -o "$STAGE/$b.wasm" "./web/wasm/$b/" \
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
[ -s "$STAGE/app.wasm" ] || fail "build" "app.wasm was not produced"
[ -d "$STAGE/\$runtime" ] || fail "build" "the runtime staging (\$runtime) is missing"
[ -s "$STAGE/bridge-common.mjs" ] || fail "build" "bridge-common.mjs is missing"
log_pass "ui:build" "$bundles bundles + runtime staging; $relay_note"

# ── run the app's own smoke suite; it starts and stops the relay itself
PY_LOG="$(tempdir ui)/pytest.log"
rm -rf "$PY_LOG"
(cd "$APP" && SMESH_RELAY_BIN="$RELAY_BIN" \
	ORLY_STATIC_DIR="$STAGE" \
	timeout "$SUITE_TIMEOUT" python3 -m pytest test/test_app.py -q) >"$PY_LOG" 2>&1
rc=$?

summary="$(grep -E '^[0-9]+ (passed|failed)|passed|failed' "$PY_LOG" | tail -1)"
if [ "$rc" = 0 ]; then
	log_pass "ui:smoke" "${summary:-passed}"
else
	log_fail "ui:smoke" "$(tail -6 "$PY_LOG" | tr '\n' ' ')"
fi

exit "$rc"
