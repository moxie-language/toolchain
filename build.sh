#!/bin/bash
set -e

FAIL=""
command -v llvm-extract-22 >/dev/null 2>&1 || command -v llvm-extract-21 >/dev/null 2>&1 || FAIL="$FAIL llvm-extract-21"
command -v ld.lld-22 >/dev/null 2>&1 || command -v ld.lld-21 >/dev/null 2>&1 || FAIL="$FAIL ld.lld-21"
command -v clang-22 >/dev/null 2>&1 || command -v clang-21 >/dev/null 2>&1 || FAIL="$FAIL clang-21"
if [ -n "$FAIL" ]; then
    echo "error: LLVM 21 required. missing:$FAIL" >&2
    echo "  arch:   pacman -S llvm21 clang21 lld21" >&2
    echo "  debian: apt install llvm-21 clang-21 lld-21" >&2
    exit 1
fi

MOXIEROOT="$(pwd)"
export MOXIEROOT
export LLVM_GLUE_PATH="$MOXIEROOT/llvm-glue/libmoxie-llvm-glue.so"
export PATH="/usr/local/bin:$PATH"

# build llvm glue if missing
if [ ! -f "$LLVM_GLUE_PATH" ]; then
    echo "Building llvm-glue ..."
    bash "$MOXIEROOT/llvm-glue/build.sh"
fi

# find a moxie binary to bootstrap with
MOXIE="${MOXIE:-}"
if [ -z "$MOXIE" ]; then
    for m in "$MOXIEROOT/moxie-new" "$MOXIEROOT/moxie" "$MOXIEROOT/legacy/moxie" "$(command -v moxie 2>/dev/null || true)"; do
        if [ -x "$m" ]; then
            MOXIE="$m"
            break
        fi
    done
fi
if [ -z "$MOXIE" ]; then
    echo "error: no moxie binary found" >&2
    echo "  place a bootstrap binary at ./moxie-new, ./moxie, or ./legacy/moxie" >&2
    exit 1
fi

MOXIE="$(cd "$(dirname "$MOXIE")" && pwd)/$(basename "$MOXIE")"
echo "=== Building moxie (self-hosting) ==="
echo "Bootstrap: $MOXIE"

# compiler source directory
COMPILER_SRC="$MOXIEROOT/_mxc_stage4"
if [ ! -d "$COMPILER_SRC" ] || [ ! -f "$COMPILER_SRC/main.mx" ]; then
    echo "error: compiler source not found in _mxc_stage4/" >&2
    exit 1
fi
echo "Source: $COMPILER_SRC"

$MOXIE build -internal-nodwarf -o "$MOXIEROOT/moxie-new" "$COMPILER_SRC/"

# verify the new binary works
"$MOXIEROOT/moxie-new" version

mv "$MOXIEROOT/moxie-new" "$MOXIEROOT/moxie"

# rebuild all cached packages with the new compiler and remove old version caches
"$MOXIEROOT/moxie" update --all

# run regression validation
echo "=== Regression Check ==="
if ! bash "$MOXIEROOT/tests/regressions/run.sh"; then
    echo "REGRESSION DETECTED: bootstrap build suspect" >&2
    exit 1
fi

echo "=== Done: ./moxie ==="
