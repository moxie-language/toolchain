#!/bin/bash
# Install a Moxie binary release.
#
# Layout (all versions retained):
#   $INSTALL_ROOT/versions/<tag>/     bin, src, sysroot, llvm-glue, compiler
#   $INSTALL_ROOT/current -> versions/<tag>
#   $INSTALL_ROOT/{src,sysroot,llvm-glue,compiler,bin} -> current/<name>
#   $BINDIR/moxie-<tag>               real binary
#   $BINDIR/moxie -> moxie-<tag>
#
# MOXIEROOT=$INSTALL_ROOT therefore follows the current version, and the stdlib
# is versioned with the compiler that expects it.
#
# Usage: install.sh [TAG]
# Env: MOXIE_INSTALL_DIR, MOXIE_BINDIR, GH_REPO, GH_TOKEN
set -euo pipefail

REPO="${GH_REPO:-moxie-language/toolchain}"
INSTALL_ROOT="${MOXIE_INSTALL_DIR:-$HOME/.local/share/moxie}"
BINDIR="${MOXIE_BINDIR:-$HOME/.local/bin}"
VERSION="${1:-}"

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) echo "error: unsupported arch $ARCH" >&2; exit 1 ;;
esac
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [ "$OS" != "linux" ]; then
    echo "error: only linux supported, got $OS" >&2
    exit 1
fi

if [ -e "$INSTALL_ROOT/.git" ]; then
    echo "error: $INSTALL_ROOT is a source checkout (it has .git)" >&2
    echo "       set MOXIE_INSTALL_DIR to the directory this installer should own" >&2
    exit 1
fi

if [ -z "$VERSION" ]; then
    if command -v gh >/dev/null 2>&1 && gh api user --jq .login >/dev/null 2>&1; then
        VERSION=$(gh release list --repo "$REPO" --limit 100 --json tagName,isDraft \
            --jq '.[] | select(.isDraft|not) | .tagName' | sort -V | tail -1)
    else
        VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=100" \
            | grep -o '"tag_name": *"[^"]*"' | sed 's/.*"tag_name": *"//;s/"$//' | sort -V | tail -1)
    fi
fi
if [ -z "$VERSION" ]; then
    echo "error: no release found for $REPO" >&2
    exit 1
fi

ASSET="moxie-${VERSION}-${OS}-${ARCH}.tar.gz"
echo "installing $VERSION ($ASSET) ..." >&2

DL=$(mktemp -d)
trap 'rm -rf "$DL"' EXIT

if ! command -v gh >/dev/null 2>&1 || ! gh release download "$VERSION" --repo "$REPO" \
        --pattern "$ASSET" --dir "$DL" >/dev/null 2>&1; then
    echo "downloading asset via curl ..." >&2
    curl -fsSL "https://github.com/$REPO/releases/download/$VERSION/$ASSET" -o "$DL/$ASSET"
fi
[ -f "$DL/$ASSET" ] || { echo "error: asset not downloaded: $ASSET" >&2; exit 1; }

VROOT="$INSTALL_ROOT/versions/$VERSION"
mkdir -p "$INSTALL_ROOT/versions"
rm -rf "$VROOT"
mkdir -p "$VROOT"
tar xzf "$DL/$ASSET" --strip-components=1 -C "$VROOT"

for need in bin/moxie src sysroot; do
    if [ ! -e "$VROOT/$need" ]; then
        echo "error: release is missing $need" >&2
        exit 1
    fi
done

# First versioned install over an old flat install: keep the old tree rather
# than deleting it, then replace the top-level names with links.
if [ -d "$INSTALL_ROOT/src" ] && [ ! -L "$INSTALL_ROOT/src" ]; then
    BAK="$INSTALL_ROOT/../$(basename "$INSTALL_ROOT").pre-versioned.$(date +%s)"
    echo "moving old flat install to $BAK ..." >&2
    mv "$INSTALL_ROOT" "$BAK"
    mkdir -p "$INSTALL_ROOT/versions"
fi

ln -sfn "versions/$VERSION" "$INSTALL_ROOT/current"
for name in src sysroot llvm-glue compiler bin; do
    rm -rf "$INSTALL_ROOT/$name"
    if [ -e "$VROOT/$name" ]; then
        ln -sfn "current/$name" "$INSTALL_ROOT/$name"
    fi
done

mkdir -p "$BINDIR"
cp "$VROOT/bin/moxie" "$BINDIR/moxie-$VERSION"
chmod +x "$BINDIR/moxie-$VERSION"
ln -sfn "moxie-$VERSION" "$BINDIR/moxie"

echo "verifying ..." >&2
MOXIEROOT="$INSTALL_ROOT" "$BINDIR/moxie" version

echo "rebuilding package cache with new compiler ..." >&2
MOXIEROOT="$INSTALL_ROOT" LLVM_GLUE_PATH="$INSTALL_ROOT/llvm-glue/libmoxie-llvm-glue.so" \
    "$BINDIR/moxie" update --all

echo "warming stdlib cache ..." >&2
MOXIEROOT="$INSTALL_ROOT" LLVM_GLUE_PATH="$INSTALL_ROOT/llvm-glue/libmoxie-llvm-glue.so" \
    "$BINDIR/moxie" cache warm

echo "" >&2
echo "installed $VERSION:" >&2
echo "  root:   $INSTALL_ROOT (current -> versions/$VERSION)" >&2
echo "  binary: $BINDIR/moxie-$VERSION (symlink: moxie)" >&2
echo "" >&2
echo "add to your shell profile:" >&2
echo "  export PATH=\"$BINDIR:\$PATH\"" >&2
echo "  export MOXIEROOT=\"$INSTALL_ROOT\"" >&2
