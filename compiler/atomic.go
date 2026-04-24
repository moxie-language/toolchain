package compiler

import (
	"tinygo.org/x/go-llvm"
)

func (b *builder) createAtomicOp(name string) llvm.Value {
	p := b.mxFn.Params
	pos := getPosTC(p[0])
	switch name {
	case "AddInt32", "AddInt64", "AddUint32", "AddUint64", "AddUintptr":
		ptr := b.getValueTC(p[0], pos)
		val := b.getValueTC(p[1], pos)
		oldVal := b.CreateAtomicRMW(llvm.AtomicRMWBinOpAdd, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
		return b.CreateAdd(oldVal, val, "")
	case "AndInt32", "AndInt64", "AndUint32", "AndUint64", "AndUintptr":
		ptr := b.getValueTC(p[0], pos)
		val := b.getValueTC(p[1], pos)
		return b.CreateAtomicRMW(llvm.AtomicRMWBinOpAnd, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
	case "OrInt32", "OrInt64", "OrUint32", "OrUint64", "OrUintptr":
		ptr := b.getValueTC(p[0], pos)
		val := b.getValueTC(p[1], pos)
		return b.CreateAtomicRMW(llvm.AtomicRMWBinOpOr, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
	case "SwapInt32", "SwapInt64", "SwapUint32", "SwapUint64", "SwapUintptr", "SwapPointer":
		ptr := b.getValueTC(p[0], pos)
		val := b.getValueTC(p[1], pos)
		return b.CreateAtomicRMW(llvm.AtomicRMWBinOpXchg, ptr, val, llvm.AtomicOrderingSequentiallyConsistent, true)
	case "CompareAndSwapInt32", "CompareAndSwapInt64", "CompareAndSwapUint32", "CompareAndSwapUint64", "CompareAndSwapUintptr", "CompareAndSwapPointer":
		ptr := b.getValueTC(p[0], pos)
		old := b.getValueTC(p[1], pos)
		newVal := b.getValueTC(p[2], pos)
		tuple := b.CreateAtomicCmpXchg(ptr, old, newVal, llvm.AtomicOrderingSequentiallyConsistent, llvm.AtomicOrderingSequentiallyConsistent, true)
		return b.CreateExtractValue(tuple, 1, "")
	case "LoadInt32", "LoadInt64", "LoadUint32", "LoadUint64", "LoadUintptr", "LoadPointer":
		ptr := b.getValueTC(p[0], pos)
		val := b.CreateLoad(b.getLLVMTypeTC(b.mxFn.Signature.Results().At(0).Type()), ptr, "")
		val.SetOrdering(llvm.AtomicOrderingSequentiallyConsistent)
		val.SetAlignment(b.targetData.PrefTypeAlignment(val.Type()))
		return val
	case "StoreInt32", "StoreInt64", "StoreUint32", "StoreUint64", "StoreUintptr", "StorePointer":
		ptr := b.getValueTC(p[0], pos)
		val := b.getValueTC(p[1], pos)
		store := b.CreateStore(val, ptr)
		store.SetOrdering(llvm.AtomicOrderingSequentiallyConsistent)
		store.SetAlignment(b.targetData.PrefTypeAlignment(val.Type()))
		return llvm.Value{}
	default:
		b.addError(b.mxFn.Pos(), "unknown atomic operation: "+b.mxFn.Name())
		return llvm.Value{}
	}
}
