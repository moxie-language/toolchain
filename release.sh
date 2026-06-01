#!/bin/bash
set -e

VERSION=$(git describe --tags --abbrev=0)
if [ -z "$VERSION" ]; then
    echo "error: no git tag found" >&2
    exit 1
fi

if ! git diff --quiet HEAD --; then
    echo "warning: working tree is dirty" >&2
fi

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) echo "error: unsupported arch $ARCH" >&2; exit 1 ;;
esac

OS=linux
NAME="moxie-${VERSION}-${OS}-${ARCH}"
STAGE="/tmp/${NAME}"
TARBALL="/tmp/${NAME}.tar.gz"

echo "building release $NAME ..." >&2

# build fixpoint binary
./build.sh

# verify fixpoint: compiling the source with the binary produces the same binary
echo "verifying fixpoint ..." >&2
MOXIEROOT="$(pwd)"
export MOXIEROOT
export LLVM_GLUE_PATH="$MOXIEROOT/llvm-glue/libmoxie-llvm-glue.so"
rm -rf ~/.cache/moxie/pkg
./moxie build -o moxie-verify ./_mxc_stage4/
if ! cmp -s moxie moxie-verify; then
    echo "error: fixpoint verification failed - binary not reproducible" >&2
    echo "  run another stage: MOXIE=./moxie ./build.sh" >&2
    rm -f moxie-verify
    exit 1
fi
rm -f moxie-verify
echo "fixpoint verified" >&2

rm -rf "$STAGE"
mkdir -p "$STAGE/bin"
mkdir -p "$STAGE/compiler"

cp moxie "$STAGE/bin/moxie"
strip "$STAGE/bin/moxie" 2>/dev/null || true

# sysroot - precompiled runtime, stdlib, libs
cp -a sysroot "$STAGE/sysroot"

# stdlib source
cp -a src "$STAGE/src"

# jsruntime - WASM browser shims
cp -a jsruntime "$STAGE/jsruntime"

# llvm glue
mkdir -p "$STAGE/llvm-glue"
cp llvm-glue/libmoxie-llvm-glue.so "$STAGE/llvm-glue/"
cp llvm-glue/build.sh "$STAGE/llvm-glue/"
cp llvm-glue/*.cpp "$STAGE/llvm-glue/" 2>/dev/null || true

# compiler source - for rebuilding from source
cp _mxc_stage4/*.mx "$STAGE/compiler/"
cp _mxc_stage4/host.c "$STAGE/compiler/"
cp _mxc_stage4/host.bc "$STAGE/compiler/"

# build script
cp build.sh "$STAGE/"

# SHA256 of the fixpoint binary for verification
sha256sum moxie | cut -d' ' -f1 > "$STAGE/SHA256"

echo "creating tarball ..." >&2
tar czf "$TARBALL" -C /tmp "$NAME"

SIZE=$(du -h "$TARBALL" | cut -f1)
SHA=$(sha256sum "$TARBALL" | cut -d' ' -f1)
echo "tarball: $TARBALL ($SIZE)" >&2
echo "sha256:  $SHA" >&2

rm -rf "$STAGE"
echo "release $VERSION ready at $TARBALL" >&2
