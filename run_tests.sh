#!/bin/bash
# Run stdlib tests using the stage-2 (moxie-compiled) compiler
set -euo pipefail

MOXIE="${MOXIE:-./moxie}"
TMPBASE="${TMPBASE:-/home/mleku/tmp/mxtest}"
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

rm -rf "$TMPBASE"
mkdir -p "$TMPBASE"

# Find all test packages
TEST_FILES=$(find ./src -name "*_test.mx" -not -path "./legacy/*")

# Group by directory
DIRS=$(echo "$TEST_FILES" | xargs -n1 dirname | sort -u)

for dir in $DIRS; do
    pkgname=$(head -5 "$dir"/*_test.mx 2>/dev/null | grep "^package " | head -1 | awk '{print $2}')
    if [ -z "$pkgname" ]; then
        continue
    fi

    # Collect test functions
    testfuncs=$(grep -h "^func Test" "$dir"/*_test.mx 2>/dev/null | sed 's/func \(Test[^ (]*\).*/\1/')
    if [ -z "$testfuncs" ]; then
        continue
    fi

    testdir="$TMPBASE/$(echo "$dir" | tr '/' '_')"
    mkdir -p "$testdir"

    # Copy all .mx files from the package dir (including _test.mx)
    for f in "$dir"/*.mx; do
        [ -f "$f" ] || continue
        base=$(basename "$f")
        # Rename _test.mx to _testdata.mx because the compiler skips *_test.*
        outname=$(echo "$base" | sed 's/_test\.mx$/_testdata.mx/')
        # Rewrite package declaration to main
        sed "s/^package $pkgname$/package main/" "$f" > "$testdir/$outname"
    done

    # Generate test main
    {
        echo "package main"
        echo ""
        echo "import \"testing\""
        echo ""
        echo "func main() {"
        echo "    tests := []testing.InternalTest{"
        for fn in $testfuncs; do
            echo "        {\"$fn\", $fn},"
        done
        echo "    }"
        echo "    testing.Main(tests)"
        echo "}"
    } > "$testdir/testmain_gen.mx"

    # Build
    shortname=$(echo "$dir" | sed 's|^\./src/||')
    printf "%-50s" "TEST $shortname"

    if $MOXIE build -o "$testdir/test.bin" "$testdir" >"$testdir/build.log" 2>&1; then
        # Ensure binary is executable
        chmod +x "$testdir/test.bin"
        # Run
        if output=$("$testdir/test.bin" 2>&1); then
            echo "PASS"
            PASSED=$((PASSED + 1))
        else
            echo "FAIL (runtime)"
            echo "$output" | tail -10
            ERRORS="$ERRORS  FAIL(run): $shortname\n"
            FAILED=$((FAILED + 1))
        fi
    else
        echo "FAIL (build)"
        tail -5 "$testdir/build.log"
        ERRORS="$ERRORS  FAIL(build): $shortname\n"
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
