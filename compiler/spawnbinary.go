package compiler

// Cross-binary spawn: when a spawn target's package was loaded from a .mxh
// cache file (loader.MXHPackages), the compiler routes to SpawnBinary instead
// of spawnDomain. The binary path and .mxh hash are embedded as string
// constants resolved from the mxinstall cache at compile time.

import (
	"strings"

	"moxie/cache"
	"moxie/loader"
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// isExternalMXHSpawn returns true if the spawn target function's package was
// loaded from a .mxh cache file and should use the cross-binary spawn path.
func (b *builder) isExternalMXHSpawn(fn *ssa.Function) bool {
	if fn.Package() == nil {
		return false
	}
	return b.MXHPackages[fn.Package().Pkg.Path()]
}

// createCrossBinarySpawn handles spawn() calls whose target is in a .mxh
// package. It emits a call to runtime.SpawnBinary with the cached binary path
// and the .mxh content hash for the protocol types crossing the boundary.
//
// The child binary is a standalone program that reads MOXIE_IPC_FD from its
// environment to find the IPC channel established by SpawnBinary. The .mxh
// hash is exchanged during the handshake to verify protocol compatibility.
func (b *builder) createCrossBinarySpawn(targetFn *ssa.Function, instr *ssa.CallCommon) (llvm.Value, error) {
	importPath := targetFn.Package().Pkg.Path()

	// Resolve binary path from cache.
	binPath := cache.BinPath(b.Triple, importPath, binaryBaseName(importPath))

	// Look up the .mxh hash for this package.
	hash := loader.MXHHashForImport(importPath)
	if hash == "" {
		b.addError(instr.Pos(), "spawn: no .mxh found for "+importPath+"; run `moxie install "+importPath+"`")
		return llvm.Value{}, nil
	}

	// Build LLVM string constants for binPath and hash.
	binPathVal := b.makeStringConst(binPath)
	hashVal := b.makeStringConst(hash)

	// Call runtime.SpawnBinary(binPath, hash).
	runtimePkg := b.program.ImportedPackage("runtime")
	spawnBinaryMember, ok := runtimePkg.Members["SpawnBinary"]
	if !ok {
		b.addError(instr.Pos(), "spawn: runtime.SpawnBinary not found — rebuild runtime")
		return llvm.Value{}, nil
	}
	fnType, fnVal := b.getFunction(spawnBinaryMember.(*ssa.Function))
	b.createCall(fnType, fnVal, []llvm.Value{binPathVal, hashVal, llvm.Undef(b.dataPtrType)}, "ipc.fd")

	// Cross-binary spawn doesn't return a lifecycle channel yet.
	// TODO: extend SpawnBinary to register domain and return lifecycle chan.
	return llvm.Undef(b.dataPtrType), nil
}

// binaryBaseName extracts the binary name from an import path.
// "git.smesh.lol/iskradb" → "iskradb"
func binaryBaseName(importPath string) string {
	if i := strings.LastIndexByte(importPath, '/'); i >= 0 {
		return importPath[i+1:]
	}
	return importPath
}

// makeStringConst creates an LLVM string constant (_string struct) for s.
// Follows the same pattern as constant string emission in compiler.go:3155.
func (b *builder) makeStringConst(s string) llvm.Value {
	strLen := llvm.ConstInt(b.uintptrType, uint64(len(s)), false)
	var strPtr llvm.Value
	if s != "" {
		globalType := llvm.ArrayType(b.ctx.Int8Type(), len(s))
		global := llvm.AddGlobal(b.mod, globalType, ".spawn.str")
		global.SetInitializer(b.ctx.ConstString(s, false))
		global.SetLinkage(llvm.InternalLinkage)
		global.SetGlobalConstant(true)
		global.SetUnnamedAddr(true)
		global.SetAlignment(1)
		zero := llvm.ConstInt(b.ctx.Int32Type(), 0, false)
		strPtr = llvm.ConstInBoundsGEP(globalType, global, []llvm.Value{zero, zero})
	} else {
		strPtr = llvm.ConstNull(b.dataPtrType)
	}
	return llvm.ConstNamedStruct(b.getLLVMRuntimeType("_string"), []llvm.Value{strPtr, strLen, strLen})
}
