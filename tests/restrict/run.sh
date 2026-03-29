#!/bin/bash
# Restriction test suite for Moxie.
# Tests that removed builtins, types, and statements produce compile errors,
# and that valid Moxie patterns compile and run correctly.
set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
TESTDIR="$(cd "$(dirname "$0")" && pwd)"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"

pass=0
fail=0

run_pass_test() {
    local src="$1"
    local name=$(basename "$src" .go)
    local bin="$TMPDIR/$name"

    if ! "$MOXIE" build -o "$bin" "$src" 2>"$TMPDIR/$name.err"; then
        echo "FAIL $name: compilation failed"
        cat "$TMPDIR/$name.err"
        fail=$((fail + 1))
        return
    fi

    local output
    output=$(timeout 5 "$bin" 2>&1 || true)

    if echo "$output" | grep -q "FAIL:"; then
        echo "FAIL $name: wrong values"
        echo "$output"
        fail=$((fail + 1))
        return
    fi
    if echo "$output" | grep -q "PASS:"; then
        local passes=$(echo "$output" | grep -c "PASS:" || true)
        echo "OK   $name ($passes checks)"
        pass=$((pass + 1))
    else
        echo "FAIL $name: no PASS markers in output"
        echo "$output"
        fail=$((fail + 1))
    fi
}

run_fail_test() {
    local src="$1"
    local pattern="$2"
    local name=$(basename "$src" .go)
    local bin="$TMPDIR/$name"

    local errout
    if errout=$("$MOXIE" build -o "$bin" "$src" 2>&1); then
        echo "FAIL $name: compilation should have failed"
        fail=$((fail + 1))
        return
    fi

    if echo "$errout" | grep -qi "$pattern"; then
        echo "OK   $name (rejected: $pattern)"
        pass=$((pass + 1))
    else
        echo "FAIL $name: expected error containing '$pattern'"
        echo "$errout"
        fail=$((fail + 1))
    fi
}

echo "=== Restriction positive tests ==="
for src in "$TESTDIR"/pass_*.go; do
    run_pass_test "$src"
done

echo ""
echo "=== Restriction negative tests ==="
run_fail_test "$TESTDIR/fail_new.go"              "new.*not allowed"
run_fail_test "$TESTDIR/fail_complex_builtin.go"   "complex.*not"
run_fail_test "$TESTDIR/fail_real.go"              "real.*not\|complex.*not"
run_fail_test "$TESTDIR/fail_imag.go"              "imag.*not\|complex.*not"
run_fail_test "$TESTDIR/fail_complex64.go"         "complex64"
run_fail_test "$TESTDIR/fail_complex128.go"        "complex128"
run_fail_test "$TESTDIR/fail_uintptr.go"           "uintptr"
run_fail_test "$TESTDIR/fail_fallthrough.go"       "fallthrough"

echo ""
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
