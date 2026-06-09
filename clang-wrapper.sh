#!/bin/bash
# Wrapper for clang-21 that patches known IR issues before compiling
for arg in "$@"; do
  if [[ "$arg" == *"_mxc_stage4_"*".ll" ]]; then
    # Patch the resolveArrayLenFromSrc call
    if grep -q 'resolveArrayLenFromSrc({ptr, i32, i32} %t320, ptr null)' "$arg" 2>/dev/null; then
      sed -i 's|%t319 = load ptr, ptr %t264|%t319.gep = getelementptr inbounds {{ptr, ptr}, {ptr, ptr}, {{{ptr, i32, i32}}, {{{ptr, ptr}, {ptr, ptr}, i16}}}}, ptr %t318, i32 0, i32 2, i32 0, i32 0\n  %t319.pos = load {ptr, i32, i32}, ptr %t319.gep|' "$arg"
      sed -i 's|call i64 @main.resolveArrayLenFromSrc({ptr, i32, i32} %t320, ptr null)|call i64 @main.resolveArrayLenFromSrc({ptr, i32, i32} %t319.pos, ptr %t320)|' "$arg"
    fi
  fi
done
exec clang-21 "$@"
