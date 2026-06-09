package typecheck

import "moxie/syntax"

// TypeAndValue records the type and optional constant value for an expression.
type TypeAndValue struct {
	Type  Type
	Value ConstVal // nil for non-constant expressions
	mode  exprMode
}

type exprMode uint8

const (
	modeInvalid exprMode = iota
	modeVoid             // no value (statement context)
	modeValue            // an addressable or non-addressable value
	modeVar              // addressable variable
	modeConst            // constant expression
	modeType             // a type expression
	modeBuiltin          // a builtin identifier
	modePkg              // a package name
	modeNil              // the predeclared nil
)

func (tv TypeAndValue) IsValue() bool   { return tv.mode == modeValue || tv.mode == modeVar || tv.mode == modeConst || tv.mode == modeNil }
func (tv TypeAndValue) IsType() bool    { return tv.mode == modeType }
func (tv TypeAndValue) IsBuiltin() bool { return tv.mode == modeBuiltin }
func (tv TypeAndValue) IsConst() bool   { return tv.mode == modeConst }
func (tv TypeAndValue) Addressable() bool { return tv.mode == modeVar }

// Info holds the result of type-checking a set of syntax.Files.
// Consumers access type info for each expression and identifier.
type Info struct {
	// Types maps each expression to its type and value.
	// For type expressions, TypeAndValue.IsType() is true.
	// For constants, TypeAndValue.Value is non-nil.
	Types map[syntax.Expr]TypeAndValue

	// Defs maps each identifier that declares something to the object it declares.
	// nil objects represent anonymous declarations (blank _ idents).
	Defs map[*syntax.Name]Object

	// Uses maps each identifier that is used (not declared) to the object it denotes.
	Uses map[*syntax.Name]Object

	// Implicits maps nodes to the object they implicitly declare.
	// For imports: ImportDecl → *PkgName
	// For type switches: the guarded var in each case clause
	Implicits map[syntax.Node]Object

	// Scopes maps AST nodes to the scope they open.
	// File → file scope, FuncDecl/FuncLit → function scope, etc.
	Scopes map[syntax.Node]*Scope

	// Selections maps selector expressions X.f to the selection.
	Selections map[*syntax.SelectorExpr]*Selection
}

// Selection describes a field or method selection X.f.
type Selection struct {
	kind  SelectionKind
	recv  Type   // type of X
	obj   Object // selected field or method
	index []int  // path of field indices (for embedded fields)
	indir bool   // true if recv contains a pointer indirection
}

type SelectionKind int

const (
	FieldVal SelectionKind = iota
	MethodVal
	MethodExpr
)

func (s *Selection) Kind() SelectionKind { return s.kind }
func (s *Selection) Recv() Type          { return s.recv }
func (s *Selection) Obj() Object         { return s.obj }
func (s *Selection) Index() []int        { return s.index }
func (s *Selection) Indirect() bool      { return s.indir }
func (s *Selection) Type() Type {
	switch s.kind {
	case FieldVal:
		return s.obj.Type()
	case MethodVal:
		sig := s.obj.(*Func).Signature()
		// bound method: drop the receiver parameter
		return NewSignature(nil, sig.params, sig.results, sig.variadic)
	case MethodExpr:
		// unbound: receiver becomes first parameter
		return s.obj.(*Func).Signature()
	}
	return nil
}

func newInfo() *Info {
	return &Info{
		Types:      map[syntax.Expr]TypeAndValue{},
		Defs:       map[*syntax.Name]Object{},
		Uses:       map[*syntax.Name]Object{},
		Implicits:  map[syntax.Node]Object{},
		Scopes:     map[syntax.Node]*Scope{},
		Selections: map[*syntax.SelectorExpr]*Selection{},
	}
}

// TypeOf returns the type of expression e, or nil if unknown.
func (info *Info) TypeOf(e syntax.Expr) Type {
	if tv, ok := info.Types[e]; ok {
		return tv.Type
	}
	return nil
}

// ObjectOf returns the object denoted by identifier id, or nil.
func (info *Info) ObjectOf(id *syntax.Name) Object {
	if obj, ok := info.Defs[id]; ok {
		return obj
	}
	return info.Uses[id]
}
