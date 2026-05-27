#!/bin/sh
set -e

LLVM_PREFIX=""
LLVM_VER=""
for v in 21 19; do
    for p in /usr/lib/llvm-$v /usr/lib/llvm$v /usr/lib/llvm/$v; do
        if [ -d "$p" ]; then
            LLVM_PREFIX="$p"
            LLVM_VER="$v"
            break 2
        fi
    done
done

if [ -z "$LLVM_PREFIX" ]; then
    echo "error: LLVM (21 or 19) not found" >&2
    exit 1
fi

VENDOR="$(cd "$(dirname "$0")/../../vendor/tinygo.org/x/go-llvm" && pwd)"
OUT="$(dirname "$0")/libmoxie-llvm-glue.so"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CXX="${CXX:-clang++-$LLVM_VER}"
command -v "$CXX" >/dev/null 2>&1 || CXX="clang++"
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
