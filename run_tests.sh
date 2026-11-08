#!/bin/bash
# Run stdlib tests with the tree compiler.
set -euo pipefail

# Pin MOXIEROOT to this checkout. The compiler reads the stdlib from
# $MOXIEROOT/src, and an inherited MOXIEROOT (a stale installed tree, e.g.
# ~/.local/share/moxie) silently compiles THAT source instead - which showed up
# as seven bogus "stdlib test failures" reporting a shadowing violation that
# the current tree had already fixed. tests/run.sh pins it the same way.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MOXIEROOT="$SCRIPT_DIR"
export MOXIEROOT

MOXIE="${MOXIE:-./moxie}"
PASSED=0
FAILED=0
ERRORS=""

if [ ! -x "$MOXIE" ]; then
    echo "FATAL: $MOXIE not found or not executable"
    exit 1
fi

echo "=== Running stdlib tests with: $MOXIE ==="
echo "    $($MOXIE version 2>&1)"
echo

# Find all test packages
TEST_FILES=$(find ./src -name "*_test.mx" -not -path "./legacy/*")

# Group by directory
DIRS=$(echo "$TEST_FILES" | xargs -n1 dirname | sort -u)

# Each package is compiled by `moxie test`, the compiler's own harness. It
# builds the test main itself, which is what keeps a package's init() legal:
# this script used to copy each package into a scratch directory, rename it to
# `package main` and generate a main of its own, and that copy cost the package
# its init() ("init() is not allowed in main package") and its global stores
# ("package globals are immutable outside init") - nine packages failed to build
# for that reason and two more for a missing shutdown arm in the generated main.
# The harness compiles the package in place, so neither rewrite happens.
for dir in $DIRS; do
    # Skip a directory whose test files declare no test function: the harness
    # would build it and run nothing.
    testfuncs=$(grep -h "^func Test" "$dir"/*_test.mx 2>/dev/null | sed 's/func \(Test[^ (]*\).*/\1/')
    if [ -z "$testfuncs" ]; then
        continue
    fi

    shortname=$(echo "$dir" | sed 's|^\./src/||')
    printf "%-50s" "TEST $shortname"

    if output=$(MOXIEROOT="$MOXIEROOT" "$MOXIE" test "$dir" 2>&1); then
        echo "PASS"
        PASSED=$((PASSED + 1))
    else
        if echo "$output" | grep -q "compile error\|clang failed\|linker failed"; then
            echo "FAIL (build)"
        else
            echo "FAIL (run)"
        fi
        echo "$output" | tail -10
        ERRORS="$ERRORS  FAIL: $shortname\n"
        FAILED=$((FAILED + 1))
    fi
done

echo
echo "=== Results ==="
echo "PASSED: $PASSED"
echo "FAILED: $FAILED"
if [ -n "$ERRORS" ]; then
    echo
    echo "Failures:"
    printf "$ERRORS"
fi

if [ "$FAILED" -gt 0 ]; then
    exit 1
fi
