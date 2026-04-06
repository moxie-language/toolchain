package jsbackend

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// FunctionCompiler handles the translation of a single SSA function to JS.
type FunctionCompiler struct {
	pc     *ProgramCompiler
	fn     *ssa.Function
	e      *Emitter
	locals map[ssa.Value]string // SSA value -> JS variable name
	varGen int                  // variable name counter
	// Track which blocks need phi variable assignments.
	phis []*ssa.Phi
}

// freshVar generates a unique local variable name.
func (fc *FunctionCompiler) freshVar(hint string) string {
	fc.varGen++
	if hint != "" {
		return fmt.Sprintf("$%s_%d", JsIdentifier(hint), fc.varGen)
	}
	return fmt.Sprintf("$v_%d", fc.varGen)
}

// compile generates JS for the entire function.
func (fc *FunctionCompiler) compile() {
	fn := fc.fn
	e := fc.e

	name := functionJsName(fn)
	params := functionParams(fn)

	// Determine if function needs to be async (contains channel ops, goroutine spawns, etc.).
	async := fc.needsAsync()
	hasDefer := fc.hasDefer()

	prefix := "export "
	if fn.Parent() != nil {
		prefix = "" // nested/anonymous functions are not exported
	}

	asyncStr := ""
	if async {
		asyncStr = "async "
	}

	e.Block("%s%sfunction %s(%s)", prefix, asyncStr, name, params)

	// Emit local variable declarations for all SSA values.
	fc.emitLocals()

	if hasDefer {
		e.Line("const $defers = [];")
		e.Line("let $panicValue = null;")
		e.Block("try")
	}

	// Emit blocks.
	// Use a block-label scheme: while(true) { switch($block) { case 0: ... } }
	if len(fn.Blocks) == 1 {
		// Simple function — no control flow needed.
		fc.emitBlock(fn.Blocks[0], false)
	} else if len(fn.Blocks) > 1 {
		e.Line("let $block = 0;")
		e.Block("while (true)")
		e.Block("switch ($block)")

		for _, block := range fn.Blocks {
			// Skip the recover block — it's emitted after the try/catch/finally.
			if hasDefer && fn.Recover != nil && block == fn.Recover {
				continue
			}
			e.Line("case %d: {", block.Index)
			e.Indent()
			fc.emitBlock(block, true)
			e.Line("break;")
			e.Dedent()
			e.Line("}")
		}

		e.EndBlock() // switch
		e.EndBlock() // while
	}

	if hasDefer {
		e.EndBlock() // try
		e.Block("catch ($e)")
		e.Line("$panicValue = $e;")
		e.EndBlock() // catch
		e.Block("finally")
		e.Line("$rt.runtime.runDeferStack($defers, $panicValue);")
		e.EndBlock() // finally

		// If a panic was recovered, execution continues here.
		// Emit the recover block which reads named return values.
		if fn.Recover != nil {
			e.Comment("Recover block — reached after successful recovery.")
			fc.emitBlock(fn.Recover, false)
		}
	}

	e.EndBlock() // function
	e.Newline()

	// Compile anonymous (closure) functions.
	for _, anon := range fn.AnonFuncs {
		fc.pc.compileFunction(e, anon)
	}
}

// hasDefer checks if the function contains any defer statements.
func (fc *FunctionCompiler) hasDefer() bool {
	for _, block := range fc.fn.Blocks {
		for _, instr := range block.Instrs {
			if _, ok := instr.(*ssa.Defer); ok {
				return true
			}
		}
	}
	return false
}

// emitLocals declares JS variables for all SSA values used in the function.
func (fc *FunctionCompiler) emitLocals() {
	// Parameters get their names from the SSA params.
	for _, p := range fc.fn.Params {
		fc.locals[p] = JsIdentifier(p.Name())
	}

	// Free variables (closures).
	for _, fv := range fc.fn.FreeVars {
		fc.locals[fv] = JsIdentifier(fv.Name())
	}

	// Scan all instructions for values that need variables.
	var varDecls []string
	for _, block := range fc.fn.Blocks {
		for _, instr := range block.Instrs {
			val, ok := instr.(ssa.Value)
			if !ok {
				continue
			}
			if _, exists := fc.locals[val]; exists {
				continue
			}
			name := fc.freshVar(val.Name())
			fc.locals[val] = name
			varDecls = append(varDecls, name)
		}
	}
	if len(varDecls) > 0 {
		fc.e.Line("let %s;", strings.Join(varDecls, ", "))
	}
}

// emitBlock emits JS for all instructions in a basic block.
func (fc *FunctionCompiler) emitBlock(block *ssa.BasicBlock, inSwitch bool) {
	for _, instr := range block.Instrs {
		fc.emitInstruction(instr, inSwitch)
	}
}

// emitInstruction dispatches a single SSA instruction to JS code.
func (fc *FunctionCompiler) emitInstruction(instr ssa.Instruction, inSwitch bool) {
	e := fc.e

	switch instr := instr.(type) {
	case *ssa.Alloc:
		dst := fc.local(instr)
		elemType := instr.Type().(*types.Pointer).Elem()
		zero := fc.pc.typeMapper.ZeroExpr(elemType)
		// Alloc creates a pointer. We model it as an accessor object.
		if IsValueType(elemType) {
			e.Line("%s = { $value: %s, $get() { return this.$value; }, $set(v) { this.$value = v; } };", dst, zero)
		} else {
			e.Line("%s = { $value: %s, $get() { return this.$value; }, $set(v) { this.$value = v; } };", dst, zero)
		}

	case *ssa.BinOp:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		y := fc.value(instr.Y)
		op := fc.binOp(instr.Op, instr.X.Type())
		e.Line("%s = %s;", dst, op(x, y))

	case *ssa.Call:
		dst := fc.local(instr)
		callExpr := fc.emitCall(instr.Common())
		if dst != "" && instr.Type() != nil {
			e.Line("%s = %s;", dst, callExpr)
		} else {
			e.Line("%s;", callExpr)
		}

	case *ssa.ChangeInterface:
		dst := fc.local(instr)
		e.Line("%s = %s;", dst, fc.value(instr.X))

	case *ssa.ChangeType:
		dst := fc.local(instr)
		e.Line("%s = %s;", dst, fc.value(instr.X))

	case *ssa.Convert:
		dst := fc.local(instr)
		e.Line("%s = %s;", dst, fc.emitConvert(instr))

	case *ssa.DebugRef:
		// Skip.

	case *ssa.Defer:
		fc.emitDefer(instr)

	case *ssa.Extract:
		dst := fc.local(instr)
		tuple := fc.value(instr.Tuple)
		e.Line("%s = %s[%d];", dst, tuple, instr.Index)

	case *ssa.Field:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		fieldName := fieldJsName(instr.X.Type(), instr.Field)
		e.Line("%s = %s.%s;", dst, x, fieldName)

	case *ssa.FieldAddr:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		fieldName := fieldJsName(instr.X.Type().(*types.Pointer).Elem(), instr.Field)
		// Return an accessor that reads/writes the field.
		e.Line("%s = { $get() { return %s.$get().%s; }, $set(v) { const obj = %s.$get(); obj.%s = v; %s.$set(obj); } };",
			dst, x, fieldName, x, fieldName, x)

	case *ssa.Go:
		fc.emitGo(instr)

	case *ssa.If:
		cond := fc.value(instr.Cond)
		block := instr.Block()
		if inSwitch {
			// Emit phi assignments before each branch target.
			thenPhis := fc.phiAssignmentStmts(block, block.Succs[0])
			elsePhis := fc.phiAssignmentStmts(block, block.Succs[1])
			e.Block("if (%s)", cond)
			for _, s := range thenPhis {
				e.Line("%s", s)
			}
			e.Line("$block = %d; break;", block.Succs[0].Index)
			e.EndBlock()
			e.Block("else")
			for _, s := range elsePhis {
				e.Line("%s", s)
			}
			e.Line("$block = %d; break;", block.Succs[1].Index)
			e.EndBlock()
		} else {
			e.Line("if (%s) { /* block %d */ } else { /* block %d */ }",
				cond, block.Succs[0].Index, block.Succs[1].Index)
		}

	case *ssa.Index:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		idx := fc.value(instr.Index)
		switch instr.X.Type().Underlying().(type) {
		case *types.Basic: // string — use UTF-8 byte semantics
			e.Line("$rt.runtime.boundsCheck(%s, $rt.builtin.byteLen(%s));", idx, x)
			e.Line("%s = $rt.builtin.stringByteAt(%s, %s);", dst, x, idx)
		default: // array/slice
			e.Line("%s = %s.get(%s);", dst, x, idx)
		}

	case *ssa.IndexAddr:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		idx := fc.value(instr.Index)
		// X is a pointer to an array or slice. Dereference through $get() for pointer types.
		if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok {
			e.Line("%s = %s.$get().addr(%s);", dst, x, idx)
		} else {
			e.Line("%s = %s.addr(%s);", dst, x, idx)
		}

	case *ssa.Jump:
		if inSwitch {
			block := instr.Block()
			// Emit phi assignments before jumping.
			fc.emitPhiAssignments(block, block.Succs[0])
			e.Line("$block = %d; break;", block.Succs[0].Index)
		}

	case *ssa.Lookup:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		idx := fc.value(instr.Index)

		if _, ok := instr.X.Type().Underlying().(*types.Map); ok {
			if instr.CommaOk {
				e.Line("{ const $r = $rt.builtin.mapLookup(%s, %s); %s = [$r.value, $r.ok]; }", x, idx, dst)
			} else {
				e.Line("%s = $rt.builtin.mapLookup(%s, %s).value;", dst, x, idx)
			}
		} else {
			// String index — UTF-8 byte semantics.
			e.Line("%s = $rt.builtin.stringByteAt(%s, %s);", dst, x, idx)
		}

	case *ssa.MakeChan:
		dst := fc.local(instr)
		size := fc.value(instr.Size)
		e.Line("%s = new $rt.channel.Channel(%s);", dst, size)

	case *ssa.MakeClosure:
		dst := fc.local(instr)
		fnRef := fc.value(instr.Fn.(*ssa.Function))
		var bindings []string
		for _, b := range instr.Bindings {
			bindings = append(bindings, fc.value(b))
		}
		e.Line("%s = %s.bind(null, %s);", dst, fnRef, strings.Join(bindings, ", "))

	case *ssa.MakeInterface:
		dst := fc.local(instr)
		typeID := fc.pc.typeMapper.TypeID(instr.X.Type())
		x := fc.value(instr.X)
		e.Line("%s = $rt.types.makeInterface(%s, %s);", dst, JsString(typeID), x)

	case *ssa.MakeMap:
		dst := fc.local(instr)
		mapType := instr.Type().Underlying().(*types.Map)
		keyKind := fc.pc.typeMapper.KeyKind(mapType.Key())
		e.Line("%s = $rt.builtin.makeMap(%s);", dst, JsString(keyKind))

	case *ssa.MakeSlice:
		dst := fc.local(instr)
		length := fc.value(instr.Len)
		capacity := fc.value(instr.Cap)
		elemType := instr.Type().Underlying().(*types.Slice).Elem()
		zero := fc.pc.typeMapper.ZeroExpr(elemType)
		e.Line("%s = $rt.builtin.makeSlice(%s, %s, %s);", dst, length, capacity, zero)

	case *ssa.MapUpdate:
		m := fc.value(instr.Map)
		k := fc.value(instr.Key)
		v := fc.value(instr.Value)
		e.Line("$rt.builtin.mapUpdate(%s, %s, %s);", m, k, v)

	case *ssa.Next:
		dst := fc.local(instr)
		iter := fc.value(instr.Iter)
		if instr.IsString {
			e.Line("%s = %s.next();", dst, iter)
		} else {
			e.Line("%s = %s.next();", dst, iter)
		}

	case *ssa.Panic:
		x := fc.value(instr.X)
		e.Line("$rt.runtime.panic(%s);", x)

	case *ssa.Phi:
		// Phi nodes are handled via assignments at the end of predecessor blocks.
		fc.phis = append(fc.phis, instr)

	case *ssa.Range:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		switch instr.X.Type().Underlying().(type) {
		case *types.Basic: // string range — use UTF-8 byte semantics
			e.Line("%s = $rt.builtin.stringRange(%s);", dst, x)
		case *types.Map:
			e.Line("%s = { $entries: [...%s.entries()], $pos: 0, next() { if (this.$pos >= this.$entries.length) return [false, null, null]; const [k, v] = this.$entries[this.$pos++]; return [true, k, v]; } };", dst, x)
		}

	case *ssa.Return:
		if len(instr.Results) == 0 {
			e.Line("return;")
		} else if len(instr.Results) == 1 {
			e.Line("return %s;", fc.value(instr.Results[0]))
		} else {
			var vals []string
			for _, r := range instr.Results {
				vals = append(vals, fc.value(r))
			}
			e.Line("return [%s];", strings.Join(vals, ", "))
		}

	case *ssa.RunDefers:
		e.Line("// RunDefers handled by try/finally")

	case *ssa.Select:
		fc.emitSelect(instr)

	case *ssa.Send:
		ch := fc.value(instr.Chan)
		x := fc.value(instr.X)
		e.Line("await %s.send(%s);", ch, x)

	case *ssa.Slice:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		low := "undefined"
		high := "undefined"
		max := "undefined"
		if instr.Low != nil {
			low = fc.value(instr.Low)
		}
		if instr.High != nil {
			high = fc.value(instr.High)
		}
		if instr.Max != nil {
			max = fc.value(instr.Max)
		}
		xType := instr.X.Type().Underlying()
		// Dereference pointer-to-array.
		if ptr, ok := xType.(*types.Pointer); ok {
			xType = ptr.Elem().Underlying()
			x = x + ".$get()"
		}
		switch xType.(type) {
		case *types.Basic: // string slice
			e.Line("%s = $rt.builtin.stringSlice(%s, %s, %s);", dst, x, low, high)
		default:
			e.Line("%s = $rt.builtin.sliceSlice(%s, %s, %s, %s);", dst, x, low, high, max)
		}

	case *ssa.Store:
		addr := fc.value(instr.Addr)
		val := fc.value(instr.Val)
		if needsClone(instr.Val.Type()) {
			e.Line("%s.$set($rt.builtin.cloneValue(%s));", addr, val)
		} else {
			e.Line("%s.$set(%s);", addr, val)
		}

	case *ssa.TypeAssert:
		dst := fc.local(instr)
		x := fc.value(instr.X)
		targetType := fc.pc.typeMapper.TypeID(instr.AssertedType)

		if instr.CommaOk {
			if types.IsInterface(instr.AssertedType) {
				e.Line("{ try { %s = [$rt.types.interfaceAssert(%s, %s).$value, true]; } catch(e) { %s = [null, false]; } }",
					dst, x, JsString(targetType), dst)
			} else {
				e.Line("%s = $rt.types.typeAssertOk(%s, %s);", dst, x, JsString(targetType))
			}
		} else {
			if types.IsInterface(instr.AssertedType) {
				e.Line("%s = $rt.types.interfaceAssert(%s, %s);", dst, x, JsString(targetType))
			} else {
				e.Line("%s = $rt.types.typeAssert(%s, %s);", dst, x, JsString(targetType))
			}
		}

	case *ssa.UnOp:
		fc.emitUnOp(instr)

	default:
		e.Comment("TODO: unhandled instruction: %T %s", instr, instr.String())
	}
}

// phiAssignmentStmts returns JS assignment statements for phi nodes when jumping from src to dst.
// Uses temporaries to avoid sequential assignment corruption (all phis read old values simultaneously).
func (fc *FunctionCompiler) phiAssignmentStmts(src, dst *ssa.BasicBlock) []string {
	type phiPair struct {
		dst, val string
	}
	var pairs []phiPair
	for _, instr := range dst.Instrs {
		phi, ok := instr.(*ssa.Phi)
		if !ok {
			break
		}
		for i, pred := range dst.Preds {
			if pred == src {
				pairs = append(pairs, phiPair{fc.local(phi), fc.value(phi.Edges[i])})
				break
			}
		}
	}
	if len(pairs) <= 1 {
		// Single phi or none — no reordering issue.
		var stmts []string
		for _, p := range pairs {
			stmts = append(stmts, fmt.Sprintf("%s = %s;", p.dst, p.val))
		}
		return stmts
	}
	// Check if any phi destination appears as a source in another phi.
	dstSet := make(map[string]bool)
	for _, p := range pairs {
		dstSet[p.dst] = true
	}
	needsTemps := false
	for _, p := range pairs {
		if dstSet[p.val] {
			needsTemps = true
			break
		}
	}
	if !needsTemps {
		var stmts []string
		for _, p := range pairs {
			stmts = append(stmts, fmt.Sprintf("%s = %s;", p.dst, p.val))
		}
		return stmts
	}
	// Use temporaries: save all sources, then assign.
	var stmts []string
	for i, p := range pairs {
		stmts = append(stmts, fmt.Sprintf("let $phi%d = %s;", i, p.val))
	}
	for i, p := range pairs {
		stmts = append(stmts, fmt.Sprintf("%s = $phi%d;", p.dst, i))
	}
	return stmts
}

// emitPhiAssignments emits variable assignments for phi nodes when jumping from src to dst.
func (fc *FunctionCompiler) emitPhiAssignments(src, dst *ssa.BasicBlock) {
	stmts := fc.phiAssignmentStmts(src, dst)
	for _, s := range stmts {
		fc.e.Line("%s", s)
	}
}

// emitCall generates JS for a function call.
func (fc *FunctionCompiler) emitCall(call *ssa.CallCommon) string {
	if call.IsInvoke() {
		// Interface method call — await if caller is async (method may be async).
		recv := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		awaitPrefix := ""
		if fc.needsAsync() {
			awaitPrefix = "await "
		}
		return fmt.Sprintf("%s$rt.types.methodCall(%s, %s, [%s])",
			awaitPrefix, recv, JsString(call.Method.Name()), strings.Join(args, ", "))
	}

	callee := call.StaticCallee()
	if callee != nil {
		return fc.emitStaticCall(callee, call.Args)
	}

	// Builtin call (println, len, cap, append, etc.).
	if builtin, ok := call.Value.(*ssa.Builtin); ok {
		return fc.emitBuiltinCall(builtin.Name(), call.Args)
	}

	// Dynamic call (function value) — await if caller is async.
	fnVal := fc.value(call.Value)
	var args []string
	for _, a := range call.Args {
		args = append(args, fc.value(a))
	}
	if fc.needsAsync() {
		return fmt.Sprintf("await %s(%s)", fnVal, strings.Join(args, ", "))
	}
	return fmt.Sprintf("%s(%s)", fnVal, strings.Join(args, ", "))
}

// emitStaticCall generates JS for a static function call.
func (fc *FunctionCompiler) emitStaticCall(callee *ssa.Function, args []ssa.Value) string {
	var jsArgs []string
	for _, a := range args {
		jsArgs = append(jsArgs, fc.value(a))
	}
	argStr := strings.Join(jsArgs, ", ")

	fullName := callee.String()

	// Map well-known functions to JS equivalents.
	switch fullName {
	case "println":
		return fmt.Sprintf("$rt.runtime.println(%s)", argStr)
	case "print":
		return fmt.Sprintf("$rt.runtime.print(%s)", argStr)
	case "len":
		return fmt.Sprintf("$rt.builtin.len(%s)", argStr)
	case "cap":
		return fmt.Sprintf("$rt.builtin.cap(%s)", argStr)
	case "append":
		return fmt.Sprintf("$rt.builtin.append(%s)", argStr)
	case "copy":
		return fmt.Sprintf("$rt.builtin.copy(%s)", argStr)
	case "delete":
		return fmt.Sprintf("$rt.builtin.mapDelete(%s)", argStr)
	case "close":
		if len(jsArgs) > 0 {
			return fmt.Sprintf("%s.close()", jsArgs[0])
		}
	case "panic":
		return fmt.Sprintf("$rt.runtime.panic(%s)", argStr)
	case "recover":
		return "$rt.runtime.recover()"
	case "spawn":
		return fc.emitSpawn(callee, args)
	}

	// Package-qualified call.
	fnName := functionJsName(callee)
	pkg := callee.Package()

	// Determine if the call needs to be awaited.
	awaitPrefix := ""
	if fc.needsAsync() && fc.pc.asyncFuncs[callee] {
		awaitPrefix = "await "
	}

	if pkg != nil && pkg.Pkg.Path() != fc.fn.Package().Pkg.Path() {
		// Cross-package call.
		pkgAlias := JsIdentifier(pkg.Pkg.Path())
		if shouldSkipPackage(pkg.Pkg.Path()) {
			// Runtime function — map to JS runtime.
			return fc.mapRuntimeCall(fullName, argStr)
		}
		return fmt.Sprintf("%s%s.%s(%s)", awaitPrefix, pkgAlias, fnName, argStr)
	}

	// Same-package call.
	return fmt.Sprintf("%s%s(%s)", awaitPrefix, fnName, argStr)
}

// mapRuntimeCall maps Go runtime function calls to JS runtime equivalents.
func (fc *FunctionCompiler) mapRuntimeCall(fullName string, argStr string) string {
	switch {
	case strings.HasPrefix(fullName, "fmt.Println"), fullName == "fmt.Println":
		return fmt.Sprintf("$rt.runtime.println(%s)", argStr)
	case strings.HasPrefix(fullName, "fmt.Printf"), fullName == "fmt.Printf":
		return fmt.Sprintf("$rt.runtime.print(%s)", argStr)
	case strings.HasPrefix(fullName, "fmt.Sprintf"):
		return fmt.Sprintf("String(%s)", argStr)
	default:
		return fmt.Sprintf("/* runtime: %s */ undefined", fullName)
	}
}

// emitBuiltinCall handles calls to Go builtin functions.
func (fc *FunctionCompiler) emitBuiltinCall(name string, args []ssa.Value) string {
	var jsArgs []string
	for _, a := range args {
		jsArgs = append(jsArgs, fc.value(a))
	}
	argStr := strings.Join(jsArgs, ", ")

	switch name {
	case "println":
		return fmt.Sprintf("$rt.runtime.println(%s)", argStr)
	case "print":
		return fmt.Sprintf("$rt.runtime.print(%s)", argStr)
	case "len":
		return fmt.Sprintf("$rt.builtin.len(%s)", argStr)
	case "cap":
		return fmt.Sprintf("$rt.builtin.cap(%s)", argStr)
	case "append":
		// SSA append: first arg is the slice, second arg is also a slice (append(s1, s2...)).
		if len(args) == 2 {
			_, isSlice := args[1].Type().Underlying().(*types.Slice)
			if isSlice {
				return fmt.Sprintf("$rt.builtin.appendSlice(%s, %s)", jsArgs[0], jsArgs[1])
			}
			// append([]byte, string...) — spread string bytes.
			if basic, ok := args[1].Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
				return fmt.Sprintf("$rt.builtin.appendString(%s, %s)", jsArgs[0], jsArgs[1])
			}
		}
		return fmt.Sprintf("$rt.builtin.append(%s)", argStr)
	case "copy":
		return fmt.Sprintf("$rt.builtin.copy(%s)", argStr)
	case "delete":
		return fmt.Sprintf("$rt.builtin.mapDelete(%s)", argStr)
	case "close":
		if len(jsArgs) > 0 {
			return fmt.Sprintf("%s.close()", jsArgs[0])
		}
		return "undefined"
	case "panic":
		return fmt.Sprintf("$rt.runtime.panic(%s)", argStr)
	case "recover":
		return "$rt.runtime.recover()"
	case "real":
		if len(jsArgs) > 0 {
			return fmt.Sprintf("(%s).re", jsArgs[0])
		}
		return "0"
	case "imag":
		if len(jsArgs) > 0 {
			return fmt.Sprintf("(%s).im", jsArgs[0])
		}
		return "0"
	case "complex":
		if len(jsArgs) >= 2 {
			return fmt.Sprintf("{ re: %s, im: %s }", jsArgs[0], jsArgs[1])
		}
		return "{ re: 0, im: 0 }"
	case "min":
		if len(jsArgs) == 2 {
			return fmt.Sprintf("Math.min(%s, %s)", jsArgs[0], jsArgs[1])
		}
		return fmt.Sprintf("Math.min(%s)", argStr)
	case "max":
		if len(jsArgs) == 2 {
			return fmt.Sprintf("Math.max(%s, %s)", jsArgs[0], jsArgs[1])
		}
		return fmt.Sprintf("Math.max(%s)", argStr)
	case "make":
		// Make is handled by MakeChan, MakeMap, MakeSlice SSA nodes, not as a call.
		return fmt.Sprintf("/* make(%s) */", argStr)
	case "new":
		return fmt.Sprintf("/* new(%s) */", argStr)
	default:
		return fmt.Sprintf("/* builtin %s(%s) */", name, argStr)
	}
}

// emitUnOp handles unary operations.
func (fc *FunctionCompiler) emitUnOp(instr *ssa.UnOp) {
	dst := fc.local(instr)
	x := fc.value(instr.X)

	switch instr.Op {
	case token.NOT:
		fc.e.Line("%s = !%s;", dst, x)
	case token.SUB:
		fc.e.Line("%s = -%s;", dst, x)
	case token.XOR:
		fc.e.Line("%s = ~%s;", dst, x)
	case token.MUL: // pointer dereference
		fc.e.Line("%s = %s.$get();", dst, x)
	case token.ARROW: // channel receive
		if instr.CommaOk {
			fc.e.Line("{ const $r = await %s.recv(); %s = [$r.value, $r.ok]; }", x, dst)
		} else {
			fc.e.Line("%s = (await %s.recv()).value;", dst, x)
		}
	default:
		fc.e.Comment("TODO: unhandled UnOp: %s", instr.Op)
	}
}

// emitDefer generates JS for a defer statement.
func (fc *FunctionCompiler) emitDefer(instr *ssa.Defer) {
	call := &instr.Call
	if call.IsInvoke() {
		recv := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		fc.e.Line("$defers.push(() => $rt.types.methodCall(%s, %s, [%s]));",
			recv, JsString(call.Method.Name()), strings.Join(args, ", "))
	} else if builtin, ok := call.Value.(*ssa.Builtin); ok {
		// Builtin call (println, etc.).
		callExpr := fc.emitBuiltinCall(builtin.Name(), call.Args)
		fc.e.Line("$defers.push(() => %s);", callExpr)
	} else if _, ok := call.Value.(*ssa.MakeClosure); ok {
		// Closure — use the bound closure variable.
		fnVal := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		fc.e.Line("$defers.push(() => %s(%s));", fnVal, strings.Join(args, ", "))
	} else if callee := call.StaticCallee(); callee != nil {
		callExpr := fc.emitStaticCall(callee, call.Args)
		fc.e.Line("$defers.push(() => %s);", callExpr)
	} else {
		fnVal := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		fc.e.Line("$defers.push(() => %s(%s));", fnVal, strings.Join(args, ", "))
	}
}

// emitGo generates JS for a go statement (goroutine spawn).
func (fc *FunctionCompiler) emitGo(instr *ssa.Go) {
	call := &instr.Call
	if call.IsInvoke() {
		recv := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		fc.e.Line("$rt.goroutine.spawn(async () => $rt.types.methodCall(%s, %s, [%s]));",
			recv, JsString(call.Method.Name()), strings.Join(args, ", "))
	} else if _, isClosure := call.Value.(*ssa.MakeClosure); isClosure {
		// For closures, spawn the closure variable (which has bindings already applied).
		fnVal := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		fc.e.Line("$rt.goroutine.spawn(async () => %s(%s));", fnVal, strings.Join(args, ", "))
	} else if callee := call.StaticCallee(); callee != nil {
		callExpr := fc.emitStaticCall(callee, call.Args)
		fc.e.Line("$rt.goroutine.spawn(async () => %s);", callExpr)
	} else {
		fnVal := fc.value(call.Value)
		var args []string
		for _, a := range call.Args {
			args = append(args, fc.value(a))
		}
		fc.e.Line("$rt.goroutine.spawn(async () => %s(%s));", fnVal, strings.Join(args, ", "))
	}
}

// emitSpawn generates JS for spawn — creates a new isolated browser domain.
//
// The spawn builtin's SSA signature is func(interface{}, ...interface{}).
// Args[0] is MakeInterface(targetFn), Args[1] is a []interface{} slice
// created by the SSA builder. We walk the SSA graph to recover the
// concrete typed values (same approach as the native backend).
func (fc *FunctionCompiler) emitSpawn(callee *ssa.Function, args []ssa.Value) string {
	if len(args) < 1 {
		return "/* spawn: no function argument */ undefined"
	}

	// Check if the first positional argument (fn) is a transport string.
	// SSA: spawn("pipe", worker, ch) → args[0]=MakeInterface("pipe"), args[1]=variadic
	transport := "local"
	hasTransport := false
	if ts, ok := extractTransportStringJS(args[0]); ok {
		transport = ts
		hasTransport = true
	}

	// Validate transport.
	switch transport {
	case "local", "pipe":
		// OK.
	default:
		if len(transport) > 6 && (transport[:6] == "tcp://" || transport[:5] == "ws://" || transport[:6] == "wss://") {
			return fmt.Sprintf("/* spawn: network transport %q not yet implemented */ undefined", transport)
		}
		return fmt.Sprintf("/* spawn: unknown transport %q */ undefined", transport)
	}

	// Extract all concrete values from the variadic slice.
	allConcrete := extractSpawnSSAArgsJS(args)

	// When transport is present, the function is allConcrete[0], args are [1:].
	var fnArg ssa.Value
	var concreteArgs []ssa.Value
	if hasTransport {
		if len(allConcrete) < 1 {
			return "/* spawn: no function argument after transport string */ undefined"
		}
		fnArg = allConcrete[0]
		concreteArgs = allConcrete[1:]
	} else {
		fnArg = args[0]
		if mi, ok := fnArg.(*ssa.MakeInterface); ok {
			fnArg = mi.X
		}
		concreteArgs = allConcrete
	}

	// Unwrap MakeInterface on fnArg.
	if mi, ok := fnArg.(*ssa.MakeInterface); ok {
		fnArg = mi.X
	}
	fnVal := fc.value(fnArg)

	// Find the actual target function to determine its signature.
	var targetFn *ssa.Function
	switch v := fnArg.(type) {
	case *ssa.Function:
		targetFn = v
	case *ssa.MakeClosure:
		if fn, ok := v.Fn.(*ssa.Function); ok {
			targetFn = fn
		}
	}

	// Verify argument count and types match the target function.
	if targetFn != nil {
		sig := targetFn.Signature
		if len(concreteArgs) != sig.Params().Len() {
			return fmt.Sprintf("/* spawn: %s expects %d arguments, got %d */ undefined",
				targetFn.Name(), sig.Params().Len(), len(concreteArgs))
		}
		for i, arg := range concreteArgs {
			paramType := sig.Params().At(i).Type()
			argType := arg.Type()
			if !types.Identical(argType.Underlying(), paramType.Underlying()) {
				return fmt.Sprintf("/* spawn: argument %d has type %s, %s expects %s */ undefined",
					i+1, argType, targetFn.Name(), paramType)
			}
		}
	}

	var jsArgs []string
	for i, arg := range concreteArgs {
		jsVal := fc.value(arg)
		// Emit type-appropriate JS serialization for the spawn boundary.
		if targetFn != nil && i < targetFn.Signature.Params().Len() {
			jsVal = fc.emitSpawnArgSerialize(arg, targetFn.Signature.Params().At(i).Type(), jsVal)
		}
		jsArgs = append(jsArgs, jsVal)
	}

	awaitPrefix := ""
	if fc.needsAsync() {
		awaitPrefix = "await "
	}

	if len(jsArgs) > 0 {
		return fmt.Sprintf("%s$rt.domain.spawn(async () => %s(%s))", awaitPrefix, fnVal, strings.Join(jsArgs, ", "))
	}
	return fmt.Sprintf("%s$rt.domain.spawn(async () => %s())", awaitPrefix, fnVal)
}

// emitSpawnArgSerialize wraps a JS value in the appropriate serialization
// for crossing a spawn boundary. Slices become Array copies, maps become
// Object copies. Value types pass through unchanged.
func (fc *FunctionCompiler) emitSpawnArgSerialize(arg ssa.Value, goType types.Type, jsVal string) string {
	switch goType.Underlying().(type) {
	case *types.Slice:
		// Deep copy the slice so child gets independent data.
		return fmt.Sprintf("[...%s]", jsVal)
	case *types.Map:
		// Shallow copy the map so child gets independent entries.
		return fmt.Sprintf("Object.assign({}, %s)", jsVal)
	default:
		return jsVal
	}
}

// extractSpawnSSAArgsJS recovers concrete values from spawn's variadic slice.
// Same logic as extractSpawnSSAArgs in compiler/spawn.go but accessible from
// the jsbackend package.
// extractTransportStringJS checks if an SSA value is a string constant
// (possibly wrapped in MakeInterface) and returns it.
func extractTransportStringJS(v ssa.Value) (string, bool) {
	if mi, ok := v.(*ssa.MakeInterface); ok {
		v = mi.X
	}
	c, ok := v.(*ssa.Const)
	if !ok {
		return "", false
	}
	if c.Value == nil || c.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(c.Value), true
}

func extractSpawnSSAArgsJS(args []ssa.Value) []ssa.Value {
	if len(args) < 2 {
		return nil
	}

	variadicArg := args[1]
	slice, ok := variadicArg.(*ssa.Slice)
	if !ok {
		return nil
	}
	alloc, ok := slice.X.(*ssa.Alloc)
	if !ok {
		return nil
	}
	refs := alloc.Referrers()
	if refs == nil {
		return nil
	}

	type indexedValue struct {
		index int64
		value ssa.Value
	}
	var indexed []indexedValue

	for _, ref := range *refs {
		ia, ok := ref.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		iaRefs := ia.Referrers()
		if iaRefs == nil {
			continue
		}
		for _, iaRef := range *iaRefs {
			store, ok := iaRef.(*ssa.Store)
			if !ok || store.Addr != ia {
				continue
			}
			idx := int64(0)
			if constIdx, ok := ia.Index.(*ssa.Const); ok {
				if v, ok := constant.Int64Val(constIdx.Value); ok {
					idx = v
				}
			}
			val := store.Val
			if mi, ok := val.(*ssa.MakeInterface); ok {
				val = mi.X
			}
			indexed = append(indexed, indexedValue{idx, val})
		}
	}

	for i := 1; i < len(indexed); i++ {
		for j := i; j > 0 && indexed[j].index < indexed[j-1].index; j-- {
			indexed[j], indexed[j-1] = indexed[j-1], indexed[j]
		}
	}

	result := make([]ssa.Value, len(indexed))
	for i, iv := range indexed {
		result[i] = iv.value
	}
	return result
}

// emitSelect generates JS for a select statement.
func (fc *FunctionCompiler) emitSelect(instr *ssa.Select) {
	dst := fc.local(instr)

	var cases []string
	for i, state := range instr.States {
		ch := fc.value(state.Chan)
		if state.Dir == types.SendOnly {
			val := fc.value(state.Send)
			cases = append(cases, fmt.Sprintf("{ ch: %s, dir: 'send', value: %s, id: %d }", ch, val, i))
		} else {
			cases = append(cases, fmt.Sprintf("{ ch: %s, dir: 'recv', id: %d }", ch, i))
		}
	}

	hasDefault := "false"
	if !instr.Blocking {
		hasDefault = "true"
	}

	fc.e.Line("%s = await $rt.channel.select([%s], %s);",
		dst, strings.Join(cases, ", "), hasDefault)
}

// emitConvert handles type conversions.
func (fc *FunctionCompiler) emitConvert(instr *ssa.Convert) string {
	x := fc.value(instr.X)
	fromType := instr.X.Type().Underlying()
	toType := instr.Type().Underlying()

	fromBasic, fromIsBasic := fromType.(*types.Basic)
	toBasic, toIsBasic := toType.(*types.Basic)

	// String conversions.
	if toIsBasic && toBasic.Kind() == types.String {
		if fromIsBasic && fromBasic.Info()&types.IsInteger != 0 {
			return fmt.Sprintf("String.fromCodePoint(%s)", x)
		}
		if _, ok := fromType.(*types.Slice); ok {
			return fmt.Sprintf("$rt.builtin.bytesToString(%s)", x)
		}
	}

	if fromIsBasic && fromBasic.Kind() == types.String {
		if _, ok := toType.(*types.Slice); ok {
			return fmt.Sprintf("$rt.builtin.stringToBytes(%s)", x)
		}
	}

	// Numeric conversions.
	if fromIsBasic && toIsBasic {
		from64 := fromBasic.Kind() == types.Int64 || fromBasic.Kind() == types.Uint64
		to64 := toBasic.Kind() == types.Int64 || toBasic.Kind() == types.Uint64

		// BigInt (64-bit) → Number (float/smaller int).
		if from64 && toBasic.Info()&types.IsFloat != 0 {
			return fmt.Sprintf("Number(%s)", x)
		}
		if from64 && toBasic.Info()&types.IsInteger != 0 && !to64 {
			return fc.intTruncate(fmt.Sprintf("Number(%s)", x), toBasic)
		}

		// Number → BigInt (64-bit).
		if !from64 && to64 {
			if toBasic.Kind() == types.Uint64 {
				return fmt.Sprintf("BigInt.asUintN(64, BigInt(%s))", x)
			}
			return fmt.Sprintf("BigInt(%s)", x)
		}

		// float → int (both Number).
		if toBasic.Info()&types.IsFloat != 0 && fromBasic.Info()&types.IsInteger != 0 {
			if from64 {
				return fmt.Sprintf("Number(%s)", x)
			}
			return x // JS numbers are already float64
		}
		if toBasic.Info()&types.IsInteger != 0 && fromBasic.Info()&types.IsFloat != 0 {
			if to64 {
				return fmt.Sprintf("BigInt(Math.trunc(%s))", x)
			}
			return fmt.Sprintf("Math.trunc(%s)", x)
		}
		if toBasic.Info()&types.IsInteger != 0 && fromBasic.Info()&types.IsInteger != 0 {
			return fc.intTruncate(x, toBasic)
		}
	}

	return x
}

// intTruncate wraps a value to fit a specific integer type.
func (fc *FunctionCompiler) intTruncate(x string, t *types.Basic) string {
	switch t.Kind() {
	case types.Int8:
		return fmt.Sprintf("((%s << 24) >> 24)", x)
	case types.Int16:
		return fmt.Sprintf("((%s << 16) >> 16)", x)
	case types.Int32:
		return fmt.Sprintf("(%s | 0)", x)
	case types.Uint8:
		return fmt.Sprintf("(%s & 0xFF)", x)
	case types.Uint16:
		return fmt.Sprintf("(%s & 0xFFFF)", x)
	case types.Uint32:
		return fmt.Sprintf("(%s >>> 0)", x)
	case types.Int64:
		return fmt.Sprintf("BigInt.asIntN(64, BigInt(%s))", x)
	case types.Uint64:
		return fmt.Sprintf("BigInt.asUintN(64, BigInt(%s))", x)
	default:
		return x
	}
}

// value returns the JS expression for an SSA value.
func (fc *FunctionCompiler) value(v ssa.Value) string {
	if v == nil {
		return "undefined"
	}

	// Constants.
	if c, ok := v.(*ssa.Const); ok {
		return fc.constValue(c)
	}

	// Functions.
	if fn, ok := v.(*ssa.Function); ok {
		return functionJsName(fn)
	}

	// Globals.
	if g, ok := v.(*ssa.Global); ok {
		pkg := g.Package()
		if pkg != nil && pkg.Pkg.Path() != fc.fn.Package().Pkg.Path() {
			return fmt.Sprintf("%s.%s", JsIdentifier(pkg.Pkg.Path()), JsIdentifier(g.Name()))
		}
		return JsIdentifier(g.Name())
	}

	// Builtins.
	if _, ok := v.(*ssa.Builtin); ok {
		return "/* builtin */"
	}

	// Local variable.
	if name, ok := fc.locals[v]; ok {
		return name
	}

	return fmt.Sprintf("/* unknown: %T */", v)
}

// local returns the JS variable name for an SSA value (creating one if needed).
func (fc *FunctionCompiler) local(v ssa.Value) string {
	if name, ok := fc.locals[v]; ok {
		return name
	}
	name := fc.freshVar(v.Name())
	fc.locals[v] = name
	return name
}

// is64bit returns true if the type is int64 or uint64.
func is64bit(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == types.Int64 || basic.Kind() == types.Uint64
}

// constValue returns the JS literal for an SSA constant.
func (fc *FunctionCompiler) constValue(c *ssa.Const) string {
	if c.Value == nil {
		// Typed nil or zero value.
		if is64bit(c.Type()) {
			return "0n"
		}
		return fc.pc.typeMapper.ZeroExpr(c.Type())
	}

	switch c.Value.Kind() {
	case constant.Bool:
		if constant.BoolVal(c.Value) {
			return "true"
		}
		return "false"
	case constant.Int:
		// 64-bit integers use BigInt (suffix n) for full precision.
		if is64bit(c.Type()) {
			if v, exact := constant.Int64Val(c.Value); exact {
				return fmt.Sprintf("%dn", v)
			}
			if v, exact := constant.Uint64Val(c.Value); exact {
				return fmt.Sprintf("%dn", v)
			}
			return c.Value.ExactString() + "n"
		}
		if v, exact := constant.Int64Val(c.Value); exact {
			return fmt.Sprintf("%d", v)
		}
		if v, exact := constant.Uint64Val(c.Value); exact {
			return fmt.Sprintf("%d", v)
		}
		return c.Value.ExactString()
	case constant.Float:
		f, _ := constant.Float64Val(c.Value)
		return fmt.Sprintf("%g", f)
	case constant.String:
		return JsString(constant.StringVal(c.Value))
	case constant.Complex:
		re, _ := constant.Float64Val(constant.Real(c.Value))
		im, _ := constant.Float64Val(constant.Imag(c.Value))
		return fmt.Sprintf("{ re: %g, im: %g }", re, im)
	default:
		return "null"
	}
}

// intTypeInfo returns (bitSize, isUnsigned) for an integer type.
func intTypeInfo(t types.Type) (int, bool) {
	basic, ok := t.Underlying().(*types.Basic)
	if !ok {
		return 0, false
	}
	switch basic.Kind() {
	case types.Uint8:
		return 8, true
	case types.Uint16:
		return 16, true
	case types.Uint32:
		return 32, true
	case types.Uint64:
		return 64, true
	case types.Uint, types.Uintptr:
		return 64, true // Go int is 64-bit; in JS we treat uint as 64-bit
	case types.Int8:
		return 8, false
	case types.Int16:
		return 16, false
	case types.Int32:
		return 32, false
	case types.Int64:
		return 64, false
	case types.Int:
		return 64, false
	}
	return 0, false
}

// wrapUint wraps an expression to the correct unsigned bit width.
func wrapUint(expr string, bits int) string {
	switch bits {
	case 8:
		return fmt.Sprintf("((%s) & 0xFF)", expr)
	case 16:
		return fmt.Sprintf("((%s) & 0xFFFF)", expr)
	case 32:
		return fmt.Sprintf("((%s) >>> 0)", expr)
	case 64:
		return fmt.Sprintf("BigInt.asUintN(64, %s)", expr)
	default:
		return expr
	}
}

// binOp returns a function that generates the JS binary expression.
func (fc *FunctionCompiler) binOp(op token.Token, t types.Type) func(x, y string) string {
	isString := false
	if basic, ok := t.Underlying().(*types.Basic); ok {
		isString = basic.Info()&types.IsString != 0
	}

	bits, unsigned := intTypeInfo(t)

	// 64-bit integer operations use BigInt for full precision.
	if bits == 64 {
		wrap := "BigInt.asUintN"
		if !unsigned {
			wrap = "BigInt.asIntN"
		}
		switch op {
		case token.ADD:
			return func(x, y string) string { return fmt.Sprintf("%s(64, %s + %s)", wrap, x, y) }
		case token.SUB:
			return func(x, y string) string { return fmt.Sprintf("%s(64, %s - %s)", wrap, x, y) }
		case token.MUL:
			return func(x, y string) string { return fmt.Sprintf("%s(64, %s * %s)", wrap, x, y) }
		case token.QUO:
			return func(x, y string) string { return fmt.Sprintf("%s(64, %s / %s)", wrap, x, y) }
		case token.REM:
			return func(x, y string) string { return fmt.Sprintf("(%s %% %s)", x, y) }
		case token.AND:
			return func(x, y string) string { return fmt.Sprintf("(%s & %s)", x, y) }
		case token.OR:
			return func(x, y string) string { return fmt.Sprintf("(%s | %s)", x, y) }
		case token.XOR:
			return func(x, y string) string { return fmt.Sprintf("(%s ^ %s)", x, y) }
		case token.SHL:
			// Shift amount may be a Number (e.g. uint); coerce to BigInt.
			return func(x, y string) string { return fmt.Sprintf("%s(64, %s << BigInt(%s))", wrap, x, y) }
		case token.SHR:
			if unsigned {
				return func(x, y string) string { return fmt.Sprintf("(%s >> BigInt(%s))", x, y) }
			}
			return func(x, y string) string {
				return fmt.Sprintf("BigInt.asIntN(64, %s >> BigInt(%s))", x, y)
			}
		case token.AND_NOT:
			return func(x, y string) string { return fmt.Sprintf("(%s & ~%s)", x, y) }
		case token.EQL:
			return func(x, y string) string { return fmt.Sprintf("(%s === %s)", x, y) }
		case token.NEQ:
			return func(x, y string) string { return fmt.Sprintf("(%s !== %s)", x, y) }
		case token.LSS:
			return func(x, y string) string { return fmt.Sprintf("(%s < %s)", x, y) }
		case token.LEQ:
			return func(x, y string) string { return fmt.Sprintf("(%s <= %s)", x, y) }
		case token.GTR:
			return func(x, y string) string { return fmt.Sprintf("(%s > %s)", x, y) }
		case token.GEQ:
			return func(x, y string) string { return fmt.Sprintf("(%s >= %s)", x, y) }
		}
	}

	switch op {
	case token.ADD:
		if isString {
			return func(x, y string) string { return fmt.Sprintf("(%s + %s)", x, y) }
		}
		if unsigned && bits <= 32 {
			return func(x, y string) string { return wrapUint(fmt.Sprintf("%s + %s", x, y), bits) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s + %s)", x, y) }
	case token.SUB:
		if unsigned && bits <= 32 {
			return func(x, y string) string { return wrapUint(fmt.Sprintf("%s - %s", x, y), bits) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s - %s)", x, y) }
	case token.MUL:
		if unsigned && bits == 32 {
			// Math.imul gives low 32 bits of integer multiply, then >>> 0 for unsigned.
			return func(x, y string) string { return fmt.Sprintf("(Math.imul(%s, %s) >>> 0)", x, y) }
		}
		if unsigned && bits < 32 {
			return func(x, y string) string { return wrapUint(fmt.Sprintf("%s * %s", x, y), bits) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s * %s)", x, y) }
	case token.QUO:
		// Integer division in Go truncates toward zero.
		if basic, ok := t.Underlying().(*types.Basic); ok && basic.Info()&types.IsInteger != 0 {
			if unsigned && bits <= 32 {
				return func(x, y string) string { return wrapUint(fmt.Sprintf("Math.trunc(%s / %s)", x, y), bits) }
			}
			return func(x, y string) string { return fmt.Sprintf("Math.trunc(%s / %s)", x, y) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s / %s)", x, y) }
	case token.REM:
		if unsigned && bits <= 32 {
			return func(x, y string) string { return wrapUint(fmt.Sprintf("%s %% %s", x, y), bits) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s %% %s)", x, y) }
	case token.AND:
		if unsigned && bits == 32 {
			return func(x, y string) string { return fmt.Sprintf("((%s & %s) >>> 0)", x, y) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s & %s)", x, y) }
	case token.OR:
		if unsigned && bits == 32 {
			return func(x, y string) string { return fmt.Sprintf("((%s | %s) >>> 0)", x, y) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s | %s)", x, y) }
	case token.XOR:
		if unsigned && bits == 32 {
			return func(x, y string) string { return fmt.Sprintf("((%s ^ %s) >>> 0)", x, y) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s ^ %s)", x, y) }
	case token.SHL:
		if unsigned && bits <= 32 {
			return func(x, y string) string { return wrapUint(fmt.Sprintf("%s << %s", x, y), bits) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s << %s)", x, y) }
	case token.SHR:
		if unsigned {
			if bits == 32 {
				return func(x, y string) string { return fmt.Sprintf("(%s >>> %s)", x, y) }
			}
			if bits < 32 {
				return func(x, y string) string { return fmt.Sprintf("((%s & %s) >> %s)", x, wrapMask(bits), y) }
			}
			return func(x, y string) string { return fmt.Sprintf("(%s >>> %s)", x, y) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s >> %s)", x, y) }
	case token.AND_NOT:
		if unsigned && bits == 32 {
			return func(x, y string) string { return fmt.Sprintf("((%s & ~%s) >>> 0)", x, y) }
		}
		return func(x, y string) string { return fmt.Sprintf("(%s & ~%s)", x, y) }
	case token.EQL:
		return func(x, y string) string { return fmt.Sprintf("(%s === %s)", x, y) }
	case token.NEQ:
		return func(x, y string) string { return fmt.Sprintf("(%s !== %s)", x, y) }
	case token.LSS:
		return func(x, y string) string { return fmt.Sprintf("(%s < %s)", x, y) }
	case token.LEQ:
		return func(x, y string) string { return fmt.Sprintf("(%s <= %s)", x, y) }
	case token.GTR:
		return func(x, y string) string { return fmt.Sprintf("(%s > %s)", x, y) }
	case token.GEQ:
		return func(x, y string) string { return fmt.Sprintf("(%s >= %s)", x, y) }
	default:
		return func(x, y string) string { return fmt.Sprintf("/* TODO: %s */ (%s ? %s)", op, x, y) }
	}
}

func wrapMask(bits int) string {
	switch bits {
	case 8:
		return "0xFF"
	case 16:
		return "0xFFFF"
	default:
		return "0xFFFFFFFF"
	}
}

// needsClone returns true if a Go type needs deep-cloning when stored via pointer.
// Arrays and structs are value types in Go; in JS they're reference types.
func needsClone(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Array:
		return true
	case *types.Struct:
		return true
	}
	return false
}

// needsAsync checks if this function needs to be declared async.
// Uses the precomputed transitive async set from ProgramCompiler.
func (fc *FunctionCompiler) needsAsync() bool {
	return fc.pc.asyncFuncs[fc.fn]
}

// fieldJsName returns the JS property name for a struct field by index.
func fieldJsName(t types.Type, index int) string {
	switch t := t.Underlying().(type) {
	case *types.Struct:
		if index < t.NumFields() {
			return JsIdentifier(t.Field(index).Name())
		}
	}
	return fmt.Sprintf("$field_%d", index)
}
