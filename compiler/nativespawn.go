package compiler

// Native spawn channel routing. Phase A added pipeBoundFd/pipeChanID
// metadata to the channel struct plus pipe-routing helpers. Phase B
// wires the compiler so that:
//
//   1. createSpawn (native path) calls runtime.MarkParentPipeChannel on
//      every channel argument right after spawnDomain returns. The
//      runtime helper looks up the parent fd from LastSpawnedParentFd.
//   2. setupNativeSpawnTargetChannels (mirror of the wasm helper) emits
//      runtime.MarkChildPipeChannel at the entry of every spawn-target
//      function. The helper internally checks ChildPipeFd >= 0 so that
//      calling the function NOT through spawn leaves its parameters as
//      ordinary local channels.
//   3. createChanSend / createChanRecv check b.pipeChannels and emit a
//      pipe-routed call (PipeChanSendCodec / PipeChanRecvCodec) that
//      invokes the user-defined Codec methods to serialize the chan
//      element, instead of the in-runtime queue path.
//
// Channels are multiplexed over a single socketpair fd by chanID. A
// stable layout is fixed by parameter order: the i-th channel parameter
// of the spawn target gets chanID = i+1. ChanID 0 is reserved for
// pipe control messages (currently just close-of-channel notifications).

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

type nativeSpawnTarget struct {
	fn            *ssa.Function
	chanParamIdxs []int    // indices of channel-typed params, declaration order
	chanIDs       []uint16 // aligned with chanParamIdxs, 1-based
}

func (c *compilerContext) scanNativeSpawnTargets(pkg *ssa.Package) {
	if c.isWasmContext() {
		return
	}
	if c.nativeSpawnIndex == nil {
		c.nativeSpawnIndex = make(map[*ssa.Function]*nativeSpawnTarget)
	}
	seen := map[*ssa.Function]bool{}
	for _, mem := range pkg.Members {
		fn, ok := mem.(*ssa.Function)
		if !ok {
			continue
		}
		c.scanFnForNativeSpawn(fn, seen)
	}
}

func (c *compilerContext) scanFnForNativeSpawn(fn *ssa.Function, seen map[*ssa.Function]bool) {
	for _, blk := range fn.Blocks {
		for _, instr := range blk.Instrs {
			call, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			bi, ok := call.Call.Value.(*ssa.Builtin)
			if !ok || bi.Name() != "spawn" {
				continue
			}
			target := extractWasmSpawnTargetFn(call.Call.Args)
			if target == nil || seen[target] {
				continue
			}
			seen[target] = true
			sig := target.Signature
			var chanIdxs []int
			var chanIDs []uint16
			var nextID uint16 = 1
			for i := 0; i < sig.Params().Len(); i++ {
				if _, isChan := sig.Params().At(i).Type().Underlying().(*types.Chan); isChan {
					chanIdxs = append(chanIdxs, i)
					chanIDs = append(chanIDs, nextID)
					nextID++
				}
			}
			t := &nativeSpawnTarget{
				fn:            target,
				chanParamIdxs: chanIdxs,
				chanIDs:       chanIDs,
			}
			c.nativeSpawnTargets = append(c.nativeSpawnTargets, t)
			c.nativeSpawnIndex[target] = t
		}
	}
}

// isWasmContext returns true on the wasm target. Native spawn handling
// is suppressed for wasm because the wasm path uses SAB channels.
func (c *compilerContext) isWasmContext() bool {
	return c.GOOS == "js" && len(c.Triple) >= 4 && c.Triple[:4] == "wasm"
}

func (c *compilerContext) nativeSpawnTargetFor(fn *ssa.Function) *nativeSpawnTarget {
	if c.nativeSpawnIndex == nil {
		return nil
	}
	return c.nativeSpawnIndex[fn]
}

// setupNativeSpawnTargetChannels emits the child-side prologue for a
// spawn target function. Linear, no branching: a single call to
// runtime.MarkChildPipeChannel(param, chanID) per channel parameter.
// The helper internally no-ops when ChildPipeFd < 0.
func (b *builder) setupNativeSpawnTargetChannels() {
	if b.isWasmTarget() || b.fn == nil {
		return
	}
	target := b.nativeSpawnTargetFor(b.fn)
	if target == nil {
		return
	}
	if b.pipeChannels == nil {
		b.pipeChannels = make(map[ssa.Value]bool)
	}

	runtimePkg := b.program.ImportedPackage("runtime")
	mark := runtimePkg.Members["MarkChildPipeChannel"].(*ssa.Function)
	markType, markFn := b.getFunction(mark)
	i16 := b.ctx.Int16Type()

	for i, paramIdx := range target.chanParamIdxs {
		if paramIdx >= len(b.fn.Params) {
			continue
		}
		param := b.fn.Params[paramIdx]
		llvmParam, ok := b.locals[param]
		if !ok {
			continue
		}
		chanID := target.chanIDs[i]
		b.CreateCall(markType, markFn, []llvm.Value{
			llvmParam,
			llvm.ConstInt(i16, uint64(chanID), false),
			llvm.Undef(b.dataPtrType),
		}, "")
		b.pipeChannels[param] = true
	}
}

// emitNativeSpawnChannelMark emits the parent-side MarkParentPipeChannel
// calls right after spawnDomain returns, plus tracks each channel SSA
// value in b.pipeChannels so subsequent chan ops route via pipe.
//
// chanArgs lists the (ssa.Value, llvm.Value) pairs for every channel
// argument in the spawned function's parameter order. Aligns with the
// target's chanIDs.
func (b *builder) emitNativeSpawnChannelMark(target *nativeSpawnTarget, chanArgs []spawnChanArg) {
	if len(chanArgs) == 0 {
		return
	}
	runtimePkg := b.program.ImportedPackage("runtime")
	mark := runtimePkg.Members["MarkParentPipeChannel"].(*ssa.Function)
	markType, markFn := b.getFunction(mark)

	if b.pipeChannels == nil {
		b.pipeChannels = make(map[ssa.Value]bool)
	}
	i16 := b.ctx.Int16Type()
	for i, ca := range chanArgs {
		chanID := target.chanIDs[i]
		b.CreateCall(markType, markFn, []llvm.Value{
			ca.llvm,
			llvm.ConstInt(i16, uint64(chanID), false),
			llvm.Undef(b.dataPtrType),
		}, "")
		b.pipeChannels[ca.ssa] = true
	}
}

type spawnChanArg struct {
	ssa  ssa.Value
	llvm llvm.Value
}
