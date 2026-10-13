#!/bin/bash
# Check the bundle a build PRODUCES, not the repository it came from.
#
# `moxie build -target wasm` stages the page-side half of the module beside it:
# the JS runtime in $runtime/ and the page bootstrap <name>-wasm-host.mjs. This
# builds a fixture, checks the staged set against jsruntime/, and then loads
# THAT bootstrap in Chromium - so what is proven is the scaffolding an app
# ships, not the checkout's own copy.
#
# Usage: emit_check.sh [fixture.mx]      (default: the DOM round-trip fixture)
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MOXIE="${MOXIE:-$ROOT/moxie}"
FIXTURE="${1:-$ROOT/tests/regressions/should_compile/web_dom_roundtrip.mx}"
EXPECTED="$ROOT/tests/wasm/expected/$(basename "$FIXTURE" .mx).txt"
WORK="$ROOT/tests/wasm/tmp/emit"
NAME="$(basename "$FIXTURE" .mx)"

fail() {
	echo "  FAIL: $*"
	exit 1
}

rm -rf "$WORK"
mkdir -p "$WORK/pkg"
cp "$FIXTURE" "$WORK/pkg/main.mx"

if ! (cd "$ROOT" && MOXIEROOT="$ROOT" "$MOXIE" build -target wasm \
	-o "$WORK/$NAME.wasm" "$WORK/pkg") >"$WORK/build.log" 2>&1; then
	fail "wasm build failed"
	tail -3 "$WORK/build.log" | sed 's/^/      /'
fi

[ -f "$WORK/$NAME.wasm" ] || fail "no module at $WORK/$NAME.wasm"
[ -d "$WORK/\$runtime" ] || fail "the build staged no \$runtime/ beside the module"
[ -f "$WORK/$NAME-wasm-host.mjs" ] || fail "the build wrote no $NAME-wasm-host.mjs"

# The staged runtime is the whole JS runtime, byte for byte. A partial copy is
# the failure mode this check exists for: a bundle that loads in the checkout
# and 404s in a deployed directory.
want=$(ls "$ROOT"/jsruntime/*.mjs | wc -l)
have=$(ls "$WORK/\$runtime"/*.mjs 2>/dev/null | wc -l)
[ "$have" = "$want" ] || fail "staged $have runtime modules, jsruntime/ has $want"
for f in "$ROOT"/jsruntime/*.mjs; do
	b="$(basename "$f")"
	cmp -s "$f" "$WORK/\$runtime/$b" || fail "staged $b differs from jsruntime/$b"
done

grep -q "\./$NAME\.wasm" "$WORK/$NAME-wasm-host.mjs" || fail "the bootstrap does not point at $NAME.wasm"

# Serve the emitted directory as /tmp/emit/ (run.py maps tests/wasm/ to /).
URL="/tmp/emit/$NAME-wasm-host.mjs"
timeout "${RUN_TIMEOUT:-60}" python3 "$ROOT/tests/wasm/run.py" -q --host "$URL" \
	>"$WORK/browser.out" 2>&1
rc=$?
[ "$rc" -eq 0 ] || { fail "the emitted bootstrap failed to run (rc=$rc)"; tail -3 "$WORK/browser.out" | sed 's/^/      /'; }

if [ -f "$EXPECTED" ]; then
	diff -q "$EXPECTED" "$WORK/browser.out" >/dev/null || {
		fail "the emitted bundle produced different output"
		diff "$EXPECTED" "$WORK/browser.out" | head -6 | sed 's/^/      /'
	}
fi

# The bootstrap text exists twice, once per compiler, as string literals. The
# two must describe the same file or a bundle built by legacy would differ from
# one built by stage4 - the bilateral rule, checked at the only place it can be
# without linking the legacy binary (which needs LLVM 22, absent here).
python3 - "$ROOT" <<'PYEOF' || fail "the legacy and stage4 emitters no longer describe the same bootstrap"
import pathlib, re, sys

def literals(path, start_marker):
    src = pathlib.Path(path).read_text()
    i = src.index(start_marker)
    # Bound to the function body: the next top-level closing brace.
    body = src[i:]
    body = body[:body.index("\n}\n")]
    # The function's own string literals, in source order, unescaped.
    out = []
    for lit in re.findall(r'"((?:[^"\\]|\\.)*)"', body):
        out.append(lit.encode().decode('unicode_escape'))
    return "".join(out)

stage4 = literals(sys.argv[1] + "/_mxc_stage4/main.mx", "func wasmHostModule")
legacy = literals(sys.argv[1] + "/legacy/builder/wasmhost.go", "func wasmHostModule")

def code(text):
    # The parts outside a function body: the comment is documentation, the
    # executable lines are what a page runs.
    return [l for l in text.splitlines()
            if l and not l.lstrip().startswith("//")]

if code(stage4) != code(legacy):
    print("  stage4:", code(stage4))
    print("  legacy:", code(legacy))
    raise SystemExit(1)
if len(code(stage4)) == 0:
    raise SystemExit("no bootstrap code found to compare")
PYEOF

# The service worker's half of the same bundle. Its host is staged by the same
# build; this builds the module the host instantiates, registers the worker from
# a page through the emitted glue, and requires activation with the module
# actually fetched - a worker whose host or module is broken never gets there.
SWFIX="$ROOT/tests/regressions/should_compile/web_sw_boot.mx"
if [ -f "$SWFIX" ]; then
	ls "$WORK/\$sw" >/dev/null 2>&1 || fail "the build staged no \$sw/ beside the module"
	[ -f "$WORK/\$sw/wasm-host-sw.mjs" ] || fail "the build staged no \$sw/wasm-host-sw.mjs"
	[ -f "$WORK/sw-register.js" ] || fail "the build wrote no sw-register.js"
	mkdir -p "$WORK/swpkg"
	cp "$SWFIX" "$WORK/swpkg/main.mx"
	if ! (cd "$ROOT" && MOXIEROOT="$ROOT" "$MOXIE" build -target wasm \
		-o "$WORK/\$sw/sw.wasm" "$WORK/swpkg") >"$WORK/sw-build.log" 2>&1; then
		fail "service-worker module build failed"
		tail -3 "$WORK/sw-build.log" | sed 's/^/      /'
	fi
	timeout "${RUN_TIMEOUT:-60}" python3 "$ROOT/tests/wasm/run.py" -q \
		--host "/tmp/emit/$NAME-wasm-host.mjs" --sw /tmp/emit/sw-register.js \
		>"$WORK/sw-browser.out" 2>"$WORK/sw-browser.err"
	rc=$?
	[ "$rc" -eq 0 ] || {
		fail "the emitted service worker did not boot (rc=$rc)"
		tail -3 "$WORK/sw-browser.err" | sed 's/^/      /'
	}
	grep -q "state=activated module_fetched=true" "$WORK/sw-browser.err" || {
		fail "the emitted service worker did not activate with its module fetched"
		tail -3 "$WORK/sw-browser.err" | sed 's/^/      /'
	}
	echo "  the emitted service worker booted: $(tail -1 "$WORK/sw-browser.err")"
fi

# A bundle whose module imports symbols only its app can answer: built here, run
# with the app's own bridge module, and diffed against its expected output. The
# generic phase-5 loop cannot run this one (the plain host has no app bridge by
# definition), which is why it lives with the emitted-bundle checks.
APPFIX="$ROOT/tests/wasm/fixtures/app_bridge/main.mx"
if [ -f "$APPFIX" ]; then
	mkdir -p "$WORK/apppkg"
	cp "$APPFIX" "$WORK/apppkg/main.mx"
	if ! (cd "$ROOT" && MOXIEROOT="$ROOT" "$MOXIE" build -target wasm \
		-o "$WORK/app_bridge.wasm" "$WORK/apppkg") >"$WORK/app-build.log" 2>&1; then
		fail "app-bridge fixture build failed"
		tail -3 "$WORK/app-build.log" | sed 's/^/      /'
	fi
	timeout "${RUN_TIMEOUT:-60}" python3 "$ROOT/tests/wasm/run.py" -q \
		--app /app-bridge-probe.mjs "$WORK/app_bridge.wasm" >"$WORK/app-browser.out" 2>&1
	rc=$?
	[ "$rc" -eq 0 ] || { fail "the bundle with an app bridge did not run (rc=$rc)"; tail -3 "$WORK/app-browser.out" | sed 's/^/      /'; }
	EXPECTED="$ROOT/tests/wasm/expected/web_app_bridge.txt"
	if [ -f "$EXPECTED" ]; then
		diff -q "$EXPECTED" "$WORK/app-browser.out" >/dev/null || {
			fail "the app bridge produced different output"
			diff "$EXPECTED" "$WORK/app-browser.out" | head -4 | sed 's/^/      /'
		}
	fi
	echo "  the app bridge answered an import the host does not provide: $(cat "$WORK/app-browser.out")"
fi

echo "  the build staged $have runtime modules and a loadable bootstrap"
exit 0
