#!/bin/bash
set -e

MOXIE="$(cd "$(dirname "${MOXIE:-legacy/moxie-purego}")" && pwd)/$(basename "${MOXIE:-legacy/moxie-purego}")"
MOXIEROOT="$(pwd)"
export MOXIEROOT
export LLVM_GLUE_PATH="$MOXIEROOT/llvm-glue/libmoxie-llvm-glue.so"
export GOTOOLCHAIN=local

echo "=== Building moxie compiler from .mx source ==="
echo "Using bootstrap binary: $MOXIE"

# Step 1: Build runtime bundle by compiling empty main and capturing linker args
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

# Strip main.main and runtime.initAll to allow user definitions
llvm-extract-21 --delete --func=main.main --func=runtime.initAll \
    -o _runtime.bc "$WORK/main.o"

# Extract obj-*.bc paths and lib paths from linker command
OBJ_FILES=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/obj-' | tr '\n' ' ')
MUSL_DIR=$(echo "$LD_CMD" | tr ' ' '\n' | grep 'musl.*crt1.o' | sed 's|/crt1.o||')
COMPRT=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/compiler-rt-[^/]*/lib.a')
BDWGC=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/bdwgc-[^/]*/lib.a')
MUSL_LIB=$(echo "$LD_CMD" | tr ' ' '\n' | grep '/musl-[^/]*/lib.a')

rm -rf "$RT_DIR"

# Write sysroot config
cat > _sysroot.env << EOF
RUNTIME_BC=$MOXIEROOT/_runtime.bc
MUSL_DIR=$MUSL_DIR
COMPRT_LIB=$COMPRT
BDWGC_LIB=$BDWGC
MUSL_LIB=$MUSL_LIB
OBJ_FILES=$OBJ_FILES
EOF
echo "  runtime bundle: _runtime.bc"
echo "  obj files: $(echo $OBJ_FILES | wc -w) files"

# Step 2: Stage and build monolithic mxc binary
echo "Step 2: Build monolithic mxc binary"
STAGE=_mxc_stage
rm -rf "$STAGE"
mkdir -p "$STAGE"

cp compile/*.mx "$STAGE/"
cp mxc/main.go "$STAGE/"
cp mxc/host.c "$STAGE/"
sed -i '/^func main() {}$/d' "$STAGE/compile_exports.mx"

$MOXIE build -o moxie-self "./$STAGE"
rm -rf "$STAGE"

echo "Step 3: Verify"
./moxie-self version

echo "=== Done: ./moxie-self ==="
