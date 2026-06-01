package compiler

import (
	"fmt"

	"moxie/compiler/llvmutil"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// usesDealloc returns true when the dealloc GC is active.
func (b *builder) usesDealloc() bool {
	return b.GC == "dealloc"
}

// deallocPushAlloc emits a runtime.deallocPush call to register a heap
// pointer in the function's shadow list. The entry node is stack-allocated.
func (b *builder) deallocPushAlloc(ptr llvm.Value, allocInstr ssa.Value) {
	if b.deallocHead.IsNil() {
		return
	}
	entryType := b.ctx.StructType([]llvm.Type{b.dataPtrType, b.dataPtrType}, false)
	entry := llvmutil.CreateEntryBlockAlloca(b.Builder, entryType, "dealloc.entry")
	b.CreateStore(llvm.ConstNull(entryType), entry)
	b.createRuntimeCall("deallocPush", []llvm.Value{ptr, b.deallocHead, entry}, "")
	if allocInstr != nil {
		b.deallocAllocs[allocInstr] = true
	}
}

// deallocEmitFree emits the runtime.deallocFree call that frees all
// non-escaped entries in the shadow list. Called before each Return.
func (b *builder) deallocEmitFree() {
	if b.deallocHead.IsNil() {
		return
	}
	head := b.CreateLoad(b.dataPtrType, b.deallocHead, "dealloc.list")
	b.createRuntimeCall("deallocFree", []llvm.Value{head}, "")
}

// deallocEmitEscape marks a pointer as escaped so deallocFree won't free it.
// Called when a heap-allocated value is returned from the function.
func (b *builder) deallocEmitEscape(ptr llvm.Value) {
	if b.deallocHead.IsNil() {
		return
	}
	head := b.CreateLoad(b.dataPtrType, b.deallocHead, "dealloc.esc.list")
	b.createRuntimeCall("deallocEscape", []llvm.Value{ptr, head}, "")
}

// deallocCheckReturn inspects the return values and emits deallocEscape
// for any that were heap-allocated in this function.
func (b *builder) deallocCheckReturn(instr *ssa.Return) {
	if b.deallocHead.IsNil() {
		return
	}
	for _, result := range instr.Results {
		b.deallocEscapeValue(result)
	}
}

// deallocEscapeValue marks a value as escaped if it was heap-allocated
// in this function or is derived from such an allocation.
func (b *builder) deallocEscapeValue(val ssa.Value) {
	switch v := val.(type) {
	case *ssa.Alloc:
		if b.deallocAllocs[v] {
			b.deallocEmitEscape(b.getValue(v, v.Pos()))
		}
	case *ssa.MakeInterface:
		b.deallocEscapeValue(v.X)
	case *ssa.FieldAddr:
		b.deallocEscapeValue(v.X)
	case *ssa.IndexAddr:
		b.deallocEscapeValue(v.X)
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if alloc, ok := edge.(*ssa.Alloc); ok && b.deallocAllocs[alloc] {
				b.deallocEmitEscape(b.getValue(v, v.Pos()))
				return
			}
		}
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			b.deallocEscapeValue(v.X)
		}
	}
}

// deallocAddrIsHeap returns true if addr is derived from a tracked
// heap allocation (FieldAddr/IndexAddr chain rooted at a tracked Alloc).
func (b *builder) deallocAddrIsHeap(addr ssa.Value) bool {
	return b.deallocAddrIsHeapSeen(addr, make(map[ssa.Value]bool))
}

func (b *builder) deallocAddrIsHeapSeen(addr ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[addr] {
		return false
	}
	seen[addr] = true
	switch a := addr.(type) {
	case *ssa.Alloc:
		return b.deallocAllocs[a]
	case *ssa.FieldAddr:
		return b.deallocAddrIsHeapSeen(a.X, seen)
	case *ssa.IndexAddr:
		return b.deallocAddrIsHeapSeen(a.X, seen)
	case *ssa.Phi:
		for _, edge := range a.Edges {
			if b.deallocAddrIsHeapSeen(edge, seen) {
				return true
			}
		}
	}
	return false
}

// deallocCheckStore emits deallocEscape when a heap-allocated value is
// stored into a global or into another heap-allocated object (the stored
// value's lifetime is no longer bounded by this function).
func (b *builder) deallocCheckStore(instr *ssa.Store) {
	if b.deallocHead.IsNil() {
		return
	}
	if _, ok := instr.Addr.(*ssa.Global); ok {
		b.deallocEscapeValue(instr.Val)
		return
	}
	if b.deallocAddrIsHeap(instr.Addr) {
		b.deallocEscapeValue(instr.Val)
	}
}

// createFreeBuiltin handles the free(x) builtin. Rejects freeing
// parameters or return values (caller-owned). Emits runtime.free and
// removes the pointer from the shadow list.
// Accepts pointers (*T, unsafe.Pointer) and slices/strings (extracts
// the backing data pointer at compile time).
func (b *builder) createFreeBuiltin(instr *ssa.CallCommon) (llvm.Value, error) {
	if len(instr.Args) != 1 {
		b.addError(instr.Pos(), "free requires exactly one argument")
		return llvm.Value{}, nil
	}
	arg := instr.Args[0]
	if derivesFromParam(arg, b.fn) {
		b.addError(instr.Pos(), "free: cannot free parameter (caller-owned)")
		return llvm.Value{}, nil
	}
	ptr := b.getValue(arg, instr.Pos())
	if ptr.Type().TypeKind() == llvm.StructTypeKind {
		ptr = b.CreateExtractValue(ptr, 0, "free.data")
	}
	if ptr.Type().TypeKind() != llvm.PointerTypeKind {
		b.addError(instr.Pos(), fmt.Sprintf("free: argument must be a pointer or slice, got %s", arg.Type()))
		return llvm.Value{}, nil
	}
	b.createRuntimeCall("free", []llvm.Value{ptr}, "")
	if !b.deallocHead.IsNil() {
		b.deallocEmitEscape(ptr)
	}
	return llvm.Value{}, nil
}

// derivesFromParam returns true if val is a function parameter or is
// derived from one via field/index addressing. Freeing such values is
// a compile error because parameters are caller-owned.
func derivesFromParam(val ssa.Value, fn *ssa.Function) bool {
	switch v := val.(type) {
	case *ssa.Parameter:
		return true
	case *ssa.FieldAddr:
		return derivesFromParam(v.X, fn)
	case *ssa.IndexAddr:
		return derivesFromParam(v.X, fn)
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			return derivesFromParam(v.X, fn)
		}
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if derivesFromParam(edge, fn) {
				return true
			}
		}
	}
	return false
}
