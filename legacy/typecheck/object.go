package typecheck

import "fmt"

// Object is a named entity declared in the source: Var, Func, TypeName,
// Const, PkgName, Builtin, or Label.
type Object interface {
	Name() string
	Type() Type
	Pkg() *Package
	Exported() bool
	objectTag() // type-tag to prevent accidental interface satisfaction
}

type object struct {
	pkg  *Package
	name string
	typ  Type
}

func (o *object) Name() string    { return o.name }
func (o *object) Type() Type      { return o.typ }
func (o *object) Pkg() *Package   { return o.pkg }
func (o *object) Exported() bool  { return len(o.name) > 0 && o.name[0] >= 'A' && o.name[0] <= 'Z' }
func (o *object) objectTag()      {}

// ----------------------------------------------------------------------------
// TypeName

type TypeName struct {
	object
}

func NewTypeName(pkg *Package, name string, typ Type) *TypeName {
	return &TypeName{object{pkg: pkg, name: name, typ: typ}}
}
func (o *TypeName) String() string { return "type " + o.name }

// ----------------------------------------------------------------------------
// Func

type Func struct {
	object
	hasPtrRecv bool
}

func NewFunc(pkg *Package, name string, sig *Signature) *Func {
	return &Func{object: object{pkg: pkg, name: name, typ: sig}}
}
func (o *Func) Signature() *Signature {
	if sig, ok := o.typ.(*Signature); ok {
		return sig
	}
	return nil
}
func (o *Func) HasPtrRecv() bool  { return o.hasPtrRecv }
func (o *Func) String() string    { return "func " + o.name }

// ----------------------------------------------------------------------------
// Const

type Const struct {
	object
	val ConstVal
}

func NewConst(pkg *Package, name string, typ Type, val ConstVal) *Const {
	return &Const{object: object{pkg: pkg, name: name, typ: typ}, val: val}
}
func (o *Const) Val() ConstVal { return o.val }
func (o *Const) String() string { return fmt.Sprintf("const %s = %v", o.name, o.val) }

// ConstVal wraps go/constant.Value for constant evaluation.
// We re-export it as an opaque type here so consumers don't need to import go/constant.
// In B4 (full Moxie rewrite) this becomes a native Moxie type.
type ConstVal = interface{ String() string }

// ----------------------------------------------------------------------------
// PkgName (an imported package)

type PkgName struct {
	object
	imported *Package
}

func NewPkgName(pkg *Package, name string, imported *Package) *PkgName {
	return &PkgName{object: object{pkg: pkg, name: name}, imported: imported}
}
func (o *PkgName) Imported() *Package { return o.imported }
func (o *PkgName) String() string     { return "package " + o.name }

// ----------------------------------------------------------------------------
// Builtin

type BuiltinID int

const (
	BuiltinAppend BuiltinID = iota
	BuiltinCap
	BuiltinClear
	BuiltinClose
	BuiltinComplex // not valid in Moxie, rejected by checker
	BuiltinCopy
	BuiltinDelete
	BuiltinImag   // not valid in Moxie
	BuiltinLen
	BuiltinMake   // rejected by checker (use literal syntax)
	BuiltinNew    // rejected by checker (use &T{})
	BuiltinPanic
	BuiltinPrint
	BuiltinPrintln
	BuiltinReal   // not valid in Moxie
	BuiltinRecover
	BuiltinSpawn // Moxie-specific
	BuiltinFree  // Moxie-specific: explicit free()
	BuiltinMin   // Go 1.21
	BuiltinMax   // Go 1.21
)

type Builtin struct {
	object
	id BuiltinID
}

func newBuiltin(name string, id BuiltinID) *Builtin {
	return &Builtin{object: object{name: name}, id: id}
}
func (o *Builtin) ID() BuiltinID  { return o.id }
func (o *Builtin) String() string { return "builtin " + o.name }

// ----------------------------------------------------------------------------
// Label

type Label struct {
	object
	used bool
}

func NewLabel(pkg *Package, name string) *Label {
	return &Label{object: object{pkg: pkg, name: name}}
}
func (o *Label) String() string { return "label " + o.name }
