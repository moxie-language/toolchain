#!/bin/bash
# Milestone-1 verification for the secure allocator.
#
# Builds tests/secalloc/main.go, runs the binary, and verifies:
#   - the process was killed by SIGSEGV (not a clean exit)
#   - "WROTE_SECRET" appeared on stdout (so the write-the-pattern step ran)
#   - "MOXIE_SECALLOC_LOCKDOWN" appeared on stderr (the signal-handler notify ran)
#   - "UNREACHABLE" did NOT appear (the faulting load did not resume)
#   - the raw secret pattern does NOT appear anywhere in stdout/stderr
set -euo pipefail

MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../../.." && pwd)}"
MOXIE="${MOXIE:-$MOXIEROOT/moxie}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"
export MOXIEROOT

BIN="$TMPDIR/secalloc_test"

# moxie's package resolver requires the source path to be a relative
# `./...` path from the current directory. Build from MOXIEROOT.
cd "$MOXIEROOT"

if ! "$MOXIE" build -o "$BIN" ./legacy/tests/secalloc 2>"$TMPDIR/build.err"; then
    echo "FAIL secalloc: compilation failed"
    cat "$TMPDIR/build.err"
    exit 1
fi

# Run the test binary, capturing stdout, stderr, and exit status separately.
# bash prints "Segmentation fault" to its own stderr when a child dies on
# a signal; we wrap the run in a child bash so we can swallow that one
# line via the wrapper's own stderr redirection. Don't let bash -e trip
# on the expected non-zero exit.
set +e
bash -c "\"$BIN\" >\"$TMPDIR/stdout\" 2>\"$TMPDIR/stderr\"" 2>/dev/null
rc=$?
set -e

stdout=$(cat "$TMPDIR/stdout")
stderr=$(cat "$TMPDIR/stderr")

fail=0

# 139 = 128 + SIGSEGV(11). Bash reports signalled deaths via 128+sig.
if [ "$rc" -ne 139 ]; then
    echo "FAIL secalloc: expected exit 139 (SIGSEGV), got $rc"
    fail=1
fi

if ! echo "$stdout" | grep -q "WROTE_SECRET"; then
    echo "FAIL secalloc: expected WROTE_SECRET on stdout"
    fail=1
fi

if echo "$stdout" | grep -q "UNREACHABLE"; then
    echo "FAIL secalloc: UNREACHABLE appeared on stdout (guard page did not fire)"
    fail=1
fi

if ! echo "$stderr" | grep -q "MOXIE_SECALLOC_LOCKDOWN"; then
    echo "FAIL secalloc: expected MOXIE_SECALLOC_LOCKDOWN on stderr"
    fail=1
fi

# The raw secret must never leak to either stream.
if echo "$stdout$stderr" | grep -q "MOXIE_SECRET_PAYLOAD"; then
    echo "FAIL secalloc: raw secret pattern leaked into stdout/stderr"
    fail=1
fi

if [ "$fail" -eq 0 ]; then
    echo "OK   secalloc (SIGSEGV + lockdown notify, secret sealed)"
    exit 0
fi

echo "--- stdout ---"
echo "$stdout"
echo "--- stderr ---"
echo "$stderr"
exit 1
