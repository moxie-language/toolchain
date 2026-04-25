package compiler

// This file implements the syscall.Syscall and syscall.Syscall6 instructions as
// compiler builtins.

import (
	"go/token"
	"strconv"
	"strings"

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

// createRawSyscallTC creates a system call (TC path).
func (b *builder) createRawSyscallTC(instr *mxssa.CallCommon) (llvm.Value, error) {
	num := b.getValueTC(instr.Args[0], instr.Pos())
	var argValues []llvm.Value
	for _, arg := range instr.Args[1:] {
		argValues = append(argValues, b.getValueTC(arg, instr.Pos()))
	}
	return b.emitRawSyscallValues(num, argValues, instr.Pos())
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

// createDarwinFuncPCABI0CallTC lowers a call to internal/abi.FuncPCABI0 on
// macOS (TC path). Returns a nil value if not applicable (caller falls through
// to normal call).
func (b *builder) createDarwinFuncPCABI0CallTC(instr *mxssa.CallCommon) llvm.Value {
	if b.GOOS != "darwin" {
		return llvm.Value{}
	}
	itf, ok := instr.Args[0].(*mxssa.MakeInterface)
	if !ok {
		return llvm.Value{}
	}
	calledFn, ok := itf.X.(*mxssa.Function)
	if !ok {
		return llvm.Value{}
	}
	if pkg := calledFn.Pkg.Pkg.Path(); pkg != "syscall" && pkg != "internal/syscall/unix" {
		return llvm.Value{}
	}
	if !strings.HasPrefix(calledFn.Name(), "libc_") || !strings.HasSuffix(calledFn.Name(), "_trampoline") {
		return llvm.Value{}
	}
	name := strings.TrimPrefix(strings.TrimSuffix(calledFn.Name(), "_trampoline"), "libc_")
	if name == "open" {
		name = "syscall_libc_open"
	}
	if b.GOARCH == "amd64" {
		if name == "fdopendir" || name == "readdir_r" {
			name += "$INODE64"
		}
	}
	llvmFn := b.mod.NamedFunction(name)
	if llvmFn.IsNil() {
		llvmFnType := llvm.FunctionType(b.ctx.VoidType(), nil, false)
		llvmFn = llvm.AddFunction(b.mod, name, llvmFnType)
	}
	return b.CreatePtrToInt(llvmFn, b.uintptrType, "")
}
