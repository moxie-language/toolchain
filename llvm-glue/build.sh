#!/bin/sh
set -e

LLVM_PREFIX=""
LLVM_VER=""
# Check LLVM_LIB_PATH override first.
if [ -n "$LLVM_LIB_PATH" ] && [ -f "$LLVM_LIB_PATH" ]; then
    case "$LLVM_LIB_PATH" in
        *libLLVM-*.so)
            v=$(echo "$LLVM_LIB_PATH" | sed 's/.*libLLVM-\([0-9]*\)\.so/\1/')
            LLVM_VER="$v"
            LLVM_PREFIX="$(dirname "$(dirname "$LLVM_LIB_PATH")")"
            ;;
    esac
fi
if [ -z "$LLVM_PREFIX" ]; then
    # Prefer llvm-config to determine paths.
    for c in llvm-config llvm-config-22 llvm-config-21 llvm-config-20; do
        if command -v "$c" >/dev/null 2>&1; then
            LLVM_PREFIX="$($c --prefix)"
            LLVM_VER="$($c --version | cut -d. -f1)"
            break
        fi
    done
fi
if [ -z "$LLVM_PREFIX" ]; then
    for v in 22 21 20; do
        for p in /usr/lib/llvm-$v /usr/lib/llvm$v /usr/lib/llvm/$v; do
            if [ -d "$p" ]; then
                LLVM_PREFIX="$p"
                LLVM_VER="$v"
                break 2
            fi
        done
    done
fi

if [ -z "$LLVM_PREFIX" ] || [ -z "$LLVM_VER" ]; then
    echo "error: LLVM not found (tried 22, 21, 20)" >&2
    exit 1
fi

VENDOR="$(cd "$(dirname "$0")/../legacy/vendor/tinygo.org/x/go-llvm" && pwd)"
OUT="$(dirname "$0")/libmoxie-llvm-glue.so"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CXX="${CXX:-clang++-$LLVM_VER}"
command -v "$CXX" >/dev/null 2>&1 || CXX="clang++"
# Determine include dir: prefer prefix/include, fall back to system include.
INCLUDE_DIR="$LLVM_PREFIX/include"
if [ ! -d "$INCLUDE_DIR/llvm-c" ] && [ -d "/usr/include/llvm-c" ]; then
    INCLUDE_DIR="/usr/include"
fi
# Determine lib dir.
LIB_DIR="$LLVM_PREFIX/lib"
if [ ! -f "$LIB_DIR/libLLVM-$LLVM_VER.so" ] && [ -f "/usr/lib/libLLVM-$LLVM_VER.so" ]; then
    LIB_DIR="/usr/lib"
fi
$CXX -shared -fPIC \
    -o "$OUT" \
    "$VENDOR/IRBindings.cpp" \
    "$VENDOR/backports.cpp" \
    "$SCRIPT_DIR/shims.c" \
    -I"$INCLUDE_DIR" \
    -L"$LIB_DIR" -lLLVM-$LLVM_VER \
    -std=c++17

echo "built: $OUT"
nm -D "$OUT" | grep " T " | head -20
