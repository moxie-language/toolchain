package compiler

// spawn_wasm.go — wasm spawn path.
//
// In wasm (GOOS=js GOARCH=wasm), spawn() creates a new Worker instead of
// forking a process. Channels at spawn boundaries are SAB-backed; all other
// channel operations use the regular runtime. Scalar args are packed into a
// linear-memory buffer and deserialized by __spawn_entry on the child side.
//
// Architecture:
//  Parent side (createWasmSpawn):
//    1. For each chan arg: bridge.channel_create → int32 SAB handle.
//    2. Pack scalar args into a stack alloca.
//    3. Pack SAB handles into a second alloca.
//    4. Create the breaker ring and call
//       bridge.spawn_domain(fnIdx, argPtr, argLen, chanHandlesPtr, nChans, breaker).
//
//  Child side (__spawn_entry, emitted at link time by emitWasmSpawnEntry):
//    A switch over fnIdx dispatches to a per-target case that:
//    1. Binds the domain's breaker to the parent's ring (WasmBreakerSet).
//    2. Deserializes scalars from argPtr.
//    3. Wraps each int32 handle into a channel (SpawnChannelWrap) so the
//       target's send/recv/close reach the parent's ring through the runtime's
//       spawn-bound channel path - the compilers do not special-case them.
//    4. Calls the target function directly, then closes the breaker.

import (
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// wasmSpawnTarget records a spawn-eligible function and its dispatch index.
type wasmSpawnTarget struct {
	fn              *ssa.Function
	fnIdx           int32
	chanParamIdxs   []int // indices into fn.Params that are channel types
	scalarParamIdxs []int // indices that are non-channel (scalar/struct)
}

// isWasmTarget returns true when building for GOOS=js GOARCH=wasm.
func (b *builder) isWasmTarget() bool {
	return strings.HasPrefix(b.Triple, "wasm") && b.GOOS == "js"
}

// scanWasmSpawnTargets pre-scans the package for spawn() calls, collecting
// all statically-known target functions and assigning each a stable fnIdx.
// Must be called after ssaPkg.Build() and before createPackage.
func (c *compilerContext) scanWasmSpawnTargets(pkg *ssa.Package) {
	if !strings.HasPrefix(c.Triple, "wasm") || c.GOOS != "js" {
		return
	}
	seen := make(map[*ssa.Function]bool)
	for _, mem := range pkg.Members {
		fn, ok := mem.(*ssa.Function)
		if !ok {
			continue
		}
		c.scanFnForSpawn(fn, seen)
	}
}

func (c *compilerContext) scanFnForSpawn(fn *ssa.Function, seen map[*ssa.Function]bool) {
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
			var chanIdxs, scalarIdxs []int
			for i := 0; i < sig.Params().Len(); i++ {
				if _, isChan := sig.Params().At(i).Type().Underlying().(*types.Chan); isChan {
					chanIdxs = append(chanIdxs, i)
				} else {
					scalarIdxs = append(scalarIdxs, i)
				}
			}
			idx := int32(len(c.wasmSpawnTargets))
			c.wasmSpawnTargets = append(c.wasmSpawnTargets, &wasmSpawnTarget{
				fn:              target,
				fnIdx:           idx,
				chanParamIdxs:   chanIdxs,
				scalarParamIdxs: scalarIdxs,
			})
			if c.wasmSpawnIndex == nil {
				c.wasmSpawnIndex = make(map[*ssa.Function]int32)
			}
			c.wasmSpawnIndex[target] = idx
		}
	}
}

func extractWasmSpawnTargetFn(args []ssa.Value) *ssa.Function {
	start := 0
	if len(args) > 0 {
		if _, ok := extractTransportString(args[0]); ok {
			start = 1
		}
	}
	if start >= len(args) {
		return nil
	}
	switch v := args[start].(type) {
	case *ssa.Function:
		return v
	case *ssa.MakeClosure:
		if f, ok := v.Fn.(*ssa.Function); ok {
			return f
		}
	}
	return nil
}

// validateWasmSpawnArg checks that a type is safe across the wasm spawn
// boundary. No Codec requirement. Rejects pointers, functions, interfaces.
// Top-level channel types are allowed (SAB-backed); nested channels are not.
func validateWasmSpawnArg(t types.Type, argIdx int) error {
	if _, ok := t.Underlying().(*types.Chan); ok {
		return nil
	}
	return validateWasmSpawnElem(t, argIdx)
}

func validateWasmSpawnElem(t types.Type, argIdx int) error {
	switch ut := t.Underlying().(type) {
	case *types.Basic:
		return nil
	case *types.Pointer:
		return fmt.Errorf("spawn: argument %d is a pointer — not allowed across wasm spawn boundary", argIdx+1)
	case *types.Signature:
		return fmt.Errorf("spawn: argument %d is a function value — not allowed across wasm spawn boundary", argIdx+1)
	case *types.Interface:
		return fmt.Errorf("spawn: argument %d is an interface — not allowed across wasm spawn boundary", argIdx+1)
	case *types.Chan:
		return fmt.Errorf("spawn: argument %d contains a nested channel — channels must be top-level params", argIdx+1)
	case *types.Slice:
		if basic, ok := ut.Elem().Underlying().(*types.Basic); ok && basic.Kind() == types.Byte {
			return nil // []byte / string
		}
		return validateWasmSpawnElem(ut.Elem(), argIdx)
	case *types.Struct:
		for i := 0; i < ut.NumFields(); i++ {
			if err := validateWasmSpawnElem(ut.Field(i).Type(), argIdx); err != nil {
				return err
			}
		}
		return nil
	case *types.Array:
		return validateWasmSpawnElem(ut.Elem(), argIdx)
	}
	return fmt.Errorf("spawn: argument %d type %s not allowed across wasm spawn boundary", argIdx+1, t)
}

// createWasmSpawn handles spawn() in the wasm target. It emits:
//   1. bridge.channel_create for each channel arg → SAB handle.
//   2. A packed scalar arg buffer.
//   3. A packed chan handle array.
//   4. bridge.spawn_domain(fnIdx, argPtr, argLen, chanHandlesPtr, nChans, breaker).
func (b *builder) createWasmSpawn(instr *ssa.CallCommon) (llvm.Value, error) {
	argStart := 0
	if len(instr.Args) > 0 {
		if _, ok := extractTransportString(instr.Args[0]); ok {
			argStart = 1
		}
	}
	if argStart >= len(instr.Args) {
		b.addError(instr.Pos(), "spawn: requires a function argument")
		return llvm.Value{}, nil
	}

	fnArg := instr.Args[argStart]
	var targetFn *ssa.Function
	switch v := fnArg.(type) {
	case *ssa.Function:
		targetFn = v
	case *ssa.MakeClosure:
		if f, ok := v.Fn.(*ssa.Function); ok {
			targetFn = f
		}
	}
	if targetFn == nil {
		b.addError(instr.Pos(), "spawn: first argument must be a static top-level function")
		return llvm.Value{}, nil
	}
	if targetFn.Package() != nil && b.fn.Package() != nil &&
		targetFn.Package().Pkg.Path() != b.fn.Package().Pkg.Path() {
		b.addError(instr.Pos(), "spawn: wasm spawn target must be in the same package as the caller")
		return llvm.Value{}, nil
	}

	fnIdx, ok := b.wasmSpawnIndex[targetFn]
	if !ok {
		b.addError(instr.Pos(), "spawn: internal — function not in wasm dispatch table (scan missed it)")
		return llvm.Value{}, nil
	}

	concreteArgs := instr.Args[argStart+1:]
	sig := targetFn.Signature
	if len(concreteArgs) != sig.Params().Len() {
		b.addError(instr.Pos(), fmt.Sprintf("spawn: %s expects %d args, got %d",
			targetFn.Name(), sig.Params().Len(), len(concreteArgs)))
		return llvm.Value{}, nil
	}

	// The spawn ABI is one rule for both targets: exactly one result, the
	// domain's breaker, a chan struct{}.
	if !isSpawnTargetSig(sig) {
		b.addError(instr.Pos(), fmt.Sprintf(
			"spawn: %s must be declared with exactly one result, (closed chan struct{}), which is the domain's breaker; a function without it cannot be spawned",
			targetFn.Name()))
		return llvm.Value{}, nil
	}
	if globalName, found := b.spawnTargetReadsChannelGlobal(targetFn); found {
		b.addError(instr.Pos(), fmt.Sprintf(
			"spawn: %s reads channel global %s; a spawned domain's channels must be its parameters, so pass it through the spawn call",
			targetFn.Name(), globalName))
		return llvm.Value{}, nil
	}
	for i, arg := range concreteArgs {
		if err := validateWasmSpawnArg(sig.Params().At(i).Type(), i); err != nil {
			b.addError(instr.Pos(), err.Error())
			return llvm.Value{}, nil
		}
		_ = arg
	}

	i32 := b.ctx.Int32Type()

	// --- channel args: create SAB handles ---
	var chanHandles []llvm.Value
	for _, arg := range concreteArgs {
		if _, isChan := arg.Type().Underlying().(*types.Chan); !isChan {
			continue
		}
		elemType := arg.Type().Underlying().(*types.Chan).Elem()
		elemSz := b.targetData.TypeAllocSize(b.getLLVMType(elemType))
		slotSize := nextPow2u32(uint32(elemSz) + 12)
		if slotSize < 64 {
			slotSize = 64
		}
		slotCount := uint32(16)
		handle := b.emitBridgeCall("channel_create", []llvm.Value{
			llvm.ConstInt(i32, uint64(slotSize), false),
			llvm.ConstInt(i32, uint64(slotCount), false),
		}, i32)
		chanHandles = append(chanHandles, handle)
		// Tag the parent's channel so its sends and receives reach the ring.
		// Marking at run time keeps this correct through the copies and reloads
		// SSA introduces: the same source variable is a different SSA value at
		// the spawn and at the send, so tracking SSA values misses it (the bug
		// stage4 fixed, and the reason this compiler no longer keeps a map).
		b.createRuntimeCall("MarkChannelSAB", []llvm.Value{
			b.getValue(arg, instr.Pos()),
			handle,
		}, "")
	}

	// --- scalar args: pack into a stack buffer ---
	var scalarVals []llvm.Value
	var scalarTypes []llvm.Type
	var totalScalarBytes uint64
	for i, arg := range concreteArgs {
		if _, isChan := sig.Params().At(i).Type().Underlying().(*types.Chan); isChan {
			continue
		}
		v := b.getValue(arg, instr.Pos())
		llvmType := b.getLLVMType(arg.Type())
		scalarVals = append(scalarVals, v)
		scalarTypes = append(scalarTypes, llvmType)
		totalScalarBytes += b.targetData.TypeAllocSize(llvmType)
	}

	argPtr := llvm.ConstNull(b.dataPtrType)
	argLen := llvm.ConstInt(i32, 0, false)
	if totalScalarBytes > 0 {
		bufType := llvm.ArrayType(b.ctx.Int8Type(), int(totalScalarBytes))
		bufAlloca, _ := b.createTemporaryAlloca(bufType, "spawn.args")
		var off uint64
		for idx, v := range scalarVals {
			sz := b.targetData.TypeAllocSize(scalarTypes[idx])
			gep := b.CreateGEP(bufType, bufAlloca, []llvm.Value{
				llvm.ConstInt(i32, 0, false),
				llvm.ConstInt(i32, off, false),
			}, "spawn.arg.slot")
			b.CreateStore(v, gep)
			off += sz
		}
		argPtr = b.CreateGEP(bufType, bufAlloca, []llvm.Value{
			llvm.ConstInt(i32, 0, false),
			llvm.ConstInt(i32, 0, false),
		}, "spawn.argptr")
		argLen = llvm.ConstInt(i32, totalScalarBytes, false)
	}

	// --- channel handle array ---
	nChans := len(chanHandles)
	chanPtr := llvm.ConstNull(b.dataPtrType)
	nChansVal := llvm.ConstInt(i32, uint64(nChans), false)
	if nChans > 0 {
		handleArrType := llvm.ArrayType(i32, nChans)
		handleAlloca, _ := b.createTemporaryAlloca(handleArrType, "spawn.chans")
		for hIdx, h := range chanHandles {
			gep := b.CreateGEP(handleArrType, handleAlloca, []llvm.Value{
				llvm.ConstInt(i32, 0, false),
				llvm.ConstInt(i32, uint64(hIdx), false),
			}, "")
			b.CreateStore(h, gep)
		}
		chanPtr = b.CreateGEP(handleArrType, handleAlloca, []llvm.Value{
			llvm.ConstInt(i32, 0, false),
			llvm.ConstInt(i32, 0, false),
		}, "spawn.chanptr")
	}

	// The breaker ring is created by the parent BEFORE the domain starts: spawn
	// returns the parent's end and the child binds the same ring as its
	// `closed`. Without it the child's breaker would be a local channel the
	// parent cannot close, so close(done) would stop nothing.
	breakerHandle := b.emitBridgeCall("channel_create", []llvm.Value{
		llvm.ConstInt(i32, 64, false),
		llvm.ConstInt(i32, 16, false),
	}, i32)

	// bridge.spawn_domain(fnIdx, argPtr, argLen, chanPtr, nChans, breaker)
	b.emitBridgeCall("spawn_domain", []llvm.Value{
		llvm.ConstInt(i32, uint64(fnIdx), false),
		argPtr, argLen,
		chanPtr, nChansVal,
		breakerHandle,
	}, b.ctx.VoidType())

	// spawn() yields the domain's lifecycle channel: the parent's end of the
	// breaker ring, marked spawn-bound so close() and <- reach the child.
	return b.createRuntimeCall("SpawnChannelWrap", []llvm.Value{
		breakerHandle,
		llvm.ConstInt(i32, 0, false),
	}, "spawn.breaker"), nil
}

// emitBridgeCall emits a call to a wasm import from the "bridge" module.
// The function declaration is created on first use.
func (b *builder) emitBridgeCall(name string, args []llvm.Value, retType llvm.Type) llvm.Value {
	return emitBridgeCallCtx(b.compilerContext, b.Builder, name, args, retType)
}

func emitBridgeCallCtx(c *compilerContext, builder llvm.Builder, name string, args []llvm.Value, retType llvm.Type) llvm.Value {
	internalName := "__bridge_" + name
	argTypes := make([]llvm.Type, len(args))
	for i, a := range args {
		argTypes[i] = a.Type()
	}
	fnType := llvm.FunctionType(retType, argTypes, false)
	fn := c.mod.NamedFunction(internalName)
	if fn.IsNil() {
		fn = llvm.AddFunction(c.mod, internalName, fnType)
		fn.AddFunctionAttr(c.ctx.CreateStringAttribute("wasm-import-module", "bridge"))
		fn.AddFunctionAttr(c.ctx.CreateStringAttribute("wasm-import-name", name))
		fn.SetLinkage(llvm.ExternalLinkage)
	}
	if retType == c.ctx.VoidType() {
		builder.CreateCall(fnType, fn, args, "")
		return llvm.Value{}
	}
	return builder.CreateCall(fnType, fn, args, "bridge."+name)
}

// wasmSpawnTargetFor returns the wasmSpawnTarget descriptor if fn is a spawn
// target in the current program, nil otherwise.
func (b *builder) wasmSpawnTargetFor(fn *ssa.Function) *wasmSpawnTarget {
	if _, ok := b.wasmSpawnIndex[fn]; !ok {
		return nil
	}
	for _, t := range b.wasmSpawnTargets {
		if t.fn == fn {
			return t
		}
	}
	return nil
}

// emitWasmSpawnEntry emits the __spawn_entry export after all package
// functions have been compiled. This function is the Worker entry point
// called by jsruntime/worker-host.mjs instead of _start.
//
// Signature: void __spawn_entry(fnIdx i32, argPtr ptr, argLen i32, chanHandlesPtr ptr, nChans i32, breaker i32)
func (c *compilerContext) emitWasmSpawnEntry(irbuilder llvm.Builder) {
	if !strings.HasPrefix(c.Triple, "wasm") || c.GOOS != "js" {
		return
	}
	// Only emit __spawn_entry in packages that actually contain spawn calls.
	// Each package is compiled into a separate LLVM module; packages with no
	// spawn calls must not define the symbol (link-time multiply-defined error).
	if len(c.wasmSpawnTargets) == 0 {
		return
	}

	i32 := c.ctx.Int32Type()
	ptrType := c.dataPtrType
	fnType := llvm.FunctionType(c.ctx.VoidType(), []llvm.Type{i32, ptrType, i32, ptrType, i32, i32}, false)

	entryFn := llvm.AddFunction(c.mod, "__spawn_entry", fnType)
	entryFn.AddFunctionAttr(c.ctx.CreateStringAttribute("wasm-export-name", "__spawn_entry"))
	// wasm-export-name places the function in the wasm export table,
	// which prevents DCE. No llvm.used needed.

	entryBlock := c.ctx.AddBasicBlock(entryFn, "entry")
	irbuilder.SetInsertPointAtEnd(entryBlock)

	fnIdx := entryFn.Param(0)
	argPtr := entryFn.Param(1)
	argLen := entryFn.Param(2)
	chanHandlesPtr := entryFn.Param(3)
	nChans := entryFn.Param(4)
	breaker := entryFn.Param(5)
	_, _ = argLen, nChans // used indirectly through GEPs

	defaultBlock := c.ctx.AddBasicBlock(entryFn, "default")
	irbuilder.SetInsertPointAtEnd(defaultBlock)
	irbuilder.CreateUnreachable()
	irbuilder.SetInsertPointAtEnd(entryBlock)

	// A spawned domain never runs _start: this call is its runtime
	// initialization (root arena, package initialisers) before any target code
	// or argument decode can allocate.
	if rtPkg := c.program.ImportedPackage("runtime"); rtPkg != nil {
		if member, ok := rtPkg.Members["WasmSpawnInit"].(*ssa.Function); ok {
			initType, initFn := c.getFunction(member)
			irbuilder.CreateCall(initType, initFn, []llvm.Value{llvm.Undef(c.dataPtrType)}, "")
		}
		// The parent's breaker ring becomes this domain's `closed` before any
		// target code runs, so a worker parked on it can be stopped.
		if member, ok := rtPkg.Members["WasmBreakerSet"].(*ssa.Function); ok {
			setType, setFn := c.getFunction(member)
			irbuilder.CreateCall(setType, setFn, []llvm.Value{breaker, llvm.Undef(c.dataPtrType)}, "")
		}
	}

	sw := irbuilder.CreateSwitch(fnIdx, defaultBlock, len(c.wasmSpawnTargets))

	for _, target := range c.wasmSpawnTargets {
		caseBlock := c.ctx.AddBasicBlock(entryFn, "case."+target.fn.Name())
		irbuilder.SetInsertPointAtEnd(caseBlock)

		_, targetLLVM := c.getFunction(target.fn)
		targetFnType, _ := c.getFunction(target.fn)
		sig := target.fn.Signature

		var callArgs []llvm.Value
		var scalarOffset uint64
		chanIdx := 0

		for i := 0; i < sig.Params().Len(); i++ {
			param := sig.Params().At(i)
			llvmType := c.getLLVMType(param.Type())

			if ch, isChan := param.Type().Underlying().(*types.Chan); isChan {
				// Load int32 handle from chanHandlesPtr[chanIdx], then build the
				// child's channel object around it. The handle is not a pointer:
				// inttoptr it and the target's send/recv/close would write
				// through an address that was never a *channel.
				handleGEP := irbuilder.CreateGEP(i32, chanHandlesPtr, []llvm.Value{
					llvm.ConstInt(i32, uint64(chanIdx), false),
				}, "chan.handle.gep")
				handle := irbuilder.CreateLoad(i32, handleGEP, "chan.handle")
				elemSz := c.targetData.TypeAllocSize(c.getLLVMType(ch.Elem()))
				rtPkg := c.program.ImportedPackage("runtime")
				member, ok := rtPkg.Members["SpawnChannelWrap"].(*ssa.Function)
				if !ok {
					panic("emitWasmSpawnEntry: runtime.SpawnChannelWrap not found")
				}
				wrapType, wrapFn := c.getFunction(member)
				chanPtr := irbuilder.CreateCall(wrapType, wrapFn, []llvm.Value{
					handle,
					llvm.ConstInt(i32, elemSz, false),
					llvm.Undef(c.dataPtrType),
				}, "chan.wrap")
				callArgs = append(callArgs, chanPtr)
				chanIdx++
			} else {
				sz := c.targetData.TypeAllocSize(llvmType)
				gep := irbuilder.CreateGEP(c.ctx.Int8Type(), argPtr, []llvm.Value{
					llvm.ConstInt(i32, scalarOffset, false),
				}, "arg.gep")
				val := irbuilder.CreateLoad(llvmType, gep, fmt.Sprintf("arg%d", i))
				callArgs = append(callArgs, val)
				scalarOffset += sz
			}
		}

		// Non-exported Moxie functions have a trailing context ptr parameter.
		// Spawn targets are never closures, so pass undef context.
		info := c.getFunctionInfo(target.fn)
		if !info.exported {
			callArgs = append(callArgs, llvm.Undef(ptrType))
		}

		irbuilder.CreateCall(targetFnType, targetLLVM, callArgs, "")
		// The domain is finished: closing the breaker wakes the parent's
		// `<-done` (and any other holder) the way a native domain's exit does.
		if rtPkg := c.program.ImportedPackage("runtime"); rtPkg != nil {
			if member, ok := rtPkg.Members["WasmBreakerClose"].(*ssa.Function); ok {
				closeType, closeFn := c.getFunction(member)
				irbuilder.CreateCall(closeType, closeFn, []llvm.Value{llvm.Undef(c.dataPtrType)}, "")
			}
		}
		irbuilder.CreateRetVoid()

		sw.AddCase(llvm.ConstInt(i32, uint64(target.fnIdx), false), caseBlock)
	}
}

func nextPow2u32(n uint32) uint32 {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}
