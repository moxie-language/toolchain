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

# The host must be able to answer every DOM call a module can compile.
# src/web/dom/dom_wasm.mx declares the bridge imports a program links against,
# and jsruntime/host.mjs provides them. A missing entry is not one fixture's
# problem: the whole module fails to instantiate, so it is said here once
# instead of arriving as an unexplained load error per target.
DOM_IMPORTS="$(grep -o 'wasmimport bridge dom_[a-z_0-9]*' "$ROOT/src/web/dom/dom_wasm.mx" | sed 's/.*dom_//' | sort -u)"
DOM_BRIDGE="$(grep -oE '^    dom_[a-z_0-9]+\(' "$ROOT/jsruntime/host.mjs" | sed 's/^ *dom_//;s/(//' | sort -u)"
DOM_MISSING="$(comm -23 <(echo "$DOM_IMPORTS") <(echo "$DOM_BRIDGE"))"
if [ -n "$DOM_MISSING" ]; then
    echo "wasm host is missing DOM bridge entries:" "$DOM_MISSING"
    exit 1
fi
# And the relay must not carry operations no Moxie program can ask for.
DOM_RELAY="$(sed -n 's/^  \([a-z_0-9]*\): [{].*/\1/p; s/^  \([a-z_0-9]*\): PENDING,.*/\1/p' "$ROOT/jsruntime/dom-ops.mjs" | sort -u)"
DOM_STALE="$(comm -13 <(echo "$DOM_IMPORTS") <(echo "$DOM_RELAY"))"
if [ -n "$DOM_STALE" ]; then
    echo "dom relay has operations the stdlib does not import:" "$DOM_STALE"
    exit 1
fi

# Programs to validate: each is a directory or .mx file containing a main().
TARGETS=()
if [ $# -gt 0 ]; then
    TARGETS=("$@")
else
    for f in "$ROOT"/tests/regressions/should_compile/*.mx; do
        [ -f "$f" ] || continue
        grep -q 'func main' "$f" || continue
        # A fixture can declare itself native-only. The builtin Stringers exist
        # only under the moxie.stringer build tag, which the wasm target does not
        # carry, so a fixture that calls .String() on a builtin cannot even
        # COMPILE for wasm - and a wasm build failure is a FAIL here, correctly,
        # because it usually means the compiler is broken. The opt-out is
        # explicit and opt-in: the fixture says so, and no pattern matching can
        # quietly excuse a real failure.
        grep -q '//:wasmtest skip' "$f" && continue
        TARGETS+=("$f")
    done
    # Directory cases: main.mx plus a lib/ subpackage. A cross-package call is
    # not inlined and crosses the return codec, so whole classes of layout bug
    # are only reachable this way.
    for d in "$ROOT"/tests/regressions/should_compile/*/; do
        [ -f "$d/main.mx" ] || continue
        TARGETS+=("${d%/}")
    done
fi

rm -rf "$WORK"; mkdir -p "$WORK"

# Start from an empty package cache: each run measures a cold compile, not
# whatever an earlier run happened to cache. The cross-target corruption this
# used to also guard against ("Cannot select: intrinsic %llvm.wasm.memory.size"
# from a native build linking a wasm object) is fixed at the source: a compiler
# process now owns a private scratch directory (buildScratchDir), so concurrent
# builds cannot overwrite each other's IR between clang writing it and the read
# that caches it, and each target has its own cache root.
rm -rf "$HOME/moxie/cache" 2>/dev/null
if [ -n "${MOXIEPATH:-}" ]; then
    rm -rf "$MOXIEPATH/cache" 2>/dev/null
fi

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

    # Run in a browser. A module that spins never yields, so the driver cannot
    # deliver run.py's own wait_for_function timeout and the call blocks
    # forever: bound each run here, or one bad module stalls the whole suite
    # and hides every later result. RUN_TIMEOUT is seconds.
    timeout "${RUN_TIMEOUT:-60}" python3 "$ROOT/tests/wasm/run.py" -q "$d/app.wasm" >"$d/wasm.out" 2>&1
    rrc=$?
    if [ "$rrc" -ne 0 ]; then
        if [ "$rrc" -eq 124 ]; then
            printf '%-32s FAIL  (browser run timed out after %ss)\n' "$name" "${RUN_TIMEOUT:-60}"
        else
            printf '%-32s FAIL  (browser run failed)\n' "$name"
        fi
        tail -3 "$d/wasm.out" | sed 's/^/      /'
        FAIL=$((FAIL+1))
        continue
    fi
    cp "$d/wasm.out" "$d/wasm.clean"

    # A browser-only fixture: it calls the wasm-only packages, which are panic
    # stubs off-target, so the native binary cannot be the reference. The
    # browser run above already had to exit clean, and the fixture asserts the
    # round trip itself - a wrong answer panics inside the module - so the
    # browser result stands alone rather than being compared to a panic.
    if grep -q '//:wasmtest browser-only' "$t" 2>/dev/null; then
        printf '%-32s PASS  (browser only)\n' "$name"
        PASS=$((PASS+1))
        continue
    fi

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
