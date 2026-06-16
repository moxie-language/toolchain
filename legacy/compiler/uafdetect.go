package compiler

// Compile-time use-after-free detection.
//
// Phase 1: Explicit free() tracking.
// For each free() call, find all subsequent uses of the freed value
// via dominator analysis and referrer walking.
//
// Phase 2: Implicit scope-exit tracking.
// Variables allocated in a scope are freed at scope exit unless escaped.
// Uses after scope exit are flagged.

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// checkUseAfterFree scans the function for uses of freed values.
//
// Phase 1: explicit free() tracking - implemented.
// Phase 2: implicit scope-exit tracking - legacy compiler is function-granular
// (free at return). Inner-scope allocations survive until function return.
// Stage4 compiler has per-block scope tracking.
func (b *builder) checkUseAfterFree() {
	if !isUserPackage(b.fn.Pkg) {
		return
	}
	b.checkExplicitFreeUAF()
}

// checkExplicitFreeUAF finds free() calls and checks that the freed
// value is not used after the free.
func (b *builder) checkExplicitFreeUAF() {
	for _, block := range b.fn.Blocks {
		for freeIdx, instr := range block.Instrs {
			call, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			bi, ok := call.Call.Value.(*ssa.Builtin)
			if !ok || bi.Name() != "free" {
				continue
			}
			if len(call.Call.Args) < 1 {
				continue
			}
			freedVal := call.Call.Args[0]

			// Collect all values derived from freedVal (the root allocation
			// and any FieldAddr/IndexAddr/Phi chains).
			freed := b.collectDerived(freedVal)

			// Build reachable-after set: all blocks reachable from this
			// block's successors.
			reachable := b.reachableAfter(block)

			// Check all referrers of each freed value.
			for val := range freed {
				refs := val.Referrers()
				if refs == nil {
					continue
				}
				for _, ref := range *refs {
					ri, ok := ref.(ssa.Instruction)
					if !ok {
						continue
					}
					// Skip the free call itself.
					if ri == instr {
						continue
					}
					// Skip DebugRef.
					if _, isDebug := ri.(*ssa.DebugRef); isDebug {
						continue
					}

					refBlock := ri.Block()
					isAfter := false
					if refBlock == block {
						// Same block: check instruction index.
						for j := freeIdx + 1; j < len(block.Instrs); j++ {
							if block.Instrs[j] == ri {
								isAfter = true
								break
							}
						}
						// If block has back-edge to itself, any use is reachable-after.
						if !isAfter && reachable[block] {
							isAfter = true
						}
					} else if reachable[refBlock] {
						isAfter = true
					}

					if !isAfter {
						continue
					}

					// Check if there's a reassignment between free and use.
					// If the same variable is assigned a new value, the use
					// is of the new value, not the freed one.
					if b.hasReassignBetween(val, block, freeIdx, ri) {
						continue
					}

					b.addError(ri.Pos(), "moxie: use after free - variable used after free() call")
				}
			}
		}
	}
}

// collectDerived returns a set of all SSA values derived from val
// (the value itself, plus FieldAddr, IndexAddr, UnOp * chains).
func (b *builder) collectDerived(val ssa.Value) map[ssa.Value]bool {
	result := map[ssa.Value]bool{val: true}
	work := []ssa.Value{val}
	for len(work) > 0 {
		v := work[0]
		work = work[1:]
		refs := v.Referrers()
		if refs == nil {
			continue
		}
		for _, ref := range *refs {
			derived, ok := ref.(ssa.Value)
			if !ok {
				continue
			}
			switch derived.(type) {
			case *ssa.FieldAddr, *ssa.IndexAddr:
				if !result[derived] {
					result[derived] = true
					work = append(work, derived)
				}
			case *ssa.UnOp:
				d := derived.(*ssa.UnOp)
				if d.Op == token.MUL {
					if !result[derived] {
						result[derived] = true
						work = append(work, derived)
					}
				}
			}
		}
	}
	return result
}

// reachableAfter returns all blocks reachable from block's successors.
func (b *builder) reachableAfter(block *ssa.BasicBlock) map[*ssa.BasicBlock]bool {
	reached := map[*ssa.BasicBlock]bool{}
	var work []*ssa.BasicBlock
	for _, succ := range block.Succs {
		if !reached[succ] {
			reached[succ] = true
			work = append(work, succ)
		}
	}
	for len(work) > 0 {
		bb := work[0]
		work = work[1:]
		for _, succ := range bb.Succs {
			if !reached[succ] {
				reached[succ] = true
				work = append(work, succ)
			}
		}
	}
	return reached
}

// hasReassignBetween checks whether val is reassigned (stored to)
// between the free at (block, freeIdx) and the use at ri.
// A Store to the same address means the use sees a new value.
func (b *builder) hasReassignBetween(val ssa.Value, freeBlock *ssa.BasicBlock, freeIdx int, use ssa.Instruction) bool {
	// For pointers, check if there's a Store to the same address between free and use.
	if !types.IsInterface(val.Type()) && isPointerType(val.Type()) {
		refs := val.Referrers()
		if refs == nil {
			return false
		}
		useBlock := use.Block()
		for _, ref := range *refs {
			store, ok := ref.(*ssa.Store)
			if !ok {
				continue
			}
			if store.Addr != val {
				continue
			}
			storeBlock := store.Block()
			if storeBlock == freeBlock && storeBlock == useBlock {
				// Same block: store must be between free and use.
				storeAfterFree := false
				storeBeforeUse := false
				for j := freeIdx + 1; j < len(freeBlock.Instrs); j++ {
					if freeBlock.Instrs[j] == store {
						storeAfterFree = true
					}
					if freeBlock.Instrs[j] == use {
						storeBeforeUse = true
						break
					}
				}
				if storeAfterFree && storeBeforeUse {
					return true
				}
			}
		}
	}
	return false
}

// isPointerType returns true if the type is a pointer type.
func isPointerType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Pointer)
	return ok
}
