package compiler

import (
	"fmt"
	"go/types"
	"strings"

	"moxie/compiler/llvmutil"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// Scope-based ownership promotion model.
//
// One rule: when a pointer (including slice/map fat pointers) is stored into
// a variable from an outer scope, the allocation's ownership promotes to that
// outer scope. Free happens when the owning scope closes.
//
// Scope nesting is derived from the SSA dominator tree. Loop bodies (blocks
// with back edges) get per-iteration scope save/restore.
//
// Escaping allocs (returned, stored to globals, stored cross-scope, stored to
// param-derived addresses) are detected in a pre-pass and marked escaped at
// their definition site, avoiding LLVM dominance violations.
//
// Runtime API (src/runtime/dealloc.mx):
//   scopePush(ptr, &head)           - link allocation into scope chain
//   scopeFreeTo(current, saved)     - free non-escaped, relink escaped into outer
//   scopeRelocate(ptr, head, saved) - set escaped bit on allocation

// usesDealloc returns true when scope-based dealloc is active for this
// function. Runtime/internal functions are excluded to prevent infinite
// recursion (scopePush/scopeFreeTo calling themselves).
func (b *builder) usesDealloc() bool {
	if b.fn.Pkg == nil {
		return false
	}
	pkg := b.fn.Pkg.Pkg.Path()
	if pkg == "runtime" || strings.HasPrefix(pkg, "internal/") {
		return false
	}
	return true
}

// scopeInit creates the scope chain head alloca, detects loop bodies,
// and pre-computes which allocs need escaping. Called during function setup.
func (b *builder) scopeInit() {
	b.scopeAllocs = make(map[ssa.Value]bool)
	b.scopeHead = llvmutil.CreateEntryBlockAlloca(b.Builder, b.dataPtrType, "scope.head")
	b.CreateStore(llvm.ConstNull(b.dataPtrType), b.scopeHead)

	// Detect loop body blocks and create save-point allocas.
	b.scopeLoopSaves = make(map[int]llvm.Value)
	for _, block := range b.fn.Blocks {
		if isBackEdgeSource(block) {
			save := llvmutil.CreateEntryBlockAlloca(b.Builder, b.dataPtrType,
				fmt.Sprintf("scope.save.%d", block.Index))
			b.CreateStore(llvm.ConstNull(b.dataPtrType), save)
			b.scopeLoopSaves[block.Index] = save
		}
	}

	// Pre-compute which allocs escape this function (returned, stored to
	// globals, stored cross-scope, stored to param-derived addresses).
	// These are marked escaped at their definition site to avoid LLVM
	// dominance violations from Phi-routed values.
	b.scopeEscaping = computeEscapingAllocs(b.fn)
}

// isBackEdgeSource returns true if block has a successor that dominates it
// (back edge), meaning this block is part of a loop body.
func isBackEdgeSource(block *ssa.BasicBlock) bool {
	for _, succ := range block.Succs {
		if blockDominates(succ, block) {
			return true
		}
	}
	return false
}

// blockDominates returns true if h dominates b.
func blockDominates(h, b *ssa.BasicBlock) bool {
	for x := b; x != nil; x = x.Idom() {
		if x == h {
			return true
		}
	}
	return false
}

// computeEscapingAllocs scans instructions in fn to find heap allocs
// whose values escape: returned, stored to globals/params/cross-scope,
// stored as map entries.
// Uses fixpoint iteration to transitively escape values stored into
// fields of already-escaping heap allocs.
//
// Call arguments are NOT marked as escaping. Passing a pointer to a
// function does not mean the pointer escapes - the callee handles its
// own scope tracking. Only values that escape at THIS function's level
// are marked.
func computeEscapingAllocs(fn *ssa.Function) map[ssa.Value]bool {
	escaping := make(map[ssa.Value]bool)
	var stores []*ssa.Store
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch i := instr.(type) {
			case *ssa.Return:
				for _, result := range i.Results {
					collectEscaping(result, escaping, make(map[ssa.Value]bool))
				}
			case *ssa.Store:
				stores = append(stores, i)
				shouldEscape := false
				if _, ok := i.Addr.(*ssa.Global); ok {
					shouldEscape = true
				} else if isParamDerived(i.Addr, make(map[ssa.Value]bool)) {
					shouldEscape = true
				} else {
					addrBlock := findAllocBlock(i.Addr)
					if addrBlock != nil && addrBlock != i.Block() && blockDominates(addrBlock, i.Block()) {
						shouldEscape = true
					}
				}
				if shouldEscape {
					collectEscaping(i.Val, escaping, make(map[ssa.Value]bool))
				}
			case *ssa.MapUpdate:
				collectEscaping(i.Key, escaping, make(map[ssa.Value]bool))
				collectEscaping(i.Value, escaping, make(map[ssa.Value]bool))
			case *ssa.Defer:
				for _, arg := range i.Call.Args {
					collectEscaping(arg, escaping, make(map[ssa.Value]bool))
				}
			}
		}
	}
	// Fixpoint: when a Store's address is a FieldAddr/IndexAddr of an
	// escaping heap alloc, the stored value must also escape. Iterate
	// until no new escaping allocs are discovered.
	for changed := true; changed; {
		changed = false
		for _, store := range stores {
			root := findRootAlloc(store.Addr)
			if root != nil && escaping[root] {
				prev := len(escaping)
				collectEscaping(store.Val, escaping, make(map[ssa.Value]bool))
				if len(escaping) > prev {
					changed = true
				}
			}
		}
	}
	return escaping
}

// findRootAlloc traces through FieldAddr/IndexAddr to find the root
// heap Alloc that an address derives from. Returns nil if the root
// is not a heap alloc.
func findRootAlloc(addr ssa.Value) ssa.Value {
	switch a := addr.(type) {
	case *ssa.Alloc:
		if a.Heap {
			return a
		}
	case *ssa.FieldAddr:
		return findRootAlloc(a.X)
	case *ssa.IndexAddr:
		return findRootAlloc(a.X)
	}
	return nil
}

// collectEscaping walks the SSA def chain to find heap allocations
// (Alloc, MakeSlice, MakeMap) and marks them in the escaping set.
// Follows Phi edges to find allocs in conditional branches.
func collectEscaping(val ssa.Value, escaping map[ssa.Value]bool, seen map[ssa.Value]bool) {
	if seen[val] {
		return
	}
	seen[val] = true
	switch v := val.(type) {
	case *ssa.Alloc:
		if v.Heap {
			escaping[v] = true
		}
	case *ssa.MakeSlice:
		escaping[v] = true
	case *ssa.MakeMap:
		escaping[v] = true
	case *ssa.FieldAddr:
		collectEscaping(v.X, escaping, seen)
	case *ssa.IndexAddr:
		collectEscaping(v.X, escaping, seen)
	case *ssa.MakeInterface:
		collectEscaping(v.X, escaping, seen)
	case *ssa.Slice:
		collectEscaping(v.X, escaping, seen)
	case *ssa.Convert:
		collectEscaping(v.X, escaping, seen)
	case *ssa.ChangeType:
		collectEscaping(v.X, escaping, seen)
	case *ssa.MultiConvert:
		collectEscaping(v.X, escaping, seen)
	case *ssa.Phi:
		for _, edge := range v.Edges {
			collectEscaping(edge, escaping, seen)
		}
	case *ssa.Extract:
		collectEscaping(v.Tuple, escaping, seen)
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			collectEscaping(v.X, escaping, seen)
		}
	case *ssa.Call:
		// A Call result that reaches an escaping context means the
		// call result itself needs escape marking at the push site.
		escaping[v] = true
	}
}

// isParamDerived returns true if val traces back to a function parameter
// or closure free variable (outer scope).
func isParamDerived(val ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[val] {
		return false
	}
	seen[val] = true
	switch v := val.(type) {
	case *ssa.Parameter:
		return true
	case *ssa.FreeVar:
		return true
	case *ssa.FieldAddr:
		return isParamDerived(v.X, seen)
	case *ssa.IndexAddr:
		return isParamDerived(v.X, seen)
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if isParamDerived(edge, seen) {
				return true
			}
		}
	}
	return false
}

// scopeEmitBlockSave saves the current scope head at the beginning of a
// loop body block. No-op if block is not a loop body.
func (b *builder) scopeEmitBlockSave(block *ssa.BasicBlock) {
	save, ok := b.scopeLoopSaves[block.Index]
	if !ok {
		return
	}
	head := b.CreateLoad(b.dataPtrType, b.scopeHead, "scope.save.load")
	b.CreateStore(head, save)
}

// scopeEmitBlockFree frees allocations from the current loop iteration.
// Called before terminators of loop body blocks. No-op if not a loop body.
//
// Stores saved (not scopeFreeTo's result) back to scopeHead. scopeFreeTo
// relinks escaped allocations into its return value but CLEARS their escaped
// bit. Storing the result back would re-expose those allocations to the next
// iteration's free pass, causing use-after-free. Escaped allocations survive
// because whatever they escaped into (map, outer var) holds a direct reference.
func (b *builder) scopeEmitBlockFree(block *ssa.BasicBlock) {
	save, ok := b.scopeLoopSaves[block.Index]
	if !ok {
		return
	}
	head := b.CreateLoad(b.dataPtrType, b.scopeHead, "scope.iter.head")
	saved := b.CreateLoad(b.dataPtrType, save, "scope.iter.saved")
	result := b.createRuntimeCall("scopeFreeTo", []llvm.Value{head, saved}, "scope.iter.freed")
	b.CreateStore(result, b.scopeHead)
}

// scopePushAlloc registers a heap allocation in the scope chain.
// If the alloc was pre-determined to escape, marks it escaped immediately.
func (b *builder) scopePushAlloc(ptr llvm.Value, allocInstr ssa.Value) {
	if b.scopeHead.IsNil() {
		return
	}
	b.createRuntimeCall("scopePush", []llvm.Value{ptr, b.scopeHead}, "")
	if allocInstr != nil {
		b.scopeAllocs[allocInstr] = true
		if b.scopeEscaping[allocInstr] {
			b.scopeMarkEscaped(ptr)
		}
	}
}

// scopePushCallResult pushes a function call result into the caller's
// scope chain if it contains a heap pointer (pointer, slice, or map type).
// This ensures allocations returned from callees are tracked and freed
// when the caller's scope ends.
func (b *builder) scopePushCallResult(val llvm.Value, callInstr *ssa.Call) {
	if b.scopeHead.IsNil() || val.IsNil() {
		return
	}
	// Skip builtins (append, copy, etc.) - their allocations are managed
	// by the runtime, not the scope chain. Pushing them causes the loop
	// iteration scope-free to free buffers still in use by the next iteration.
	if _, isBuiltin := callInstr.Common().Value.(*ssa.Builtin); isBuiltin {
		return
	}
	typ := callInstr.Type()
	ptr := b.extractPointerFromValue(val, typ)
	if ptr.IsNil() {
		return
	}
	b.createRuntimeCall("scopePush", []llvm.Value{ptr, b.scopeHead}, "")
	// Check if this call result escapes the current function.
	if b.scopeEscaping[callInstr] {
		b.scopeMarkEscaped(ptr)
	}
}

// extractPointerFromValue extracts the data pointer from an LLVM value
// based on its Go type. Returns nil if the type doesn't contain a pointer.
func (b *builder) extractPointerFromValue(val llvm.Value, typ types.Type) llvm.Value {
	switch typ.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan:
		return val
	case *types.Slice:
		// Extract the data pointer from the slice struct.
		return b.CreateExtractValue(val, 0, "scope.slice.ptr")
	case *types.Basic:
		if typ.Underlying().(*types.Basic).Kind() == types.UnsafePointer {
			return val
		}
	}
	return llvm.Value{}
}

// scopeEmitFreeAll frees all non-escaped allocations at function return.
func (b *builder) scopeEmitFreeAll() {
	if b.scopeHead.IsNil() {
		return
	}
	head := b.CreateLoad(b.dataPtrType, b.scopeHead, "scope.ret.head")
	nullPtr := llvm.ConstNull(b.dataPtrType)
	b.createRuntimeCall("scopeFreeTo", []llvm.Value{head, nullPtr}, "")
}

// scopeMarkEscaped sets the escaped bit on a heap allocation so
// scopeFreeTo promotes it to the outer scope instead of freeing.
func (b *builder) scopeMarkEscaped(ptr llvm.Value) {
	if b.scopeHead.IsNil() {
		return
	}
	nullPtr := llvm.ConstNull(b.dataPtrType)
	b.createRuntimeCall("scopeRelocate", []llvm.Value{ptr, b.scopeHead, nullPtr}, "")
}

// findAllocBlock finds which basic block contains the root Alloc for
// an address value.
func findAllocBlock(addr ssa.Value) *ssa.BasicBlock {
	switch a := addr.(type) {
	case *ssa.Alloc:
		return a.Block()
	case *ssa.FieldAddr:
		return findAllocBlock(a.X)
	case *ssa.IndexAddr:
		return findAllocBlock(a.X)
	case *ssa.Phi:
		return a.Block()
	}
	return nil
}

// createFreeBuiltin handles the free(x) builtin. Emits runtime.free and
// marks the allocation escaped so scopeFreeTo won't double-free.
func (b *builder) createFreeBuiltin(instr *ssa.CallCommon) (llvm.Value, error) {
	if len(instr.Args) != 1 {
		b.addError(instr.Pos(), "free requires exactly one argument")
		return llvm.Value{}, nil
	}
	arg := instr.Args[0]
	ptr := b.getValue(arg, instr.Pos())
	if ptr.Type().TypeKind() == llvm.StructTypeKind {
		ptr = b.CreateExtractValue(ptr, 0, "free.data")
	}
	if ptr.Type().TypeKind() != llvm.PointerTypeKind {
		b.addError(instr.Pos(), fmt.Sprintf("free: argument must be a pointer or slice, got %s", arg.Type()))
		return llvm.Value{}, nil
	}
	b.createRuntimeCall("free", []llvm.Value{ptr}, "")
	if !b.scopeHead.IsNil() {
		b.scopeMarkEscaped(ptr)
	}
	return llvm.Value{}, nil
}
