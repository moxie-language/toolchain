// Package typecheck implements a Moxie-native type checker that consumes
// syntax.File AST nodes directly. It replaces go/types in the compiler
// pipeline once B3 (backend rewrite) is complete.
//
// Type system differences from Go:
//   - string and []byte are the same type (Moxie text unification)
//   - int ≡ int32, uint ≡ uint32 on all targets
//   - no complex64/complex128
//   - no goroutines (go keyword is a compile error)
//   - spawn is a builtin with Codec constraints (not a type-level concept)
package typecheck

import (
	"fmt"
	"strings"

	"moxie/syntax"
)

// Type is an alias for syntax.Type. All concrete types in this package
// implement it, so they can be stored directly in syntax.TypeAndValue without
// wrapping. Type assertions on syntax.TypeAndValue.Type to *Basic, *Struct,
// etc. work transparently.
type Type = syntax.Type

// ----------------------------------------------------------------------------
// BasicKind

type BasicKind int

const (
	Invalid BasicKind = iota

	// boolean
	Bool

	// integer
	Int8
	Int16
	Int32
	Int64
	Uint8
	Uint16
	Uint32
	Uint64

	// float
	Float32
	Float64

	// text — string and []byte are the same underlying kind in Moxie
	String

	// unsafe
	UnsafePointer

	// untyped constants
	UntypedBool
	UntypedInt
	UntypedRune
	UntypedFloat
	UntypedString
	UntypedNil
)

// BasicInfo is a bitmask of type properties.
type BasicInfo uint

const (
	IsBoolean  BasicInfo = 1 << iota
	IsInteger            // includes unsigned
	IsUnsigned           // only for unsigned integer kinds
	IsFloat
	IsString
	IsUntyped
	IsOrdered  // ordered by < (integers, floats, strings)
	IsNumeric  // integers + floats
)

// Basic is an elementary type (bool, int32, string, etc.).
type Basic struct {
	kind BasicKind
	info BasicInfo
	name string
}

func (t *Basic) Kind() BasicKind  { return t.kind }
func (t *Basic) Info() BasicInfo  { return t.info }
func (t *Basic) Name() string     { return t.name }
func (t *Basic) Underlying() Type { return t }
func (t *Basic) String() string   { return t.name }

// ----------------------------------------------------------------------------
// Array

type Array struct {
	len  int64
	elem Type
}

func NewArray(elem Type, n int64) *Array { return &Array{len: n, elem: elem} }
func (t *Array) Len() int64             { return t.len }
func (t *Array) Elem() Type             { return t.elem }
func (t *Array) Underlying() Type       { return t }
func (t *Array) String() string         { return fmt.Sprintf("[%d]%s", t.len, t.elem) }

// ----------------------------------------------------------------------------
// Slice

type Slice struct{ elem Type }

func NewSlice(elem Type) *Slice  { return &Slice{elem: elem} }
func (t *Slice) Elem() Type      { return t.elem }
func (t *Slice) Underlying() Type { return t }
func (t *Slice) String() string   { return "[]" + t.elem.String() }

// ----------------------------------------------------------------------------
// Pointer

type Pointer struct{ base Type }

func NewPointer(base Type) *Pointer { return &Pointer{base: base} }
func (t *Pointer) Elem() Type       { return t.base }
func (t *Pointer) Underlying() Type { return t }
func (t *Pointer) String() string   { return "*" + t.base.String() }

// ----------------------------------------------------------------------------
// Map

type Map struct{ key, elem Type }

func NewMap(key, elem Type) *Map { return &Map{key: key, elem: elem} }
func (t *Map) Key() Type          { return t.key }
func (t *Map) Elem() Type         { return t.elem }
func (t *Map) Underlying() Type   { return t }
func (t *Map) String() string     { return fmt.Sprintf("map[%s]%s", t.key, t.elem) }

// ----------------------------------------------------------------------------
// Chan

type ChanDir int

const (
	SendRecv ChanDir = iota
	SendOnly
	RecvOnly
)

type Chan struct {
	dir  ChanDir
	elem Type
}

func NewChan(dir ChanDir, elem Type) *Chan { return &Chan{dir: dir, elem: elem} }
func (t *Chan) Dir() ChanDir               { return t.dir }
func (t *Chan) Elem() Type                 { return t.elem }
func (t *Chan) Underlying() Type           { return t }
func (t *Chan) String() string {
	switch t.dir {
	case SendOnly:
		return "chan<- " + t.elem.String()
	case RecvOnly:
		return "<-chan " + t.elem.String()
	}
	return "chan " + t.elem.String()
}

// ----------------------------------------------------------------------------
// Var (a field or variable, used in Struct/Signature/Tuple)

type Var struct {
	pkg       *Package
	name      string
	typ       Type
	anonymous bool // embedded field (no explicit name)
	used      bool
	pos       interface{} // syntax.Pos — stored as interface to avoid import cycle
}

func NewVar(pkg *Package, name string, typ Type) *Var {
	return &Var{pkg: pkg, name: name, typ: typ}
}
func NewField(pkg *Package, name string, typ Type, anonymous bool) *Var {
	return &Var{pkg: pkg, name: name, typ: typ, anonymous: anonymous}
}
func (v *Var) Name() string      { return v.name }
func (v *Var) Type() Type        { return v.typ }
func (v *Var) Anonymous() bool   { return v.anonymous }
func (v *Var) Pkg() *Package     { return v.pkg }
func (v *Var) Exported() bool    { return len(v.name) > 0 && v.name[0] >= 'A' && v.name[0] <= 'Z' }
func (v *Var) objectTag()        {}
func (v *Var) String() string    { return v.name }

// ----------------------------------------------------------------------------
// Struct

type Struct struct {
	fields []*Var
	tags   []string
}

func NewStruct(fields []*Var, tags []string) *Struct {
	return &Struct{fields: fields, tags: tags}
}
func (t *Struct) NumFields() int      { return len(t.fields) }
func (t *Struct) Field(i int) *Var    { return t.fields[i] }
func (t *Struct) Tag(i int) string {
	if i < len(t.tags) {
		return t.tags[i]
	}
	return ""
}
func (t *Struct) Underlying() Type { return t }
func (t *Struct) String() string {
	var sb strings.Builder
	sb.WriteString("struct{")
	for i, f := range t.fields {
		if i > 0 {
			sb.WriteString("; ")
		}
		if !f.anonymous {
			sb.WriteString(f.name)
			sb.WriteByte(' ')
		}
		sb.WriteString(f.typ.String())
		if tag := t.Tag(i); tag != "" {
			fmt.Fprintf(&sb, " %q", tag)
		}
	}
	sb.WriteByte('}')
	return sb.String()
}

// ----------------------------------------------------------------------------
// Tuple (multi-return, parameter list)

type Tuple struct{ vars []*Var }

func NewTuple(vars ...*Var) *Tuple { return &Tuple{vars: vars} }
func (t *Tuple) Len() int          { return len(t.vars) }
func (t *Tuple) At(i int) *Var     { return t.vars[i] }
func (t *Tuple) Underlying() Type  { return t }
func (t *Tuple) String() string {
	if t == nil || len(t.vars) == 0 {
		return "()"
	}
	var parts []string
	for _, v := range t.vars {
		parts = append(parts, v.typ.String())
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// ----------------------------------------------------------------------------
// Signature (function type)

type Signature struct {
	recv     *Var   // nil for plain functions
	params   *Tuple
	results  *Tuple
	variadic bool
}

func NewSignature(recv *Var, params, results *Tuple, variadic bool) *Signature {
	return &Signature{recv: recv, params: params, results: results, variadic: variadic}
}
func (t *Signature) Recv() *Var      { return t.recv }
func (t *Signature) Params() *Tuple  { return t.params }
func (t *Signature) Results() *Tuple { return t.results }
func (t *Signature) Variadic() bool  { return t.variadic }
func (t *Signature) Underlying() Type { return t }
func (t *Signature) String() string {
	var sb strings.Builder
	sb.WriteString("func")
	if t.params != nil {
		writeParams(&sb, t.params, t.variadic)
	} else {
		sb.WriteString("()")
	}
	if t.results != nil && t.results.Len() > 0 {
		sb.WriteByte(' ')
		if t.results.Len() == 1 && t.results.At(0).name == "" {
			sb.WriteString(t.results.At(0).typ.String())
		} else {
			writeParams(&sb, t.results, false)
		}
	}
	return sb.String()
}

func writeParams(sb *strings.Builder, t *Tuple, variadic bool) {
	sb.WriteByte('(')
	for i, v := range t.vars {
		if i > 0 {
			sb.WriteString(", ")
		}
		if v.name != "" {
			sb.WriteString(v.name)
			sb.WriteByte(' ')
		}
		if variadic && i == len(t.vars)-1 {
			if sl, ok := v.typ.(*Slice); ok {
				sb.WriteString("...")
				sb.WriteString(sl.elem.String())
				continue
			}
		}
		sb.WriteString(v.typ.String())
	}
	sb.WriteByte(')')
}

// ----------------------------------------------------------------------------
// Interface

// IfaceMethod holds a method signature for an interface.
type IfaceMethod struct {
	name string
	sig  *Signature
}

func (m *IfaceMethod) Name() string       { return m.name }
func (m *IfaceMethod) Sig() *Signature    { return m.sig }

// IfaceMethod constructor used by the bridge package.
func NewIfaceMethod(name string, sig *Signature) *IfaceMethod {
	return &IfaceMethod{name: name, sig: sig}
}

type Interface struct {
	methods    []*IfaceMethod
	embeds     []Type
	complete   bool
	allMethods []*IfaceMethod
}

func NewInterface(methods []*IfaceMethod, embeds []Type) *Interface {
	return &Interface{methods: methods, embeds: embeds}
}
func (t *Interface) NumMethods() int           { return len(t.allMethods) }
func (t *Interface) Method(i int) *IfaceMethod { return t.allMethods[i] }
func (t *Interface) NumExplicitMethods() int   { return len(t.methods) }
func (t *Interface) ExplicitMethod(i int) *IfaceMethod { return t.methods[i] }
func (t *Interface) NumEmbeddeds() int         { return len(t.embeds) }
func (t *Interface) EmbeddedType(i int) Type   { return t.embeds[i] }
func (t *Interface) IsEmpty() bool             { return len(t.allMethods) == 0 }
func (t *Interface) Underlying() Type          { return t }
func (t *Interface) String() string {
	if t.IsEmpty() {
		return "interface{}"
	}
	var sb strings.Builder
	sb.WriteString("interface{")
	for i, m := range t.allMethods {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(m.name)
		// write sig without "func" prefix
		sig := m.sig
		if sig.params != nil {
			writeParams(&sb, sig.params, sig.variadic)
		} else {
			sb.WriteString("()")
		}
		if sig.results != nil && sig.results.Len() > 0 {
			sb.WriteByte(' ')
			if sig.results.Len() == 1 {
				sb.WriteString(sig.results.At(0).typ.String())
			} else {
				writeParams(&sb, sig.results, false)
			}
		}
	}
	sb.WriteByte('}')
	return sb.String()
}

// complete fills allMethods from methods + embeds (called once after all types are resolved).
func (t *Interface) Complete() {
	if t.complete {
		return
	}
	seen := map[string]bool{}
	t.allMethods = append(t.allMethods, t.methods...)
	for _, m := range t.methods {
		seen[m.name] = true
	}
	for _, embed := range t.embeds {
		if embed == nil {
			continue
		}
		u := safeUnderlying(embed)
		if u == nil {
			continue
		}
		if iface, ok := u.(*Interface); ok {
			iface.Complete()
			for _, m := range iface.allMethods {
				if !seen[m.name] {
					t.allMethods = append(t.allMethods, m)
					seen[m.name] = true
				}
			}
		}
	}
	t.complete = true
}

// ----------------------------------------------------------------------------
// TypeParam (generics)

type TypeParam struct {
	id         int
	obj        *TypeName
	constraint Type
}

func NewTypeParam(obj *TypeName, constraint Type) *TypeParam {
	return &TypeParam{obj: obj, constraint: constraint}
}
func (t *TypeParam) Obj() *TypeName     { return t.obj }
func (t *TypeParam) Constraint() Type   { return t.constraint }
func (t *TypeParam) Underlying() Type   { return t }
func (t *TypeParam) String() string {
	if t.obj != nil {
		return t.obj.name
	}
	return fmt.Sprintf("T%d", t.id)
}

// ----------------------------------------------------------------------------
// Named (named types: type Foo struct{...})

type Named struct {
	obj        *TypeName  // the type name declaration
	underlying Type       // the underlying type
	methods    []*Func    // methods with this type as receiver
	tparams    []*TypeParam
	targs      []Type // set when instantiated
}

func NewNamed(obj *TypeName, underlying Type) *Named {
	n := &Named{obj: obj, underlying: underlying}
	if obj != nil {
		obj.typ = n
	}
	return n
}
func (t *Named) Obj() *TypeName         { return t.obj }
func (t *Named) NumMethods() int        { return len(t.methods) }
func (t *Named) Method(i int) *Func     { return t.methods[i] }
func (t *Named) AddMethod(m *Func)      { t.methods = append(t.methods, m) }
func (t *Named) TypeParams() []*TypeParam { return t.tparams }
func (t *Named) TypeArgs() []Type        { return t.targs }
func (t *Named) Underlying() Type {
	if t.underlying != nil {
		return t.underlying.Underlying()
	}
	return t
}
func (t *Named) String() string {
	if t.obj != nil {
		if t.obj.pkg != nil {
			return t.obj.pkg.path + "." + t.obj.name
		}
		return t.obj.name
	}
	return "<unnamed>"
}

// SetUnderlying sets the underlying type (used during type resolution).
func (t *Named) SetUnderlying(u Type) { t.underlying = u }

// safeUnderlying returns t.Underlying(), or nil if t is nil or its concrete
// value is a typed nil (interface with type descriptor but nil data pointer —
// common for unresolved Named types during B1 before all imports are loaded).
func safeUnderlying(t Type) (u Type) {
	if t == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			u = nil
		}
	}()
	return t.Underlying()
}
