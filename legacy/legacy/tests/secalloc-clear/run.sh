#!/bin/bash
# Milestone-3.2b verification: SecureClear + SecureLockdown + SecureRekey.
#
# Builds tests/secalloc-clear/main.go and runs the binary. Unlike the
# rotate test, this program must exit CLEANLY — manual lockdown returns
# to the caller instead of delegating to the fatal-signal teardown.
#
# Verifies:
#   - the program exits 0
#   - stdout contains the full marker sequence:
#       BEFORE_CLEAR → CLEARED_OK → LOCKDOWN_1_OK → LOCKDOWN_2_OK →
#       REKEY_OK → EXPLICIT_REKEY_OK → LOCKDOWN_SURVIVED
#   - stderr contains at least 3 MOXIE_SECALLOC_LOCKDOWN markers
#     (one per SecureLockdown call)
#   - the raw secret pattern does NOT appear anywhere
set -euo pipefail

MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
MOXIE="${MOXIE:-$MOXIEROOT/moxie}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"
export MOXIEROOT

BIN="$TMPDIR/secalloc_clear_test"

cd "$MOXIEROOT"

if ! "$MOXIE" build -o "$BIN" ./tests/secalloc-clear 2>"$TMPDIR/build.err"; then
    echo "FAIL secalloc-clear: compilation failed"
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

if [ "$rc" -ne 0 ]; then
    echo "FAIL secalloc-clear: expected clean exit, got rc=$rc"
    fail=1
fi

for marker in BEFORE_CLEAR CLEARED_OK LOCKDOWN_1_OK LOCKDOWN_2_OK REKEY_OK EXPLICIT_REKEY_OK LOCKDOWN_SURVIVED; do
    if ! echo "$stdout" | grep -q "$marker"; then
        echo "FAIL secalloc-clear: missing $marker marker on stdout"
        fail=1
    fi
done

if ! echo "$stderr" | grep -q "MOXIE_SECALLOC_LOCKDOWN"; then
    echo "FAIL secalloc-clear: missing lockdown marker on stderr"
    fail=1
fi

lockdown_count=$(echo "$stderr" | grep -c "MOXIE_SECALLOC_LOCKDOWN" || true)
if [ "$lockdown_count" -lt 3 ]; then
    echo "FAIL secalloc-clear: expected at least 3 lockdown markers, got $lockdown_count"
    fail=1
fi

if echo "$stdout$stderr" | grep -q "MOXIE_SECRET_PAYLOAD"; then
    echo "FAIL secalloc-clear: raw secret pattern leaked"
    fail=1
fi

if [ "$fail" -eq 0 ]; then
    echo "OK   secalloc-clear (manual clear + lockdown + rekey)"
    exit 0
fi

echo "--- stdout ---"
echo "$stdout"
echo "--- stderr ---"
echo "$stderr"
exit 1
