#!/bin/bash
# Restriction test suite for Moxie.
# Tests that removed builtins, types, and statements produce compile errors,
# and that valid Moxie patterns compile and run correctly.
set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
MOXIE="$(cd "$(dirname "$MOXIE")" && pwd)/$(basename "$MOXIE")"
# MOXIEROOT must point to the legacy-spec source tree (two levels up from tests/restrict/).
export MOXIEROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
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

    if ! (cd "$moddir" && "$MOXIE" build -no-debug -o "$bin" . 2>"$moddir/err.log"); then
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
    if errout=$(cd "$moddir" && "$MOXIE" build -no-debug -o "$bin" . 2>&1); then
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
run_fail_test "$TESTDIR/fail_new.mx"              "undefined: new"
run_fail_test "$TESTDIR/fail_complex_builtin.mx"   "undefined: complex"
run_fail_test "$TESTDIR/fail_real.mx"              "undefined: real"
run_fail_test "$TESTDIR/fail_imag.mx"              "undefined: imag"
run_fail_test "$TESTDIR/fail_complex64.mx"         "undefined: complex64"
run_fail_test "$TESTDIR/fail_complex128.mx"        "undefined: complex128"
run_fail_test "$TESTDIR/fail_fallthrough.mx"       "fallthrough"
run_fail_test "$TESTDIR/fail_int.mx"               "undefined: int"
run_fail_test "$TESTDIR/fail_uint.mx"              "undefined: uint"
run_fail_test "$TESTDIR/fail_go_keyword.mx"        "go.*not allowed"
run_fail_test "$TESTDIR/fail_any.mx"               "undefined: any"
run_fail_test "$TESTDIR/fail_append_spread.mx"     "undefined: append"
run_fail_test "$TESTDIR/fail_value_recv.mx"        "value receiver.*not allowed"
run_fail_test "$TESTDIR/fail_unnamed_return.mx"    "unnamed return.*not allowed"
run_fail_test "$TESTDIR/fail_pkg_var_init.mx"      "package-level var.*not allowed"
run_fail_test "$TESTDIR/fail_named_slice.mx"       "named slice.*not allowed"
run_fail_test "$TESTDIR/fail_named_map.mx"         "named map.*not allowed"
run_fail_test "$TESTDIR/fail_shadow.mx"            "shadows"
run_fail_test "$TESTDIR/fail_shadow_builtin.mx"    "shadows"
run_fail_test "$TESTDIR/fail_hash_nonident.mx"     "# must be followed"
run_fail_test "$TESTDIR/fail_hash_redecl.mx"       "redeclared"
run_fail_test "$TESTDIR/fail_init_in_main.mx"      "init.*not allowed.*main"
run_fail_test "$TESTDIR/fail_init_select.mx"        "select.*not allowed.*init"
run_fail_test "$TESTDIR/fail_init_spawn.mx"         "spawn.*not allowed.*init"
run_fail_test "$TESTDIR/fail_global_mutate.mx"      "package globals are immutable outside init"

echo ""
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
