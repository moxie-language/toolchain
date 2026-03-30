package reflect

import (
	"internal/reflectlite"
	"unsafe"
)

type Value struct {
	reflectlite.Value
}

func Indirect(v Value) Value                { return Value{reflectlite.Indirect(v.Value)} }
func ValueOf(i interface{}) Value           { return Value{reflectlite.ValueOf(i)} }
func (v Value) Type() Type                  { return toType(v.Value.Type()) }
func (v Value) Addr() Value                 { return Value{v.Value.Addr()} }
func (v Value) Slice(i, j int) Value        { return Value{v.Value.Slice(i, j)} }
func (v Value) Slice3(i, j, k int) Value    { return Value{v.Value.Slice3(i, j, k)} }
func (v Value) Elem() Value                 { return Value{v.Value.Elem()} }
func (v Value) Field(i int) Value           { return Value{v.Value.Field(i)} }
func (v Value) Index(i int) Value           { return Value{v.Value.Index(i)} }
func (v Value) Equal(u Value) bool          { return v.Value.Equal(u.Value) }
func (v Value) CanConvert(t Type) bool      { return v.Value.CanConvert(toRawType(t)) }
func (v Value) Convert(t Type) Value        { return Value{v.Value.Convert(toRawType(t))} }

func (v Value) MapKeys() []Value {
	keys := v.Value.MapKeys()
	return *(*[]Value)(unsafe.Pointer(&keys))
}

func (v Value) MapIndex(key Value) Value { return Value{v.Value.MapIndex(key.Value)} }
func (v Value) MapRange() *MapIter      { return (*MapIter)(v.Value.MapRange()) }

type MapIter reflectlite.MapIter

func (it *MapIter) Key() Value   { return Value{((*reflectlite.MapIter)(it)).Key()} }
func (it *MapIter) Value() Value { return Value{((*reflectlite.MapIter)(it)).Value()} }
func (it *MapIter) Next() bool   { return ((*reflectlite.MapIter)(it)).Next() }
func (it *MapIter) Reset(v Value) { (*reflectlite.MapIter)(it).Reset(v.Value) }

func (v Value) FieldByIndex(index []int) Value {
	return Value{v.Value.FieldByIndex(index)}
}

func (v Value) FieldByIndexErr(index []int) (Value, error) {
	out, err := v.Value.FieldByIndexErr(index)
	return Value{out}, err
}

func (v Value) FieldByName(name string) Value {
	return Value{v.Value.FieldByName(name)}
}

func (v Value) FieldByNameFunc(match func(string) bool) Value {
	return Value{v.Value.FieldByNameFunc(match)}
}

func Zero(typ Type) Value { return Value{reflectlite.Zero(toRawType(typ))} }

// Write-side wrappers bridging reflect.Value ↔ reflectlite.Value types.

func (v Value) Set(x Value)                 { v.Value.Set(x.Value) }
func (v Value) SetMapIndex(key, elem Value) { v.Value.SetMapIndex(key.Value, elem.Value) }
func (v Value) SetIterKey(iter *MapIter) {
	v.Value.SetIterKey((*reflectlite.MapIter)(iter))
}
func (v Value) SetIterValue(iter *MapIter) {
	v.Value.SetIterValue((*reflectlite.MapIter)(iter))
}

func New(typ Type) Value { return Value{reflectlite.New(toRawType(typ))} }
func MakeSlice(typ Type, len, cap int) Value {
	return Value{reflectlite.MakeSlice(toRawType(typ), len, cap)}
}
func MakeMap(typ Type) Value { return Value{reflectlite.MakeMap(toRawType(typ))} }
func MakeMapWithSize(typ Type, n int) Value {
	return Value{reflectlite.MakeMapWithSize(toRawType(typ), n)}
}

func Append(s Value, x ...Value) Value {
	xs := make([]reflectlite.Value, len(x))
	for i, v := range x {
		xs[i] = v.Value
	}
	return Value{reflectlite.Append(s.Value, xs...)}
}

func AppendSlice(s, t Value) Value {
	return Value{reflectlite.AppendSlice(s.Value, t.Value)}
}

func Copy(dst, src Value) int { return reflectlite.Copy(dst.Value, src.Value) }

type ValueError = reflectlite.ValueError
