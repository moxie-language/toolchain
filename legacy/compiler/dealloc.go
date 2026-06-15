package compiler

import (
	"fmt"

	"moxie/compiler/llvmutil"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// usesDealloc returns true when the dealloc GC is active and this function
// should be instrumented. The dealloc runtime functions themselves are excluded
// to prevent infinite recursion (deallocFree calling deallocFree at its return).
func (b *builder) usesDealloc() bool {
	if b.GC != "dealloc" {
		return false
	}
	name := b.fn.RelString(nil)
	switch name {
	case "runtime.deallocFree", "runtime.deallocPush", "runtime.deallocEscape",
		"runtime.scopePush", "runtime.scopeFreeTo", "runtime.scopeRelocate",
		"runtime.Free", "runtime.free", "runtime.alloc":
		return false
	}
	return true
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
	case *ssa.Slice:
		b.deallocEscapeValue(v.X)
	case *ssa.Convert:
		b.deallocEscapeValue(v.X)
	case *ssa.ChangeType:
		b.deallocEscapeValue(v.X)
	case *ssa.MultiConvert:
		b.deallocEscapeValue(v.X)
	case *ssa.Extract:
		b.deallocEscapeValue(v.Tuple)
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
	case *ssa.Slice:
		return b.deallocAddrIsHeapSeen(a.X, seen)
	case *ssa.Convert:
		return b.deallocAddrIsHeapSeen(a.X, seen)
	case *ssa.ChangeType:
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

// deallocCheckCall emits deallocEscape for any tracked heap allocations
// passed as arguments to a function call. Since we cannot know whether the
// callee stores the pointer, we must conservatively assume it escapes.
func (b *builder) deallocCheckCall(instr *ssa.CallCommon) {
	if b.deallocHead.IsNil() {
		return
	}
	for _, arg := range instr.Args {
		b.deallocEscapeValue(arg)
	}
	// The receiver of a method call (instr.Value) can also carry a tracked
	// allocation (e.g. scope.Insert where scope is derived from a tracked
	// alloc via FieldAddr).
	if instr.Value != nil && !instr.IsInvoke() {
		b.deallocEscapeValue(instr.Value)
	}
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

// deallocCheckMapUpdate emits deallocEscape when a tracked heap allocation
// is stored as a map key or value. The map outlives the function scope.
func (b *builder) deallocCheckMapUpdate(instr *ssa.MapUpdate) {
	if b.deallocHead.IsNil() {
		return
	}
	b.deallocEscapeValue(instr.Key)
	b.deallocEscapeValue(instr.Value)
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
	// Moxie: free() on parameters is valid - ownership transfer is the
	// programmer's responsibility. No derivesFromParam check.
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

