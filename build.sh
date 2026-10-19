#!/bin/bash
set -e

# The DRIVER speaks one LLVM major: whichever `llvm-config` reports. The tools
# are asked for by their unversioned names and every one of them must report that
# same major - a "-22" suffix on a binary that is really 23 is exactly the lie
# that let this stack drift for months, and mixing majors inside one pipeline
# produces bitcode the next tool cannot read (LLVM reads older bitcode, never
# newer: a newer opt rewrites llvm.returnaddress into a name an older clang
# cannot lower).
#
# The LEGACY bootstrap compiler is a separate client: its vendored go-llvm
# purego layer is LLVM 22 shaped (it reports a `br` with the wrong opcode under
# 23, measured), so it loads libLLVM-22. It writes 22 bitcode that this 23
# driver reads - the supported direction - and no 22 tool ever reads 23 output.
LLVM_MAJOR="${LLVM_MAJOR:-}"
if [ -z "$LLVM_MAJOR" ]; then
    if command -v llvm-config >/dev/null 2>&1; then
        LLVM_MAJOR="$(llvm-config --version 2>/dev/null | cut -d. -f1)"
    fi
fi
LEGACY_LLVM_MAJOR="${LEGACY_LLVM_MAJOR:-22}"

need_major() {
    local name="$1" bin="$1" want="$2"
    command -v "$bin" >/dev/null 2>&1 || { FAIL="$FAIL $name"; return; }
    local ver
    ver="$("$bin" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)"
    if [ "$ver" != "$want" ]; then FAIL="$FAIL $name(=${ver:-unknown})"; fi
}

FAIL=""
if [ -z "$LLVM_MAJOR" ]; then
    echo "error: no llvm-config found; set LLVM_MAJOR to the LLVM major to use" >&2
    exit 1
fi
for tool in clang ld.lld opt llvm-link llvm-extract; do need_major "$tool" "$LLVM_MAJOR"; done
if [ -n "$FAIL" ]; then
    echo "error: LLVM $LLVM_MAJOR required (the host's one major). bad/missing:$FAIL" >&2
    echo "  arch:   pacman -S llvm clang lld" >&2
    exit 1
fi

# The legacy bootstrap compiler needs its own major's shared library; without it
# the reference half of the bootstrap (phase 2, phase 6 :legacy) cannot run.
LEGACY_LIB=""
for p in "/usr/lib/libLLVM-${LEGACY_LLVM_MAJOR}.so" \
         "/usr/lib64/libLLVM-${LEGACY_LLVM_MAJOR}.so" \
         "/usr/lib/llvm-${LEGACY_LLVM_MAJOR}/lib/libLLVM-${LEGACY_LLVM_MAJOR}.so"; do
    if [ -f "$p" ]; then LEGACY_LIB="$p"; break; fi
done
if [ -z "$LEGACY_LIB" ]; then
    echo "warning: libLLVM-${LEGACY_LLVM_MAJOR}.so not found: legacy/moxie cannot run," >&2
    echo "         so the bootstrap chain (phase 2, phase 6 :legacy) is unavailable." >&2
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
    echo "Building llvm-glue (LLVM ${LEGACY_LLVM_MAJOR}, the legacy client) ..."
    LLVM_VER="$LEGACY_LLVM_MAJOR" bash "$GLUE_SRC_DIR/build.sh"
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
