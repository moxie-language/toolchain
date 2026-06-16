package compiler

import (
	"fmt"
	"os"
	"strings"

	"moxie/compiler/llvmutil"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

var scopeFreeDebug = func() bool {
	v := os.Getenv("SCOPE_FREE_DEBUG")
	if v != "" {
		fmt.Fprintf(os.Stderr, "[scope-free] debug enabled (SCOPE_FREE_DEBUG=%s)\n", v)
	}
	return v != ""
}()

// Compile-time scope-based free() emission.
//
// The compiler analyzes each heap allocation's ownership at compile time
// and emits free() calls at the appropriate scope boundary - same
// heuristic a C programmer follows, automated.
//
// Each heap allocation is assigned to a scope:
//   scopeLocal    - freed at enclosing scope exit (loop iteration end or function return)
//   scopePromoted - loop-body alloc stored to function-scope var, freed at function return
//   scopeEscaped  - stored to global/param, returned, passed to call: caller's problem
//
// Each tracked allocation gets an entry-block alloca holding its pointer.
// Scope exit loads the alloca and calls free(). Explicit free(x) nulls the
// alloca so the scope-exit free is a no-op (prevents double-free).

const (
	scopeLocal    = 0
	scopePromoted = 1
	scopeEscaped  = 2
)

type scopeFreeEntry struct {
	ptrAlloca llvm.Value // entry-block alloca holding the heap pointer
	scope     int        // scopeLocal, scopePromoted, scopeEscaped
	inLoop    bool       // alloc site is inside a loop body
}

// scopeFreeInit sets up scope tracking for the function. Called at function
// entry during codegen - not a GC mode, just part of how the compiler
// generates code.
func (b *builder) scopeFreeInit() {
	b.scopeFreeEntries = make(map[ssa.Value]*scopeFreeEntry)
	b.scopeFreeLoopBlocks = make(map[int]bool)

	for _, block := range b.fn.Blocks {
		if isBackEdgeSource(block) {
			b.scopeFreeLoopBlocks[block.Index] = true
		}
	}

	b.scopeFreeAssignments = computeScopeAssignments(b.fn, b.scopeFreeLoopBlocks)

	if scopeFreeDebug {
		fnName := b.fn.String()
		fmt.Fprintf(os.Stderr, "[scope-free-init] %s: blocks=%d assignments=%d\n",
			fnName, len(b.fn.Blocks), len(b.scopeFreeAssignments))
		if strings.Contains(fnName, "declStmt") {
			b.fn.WriteTo(os.Stderr)
		}
	}
	if scopeFreeDebug && len(b.scopeFreeAssignments) > 0 {
		fnName := b.fn.String()
		for val, scope := range b.scopeFreeAssignments {
			scopeName := [3]string{"local", "promoted", "escaped"}[scope]
			fmt.Fprintf(os.Stderr, "[scope-free] %s: %s (%T) -> %s\n", fnName, val.Name(), val, scopeName)
		}
	}
}

// isBackEdgeSource returns true if block has a successor that dominates it.
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

// scopeTrackAlloc registers a heap allocation for scope-based free emission.
func (b *builder) scopeTrackAlloc(ptr llvm.Value, allocInstr ssa.Value) {
	if b.scopeFreeEntries == nil {
		return
	}
	scope := scopeLocal
	if s, ok := b.scopeFreeAssignments[allocInstr]; ok {
		scope = s
	}
	inLoop := false
	if allocInstr != nil {
		if instr, ok := allocInstr.(ssa.Instruction); ok {
			if block := instr.Block(); block != nil {
				inLoop = b.scopeFreeLoopBlocks[block.Index]
			}
		}
	}

	ptrAlloca := llvmutil.CreateEntryBlockAlloca(b.Builder, b.dataPtrType,
		fmt.Sprintf("scope.%s", allocInstr.Name()))
	b.CreateStore(ptr, ptrAlloca)

	b.scopeFreeEntries[allocInstr] = &scopeFreeEntry{
		ptrAlloca: ptrAlloca,
		scope:     scope,
		inLoop:    inLoop,
	}

	if scopeFreeDebug {
		scopeName := "local"
		if scope == scopePromoted {
			scopeName = "promoted"
		} else if scope == scopeEscaped {
			scopeName = "escaped"
		}
		fmt.Fprintf(os.Stderr, "[scope-free-track] %s: %s (%T) scope=%s inLoop=%v\n",
			b.fn.String(), allocInstr.Name(), allocInstr, scopeName, inLoop)
	}
}

// scopeEmitBlockFree frees loop-iteration-local allocations at the iteration
// boundary. Only fires for loop body blocks. After freeing, nulls the alloca
// so the next iteration starts clean.
func (b *builder) scopeEmitBlockFree(block *ssa.BasicBlock) {
	if !b.scopeFreeLoopBlocks[block.Index] {
		return
	}
	for _, entry := range b.scopeFreeEntries {
		if entry.inLoop && entry.scope == scopeLocal {
			ptr := b.CreateLoad(b.dataPtrType, entry.ptrAlloca, "scope.iter.free")
			b.createRuntimeCall("free", []llvm.Value{ptr}, "")
			b.CreateStore(llvm.ConstNull(b.dataPtrType), entry.ptrAlloca)
		}
	}
}

// scopeEmitReturnFree frees all function-owned allocations at return.
// Frees scopeLocal (function-scope, not loop) and scopePromoted (loop->function).
// Skips scopeEscaped (caller's problem) and scopeLocal+inLoop (already freed
// at iteration end).
func (b *builder) scopeEmitReturnFree() {
	for allocInstr, entry := range b.scopeFreeEntries {
		if entry.scope == scopeEscaped {
			continue
		}
		if entry.scope == scopeLocal && entry.inLoop {
			continue // already freed at loop iteration end
		}
		if scopeFreeDebug {
			scopeName := "local"
			if entry.scope == scopePromoted {
				scopeName = "promoted"
			}
			fmt.Fprintf(os.Stderr, "[scope-free-return] %s: freeing %s (%T) scope=%s\n",
				b.fn.String(), allocInstr.Name(), allocInstr, scopeName)
		}
		ptr := b.CreateLoad(b.dataPtrType, entry.ptrAlloca, "scope.ret.free")
		b.createRuntimeCall("free", []llvm.Value{ptr}, "")
	}
}

// createFreeBuiltin handles explicit free(x). Emits runtime.free and nulls
// the tracking alloca so scope-exit free is a no-op (prevents double-free).
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
	// Null the tracking alloca to prevent double-free at scope exit.
	if entry, ok := b.scopeFreeEntries[arg]; ok {
		b.CreateStore(llvm.ConstNull(b.dataPtrType), entry.ptrAlloca)
	}
	return llvm.Value{}, nil
}

// --- Scope assignment analysis (compile-time) ---

// computeScopeAssignments determines the owning scope for each heap allocation
// in fn. Structural: Store targets determine ownership.
func computeScopeAssignments(fn *ssa.Function, loopBlocks map[int]bool) map[ssa.Value]int {
	assignments := make(map[ssa.Value]int)

	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch i := instr.(type) {
			case *ssa.Return:
				for _, result := range i.Results {
					markScope(result, scopeEscaped, assignments, make(map[ssa.Value]bool))
				}
			case *ssa.Store:
				target := storeTargetScope(i, loopBlocks)
				if target > scopeLocal {
					markScope(i.Val, target, assignments, make(map[ssa.Value]bool))
				}
			case *ssa.MapUpdate:
				markScope(i.Key, scopeEscaped, assignments, make(map[ssa.Value]bool))
				markScope(i.Value, scopeEscaped, assignments, make(map[ssa.Value]bool))
			case *ssa.Defer:
				if i.Call.Value != nil {
					markScope(i.Call.Value, scopeEscaped, assignments, make(map[ssa.Value]bool))
				}
				for _, arg := range i.Call.Args {
					markScope(arg, scopeEscaped, assignments, make(map[ssa.Value]bool))
				}
			case *ssa.Call:
				if i.Call.StaticCallee() == nil {
					markScope(i.Call.Value, scopeEscaped, assignments, make(map[ssa.Value]bool))
				}
				for _, arg := range i.Call.Args {
					markScope(arg, scopeEscaped, assignments, make(map[ssa.Value]bool))
				}
			case *ssa.Send:
				markScope(i.X, scopeEscaped, assignments, make(map[ssa.Value]bool))
			case *ssa.Panic:
				markScope(i.X, scopeEscaped, assignments, make(map[ssa.Value]bool))
			}
		}
	}

	// Fixpoint: stores into fields of escaped/promoted allocs propagate.
	var stores []*ssa.Store
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if s, ok := instr.(*ssa.Store); ok {
				stores = append(stores, s)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, store := range stores {
			root := findRootAlloc(store.Addr)
			if root != nil && assignments[root] > scopeLocal {
				prev := len(assignments)
				markScope(store.Val, assignments[root], assignments, make(map[ssa.Value]bool))
				if len(assignments) > prev {
					changed = true
				}
			}
		}
	}

	return assignments
}

// storeTargetScope determines what scope a Store promotes its value to.
func storeTargetScope(store *ssa.Store, loopBlocks map[int]bool) int {
	addr := store.Addr
	if isGlobalDerived(addr) {
		return scopeEscaped
	}
	if isParamDerived(addr, make(map[ssa.Value]bool)) {
		return scopeEscaped
	}
	addrBlock := findAllocBlock(addr)
	if addrBlock != nil && addrBlock != store.Block() && blockDominates(addrBlock, store.Block()) {
		if loopBlocks[store.Block().Index] && !loopBlocks[addrBlock.Index] {
			return scopePromoted
		}
	}
	return scopeLocal
}

// markScope walks the SSA def chain to find heap allocations and assigns
// them to the given scope. Higher scope wins (escaped > promoted > local).
func markScope(val ssa.Value, scope int, assignments map[ssa.Value]int, seen map[ssa.Value]bool) {
	if seen[val] {
		return
	}
	seen[val] = true
	switch v := val.(type) {
	case *ssa.Alloc:
		if scope > assignments[v] {
			assignments[v] = scope
		}
	case *ssa.MakeSlice:
		if scope > assignments[v] {
			assignments[v] = scope
		}
	case *ssa.MakeMap:
		if scope > assignments[v] {
			assignments[v] = scope
		}
	case *ssa.FieldAddr:
		markScope(v.X, scope, assignments, seen)
	case *ssa.IndexAddr:
		markScope(v.X, scope, assignments, seen)
	case *ssa.MakeInterface:
		markScope(v.X, scope, assignments, seen)
	case *ssa.TypeAssert:
		markScope(v.X, scope, assignments, seen)
	case *ssa.ChangeInterface:
		markScope(v.X, scope, assignments, seen)
	case *ssa.Slice:
		markScope(v.X, scope, assignments, seen)
	case *ssa.Convert:
		markScope(v.X, scope, assignments, seen)
	case *ssa.ChangeType:
		markScope(v.X, scope, assignments, seen)
	case *ssa.MultiConvert:
		markScope(v.X, scope, assignments, seen)
	case *ssa.MakeClosure:
		for _, binding := range v.Bindings {
			markScope(binding, scope, assignments, seen)
		}
	case *ssa.Phi:
		for _, edge := range v.Edges {
			markScope(edge, scope, assignments, seen)
		}
	case *ssa.Extract:
		markScope(v.Tuple, scope, assignments, seen)
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			markScope(v.X, scope, assignments, seen)
		}
	case *ssa.Call:
		if scope > assignments[v] {
			assignments[v] = scope
		}
	}
}

// findRootAlloc traces through FieldAddr/IndexAddr to find the root Alloc.
func findRootAlloc(addr ssa.Value) ssa.Value {
	switch a := addr.(type) {
	case *ssa.Alloc:
		return a
	case *ssa.FieldAddr:
		return findRootAlloc(a.X)
	case *ssa.IndexAddr:
		return findRootAlloc(a.X)
	}
	return nil
}

// isGlobalDerived returns true if val traces back to a global variable.
func isGlobalDerived(val ssa.Value) bool {
	switch v := val.(type) {
	case *ssa.Global:
		return true
	case *ssa.FieldAddr:
		return isGlobalDerived(v.X)
	case *ssa.IndexAddr:
		return isGlobalDerived(v.X)
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			return isGlobalDerived(v.X)
		}
	}
	return false
}

// isParamDerived returns true if val traces back to a function parameter
// or closure free variable.
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
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			return isParamDerived(v.X, seen)
		}
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if isParamDerived(edge, seen) {
				return true
			}
		}
	}
	return false
}

// findAllocBlock finds which basic block contains the root Alloc for an address.
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
