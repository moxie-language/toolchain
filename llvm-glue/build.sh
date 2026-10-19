#!/bin/sh
set -e

# The glue exists for the LEGACY compiler's purego bindings, whose view of the
# LLVM C API is LLVM 22's. Headers and library must be the SAME major: a 22
# library with 23 headers agrees on neither struct layouts nor enum values, and
# the process corrupts its heap (measured: free(): invalid pointer inside
# LLVMDisposeBuilder, with the same source that works when both are 22).
LLVM_VER="${LLVM_VER:-22}"
LLVM_PREFIX=""
INCLUDE_DIR=""
LIB_DIR=""

# The versioned llvm-config knows all three paths; prefer it.
if command -v "llvm-config-$LLVM_VER" >/dev/null 2>&1; then
    LLVM_PREFIX="$("llvm-config-$LLVM_VER" --prefix)"
    INCLUDE_DIR="$("llvm-config-$LLVM_VER" --includedir)"
    LIB_DIR="$("llvm-config-$LLVM_VER" --libdir)"
fi

# LLVM_LIB_PATH pins the library file itself; take its major and dirname.
if [ -n "${LLVM_LIB_PATH:-}" ] && [ -f "$LLVM_LIB_PATH" ]; then
    case "$LLVM_LIB_PATH" in
        *libLLVM-*.so)
            v=$(echo "$LLVM_LIB_PATH" | sed 's/.*libLLVM-\([0-9]*\)\.so/\1/')
            [ -z "$LLVM_VER" ] && LLVM_VER="$v"
            [ -z "$LIB_DIR" ] && LIB_DIR="$(dirname "$LLVM_LIB_PATH")"
            ;;
    esac
fi

if [ -z "$LLVM_PREFIX" ]; then
    for p in "/usr/lib/llvm-${LLVM_VER}" "/usr/lib/llvm${LLVM_VER}" "/usr/lib/llvm/${LLVM_VER}"; do
        if [ -d "$p" ]; then LLVM_PREFIX="$p"; break; fi
    done
fi
if [ -z "$INCLUDE_DIR" ] || [ ! -d "$INCLUDE_DIR/llvm-c" ]; then
    if [ -n "$LLVM_PREFIX" ] && [ -d "$LLVM_PREFIX/include/llvm-c" ]; then
        INCLUDE_DIR="$LLVM_PREFIX/include"
    elif [ -d "/usr/include/llvm-c" ]; then
        INCLUDE_DIR="/usr/include"
    fi
fi
if [ -z "$LIB_DIR" ] || [ ! -f "$LIB_DIR/libLLVM-$LLVM_VER.so" ]; then
    if [ -f "/usr/lib/libLLVM-$LLVM_VER.so" ]; then
        LIB_DIR="/usr/lib"
    elif [ -n "$LLVM_PREFIX" ] && [ -f "$LLVM_PREFIX/lib/libLLVM-$LLVM_VER.so" ]; then
        LIB_DIR="$LLVM_PREFIX/lib"
    fi
fi

if [ -z "$INCLUDE_DIR" ] || [ ! -d "$INCLUDE_DIR/llvm-c" ]; then
    echo "error: LLVM $LLVM_VER headers not found (looked for llvm-c under llvm-config-$LLVM_VER, $LLVM_PREFIX/include, /usr/include)" >&2
    exit 1
fi
if [ -z "$LIB_DIR" ] || [ ! -f "$LIB_DIR/libLLVM-$LLVM_VER.so" ]; then
    echo "error: libLLVM-$LLVM_VER.so not found (looked in llvm-config-$LLVM_VER, /usr/lib, $LLVM_PREFIX/lib)" >&2
    exit 1
fi

VENDOR="$(cd "$(dirname "$0")/../legacy/vendor/tinygo.org/x/go-llvm" && pwd)"
OUT="$(dirname "$0")/libmoxie-llvm-glue.so"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CXX="${CXX:-clang++-$LLVM_VER}"
command -v "$CXX" >/dev/null 2>&1 || CXX="clang++"
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
