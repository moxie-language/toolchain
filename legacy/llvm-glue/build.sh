#!/bin/sh
set -e

LLVM_PREFIX=""
LLVM_VER="22"

# Arch Linux style
if [ -f "/usr/lib/libLLVM-22.so" ]; then
    LLVM_PREFIX="/usr"
fi

# Debian/Ubuntu style
if [ -z "$LLVM_PREFIX" ]; then
    for p in /usr/lib/llvm-22 /usr/lib/llvm22 /usr/lib/llvm/22; do
        if [ -d "$p" ]; then
            LLVM_PREFIX="$p"
            break
        fi
    done
fi

if [ -z "$LLVM_PREFIX" ]; then
    echo "error: LLVM 22 not found" >&2
    exit 1
fi

echo "using LLVM $LLVM_VER at $LLVM_PREFIX"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VENDOR="$(cd "$SCRIPT_DIR/../vendor/tinygo.org/x/go-llvm" && pwd)"
OUT="$SCRIPT_DIR/libmoxie-llvm-glue.so"

CXX="${CXX:-clang++}"
if command -v "clang++-$LLVM_VER" >/dev/null 2>&1; then
    CXX="clang++-$LLVM_VER"
fi

$CXX -shared -fPIC \
    -o "$OUT" \
    "$VENDOR/IRBindings.cpp" \
    "$VENDOR/backports.cpp" \
    "$SCRIPT_DIR/shims.c" \
    -I"$LLVM_PREFIX/include" \
    -L"$LLVM_PREFIX/lib" -lLLVM-$LLVM_VER \
    -std=c++17

echo "built: $OUT"
nm -D "$OUT" | grep " T " | head -20
