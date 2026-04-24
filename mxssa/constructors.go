package mxssa

import (
	"go/constant"
	"go/token"

	"moxie/typecheck"
)

// RegisterPackage adds a package to the program's import index.
func (prog *Program) RegisterPackage(p *Package) {
	prog.packages[p.Pkg] = p
	prog.imported[p.Pkg.Path()] = p
}

// NewFunction creates a function shell (no body).
func NewFunction(name string, sig *typecheck.Signature, synthetic string, pkg *Package, prog *Program) *Function {
	return &Function{
		name:      name,
		Signature: sig,
		Synthetic: synthetic,
		Pkg:       pkg,
		Prog:      prog,
		vars:      map[typecheck.Object]Value{},
	}
}

func (f *Function) SetPos(p token.Pos) { f.pos = p }

// NewGlobal creates a package-level global variable.
func NewGlobal(name string, typ typecheck.Type, pkg *Package) *Global {
	return &Global{name: name, typ: typ, pkg: pkg}
}

func (g *Global) SetPos(p token.Pos) { g.pos = p }

// NewType_ creates a package-level named type member.
func NewType_(name string, typ typecheck.Type, pkg *Package) *Type_ {
	obj := typecheck.NewTypeName(pkg.Pkg, name, typ)
	return &Type_{object: obj, pkg: pkg}
}

// NewNamedConst creates a package-level named constant.
func NewNamedConst(name string, typ typecheck.Type, val constant.Value, pkg *Package) *NamedConst {
	obj := typecheck.NewConst(pkg.Pkg, name, typ, val)
	c := NewConst(val, typ)
	return &NamedConst{object: obj, Value: c, pkg: pkg}
}

// NewParameter creates a function parameter.
func NewParameter(name string, typ typecheck.Type, parent *Function) *Parameter {
	return &Parameter{name: name, typ: typ, parent: parent}
}

func (p *Parameter) SetPos(pos token.Pos) { p.pos = pos }

// NewFreeVar creates a free variable for a closure.
func NewFreeVar(name string, typ typecheck.Type, parent *Function) *FreeVar {
	return &FreeVar{name: name, typ: typ, parent: parent}
}

func (v *FreeVar) SetPos(pos token.Pos) { v.pos = pos }

// NewBuiltin creates a builtin function reference by name.
func NewBuiltin(name string) *Builtin {
	id := builtinByName(name)
	return &Builtin{id: id, name: name}
}

func builtinByName(name string) BuiltinID {
	switch name {
	case "append":
		return BuiltinAppend
	case "cap":
		return BuiltinCap
	case "clear":
		return BuiltinClear
	case "close":
		return BuiltinClose
	case "complex":
		return BuiltinComplex
	case "copy":
		return BuiltinCopy
	case "delete":
		return BuiltinDelete
	case "imag":
		return BuiltinImag
	case "len":
		return BuiltinLen
	case "make":
		return BuiltinMake
	case "max":
		return BuiltinMax
	case "min":
		return BuiltinMin
	case "new":
		return BuiltinNew
	case "panic":
		return BuiltinPanic
	case "print":
		return BuiltinPrint
	case "println":
		return BuiltinPrintln
	case "real":
		return BuiltinReal
	case "recover":
		return BuiltinRecover
	}
	return BuiltinPanic
}

// ─── Instruction constructors ────────────────────────────────────────────────

func NewAlloc(typ typecheck.Type, heap bool, comment string, block *BasicBlock) *Alloc {
	a := &Alloc{Heap: heap, Comment: comment}
	a.typ = typ
	a.block = block
	return a
}

func NewPhi(typ typecheck.Type, comment string, edges []Value, block *BasicBlock) *Phi {
	p := &Phi{Comment: comment, Edges: edges}
	p.typ = typ
	p.block = block
	return p
}

func NewCall(typ typecheck.Type, cc CallCommon, block *BasicBlock) *Call {
	c := &Call{Call: cc}
	c.typ = typ
	c.block = block
	return c
}

func NewBinOp(typ typecheck.Type, op token.Token, x, y Value, block *BasicBlock) *BinOp {
	b := &BinOp{Op: op, X: x, Y: y}
	b.typ = typ
	b.block = block
	return b
}

func NewUnOp(typ typecheck.Type, op token.Token, x Value, commaOk bool, block *BasicBlock) *UnOp {
	u := &UnOp{Op: op, X: x, CommaOk: commaOk}
	u.typ = typ
	u.block = block
	return u
}

func NewChangeType(typ typecheck.Type, x Value, block *BasicBlock) *ChangeType {
	c := &ChangeType{X: x}
	c.typ = typ
	c.block = block
	return c
}

func NewConvert(typ typecheck.Type, x Value, block *BasicBlock) *Convert {
	c := &Convert{X: x}
	c.typ = typ
	c.block = block
	return c
}

func NewChangeInterface(typ typecheck.Type, x Value, block *BasicBlock) *ChangeInterface {
	c := &ChangeInterface{X: x}
	c.typ = typ
	c.block = block
	return c
}

func NewSliceToArrayPointer(typ typecheck.Type, x Value, block *BasicBlock) *SliceToArrayPointer {
	s := &SliceToArrayPointer{X: x}
	s.typ = typ
	s.block = block
	return s
}

func NewMakeInterface(typ typecheck.Type, x Value, block *BasicBlock) *MakeInterface {
	m := &MakeInterface{X: x}
	m.typ = typ
	m.block = block
	return m
}

func NewMakeClosure(typ typecheck.Type, fn Value, bindings []Value, block *BasicBlock) *MakeClosure {
	m := &MakeClosure{Fn: fn, Bindings: bindings}
	m.typ = typ
	m.block = block
	return m
}

func NewMakeMap(typ typecheck.Type, reserve Value, block *BasicBlock) *MakeMap {
	m := &MakeMap{Reserve: reserve}
	m.typ = typ
	m.block = block
	return m
}

func NewMakeChan(typ typecheck.Type, size Value, block *BasicBlock) *MakeChan {
	m := &MakeChan{Size: size}
	m.typ = typ
	m.block = block
	return m
}

func NewMakeSlice(typ typecheck.Type, len, cap Value, block *BasicBlock) *MakeSlice {
	m := &MakeSlice{Len: len, Cap: cap}
	m.typ = typ
	m.block = block
	return m
}

func NewSlice(typ typecheck.Type, x, low, high, max Value, block *BasicBlock) *Slice {
	s := &Slice{X: x, Low: low, High: high, Max: max}
	s.typ = typ
	s.block = block
	return s
}

func NewFieldAddr(typ typecheck.Type, x Value, field int, block *BasicBlock) *FieldAddr {
	f := &FieldAddr{X: x, Field: field}
	f.typ = typ
	f.block = block
	return f
}

func NewField(typ typecheck.Type, x Value, field int, block *BasicBlock) *Field {
	f := &Field{X: x, Field: field}
	f.typ = typ
	f.block = block
	return f
}

func NewIndexAddr(typ typecheck.Type, x, index Value, block *BasicBlock) *IndexAddr {
	i := &IndexAddr{X: x, Index: index}
	i.typ = typ
	i.block = block
	return i
}

func NewIndex(typ typecheck.Type, x, index Value, block *BasicBlock) *Index {
	i := &Index{X: x, Index: index}
	i.typ = typ
	i.block = block
	return i
}

func NewLookup(typ typecheck.Type, x, index Value, commaOk bool, block *BasicBlock) *Lookup {
	l := &Lookup{X: x, Index: index, CommaOk: commaOk}
	l.typ = typ
	l.block = block
	return l
}

func NewSelect(typ typecheck.Type, states []*SelectState, blocking bool, block *BasicBlock) *Select {
	s := &Select{States: states, Blocking: blocking}
	s.typ = typ
	s.block = block
	return s
}

func NewRange(typ typecheck.Type, x Value, block *BasicBlock) *Range {
	r := &Range{X: x}
	r.typ = typ
	r.block = block
	return r
}

func NewNext(typ typecheck.Type, iter Value, isString bool, block *BasicBlock) *Next {
	n := &Next{Iter: iter, IsString: isString}
	n.typ = typ
	n.block = block
	return n
}

func NewTypeAssert(typ typecheck.Type, x Value, assertedType typecheck.Type, commaOk bool, block *BasicBlock) *TypeAssert {
	t := &TypeAssert{X: x, AssertedType: assertedType, CommaOk: commaOk}
	t.typ = typ
	t.block = block
	return t
}

func NewExtract(typ typecheck.Type, tuple Value, index int, block *BasicBlock) *Extract {
	e := &Extract{Tuple: tuple, Index: index}
	e.typ = typ
	e.block = block
	return e
}

// Terminators.

func NewJump(comment string, block *BasicBlock) *Jump {
	j := &Jump{Comment: comment}
	j.block = block
	return j
}

func NewIf(cond Value, block *BasicBlock) *If {
	i := &If{Cond: cond}
	i.block = block
	return i
}

func NewReturn(results []Value, block *BasicBlock) *Return {
	r := &Return{Results: results}
	r.block = block
	return r
}

func (r *Return) SetPos(p token.Pos) { r.pos = p }

func NewRunDefers(block *BasicBlock) *RunDefers {
	rd := &RunDefers{}
	rd.block = block
	return rd
}

func NewPanic(x Value, block *BasicBlock) *Panic {
	p := &Panic{X: x}
	p.block = block
	return p
}

func (p *Panic) SetPos(pos token.Pos) { p.pos = pos }

// Side-effecting instructions.

func NewGo(cc CallCommon, block *BasicBlock) *Go {
	g := &Go{Call: cc}
	g.block = block
	return g
}

func (g *Go) SetPos(p token.Pos) { g.pos = p }

func NewDefer(cc CallCommon, block *BasicBlock) *Defer {
	d := &Defer{Call: cc}
	d.block = block
	return d
}

func (d *Defer) SetPos(p token.Pos) { d.pos = p }

func NewSend(ch, x Value, block *BasicBlock) *Send {
	s := &Send{Chan: ch, X: x}
	s.block = block
	return s
}

func (s *Send) SetPos(p token.Pos) { s.pos = p }

func NewStore(addr, val Value, block *BasicBlock) *Store {
	s := &Store{Addr: addr, Val: val}
	s.block = block
	return s
}

func (s *Store) SetPos(p token.Pos) { s.pos = p }

func NewMapUpdate(m, key, val Value, block *BasicBlock) *MapUpdate {
	mu := &MapUpdate{Map: m, Key: key, Value: val}
	mu.block = block
	return mu
}

func (m *MapUpdate) SetPos(p token.Pos) { m.pos = p }

func NewDebugRef(x Value, obj typecheck.Object, isAddr bool, block *BasicBlock) *DebugRef {
	d := &DebugRef{X: x, object: obj, IsAddr: isAddr}
	d.block = block
	return d
}

// SetPos on register-based instructions (Alloc, Phi, Call, etc.)
func (r *register) SetPos(p token.Pos) { r.pos = p }
func (r *register) SetName(n string)   { r.name = n }

// SetPos on CallCommon.
func (c *CallCommon) SetPos(p token.Pos) { c.pos = p }
