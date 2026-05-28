#!/bin/bash
# Build the Moxie compiler with a patched GOROOT.
# The patched go/types enables string=[]byte unification.
set -e

REAL_GOROOT=$(go env GOROOT)
MOXIE_GOROOT="${MOXIE_GOROOT_CACHE:-/tmp/moxie-goroot}"

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

# Create or update patched GOROOT.
# Only recreate if the Go version changed or patches were modified.
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

    # Symlink top-level items.
    for item in "$REAL_GOROOT"/*; do
        ln -sf "$item" "$MOXIE_GOROOT/$(basename "$item")"
    done

    # Replace src → symlink tree with writable go/types.
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
    # go/types gets copied (not symlinked) so we can patch it.
    rm -f "$MOXIE_GOROOT/src/go/types"

    GOROOT="$REAL_GOROOT" go run build/patch-gotypes.go "$MOXIE_GOROOT"
    echo "$WANT_STAMP" > "$STAMP"
fi

# Build.
export GOROOT="$MOXIE_GOROOT"
export GOWORK=off
export GOTOOLCHAIN=local
export CGO_ENABLED=0
export LLVM_LIB_PATH="${LLVM_PREFIX}/lib/libLLVM-${LLVM_VER}.so"

exec "$MOXIE_GOROOT/bin/go" build -mod=vendor -tags purego -o moxie "$@" .
