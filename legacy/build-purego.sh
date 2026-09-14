#!/bin/bash
# Build the Moxie compiler with PureGo LLVM bindings.
# No C/C++ toolchain needed at build time.
# Runtime deps: libLLVM-22.so, clang-22, ld.lld-22 (distro packages).
set -e

REAL_GOROOT=$(go env GOROOT)
MOXIE_GOROOT="${MOXIE_GOROOT_CACHE:-/tmp/moxie-goroot}"

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

export GOROOT="$MOXIE_GOROOT"
export GOWORK=off
export GOTOOLCHAIN=local

exec "$MOXIE_GOROOT/bin/go" build -mod=vendor -tags "purego" -o moxie-purego "$@" .
