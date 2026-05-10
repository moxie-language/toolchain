#!/bin/bash
set -e

BASE_URL="https://git.smesh.lol/moxie-releases"
INSTALL_DIR="${MOXIE_INSTALL_DIR:-/opt/moxie}"

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

PATTERN="moxie-v.*-${OS}-${ARCH}\\.tar\\.gz"

echo "fetching release list ..." >&2
PAGE=$(curl -sL "$BASE_URL")
FILES=$(echo "$PAGE" | grep -oP '(?<=href="/moxie-releases/blob/)[^"]+\.tar\.gz' | grep -E "$PATTERN" || true)

if [ -z "$FILES" ]; then
    echo "error: no releases found for ${OS}/${ARCH}" >&2
    exit 1
fi

LATEST=$(echo "$FILES" | sed 's/moxie-v\([0-9]*\)\.\([0-9]*\)\.\([0-9]*\).*/\1 \2 \3 &/' | sort -k1,1n -k2,2n -k3,3n | tail -1 | awk '{print $4}')

echo "latest: $LATEST" >&2
echo "downloading ..." >&2

DL=$(mktemp -d)
trap "rm -rf $DL" EXIT

curl -fsSL "${BASE_URL}/raw/${LATEST}" -o "${DL}/${LATEST}"

SUDO=""
if [ ! -w "$(dirname "$INSTALL_DIR")" ] && [ "$(id -u)" != "0" ]; then
    SUDO=sudo
fi

if [ -d "$INSTALL_DIR" ]; then
    echo "removing old installation ..." >&2
    $SUDO rm -rf "$INSTALL_DIR"
fi

$SUDO mkdir -p "$INSTALL_DIR"
$SUDO tar xzf "${DL}/${LATEST}" --strip-components=1 -C "$INSTALL_DIR"

if [ ! -x "$INSTALL_DIR/bin/moxie" ]; then
    echo "error: install failed - moxie binary not found" >&2
    exit 1
fi

echo "" >&2
echo "installed to $INSTALL_DIR" >&2
echo "" >&2
echo "add to your shell profile:" >&2
echo "  export PATH=\"$INSTALL_DIR/bin:\$PATH\"" >&2
echo "  export MOXIEROOT=\"$INSTALL_DIR\"" >&2
echo "" >&2
echo "moxie requires LLVM 19 (libLLVM-19, libclang)." >&2
