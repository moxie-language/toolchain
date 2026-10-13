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

echo "  the build staged $have runtime modules and a loadable bootstrap"
exit 0
