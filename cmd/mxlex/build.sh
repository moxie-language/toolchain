#!/bin/bash
set -e
cd "$(dirname "$0")"
ROOT="${MOXIEROOT:-../..}"
OUT="${1:-/tmp/mxlex}"

# Try new moxie first
if command -v moxie >/dev/null 2>&1; then
    echo "trying: moxie (new)" >&2
    if MOXIEROOT="$ROOT" moxie build -o "$OUT" . 2>&1; then
        echo "built with new moxie: $OUT" >&2
        exit 0
    fi
    echo "new moxie failed, falling back to legacy" >&2
fi

# Fall back to legacy
LEGACY="$ROOT/legacy/moxie"
if [ ! -x "$LEGACY" ]; then
    echo "error: no legacy compiler at $LEGACY" >&2
    exit 1
fi
echo "trying: legacy moxie" >&2
MOXIEROOT="$ROOT" "$LEGACY" build -o "$OUT" . 2>&1
echo "built with legacy: $OUT" >&2
