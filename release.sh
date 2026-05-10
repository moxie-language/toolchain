#!/bin/bash
set -e

VERSION=$(git describe --tags --abbrev=0)
if [ -z "$VERSION" ]; then
    echo "error: no git tag found" >&2
    exit 1
fi

DIRTY=$(git diff --quiet HEAD -- && echo "" || echo "-dirty")
if [ -n "$DIRTY" ]; then
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

# build moxie compiler
./build.sh

# verify moxiejs binary exists (tracked in git, not built from source)
if [ ! -f moxiejs ]; then
    echo "error: moxiejs binary not found" >&2
    exit 1
fi

# stage the release tree
rm -rf "$STAGE"
mkdir -p "$STAGE/bin"

cp moxie "$STAGE/bin/"
cp moxiejs "$STAGE/bin/"

# stdlib sources
cp -a src "$STAGE/src"

# lib - dereference symlinks so tarball is self-contained
cp -aL lib "$STAGE/lib"

# jsruntime
cp -a jsruntime "$STAGE/jsruntime"

# strip
strip "$STAGE/bin/moxie" 2>/dev/null || true
strip "$STAGE/bin/moxiejs" 2>/dev/null || true

echo "creating tarball ..." >&2
tar czf "$TARBALL" -C /tmp "$NAME"

SIZE=$(du -h "$TARBALL" | cut -f1)
echo "tarball: $TARBALL ($SIZE)" >&2

# push to releases repo
RELEASES_DIR="/tmp/moxie-releases-stage"
rm -rf "$RELEASES_DIR"

git clone -b main git@orly:moxie-releases.git "$RELEASES_DIR"

cp "$TARBALL" "$RELEASES_DIR/"

cd "$RELEASES_DIR"
git add "$(basename "$TARBALL")"
git commit -m "release ${VERSION} ${OS}/${ARCH}"
git tag -f "$VERSION"
git push origin main
git push origin "$VERSION" -f
cd - >/dev/null

rm -rf "$STAGE" "$RELEASES_DIR"
echo "released $VERSION -> git.smesh.lol/moxie-releases" >&2
