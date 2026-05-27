//go:build purego

package llvm

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

type PassBuilderOptions struct {
	C uintptr
}

var (
	symCreatePassBuilderOptions  uintptr
	symDisposePassBuilderOptions uintptr
	symRunPasses                 uintptr
	symGetErrorMessage           uintptr
	symDisposeErrorMessage       uintptr
	symPBOSetVerifyEach          uintptr
	symPBOSetDebugLogging        uintptr
	symPBOSetLoopInterleaving    uintptr
	symPBOSetLoopVectorization   uintptr
	symPBOSetSLPVectorization    uintptr
	symPBOSetLoopUnrolling       uintptr
	symPBOSetMergeFunctions      uintptr
)

func registerPassSymbols() {
	symCreatePassBuilderOptions = mustSym(libLLVM, "LLVMCreatePassBuilderOptions")
	symDisposePassBuilderOptions = mustSym(libLLVM, "LLVMDisposePassBuilderOptions")
	symRunPasses = mustSym(libLLVM, "LLVMRunPasses")
	symGetErrorMessage = mustSym(libLLVM, "LLVMGetErrorMessage")
	symDisposeErrorMessage = mustSym(libLLVM, "LLVMDisposeErrorMessage")
	symPBOSetVerifyEach = mustSym(libLLVM, "LLVMPassBuilderOptionsSetVerifyEach")
	symPBOSetDebugLogging = mustSym(libLLVM, "LLVMPassBuilderOptionsSetDebugLogging")
	symPBOSetLoopInterleaving = mustSym(libLLVM, "LLVMPassBuilderOptionsSetLoopInterleaving")
	symPBOSetLoopVectorization = mustSym(libLLVM, "LLVMPassBuilderOptionsSetLoopVectorization")
	symPBOSetSLPVectorization = mustSym(libLLVM, "LLVMPassBuilderOptionsSetSLPVectorization")
	symPBOSetLoopUnrolling = mustSym(libLLVM, "LLVMPassBuilderOptionsSetLoopUnrolling")
	symPBOSetMergeFunctions = mustSym(libLLVM, "LLVMPassBuilderOptionsSetMergeFunctions")
}

func NewPassBuilderOptions() (pbo PassBuilderOptions) {
	r, _, _ := purego.SyscallN(symCreatePassBuilderOptions)
	pbo.C = r
	return
}

func (mod Module) RunPasses(passes string, tm TargetMachine, options PassBuilderOptions) error {
	cpasses := cString(passes)
	r, _, _ := purego.SyscallN(symRunPasses, mod.C, cpasses, tm.C, options.C)
	runtime.KeepAlive(passes)
	if r != 0 {
		cstr, _, _ := purego.SyscallN(symGetErrorMessage, r)
		s := goString(cstr)
		purego.SyscallN(symDisposeErrorMessage, cstr)
		return errors.New(s)
	}
	return nil
}

func (pbo PassBuilderOptions) SetVerifyEach(v bool) {
	purego.SyscallN(symPBOSetVerifyEach, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) SetDebugLogging(v bool) {
	purego.SyscallN(symPBOSetDebugLogging, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) SetLoopInterleaving(v bool) {
	purego.SyscallN(symPBOSetLoopInterleaving, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) SetLoopVectorization(v bool) {
	purego.SyscallN(symPBOSetLoopVectorization, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) SetSLPVectorization(v bool) {
	purego.SyscallN(symPBOSetSLPVectorization, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) SetLoopUnrolling(v bool) {
	purego.SyscallN(symPBOSetLoopUnrolling, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) SetMergeFunctions(v bool) {
	purego.SyscallN(symPBOSetMergeFunctions, pbo.C, boolToInt(v))
}

func (pbo PassBuilderOptions) Dispose() {
	purego.SyscallN(symDisposePassBuilderOptions, pbo.C)
}

func (pbo PassBuilderOptions) SetForgetAllSCEVInLoopUnroll(v bool) {}
func (pbo PassBuilderOptions) SetLicmMssaOptCap(optCap uint)      {}
func (pbo PassBuilderOptions) SetLicmMssaNoAccForPromotionCap(cap uint) {}
func (pbo PassBuilderOptions) SetCallGraphProfile(v bool) {}

func init() {
	_ = unsafe.Pointer(nil)
}
