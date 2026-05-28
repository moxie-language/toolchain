#!/bin/bash
set -e

FAIL=""
command -v llvm-extract-21 >/dev/null 2>&1 || FAIL="$FAIL llvm-extract-21"
command -v ld.lld-21 >/dev/null 2>&1 || FAIL="$FAIL ld.lld-21"
command -v clang-21 >/dev/null 2>&1 || FAIL="$FAIL clang-21"
if [ -n "$FAIL" ]; then
    echo "error: LLVM 21 required. missing:$FAIL" >&2
    echo "  arch:   pacman -S llvm21 clang21 lld21" >&2
    echo "  debian: apt install llvm-21 clang-21 lld-21" >&2
    exit 1
fi

MOXIE="${MOXIE:-$(command -v moxie 2>/dev/null || true)}"
if [ -z "$MOXIE" ]; then
    echo "error: moxie not found in PATH. install from https://git.smesh.lol/moxie-releases" >&2
    exit 1
fi

MOXIE="$(cd "$(dirname "$MOXIE")" && pwd)/$(basename "$MOXIE")"
MOXIEROOT="$(pwd)"
export MOXIEROOT
export LLVM_GLUE_PATH="$MOXIEROOT/llvm-glue/libmoxie-llvm-glue.so"

echo "=== Building moxie from .mx source ==="
echo "Using: $MOXIE"

# build llvm glue if missing
if [ ! -f "$MOXIEROOT/llvm-glue/libmoxie-llvm-glue.so" ]; then
    echo "Building llvm-glue ..."
    bash "$MOXIEROOT/llvm-glue/build.sh"
fi

# locate sysroot: installed moxie's sysroot, or local
INSTALLED_ROOT="$(dirname "$(dirname "$MOXIE")")"
SYSROOT=""
for d in "$INSTALLED_ROOT/sysroot" "$MOXIEROOT/sysroot"; do
    if [ -d "$d" ] && [ -f "$d/runtime.bc" ]; then
        SYSROOT="$d"
        break
    fi
done

if [ -n "$SYSROOT" ]; then
    echo "Sysroot: $SYSROOT"
    cat > _sysroot.env << EOF
RUNTIME_BC=$SYSROOT/runtime.bc
MUSL_DIR=$SYSROOT/musl
COMPRT_LIB=$SYSROOT/compiler-rt/lib.a
BDWGC_LIB=$SYSROOT/bdwgc/lib.a
MUSL_LIB=$SYSROOT/musl/lib.a
OBJ_FILES=$(echo $SYSROOT/obj/*.bc)
EOF
else
    # no packaged sysroot - extract from a bootstrap build
    echo "Step 1: Build runtime bundle"
    RT_DIR=$(mktemp -d)
    cat > "$RT_DIR/main.mx" << 'RTEOF'
package main
func main() {}
RTEOF
    BUILD_OUT=$(cd "$RT_DIR" && $MOXIE build -x -work -o "$RT_DIR/rt" . 2>&1)
    WORK=$(echo "$BUILD_OUT" | sed -n 's/^WORK=//p')
    LD_CMD=$(echo "$BUILD_OUT" | grep "^ld.lld")
    if [ -z "$WORK" ] || [ ! -f "$WORK/main.o" ]; then
        echo "error: failed to build runtime bundle"
        echo "$BUILD_OUT"
        rm -rf "$RT_DIR"
        exit 1
    fi
    llvm-extract-21 --delete --func=main.main --func=runtime.initAll \
        -o _runtime.bc "$WORK/main.o"
    OBJ_FILES=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/obj-' | tr '\n' ' ')
    MUSL_DIR=$(echo "$LD_CMD" | tr ' ' '\n' | grep 'musl.*crt1.o' | sed 's|/crt1.o||')
    COMPRT=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/compiler-rt-[^/]*/lib.a')
    BDWGC=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/bdwgc-[^/]*/lib.a')
    MUSL_LIB=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/musl-[^/]*/lib.a')
    rm -rf "$RT_DIR"
    cat > _sysroot.env << EOF
RUNTIME_BC=$MOXIEROOT/_runtime.bc
MUSL_DIR=$MUSL_DIR
COMPRT_LIB=$COMPRT
BDWGC_LIB=$BDWGC
MUSL_LIB=$MUSL_LIB
OBJ_FILES=$OBJ_FILES
EOF
    echo "  runtime bundle: _runtime.bc"
fi

# stage and build
echo "Building mxc binary ..."
STAGE=_mxc_stage
rm -rf "$STAGE"
mkdir -p "$STAGE"

cp compile/*.mx "$STAGE/"
cp mxc/main.mx "$STAGE/"
cp mxc/host.c "$STAGE/"
sed -i '/^func main() {}$/d' "$STAGE/compile_exports.mx"

# Pre-compile host.c for self-hosted mxc (which doesn't handle .c files)
if [ -f "$STAGE/host.c" ]; then
    clang-21 --target=x86_64-unknown-linux-musleabihf -emit-llvm -c \
        "$STAGE/host.c" -o "$STAGE/host.bc" 2>/dev/null || true
fi

$MOXIE build -o moxie "./$STAGE"
rm -rf "$STAGE"

./moxie version
echo "=== Done: ./moxie ==="
