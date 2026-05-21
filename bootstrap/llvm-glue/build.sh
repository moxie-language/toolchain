#!/bin/sh
set -e

LLVM_PREFIX=""
for p in /usr/lib/llvm-19 /usr/lib/llvm19 /usr/lib/llvm/19; do
    if [ -d "$p" ]; then
        LLVM_PREFIX="$p"
        break
    fi
done

if [ -z "$LLVM_PREFIX" ]; then
    echo "error: LLVM 19 not found" >&2
    exit 1
fi

VENDOR="$(cd "$(dirname "$0")/../../vendor/tinygo.org/x/go-llvm" && pwd)"
OUT="$(dirname "$0")/libmoxie-llvm-glue.so"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

clang++ -shared -fPIC \
    -o "$OUT" \
    "$VENDOR/IRBindings.cpp" \
    "$VENDOR/backports.cpp" \
    "$SCRIPT_DIR/shims.c" \
    -I"$LLVM_PREFIX/include" \
    -L/usr/lib -lLLVM-19 \
    -std=c++17

echo "built: $OUT"
nm -D "$OUT" | grep " T " | head -20
