package compiler

// This file implements the syscall.Syscall and syscall.Syscall6 instructions as
// compiler builtins.

import (
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"
	"moxie/mxssa"
	"tinygo.org/x/go-llvm"
)

// emitRawSyscallValues is the core syscall emitter. It takes pre-extracted
// LLVM values (num + args) and emits the platform-appropriate inline assembly.
// Both SSA-path and TC-path wrappers call this.
func (b *builder) emitRawSyscallValues(num llvm.Value, argValues []llvm.Value, pos token.Pos) (llvm.Value, error) {
	switch {
	case b.GOARCH == "amd64" && b.GOOS == "linux":
		args := []llvm.Value{num}
		argTypes := []llvm.Type{b.uintptrType}
		constraints := "={rax},0"
		regs := [...]string{"{rdi}", "{rsi}", "{rdx}", "{r10}", "{r8}", "{r9}"}
		for i, v := range argValues {
			constraints += "," + regs[i]
			args = append(args, v)
			argTypes = append(argTypes, v.Type())
		}
		constraints += ",~{rcx},~{r11}"
		fnType := llvm.FunctionType(b.uintptrType, argTypes, false)
		target := llvm.InlineAsm(fnType, "syscall", constraints, true, false, llvm.InlineAsmDialectIntel, false)
		return b.CreateCall(fnType, target, args, ""), nil

	case b.GOARCH == "386" && b.GOOS == "linux":
		args := []llvm.Value{num}
		argTypes := []llvm.Type{b.uintptrType}
		constraints := "={eax},0"
		regs := [...]string{"{ebx}", "{ecx}", "{edx}", "{esi}", "{edi}", "{ebp}"}
		for i, v := range argValues {
			constraints += "," + regs[i]
			args = append(args, v)
			argTypes = append(argTypes, v.Type())
		}
		fnType := llvm.FunctionType(b.uintptrType, argTypes, false)
		target := llvm.InlineAsm(fnType, "int 0x80", constraints, true, false, llvm.InlineAsmDialectIntel, false)
		return b.CreateCall(fnType, target, args, ""), nil

	case b.GOARCH == "arm" && b.GOOS == "linux":
		if arch := b.archFamily(); arch != "arm" {
			return llvm.Value{}, b.makeError(pos, "system calls are not supported: target emulates a linux/arm system on "+arch)
		}
		args := []llvm.Value{}
		argTypes := []llvm.Type{}
		constraints := "={r0}"
		regs := [...]string{"0", "{r1}", "{r2}", "{r3}", "{r4}", "{r5}", "{r6}"}
		for i, v := range argValues {
			constraints += "," + regs[i]
			args = append(args, v)
			argTypes = append(argTypes, v.Type())
		}
		args = append(args, num)
		argTypes = append(argTypes, b.uintptrType)
		constraints += ",{r7}"
		for i := len(argValues); i < 4; i++ {
			constraints += ",~{r" + strconv.Itoa(i) + "}"
		}
		fnType := llvm.FunctionType(b.uintptrType, argTypes, false)
		target := llvm.InlineAsm(fnType, "svc #0", constraints, true, false, 0, false)
		return b.CreateCall(fnType, target, args, ""), nil

	case b.GOARCH == "arm64" && b.GOOS == "linux":
		args := []llvm.Value{}
		argTypes := []llvm.Type{}
		constraints := "={x0}"
		regs := [...]string{"0", "{x1}", "{x2}", "{x3}", "{x4}", "{x5}"}
		for i, v := range argValues {
			constraints += "," + regs[i]
			args = append(args, v)
			argTypes = append(argTypes, v.Type())
		}
		args = append(args, num)
		argTypes = append(argTypes, b.uintptrType)
		constraints += ",{x8}"
		for i := len(argValues); i < 8; i++ {
			constraints += ",~{x" + strconv.Itoa(i) + "}"
		}
		constraints += ",~{x16},~{x17}"
		fnType := llvm.FunctionType(b.uintptrType, argTypes, false)
		target := llvm.InlineAsm(fnType, "svc #0", constraints, true, false, 0, false)
		return b.CreateCall(fnType, target, args, ""), nil

	case (b.GOARCH == "mips" || b.GOARCH == "mipsle") && b.GOOS == "linux":
		args := []llvm.Value{num}
		argTypes := []llvm.Type{b.uintptrType}
		constraints := "={$2},={$7},0"
		params := argValues
		if len(params) > 7 {
			params = params[:7]
		}
		regs := [...]string{"{$4}", "{$5}", "{$6}", "1", "r", "r", "r"}
		for i, v := range params {
			constraints += "," + regs[i]
			args = append(args, v)
			argTypes = append(argTypes, v.Type())
		}
		asm := "syscall"
		switch len(params) {
		case 5:
			asm = ".set noat\nsubu $$sp, $$sp, 32\nsw $7, 16($$sp)\nsyscall\naddu $$sp, $$sp, 32\n.set at\n"
		case 6:
			asm = ".set noat\nsubu $$sp, $$sp, 32\nsw $7, 16($$sp)\nsw $8, 20($$sp)\nsyscall\naddu $$sp, $$sp, 32\n.set at\n"
		case 7:
			asm = ".set noat\nsubu $$sp, $$sp, 32\nsw $7, 16($$sp)\nsw $8, 20($$sp)\nsw $9, 24($$sp)\nsyscall\naddu $$sp, $$sp, 32\n.set at\n"
		}
		constraints += ",~{$3},~{$4},~{$5},~{$6},~{$8},~{$9},~{$10},~{$11},~{$12},~{$13},~{$14},~{$15},~{$24},~{$25},~{hi},~{lo},~{memory}"
		returnType := b.ctx.StructType([]llvm.Type{b.uintptrType, b.uintptrType}, false)
		fnType := llvm.FunctionType(returnType, argTypes, false)
		target := llvm.InlineAsm(fnType, asm, constraints, true, true, 0, false)
		call := b.CreateCall(fnType, target, args, "")
		resultCode := b.CreateExtractValue(call, 0, "")
		errorFlag := b.CreateExtractValue(call, 1, "")
		zero := llvm.ConstInt(b.uintptrType, 0, false)
		isError := b.CreateICmp(llvm.IntNE, errorFlag, zero, "")
		negativeResult := b.CreateSub(zero, resultCode, "")
		result := b.CreateSelect(isError, negativeResult, resultCode, "")
		return result, nil

	default:
		return llvm.Value{}, b.makeError(pos, "unknown GOOS/GOARCH for syscall: "+b.GOOS+"/"+b.GOARCH)
	}
}

// createRawSyscall creates a system call (SSA path).
func (b *builder) createRawSyscall(call *ssa.CallCommon) (llvm.Value, error) {
	num := b.getValue(call.Args[0], getPos(call))
	var argValues []llvm.Value
	for _, arg := range call.Args[1:] {
		argValues = append(argValues, b.getValue(arg, getPos(call)))
	}
	return b.emitRawSyscallValues(num, argValues, getPos(call))
}

// createRawSyscallTC creates a system call (TC path).
func (b *builder) createRawSyscallTC(instr *mxssa.CallCommon) (llvm.Value, error) {
	num := b.getValueTC(instr.Args[0], instr.Pos())
	var argValues []llvm.Value
	for _, arg := range instr.Args[1:] {
		argValues = append(argValues, b.getValueTC(arg, instr.Pos()))
	}
	return b.emitRawSyscallValues(num, argValues, instr.Pos())
}

// createSyscall emits instructions for the syscall.Syscall* family of
// functions, depending on the target OS/arch.
func (b *builder) createSyscall(call *ssa.CallCommon) (llvm.Value, error) {
	switch b.GOOS {
	case "linux":
		syscallResult, err := b.createRawSyscall(call)
		if err != nil {
			return syscallResult, err
		}
		// Return values: r0, r1 uintptr, err Errno
		// Pseudocode:
		//     var err uintptr
		//     if syscallResult < 0 && syscallResult > -4096 {
		//         err = -syscallResult
		//     }
		//     return syscallResult, 0, err
		zero := llvm.ConstInt(b.uintptrType, 0, false)
		inrange1 := b.CreateICmp(llvm.IntSLT, syscallResult, llvm.ConstInt(b.uintptrType, 0, false), "")
		inrange2 := b.CreateICmp(llvm.IntSGT, syscallResult, llvm.ConstInt(b.uintptrType, 0xfffffffffffff000, true), "") // -4096
		hasError := b.CreateAnd(inrange1, inrange2, "")
		errResult := b.CreateSelect(hasError, b.CreateSub(zero, syscallResult, ""), zero, "syscallError")
		retval := llvm.Undef(b.ctx.StructType([]llvm.Type{b.uintptrType, b.uintptrType, b.uintptrType}, false))
		retval = b.CreateInsertValue(retval, syscallResult, 0, "")
		retval = b.CreateInsertValue(retval, zero, 1, "")
		retval = b.CreateInsertValue(retval, errResult, 2, "")
		return retval, nil
	case "windows":
		// On Windows, syscall.Syscall* is basically just a function pointer
		// call. This is complicated in gc because of stack switching and the
		// different ABI, but easy in Moxie: just call the function pointer.
		// The signature looks like this:
		//   func Syscall(trap, nargs, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno)

		isI386 := strings.HasPrefix(b.Triple, "i386-")

		// Prepare input values.
		var paramTypes []llvm.Type
		var params []llvm.Value
		for _, val := range call.Args[2:] {
			param := b.getValue(val, getPos(call))
			params = append(params, param)
			paramTypes = append(paramTypes, param.Type())
		}
		llvmType := llvm.FunctionType(b.uintptrType, paramTypes, false)
		fn := b.getValue(call.Args[0], getPos(call))
		fnPtr := b.CreateIntToPtr(fn, b.dataPtrType, "")

		// Prepare some functions that will be called later.
		setLastError := b.mod.NamedFunction("SetLastError")
		if setLastError.IsNil() {
			llvmType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{b.ctx.Int32Type()}, false)
			setLastError = llvm.AddFunction(b.mod, "SetLastError", llvmType)
			if isI386 {
				setLastError.SetFunctionCallConv(llvm.X86StdcallCallConv)
			}
		}
		getLastError := b.mod.NamedFunction("GetLastError")
		if getLastError.IsNil() {
			llvmType := llvm.FunctionType(b.ctx.Int32Type(), nil, false)
			getLastError = llvm.AddFunction(b.mod, "GetLastError", llvmType)
			if isI386 {
				getLastError.SetFunctionCallConv(llvm.X86StdcallCallConv)
			}
		}

		// Now do the actual call. Pseudocode:
		//     SetLastError(0)
		//     r1 = trap(a1, a2, a3, ...)
		//     err = uintptr(GetLastError())
		//     return r1, 0, err
		// Note that SetLastError/GetLastError could be replaced with direct
		// access to the thread control block, which is probably smaller and
		// faster. The Go runtime does this in assembly.
		// On windows/386, we also need to save/restore the stack pointer. I'm
		// not entirely sure why this is needed, but without it these calls
		// change the stack pointer leading to a crash soon after.
		setLastErrorCall := b.CreateCall(setLastError.GlobalValueType(), setLastError, []llvm.Value{llvm.ConstNull(b.ctx.Int32Type())}, "")
		var sp llvm.Value
		if isI386 {
			setLastErrorCall.SetInstructionCallConv(llvm.X86StdcallCallConv)
			sp = b.readStackPointer()
		}
		syscallResult := b.CreateCall(llvmType, fnPtr, params, "")
		if isI386 {
			syscallResult.SetInstructionCallConv(llvm.X86StdcallCallConv)
			b.writeStackPointer(sp)
		}
		errResult := b.CreateCall(getLastError.GlobalValueType(), getLastError, nil, "err")
		if isI386 {
			errResult.SetInstructionCallConv(llvm.X86StdcallCallConv)
		}
		if b.uintptrType != b.ctx.Int32Type() {
			errResult = b.CreateZExt(errResult, b.uintptrType, "err.uintptr")
		}

		// Return r1, 0, err
		retval := llvm.ConstNull(b.ctx.StructType([]llvm.Type{b.uintptrType, b.uintptrType, b.uintptrType}, false))
		retval = b.CreateInsertValue(retval, syscallResult, 0, "")
		retval = b.CreateInsertValue(retval, errResult, 2, "")
		return retval, nil

	default:
		return llvm.Value{}, b.makeError(call.Pos(), "unknown GOOS/GOARCH for syscall: "+b.GOOS+"/"+b.GOARCH)
	}
}

// createRawSyscallNoError emits instructions for the Linux-specific
// syscall.rawSyscallNoError function (SSA path).
func (b *builder) createRawSyscallNoError(call *ssa.CallCommon) (llvm.Value, error) {
	syscallResult, err := b.createRawSyscall(call)
	if err != nil {
		return syscallResult, err
	}
	retval := llvm.ConstNull(b.ctx.StructType([]llvm.Type{b.uintptrType, b.uintptrType}, false))
	retval = b.CreateInsertValue(retval, syscallResult, 0, "")
	retval = b.CreateInsertValue(retval, llvm.ConstInt(b.uintptrType, 0, false), 1, "")
	return retval, nil
}

// createSyscallTC emits the syscall.Syscall* intrinsic (TC path).
func (b *builder) createSyscallTC(instr *mxssa.CallCommon) (llvm.Value, error) {
	switch b.GOOS {
	case "linux":
		syscallResult, err := b.createRawSyscallTC(instr)
		if err != nil {
			return syscallResult, err
		}
		zero := llvm.ConstInt(b.uintptrType, 0, false)
		inrange1 := b.CreateICmp(llvm.IntSLT, syscallResult, zero, "")
		inrange2 := b.CreateICmp(llvm.IntSGT, syscallResult, llvm.ConstInt(b.uintptrType, 0xfffffffffffff000, true), "")
		hasError := b.CreateAnd(inrange1, inrange2, "")
		errResult := b.CreateSelect(hasError, b.CreateSub(zero, syscallResult, ""), zero, "syscallError")
		retval := llvm.Undef(b.ctx.StructType([]llvm.Type{b.uintptrType, b.uintptrType, b.uintptrType}, false))
		retval = b.CreateInsertValue(retval, syscallResult, 0, "")
		retval = b.CreateInsertValue(retval, zero, 1, "")
		retval = b.CreateInsertValue(retval, errResult, 2, "")
		return retval, nil
	default:
		return llvm.Value{}, b.makeError(instr.Pos(), "unknown GOOS for syscall TC path: "+b.GOOS)
	}
}

// createRawSyscallNoErrorTC emits syscall.rawSyscallNoError (TC path).
func (b *builder) createRawSyscallNoErrorTC(instr *mxssa.CallCommon) (llvm.Value, error) {
	syscallResult, err := b.createRawSyscallTC(instr)
	if err != nil {
		return syscallResult, err
	}
	retval := llvm.ConstNull(b.ctx.StructType([]llvm.Type{b.uintptrType, b.uintptrType}, false))
	retval = b.CreateInsertValue(retval, syscallResult, 0, "")
	retval = b.CreateInsertValue(retval, llvm.ConstInt(b.uintptrType, 0, false), 1, "")
	return retval, nil
}

// Lower a call to internal/abi.FuncPCABI0 on MacOS.
// This function is called like this:
//
//	syscall(abi.FuncPCABI0(libc_mkdir_trampoline), uintptr(unsafe.Pointer(_p0)), uintptr(mode), 0)
//
// So we'll want to return a function pointer (as uintptr) that points to the
// libc function. Specifically, we _don't_ want to point to the trampoline
// function (which is implemented in Go assembly which we can't read), but
// rather to the actually intended function. For this we're going to assume that
// all the functions follow a specific pattern: libc_<functionname>_trampoline.
//
// The return value is the function pointer as an uintptr, or a nil value if
// this isn't possible (and a regular call should be made as fallback).
func (b *builder) createDarwinFuncPCABI0Call(instr *ssa.CallCommon) llvm.Value {
	if b.GOOS != "darwin" {
		// This has only been tested on MacOS (and only seems to be used there).
		return llvm.Value{}
	}

	// Check that it uses a function call like syscall.libc_*_trampoline
	itf := instr.Args[0].(*ssa.MakeInterface)
	calledFn := itf.X.(*ssa.Function)
	if pkgName := calledFn.Pkg.Pkg.Path(); pkgName != "syscall" && pkgName != "internal/syscall/unix" {
		return llvm.Value{}
	}
	if !strings.HasPrefix(calledFn.Name(), "libc_") || !strings.HasSuffix(calledFn.Name(), "_trampoline") {

		return llvm.Value{}
	}

	// Extract the libc function name.
	name := strings.TrimPrefix(strings.TrimSuffix(calledFn.Name(), "_trampoline"), "libc_")
	if name == "open" {
		// Special case: open() is a variadic function and can't be called like
		// a regular function. Therefore, we need to use a wrapper implemented
		// in C.
		name = "syscall_libc_open"
	}
	if b.GOARCH == "amd64" {
		if name == "fdopendir" || name == "readdir_r" {
			// Hack to support amd64, which needs the $INODE64 suffix.
			// This is also done in upstream Go:
			// https://github.com/golang/go/commit/096ab3c21b88ccc7d411379d09fe6274e3159467
			name += "$INODE64"
		}
	}

	// Obtain the C function.
	// Use a simple function (no parameters or return value) because all we need
	// is the address of the function.
	llvmFn := b.mod.NamedFunction(name)
	if llvmFn.IsNil() {
		llvmFnType := llvm.FunctionType(b.ctx.VoidType(), nil, false)
		llvmFn = llvm.AddFunction(b.mod, name, llvmFnType)
	}

	// Cast the function pointer to a uintptr (because that's what
	// abi.FuncPCABI0 returns).
	return b.CreatePtrToInt(llvmFn, b.uintptrType, "")
}
