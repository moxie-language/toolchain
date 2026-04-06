package jsbackend

import (
	"fmt"
	"go/types"
	"strings"
)

// TypeMapper handles Go type to JavaScript representation mapping.
type TypeMapper struct {
	// Track registered types for runtime type info emission.
	registeredTypes map[string]bool
	// Type ID counter for anonymous types.
	anonCounter int
}

// NewTypeMapper creates a new type mapper.
func NewTypeMapper() *TypeMapper {
	return &TypeMapper{
		registeredTypes: make(map[string]bool),
	}
}

// TypeID returns a unique string identifier for a Go type.
func (tm *TypeMapper) TypeID(t types.Type) string {
	switch t := t.(type) {
	case *types.Named:
		obj := t.Obj()
		if obj.Pkg() != nil {
			return obj.Pkg().Path() + "." + obj.Name()
		}
		return obj.Name()
	case *types.Basic:
		return t.Name()
	case *types.Pointer:
		return "*" + tm.TypeID(t.Elem())
	case *types.Slice:
		return "[]" + tm.TypeID(t.Elem())
	case *types.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), tm.TypeID(t.Elem()))
	case *types.Map:
		return "map[" + tm.TypeID(t.Key()) + "]" + tm.TypeID(t.Elem())
	case *types.Chan:
		prefix := "chan "
		switch t.Dir() {
		case types.SendOnly:
			prefix = "chan<- "
		case types.RecvOnly:
			prefix = "<-chan "
		}
		return prefix + tm.TypeID(t.Elem())
	case *types.Interface:
		if t.NumMethods() == 0 {
			return "interface{}"
		}
		tm.anonCounter++
		return fmt.Sprintf("$iface_%d", tm.anonCounter)
	case *types.Struct:
		tm.anonCounter++
		return fmt.Sprintf("$struct_%d", tm.anonCounter)
	case *types.Signature:
		return "func"
	case *types.Alias:
		return tm.TypeID(types.Unalias(t))
	default:
		return t.String()
	}
}

// ZeroExpr returns the JavaScript expression for the zero value of a Go type.
func (tm *TypeMapper) ZeroExpr(t types.Type) string {
	t = t.Underlying()
	switch t := t.(type) {
	case *types.Basic:
		info := t.Info()
		if info&types.IsBoolean != 0 {
			return "false"
		}
		if info&types.IsNumeric != 0 {
			if info&types.IsComplex != 0 {
				return "{ re: 0, im: 0 }"
			}
			if t.Kind() == types.Int64 || t.Kind() == types.Uint64 {
				return "0n"
			}
			return "0"
		}
		if info&types.IsString != 0 {
			return "''"
		}
		return "null"
	case *types.Pointer:
		return "null"
	case *types.Slice:
		return "null"
	case *types.Map:
		return "null"
	case *types.Chan:
		return "null"
	case *types.Interface:
		return "null"
	case *types.Signature:
		return "null"
	case *types.Struct:
		return tm.StructZero(t)
	case *types.Array:
		elem := tm.ZeroExpr(t.Elem())
		return fmt.Sprintf("$rt.builtin.makeSlice(%d, %d, %s)", t.Len(), t.Len(), elem)
	default:
		return "null"
	}
}

// StructZero returns the JS expression for a zero-value struct literal.
func (tm *TypeMapper) StructZero(t *types.Struct) string {
	if t.NumFields() == 0 {
		return "{}"
	}
	var fields []string
	for i := 0; i < t.NumFields(); i++ {
		f := t.Field(i)
		name := JsIdentifier(f.Name())
		zero := tm.ZeroExpr(f.Type())
		fields = append(fields, fmt.Sprintf("%s: %s", name, zero))
	}
	return "{ " + strings.Join(fields, ", ") + " }"
}

// StructClone returns JS code that deep-clones a struct value.
func (tm *TypeMapper) StructClone(varName string, t *types.Struct) string {
	if t.NumFields() == 0 {
		return "{}"
	}
	var fields []string
	for i := 0; i < t.NumFields(); i++ {
		f := t.Field(i)
		name := JsIdentifier(f.Name())
		val := fmt.Sprintf("%s.%s", varName, name)
		if isStructType(f.Type()) {
			val = tm.StructClone(val, f.Type().Underlying().(*types.Struct))
		}
		fields = append(fields, fmt.Sprintf("%s: %s", name, val))
	}
	return "{ " + strings.Join(fields, ", ") + " }"
}

// IsValueType returns true if the type should be passed by value in JS.
// Structs are value types in Go, so we need to clone them on assignment.
func IsValueType(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Struct:
		return true
	case *types.Array:
		return true
	}
	return false
}

// isStructType checks if the underlying type is a struct.
func isStructType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Struct)
	return ok
}

// NeedsPointerWrapper returns true if this type needs $get/$set pointer wrappers.
func NeedsPointerWrapper(t types.Type) bool {
	_, isPtr := t.Underlying().(*types.Pointer)
	return isPtr
}

// KeyKind returns the JS map key kind string for a Go type.
func (tm *TypeMapper) KeyKind(t types.Type) string {
	t = t.Underlying()
	switch t := t.(type) {
	case *types.Basic:
		info := t.Info()
		if info&types.IsString != 0 {
			return "string"
		}
		if info&types.IsInteger != 0 {
			return "int"
		}
		if info&types.IsFloat != 0 {
			return "float64"
		}
		if info&types.IsBoolean != 0 {
			return "bool"
		}
	}
	return "struct"
}

// EmitTypeRegistration generates JS code to register runtime type info.
func (tm *TypeMapper) EmitTypeRegistration(e *Emitter, t types.Type) {
	id := tm.TypeID(t)
	if tm.registeredTypes[id] {
		return
	}
	tm.registeredTypes[id] = true

	switch ut := t.Underlying().(type) {
	case *types.Struct:
		e.Line("$rt.types.registerType(%s, {", JsString(id))
		e.Indent()
		e.Line("id: %s,", JsString(id))
		e.Line("kind: 'struct',")
		e.Line("methods: new Map(),")
		e.Line("fields: [")
		e.Indent()
		for i := 0; i < ut.NumFields(); i++ {
			f := ut.Field(i)
			embedded := "false"
			if f.Embedded() {
				embedded = "true"
			}
			tag := ""
			if ut.Tag(i) != "" {
				tag = ut.Tag(i)
			}
			e.Line("{ name: %s, type: %s, tag: %s, embedded: %s },",
				JsString(f.Name()),
				JsString(tm.TypeID(f.Type())),
				JsString(tag),
				embedded)
		}
		e.Dedent()
		e.Line("],")
		e.Line("zero: () => (%s),", tm.StructZero(ut))
		e.Dedent()
		e.Line("});")

	case *types.Interface:
		e.Line("$rt.types.registerType(%s, {", JsString(id))
		e.Indent()
		e.Line("id: %s,", JsString(id))
		e.Line("kind: 'interface',")
		e.Line("methods: new Map([")
		e.Indent()
		for i := 0; i < ut.NumMethods(); i++ {
			m := ut.Method(i)
			e.Line("[%s, null],", JsString(m.Name()))
		}
		e.Dedent()
		e.Line("]),")
		e.Dedent()
		e.Line("});")
	}
}
