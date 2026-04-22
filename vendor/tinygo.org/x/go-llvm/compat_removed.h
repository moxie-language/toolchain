// Stubs for C API functions removed in LLVM 20+.
// None are called by the Moxie compiler; they exist only so the Go bindings compile.

#ifndef LLVM_COMPAT_REMOVED_H
#define LLVM_COMPAT_REMOVED_H

#include "llvm-c/Core.h"
#include "llvm/Config/llvm-config.h"

#if LLVM_VERSION_MAJOR >= 20

// LLVMConstMul and friends were removed after LLVM 19.
static inline LLVMValueRef LLVMConstMul(LLVMValueRef lhs, LLVMValueRef rhs) { (void)lhs; (void)rhs; return NULL; }
static inline LLVMValueRef LLVMConstNSWMul(LLVMValueRef lhs, LLVMValueRef rhs) { (void)lhs; (void)rhs; return NULL; }
static inline LLVMValueRef LLVMConstNUWMul(LLVMValueRef lhs, LLVMValueRef rhs) { (void)lhs; (void)rhs; return NULL; }

// LandingPad clause enum was removed.
typedef int LLVMLandingPadClauseTy;
#define LLVMLandingPadCatch 0
#define LLVMLandingPadFilter 1

#endif // LLVM_VERSION_MAJOR >= 20

#endif
