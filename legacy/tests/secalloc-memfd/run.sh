#!/bin/bash
# Milestone-4 verification: memfd_secret(2) upgrade path.
#
# Builds tests/secalloc-memfd/main.go and runs the binary. The program
# allocates a secure buffer and parses /proc/self/maps to determine whether
# the data pages are backed by memfd_secret (gold standard) or by an
# anonymous mapping (portable fallback).
#
# Expected outcomes:
#   - Linux ≥5.14 with CONFIG_SECRETMEM=y: SECRETMEM_OK on stdout. This is
#     the runner's host environment and the test ASSERTS this outcome.
#   - Linux without CONFIG_SECRETMEM or older kernel: ANONYMOUS_FALLBACK.
#     This is acceptable; the runner treats it as pass IF the kernel
#     actually lacks support (detected via /proc/kallsyms probe) or if the
#     MOXIE_ALLOW_ANONYMOUS_FALLBACK env var is set.
#   - Darwin: ANONYMOUS_FALLBACK. Pass.
set -euo pipefail

MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../../.." && pwd)}"
MOXIE="${MOXIE:-$MOXIEROOT/moxie}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

export PATH="/usr/lib/llvm19/bin:$PATH"
export MOXIEROOT

BIN="$TMPDIR/secalloc_memfd_test"

cd "$MOXIEROOT"

if ! "$MOXIE" build -o "$BIN" ./legacy/tests/secalloc-memfd 2>"$TMPDIR/build.err"; then
    echo "FAIL secalloc-memfd: compilation failed"
    cat "$TMPDIR/build.err"
    exit 1
fi

set +e
"$BIN" >"$TMPDIR/stdout" 2>"$TMPDIR/stderr"
rc=$?
set -e

stdout=$(cat "$TMPDIR/stdout")
stderr=$(cat "$TMPDIR/stderr")

if [ "$rc" -ne 0 ]; then
    echo "FAIL secalloc-memfd: expected clean exit, got rc=$rc"
    echo "--- stdout ---"
    echo "$stdout"
    echo "--- stderr ---"
    echo "$stderr"
    exit 1
fi

# Detect whether the kernel actually supports memfd_secret. Use the same
# probe we use at runtime: look for __x64_sys_memfd_secret (x86_64) or
# __arm64_sys_memfd_secret (arm64) in /proc/kallsyms. Requires CAP_SYSLOG
# or kernel.kptr_restrict=0 to see the symbols; fall back to "assume
# unsupported" if the symbol list is empty.
kernel_supports_memfd_secret=0
if [ "$(uname -s)" = "Linux" ]; then
    if grep -qE 'T __(x64|arm64)_sys_memfd_secret' /proc/kallsyms 2>/dev/null; then
        kernel_supports_memfd_secret=1
    fi
fi

if echo "$stdout" | grep -q "SECRETMEM_OK"; then
    echo "OK   secalloc-memfd (memfd_secret upgrade path active)"
    exit 0
fi

if echo "$stdout" | grep -q "ANONYMOUS_FALLBACK"; then
    if [ "$kernel_supports_memfd_secret" -eq 0 ] || [ "${MOXIE_ALLOW_ANONYMOUS_FALLBACK:-}" = "1" ]; then
        echo "OK   secalloc-memfd (anonymous fallback; kernel lacks memfd_secret)"
        exit 0
    fi
    echo "FAIL secalloc-memfd: kernel supports memfd_secret but arena fell back to anonymous"
    echo "--- stdout ---"
    echo "$stdout"
    echo "--- stderr ---"
    echo "$stderr"
    exit 1
fi

echo "FAIL secalloc-memfd: neither SECRETMEM_OK nor ANONYMOUS_FALLBACK found"
echo "--- stdout ---"
echo "$stdout"
echo "--- stderr ---"
echo "$stderr"
exit 1
