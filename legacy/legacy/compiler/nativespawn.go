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
	"sort"

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
	// Build the entire program's SSA so we can scan EVERY package for
	// spawn() call sites. A spawn target function defined in package A
	// but invoked from package B needs the child-side prologue
	// (MarkChildPipeChannel) emitted when A is compiled — but the
	// spawn site that marks the function as a target lives in B's SSA
	// (which is lazy-built by default and may not yet exist when A is
	// being compiled).
	//
	// program.Build() is idempotent and fast on already-built packages,
	// so the cost is paid once per compilation context.
	c.program.Build()
	seen := map[*ssa.Function]bool{}
	scanned := map[*ssa.Function]bool{}
	// Walk every package, then for each pkg member walk the SSA
	// reachable from it. Methods aren't in pkg.Members directly — they
	// belong to type definitions — so for *ssa.Type members we have to
	// pull the method set via prog.MethodValue. Function values
	// referenced from anonymous functions / closures get picked up via
	// fn.AnonFuncs.
	for _, p := range c.program.AllPackages() {
		var names []string
		for name := range p.Members {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			switch m := p.Members[name].(type) {
			case *ssa.Function:
				c.scanFnDeep(m, seen, scanned)
			case *ssa.Type:
				c.scanTypeMethods(m.Type(), seen, scanned)
			}
		}
	}
	_ = pkg
}

// scanFnDeep scans fn, its anonymous funcs, and its direct callees.
// Following direct callees is required to find spawn() sites inside
// package init() bodies: source-level `func init()` declarations compile
// to SSA functions named init$1, init$2, etc., which are NOT package
// Members — they are only reachable as callees of the synthetic init
// wrapper. Without callee traversal, any spawn() call inside an init()
// is invisible to the scanner, so the target's channels are never
// registered and MarkChildPipeChannel is never emitted in the child
// process, leaving the child's channel.pipeBoundFd == 0 and breaking
// all channel I/O across that spawn boundary.
func (c *compilerContext) scanFnDeep(fn *ssa.Function, seen, scanned map[*ssa.Function]bool) {
	if scanned[fn] {
		return
	}
	scanned[fn] = true
	c.scanFnForNativeSpawn(fn, seen)
	for _, anon := range fn.AnonFuncs {
		c.scanFnDeep(anon, seen, scanned)
	}
	// Follow direct static callees so init$N bodies are reachable.
	for _, blk := range fn.Blocks {
		for _, instr := range blk.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				if callee, ok := call.Call.Value.(*ssa.Function); ok {
					c.scanFnDeep(callee, seen, scanned)
				}
			}
		}
	}
}

// scanTypeMethods walks every method on a named type (and on *T) and
// scans its body. The Go SSA program knows methods via MethodValue
// once the program has been Built.
func (c *compilerContext) scanTypeMethods(t types.Type, seen, scanned map[*ssa.Function]bool) {
	mset := c.program.MethodSets.MethodSet(t)
	for i := 0; i < mset.Len(); i++ {
		fn := c.program.MethodValue(mset.At(i))
		if fn != nil {
			c.scanFnDeep(fn, seen, scanned)
		}
	}
	mset = c.program.MethodSets.MethodSet(types.NewPointer(t))
	for i := 0; i < mset.Len(); i++ {
		fn := c.program.MethodValue(mset.At(i))
		if fn != nil {
			c.scanFnDeep(fn, seen, scanned)
		}
	}
}

func (c *compilerContext) scanFnForNativeSpawn(fn *ssa.Function, seen map[*ssa.Function]bool) {
	if len(fn.Blocks) == 0 {
		return
	}
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

	// Signal the parent that the child is ready.
	readyFn := runtimePkg.Members["PipeChildReady"].(*ssa.Function)
	readyType, readyVal := b.getFunction(readyFn)
	b.CreateCall(readyType, readyVal, []llvm.Value{llvm.Undef(b.dataPtrType)}, "")
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
