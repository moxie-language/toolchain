//go:build !purego && !byollvm && linux && llvm22

package llvm

// #cgo CPPFLAGS: -I/usr/include -D_GNU_SOURCE -D__STDC_CONSTANT_MACROS -D__STDC_FORMAT_MACROS -D__STDC_LIMIT_MACROS
// #cgo CXXFLAGS: -std=c++17
// #cgo LDFLAGS: -L/usr/lib -lLLVM-22
import "C"

type run_build_sh int
