# Moxie Syntax Quick Reference

## File Extension

`.mx` - the compiler does not read `.go` files.

## Keywords

```
break    case     chan     const    continue
default  defer    else    for      func
goto     if       import  interface  map
package  range    return  select   struct
switch   type     var
```

Not keywords (removed): `go`, `fallthrough`.

## Predeclared Identifiers

Types:
```
bool    byte    error   float32   float64
int8    int16   int32   int64     rune
string  uint8   uint16  uint32    uint64
comparable
```

Constants: `true`, `false`, `nil`, `iota`

Functions:
```
append  cap     clear   close   copy    delete
free    len     max     min     panic   print
println recover spawn
```

Shadowing any predeclared identifier is a compile error.

## Operators

`|` is dual-purpose:
- Integer operands: bitwise OR
- String/slice operands: **concatenation**

`+` is numeric only. Not defined on strings/slices.

`...` is variadic parameter declaration only. Not spread. `f(slice...)` is a compile error.

`~` is alias for `^` (bitwise complement).

## Removed Constructs

| Construct | Use Instead |
|-----------|-------------|
| `go f()` | `spawn` or channels + `select` |
| `make(T, ...)` | `[]T{:n}`, `chan T{}`, `map[K]V{}` |
| `new(T)` | `&T{}` |
| `"a" + "b"` | `"a" \| "b"` |
| `fallthrough` | `case A, B:` |
| `append(a, b...)` | `a \| b` |
| `interface{}` / `any` | named interface with methods |
| `int` / `uint` | explicit widths: `int32`, `int64`, etc. |
| `complex64/128` | not supported |
| `import "strings"` | `import "bytes"` |
| `import "sync"` | removed; single-threaded domains |

## Literal Syntax

```moxie
[]T{:n}             // slice length n
[]T{:n:c}           // slice length n, capacity c
chan T{}             // unbuffered channel
chan T{n}            // buffered channel capacity n
map[K]V{}           // empty map
&T{}                // pointer to zero T
```

For named slice types: `(make)(S, 0, c)` - parenthesized form bypasses restriction check.

## Declarations

Package scope allows only:
- `const` declarations
- `type` declarations
- `func` declarations (including methods)
- `var X T` (zero-value only, no initializer)

`var X = expr` at package scope is a compile error. Assign in `init()`.

## Functions

All return values must be named:
```moxie
func add(x, y int32) (sum int32) { return x + y }
```

All methods must use pointer receivers:
```moxie
func (p *Point) Move(dx float64) { p.X += dx }
```

## No Scope Shadowing

Redeclaring a name visible in any enclosing scope is a compile error:
```moxie
x := 1
if true {
    x := 2   // compile error
    x = 2    // ok: assignment, not declaration
}
```

## Range

| Over | Key | Value |
|------|-----|-------|
| array/slice | `int32` (index) | element |
| string | `int32` (byte index) | `uint8` (byte) |
| map | key type | value type |
| channel | element type | - |
| integer | `int32` | - |

String range yields bytes, not runes.

## init()

- Not allowed in `main` packages. Use `main()`.
- At most one per library package.
- No `spawn()` or `select` inside `init()`.
- Primary purpose: assign non-zero values to package-scope vars.

## Rune Literals

`rune` is `int32`. `'a'.String()` returns `"0x0061"` (hex), not UTF-8.
Appending rune to string with `|` is a compile error. Use `unicode/utf8.AppendRune`.

## Integer Literals

```
42          // decimal
0600        // octal
0o600       // octal (explicit)
0xBadFace   // hex
0b101010    // binary
1_000_000   // underscores ok
```
