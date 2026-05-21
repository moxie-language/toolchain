#!/bin/bash
# Build the Moxie compiler with PureGo LLVM bindings (no CGo LLVM dependency).
# Still needs CGo for libclang (cgo package).
set -e

REAL_GOROOT=$(go env GOROOT)
MOXIE_GOROOT="${MOXIE_GOROOT_CACHE:-/tmp/moxie-goroot}"

# Create or update patched GOROOT (same as build.sh).
STAMP="$MOXIE_GOROOT/.stamp"
GO_VER=$(go version)
PATCH_HASH=$(sha256sum build/patch-gotypes.go | cut -d' ' -f1)
WANT_STAMP="$GO_VER $PATCH_HASH"

if [ -f "$STAMP" ] && [ "$(cat "$STAMP")" = "$WANT_STAMP" ]; then
    echo "patched GOROOT up to date" >&2
else
    echo "creating patched GOROOT at $MOXIE_GOROOT ..." >&2
    rm -rf "$MOXIE_GOROOT"
    mkdir -p "$MOXIE_GOROOT"
    for item in "$REAL_GOROOT"/*; do
        ln -sf "$item" "$MOXIE_GOROOT/$(basename "$item")"
    done
    rm -f "$MOXIE_GOROOT/src"
    mkdir -p "$MOXIE_GOROOT/src"
    for item in "$REAL_GOROOT/src"/*; do
        ln -sf "$item" "$MOXIE_GOROOT/src/$(basename "$item")"
    done
    rm -f "$MOXIE_GOROOT/src/go"
    mkdir -p "$MOXIE_GOROOT/src/go"
    for item in "$REAL_GOROOT/src/go"/*; do
        ln -sf "$item" "$MOXIE_GOROOT/src/go/$(basename "$item")"
    done
    rm -f "$MOXIE_GOROOT/src/go/types"
    GOROOT="$REAL_GOROOT" go run build/patch-gotypes.go "$MOXIE_GOROOT"
    echo "$WANT_STAMP" > "$STAMP"
fi

# Ensure abi_amd64.h exists for purego's fakecgo assembly.
if [ ! -f "$MOXIE_GOROOT/pkg/include/abi_amd64.h" ]; then
    cp "$REAL_GOROOT/src/runtime/cgo/abi_amd64.h" "$MOXIE_GOROOT/pkg/include/" 2>/dev/null || true
fi

# Detect LLVM 19 for libclang (still needed for the cgo package).
LLVM_PREFIX="/usr/lib/llvm19"
if [ ! -d "$LLVM_PREFIX" ]; then
    LLVM_PREFIX="/usr/lib/llvm-19"
fi
if [ ! -d "$LLVM_PREFIX" ]; then
    echo "error: LLVM 19 not found (needed for libclang)" >&2
    exit 1
fi

export GOROOT="$MOXIE_GOROOT"
export GOWORK=off
export CGO_CFLAGS="-I${LLVM_PREFIX}/include"
export CGO_LDFLAGS="-L${LLVM_PREFIX}/lib -lclang"

exec go build -mod=vendor -tags "purego" -o moxie-purego "$@" .
