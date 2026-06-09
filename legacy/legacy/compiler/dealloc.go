package compiler

import (
	"go/types"

	"moxie/compiler/llvmutil"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// usesDealloc returns true when the dealloc GC is active and the current
// function is in a user package (not stdlib). Stdlib packages have complex
// internal escape patterns (sync.Pool, interface boxing) that the shadow
// list escape analysis cannot track.
func (b *builder) usesDealloc() bool {
	// Legacy compiler dealloc is disabled: the function-level shadow list
	// lacks proper escape tracking. The stage4 compiler uses scope-based
	// dealloc instead. The runtime's free() and scope functions still work;
	// only the legacy compiler's auto-generated deallocPush/deallocFree
	// calls are suppressed.
	return false
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
		// Escape the phi value itself (defined in the phi block which
		// dominates the return block), not individual edge values which
		// may be defined in non-dominating blocks (e.g., loop bodies).
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

// deallocAddrIsLocal returns true if addr traces back to a stack-only
// local alloc (not tracked by dealloc). Storing into a local is safe.
// Everything else (parameters, globals, heap allocs, call results,
// free vars) means the stored value potentially escapes this function.
func (b *builder) deallocAddrIsLocal(addr ssa.Value) bool {
	return b.deallocAddrIsLocalSeen(addr, make(map[ssa.Value]bool))
}

func (b *builder) deallocAddrIsLocalSeen(addr ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[addr] {
		return false
	}
	seen[addr] = true
	switch a := addr.(type) {
	case *ssa.Alloc:
		return !b.deallocAllocs[a]
	case *ssa.FieldAddr:
		return b.deallocAddrIsLocalSeen(a.X, seen)
	case *ssa.IndexAddr:
		return b.deallocAddrIsLocalSeen(a.X, seen)
	case *ssa.Phi:
		for _, edge := range a.Edges {
			if !b.deallocAddrIsLocalSeen(edge, seen) {
				return false
			}
		}
		return len(a.Edges) > 0
	}
	return false
}

// deallocCheckStore emits deallocEscape when a tracked value is stored
// into any address that is not a proven-local stack allocation. Parameters,
// globals, heap allocs, call results, and free vars all mean the value's
// lifetime extends beyond this function.
func (b *builder) deallocCheckStore(instr *ssa.Store) {
	if b.deallocHead.IsNil() {
		return
	}
	if !b.deallocAddrIsLocal(instr.Addr) {
		b.deallocEscapeValue(instr.Val)
	}
}

// deallocCheckCall escapes any tracked allocation passed as an argument
// to a function call. Without full escape analysis, any value passed to
// an external function could be stored in a long-lived data structure.
func (b *builder) deallocCheckCall(common *ssa.CallCommon) {
	if b.deallocHead.IsNil() {
		return
	}
	for _, arg := range common.Args {
		b.deallocEscapeValue(arg)
	}
	if common.Value != nil {
		b.deallocEscapeValue(common.Value)
	}
}

// createFreeCall handles the Free(x) builtin. Emits runtime.free and
// removes x from the shadow list.
func (b *builder) createFreeCall(args []llvm.Value, argTypes []types.Type) {
	if len(args) != 1 {
		return
	}
	ptr := args[0]
	if ptr.Type().TypeKind() != llvm.PointerTypeKind {
		return
	}
	b.createRuntimeCall("free", []llvm.Value{ptr}, "")
	if !b.deallocHead.IsNil() {
		b.deallocEmitEscape(ptr)
	}
}
