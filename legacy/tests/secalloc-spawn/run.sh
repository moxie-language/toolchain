#!/bin/bash
# Milestone-2 verification: spawn-inherited lockdown channel.
#
# Builds tests/secalloc-spawn/main.go and runs the binary as the parent.
# The parent forks a child, hands the child a pipe write fd via
# SetSecureLockdownFd, and the child triggers a guard-page fault. The
# signal handler must route the lockdown notification through the
# inherited pipe, NOT to stderr. The parent reads the byte and prints
# LOCKDOWN_RECEIVED if the cross-process notification arrived.
#
# Verifies:
#   - parent exit status is 0 (parent did not crash)
#   - parent stdout contains LOCKDOWN_RECEIVED
#   - parent stdout contains CHILD_EXITED_SIGSEGV
#   - the raw secret pattern does NOT appear anywhere in stdout/stderr
#   - parent stderr does NOT contain MOXIE_SECALLOC_LOCKDOWN
#     (the marker should travel through the pipe, not via stderr)
set -euo pipefail

MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../../.." && pwd)}"
MOXIE="${MOXIE:-$MOXIEROOT/moxie}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"
export MOXIEROOT

BIN="$TMPDIR/secalloc_spawn_test"

cd "$MOXIEROOT"

if ! "$MOXIE" build -o "$BIN" ./legacy/tests/secalloc-spawn 2>"$TMPDIR/build.err"; then
    echo "FAIL secalloc-spawn: compilation failed"
    cat "$TMPDIR/build.err"
    exit 1
fi

set +e
bash -c "\"$BIN\" >\"$TMPDIR/stdout\" 2>\"$TMPDIR/stderr\"" 2>/dev/null
rc=$?
set -e

stdout=$(cat "$TMPDIR/stdout")
stderr=$(cat "$TMPDIR/stderr")

fail=0

if [ "$rc" -ne 0 ]; then
    echo "FAIL secalloc-spawn: parent exited non-zero (got $rc)"
    fail=1
fi

if ! echo "$stdout" | grep -q "LOCKDOWN_RECEIVED"; then
    echo "FAIL secalloc-spawn: lockdown marker did not arrive via inherited pipe"
    fail=1
fi

if ! echo "$stdout" | grep -q "CHILD_EXITED_SIGSEGV"; then
    echo "FAIL secalloc-spawn: child did not exit on SIGSEGV"
    fail=1
fi

if echo "$stdout$stderr" | grep -q "MOXIE_SECRET_PAYLOAD"; then
    echo "FAIL secalloc-spawn: raw secret pattern leaked"
    fail=1
fi

# stderr must NOT contain the lockdown marker — it should have been
# routed through the pipe instead. If it shows up on stderr, the fd
# override didn't take effect.
if echo "$stderr" | grep -q "MOXIE_SECALLOC_LOCKDOWN"; then
    echo "FAIL secalloc-spawn: marker leaked to stderr instead of pipe"
    fail=1
fi

if [ "$fail" -eq 0 ]; then
    echo "OK   secalloc-spawn (lockdown routed via inherited pipe)"
    exit 0
fi

echo "--- stdout ---"
echo "$stdout"
echo "--- stderr ---"
echo "$stderr"
exit 1
