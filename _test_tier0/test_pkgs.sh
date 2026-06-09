#!/bin/bash
set -e
cd /home/mleku/s/moxie
PASS=0
FAIL=0
SKIP=0

test_pkg() {
    local pkg="$1"
    local code="$2"
    cat > _test_tier0/main.mx << EOF
package main

import "$pkg"

func main() {
    $code
}
EOF
    if MOXIEROOT=. ./moxie build -a -o _test_tier0/out ./_test_tier0 2>/tmp/mxc_err; then
        if ./_test_tier0/out 2>&1; then
            echo "PASS: $pkg"
            PASS=$((PASS+1))
        else
            echo "FAIL(run): $pkg"
            FAIL=$((FAIL+1))
        fi
    else
        echo "FAIL(build): $pkg"
        cat /tmp/mxc_err | tail -5
        FAIL=$((FAIL+1))
    fi
    rm -f _test_tier0/out
}

# Tier 0 packages with actual compilable source
test_pkg "errors" 'e := errors.New("x"); _ = e'
test_pkg "unicode/utf8" '_ = utf8.RuneCountInString("hi")'
test_pkg "unicode/utf16" 'b := utf16.Encode([]int32{0x48}); _ = b'
test_pkg "unicode" '_ = unicode.IsLetter(int32(65))'
test_pkg "internal/bytealg" '_ = bytealg.IndexByte([]byte("abc"), byte(98))'
test_pkg "internal/byteorder" '_ = byteorder.LEUint32([]byte{1,0,0,0})'
test_pkg "internal/itoa" '_ = itoa.Itoa(42)'
test_pkg "internal/binary" '_ = binary.LittleEndian.Uint32([]byte{1,0,0,0})'
test_pkg "structs" '_ = structs.HostLayout[int32]{}'

echo ""
echo "Results: $PASS pass, $FAIL fail, $SKIP skip"
