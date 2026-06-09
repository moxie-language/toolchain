package transform

import (
	"tinygo.org/x/go-llvm"
)

// OptimizeMaps eliminates created but unused maps.
//
// In the future, this should statically allocate created but never modified
// maps. This has not yet been implemented, however.
func OptimizeMaps(mod llvm.Module) {
	hashmapMake := mod.NamedFunction("runtime.hashmapMake")
	if hashmapMake.IsNil() {
		// nothing to optimize
		return
	}

	hashmapBinarySet := mod.NamedFunction("runtime.hashmapBinarySet")
	hashmapContentSet := mod.NamedFunction("runtime.hashmapContentSet")

	for _, makeInst := range getUses(hashmapMake) {
		updateInsts := []llvm.Value{}
		unknownUses := false // are there any uses other than setting a value?

		for _, use := range getUses(makeInst) {
			if use := use.IsACallInst(); !use.IsNil() {
				switch use.CalledValue() {
				case hashmapBinarySet, hashmapContentSet:
					updateInsts = append(updateInsts, use)
				default:
					unknownUses = true
				}
			} else {
				unknownUses = true
			}
		}

		if !unknownUses {
			// This map can be entirely removed, as it is only created but never
			// used.
			for _, inst := range updateInsts {
				inst.EraseFromParentAsInstruction()
			}
			makeInst.EraseFromParentAsInstruction()
		}
	}
}
