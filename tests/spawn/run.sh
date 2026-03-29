#!/bin/bash
# Spawn test suite for Moxie.
# Runs each test program through the moxie compiler and checks results.
set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
TESTDIR="$(cd "$(dirname "$0")" && pwd)"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"

pass=0
fail=0
skip=0

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

    # Check that no FAIL markers appear and at least one PASS marker does.
    if echo "$output" | grep -q "FAIL:"; then
        echo "FAIL $name: wrong values received"
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

echo "=== Spawn positive tests ==="
for src in "$TESTDIR"/pass_*.go; do
    run_pass_test "$src"
done

echo ""
echo "=== Spawn negative tests ==="
run_fail_test "$TESTDIR/fail_pointer.go"      "pointer"
run_fail_test "$TESTDIR/fail_func.go"          "function"
run_fail_test "$TESTDIR/fail_interface.go"     "interface"
run_fail_test "$TESTDIR/fail_empty_iface.go"   "interface"
run_fail_test "$TESTDIR/fail_slice_iface.go"   "interface"
run_fail_test "$TESTDIR/fail_struct_chan.go"    "channel"
run_fail_test "$TESTDIR/fail_array_chan.go"     "channel"
run_fail_test "$TESTDIR/fail_alias.go"         "after spawn"
run_fail_test "$TESTDIR/fail_type_mismatch.go" "expects"
run_fail_test "$TESTDIR/fail_wrong_count.go"   "expects"

echo ""
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
