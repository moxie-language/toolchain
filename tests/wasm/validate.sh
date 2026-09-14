#!/bin/bash
# Validate the wasm target against native.
#
# Every program with a main() is compiled twice - once natively and once for
# -target wasm - then both are run and their stdout compared. Native is the
# reference: the wasm module must produce identical output in a real browser.
# This is the evidence that the wasm target is correct, not merely that it
# links.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
MOXIE="${MOXIE:-$ROOT/moxie}"
WORK="${WORK:-$ROOT/tests/wasm/tmp}"
PASS=0
FAIL=0

# Programs to validate: each is a directory or .mx file containing a main().
TARGETS=()
if [ $# -gt 0 ]; then
    TARGETS=("$@")
else
    for f in "$ROOT"/tests/regressions/should_compile/*.mx; do
        [ -f "$f" ] || continue
        grep -q 'func main' "$f" && TARGETS+=("$f")
    done
fi

rm -rf "$WORK"; mkdir -p "$WORK"

for t in "${TARGETS[@]}"; do
    name="$(basename "$t")"; name="${name%.mx}"
    d="$WORK/$name"
    mkdir -p "$d/pkg"
    if [ -f "$t" ]; then
        cp "$t" "$d/pkg/main.mx"
    else
        cp "$t"/*.mx "$d/pkg/" 2>/dev/null
    fi
    src="$d/pkg"

    # native reference
    if ! (cd "$ROOT" && MOXIEROOT="$ROOT" "$MOXIE" build -o "$d/native" "$src") >"$d/native.log" 2>&1; then
        printf '%-32s SKIP  (native build failed)\n' "$name"
        continue
    fi
    "$d/native" >"$d/native.out" 2>&1
    nrc=$?

    # wasm module
    if ! (cd "$ROOT" && MOXIEROOT="$ROOT" "$MOXIE" build -target wasm -o "$d/app.wasm" "$src") >"$d/wasm.log" 2>&1; then
        printf '%-32s FAIL  (wasm build failed)\n' "$name"
        tail -3 "$d/wasm.log" | sed 's/^/      /'
        FAIL=$((FAIL+1))
        continue
    fi

    # run in a browser
    if ! python3 "$ROOT/tests/wasm/run.py" -q "$d/app.wasm" >"$d/wasm.out" 2>&1; then
        printf '%-32s FAIL  (browser run failed)\n' "$name"
        tail -3 "$d/wasm.out" | sed 's/^/      /'
        FAIL=$((FAIL+1))
        continue
    fi
    cp "$d/wasm.out" "$d/wasm.clean"

    if [ "$nrc" != "0" ]; then
        printf '%-32s SKIP  (native exited %s)\n' "$name" "$nrc"
        continue
    fi
    if diff -q "$d/native.out" "$d/wasm.clean" >/dev/null; then
        printf '%-32s PASS\n' "$name"
        PASS=$((PASS+1))
    else
        printf '%-32s FAIL  (output differs)\n' "$name"
        diff "$d/native.out" "$d/wasm.clean" | head -6 | sed 's/^/      /'
        FAIL=$((FAIL+1))
    fi
done

echo
echo "wasm validation: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
