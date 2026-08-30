#!/bin/bash
# Milestone-3.2 verification: SecureRotate primitive.
#
# Builds tests/secalloc-rotate/main.go and runs the binary. The program
# allocates a secret, rotates it to a fresh mapping, verifies contents
# survived, then reads the old (now-unmapped) base to trigger a SIGSEGV.
#
# Verifies:
#   - the program dies from SIGSEGV (not a clean exit)
#   - stdout contains BEFORE_ROTATE and ROTATED_OK
#   - stderr contains MOXIE_SECALLOC_LOCKDOWN (wipe+notify still works
#     after rotation)
#   - the raw secret pattern does NOT appear anywhere
set -euo pipefail

MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../../.." && pwd)}"
MOXIE="${MOXIE:-$MOXIEROOT/moxie}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"
export MOXIEROOT

BIN="$TMPDIR/secalloc_rotate_test"

cd "$MOXIEROOT"

if ! "$MOXIE" build -o "$BIN" ./legacy/tests/secalloc-rotate 2>"$TMPDIR/build.err"; then
    echo "FAIL secalloc-rotate: compilation failed"
    cat "$TMPDIR/build.err"
    exit 1
fi

set +e
"$BIN" >"$TMPDIR/stdout" 2>"$TMPDIR/stderr"
rc=$?
set -e

stdout=$(cat "$TMPDIR/stdout")
stderr=$(cat "$TMPDIR/stderr")

fail=0

# 139 = 128 + 11 (SIGSEGV). Any signalled death (128+) is acceptable —
# some shells deliver the signal via a wrapper.
if [ "$rc" -lt 128 ]; then
    echo "FAIL secalloc-rotate: expected signalled death, got rc=$rc"
    fail=1
fi

if ! echo "$stdout" | grep -q "BEFORE_ROTATE"; then
    echo "FAIL secalloc-rotate: missing BEFORE_ROTATE marker"
    fail=1
fi

if ! echo "$stdout" | grep -q "ROTATED_OK"; then
    echo "FAIL secalloc-rotate: missing ROTATED_OK marker"
    fail=1
fi

if ! echo "$stdout" | grep -q "MULTI_ROTATED_OK"; then
    echo "FAIL secalloc-rotate: multi-rotate did not complete (slot reuse?)"
    fail=1
fi

if echo "$stdout" | grep -q "UNREACHABLE"; then
    echo "FAIL secalloc-rotate: reached UNREACHABLE (read of unmapped base did not fault)"
    fail=1
fi

if ! echo "$stderr" | grep -q "MOXIE_SECALLOC_LOCKDOWN"; then
    echo "FAIL secalloc-rotate: missing lockdown marker on stderr"
    fail=1
fi

if echo "$stdout$stderr" | grep -q "MOXIE_SECRET_PAYLOAD"; then
    echo "FAIL secalloc-rotate: raw secret pattern leaked"
    fail=1
fi

if [ "$fail" -eq 0 ]; then
    echo "OK   secalloc-rotate (fresh mapping, old arena unmapped)"
    exit 0
fi

echo "--- stdout ---"
echo "$stdout"
echo "--- stderr ---"
echo "$stderr"
exit 1
