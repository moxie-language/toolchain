#!/bin/bash
# Build the Moxie compiler with a patched GOROOT.
# The patched go/types enables string=[]byte unification.
set -e

REAL_GOROOT=$(go env GOROOT)
MOXIE_GOROOT="${MOXIE_GOROOT_CACHE:-/tmp/moxie-goroot}"

# LLVM 22 is pinned across the whole stack: the legacy compiler links
# libLLVM-22, stage4 invokes clang-22/llvm-link-22/ld.lld-22/opt, and the
# top-level build.sh checks for the -22 tools. Do not fall back to another
# major version - mixing majors yields bitcode one tool cannot read.
# Some distros install LLVM 22 as the unversioned toolchain, so accept the
# unversioned llvm-config but verify the major version it reports.
LLVM_VER="22"
LLVM_PREFIX=""
for c in "llvm-config-${LLVM_VER}" llvm-config; do
    if command -v "$c" >/dev/null 2>&1; then
        ver="$("$c" --version 2>/dev/null | cut -d. -f1)"
        if [ "$ver" = "$LLVM_VER" ]; then
            LLVM_PREFIX="$("$c" --prefix)"
            break
        fi
    fi
done
if [ -z "$LLVM_PREFIX" ]; then
    for p in "/usr/lib/llvm-${LLVM_VER}" "/usr/lib/llvm${LLVM_VER}" "/usr/lib/llvm/${LLVM_VER}"; do
        if [ -d "$p" ]; then
            LLVM_PREFIX="$p"
            break
        fi
    done
fi
if [ -z "$LLVM_PREFIX" ]; then
    echo "error: LLVM ${LLVM_VER} not found (need an llvm-config reporting ${LLVM_VER}, or /usr/lib/llvm-${LLVM_VER})" >&2
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
