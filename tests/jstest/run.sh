#!/bin/bash
# moxiejs regression test suite.
# Compiles main.go via moxiejs and runs the output under Node, checking
# for PASS: markers and absence of FAIL: markers.
set -euo pipefail

MOXIEJS="${MOXIEJS:-moxiejs}"
MOXIEROOT="${MOXIEROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
TESTDIR="$(cd "$(dirname "$0")" && pwd)"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# Stage source into a package directory the resolver can find.
STAGE="$MOXIEROOT/tests/_jstest_stage_$$"
mkdir -p "$STAGE"
cp "$TESTDIR/main.go" "$STAGE/main.go"
trap "rm -rf $TMPDIR $STAGE" EXIT

OUT="$TMPDIR/out"
mkdir -p "$OUT"

# Compile.
if ! (cd "$MOXIEROOT" && GOWORK=off "$MOXIEJS" -runtime "$MOXIEROOT/jsruntime" -o "$OUT" "./tests/_jstest_stage_$$") 2>"$TMPDIR/compile.err"; then
    echo "FAIL: compilation failed"
    cat "$TMPDIR/compile.err"
    exit 1
fi

# Entry wrapper for Node. The runtime index.mjs re-exports sw.mjs, which
# registers SW event listeners at load time — polyfill the SW globals so
# the module evaluates without throwing.
cat > "$TMPDIR/entry.mjs" <<EOF
const g = globalThis;
if (typeof g.self === 'undefined') g.self = g;
if (typeof g.self.addEventListener !== 'function') { g.self.addEventListener = () => {}; g.self.removeEventListener = () => {}; }
if (typeof g.self.skipWaiting !== 'function') g.self.skipWaiting = () => Promise.resolve();
if (!g.self.clients) g.self.clients = { claim: () => Promise.resolve(), matchAll: () => Promise.resolve([]), get: () => Promise.resolve(null) };
if (!g.self.registration) g.self.registration = { scope: 'node://jstest', active: null, waiting: null, installing: null, update: () => Promise.resolve(), unregister: () => Promise.resolve(true), showNotification: () => Promise.resolve() };
if (!g.self.location) g.self.location = { origin: 'node://jstest', href: 'node://jstest/' };
if (typeof g.caches === 'undefined') g.caches = { open: () => Promise.resolve({ match: () => Promise.resolve(undefined), put: () => Promise.resolve(), delete: () => Promise.resolve(true), keys: () => Promise.resolve([]) }), match: () => Promise.resolve(undefined), has: () => Promise.resolve(false), delete: () => Promise.resolve(true), keys: () => Promise.resolve([]) };
process.on('unhandledRejection', (reason) => {
  process.stderr.write('[jstest] unhandled: ' + (reason && reason.stack || reason) + '\n');
  process.exit(1);
});
await import('$OUT/\$entry.mjs');
EOF

# Run.
output=$(node "$TMPDIR/entry.mjs" 2>&1 || true)
echo "$output"

if echo "$output" | grep -q "FAIL:"; then
    echo "=== jstest: FAILED ==="
    exit 1
fi

pass_count=$(echo "$output" | grep -c "PASS:" || true)
if [ "$pass_count" -lt 3 ]; then
    echo "=== jstest: only $pass_count of 3 PASS markers ==="
    exit 1
fi

echo "=== jstest: $pass_count/3 passed ==="
