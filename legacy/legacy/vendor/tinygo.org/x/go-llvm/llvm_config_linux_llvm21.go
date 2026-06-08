//go:build !purego && !byollvm && linux && !llvm14 && !llvm15 && !llvm16 && !llvm17 && !llvm18 && !llvm19 && !llvm20

package llvm

// #cgo CPPFLAGS: -I/usr/lib/llvm21/include -D_GNU_SOURCE -D__STDC_CONSTANT_MACROS -D__STDC_FORMAT_MACROS -D__STDC_LIMIT_MACROS
// #cgo CXXFLAGS: -std=c++17
// #cgo LDFLAGS: -L/usr/lib/llvm21/lib -lLLVM-21
import "C"

type run_build_sh int
