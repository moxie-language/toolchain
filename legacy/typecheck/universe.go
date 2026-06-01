package typecheck

// Universe is the outermost scope containing all predeclared identifiers.
// All package scopes have Universe as their ultimate ancestor.
var Universe *Scope

// Predeclared types. Indices match BasicKind values.
var Typ [UntypedNil + 1]*Basic

// Predeclared named types (accessible as objects in Universe).
var (
	universeBool          *TypeName
	universeError         *TypeName
	universeByteAlias     *TypeName // byte = uint8
	universeRuneAlias     *TypeName // rune = int32
)

func init() {
	Universe = NewScope(nil)

	// Initialize Basic types in Typ table.
	infos := [...]struct {
		kind BasicKind
		info BasicInfo
		name string
	}{
		{Invalid, 0, "invalid type"},

		{Bool, IsBoolean, "bool"},

		{Int8, IsInteger | IsOrdered | IsNumeric, "int8"},
		{Int16, IsInteger | IsOrdered | IsNumeric, "int16"},
		{Int32, IsInteger | IsOrdered | IsNumeric, "int32"},
		{Int64, IsInteger | IsOrdered | IsNumeric, "int64"},
		{Uint8, IsInteger | IsUnsigned | IsOrdered | IsNumeric, "uint8"},
		{Uint16, IsInteger | IsUnsigned | IsOrdered | IsNumeric, "uint16"},
		{Uint32, IsInteger | IsUnsigned | IsOrdered | IsNumeric, "uint32"},
		{Uint64, IsInteger | IsUnsigned | IsOrdered | IsNumeric, "uint64"},

		{Float32, IsFloat | IsOrdered | IsNumeric, "float32"},
		{Float64, IsFloat | IsOrdered | IsNumeric, "float64"},

		// string and []byte share kind String in Moxie.
		{String, IsString | IsOrdered, "string"},

		{UnsafePointer, 0, "Pointer"}, // lives in unsafe package

		{UntypedBool, IsBoolean | IsUntyped, "untyped bool"},
		{UntypedInt, IsInteger | IsNumeric | IsUntyped, "untyped int"},
		{UntypedRune, IsInteger | IsNumeric | IsUntyped, "untyped rune"},
		{UntypedFloat, IsFloat | IsNumeric | IsUntyped, "untyped float"},
		{UntypedString, IsString | IsUntyped, "untyped string"},
		{UntypedNil, IsUntyped, "untyped nil"},
	}
	for i, info := range infos {
		Typ[i] = &Basic{kind: info.kind, info: info.info, name: info.name}
	}

	// In Moxie, int ≡ int32 and uint ≡ uint32. We expose "int" and "uint"
	// as named aliases to int32/uint32 in the universe scope.
	defTypeName("bool", Typ[Bool])
	defTypeName("int8", Typ[Int8])
	defTypeName("int16", Typ[Int16])
	defTypeName("int32", Typ[Int32])
	defTypeName("int64", Typ[Int64])
	defTypeName("uint8", Typ[Uint8])
	defTypeName("uint16", Typ[Uint16])
	defTypeName("uint32", Typ[Uint32])
	defTypeName("uint64", Typ[Uint64])
	defTypeName("float32", Typ[Float32])
	defTypeName("float64", Typ[Float64])
	defTypeName("string", Typ[String])

	// int = int32, uint = uint32 (Moxie always 32-bit)
	intType := NewNamed(NewTypeName(nil, "int", nil), Typ[Int32])
	uintType := NewNamed(NewTypeName(nil, "uint", nil), Typ[Uint32])
	defObj(intType.obj)
	defObj(uintType.obj)
	intType.obj.typ = intType
	uintType.obj.typ = uintType

	// byte = uint8
	byteType := NewNamed(NewTypeName(nil, "byte", nil), Typ[Uint8])
	defObj(byteType.obj)
	byteType.obj.typ = byteType
	universeByteAlias = byteType.obj

	// rune = int32
	runeType := NewNamed(NewTypeName(nil, "rune", nil), Typ[Int32])
	defObj(runeType.obj)
	runeType.obj.typ = runeType
	universeRuneAlias = runeType.obj

	// uintptr (allowed only with import "unsafe")
	uintptrType := NewNamed(NewTypeName(nil, "uintptr", nil), Typ[Uint64])
	defObj(uintptrType.obj)
	uintptrType.obj.typ = uintptrType

	// error interface
	errMeth := &IfaceMethod{name: "Error", sig: NewSignature(nil, nil, NewTuple(NewVar(nil, "", Typ[String])), false)}
	errIface := NewInterface([]*IfaceMethod{errMeth}, nil)
	errIface.Complete()
	universeError = NewTypeName(nil, "error", errIface)
	defObj(universeError)

	// complex64/128 are not valid in Moxie programs but appear in stdlib source.
	// Register them as invalid basic types so the checker doesn't report "undefined".
	complex64Type := NewNamed(NewTypeName(nil, "complex64", nil), Typ[Invalid])
	defObj(complex64Type.obj)
	complex128Type := NewNamed(NewTypeName(nil, "complex128", nil), Typ[Invalid])
	defObj(complex128Type.obj)

	// any is an alias for interface{} (Go 1.18+)
	anyIface := NewInterface(nil, nil)
	anyIface.Complete()
	anyObj := NewTypeName(nil, "any", anyIface)
	defObj(anyObj)

	// comparable is a builtin interface satisfied by comparable types.
	comparableIface := NewInterface(nil, nil)
	comparableIface.Complete()
	comparableObj := NewTypeName(nil, "comparable", comparableIface)
	defObj(comparableObj)

	// predeclared constants
	defConst("true", Typ[UntypedBool], untypedBool(true))
	defConst("false", Typ[UntypedBool], untypedBool(false))
	defConst("iota", Typ[UntypedInt], untypedInt(0))
	defNil()

	// predeclared builtins
	for id, name := range builtinNames {
		Universe.Insert(newBuiltin(name, BuiltinID(id)))
	}

	// Go 1.21 builtins.
	Universe.Insert(newBuiltin("min", BuiltinMin))
	Universe.Insert(newBuiltin("max", BuiltinMax))
}

var builtinNames = [...]string{
	BuiltinAppend:  "append",
	BuiltinCap:     "cap",
	BuiltinClear:   "clear",
	BuiltinClose:   "close",
	BuiltinComplex: "complex",
	BuiltinCopy:    "copy",
	BuiltinDelete:  "delete",
	BuiltinImag:    "imag",
	BuiltinLen:     "len",
	BuiltinMake:    "make",
	BuiltinNew:     "new",
	BuiltinPanic:   "panic",
	BuiltinPrint:   "print",
	BuiltinPrintln: "println",
	BuiltinReal:    "real",
	BuiltinRecover: "recover",
	BuiltinSpawn:   "spawn",
	BuiltinFree:    "free",
}

func defTypeName(name string, typ Type) {
	tn := NewTypeName(nil, name, typ)
	Universe.Insert(tn)
}

func defObj(obj Object) {
	Universe.Insert(obj)
}

func defConst(name string, typ Type, val ConstVal) {
	Universe.Insert(NewConst(nil, name, typ, val))
}

func defNil() {
	Universe.Insert(NewConst(nil, "nil", Typ[UntypedNil], untypedNil()))
}

// Placeholder constant values. These wrap go/constant values. In B4 they
// become native Moxie values.
type constBool struct{ v bool }
type constInt struct{ v int64 }
type constNil struct{}

func untypedBool(v bool) ConstVal  { return constBool{v} }
func untypedInt(v int64) ConstVal  { return constInt{v} }
func untypedNil() ConstVal         { return constNil{} }

func (c constBool) String() string { if c.v { return "true" }; return "false" }
func (c constInt) String() string  { return "0" }
func (c constNil) String() string  { return "nil" }

// Predeclared returns the universe-scope object with the given name, or nil.
func Predeclared(name string) Object {
	return Universe.Lookup(name)
}
