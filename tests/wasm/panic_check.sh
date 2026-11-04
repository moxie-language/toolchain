#!/bin/bash
# The wasm half of the panic contract: a panic in a spawned domain posts its
# message to the page as stderr, and the page stops every domain it spawned for
# the module. tests/wasm/panic_tree/main.mx makes both halves observable - the
# panicker prints, the sibling would print 1.5 s later - and this harness runs
# the module, waits three seconds, and requires exactly one of them.
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MOXIE="${MOXIE:-$ROOT/moxie}"
TD="$(mktemp -d)"
trap 'rm -rf "$TD"' EXIT

fail() {
	echo "FAIL: $1"
	exit 1
}

if ! (cd "$ROOT" && MOXIEROOT="$ROOT" "$MOXIE" build -target wasm \
	-o "$TD/app.wasm" "$ROOT/tests/wasm/panic_tree") >"$TD/build.log" 2>&1; then
	fail "wasm build: $(tail -2 "$TD/build.log" | tr '\n' ' ')"
fi

RUN_SETTLE_MS=3000 timeout 60 python3 "$ROOT/tests/wasm/run.py" -q "$TD/app.wasm" \
	>"$TD/out" 2>"$TD/err"
rc=$?

# The module must die: the page's loader rejects when the tree stops.
[ "$rc" = "1" ] || fail "browser run exited $rc (want 1: the domain died)"
# The diagnostic must be the panic's own text, on stderr where the page prints it.
grep -q 'panic: sibling died' "$TD/err" || fail "stderr did not carry the panic: $(tail -3 "$TD/err" | tr '\n' ' ')"
# The generic action itself must have run, not merely printed: the page records
# it on the hook host.html defines.
grep -q 'panic action:' "$TD/err" || fail "the page's panic action did not run"
# The sibling must not have acted. Its print would have been forwarded through
# the root worker, which is the observable proof the whole tree was stopped.
if grep -q 'sibling survived' "$TD/out" "$TD/err"; then
	fail "the sibling survived: the page did not stop the tree"
fi
echo "PASS: panic reached the page, the tree stopped"
