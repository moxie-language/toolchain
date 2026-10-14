#!/bin/bash
set -e

# LLVM 22 is pinned across the whole stack. Some distros ship only the
# unversioned tool names for opt/llvm-extract, so accept either name but
# verify the major version is 22 - a name is not proof of provenance.
# Output formats differ ("clang version 22.1.8", "LLD 22.1.8",
# "LLVM version 22.1.8"), so pull the first major.minor token.
need_llvm22() {
    local name="$1" bin=""
    for c in "${name}-22" "$name"; do
        if command -v "$c" >/dev/null 2>&1; then bin="$c"; break; fi
    done
    if [ -z "$bin" ]; then FAIL="$FAIL ${name}-22"; return; fi
    local ver
    ver="$("$bin" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)"
    if [ "$ver" != "22" ]; then FAIL="$FAIL ${name}(=${ver:-unknown})"; fi
}

FAIL=""
for tool in clang ld.lld opt llvm-link llvm-extract; do need_llvm22 "$tool"; done
if [ -n "$FAIL" ]; then
    echo "error: LLVM 22 required (pinned across the stack). bad/missing:$FAIL" >&2
    echo "  arch:   pacman -S llvm22 clang22 lld22" >&2
    echo "  debian: apt install llvm-22 clang-22 lld-22" >&2
    exit 1
fi

MOXIEROOT="$(pwd)"
export MOXIEROOT
export LLVM_GLUE_PATH="$MOXIEROOT/llvm-glue/libmoxie-llvm-glue.so"
export PATH="/usr/local/bin:$PATH"

# Build the LLVM glue when missing or when a source is newer than the .so.
# The purego bindings resolve glue symbols with mustSym, so a stale .so is a
# hard failure (missing symbol), not a silent fallback.
GLUE_SRC_DIR="$MOXIEROOT/llvm-glue"
GLUE_STALE=0
for src in "$GLUE_SRC_DIR/shims.c" \
	"$MOXIEROOT/legacy/vendor/tinygo.org/x/go-llvm/IRBindings.cpp" \
	"$MOXIEROOT/legacy/vendor/tinygo.org/x/go-llvm/backports.cpp"; do
	if [ "$src" -nt "$LLVM_GLUE_PATH" ]; then GLUE_STALE=1; fi
done
if [ ! -f "$LLVM_GLUE_PATH" ] || [ "$GLUE_STALE" = 1 ]; then
    echo "Building llvm-glue ..."
    bash "$GLUE_SRC_DIR/build.sh"
fi

# find a moxie binary to bootstrap with
MOXIE="${MOXIE:-}"
if [ -z "$MOXIE" ]; then
    for m in "$MOXIEROOT/moxie-new" "$MOXIEROOT/moxie" "$MOXIEROOT/legacy/moxie" "$(command -v moxie 2>/dev/null || true)"; do
        if [ -x "$m" ]; then
            MOXIE="$m"
            break
        fi
    done
fi
if [ -z "$MOXIE" ]; then
    echo "error: no moxie binary found" >&2
    echo "  place a bootstrap binary at ./moxie-new, ./moxie, or ./legacy/moxie" >&2
    exit 1
fi

MOXIE="$(cd "$(dirname "$MOXIE")" && pwd)/$(basename "$MOXIE")"
echo "=== Building moxie (self-hosting) ==="
echo "Bootstrap: $MOXIE"

# compiler source directory
COMPILER_SRC="$MOXIEROOT/_mxc_stage4"
if [ ! -d "$COMPILER_SRC" ] || [ ! -f "$COMPILER_SRC/main.mx" ]; then
    echo "error: compiler source not found in _mxc_stage4/" >&2
    exit 1
fi
echo "Source: $COMPILER_SRC"

$MOXIE build -o "$MOXIEROOT/moxie-new" "$COMPILER_SRC/"

# verify the new binary works
"$MOXIEROOT/moxie-new" version

mv "$MOXIEROOT/moxie-new" "$MOXIEROOT/moxie"

# rebuild all cached packages with the new compiler and remove old version caches
"$MOXIEROOT/moxie" update --all

# run regression validation
echo "=== Regression Check ==="
if ! MOXIE="$MOXIEROOT/moxie" bash "$MOXIEROOT/tests/unitrun.sh" --phases 0a --fail-fast; then
    echo "REGRESSION DETECTED: bootstrap build suspect" >&2
    exit 1
fi

echo "=== Done: ./moxie ==="
