package compiler

import (
	"strconv"
	"strings"

	"tinygo.org/x/go-llvm"
)

func (b *builder) defineIntrinsicFunction() {
	name := b.mxFn.RelString(nil)
	switch {
	case name == "runtime.memcpy" || name == "runtime.memmove":
		b.createMemoryCopyImpl()
	case name == "runtime.memzero":
		b.createMemoryZeroImpl()
	case name == "runtime.stacksave":
		b.createStackSaveImpl()
	case name == "runtime.KeepAlive":
		b.createKeepAliveImpl()
	case name == "machine.keepAliveNoEscape":
		b.createMachineKeepAliveImpl()
	case strings.HasPrefix(name, "runtime/volatile.Load"):
		b.createVolatileLoad()
	case strings.HasPrefix(name, "runtime/volatile.Store"):
		b.createVolatileStore()
	case strings.HasPrefix(name, "sync/atomic.") && isExported(b.mxFn.Name()):
		b.createFunctionStart(true)
		returnValue := b.createAtomicOp(b.mxFn.Name())
		if !returnValue.IsNil() {
			b.CreateRet(returnValue)
		} else {
			b.CreateRetVoid()
		}
	}
}

func isExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func (b *builder) createMemoryCopyImpl() {
	b.createFunctionStart(true)
	fnName := "llvm." + b.mxFn.Name() + ".p0.p0.i" + strconv.Itoa(b.uintptrType.IntTypeWidth())
	llvmFn := b.mod.NamedFunction(fnName)
	if llvmFn.IsNil() {
		fnType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{b.dataPtrType, b.dataPtrType, b.uintptrType, b.ctx.Int1Type()}, false)
		llvmFn = llvm.AddFunction(b.mod, fnName, fnType)
	}
	var params []llvm.Value
	for _, param := range b.mxFn.Params {
		params = append(params, b.getValueTC(param, getPosTC(param)))
	}
	params = append(params, llvm.ConstInt(b.ctx.Int1Type(), 0, false))
	b.CreateCall(llvmFn.GlobalValueType(), llvmFn, params, "")
	b.CreateRetVoid()
}

func (b *builder) createMemoryZeroImpl() {
	b.createFunctionStart(true)
	llvmFn := b.getMemsetFunc()
	p := b.mxFn.Params
	params := []llvm.Value{
		b.getValueTC(p[0], getPosTC(p[0])),
		llvm.ConstInt(b.ctx.Int8Type(), 0, false),
		b.getValueTC(p[1], getPosTC(p[1])),
		llvm.ConstInt(b.ctx.Int1Type(), 0, false),
	}
	b.CreateCall(llvmFn.GlobalValueType(), llvmFn, params, "")
	b.CreateRetVoid()
}

func (b *builder) createStackSaveImpl() {
	b.createFunctionStart(true)
	sp := b.readStackPointer()
	b.CreateRet(sp)
}

func (c *compilerContext) getMemsetFunc() llvm.Value {
	fnName := "llvm.memset.p0.i" + strconv.Itoa(c.uintptrType.IntTypeWidth())
	llvmFn := c.mod.NamedFunction(fnName)
	if llvmFn.IsNil() {
		fnType := llvm.FunctionType(c.ctx.VoidType(), []llvm.Type{c.dataPtrType, c.ctx.Int8Type(), c.uintptrType, c.ctx.Int1Type()}, false)
		llvmFn = llvm.AddFunction(c.mod, fnName, fnType)
	}
	return llvmFn
}

func (b *builder) intrinsicParam0() llvm.Value {
	// mxFn is always non-nil for live builds (verified via MOXIE_SSA_TRACE).
	return b.getValueTC(b.mxFn.Params[0], getPosTC(b.mxFn.Params[0]))
}

func (b *builder) createKeepAliveImpl() {
	b.createFunctionStart(true)
	interfaceValue := b.intrinsicParam0()
	pointerValue := b.CreateExtractValue(interfaceValue, 1, "")
	asmType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{b.dataPtrType}, false)
	asmFn := llvm.InlineAsm(asmType, "", "r", true, false, 0, false)
	b.createCall(asmType, asmFn, []llvm.Value{pointerValue}, "")
	b.CreateRetVoid()
}

func (b *builder) createAbiEscapeImpl() {
	b.createFunctionStart(true)
	pointerValue := b.intrinsicParam0()
	asmType := llvm.FunctionType(b.dataPtrType, []llvm.Type{b.dataPtrType}, false)
	asmFn := llvm.InlineAsm(asmType, "", "=r,0", true, false, 0, false)
	result := b.createCall(asmType, asmFn, []llvm.Value{pointerValue}, "")
	b.CreateRet(result)
}

func (b *builder) createMachineKeepAliveImpl() {
	b.createFunctionStart(true)
	pointerValue := b.intrinsicParam0()
	asmType := llvm.FunctionType(b.ctx.VoidType(), []llvm.Type{b.dataPtrType}, false)
	asmFn := llvm.InlineAsm(asmType, "", "r", true, false, 0, false)
	b.createCall(asmType, asmFn, []llvm.Value{pointerValue}, "")
	b.CreateRetVoid()
}

var mathToLLVMMapping = map[string]string{
	"math.Ceil":  "llvm.ceil.f64",
	"math.Exp":   "llvm.exp.f64",
	"math.Exp2":  "llvm.exp2.f64",
	"math.Floor": "llvm.floor.f64",
	"math.Log":   "llvm.log.f64",
	"math.Sqrt":  "llvm.sqrt.f64",
	"math.Trunc": "llvm.trunc.f64",
}

func (b *builder) defineMathOp() {
	b.createFunctionStart(true)
	llvmName := mathToLLVMMapping[b.mxFn.RelString(nil)]
	if llvmName == "" {
		panic("unreachable: unknown math operation")
	}
	llvmFn := b.mod.NamedFunction(llvmName)
	if llvmFn.IsNil() {
		llvmType := llvm.FunctionType(b.ctx.DoubleType(), []llvm.Type{b.ctx.DoubleType()}, false)
		llvmFn = llvm.AddFunction(b.mod, llvmName, llvmType)
	}
	args := make([]llvm.Value, len(b.mxFn.Params))
	for i, param := range b.mxFn.Params {
		args[i] = b.getValueTC(param, getPosTC(param))
	}
	result := b.CreateCall(llvmFn.GlobalValueType(), llvmFn, args, "")
	b.CreateRet(result)
}

func (b *builder) defineMathBitsIntrinsic() bool {
	if b.mxFn.Pkg == nil || b.mxFn.Pkg.Pkg.Path() != "math/bits" {
		return false
	}
	name := b.mxFn.Name()
	switch name {
	case "LeadingZeros", "LeadingZeros8", "LeadingZeros16", "LeadingZeros32", "LeadingZeros64",
		"TrailingZeros", "TrailingZeros8", "TrailingZeros16", "TrailingZeros32", "TrailingZeros64":
		b.createFunctionStart(true)
		param := b.getValueTC(b.mxFn.Params[0], b.mxFn.Pos())
		valueType := param.Type()
		var intrinsicName string
		if strings.HasPrefix(name, "Leading") {
			intrinsicName = "llvm.ctlz.i" + strconv.Itoa(valueType.IntTypeWidth())
		} else {
			intrinsicName = "llvm.cttz.i" + strconv.Itoa(valueType.IntTypeWidth())
		}
		llvmFn := b.mod.NamedFunction(intrinsicName)
		llvmFnType := llvm.FunctionType(valueType, []llvm.Type{valueType, b.ctx.Int1Type()}, false)
		if llvmFn.IsNil() {
			llvmFn = llvm.AddFunction(b.mod, intrinsicName, llvmFnType)
		}
		result := b.createCall(llvmFnType, llvmFn, []llvm.Value{
			param,
			llvm.ConstInt(b.ctx.Int1Type(), 0, false),
		}, "")
		result = b.createZExtOrTrunc(result, b.intType)
		b.CreateRet(result)
		return true
	case "Len", "Len8", "Len16", "Len32", "Len64":
		b.createFunctionStart(true)
		param := b.getValueTC(b.mxFn.Params[0], b.mxFn.Pos())
		valueType := param.Type()
		valueBits := valueType.IntTypeWidth()
		intrinsicName := "llvm.ctlz.i" + strconv.Itoa(valueBits)
		llvmFn := b.mod.NamedFunction(intrinsicName)
		llvmFnType := llvm.FunctionType(valueType, []llvm.Type{valueType, b.ctx.Int1Type()}, false)
		if llvmFn.IsNil() {
			llvmFn = llvm.AddFunction(b.mod, intrinsicName, llvmFnType)
		}
		result := b.createCall(llvmFnType, llvmFn, []llvm.Value{
			param,
			llvm.ConstInt(b.ctx.Int1Type(), 0, false),
		}, "")
		result = b.createZExtOrTrunc(result, b.intType)
		maxLen := llvm.ConstInt(b.intType, uint64(valueBits), false)
		result = b.CreateSub(maxLen, result, "")
		b.CreateRet(result)
		return true
	case "OnesCount", "OnesCount8", "OnesCount16", "OnesCount32", "OnesCount64":
		b.createFunctionStart(true)
		param := b.getValueTC(b.mxFn.Params[0], b.mxFn.Pos())
		valueType := param.Type()
		intrinsicName := "llvm.ctpop.i" + strconv.Itoa(valueType.IntTypeWidth())
		llvmFn := b.mod.NamedFunction(intrinsicName)
		llvmFnType := llvm.FunctionType(valueType, []llvm.Type{valueType}, false)
		if llvmFn.IsNil() {
			llvmFn = llvm.AddFunction(b.mod, intrinsicName, llvmFnType)
		}
		result := b.createCall(llvmFnType, llvmFn, []llvm.Value{param}, "")
		result = b.createZExtOrTrunc(result, b.intType)
		b.CreateRet(result)
		return true
	case "Reverse", "Reverse8", "Reverse16", "Reverse32", "Reverse64",
		"ReverseBytes", "ReverseBytes16", "ReverseBytes32", "ReverseBytes64":
		b.createFunctionStart(true)
		param := b.getValueTC(b.mxFn.Params[0], b.mxFn.Pos())
		valueType := param.Type()
		var intrinsicName string
		if strings.HasPrefix(name, "ReverseBytes") {
			intrinsicName = "llvm.bswap.i" + strconv.Itoa(valueType.IntTypeWidth())
		} else {
			intrinsicName = "llvm.bitreverse.i" + strconv.Itoa(valueType.IntTypeWidth())
		}
		llvmFn := b.mod.NamedFunction(intrinsicName)
		llvmFnType := llvm.FunctionType(valueType, []llvm.Type{valueType}, false)
		if llvmFn.IsNil() {
			llvmFn = llvm.AddFunction(b.mod, intrinsicName, llvmFnType)
		}
		result := b.createCall(llvmFnType, llvmFn, []llvm.Value{param}, "")
		b.CreateRet(result)
		return true
	case "RotateLeft", "RotateLeft8", "RotateLeft16", "RotateLeft32", "RotateLeft64":
		b.createFunctionStart(true)
		x := b.getValueTC(b.mxFn.Params[0], b.mxFn.Pos())
		k := b.getValueTC(b.mxFn.Params[1], b.mxFn.Pos())
		valueType := x.Type()
		intrinsicName := "llvm.fshl.i" + strconv.Itoa(valueType.IntTypeWidth())
		llvmFn := b.mod.NamedFunction(intrinsicName)
		llvmFnType := llvm.FunctionType(valueType, []llvm.Type{valueType, valueType, valueType}, false)
		if llvmFn.IsNil() {
			llvmFn = llvm.AddFunction(b.mod, intrinsicName, llvmFnType)
		}
		k = b.createZExtOrTrunc(k, valueType)
		result := b.createCall(llvmFnType, llvmFn, []llvm.Value{x, x, k}, "")
		b.CreateRet(result)
		return true
	default:
		return false
	}
}
