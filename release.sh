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

./build.sh

rm -rf "$STAGE"
mkdir -p "$STAGE/bin"
mkdir -p "$STAGE/compile"
mkdir -p "$STAGE/mxc"

cp moxie "$STAGE/bin/moxie"
strip "$STAGE/bin/moxie" 2>/dev/null || true

# sysroot - precompiled runtime, stdlib, libs
cp -a sysroot "$STAGE/sysroot"

# stdlib source
cp -a src "$STAGE/src"

# jsruntime - WASM browser shims
cp -a jsruntime "$STAGE/jsruntime"

# compiler source - for rebuilding from source
cp compile/*.mx "$STAGE/compile/"
cp mxc/main.mx "$STAGE/mxc/"
cp mxc/host.c "$STAGE/mxc/"
cp build.sh "$STAGE/"

echo "creating tarball ..." >&2
tar czf "$TARBALL" -C /tmp "$NAME"

SIZE=$(du -h "$TARBALL" | cut -f1)
echo "tarball: $TARBALL ($SIZE)" >&2

RELEASES_DIR="/tmp/moxie-releases-stage"
rm -rf "$RELEASES_DIR"
git clone -b main git@orly:moxie-releases.git "$RELEASES_DIR"
cp "$TARBALL" "$RELEASES_DIR/"
cd "$RELEASES_DIR"
git add "$(basename "$TARBALL")"
git commit -m "release ${VERSION} ${OS}/${ARCH}"
git tag -f "$VERSION"
git push origin "$VERSION" -f
cd - >/dev/null

rm -rf "$STAGE" "$RELEASES_DIR"
echo "released $VERSION -> git.smesh.lol/moxie-releases" >&2
