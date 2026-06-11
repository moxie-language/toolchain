# Moxie Type System

## Numeric Types

| Type | Size | Notes |
|------|------|-------|
| `int8` | 1 byte | signed |
| `int16` | 2 bytes | signed |
| `int32` | 4 bytes | signed. `rune` is alias. |
| `int64` | 8 bytes | signed |
| `uint8` | 1 byte | unsigned. `byte` is alias. |
| `uint16` | 2 bytes | unsigned |
| `uint32` | 4 bytes | unsigned |
| `uint64` | 8 bytes | unsigned |
| `float32` | 4 bytes | IEEE 754 |
| `float64` | 8 bytes | IEEE 754 |
| `bool` | 1 byte | `true`/`false` |

**`int` and `uint` do not exist.** They are compile errors. Always use explicit widths.

`uintptr` available only with `import "unsafe"`. Alias for `uint64`.

No complex types (`complex64`, `complex128` do not exist).

## string = []byte

`string` and `[]byte` are the **same type**. Identical 3-word runtime layout (pointer, length, capacity). Mutually assignable. No conversion needed.

```moxie
var s string = []byte{72, 101}   // direct assignment
b := []byte("hello")             // no-op
s = b                            // no copy
```

`string` is retained as a **representation pragma** - on the WASM target, the compiler maps `string` to native JS string and `[]byte` to byte-array object. Use `string` for text, `[]byte` for binary data.

`import "strings"` is a compile error. Use `import "bytes"` - they operate on the same type.

`range` over a string yields **bytes** (`uint8`), not runes. Use `unicode/utf8.DecodeRune` for codepoint iteration.

## Builtin Stringers

All builtin types implement `String() string`:

| Type | Example | Output |
|------|---------|--------|
| `bool` | `true.String()` | `"true"` |
| `int8`..`int64` | `int32(42).String()` | `"42"` |
| `uint8`..`uint64` | `uint64(255).String()` | `"255"` |
| `float32/64` | `float64(3.14).String()` | `"+3.14000000000000e+000"` |
| `byte` | `byte(0xff).String()` | `"ff"` (hex pair) |
| `rune` | `'a'.String()` | `"0x0061"` (hex codepoint) |
| `string` | `"hi".String()` | `"hi"` (identity) |

`fmt` variadics use `...Stringer`, not `...any`. No reflection, no empty interfaces.

```moxie
n := int32(42)
msg := "count: " | n.String() | " items"
```

## Slice Equality

Any `[]T` where `T` is comparable supports `==` and `!=`. Slices can be used as map keys.

```moxie
a := []int32{1, 2, 3}
b := []int32{1, 2, 3}
println(a == b)   // true
```

## Named Type Restrictions

Named types may only be declared over value types and interfaces:

```moxie
type Duration int64              // ok
type Point struct{ X, Y float64 }  // ok

type Events []Event              // compile error
type Index map[string]int32      // compile error
```

Named slice/map types are banned. Wrap in a struct to add methods:

```moxie
type Events struct {
    items []*Event
}

func (e *Events) Add(ev *Event) { e.items = append(e.items, ev) }
func (e *Events) Len() (n int32) { return len(e.items) }
```

Methods belong on the struct that holds the slice, not on the slice itself.

## Interfaces

Interfaces define method sets. Structural satisfaction (no `implements` keyword).

```moxie
type Reader interface {
    Read(p []byte) (n int32, err error)
}
```

**`interface{}` and `any` are compile errors.** Every interface must declare at least one method.

Compile-time satisfaction check:
```moxie
var _ Reader = (*MyReader)(nil)   // zero-cost, fails at compile time if unmet
```

## Pointers

```moxie
p := &Point{X: 1, Y: 2}   // *Point
var q *int32
```

`new(T)` does not exist. Use `&T{}` or `var x T; p := &x`.

## Maps

```moxie
m := map[string]int32{}
m["key"] = 42
v, ok := m["key"]
delete(m, "key")
```

Map iteration is **deterministic** - keys in ascending lexicographic order.

## Channels

```moxie
ch := chan int32{}      // unbuffered
ch := chan int32{10}    // buffered, capacity 10
```

Directional: `chan<- T` (send-only), `<-chan T` (receive-only).
