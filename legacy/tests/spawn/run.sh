#!/bin/bash
# Spawn test suite for Moxie.
# Runs each test program through the moxie compiler and checks results.
set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../../.." && pwd)}"
export MOXIEROOT
TESTDIR="$(cd "$(dirname "$0")" && pwd)"
TMPDIR=$(mktemp -d)
STAGEROOT="$MOXIEROOT/tests/_spawn_stage_$$"
mkdir -p "$STAGEROOT"
trap "rm -rf $TMPDIR $STAGEROOT" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"

pass=0
fail=0
skip=0

# stage_src copies $1 to a fresh per-test package directory under the moxie
# tree so the package resolver can find it, and prints the package path.
stage_src() {
    local src="$1"
    local name=$(basename "$src" .go)
    local stage="$STAGEROOT/$name"
    mkdir -p "$stage"
    cp "$src" "$stage/main.go"
    echo "./tests/_spawn_stage_$$/$name"
}

run_pass_test() {
    local src="$1"
    local name=$(basename "$src" .go)
    local bin="$TMPDIR/$name"
    local pkg
    pkg=$(stage_src "$src")

    if ! (cd "$MOXIEROOT" && "$MOXIE" build -o "$bin" "$pkg") 2>"$TMPDIR/$name.err"; then
        echo "FAIL $name: compilation failed"
        cat "$TMPDIR/$name.err"
        fail=$((fail + 1))
        return
    fi

    local output
    output=$(timeout 30 "$bin" 2>&1 || true)

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
    local pkg
    pkg=$(stage_src "$src")

    local errout
    if errout=$(cd "$MOXIEROOT" && "$MOXIE" build -o "$bin" "$pkg" 2>&1); then
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
run_fail_test "$TESTDIR/fail_cross_block.go"   "after spawn"
run_fail_test "$TESTDIR/fail_type_mismatch.go" "expects"
run_fail_test "$TESTDIR/fail_wrong_count.go"   "expects"

echo ""
echo "=== Results: $pass passed, $fail failed ==="
[ "$fail" -eq 0 ]
