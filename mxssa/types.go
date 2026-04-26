// Package mxssa defines the Moxie SSA intermediate representation.
// It replaces golang.org/x/tools/go/ssa in B3 when the native type checker
// and SSA builder are complete.
//
// The type system mirrors go/ssa's structure with typecheck.Type replacing
// go/types.Type throughout. The builder accepts *syntax.File + typecheck.Info
// instead of go/ast.File + types.Info.
package mxssa

import (
	"go/ast"
	"go/constant"
	"go/token"

	"moxie/typecheck"
)

// ─── Interfaces ───────────────────────────────────────────────────────────────

// Value is anything that produces a value in the SSA program.
type Value interface {
	// Name returns the SSA name of this value.
	Name() string
	// String returns a human-readable representation.
	String() string
	// Type returns the type of this value.
	Type() typecheck.Type
	// Parent returns the enclosing function, or nil for package-level values.
	Parent() *Function
	// Referrers returns the instruction list that use this value.
	// The caller may append to it.
	Referrers() *[]Instruction
	// Pos returns the source position.
	Pos() token.Pos

	setType(typecheck.Type)
	setName(string)
	addReferrer(Instruction)
}

// Instruction is a statement in a BasicBlock.
type Instruction interface {
	// Block returns the enclosing basic block.
	Block() *BasicBlock
	// Parent returns the enclosing function.
	Parent() *Function
	// Pos returns the source position.
	Pos() token.Pos
	// Operands fills rands with pointers to operand Values and returns the slice.
	// Used for graph traversal and register renaming.
	Operands(rands []*Value) []*Value
	// String returns a disassembly-style representation.
	String() string

	setBlock(*BasicBlock)
}

// Member is a package-level declaration: function, variable, const, or type.
type Member interface {
	Name() string
	String() string
	RelString(*typecheck.Package) string
	Pos() token.Pos
	Type() typecheck.Type
	Package() *Package
	Object() typecheck.Object
}

// ─── Program / Package ────────────────────────────────────────────────────────

// Program is the top-level container for all packages in an SSA program.
type Program struct {
	Fset      *token.FileSet
	imported  map[string]*Package
	packages  map[*typecheck.Package]*Package
}

func NewProgram(fset *token.FileSet) *Program {
	return &Program{
		Fset:     fset,
		imported: map[string]*Package{},
		packages: map[*typecheck.Package]*Package{},
	}
}

// ImportedPackage returns the package for the given import path, or nil.
func (prog *Program) ImportedPackage(path string) *Package {
	return prog.imported[path]
}

// Package is a single package in SSA form.
type Package struct {
	Prog    *Program
	Pkg     *typecheck.Package
	Members map[string]Member
	init    *Function
	debug   bool
}

func (p *Package) Func(name string) *Function {
	m, _ := p.Members[name].(*Function)
	return m
}

// ─── Function ─────────────────────────────────────────────────────────────────

// Function is a named or anonymous function in SSA form.
type Function struct {
	name      string
	object    *typecheck.Func
	Signature *typecheck.Signature
	pos       token.Pos

	Synthetic string // "" for real source functions

	// SyntaxNode is the AST node for this function (nil for synthetic
	// functions or external declarations). Used by AST-based checks like
	// fallthrough detection. Populated by ssabridge from f.Syntax().
	SyntaxNode ast.Node

	Pkg  *Package
	Prog *Program

	// Body (nil if external)
	Params    []*Parameter
	FreeVars  []*FreeVar
	Locals    []*Alloc
	Blocks    []*BasicBlock
	Recover   *BasicBlock
	AnonFuncs []*Function

	referrers []Instruction
	parent    *Function

	// type parameter info (generics)
	typeparams []typecheck.Type
	typeargs   []typecheck.Type

	// builder state (cleared after Build)
	currentBlock *BasicBlock
	vars         map[typecheck.Object]Value
	results      []*Alloc
}

func (f *Function) Name() string    { return f.name }
func (f *Function) String() string  { return f.name }
func (f *Function) Pos() token.Pos  { return f.pos }
func (f *Function) Type() typecheck.Type {
	if f.Signature == nil {
		return nil
	}
	return f.Signature
}
func (f *Function) Parent() *Function   { return f.parent }
func (f *Function) Referrers() *[]Instruction { return &f.referrers }
func (f *Function) Package() *Package   { return f.Pkg }
func (f *Function) Object() typecheck.Object { return f.object }
func (f *Function) RelString(from *typecheck.Package) string {
	if from != nil && f.Pkg != nil && f.Pkg.Pkg == from {
		return f.name
	}
	if f.Pkg != nil {
		return f.Pkg.Pkg.Path() + "." + f.name
	}
	return f.name
}
func (f *Function) TypeParams() []typecheck.Type { return f.typeparams }
func (f *Function) Syntax() ast.Node              { return f.SyntaxNode }

func (f *Function) setName(n string)                   { f.name = n }
func (f *Function) setType(t typecheck.Type)           {}
func (f *Function) addReferrer(i Instruction)          { f.referrers = append(f.referrers, i) }

// ─── BasicBlock ───────────────────────────────────────────────────────────────

// BasicBlock is a maximal sequence of non-branching instructions.
type BasicBlock struct {
	Index   int
	Comment string
	parent  *Function
	Instrs  []Instruction
	Preds   []*BasicBlock
	Succs   []*BasicBlock
}

func (b *BasicBlock) Parent() *Function { return b.parent }

// NewBasicBlock appends a new basic block to the function.
func NewBasicBlock(parent *Function, comment string) *BasicBlock {
	b := &BasicBlock{
		Index:   len(parent.Blocks),
		Comment: comment,
		parent:  parent,
	}
	parent.Blocks = append(parent.Blocks, b)
	return b
}

// WireReferrers walks every instruction in f and registers it as a referrer of
// each operand Value it uses. Required after translation so Value.Referrers()
// returns the correct usage list — checks like "channel created but never
// used" depend on this.
//
// ssabridge.translateFunctionBody does not populate referrers automatically;
// callers must invoke WireReferrers after the body is fully translated (when
// all operand instructions have been created).
func (f *Function) WireReferrers() {
	for _, bb := range f.Blocks {
		for _, instr := range bb.Instrs {
			for _, op := range instr.Operands(nil) {
				if op == nil || *op == nil {
					continue
				}
				(*op).addReferrer(instr)
			}
		}
	}
}

// ─── register (shared embed for instructions that define a value) ─────────────

type register struct {
	anInstruction
	name      string
	typ       typecheck.Type
	pos       token.Pos
	referrers []Instruction
}

func (r *register) Name() string                    { return r.name }
func (r *register) Type() typecheck.Type            { return r.typ }
func (r *register) Pos() token.Pos                  { return r.pos }
func (r *register) Referrers() *[]Instruction       { return &r.referrers }
func (r *register) Parent() *Function               { return r.block.parent }
func (r *register) setName(n string)                { r.name = n }
func (r *register) setType(t typecheck.Type)        { r.typ = t }
func (r *register) addReferrer(i Instruction)       { r.referrers = append(r.referrers, i) }

// anInstruction is embedded by all Instructions.
type anInstruction struct {
	block *BasicBlock
}

func (a *anInstruction) Block() *BasicBlock          { return a.block }
func (a *anInstruction) Parent() *Function           { return a.block.parent }
func (a *anInstruction) setBlock(b *BasicBlock)      { a.block = b }

// ─── Package-level values ─────────────────────────────────────────────────────

// Global is a package-level variable.
type Global struct {
	name   string
	object *typecheck.Var
	typ    typecheck.Type
	pos    token.Pos
	pkg    *Package

	referrers []Instruction
}

func (g *Global) Name() string                    { return g.name }
func (g *Global) String() string                  { return g.pkg.Pkg.Path() + "." + g.name }
func (g *Global) Type() typecheck.Type            { return g.typ }
func (g *Global) Pos() token.Pos                  { return g.pos }
func (g *Global) Parent() *Function               { return nil }
func (g *Global) Referrers() *[]Instruction       { return &g.referrers }
func (g *Global) Package() *Package               { return g.pkg }
func (g *Global) Object() typecheck.Object        { return g.object }
func (g *Global) RelString(from *typecheck.Package) string {
	if from != nil && g.pkg != nil && g.pkg.Pkg == from {
		return g.name
	}
	return g.String()
}
func (g *Global) setName(n string)          { g.name = n }
func (g *Global) setType(t typecheck.Type)  { g.typ = t }
func (g *Global) addReferrer(i Instruction) { g.referrers = append(g.referrers, i) }

// Type_ is a package-level named type member (named Type to avoid collision).
type Type_ struct {
	object *typecheck.TypeName
	pkg    *Package
}

func (t *Type_) Name() string               { return t.object.Name() }
func (t *Type_) String() string             { return t.pkg.Pkg.Path() + "." + t.object.Name() }
func (t *Type_) Type() typecheck.Type       { return t.object.Type() }
func (t *Type_) Pos() token.Pos             { return 0 }
func (t *Type_) Package() *Package          { return t.pkg }
func (t *Type_) Object() typecheck.Object   { return t.object }
func (t *Type_) RelString(from *typecheck.Package) string {
	if from != nil && t.pkg != nil && t.pkg.Pkg == from {
		return t.object.Name()
	}
	return t.String()
}

// NamedConst is a package-level named constant.
type NamedConst struct {
	object *typecheck.Const
	Value  *Const
	pkg    *Package
}

func (c *NamedConst) Name() string               { return c.object.Name() }
func (c *NamedConst) String() string             { return c.pkg.Pkg.Path() + "." + c.object.Name() }
func (c *NamedConst) Type() typecheck.Type       { return c.object.Type() }
func (c *NamedConst) Pos() token.Pos             { return 0 }
func (c *NamedConst) Package() *Package          { return c.pkg }
func (c *NamedConst) Object() typecheck.Object   { return c.object }
func (c *NamedConst) RelString(from *typecheck.Package) string {
	if from != nil && c.pkg != nil && c.pkg.Pkg == from {
		return c.object.Name()
	}
	return c.String()
}

// ─── Parameter / FreeVar / Const / Builtin ────────────────────────────────────

// Parameter is a function parameter (includes method receiver).
type Parameter struct {
	name      string
	object    *typecheck.Var
	typ       typecheck.Type
	pos       token.Pos
	parent    *Function
	referrers []Instruction
}

func (p *Parameter) Name() string                    { return p.name }
func (p *Parameter) String() string                  { return p.name }
func (p *Parameter) Type() typecheck.Type            { return p.typ }
func (p *Parameter) Pos() token.Pos                  { return p.pos }
func (p *Parameter) Parent() *Function               { return p.parent }
func (p *Parameter) Referrers() *[]Instruction       { return &p.referrers }
func (p *Parameter) Object() typecheck.Object        { return p.object }
func (p *Parameter) setName(n string)                { p.name = n }
func (p *Parameter) setType(t typecheck.Type)        { p.typ = t }
func (p *Parameter) addReferrer(i Instruction)       { p.referrers = append(p.referrers, i) }

// FreeVar is a free variable captured by a closure.
type FreeVar struct {
	name      string
	typ       typecheck.Type
	pos       token.Pos
	parent    *Function
	referrers []Instruction
}

func (v *FreeVar) Name() string                    { return v.name }
func (v *FreeVar) String() string                  { return v.name }
func (v *FreeVar) Type() typecheck.Type            { return v.typ }
func (v *FreeVar) Pos() token.Pos                  { return v.pos }
func (v *FreeVar) Parent() *Function               { return v.parent }
func (v *FreeVar) Referrers() *[]Instruction       { return &v.referrers }
func (v *FreeVar) setName(n string)                { v.name = n }
func (v *FreeVar) setType(t typecheck.Type)        { v.typ = t }
func (v *FreeVar) addReferrer(i Instruction)       { v.referrers = append(v.referrers, i) }
func (v *FreeVar) Operands(rands []*Value) []*Value { return rands }

// Const is a constant value.
type Const struct {
	typ  typecheck.Type
	val  constant.Value
	referrers []Instruction
}

func NewConst(val constant.Value, typ typecheck.Type) *Const {
	return &Const{typ: typ, val: val}
}

func (c *Const) Name() string {
	if c.val == nil {
		return "nil"
	}
	return c.val.String()
}
func (c *Const) String() string                  { return c.Name() }
func (c *Const) Type() typecheck.Type            { return c.typ }
func (c *Const) Pos() token.Pos                  { return 0 }
func (c *Const) Parent() *Function               { return nil }
func (c *Const) Referrers() *[]Instruction       { return &c.referrers }
func (c *Const) Value() constant.Value           { return c.val }
func (c *Const) IsNil() bool                     { return c.val == nil && nillable(c.typ) }
func (c *Const) setName(n string)                {}
func (c *Const) setType(t typecheck.Type)        { c.typ = t }
func (c *Const) addReferrer(i Instruction)       { c.referrers = append(c.referrers, i) }

// BuiltinID identifies a Go built-in function.
type BuiltinID int

const (
	BuiltinAppend BuiltinID = iota
	BuiltinCap
	BuiltinClear
	BuiltinClose
	BuiltinComplex
	BuiltinCopy
	BuiltinDelete
	BuiltinImag
	BuiltinLen
	BuiltinMake
	BuiltinMax
	BuiltinMin
	BuiltinNew
	BuiltinPanic
	BuiltinPrint
	BuiltinPrintln
	BuiltinReal
	BuiltinRecover
)

// Builtin represents a use of a Go built-in as a function value.
type Builtin struct {
	id        BuiltinID
	name      string
	referrers []Instruction
}

func (b *Builtin) Name() string                    { return b.name }
func (b *Builtin) String() string                  { return b.name }
func (b *Builtin) Type() typecheck.Type            { return nil }
func (b *Builtin) Pos() token.Pos                  { return 0 }
func (b *Builtin) Parent() *Function               { return nil }
func (b *Builtin) Referrers() *[]Instruction       { return &b.referrers }
func (b *Builtin) ID() BuiltinID                   { return b.id }
func (b *Builtin) setName(n string)                { b.name = n }
func (b *Builtin) setType(t typecheck.Type)        {}
func (b *Builtin) addReferrer(i Instruction)       { b.referrers = append(b.referrers, i) }

// ─── CallCommon ───────────────────────────────────────────────────────────────

// CallCommon describes a function or method call.
// It is shared between Call, Go, and Defer instructions.
type CallCommon struct {
	Value    Value              // for static call: *Function or *Builtin; for interface call: the receiver
	Method   *typecheck.Func   // non-nil for interface calls; nil for static calls
	Args     []Value
	pos      token.Pos
	IsInvoke bool // true for interface method calls
}

func (c *CallCommon) Pos() token.Pos { return c.pos }

func (c *CallCommon) Operands(rands []*Value) []*Value {
	rands = append(rands, &c.Value)
	for i := range c.Args {
		rands = append(rands, &c.Args[i])
	}
	return rands
}

// ─── Instructions ─────────────────────────────────────────────────────────────

// Alloc allocates space for a variable of type T.
// Heap == true means heap allocation (new(T) or &T{}).
// Heap == false means stack allocation (function-local var).
type Alloc struct {
	register
	Heap    bool
	Comment string
}

func (a *Alloc) Operands(rands []*Value) []*Value { return rands }
func (a *Alloc) String() string {
	if a.Heap {
		return "new " + a.typ.String()
	}
	return "local " + a.typ.String()
}

// Phi is a Φ-node: a join point that selects a value based on the predecessor.
type Phi struct {
	register
	Comment string
	Edges   []Value // must match len(block.Preds)
}

func (p *Phi) Operands(rands []*Value) []*Value {
	for i := range p.Edges {
		rands = append(rands, &p.Edges[i])
	}
	return rands
}
func (p *Phi) String() string { return "phi " + p.Comment }

// Call is a function call: result = f(args...).
type Call struct {
	register
	Call CallCommon
}

func (c *Call) Operands(rands []*Value) []*Value { return c.Call.Operands(rands) }
func (c *Call) Common() *CallCommon              { return &c.Call }
func (c *Call) String() string                   { return "call " + c.Call.Value.Name() }

// BinOp is a binary operation: result = X op Y.
type BinOp struct {
	register
	Op token.Token
	X  Value
	Y  Value
}

func (b *BinOp) Operands(rands []*Value) []*Value {
	return append(rands, &b.X, &b.Y)
}
func (b *BinOp) String() string { return "binop " + b.Op.String() }

// UnOp is a unary operation: result = op X.
type UnOp struct {
	register
	Op      token.Token
	X       Value
	CommaOk bool // for *x yielding (T, bool)
}

func (u *UnOp) Operands(rands []*Value) []*Value {
	return append(rands, &u.X)
}
func (u *UnOp) String() string { return "unop " + u.Op.String() }

// ChangeType is a type change with no runtime effect (type assertion on interface).
type ChangeType struct {
	register
	X Value
}

func (c *ChangeType) Operands(rands []*Value) []*Value { return append(rands, &c.X) }
func (c *ChangeType) String() string                    { return "changetype " + c.typ.String() }

// Convert is a value conversion T(x).
type Convert struct {
	register
	X Value
}

func (c *Convert) Operands(rands []*Value) []*Value { return append(rands, &c.X) }
func (c *Convert) String() string                    { return "convert " + c.typ.String() }

// ChangeInterface changes the interface type of a value.
type ChangeInterface struct {
	register
	X Value
}

func (c *ChangeInterface) Operands(rands []*Value) []*Value { return append(rands, &c.X) }
func (c *ChangeInterface) String() string                    { return "changeinterface" }

// SliceToArrayPointer converts a slice to an array pointer.
type SliceToArrayPointer struct {
	register
	X Value
}

func (s *SliceToArrayPointer) Operands(rands []*Value) []*Value { return append(rands, &s.X) }
func (s *SliceToArrayPointer) String() string                    { return "slicearrptr" }

// MakeInterface boxes a value into an interface.
type MakeInterface struct {
	register
	X Value
}

func (m *MakeInterface) Operands(rands []*Value) []*Value { return append(rands, &m.X) }
func (m *MakeInterface) String() string                    { return "makeinterface " + m.X.Type().String() }

// MakeClosure creates a closure: {fn, freevar...}.
type MakeClosure struct {
	register
	Fn      Value   // *Function
	Bindings []Value // free variable bindings (in order)
}

func (m *MakeClosure) Operands(rands []*Value) []*Value {
	rands = append(rands, &m.Fn)
	for i := range m.Bindings {
		rands = append(rands, &m.Bindings[i])
	}
	return rands
}
func (m *MakeClosure) String() string { return "makeclosure" }

// MakeMap allocates a new map.
type MakeMap struct {
	register
	Reserve Value // initial capacity hint (may be nil)
}

func (m *MakeMap) Operands(rands []*Value) []*Value {
	if m.Reserve != nil {
		return append(rands, &m.Reserve)
	}
	return rands
}
func (m *MakeMap) String() string { return "makemap " + m.typ.String() }

// MakeChan allocates a new channel.
type MakeChan struct {
	register
	Size Value // buffer size (may be nil for unbuffered)
}

func (m *MakeChan) Operands(rands []*Value) []*Value {
	if m.Size != nil {
		return append(rands, &m.Size)
	}
	return rands
}
func (m *MakeChan) String() string { return "makechan " + m.typ.String() }

// MakeSlice allocates a new slice.
type MakeSlice struct {
	register
	Len Value
	Cap Value
}

func (m *MakeSlice) Operands(rands []*Value) []*Value {
	rands = append(rands, &m.Len)
	if m.Cap != nil {
		rands = append(rands, &m.Cap)
	}
	return rands
}
func (m *MakeSlice) String() string { return "makeslice " + m.typ.String() }

// Slice produces a slice of x[low:high:max].
type Slice struct {
	register
	X    Value  // slice, string, or *array
	Low  Value  // may be nil
	High Value  // may be nil
	Max  Value  // may be nil (3-index slice only)
}

func (s *Slice) Operands(rands []*Value) []*Value {
	rands = append(rands, &s.X)
	if s.Low != nil {
		rands = append(rands, &s.Low)
	}
	if s.High != nil {
		rands = append(rands, &s.High)
	}
	if s.Max != nil {
		rands = append(rands, &s.Max)
	}
	return rands
}
func (s *Slice) String() string { return "slice" }

// FieldAddr computes &struct.field.
type FieldAddr struct {
	register
	X     Value
	Field int
}

func (f *FieldAddr) Operands(rands []*Value) []*Value { return append(rands, &f.X) }
func (f *FieldAddr) String() string                    { return "fieldaddr" }

// Field extracts a field from a struct value (by copy).
type Field struct {
	register
	X     Value
	Field int
}

func (f *Field) Operands(rands []*Value) []*Value { return append(rands, &f.X) }
func (f *Field) String() string                    { return "field" }

// IndexAddr computes &x[i].
type IndexAddr struct {
	register
	X     Value
	Index Value
}

func (i *IndexAddr) Operands(rands []*Value) []*Value {
	return append(rands, &i.X, &i.Index)
}
func (i *IndexAddr) String() string { return "indexaddr" }

// Index extracts an element from an array (by copy).
type Index struct {
	register
	X     Value
	Index Value
}

func (i *Index) Operands(rands []*Value) []*Value {
	return append(rands, &i.X, &i.Index)
}
func (i *Index) String() string { return "index" }

// Lookup computes x[key] for a map, and optionally returns (v, ok).
type Lookup struct {
	register
	X       Value
	Index   Value
	CommaOk bool
}

func (l *Lookup) Operands(rands []*Value) []*Value {
	return append(rands, &l.X, &l.Index)
}
func (l *Lookup) String() string { return "lookup" }

// SelectState describes a case in a select statement.
type SelectState struct {
	Dir       token.Token // token.SEND or token.RECV
	Chan      Value
	Send      Value // nil for recv
	Pos       token.Pos
	DebugRef0 Value
}

// Select implements a select statement.
type Select struct {
	register
	States   []*SelectState
	Blocking bool
}

func (s *Select) Operands(rands []*Value) []*Value {
	for _, st := range s.States {
		rands = append(rands, &st.Chan)
		if st.Send != nil {
			rands = append(rands, &st.Send)
		}
	}
	return rands
}
func (s *Select) String() string { return "select" }

// Range creates an iterator for ranging over a string or map.
type Range struct {
	register
	X Value
}

func (r *Range) Operands(rands []*Value) []*Value { return append(rands, &r.X) }
func (r *Range) String() string                    { return "range" }

// Next advances an iterator: (ok, k, v) = next(iter).
type Next struct {
	register
	Iter    Value
	IsString bool
}

func (n *Next) Operands(rands []*Value) []*Value { return append(rands, &n.Iter) }
func (n *Next) String() string                    { return "next" }

// TypeAssert tests or converts an interface value: v, ok = x.(T).
type TypeAssert struct {
	register
	X            Value
	AssertedType typecheck.Type
	CommaOk      bool
}

func (t *TypeAssert) Operands(rands []*Value) []*Value { return append(rands, &t.X) }
func (t *TypeAssert) String() string                    { return "typeassert " + t.AssertedType.String() }

// Extract extracts the ith component from a multi-return tuple.
type Extract struct {
	register
	Tuple Value
	Index int
}

func (e *Extract) Operands(rands []*Value) []*Value { return append(rands, &e.Tuple) }
func (e *Extract) String() string                    { return "extract" }

// ─── Terminator instructions ──────────────────────────────────────────────────

// Jump is an unconditional branch.
type Jump struct {
	anInstruction
	Comment string
}

func (j *Jump) Operands(rands []*Value) []*Value { return rands }
func (j *Jump) Pos() token.Pos                   { return 0 }
func (j *Jump) String() string                   { return "jump → " + j.Comment }

// If is a conditional branch: if cond goto t else f.
type If struct {
	anInstruction
	Cond Value
}

func (i *If) Operands(rands []*Value) []*Value { return append(rands, &i.Cond) }
func (i *If) Pos() token.Pos                   { return 0 }
func (i *If) String() string                   { return "if " + i.Cond.Name() }

// Return returns from the current function.
type Return struct {
	anInstruction
	Results []Value
	pos     token.Pos
}

func (r *Return) Operands(rands []*Value) []*Value {
	for i := range r.Results {
		rands = append(rands, &r.Results[i])
	}
	return rands
}
func (r *Return) Pos() token.Pos { return r.pos }
func (r *Return) String() string { return "return" }

// RunDefers executes all deferred calls.
type RunDefers struct {
	anInstruction
}

func (rd *RunDefers) Operands(rands []*Value) []*Value { return rands }
func (rd *RunDefers) Pos() token.Pos                   { return 0 }
func (rd *RunDefers) String() string                   { return "rundefers" }

// Panic triggers a runtime panic.
type Panic struct {
	anInstruction
	X   Value
	pos token.Pos
}

func (p *Panic) Operands(rands []*Value) []*Value { return append(rands, &p.X) }
func (p *Panic) Pos() token.Pos                   { return p.pos }
func (p *Panic) String() string                   { return "panic" }

// ─── Spawn / goroutine instructions ──────────────────────────────────────────

// Go starts a goroutine (go stmt). In Moxie this is blocked; in stdlib code it
// may still appear. Kept for compatibility with stdlib imports until B3.
type Go struct {
	anInstruction
	Call CallCommon
	pos  token.Pos
}

func (g *Go) Operands(rands []*Value) []*Value { return g.Call.Operands(rands) }
func (g *Go) Pos() token.Pos                   { return g.pos }
func (g *Go) Common() *CallCommon              { return &g.Call }
func (g *Go) String() string                   { return "go" }

// Defer queues a deferred call.
type Defer struct {
	anInstruction
	Call CallCommon
	pos  token.Pos
}

func (d *Defer) Operands(rands []*Value) []*Value { return d.Call.Operands(rands) }
func (d *Defer) Pos() token.Pos                   { return d.pos }
func (d *Defer) Common() *CallCommon              { return &d.Call }
func (d *Defer) String() string                   { return "defer" }

// ─── Side-effecting instructions ──────────────────────────────────────────────

// Send sends a value on a channel.
type Send struct {
	anInstruction
	Chan  Value
	X     Value
	pos   token.Pos
}

func (s *Send) Operands(rands []*Value) []*Value { return append(rands, &s.Chan, &s.X) }
func (s *Send) Pos() token.Pos                   { return s.pos }
func (s *Send) String() string                   { return "send" }

// Store stores a value at an address.
type Store struct {
	anInstruction
	Addr  Value
	Val   Value
	pos   token.Pos
}

func (s *Store) Operands(rands []*Value) []*Value { return append(rands, &s.Addr, &s.Val) }
func (s *Store) Pos() token.Pos                   { return s.pos }
func (s *Store) String() string                   { return "store" }

// MapUpdate sets a map entry: m[k] = v.
type MapUpdate struct {
	anInstruction
	Map   Value
	Key   Value
	Value Value
	pos   token.Pos
}

func (m *MapUpdate) Operands(rands []*Value) []*Value {
	return append(rands, &m.Map, &m.Key, &m.Value)
}
func (m *MapUpdate) Pos() token.Pos { return m.pos }
func (m *MapUpdate) String() string { return "mapupdate" }

// DebugRef is a debug reference: associates an ast node with a value.
// Used to generate DWARF variable entries.
type DebugRef struct {
	anInstruction
	Expr   any // the syntax node (kept as any to avoid syntax import cycle)
	object typecheck.Object
	IsAddr bool
	X      Value
}

func (d *DebugRef) Operands(rands []*Value) []*Value { return append(rands, &d.X) }
func (d *DebugRef) Pos() token.Pos                   { return 0 }
func (d *DebugRef) Object() typecheck.Object         { return d.object }
func (d *DebugRef) String() string                   { return "debugref" }

func nillable(t typecheck.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *typecheck.Pointer, *typecheck.Slice, *typecheck.Chan, *typecheck.Map, *typecheck.Signature:
		return true
	case *typecheck.Interface:
		return true
	default:
		return false
	}
}
