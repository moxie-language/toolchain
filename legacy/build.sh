#!/bin/bash
# Build the Moxie compiler with a patched GOROOT.
# The patched go/types enables string=[]byte unification.
set -e

REAL_GOROOT=$(go env GOROOT)
MOXIE_GOROOT="${MOXIE_GOROOT_CACHE:-/tmp/moxie-goroot}"

LLVM_PREFIX=""
LLVM_VER=""
# Prefer llvm-config to determine paths.
for c in llvm-config llvm-config-22 llvm-config-21 llvm-config-20; do
    if command -v "$c" >/dev/null 2>&1; then
        LLVM_PREFIX="$($c --prefix)"
        LLVM_VER="$($c --version | cut -d. -f1)"
        break
    fi
done
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
# Determine LLVM library path.
if [ -f "${LLVM_PREFIX}/lib/libLLVM-${LLVM_VER}.so" ]; then
    export LLVM_LIB_PATH="${LLVM_PREFIX}/lib/libLLVM-${LLVM_VER}.so"
elif [ -f "/usr/lib/libLLVM-${LLVM_VER}.so" ]; then
    export LLVM_LIB_PATH="/usr/lib/libLLVM-${LLVM_VER}.so"
else
    export LLVM_LIB_PATH="${LLVM_PREFIX}/lib/libLLVM-${LLVM_VER}.so"
fi

exec "$MOXIE_GOROOT/bin/go" build -mod=vendor -tags purego -gcflags="-N -l" -o moxie "$@" .
