#!/bin/bash
# Moxie full test suite. Runs all test suites and reports results.
set -euo pipefail

TESTDIR="$(cd "$(dirname "$0")" && pwd)"
MOXIE="${MOXIE:-./moxie}"
export MOXIE PATH="/usr/lib/llvm19/bin:$PATH"

total_pass=0
total_fail=0

run_suite() {
    local name="$1"
    local script="$2"
    echo "╔══ $name ══╗"
    if bash "$script"; then
        total_pass=$((total_pass + 1))
    else
        total_fail=$((total_fail + 1))
    fi
    echo ""
}

# Cross-compilation smoke test: verify binaries are produced for each target.
cross_compile_check() {
    local src="$TESTDIR/restrict/pass_valid_patterns.go"
    local tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" RETURN

    echo "╔══ Cross-compilation ══╗"
    local pass=0
    local fail=0

    for target in linux/amd64 linux/arm64; do
        local goos="${target%/*}"
        local goarch="${target#*/}"
        local bin="$tmpdir/${goos}_${goarch}"
        if GOOS="$goos" GOARCH="$goarch" "$MOXIE" build -o "$bin" "$src" 2>/dev/null; then
            local fmt=$(file "$bin" | grep -oP '(ELF|Mach-O)[^,]*' | head -1)
            echo "OK   $target ($fmt)"
            pass=$((pass + 1))
        else
            echo "FAIL $target: compilation failed"
            fail=$((fail + 1))
        fi
    done

    echo ""
    echo "=== Cross-compile: $pass passed, $fail failed ==="
    [ "$fail" -eq 0 ]
}

run_suite "Restriction tests" "$TESTDIR/restrict/run.sh"
run_suite "Spawn tests" "$TESTDIR/spawn/run.sh"

if cross_compile_check; then
    total_pass=$((total_pass + 1))
else
    total_fail=$((total_fail + 1))
fi

echo ""
echo "════════════════════════════════════════"
echo "  Suites: $total_pass passed, $total_fail failed"
echo "════════════════════════════════════════"
[ "$total_fail" -eq 0 ]
