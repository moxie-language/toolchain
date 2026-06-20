package mxssa

import (
	"go/constant"
	"go/token"

	"moxie/syntax"
	"moxie/typecheck"
)

// ─── Package builder ──────────────────────────────────────────────────────────

// CreatePackage builds the SSA Package for pkg from its type-checked syntax files.
func (prog *Program) CreatePackage(pkg *typecheck.Package, files []*syntax.File, info *typecheck.Info) *Package {
	p := &Package{
		Prog:    prog,
		Pkg:     pkg,
		Members: map[string]Member{},
	}
	prog.packages[pkg] = p
	prog.imported[pkg.Path()] = p

	pb := &pkgBuilder{pkg: p, info: info, prog: prog}

	// Pass 1: register top-level members (no bodies yet).
	for _, f := range files {
		pb.registerFile(f)
	}

	// Pass 2: build function bodies.
	for _, f := range files {
		pb.buildFile(f)
	}

	return p
}

// pkgBuilder builds one Package.
type pkgBuilder struct {
	pkg  *Package
	info *typecheck.Info
	prog *Program
}

func (pb *pkgBuilder) registerFile(f *syntax.File) {
	for _, d := range f.DeclList {
		switch d := d.(type) {
		case *syntax.FuncDecl:
			pb.registerFunc(d)
		case *syntax.VarDecl:
			pb.registerVar(d)
		case *syntax.TypeDecl:
			pb.registerType(d)
		case *syntax.ConstDecl:
			pb.registerConst(d)
		}
	}
}

func (pb *pkgBuilder) registerFunc(d *syntax.FuncDecl) {
	if d.Name.Value == "init" {
		return // init functions handled separately
	}
	var obj *typecheck.Func
	if o := pb.pkg.Pkg.Scope().Lookup(d.Name.Value); o != nil {
		obj, _ = o.(*typecheck.Func)
	}
	var sig *typecheck.Signature
	if obj != nil {
		sig, _ = obj.Type().(*typecheck.Signature)
	}
	fn := &Function{
		name:      d.Name.Value,
		object:    obj,
		Signature: sig,
		pos:       posFromSyntax(d.Name.Pos()),
		Pkg:       pb.pkg,
		Prog:      pb.prog,
	}
	pb.pkg.Members[fn.name] = fn
}

func (pb *pkgBuilder) registerVar(d *syntax.VarDecl) {
	for _, name := range d.NameList {
		var obj *typecheck.Var
		if o := pb.pkg.Pkg.Scope().Lookup(name.Value); o != nil {
			obj, _ = o.(*typecheck.Var)
		}
		var typ typecheck.Type
		if obj != nil {
			typ = obj.Type()
		}
		g := &Global{
			name:   name.Value,
			object: obj,
			typ:    typecheck.NewPointer(typ), // globals are pointers in SSA
			pos:    posFromSyntax(name.Pos()),
			pkg:    pb.pkg,
		}
		pb.pkg.Members[name.Value] = g
	}
}

func (pb *pkgBuilder) registerType(d *syntax.TypeDecl) {
	var obj *typecheck.TypeName
	if o := pb.pkg.Pkg.Scope().Lookup(d.Name.Value); o != nil {
		obj, _ = o.(*typecheck.TypeName)
	}
	if obj == nil {
		return
	}
	t := &Type_{object: obj, pkg: pb.pkg}
	pb.pkg.Members[d.Name.Value] = t
}

func (pb *pkgBuilder) registerConst(d *syntax.ConstDecl) {
	for _, name := range d.NameList {
		var obj *typecheck.Const
		if o := pb.pkg.Pkg.Scope().Lookup(name.Value); o != nil {
			obj, _ = o.(*typecheck.Const)
		}
		if obj == nil {
			continue
		}
		var cval constant.Value
		if obj.Val() != nil {
			cval, _ = obj.Val().(constant.Value)
		}
		c := &NamedConst{
			object: obj,
			Value:  &Const{typ: obj.Type(), val: cval},
			pkg:    pb.pkg,
		}
		pb.pkg.Members[name.Value] = c
	}
}

func (pb *pkgBuilder) buildFile(f *syntax.File) {
	for _, d := range f.DeclList {
		if fd, ok := d.(*syntax.FuncDecl); ok {
			pb.buildFunc(fd)
		}
	}
}

func (pb *pkgBuilder) buildFunc(d *syntax.FuncDecl) {
	if d.Body == nil {
		return // external function
	}
	fn, _ := pb.pkg.Members[d.Name.Value].(*Function)
	if fn == nil {
		return
	}
	fb := newFuncBuilder(fn, pb.info)
	fb.buildBody(d)
}

// ─── Function builder ─────────────────────────────────────────────────────────

// funcBuilder builds one Function body.
type funcBuilder struct {
	fn           *Function
	info         *typecheck.Info
	currentBlock *BasicBlock
	vars         map[typecheck.Object]*Alloc // local variable allocas
	namedResults []*Alloc
	counter      int // for generating unique SSA names

	// Loop/defer state
	loops    []loopState
	deferred int // number of pending defer statements
}

type loopState struct {
	body  *BasicBlock
	post  *BasicBlock
	done  *BasicBlock
}

func newFuncBuilder(fn *Function, info *typecheck.Info) *funcBuilder {
	return &funcBuilder{
		fn:   fn,
		info: info,
		vars: map[typecheck.Object]*Alloc{},
	}
}

func (fb *funcBuilder) newBlock(comment string) *BasicBlock {
	return NewBasicBlock(fb.fn, comment)
}

func (fb *funcBuilder) emit(instr Instruction) {
	if fb.currentBlock == nil {
		return
	}
	instr.setBlock(fb.currentBlock)
	fb.currentBlock.Instrs = append(fb.currentBlock.Instrs, instr)
}

func (fb *funcBuilder) nextName() string {
	fb.counter++
	return "t" + itoa(fb.counter)
}

func (fb *funcBuilder) emitValue(instr interface {
	Instruction
	Value
}) Value {
	instr.setName(fb.nextName())
	fb.emit(instr)
	return instr
}

// buildBody initializes parameters and builds the function body.
func (fb *funcBuilder) buildBody(d *syntax.FuncDecl) {
	entry := fb.newBlock("entry")
	fb.currentBlock = entry
	fb.fn.currentBlock = entry

	sig := fb.fn.Signature
	if sig == nil {
		return
	}

	// Allocate parameters.
	params := sig.Params()
	if d.Recv != nil {
		// receiver as first param
		recv := sig.Recv()
		if recv != nil {
			var recvObj *typecheck.Var
			if d.Recv.Name != nil {
				recvObj = fb.lookupVar(d.Recv.Name.Value)
			}
			p := &Parameter{
				name:   recv.Name(),
				object: recvObj,
				typ:    recv.Type(),
				pos:    posFromSyntax(d.Recv.Pos()),
				parent: fb.fn,
			}
			fb.fn.Params = append(fb.fn.Params, p)
			if recvObj != nil {
				alloc := fb.emitAlloc(recv.Type(), p.pos)
				fb.vars[recvObj] = alloc
				fb.emitStore(alloc, p)
			}
		}
	}
	if params != nil {
		for i := 0; i < params.Len(); i++ {
			pvar := params.At(i)
			p := &Parameter{
				name:   pvar.Name(),
				typ:    pvar.Type(),
				pos:    fb.fn.pos,
				parent: fb.fn,
			}
			fb.fn.Params = append(fb.fn.Params, p)
			if pvar.Name() != "" && pvar.Name() != "_" {
				if obj := fb.lookupVarByType(pvar.Name(), pvar.Type()); obj != nil {
					alloc := fb.emitAlloc(pvar.Type(), p.pos)
					fb.vars[obj] = alloc
					fb.emitStore(alloc, p)
				}
			}
		}
	}

	// Named results.
	if sig.Results() != nil {
		for i := 0; i < sig.Results().Len(); i++ {
			r := sig.Results().At(i)
			if r.Name() != "" && r.Name() != "_" {
				alloc := fb.emitAlloc(r.Type(), fb.fn.pos)
				fb.namedResults = append(fb.namedResults, alloc)
				fb.fn.Locals = append(fb.fn.Locals, alloc)
			}
		}
	}

	// Build the body.
	if d.Body != nil {
		fb.buildBlock(d.Body)
	}

	// Implicit return at end.
	if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
		fb.emitReturn(nil, 0)
	}
}

func (fb *funcBuilder) emitAlloc(typ typecheck.Type, pos token.Pos) *Alloc {
	a := &Alloc{Heap: false}
	a.typ = typecheck.NewPointer(typ)
	a.pos = pos
	a.name = fb.nextName()
	fb.emit(a)
	fb.fn.Locals = append(fb.fn.Locals, a)
	return a
}

func (fb *funcBuilder) emitStore(addr Value, val Value) {
	fb.emit(&Store{Addr: addr, Val: val})
}

func (fb *funcBuilder) emitLoad(addr Value, typ typecheck.Type) Value {
	u := &UnOp{Op: token.MUL, X: addr}
	u.typ = typ
	u.name = fb.nextName()
	fb.emit(u)
	return u
}

func (fb *funcBuilder) emitReturn(vals []Value, pos token.Pos) {
	fb.emit(&Return{Results: vals, pos: pos})
	fb.currentBlock = nil
}

func (fb *funcBuilder) blockTerminated(b *BasicBlock) bool {
	if len(b.Instrs) == 0 {
		return false
	}
	switch b.Instrs[len(b.Instrs)-1].(type) {
	case *Return, *Jump, *If, *Panic:
		return true
	}
	return false
}

// ─── Statement builder ────────────────────────────────────────────────────────

func (fb *funcBuilder) buildBlock(b *syntax.BlockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.List {
		fb.buildStmt(s)
		if fb.currentBlock == nil {
			return // terminated
		}
	}
}

func (fb *funcBuilder) buildStmt(s syntax.Stmt) {
	if s == nil || fb.currentBlock == nil {
		return
	}
	switch s := s.(type) {
	case *syntax.EmptyStmt:
		// nothing
	case *syntax.ExprStmt:
		// push/resize are statement-level mutating builtins.
		if call, ok := s.X.(*syntax.CallExpr); ok {
			fmt.Fprintf(os.Stderr, "EXPRSTMT: call fun type=%T\n", call.Fun)
			if name, ok2 := call.Fun.(*syntax.Name); ok2 {
				fmt.Fprintf(os.Stderr, "EXPRSTMT: name=%s\n", name.Value)
				if name.Value == "push" || name.Value == "resize" {
					result := fb.buildExpr(s.X)
					if result != nil && len(call.ArgList) > 0 {
						fb.buildStore(call.ArgList[0], result)
						fmt.Fprintf(os.Stderr, "PUSH STORE-BACK: fn=%s argType=%T\n", fb.fn.Name(), call.ArgList[0])
					}
				} else if name.Value == "pop" {
					// pop as statement: build the pop (discards element),
					// then build a resize(s, len(s)-1) for the store-back.
					fb.buildExpr(s.X) // discard element
					if len(call.ArgList) > 0 {
						sv := fb.buildExpr(call.ArgList[0])
						if sv != nil {
							// Build len(s) - 1
							lenB := &Builtin{name: "len", id: BuiltinLen}
							lenCall := &Call{Call: CallCommon{Value: lenB, Args: []Value{sv}}}
							lenCall.typ = typecheck.Typ[typecheck.Int32]
							lenCall.name = fb.nextName()
							fb.emit(lenCall)
							one := &Const{typ: typecheck.Typ[typecheck.Int32], val: constant.MakeInt64(1)}
							sub := &BinOp{Op: token.SUB, X: lenCall, Y: one}
							sub.typ = typecheck.Typ[typecheck.Int32]
							sub.name = fb.nextName()
							fb.emit(sub)
							// Build resize(s, newLen)
							resizeB := &Builtin{name: "resize", id: BuiltinResize}
							resizeCall := &Call{Call: CallCommon{Value: resizeB, Args: []Value{sv, sub}}}
							resizeCall.typ = sv.Type()
							resizeCall.name = fb.nextName()
							fb.emit(resizeCall)
							fb.buildStore(call.ArgList[0], resizeCall)
						}
					}
				} else {
					fb.buildExpr(s.X)
				}
			} else {
				fb.buildExpr(s.X)
			}
		} else {
			fb.buildExpr(s.X)
		}
	case *syntax.AssignStmt:
		fb.buildAssign(s)
	case *syntax.BlockStmt:
		fb.buildBlock(s)
	case *syntax.DeclStmt:
		for _, d := range s.DeclList {
			fb.buildLocalDecl(d)
		}
	case *syntax.IfStmt:
		fb.buildIf(s)
	case *syntax.ForStmt:
		fb.buildFor(s)
	case *syntax.SwitchStmt:
		fb.buildSwitch(s)
	case *syntax.SelectStmt:
		fb.buildSelect(s)
	case *syntax.ReturnStmt:
		fb.buildReturn(s)
	case *syntax.BranchStmt:
		fb.buildBranch(s)
	case *syntax.LabeledStmt:
		fb.buildStmt(s.Stmt)
	case *syntax.SendStmt:
		ch := fb.buildExpr(s.Chan)
		val := fb.buildExpr(s.Value)
		if ch != nil && val != nil {
			fb.emit(&Send{Chan: ch, X: val})
		}
	case *syntax.CallStmt:
		call, _ := s.Call.(*syntax.CallExpr)
		switch s.Tok {
		case syntax.Go:
			if call != nil {
				fb.buildGoStmt(call)
			}
		case syntax.Defer:
			if call != nil {
				fb.buildDeferStmt(call)
			}
		default:
			fb.buildExpr(s.Call)
		}
	}
}

func (fb *funcBuilder) buildAssign(s *syntax.AssignStmt) {
	if s.Rhs == nil {
		// x++ or x--
		lv := fb.buildExpr(s.Lhs)
		if lv == nil {
			return
		}
		one := &Const{typ: lv.Type(), val: constant.MakeInt64(1)}
		op := token.ADD
		if s.Op == syntax.Sub {
			op = token.SUB
		}
		bin := &BinOp{Op: op, X: lv, Y: one}
		bin.typ = lv.Type()
		bin.name = fb.nextName()
		fb.emit(bin)
		fb.buildStore(s.Lhs, bin)
		return
	}
	if s.Op == syntax.Def {
		fb.buildShortVarDecl(s)
		return
	}
	// compound assignment (+=, -=, etc.): Op != 0 means an operator is applied before =
	if s.Op != 0 {
		lv := fb.buildExpr(s.Lhs)
		rv := fb.buildExpr(s.Rhs)
		if lv == nil || rv == nil {
			return
		}
		op := compoundOp(s.Op)
		bin := &BinOp{Op: op, X: lv, Y: rv}
		bin.typ = lv.Type()
		bin.name = fb.nextName()
		fb.emit(bin)
		fb.buildStore(s.Lhs, bin)
		return
	}
	// plain assignment
	rhs := fb.buildExpr(s.Rhs)
	if rhs == nil {
		return
	}
	fb.buildStore(s.Lhs, rhs)
}

// compoundOp translates a syntax.Operator (from a compound assignment like +=)
// to the corresponding token.Token for BinOp. The parser stores the base op
// (syntax.Add for +=) in AssignStmt.Op; there are no AddAssign etc. constants.
func compoundOp(op syntax.Operator) token.Token {
	switch op {
	case syntax.Add, syntax.Or: // += and |= both map to ADD (| on strings is concat)
		return token.ADD
	case syntax.Sub:
		return token.SUB
	case syntax.Mul:
		return token.MUL
	case syntax.Div:
		return token.QUO
	case syntax.Rem:
		return token.REM
	case syntax.And:
		return token.AND
	case syntax.Xor:
		return token.XOR
	case syntax.Shl:
		return token.SHL
	case syntax.Shr:
		return token.SHR
	case syntax.AndNot:
		return token.AND_NOT
	}
	return token.ILLEGAL
}

func (fb *funcBuilder) buildStore(lhs syntax.Expr, val Value) {
	switch lhs := lhs.(type) {
	case *syntax.Name:
		if lhs.Value == "_" {
			return
		}
		obj := fb.lookupObject(lhs.Value)
		if obj == nil {
			return
		}
		if alloc, ok := fb.vars[obj]; ok {
			fb.emitStore(alloc, val)
		}
	case *syntax.Operation:
		if lhs.Y == nil && lhs.Op == syntax.Mul {
			// *ptr = val
			ptr := fb.buildExpr(lhs.X)
			if ptr != nil {
				fb.emitStore(ptr, val)
			}
		}
	case *syntax.SelectorExpr:
		base := fb.buildExpr(lhs.X)
		if base == nil {
			return
		}
		idx := fb.fieldIndex(base.Type(), lhs.Sel.Value)
		fa := &FieldAddr{X: base, Field: idx}
		fa.typ = typecheck.NewPointer(fb.fieldType(base.Type(), idx))
		fa.name = fb.nextName()
		fb.emit(fa)
		fb.emitStore(fa, val)
	case *syntax.IndexExpr:
		base := fb.buildExpr(lhs.X)
		idx := fb.buildExpr(lhs.Index)
		if base == nil || idx == nil {
			return
		}
		ia := &IndexAddr{X: base, Index: idx}
		ia.typ = typecheck.NewPointer(elemType(base.Type()))
		ia.name = fb.nextName()
		fb.emit(ia)
		fb.emitStore(ia, val)
	case *syntax.ListExpr:
		// multi-assign: a, b = ...
		// val is a tuple; extract each component
		for i, e := range lhs.ElemList {
			ext := &Extract{Tuple: val, Index: i}
			ext.typ = tupleElemType(val.Type(), i)
			ext.name = fb.nextName()
			fb.emit(ext)
			fb.buildStore(e, ext)
		}
	}
}

func (fb *funcBuilder) buildShortVarDecl(s *syntax.AssignStmt) {
	rhs := fb.buildExpr(s.Rhs)
	names := exprNames(s.Lhs)

	for i, name := range names {
		if name.Value == "_" {
			continue
		}
		var typ typecheck.Type
		if rhs != nil {
			if i == 0 {
				typ = rhs.Type()
			} else if tup, ok := rhs.Type().(*typecheck.Tuple); ok && i < tup.Len() {
				typ = tup.At(i).Type()
			}
		}
		// Look up existing object from the info.Defs map.
		var obj typecheck.Object
		if fb.info != nil {
			obj = fb.info.Defs[name]
		}
		if obj == nil {
			// fall back to a synthetic var
			obj = typecheck.NewVar(fb.fn.Pkg.Pkg, name.Value, typ)
		}
		alloc := fb.emitAlloc(typ, posFromSyntax(name.Pos()))
		fb.vars[obj] = alloc
		// Store the initial value.
		var initVal Value
		if rhs != nil {
			if i == 0 {
				initVal = rhs
			} else {
				ext := &Extract{Tuple: rhs, Index: i}
				ext.typ = typ
				ext.name = fb.nextName()
				fb.emit(ext)
				initVal = ext
			}
		}
		if initVal != nil {
			fb.emitStore(alloc, initVal)
		}
	}
}

func (fb *funcBuilder) buildLocalDecl(d syntax.Decl) {
	switch d := d.(type) {
	case *syntax.VarDecl:
		var typ typecheck.Type
		if fb.info != nil && len(d.NameList) > 0 {
			if obj := fb.info.Defs[d.NameList[0]]; obj != nil {
				typ = obj.Type()
			}
		}
		initVal := fb.buildExpr(d.Values)
		for _, name := range d.NameList {
			if name.Value == "_" {
				continue
			}
			var obj typecheck.Object
			if fb.info != nil {
				obj = fb.info.Defs[name]
			}
			if obj == nil {
				obj = typecheck.NewVar(fb.fn.Pkg.Pkg, name.Value, typ)
			}
			alloc := fb.emitAlloc(obj.Type(), posFromSyntax(name.Pos()))
			fb.vars[obj] = alloc
			fb.fn.Locals = append(fb.fn.Locals, alloc)
			if initVal != nil {
				fb.emitStore(alloc, initVal)
			}
		}
	case *syntax.ConstDecl:
		// Constants don't generate alloca; they're inlined as Const values.
	case *syntax.TypeDecl:
		// Local type declarations don't generate code.
	}
}

// buildIf builds an if statement.
func (fb *funcBuilder) buildIf(s *syntax.IfStmt) {
	// Optional init.
	if s.Init != nil {
		fb.buildStmt(s.Init)
	}
	if fb.currentBlock == nil {
		return
	}
	cond := fb.buildExpr(s.Cond)
	if cond == nil {
		return
	}

	thenBlock := fb.newBlock("if.then")
	var elseBlock *BasicBlock
	doneBlock := fb.newBlock("if.done")

	if s.Else != nil {
		elseBlock = fb.newBlock("if.else")
	} else {
		elseBlock = doneBlock
	}

	fb.emit(&If{Cond: cond})
	fb.currentBlock.Succs = append(fb.currentBlock.Succs, thenBlock, elseBlock)
	thenBlock.Preds = append(thenBlock.Preds, fb.currentBlock)
	elseBlock.Preds = append(elseBlock.Preds, fb.currentBlock)

	// then branch
	fb.currentBlock = thenBlock
	fb.buildBlock(s.Then)
	if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
		fb.emit(&Jump{Comment: "if.done"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
		doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
	}

	// else branch
	if s.Else != nil {
		fb.currentBlock = elseBlock
		fb.buildStmt(s.Else)
		if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
			fb.emit(&Jump{Comment: "if.done"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
			doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
		}
	}

	fb.currentBlock = doneBlock
}

// buildFor builds a for/range loop.
func (fb *funcBuilder) buildFor(s *syntax.ForStmt) {
	if s.Init != nil {
		if rc, ok := s.Init.(*syntax.RangeClause); ok {
			fb.buildRangeLoop(s, rc)
			return
		}
		fb.buildStmt(s.Init)
	}

	condBlock := fb.newBlock("for.cond")
	bodyBlock := fb.newBlock("for.body")
	postBlock := fb.newBlock("for.post")
	doneBlock := fb.newBlock("for.done")

	// Jump to cond.
	fb.emit(&Jump{Comment: "for.cond"})
	fb.currentBlock.Succs = append(fb.currentBlock.Succs, condBlock)
	condBlock.Preds = append(condBlock.Preds, fb.currentBlock)

	fb.loops = append(fb.loops, loopState{body: bodyBlock, post: postBlock, done: doneBlock})

	// Condition.
	fb.currentBlock = condBlock
	if s.Cond != nil {
		cond := fb.buildExpr(s.Cond)
		if cond != nil {
			fb.emit(&If{Cond: cond})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, bodyBlock, doneBlock)
			bodyBlock.Preds = append(bodyBlock.Preds, condBlock)
			doneBlock.Preds = append(doneBlock.Preds, condBlock)
		}
	} else {
		fb.emit(&Jump{Comment: "for.body"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, bodyBlock)
		bodyBlock.Preds = append(bodyBlock.Preds, condBlock)
	}

	// Body.
	fb.currentBlock = bodyBlock
	fb.buildBlock(s.Body)
	if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
		fb.emit(&Jump{Comment: "for.post"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, postBlock)
		postBlock.Preds = append(postBlock.Preds, fb.currentBlock)
	}

	// Post.
	fb.currentBlock = postBlock
	if s.Post != nil {
		fb.buildStmt(s.Post)
	}
	if fb.currentBlock != nil {
		fb.emit(&Jump{Comment: "for.cond"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, condBlock)
		condBlock.Preds = append(condBlock.Preds, fb.currentBlock)
	}

	fb.loops = fb.loops[:len(fb.loops)-1]
	fb.currentBlock = doneBlock
}

func (fb *funcBuilder) buildRangeLoop(s *syntax.ForStmt, rc *syntax.RangeClause) {
	iterExpr := fb.buildExpr(rc.X)
	if iterExpr == nil {
		return
	}

	r := &Range{X: iterExpr}
	r.typ = iterType(iterExpr.Type())
	r.name = fb.nextName()
	fb.emit(r)

	bodyBlock := fb.newBlock("range.body")
	doneBlock := fb.newBlock("range.done")

	fb.loops = append(fb.loops, loopState{body: bodyBlock, done: doneBlock})

	// Check done.
	condBlock := fb.newBlock("range.cond")
	fb.emit(&Jump{Comment: "range.cond"})
	fb.currentBlock.Succs = append(fb.currentBlock.Succs, condBlock)
	condBlock.Preds = append(condBlock.Preds, fb.currentBlock)

	fb.currentBlock = condBlock
	nxt := &Next{Iter: r, IsString: isStringType(iterExpr.Type())}
	nxt.typ = nextType(r.typ)
	nxt.name = fb.nextName()
	fb.emit(nxt)

	// Extract ok (index 0).
	okExt := &Extract{Tuple: nxt, Index: 0}
	okExt.typ = typecheck.Typ[typecheck.Bool]
	okExt.name = fb.nextName()
	fb.emit(okExt)

	fb.emit(&If{Cond: okExt})
	fb.currentBlock.Succs = append(fb.currentBlock.Succs, bodyBlock, doneBlock)
	bodyBlock.Preds = append(bodyBlock.Preds, condBlock)
	doneBlock.Preds = append(doneBlock.Preds, condBlock)

	// Body: bind key/value.
	fb.currentBlock = bodyBlock
	if rc.Lhs != nil && rc.Def {
		names := exprNames(rc.Lhs)
		// index 1 = key, index 2 = value
		for i, name := range names {
			if name.Value == "_" {
				continue
			}
			ext := &Extract{Tuple: nxt, Index: i + 1}
			ext.typ = extractNthType(nxt.typ, i+1)
			ext.name = fb.nextName()
			fb.emit(ext)
			var obj typecheck.Object
			if fb.info != nil {
				obj = fb.info.Defs[name]
			}
			if obj == nil {
				obj = typecheck.NewVar(fb.fn.Pkg.Pkg, name.Value, ext.typ)
			}
			alloc := fb.emitAlloc(ext.typ, posFromSyntax(name.Pos()))
			fb.vars[obj] = alloc
			fb.emitStore(alloc, ext)
		}
	}
	fb.buildBlock(s.Body)
	if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
		fb.emit(&Jump{Comment: "range.cond"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, condBlock)
		condBlock.Preds = append(condBlock.Preds, fb.currentBlock)
	}

	fb.loops = fb.loops[:len(fb.loops)-1]
	fb.currentBlock = doneBlock
}

func (fb *funcBuilder) buildSwitch(s *syntax.SwitchStmt) {
	// Optional init.
	if s.Init != nil {
		fb.buildStmt(s.Init)
	}
	if fb.currentBlock == nil {
		return
	}

	doneBlock := fb.newBlock("switch.done")
	savedLoops := fb.loops
	fb.loops = append(fb.loops, loopState{done: doneBlock})

	// Type switch vs expression switch.
	if s.Tag != nil {
		if tsg, ok := s.Tag.(*syntax.TypeSwitchGuard); ok {
			fb.buildTypeSwitch(s, tsg, doneBlock)
			fb.loops = savedLoops
			fb.currentBlock = doneBlock
			return
		}
	}

	var tag Value
	if s.Tag != nil {
		tag = fb.buildExpr(s.Tag)
	}

	for _, clause := range s.Body {
		caseBlock := fb.newBlock("switch.case")
		nextBlock := fb.newBlock("switch.next")

		if clause.Cases != nil && tag != nil {
			caseVal := fb.buildExpr(clause.Cases)
			cmp := &BinOp{Op: token.EQL, X: tag, Y: caseVal}
			cmp.typ = typecheck.Typ[typecheck.Bool]
			cmp.name = fb.nextName()
			fb.emit(cmp)
			fb.emit(&If{Cond: cmp})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, caseBlock, nextBlock)
			caseBlock.Preds = append(caseBlock.Preds, fb.currentBlock)
			nextBlock.Preds = append(nextBlock.Preds, fb.currentBlock)
		} else {
			// default or no tag
			fb.emit(&Jump{Comment: "switch.case"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, caseBlock)
			caseBlock.Preds = append(caseBlock.Preds, fb.currentBlock)
		}

		fb.currentBlock = caseBlock
		for _, stmt := range clause.Body {
			fb.buildStmt(stmt)
			if fb.currentBlock == nil {
				break
			}
		}
		if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
			fb.emit(&Jump{Comment: "switch.done"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
			doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
		}
		fb.currentBlock = nextBlock
	}

	// Fall through last nextBlock to done.
	if fb.currentBlock != nil {
		fb.emit(&Jump{Comment: "switch.done"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
		doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
	}

	fb.loops = savedLoops
	fb.currentBlock = doneBlock
}

func (fb *funcBuilder) buildTypeSwitch(s *syntax.SwitchStmt, tsg *syntax.TypeSwitchGuard, doneBlock *BasicBlock) {
	x := fb.buildExpr(tsg.X)
	for _, clause := range s.Body {
		caseBlock := fb.newBlock("typeswitch.case")
		nextBlock := fb.newBlock("typeswitch.next")

		if clause.Cases != nil {
			var assertedType typecheck.Type
			if fb.info != nil {
				tv := fb.info.Types[clause.Cases]
				assertedType = tv.Type
			}
			if assertedType == nil {
				assertedType = typecheck.Typ[typecheck.Invalid]
			}
			ta := &TypeAssert{X: x, AssertedType: assertedType, CommaOk: true}
			ta.typ = typecheck.NewTuple(
				typecheck.NewVar(nil, "val", assertedType),
				typecheck.NewVar(nil, "ok", typecheck.Typ[typecheck.Bool]),
			)
			ta.name = fb.nextName()
			fb.emit(ta)
			okExt := &Extract{Tuple: ta, Index: 1}
			okExt.typ = typecheck.Typ[typecheck.Bool]
			okExt.name = fb.nextName()
			fb.emit(okExt)
			fb.emit(&If{Cond: okExt})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, caseBlock, nextBlock)
			caseBlock.Preds = append(caseBlock.Preds, fb.currentBlock)
			nextBlock.Preds = append(nextBlock.Preds, fb.currentBlock)

			// Bind guard variable in case block.
			fb.currentBlock = caseBlock
			if tsg.Lhs != nil {
				guardVal := &Extract{Tuple: ta, Index: 0}
				guardVal.typ = assertedType
				guardVal.name = fb.nextName()
				fb.emit(guardVal)
				obj := typecheck.NewVar(fb.fn.Pkg.Pkg, tsg.Lhs.Value, assertedType)
				alloc := fb.emitAlloc(assertedType, posFromSyntax(tsg.Lhs.Pos()))
				fb.vars[obj] = alloc
				fb.emitStore(alloc, guardVal)
			}
		} else {
			fb.emit(&Jump{Comment: "typeswitch.case"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, caseBlock)
			caseBlock.Preds = append(caseBlock.Preds, fb.currentBlock)
			fb.currentBlock = caseBlock
		}

		for _, stmt := range clause.Body {
			fb.buildStmt(stmt)
			if fb.currentBlock == nil {
				break
			}
		}
		if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
			fb.emit(&Jump{Comment: "switch.done"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
			doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
		}
		fb.currentBlock = nextBlock
	}
	if fb.currentBlock != nil {
		fb.emit(&Jump{Comment: "switch.done"})
		fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
		doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
	}
}

func (fb *funcBuilder) buildSelect(s *syntax.SelectStmt) {
	doneBlock := fb.newBlock("select.done")

	var states []*SelectState
	caseBlocks := make([]*BasicBlock, len(s.Body))
	for i, clause := range s.Body {
		cb := fb.newBlock("select.case")
		caseBlocks[i] = cb
		state := &SelectState{}
		if clause.Comm != nil {
			switch comm := clause.Comm.(type) {
			case *syntax.SendStmt:
				state.Dir = token.ARROW // SEND direction marker
				state.Chan = fb.buildExpr(comm.Chan)
				state.Send = fb.buildExpr(comm.Value)
			case *syntax.AssignStmt:
				// recv: v, ok := <-ch  or  v := <-ch
				var chanExpr syntax.Expr
				if op, ok2 := comm.Rhs.(*syntax.Operation); ok2 && op.Y == nil && op.Op == syntax.Recv {
					chanExpr = op.X
				}
				if chanExpr != nil {
					state.Dir = token.ILLEGAL // RECV direction marker
					state.Chan = fb.buildExpr(chanExpr)
				}
			case *syntax.ExprStmt:
				if op, ok2 := comm.X.(*syntax.Operation); ok2 && op.Y == nil && op.Op == syntax.Recv {
					state.Dir = token.ILLEGAL // RECV direction marker
					state.Chan = fb.buildExpr(op.X)
				}
			}
		}
		states = append(states, state)
	}

	sel := &Select{States: states, Blocking: true}
	sel.typ = selectResultType(states)
	sel.name = fb.nextName()
	fb.emit(sel)

	for i, clause := range s.Body {
		fb.currentBlock = caseBlocks[i]
		for _, stmt := range clause.Body {
			fb.buildStmt(stmt)
			if fb.currentBlock == nil {
				break
			}
		}
		if fb.currentBlock != nil && !fb.blockTerminated(fb.currentBlock) {
			fb.emit(&Jump{Comment: "select.done"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
			doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
		}
	}

	fb.currentBlock = doneBlock
}

func (fb *funcBuilder) buildReturn(s *syntax.ReturnStmt) {
	var vals []Value
	if s.Results != nil {
		v := fb.buildExpr(s.Results)
		if v != nil {
			vals = append(vals, v)
		}
	}
	fb.emitReturn(vals, posFromSyntax(s.Pos()))
}

func (fb *funcBuilder) buildBranch(s *syntax.BranchStmt) {
	if fb.currentBlock == nil {
		return
	}
	switch s.Tok {
	case syntax.Break:
		if len(fb.loops) > 0 {
			doneBlock := fb.loops[len(fb.loops)-1].done
			fb.emit(&Jump{Comment: "break"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, doneBlock)
			doneBlock.Preds = append(doneBlock.Preds, fb.currentBlock)
			fb.currentBlock = nil
		}
	case syntax.Continue:
		for i := len(fb.loops) - 1; i >= 0; i-- {
			postBlock := fb.loops[i].post
			if postBlock == nil {
				postBlock = fb.loops[i].body
			}
			if postBlock == nil {
				continue // select-loop state has no post/body; skip upward
			}
			fb.emit(&Jump{Comment: "continue"})
			fb.currentBlock.Succs = append(fb.currentBlock.Succs, postBlock)
			postBlock.Preds = append(postBlock.Preds, fb.currentBlock)
			fb.currentBlock = nil
			break
		}
	case syntax.Return:
		fb.emitReturn(nil, 0)
	}
}

func (fb *funcBuilder) buildGoStmt(call *syntax.CallExpr) {
	fn := fb.buildExpr(call.Fun)
	if fn == nil {
		return
	}
	args := fb.buildArgs(call.ArgList)
	g := &Go{Call: CallCommon{Value: fn, Args: args}}
	fb.emit(g)
}

func (fb *funcBuilder) buildDeferStmt(call *syntax.CallExpr) {
	fn := fb.buildExpr(call.Fun)
	if fn == nil {
		return
	}
	args := fb.buildArgs(call.ArgList)
	d := &Defer{Call: CallCommon{Value: fn, Args: args}}
	fb.emit(d)
	fb.deferred++
}

// ─── Expression builder ───────────────────────────────────────────────────────

// buildExpr compiles an expression and returns the resulting Value.
// Returns nil for void expressions or errors.
func (fb *funcBuilder) buildExpr(e syntax.Expr) Value {
	if e == nil || fb.currentBlock == nil {
		return nil
	}
	switch e := e.(type) {
	case *syntax.Name:
		return fb.buildIdent(e)
	case *syntax.BasicLit:
		return fb.buildLit(e)
	case *syntax.Operation:
		return fb.buildOperation(e)
	case *syntax.CallExpr:
		return fb.buildCall(e)
	case *syntax.SelectorExpr:
		return fb.buildSelector(e)
	case *syntax.IndexExpr:
		return fb.buildIndex(e)
	case *syntax.SliceExpr:
		return fb.buildSlice(e)
	case *syntax.AssertExpr:
		return fb.buildAssert(e)
	case *syntax.CompositeLit:
		return fb.buildCompositeLit(e)
	case *syntax.FuncLit:
		return fb.buildFuncLit(e)
	case *syntax.ParenExpr:
		return fb.buildExpr(e.X)
	case *syntax.ListExpr:
		// Multiple return values — return the last (callers handle multi-return differently)
		var last Value
		for _, el := range e.ElemList {
			last = fb.buildExpr(el)
		}
		return last
	case *syntax.KeyValueExpr:
		return fb.buildExpr(e.Value)
	}
	return nil
}

func (fb *funcBuilder) buildIdent(e *syntax.Name) Value {
	if e.Value == "_" || e.Value == "nil" {
		return &Const{typ: nil, val: nil}
	}
	obj := fb.lookupObject(e.Value)
	if obj == nil {
		// Could be a builtin.
		return fb.builtinValue(e.Value)
	}
	switch obj := obj.(type) {
	case *typecheck.Var:
		if alloc, ok := fb.vars[obj]; ok {
			return fb.emitLoad(alloc, obj.Type())
		}
		// Package-level global.
		if g, ok := fb.fn.Pkg.Members[e.Value].(*Global); ok {
			return fb.emitLoad(g, obj.Type())
		}
		return &Const{typ: obj.Type(), val: nil}
	case *typecheck.Const:
		var cval constant.Value
		if obj.Val() != nil {
			cval, _ = obj.Val().(constant.Value)
		}
		return &Const{typ: obj.Type(), val: cval}
	case *typecheck.Func:
		fn, _ := fb.fn.Pkg.Members[e.Value].(*Function)
		if fn != nil {
			return fn
		}
		return &Const{typ: obj.Type(), val: nil}
	case *typecheck.TypeName:
		return nil // type name used as expression (e.g., conversion)
	}
	return nil
}

func (fb *funcBuilder) builtinValue(name string) Value {
	id, ok := builtinID(name)
	if !ok {
		return nil
	}
	return &Builtin{id: id, name: name}
}

func (fb *funcBuilder) buildLit(e *syntax.BasicLit) Value {
	switch e.Kind {
	case syntax.IntLit:
		n, _ := constant.Val(constant.MakeFromLiteral(e.Value, token.INT, 0)).(int64)
		return &Const{typ: typecheck.Typ[typecheck.UntypedInt], val: constant.MakeInt64(n)}
	case syntax.FloatLit:
		return &Const{
			typ: typecheck.Typ[typecheck.UntypedFloat],
			val: constant.MakeFromLiteral(e.Value, token.FLOAT, 0),
		}
	case syntax.StringLit:
		s := e.Value
		if len(s) >= 2 {
			s = s[1 : len(s)-1] // strip quotes (simplified)
		}
		return &Const{typ: typecheck.Typ[typecheck.UntypedString], val: constant.MakeString(s)}
	case syntax.RuneLit:
		return &Const{
			typ: typecheck.Typ[typecheck.UntypedRune],
			val: constant.MakeFromLiteral(e.Value, token.CHAR, 0),
		}
	}
	return nil
}

func (fb *funcBuilder) buildOperation(e *syntax.Operation) Value {
	if e.Y == nil {
		// unary
		x := fb.buildExpr(e.X)
		if x == nil {
			return nil
		}
		op := syntaxOpToToken(e.Op, true)
		if op == token.ARROW {
			// receive
			u := &UnOp{Op: token.ARROW, X: x}
			u.typ = chanElemType(x.Type())
			u.name = fb.nextName()
			fb.emit(u)
			return u
		}
		if op == token.AND {
			// address-of
			if name, ok := e.X.(*syntax.Name); ok {
				obj := fb.lookupObject(name.Value)
				if obj != nil {
					if alloc, ok2 := fb.vars[obj]; ok2 {
						return alloc
					}
					if g, ok2 := fb.fn.Pkg.Members[name.Value].(*Global); ok2 {
						return g
					}
				}
			}
			// heap alloc
			inner := x
			a := &Alloc{Heap: true}
			a.typ = typecheck.NewPointer(inner.Type())
			a.name = fb.nextName()
			fb.emit(a)
			fb.emitStore(a, inner)
			return a
		}
		u := &UnOp{Op: op, X: x}
		u.typ = x.Type()
		u.name = fb.nextName()
		fb.emit(u)
		return u
	}
	// binary
	x := fb.buildExpr(e.X)
	y := fb.buildExpr(e.Y)
	if x == nil || y == nil {
		return nil
	}
	op := syntaxOpToToken(e.Op, false)
	b := &BinOp{Op: op, X: x, Y: y}
	b.name = fb.nextName()
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ,
		token.LAND, token.LOR:
		b.typ = typecheck.Typ[typecheck.Bool]
	default:
		if x.Type() != nil {
			b.typ = x.Type()
		} else if y.Type() != nil {
			b.typ = y.Type()
		}
	}
	fb.emit(b)
	return b
}

func (fb *funcBuilder) buildCall(e *syntax.CallExpr) Value {
	fn := fb.buildExpr(e.Fun)
	args := fb.buildArgs(e.ArgList)

	if fn == nil {
		return nil
	}
	if _, ok := fn.(*Builtin); ok {
		return fb.buildBuiltinCall(e, fn.(*Builtin), args)
	}

	var retType typecheck.Type
	if fn.Type() != nil {
		if sig, ok := fn.Type().Underlying().(*typecheck.Signature); ok {
			if sig.Results() != nil && sig.Results().Len() == 1 {
				retType = sig.Results().At(0).Type()
			} else if sig.Results() != nil && sig.Results().Len() > 1 {
				retType = sig.Results()
			}
		}
	}

	call := &Call{Call: CallCommon{Value: fn, Args: args}}
	call.typ = retType
	call.name = fb.nextName()
	fb.emit(call)

	if retType == nil {
		return nil // void call
	}
	return call
}

func (fb *funcBuilder) buildArgs(argList []syntax.Expr) []Value {
	args := make([]Value, 0, len(argList))
	for _, a := range argList {
		v := fb.buildExpr(a)
		if v != nil {
			args = append(args, v)
		}
	}
	return args
}

func (fb *funcBuilder) buildBuiltinCall(e *syntax.CallExpr, b *Builtin, args []Value) Value {
	switch b.id {
	case BuiltinLen, BuiltinCap:
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		call.typ = typecheck.Typ[typecheck.Int32]
		call.name = fb.nextName()
		fb.emit(call)
		return call
	case BuiltinAppend:
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		if len(args) > 0 {
			call.typ = args[0].Type()
		}
		call.name = fb.nextName()
		fb.emit(call)
		return call
	case BuiltinMake:
		if len(e.ArgList) == 0 {
			return nil
		}
		typ := fb.resolveType(e.ArgList[0])
		// args already has type expr filtered (buildExpr returns nil for types),
		// so args == the size arguments.
		return fb.emitMake(typ, args)
	case BuiltinNew:
		if len(e.ArgList) == 0 {
			return nil
		}
		typ := fb.resolveType(e.ArgList[0])
		a := &Alloc{Heap: true}
		a.typ = typecheck.NewPointer(typ)
		a.name = fb.nextName()
		fb.emit(a)
		return a
	case BuiltinPanic:
		if len(args) > 0 {
			fb.emit(&Panic{X: args[0]})
			fb.currentBlock = nil
		}
		return nil
	case BuiltinClose, BuiltinDelete, BuiltinClear, BuiltinPrint, BuiltinPrintln:
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		call.name = fb.nextName()
		fb.emit(call)
		return nil
	case BuiltinCopy:
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		call.typ = typecheck.Typ[typecheck.Int32]
		call.name = fb.nextName()
		fb.emit(call)
		return call
	case BuiltinRecover:
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		call.typ = typecheck.Typ[typecheck.String] // any
		call.name = fb.nextName()
		fb.emit(call)
		return call
	case BuiltinPush, BuiltinResize:
		// push(s, v...) and resize(s, n) return modified slice for store-back.
		// The ExprStmt handler desugars push(s,v) to s = push(s,v).
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		if len(args) > 0 {
			call.typ = args[0].Type()
		}
		call.name = fb.nextName()
		fb.emit(call)
		return call
	case BuiltinPop:
		// pop(s) returns the element. The modified slice store-back is
		// handled by the ExprStmt desugaring (which calls resize(s, len-1)
		// semantics via the store-back path).
		call := &Call{Call: CallCommon{Value: b, Args: args}}
		if len(args) > 0 {
			st := args[0].Type()
			if sl, ok := st.Underlying().(*typecheck.Slice); ok {
				call.typ = sl.Elem()
			} else {
				call.typ = typecheck.Typ[typecheck.Uint8]
			}
		}
		call.name = fb.nextName()
		fb.emit(call)
		return call
	}
	call := &Call{Call: CallCommon{Value: b, Args: args}}
	call.name = fb.nextName()
	fb.emit(call)
	return call
}

func (fb *funcBuilder) emitMake(typ typecheck.Type, sizeArgs []Value) Value {
	if typ == nil {
		return nil
	}
	switch typ.Underlying().(type) {
	case *typecheck.Slice:
		ms := &MakeSlice{}
		ms.typ = typ
		ms.name = fb.nextName()
		if len(sizeArgs) > 0 {
			ms.Len = sizeArgs[0]
		}
		if len(sizeArgs) > 1 {
			ms.Cap = sizeArgs[1]
		}
		fb.emit(ms)
		return ms
	case *typecheck.Map:
		mm := &MakeMap{}
		mm.typ = typ
		mm.name = fb.nextName()
		if len(sizeArgs) > 0 {
			mm.Reserve = sizeArgs[0]
		}
		fb.emit(mm)
		return mm
	case *typecheck.Chan:
		mc := &MakeChan{}
		mc.typ = typ
		mc.name = fb.nextName()
		if len(sizeArgs) > 0 {
			mc.Size = sizeArgs[0]
		}
		fb.emit(mc)
		return mc
	}
	return nil
}

func (fb *funcBuilder) buildSelector(e *syntax.SelectorExpr) Value {
	x := fb.buildExpr(e.X)
	if x == nil || x.Type() == nil {
		// Package-qualified name.
		if name, ok := e.X.(*syntax.Name); ok {
			obj := fb.lookupObject(name.Value)
			if _, ok := obj.(*typecheck.PkgName); ok {
				// imported name — look up in that package's members
				// (simplified: return nil for now; proper resolution in B3)
				return nil
			}
		}
		return nil
	}
	// field or method
	fieldIdx := fb.fieldIndex(x.Type(), e.Sel.Value)
	if fieldIdx >= 0 {
		fa := &FieldAddr{X: x, Field: fieldIdx}
		fa.typ = typecheck.NewPointer(fb.fieldType(x.Type(), fieldIdx))
		fa.name = fb.nextName()
		fb.emit(fa)
		return fb.emitLoad(fa, fb.fieldType(x.Type(), fieldIdx))
	}
	// method value
	return nil
}

func (fb *funcBuilder) buildIndex(e *syntax.IndexExpr) Value {
	x := fb.buildExpr(e.X)
	idx := fb.buildExpr(e.Index)
	if x == nil || idx == nil {
		return nil
	}
	if x.Type() == nil {
		return nil
	}
	switch t := x.Type().Underlying().(type) {
	case *typecheck.Map:
		l := &Lookup{X: x, Index: idx}
		l.typ = t.Elem()
		l.name = fb.nextName()
		fb.emit(l)
		return l
	default:
		ia := &IndexAddr{X: x, Index: idx}
		ia.typ = typecheck.NewPointer(elemType(x.Type()))
		ia.name = fb.nextName()
		fb.emit(ia)
		return fb.emitLoad(ia, elemType(x.Type()))
	}
}

func (fb *funcBuilder) buildSlice(e *syntax.SliceExpr) Value {
	x := fb.buildExpr(e.X)
	if x == nil {
		return nil
	}
	sl := &Slice{X: x}
	if len(e.Index) > 0 && e.Index[0] != nil {
		sl.Low = fb.buildExpr(e.Index[0])
	}
	if len(e.Index) > 1 && e.Index[1] != nil {
		sl.High = fb.buildExpr(e.Index[1])
	}
	if len(e.Index) > 2 && e.Index[2] != nil {
		sl.Max = fb.buildExpr(e.Index[2])
	}
	sl.typ = sliceOf(x.Type())
	sl.name = fb.nextName()
	fb.emit(sl)
	return sl
}

func (fb *funcBuilder) buildAssert(e *syntax.AssertExpr) Value {
	x := fb.buildExpr(e.X)
	assertedType := fb.resolveType(e.Type)
	if x == nil {
		return nil
	}
	ta := &TypeAssert{X: x, AssertedType: assertedType, CommaOk: false}
	ta.typ = assertedType
	ta.name = fb.nextName()
	fb.emit(ta)
	return ta
}

func (fb *funcBuilder) buildCompositeLit(e *syntax.CompositeLit) Value {
	var typ typecheck.Type
	if e.Type != nil {
		typ = fb.resolveType(e.Type)
	}
	if typ == nil {
		return nil
	}
	alloc := fb.emitAlloc(typ, posFromSyntax(e.Pos()))
	for _, el := range e.ElemList {
		if kv, ok := el.(*syntax.KeyValueExpr); ok {
			// struct field or map key
			if _, ok2 := typ.Underlying().(*typecheck.Struct); ok2 {
				idx := fb.fieldIndex(typ, kv.Key.(*syntax.Name).Value)
				if idx >= 0 {
					fa := &FieldAddr{X: alloc, Field: idx}
					fa.typ = typecheck.NewPointer(fb.fieldType(typ, idx))
					fa.name = fb.nextName()
					fb.emit(fa)
					v := fb.buildExpr(kv.Value)
					if v != nil {
						fb.emitStore(fa, v)
					}
				}
			} else if _, ok2 := typ.Underlying().(*typecheck.Map); ok2 {
				k := fb.buildExpr(kv.Key)
				v := fb.buildExpr(kv.Value)
				if k != nil && v != nil {
					fb.emit(&MapUpdate{Map: alloc, Key: k, Value: v})
				}
			}
		} else {
			v := fb.buildExpr(el)
			_ = v
		}
	}
	return fb.emitLoad(alloc, typ)
}

func (fb *funcBuilder) buildFuncLit(e *syntax.FuncLit) Value {
	// Create an anonymous function member.
	var sig *typecheck.Signature
	if fb.info != nil {
		if tv, ok := fb.info.Types[e]; ok {
			sig, _ = tv.Type.(*typecheck.Signature)
		}
	}
	name := fb.fn.name + "$" + itoa(len(fb.fn.AnonFuncs)+1)
	anon := &Function{
		name:      name,
		Signature: sig,
		pos:       posFromSyntax(e.Pos()),
		Pkg:       fb.fn.Pkg,
		Prog:      fb.fn.Prog,
		parent:    fb.fn,
	}
	fb.fn.AnonFuncs = append(fb.fn.AnonFuncs, anon)

	// Build anon's body.
	ab := newFuncBuilder(anon, fb.info)
	d := &syntax.FuncDecl{
		Name: &syntax.Name{Value: name},
		Type: e.Type,
		Body: e.Body,
	}
	ab.buildBody(d)

	// Create closure if there are free variables.
	if len(anon.FreeVars) == 0 {
		return anon
	}
	bindings := make([]Value, len(anon.FreeVars))
	for i, fv := range anon.FreeVars {
		obj := fb.lookupObject(fv.name)
		if obj != nil {
			if alloc, ok := fb.vars[obj]; ok {
				alloc.Heap = true
				bindings[i] = alloc
			}
		}
	}
	mc := &MakeClosure{Fn: anon, Bindings: bindings}
	mc.typ = sig
	mc.name = fb.nextName()
	fb.emit(mc)
	return mc
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (fb *funcBuilder) lookupObject(name string) typecheck.Object {
	// Look in local vars first.
	for obj := range fb.vars {
		if obj.Name() == name {
			return obj
		}
	}
	// Package scope.
	if fb.fn.Pkg != nil {
		if _, obj := fb.fn.Pkg.Pkg.Scope().LookupParent(name); obj != nil {
			return obj
		}
	}
	return nil
}

func (fb *funcBuilder) lookupVar(name string) *typecheck.Var {
	obj := fb.lookupObject(name)
	v, _ := obj.(*typecheck.Var)
	return v
}

func (fb *funcBuilder) lookupVarByType(name string, typ typecheck.Type) typecheck.Object {
	// For parameter binding: find the Var in info.Defs or synthesize one.
	return typecheck.NewVar(fb.fn.Pkg.Pkg, name, typ)
}

func (fb *funcBuilder) resolveType(e syntax.Expr) typecheck.Type {
	if fb.info != nil {
		if tv, ok := fb.info.Types[e]; ok && tv.Type != nil {
			return tv.Type
		}
	}
	return nil
}

func (fb *funcBuilder) fieldIndex(t typecheck.Type, name string) int {
	if t == nil {
		return -1
	}
	// dereference pointer
	if pt, ok := t.Underlying().(*typecheck.Pointer); ok {
		t = pt.Elem()
	}
	if st, ok := t.Underlying().(*typecheck.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			if st.Field(i).Name() == name {
				return i
			}
		}
	}
	return -1
}

func (fb *funcBuilder) fieldType(t typecheck.Type, idx int) typecheck.Type {
	if t == nil {
		return nil
	}
	if pt, ok := t.Underlying().(*typecheck.Pointer); ok {
		t = pt.Elem()
	}
	if st, ok := t.Underlying().(*typecheck.Struct); ok {
		if idx >= 0 && idx < st.NumFields() {
			return st.Field(idx).Type()
		}
	}
	return nil
}

// ─── Type helpers ─────────────────────────────────────────────────────────────

func elemType(t typecheck.Type) typecheck.Type {
	if t == nil {
		return nil
	}
	switch t := t.Underlying().(type) {
	case *typecheck.Slice:
		return t.Elem()
	case *typecheck.Array:
		return t.Elem()
	case *typecheck.Map:
		return t.Elem()
	case *typecheck.Pointer:
		return t.Elem()
	}
	return nil
}

func chanElemType(t typecheck.Type) typecheck.Type {
	if t == nil {
		return nil
	}
	if ch, ok := t.Underlying().(*typecheck.Chan); ok {
		return ch.Elem()
	}
	return nil
}

func isStringType(t typecheck.Type) bool {
	if t == nil {
		return false
	}
	if b, ok := t.Underlying().(*typecheck.Basic); ok {
		return b.Info()&typecheck.IsString != 0
	}
	return false
}

func sliceOf(t typecheck.Type) typecheck.Type {
	if t == nil {
		return nil
	}
	switch t := t.Underlying().(type) {
	case *typecheck.Slice:
		return t
	case *typecheck.Array:
		return typecheck.NewSlice(t.Elem())
	}
	return typecheck.NewSlice(typecheck.Typ[typecheck.Uint8])
}

func tupleElemType(t typecheck.Type, i int) typecheck.Type {
	if t == nil {
		return nil
	}
	if tup, ok := t.(*typecheck.Tuple); ok && i < tup.Len() {
		return tup.At(i).Type()
	}
	return nil
}

func extractNthType(t typecheck.Type, n int) typecheck.Type {
	return tupleElemType(t, n)
}

func iterType(t typecheck.Type) typecheck.Type {
	// Range iterator is an opaque type; approximate with a struct.
	return typecheck.Typ[typecheck.Invalid]
}

func nextType(iterT typecheck.Type) typecheck.Type {
	// next(iter) returns a tuple (bool, key, val) — approximate.
	return typecheck.NewTuple(
		typecheck.NewVar(nil, "ok", typecheck.Typ[typecheck.Bool]),
		typecheck.NewVar(nil, "k", typecheck.Typ[typecheck.Int32]),
		typecheck.NewVar(nil, "v", typecheck.Typ[typecheck.Invalid]),
	)
}

func selectResultType(states []*SelectState) typecheck.Type {
	return typecheck.NewTuple(
		typecheck.NewVar(nil, "index", typecheck.Typ[typecheck.Int32]),
		typecheck.NewVar(nil, "recvOk", typecheck.Typ[typecheck.Bool]),
	)
}

// ─── Operator mapping ─────────────────────────────────────────────────────────

func syntaxOpToToken(op syntax.Operator, unary bool) token.Token {
	if unary {
		switch op {
		case syntax.Not:
			return token.NOT
		case syntax.Recv:
			return token.ARROW
		case syntax.And:
			return token.AND
		case syntax.Mul:
			return token.MUL
		case syntax.Sub:
			return token.SUB
		case syntax.Xor:
			return token.XOR
		}
	}
	switch op {
	case syntax.Add, syntax.Or:
		return token.ADD // Moxie: | on strings is concat
	case syntax.Sub:
		return token.SUB
	case syntax.Mul:
		return token.MUL
	case syntax.Div:
		return token.QUO
	case syntax.Rem:
		return token.REM
	case syntax.And:
		return token.AND
	case syntax.Xor:
		return token.XOR
	case syntax.Shl:
		return token.SHL
	case syntax.Shr:
		return token.SHR
	case syntax.AndNot:
		return token.AND_NOT
	case syntax.OrOr:
		return token.LOR
	case syntax.AndAnd:
		return token.LAND
	case syntax.Eql:
		return token.EQL
	case syntax.Neq:
		return token.NEQ
	case syntax.Lss:
		return token.LSS
	case syntax.Leq:
		return token.LEQ
	case syntax.Gtr:
		return token.GTR
	case syntax.Geq:
		return token.GEQ
	}
	return token.ILLEGAL
}

// isSliceType reports whether t is a slice or string (string = []byte in Moxie).
func isSliceType(t typecheck.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *typecheck.Slice:
		return true
	case *typecheck.Basic:
		b := t.Underlying().(*typecheck.Basic)
		return b.Kind() == typecheck.String || b.Kind() == typecheck.UntypedString
	}
	return false
}

// ─── Builtin ID lookup ────────────────────────────────────────────────────────

func builtinID(name string) (BuiltinID, bool) {
	switch name {
	case "append":
		return BuiltinAppend, true
	case "cap":
		return BuiltinCap, true
	case "clear":
		return BuiltinClear, true
	case "close":
		return BuiltinClose, true
	case "copy":
		return BuiltinCopy, true
	case "delete":
		return BuiltinDelete, true
	case "len":
		return BuiltinLen, true
	case "make":
		return BuiltinMake, true
	case "max":
		return BuiltinMax, true
	case "min":
		return BuiltinMin, true
	case "new":
		return BuiltinNew, true
	case "panic":
		return BuiltinPanic, true
	case "print":
		return BuiltinPrint, true
	case "println":
		return BuiltinPrintln, true
	case "recover":
		return BuiltinRecover, true
	case "push":
		return BuiltinPush, true
	case "pop":
		return BuiltinPop, true
	case "resize":
		return BuiltinResize, true
	}
	return 0, false
}

// ─── Misc utilities ───────────────────────────────────────────────────────────

func posFromSyntax(p syntax.Pos) token.Pos {
	// syntax.Pos is a struct (base,line,col); full mapping via fset requires
	// the adapter's file. For B2, positions are debug-only; return NoPos.
	// B3 will thread fset through the builder for accurate positions.
	return token.NoPos
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func exprNames(e syntax.Expr) []*syntax.Name {
	if e == nil {
		return nil
	}
	if n, ok := e.(*syntax.Name); ok {
		return []*syntax.Name{n}
	}
	if l, ok := e.(*syntax.ListExpr); ok {
		var names []*syntax.Name
		for _, el := range l.ElemList {
			if n, ok := el.(*syntax.Name); ok {
				names = append(names, n)
			}
		}
		return names
	}
	return nil
}
