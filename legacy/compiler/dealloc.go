package compiler

import (
	"fmt"
	"go/types"
	"os"
	"sort"
	"strings"

	"moxie/compiler/llvmutil"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// sortedScopeKeys returns the keys of scopeEntries sorted by SSA name
// for deterministic codegen.
func sortedScopeKeys(m map[ssa.Value]*scopeEntry) []ssa.Value {
	keys := make([]ssa.Value, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Name() < keys[j].Name()
	})
	return keys
}

// sortedIntKeys returns sorted keys from a map[int]... for deterministic iteration.
func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

var scopeFreeDebug = func() bool {
	v := os.Getenv("SCOPE_FREE_DEBUG")
	if v != "" {
		fmt.Fprintf(os.Stderr, "[scope-free] debug enabled (SCOPE_FREE_DEBUG=%s)\n", v)
	}
	return v != ""
}()

// Compile-time scope-based free() emission.
//
// Every heap allocation is owned by a scope (SSA basic block index).
// Ownership is tracked in a bidirectional map:
//   - allocation -> owning block index (scopeOwnership)
//   - block index -> list of owned allocations (built at emission time)
//
// Ownership only moves UPWARD in the dominator tree (toward the entry
// block). An allocation in block B stored into a variable from block A
// (where A dominates B) promotes to block A. This is monotonic: values
// never move downward. The fixpoint converges because each promotion
// strictly reduces the dominator depth.
//
// Escape means "this function doesn't free it". Escaped allocations
// have no alloca and no free.

// scopeOwnership tracks compile-time ownership of a heap allocation.
type scopeOwnership struct {
	blockIdx int  // owning SSA basic block index
	escaped  bool // true: not freed by this function
}

// scopeEntry tracks a single heap allocation for scope-based free emission.
type scopeEntry struct {
	ptrAlloca llvm.Value       // entry-block alloca holding the heap pointer
	ownership *scopeOwnership  // compile-time ownership
}

// scopeFreeInit initializes per-scope free tracking for the function.
func (b *builder) scopeFreeInit() {
	if scopeFreeDebug {
		fmt.Fprintf(os.Stderr, "[scope-free-enter] %s\n", b.fn.String())
	}
	b.scopeEntries = make(map[ssa.Value]*scopeEntry)
	b.scopeLoops = computeLoopMembership(b.fn)
	b.scopeAssignments = computeScopeOwnership(b.fn, b.scopeLoops)
	b.scopeEdgeSplits = nil

	if scopeFreeDebug {
		fmt.Fprintf(os.Stderr, "[scope-free-init] %s: blocks=%d loops=%d assignments=%d\n",
			b.fn.String(), len(b.fn.Blocks), len(b.scopeLoops), len(b.scopeAssignments))
	}
}

// scopeTrackAlloc registers a heap allocation for scope-based free emission.
func (b *builder) scopeTrackAlloc(ptr llvm.Value, allocInstr ssa.Value) {
	if b.scopeEntries == nil {
		return
	}
	ownership := b.scopeAssignments[allocInstr]
	if ownership == nil {
		if instr, ok := allocInstr.(ssa.Instruction); ok && instr.Block() != nil {
			ownership = &scopeOwnership{blockIdx: instr.Block().Index}
		} else {
			ownership = &scopeOwnership{escaped: true}
		}
	}
	if ownership.escaped {
		return
	}

	ptrAlloca := llvmutil.CreateEntryBlockAlloca(b.Builder, b.dataPtrType,
		fmt.Sprintf("scope.%s", allocInstr.Name()))
	b.CreateStore(ptr, ptrAlloca)

	b.scopeEntries[allocInstr] = &scopeEntry{
		ptrAlloca: ptrAlloca,
		ownership: ownership,
	}
}

// --- Scope exit emission ---

// scopeEmitJumpFree emits frees for allocations whose scope dies on a
// Jump edge (single successor).
func (b *builder) scopeEmitJumpFree(block *ssa.BasicBlock) {
	succ := block.Succs[0]
	dying := b.computeDyingScopes(block, succ)
	b.scopeEmitFrees(dying)
}

// scopeEmitIfFree handles scope exits for an If terminator.
func (b *builder) scopeEmitIfFree(block *ssa.BasicBlock) (thenBlock, elseBlock llvm.BasicBlock) {
	ssaThen := block.Succs[0]
	ssaElse := block.Succs[1]
	thenBlock = b.blockInfo[ssaThen.Index].entry
	elseBlock = b.blockInfo[ssaElse.Index].entry

	dyingThen := b.computeDyingScopes(block, ssaThen)
	dyingElse := b.computeDyingScopes(block, ssaElse)

	if len(dyingThen) == 0 && len(dyingElse) == 0 {
		return
	}
	if sameIntSets(dyingThen, dyingElse) {
		b.scopeEmitFrees(dyingThen)
		return
	}

	hasThenFrees := b.hasScopeEntries(dyingThen)
	hasElseFrees := b.hasScopeEntries(dyingElse)
	if !hasThenFrees && !hasElseFrees {
		return
	}

	savedBlock := b.GetInsertBlock()
	if hasThenFrees {
		cleanup := b.ctx.AddBasicBlock(b.llvmFn,
			fmt.Sprintf("scope.cleanup.%d.%d", block.Index, ssaThen.Index))
		b.SetInsertPointAtEnd(cleanup)
		b.scopeEmitFrees(dyingThen)
		b.CreateBr(thenBlock)
		thenBlock = cleanup
		if b.scopeEdgeSplits == nil {
			b.scopeEdgeSplits = make(map[[2]int]llvm.BasicBlock)
		}
		b.scopeEdgeSplits[[2]int{block.Index, ssaThen.Index}] = cleanup
	}
	if hasElseFrees {
		cleanup := b.ctx.AddBasicBlock(b.llvmFn,
			fmt.Sprintf("scope.cleanup.%d.%d", block.Index, ssaElse.Index))
		b.SetInsertPointAtEnd(cleanup)
		b.scopeEmitFrees(dyingElse)
		b.CreateBr(elseBlock)
		elseBlock = cleanup
		if b.scopeEdgeSplits == nil {
			b.scopeEdgeSplits = make(map[[2]int]llvm.BasicBlock)
		}
		b.scopeEdgeSplits[[2]int{block.Index, ssaElse.Index}] = cleanup
	}
	b.SetInsertPointAtEnd(savedBlock)
	return
}

// scopeEmitReturnFree frees all non-escaped allocations at function return.
func (b *builder) scopeEmitReturnFree() {
	for _, key := range sortedScopeKeys(b.scopeEntries) {
		entry := b.scopeEntries[key]
		if entry.ownership.escaped {
			continue
		}
		ptr := b.CreateLoad(b.dataPtrType, entry.ptrAlloca, "scope.ret.free")
		b.createRuntimeCall("free", []llvm.Value{ptr}, "")
	}
}

// hasScopeEntries returns true if any tracked allocation is owned by a dying block.
func (b *builder) hasScopeEntries(dying map[int]bool) bool {
	for _, entry := range b.scopeEntries {
		if entry.ownership.escaped {
			continue
		}
		if dying[entry.ownership.blockIdx] {
			return true
		}
	}
	return false
}

// scopeEmitFrees emits free calls for all allocations owned by dying blocks.
func (b *builder) scopeEmitFrees(dying map[int]bool) {
	if len(dying) == 0 {
		return
	}
	for _, key := range sortedScopeKeys(b.scopeEntries) {
		entry := b.scopeEntries[key]
		if entry.ownership.escaped {
			continue
		}
		if dying[entry.ownership.blockIdx] {
			ptr := b.CreateLoad(b.dataPtrType, entry.ptrAlloca, "scope.exit.free")
			b.createRuntimeCall("free", []llvm.Value{ptr}, "")
			b.CreateStore(llvm.ConstNull(b.dataPtrType), entry.ptrAlloca)
		}
	}
}

// createFreeBuiltin handles explicit free(x).
// For map types, calls runtime.hashmapDeepFree to free both the hashmap
// header and its internal bucket array. For other types, calls runtime.free.
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
	// Use hashmapDeepFree for map types to free both header and buckets.
	if _, isMap := arg.Type().Underlying().(*types.Map); isMap {
		b.createRuntimeCall("hashmapDeepFree", []llvm.Value{ptr}, "")
	} else {
		b.createRuntimeCall("free", []llvm.Value{ptr}, "")
	}
	if entry, ok := b.scopeEntries[arg]; ok {
		b.CreateStore(llvm.ConstNull(b.dataPtrType), entry.ptrAlloca)
	}
	return llvm.Value{}, nil
}

// --- Dying scope computation ---

func (b *builder) computeDyingScopes(src, dst *ssa.BasicBlock) map[int]bool {
	dying := map[int]bool{}
	isBackEdge := dst != src && blockDominates(dst, src)
	for blk := src; blk != nil; blk = blk.Idom() {
		if blockDominates(blk, dst) {
			if isBackEdge && blk == dst {
				dying[blk.Index] = true
			}
			break
		}
		dying[blk.Index] = true
	}
	for _, headerIdx := range sortedIntKeys(b.scopeLoops) {
		members := b.scopeLoops[headerIdx]
		if members[src.Index] && !members[dst.Index] {
			for _, blockIdx := range sortedIntKeys(members) {
				_ = members[blockIdx]
				dying[blockIdx] = true
			}
		}
	}
	return dying
}

// --- Loop membership ---

func computeLoopMembership(fn *ssa.Function) map[int]map[int]bool {
	loops := make(map[int]map[int]bool)
	for _, block := range fn.Blocks {
		for _, succ := range block.Succs {
			if blockDominates(succ, block) {
				header := succ
				loop := loops[header.Index]
				if loop == nil {
					loop = map[int]bool{header.Index: true}
					loops[header.Index] = loop
				}
				addLoopBody(block, header, loop)
			}
		}
	}
	return loops
}

func addLoopBody(block, header *ssa.BasicBlock, loop map[int]bool) {
	if loop[block.Index] {
		return
	}
	loop[block.Index] = true
	for _, pred := range block.Preds {
		addLoopBody(pred, header, loop)
	}
}

// --- Scope ownership analysis ---
//
// The analysis uses a bidirectional map:
//   Forward:  ssa.Value -> *scopeOwnership  (which block owns this alloc)
//   Reverse:  computed on demand from forward map
//
// Ownership is determined by walking every USE of every tracked allocation.
// If any use is in a block that the allocation's current owner does NOT
// dominate, the allocation is promoted to a block that dominates both.
// If a use escapes (return, call arg, global store, etc.), the allocation
// is marked escaped.
//
// This replaces the old approach of chasing stores through the SSA def
// chain. Instead we look at where values are USED, which catches all
// patterns: phi merges, named returns, loads through pointers, etc.

func computeScopeOwnership(fn *ssa.Function, loops map[int]map[int]bool) map[ssa.Value]*scopeOwnership {
	assignments := make(map[ssa.Value]*scopeOwnership)

	// Phase 1: Initialize tracked heap allocations.
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			switch v := instr.(type) {
			case *ssa.Alloc:
				// Track ALL allocs, not just v.Heap. Codegen forces all
				// allocs to heap (compiler.go:2219). Without this, large
				// allocs (size > MaxStackAlloc) bypass escape analysis but
				// still get scope-freed via scopeTrackAlloc fallback.
				assignments[v] = &scopeOwnership{blockIdx: block.Index}
			case *ssa.MakeSlice:
				assignments[v] = &scopeOwnership{blockIdx: block.Index}
			case *ssa.MakeMap:
				assignments[v] = &scopeOwnership{blockIdx: block.Index}
			case *ssa.Call:
				// Track results of known allocating functions so their
				// heap memory is freed at scope exit. Without this,
				// bytesConcat (borrow pipe) results leak permanently.
				if fn := v.Common().StaticCallee(); fn != nil {
					name := fn.Name()
					if strings.HasPrefix(name, "__moxie_concat") ||
						name == "bytesConcat" || name == "stringConcat" {
						assignments[v] = &scopeOwnership{blockIdx: block.Index}
					}
				}
			}
		}
	}

	if len(assignments) == 0 {
		return assignments
	}

	// Phase 2: Escape analysis. An allocation escapes if ANY of its
	// uses (direct or through FieldAddr/IndexAddr/Store/Load chains)
	// are outside its defining block, OR if any use is an escape point
	// (call arg, return, global store, etc.).
	//
	// Conservative: if an alloc has ANY referrer outside its own block,
	// or if it's stored anywhere, mark it escaped. This is safe
	// (never frees too early) and can be refined later.
	seen := make(map[ssa.Value]bool)
	for val, own := range assignments {
		if own.escaped {
			continue
		}
		instr, ok := val.(ssa.Instruction)
		if !ok || instr.Block() == nil {
			own.escaped = true
			continue
		}
		defBlock := instr.Block()
		for k := range seen {
			delete(seen, k)
		}
		if allocEscapes(val, defBlock, seen) {
			own.escaped = true
		}
	}

	return assignments
}

// allocEscapes checks if val is used outside its defining block or in any
// way that could cause its memory to be accessed after scope exit. This is
// conservative: it returns true (escaped) whenever in doubt.
//
// An allocation is safe to free at scope exit only if ALL of these hold:
//   - Every use is in the same block as the allocation
//   - No use is a Store (stored values can be loaded later from any block)
//   - No use is a Call argument, Return, Send, MapUpdate, etc.
//   - No derived value (FieldAddr/IndexAddr) escapes
func allocEscapes(val ssa.Value, defBlock *ssa.BasicBlock, seen map[ssa.Value]bool) bool {
	if seen[val] {
		return false
	}
	seen[val] = true
	refs := val.Referrers()
	if refs == nil {
		return false
	}
	for _, ref := range *refs {
		if _, isDebug := ref.(*ssa.DebugRef); isDebug {
			continue
		}
		ri, ok := ref.(ssa.Instruction)
		if !ok {
			return true // unknown referrer type, be safe
		}
		// Any use outside the defining block escapes.
		if ri.Block() != defBlock {
			return true
		}
		// Specific instruction types that cause escape even in same block.
		switch r := ref.(type) {
		case *ssa.Store:
			// Stored somewhere - could be loaded from any block later.
			// Exception: storing INTO the allocation (FieldAddr of this
			// alloc as addr) is fine. But storing the alloc AS a value
			// into something else means it escapes - UNLESS the target
			// is a local alloca in the same block (dies at scope exit).
			if r.Val == val {
				if !isLocalAlloca(r.Addr, defBlock) {
					return true
				}
			}
			// r.Addr == val or derived from val: someone is writing INTO
			// our alloc's fields. The alloc itself doesn't escape from this.
		case *ssa.Call:
			return true
		case *ssa.Return:
			return true
		case *ssa.Defer:
			return true
		case *ssa.Send:
			return true
		case *ssa.Go:
			return true
		case *ssa.Panic:
			return true
		case *ssa.MapUpdate:
			return true
		case *ssa.MakeInterface:
			return true
		case *ssa.MakeClosure:
			// Moxie bans closure captures, so MakeClosure should have
			// no bindings. If it does, treat as escape for safety.
			return true
		case *ssa.Phi:
			return true // phi = cross-block flow
		case *ssa.FieldAddr:
			// Taking address of a field - check the field's uses.
			if allocEscapes(r, defBlock, seen) {
				return true
			}
		case *ssa.IndexAddr:
			if allocEscapes(r, defBlock, seen) {
				return true
			}
		case *ssa.Slice:
			if allocEscapes(r, defBlock, seen) {
				return true
			}
		case *ssa.UnOp:
			// Load (*ptr) result is a copy for value types, but for
			// pointer types (map, slice, chan, ptr) the loaded value
			// shares the underlying data. Be conservative: escape.
			if allocEscapes(r, defBlock, seen) {
				return true
			}
		}
	}
	return false
}

// isLocalAlloca returns true if addr is a stack-local alloca (Alloc with
// Heap=false) in the given block. Values stored into local allocas die at
// scope exit, so they don't escape.
func isLocalAlloca(addr ssa.Value, defBlock *ssa.BasicBlock) bool {
	alloc, ok := addr.(*ssa.Alloc)
	if !ok {
		return false
	}
	if alloc.Heap {
		return false // heap alloc outlives the scope
	}
	if instr, ok := addr.(ssa.Instruction); ok {
		if instr.Block() == defBlock {
			return true
		}
	}
	return false
}

// markEscaped traces backward through the SSA def chain from an escape
// point (call arg, return value, global store, etc.) to find tracked
// heap allocations and mark them escaped. Also traces through loads
// (UnOp *) to find allocations stored into the loaded address.
func markEscaped(val ssa.Value, assignments map[ssa.Value]*scopeOwnership, fn *ssa.Function, seen map[ssa.Value]bool) {
	if seen[val] {
		return
	}
	seen[val] = true

	// Mark this value if it's a tracked allocation.
	if o, ok := assignments[val]; ok {
		o.escaped = true
	}

	// Trace backward through SSA definitions.
	switch v := val.(type) {
	case *ssa.FieldAddr:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.IndexAddr:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.MakeInterface:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.TypeAssert:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.ChangeInterface:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.Slice:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.Convert:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.ChangeType:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.MultiConvert:
		markEscaped(v.X, assignments, fn, seen)
	case *ssa.MakeClosure:
		for _, binding := range v.Bindings {
			markEscaped(binding, assignments, fn, seen)
		}
	case *ssa.Phi:
		for _, edge := range v.Edges {
			markEscaped(edge, assignments, fn, seen)
		}
	case *ssa.Extract:
		markEscaped(v.Tuple, assignments, fn, seen)
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			// Load through pointer. The loaded value may be a
			// map/slice/pointer that was stored into the address.
			// Trace to the address AND find all stores to that
			// address to escape their values.
			markEscaped(v.X, assignments, fn, seen)
			escapeStoresInto(v.X, assignments, fn, seen)
		}
	}
}

// escapeStoresInto finds all Store instructions that write to addr
// and marks their stored values as escaped. This handles the pattern:
//   t0 = local var; *t0 = makeMap(); t1 = *t0; foo(t1)
// where foo(t1) escapes t1, and we need to trace through the load
// to find that makeMap was stored into t0.
func escapeStoresInto(addr ssa.Value, assignments map[ssa.Value]*scopeOwnership, fn *ssa.Function, seen map[ssa.Value]bool) {
	refs := addr.Referrers()
	if refs == nil {
		return
	}
	for _, ref := range *refs {
		if store, ok := ref.(*ssa.Store); ok && store.Addr == addr {
			markEscaped(store.Val, assignments, fn, seen)
		}
	}
}

// promoteVal traces backward through val's SSA def chain and promotes
// each tracked allocation's ownership to targetBlock if targetBlock
// is a dominator ancestor. Monotonic upward only.
func promoteVal(val ssa.Value, targetBlock int, assignments map[ssa.Value]*scopeOwnership, fn *ssa.Function, seen map[ssa.Value]bool) bool {
	if seen[val] {
		return false
	}
	seen[val] = true
	changed := false

	if o, ok := assignments[val]; ok && !o.escaped && targetBlock != o.blockIdx {
		targetBB := fn.Blocks[targetBlock]
		currentBB := fn.Blocks[o.blockIdx]
		if blockDominates(targetBB, currentBB) {
			o.blockIdx = targetBlock
			changed = true
		} else if !blockDominates(currentBB, targetBB) {
			cd := commonDom(targetBB, currentBB)
			if cd != nil && cd.Index != o.blockIdx {
				o.blockIdx = cd.Index
				changed = true
			}
		}
	}

	switch v := val.(type) {
	case *ssa.FieldAddr:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.IndexAddr:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.Slice:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.Phi:
		for _, edge := range v.Edges {
			if promoteVal(edge, targetBlock, assignments, fn, seen) { changed = true }
		}
	case *ssa.MakeInterface:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.TypeAssert:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.ChangeInterface:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.Convert:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.ChangeType:
		if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.Extract:
		if promoteVal(v.Tuple, targetBlock, assignments, fn, seen) { changed = true }
	case *ssa.UnOp:
		if v.Op.String() == "*" {
			if promoteVal(v.X, targetBlock, assignments, fn, seen) { changed = true }
			// Also promote values stored into the loaded address.
			promoteStoresInto(v.X, targetBlock, assignments, fn, seen)
		}
	}
	return changed
}

// promoteStoresInto promotes values stored into addr to targetBlock.
func promoteStoresInto(addr ssa.Value, targetBlock int, assignments map[ssa.Value]*scopeOwnership, fn *ssa.Function, seen map[ssa.Value]bool) {
	refs := addr.Referrers()
	if refs == nil {
		return
	}
	for _, ref := range *refs {
		if store, ok := ref.(*ssa.Store); ok && store.Addr == addr {
			promoteVal(store.Val, targetBlock, assignments, fn, seen)
		}
	}
}

// processAllocUses examines every referrer of val and determines if the
// allocation needs to be promoted or escaped. Returns true if ownership changed.
func processAllocUses(val ssa.Value, own *scopeOwnership, assignments map[ssa.Value]*scopeOwnership, fn *ssa.Function, seen map[ssa.Value]bool) bool {
	if seen[val] {
		return false
	}
	seen[val] = true
	refs := val.Referrers()
	if refs == nil {
		return false
	}
	changed := false
	for _, ref := range *refs {
		if own.escaped {
			break
		}
		switch r := ref.(type) {
		case *ssa.Return:
			// Returned from function - caller's problem.
			own.escaped = true
			changed = true

		case *ssa.Call:
			// Passed as argument to a call - escapes.
			own.escaped = true
			changed = true

		case *ssa.Defer:
			own.escaped = true
			changed = true

		case *ssa.Send:
			own.escaped = true
			changed = true

		case *ssa.Panic:
			own.escaped = true
			changed = true

		case *ssa.Go:
			own.escaped = true
			changed = true

		case *ssa.MapUpdate:
			// Value or key stored into a map - escapes (map may outlive).
			own.escaped = true
			changed = true

		case *ssa.MakeInterface:
			// Boxed into interface - escapes.
			own.escaped = true
			changed = true

		case *ssa.MakeClosure:
			// Captured by closure - escapes.
			own.escaped = true
			changed = true

		case *ssa.Store:
			// Stored somewhere. Where?
			if isGlobalDerived(r.Addr) || isParamDerived(r.Addr, make(map[ssa.Value]bool)) {
				own.escaped = true
				changed = true
			} else {
				// Stored into a local allocation. Promote to the
				// store target's owning block.
				targetBlock := findOwningBlock(r.Addr, assignments, fn)
				if targetBlock >= 0 && targetBlock != own.blockIdx {
					targetBB := fn.Blocks[targetBlock]
					currentBB := fn.Blocks[own.blockIdx]
					if blockDominates(targetBB, currentBB) {
						own.blockIdx = targetBlock
						changed = true
					} else if !blockDominates(currentBB, targetBB) {
						// Neither dominates - promote to common dominator.
						cd := commonDom(targetBB, currentBB)
						if cd != nil && cd.Index != own.blockIdx {
							own.blockIdx = cd.Index
							changed = true
						}
					}
				}
			}

		case *ssa.FieldAddr:
			// Taking address of a field - the result is derived from
			// this allocation. Check the FieldAddr's own referrers
			// for escape/promotion.
			if processAllocUses(r, own, assignments, fn, seen) {
				changed = true
			}

		case *ssa.IndexAddr:
			if processAllocUses(r, own, assignments, fn, seen) {
				changed = true
			}

		case *ssa.Slice:
			if processAllocUses(r, own, assignments, fn, seen) {
				changed = true
			}

		case *ssa.Phi:
			// Value flows through a phi - promote to phi's block
			// (the merge point).
			phiBlock := r.Block()
			if phiBlock != nil && phiBlock.Index != own.blockIdx {
				phiBB := fn.Blocks[phiBlock.Index]
				currentBB := fn.Blocks[own.blockIdx]
				if blockDominates(phiBB, currentBB) {
					own.blockIdx = phiBlock.Index
					changed = true
				} else if !blockDominates(currentBB, phiBB) {
					cd := commonDom(phiBB, currentBB)
					if cd != nil && cd.Index != own.blockIdx {
						own.blockIdx = cd.Index
						changed = true
					}
				}
			}
			// Also check phi's referrers.
			if processAllocUses(r, own, assignments, fn, seen) {
				changed = true
			}

		case *ssa.UnOp:
			// Load (*ptr) or other unary - check uses of the result.
			if processAllocUses(r, own, assignments, fn, seen) {
				changed = true
			}

		case *ssa.ChangeType, *ssa.Convert, *ssa.TypeAssert,
			*ssa.ChangeInterface, *ssa.MultiConvert:
			// Type conversion - check uses of the result.
			if rv, ok := r.(ssa.Value); ok {
				if processAllocUses(rv, own, assignments, fn, seen) {
					changed = true
				}
			}

		case *ssa.Extract:
			if processAllocUses(r, own, assignments, fn, seen) {
				changed = true
			}

		case *ssa.DebugRef:
			// ignore

		case *ssa.BinOp:
			// Pointer comparison etc. - doesn't escape.

		case *ssa.If:
			// Used as condition - doesn't escape.

		case *ssa.Lookup:
			// Map/string lookup - doesn't escape the container.
		}
	}
	return changed
}

// findOwningBlock determines which block owns the allocation that addr
// points into. Returns the block index, or -1 if unknown.
func findOwningBlock(addr ssa.Value, assignments map[ssa.Value]*scopeOwnership, fn *ssa.Function) int {
	root := findRootAlloc(addr)
	if root == nil {
		return -1
	}
	if own, ok := assignments[root]; ok {
		return own.blockIdx
	}
	// Stack alloc - return its defining block.
	if instr, ok := root.(ssa.Instruction); ok && instr.Block() != nil {
		return instr.Block().Index
	}
	return -1
}

// commonDom returns the nearest common dominator of a and b.
func commonDom(a, b *ssa.BasicBlock) *ssa.BasicBlock {
	da := domDepth(a)
	db := domDepth(b)
	for da > db {
		a = a.Idom()
		da--
	}
	for db > da {
		b = b.Idom()
		db--
	}
	for a != b {
		a = a.Idom()
		b = b.Idom()
		if a == nil || b == nil {
			return nil
		}
	}
	return a
}

func domDepth(b *ssa.BasicBlock) int {
	d := 0
	for x := b; x.Idom() != nil; x = x.Idom() {
		d++
	}
	return d
}

// --- Helpers ---

func blockDominates(h, b *ssa.BasicBlock) bool {
	for x := b; x != nil; x = x.Idom() {
		if x == h {
			return true
		}
	}
	return false
}

// findRootAlloc traces through FieldAddr/IndexAddr to find the root allocation.
func findRootAlloc(addr ssa.Value) ssa.Value {
	switch a := addr.(type) {
	case *ssa.Alloc:
		return a
	case *ssa.Call:
		return a
	case *ssa.FieldAddr:
		return findRootAlloc(a.X)
	case *ssa.IndexAddr:
		return findRootAlloc(a.X)
	}
	return nil
}

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

func sameIntSets(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
