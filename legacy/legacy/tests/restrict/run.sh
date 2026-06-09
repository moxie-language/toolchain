#!/bin/bash
# Restriction test suite for Moxie.
# Tests that removed builtins, types, and statements produce compile errors,
# and that valid Moxie patterns compile and run correctly.
set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
MOXIE="$(cd "$(dirname "$MOXIE")" && pwd)/$(basename "$MOXIE")"
TESTDIR="$(cd "$(dirname "$0")" && pwd)"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"

pass=0
fail=0

# Create a temp module directory, copy a single .mx file into it as main.mx,
# and return the module directory path. Each test gets an isolated module.
setup_module() {
    local src="$1"
    local name=$(basename "$src" .mx)
    local moddir="$TMPDIR/mod_$name"
    mkdir -p "$moddir"
    cp "$src" "$moddir/main.mx"
    printf 'module %s\n\nmoxie 0.1.0\n' "$name" > "$moddir/moxie.mod"
    echo "$moddir"
}

run_pass_test() {
    local src="$1"
    local name=$(basename "$src" .mx)
    local moddir
    moddir=$(setup_module "$src")
    local bin="$moddir/$name"

    if ! (cd "$moddir" && "$MOXIE" build -o "$bin" . 2>"$moddir/err.log"); then
        echo "FAIL $name: compilation failed"
        cat "$moddir/err.log"
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
    local name=$(basename "$src" .mx)
    local moddir
    moddir=$(setup_module "$src")
    local bin="$moddir/$name"

    local errout
    if errout=$(cd "$moddir" && "$MOXIE" build -o "$bin" . 2>&1); then
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
for src in "$TESTDIR"/pass_*.mx; do
    run_pass_test "$src"
done

echo ""
echo "=== Restriction negative tests ==="
run_fail_test "$TESTDIR/fail_new.mx"              "new.*not allowed"
run_fail_test "$TESTDIR/fail_complex_builtin.mx"   "complex.*not"
run_fail_test "$TESTDIR/fail_real.mx"              "real.*not\|complex.*not"
run_fail_test "$TESTDIR/fail_imag.mx"              "imag.*not\|complex.*not"
run_fail_test "$TESTDIR/fail_complex64.mx"         "complex64"
run_fail_test "$TESTDIR/fail_complex128.mx"        "complex128"
run_fail_test "$TESTDIR/fail_uintptr.mx"           "uintptr"
run_fail_test "$TESTDIR/fail_fallthrough.mx"       "fallthrough"

echo ""
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
