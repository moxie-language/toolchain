//go:build purego

package llvm

import (
	"errors"
	"unsafe"

	"github.com/ebitengine/purego"
)

type VerifierFailureAction int

const (
	AbortProcessAction VerifierFailureAction = 0
	PrintMessageAction VerifierFailureAction = 1
	ReturnStatusAction VerifierFailureAction = 2
)

func VerifyModule(m Module, a VerifierFailureAction) error {
	var msg uintptr
	r, _, _ := purego.SyscallN(symVerifyModule, m.C, uintptr(a), uintptr(unsafe.Pointer(&msg)))
	if r != 0 {
		s := goString(msg)
		purego.SyscallN(symDisposeMessage, msg)
		return errors.New(s)
	}
	return nil
}

var verifyFunctionError = errors.New("Function is broken")

func VerifyFunction(f Value, a VerifierFailureAction) error {
	r, _, _ := purego.SyscallN(symVerifyFunction, f.C, uintptr(a))
	if r != 0 {
		return verifyFunctionError
	}
	return nil
}
