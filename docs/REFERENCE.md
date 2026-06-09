# Moxie Language Reference

Moxie is a compiled systems language for domain-isolated event-driven programs.
Programs compile to static native binaries or WebAssembly. Source files use the
`.mx` extension and are UTF-8 encoded.

This document specifies the Moxie language. Familiarity with C-family syntax is
assumed.

For canonical formatting rules and the `moxiefmt` tool, see
[FORMAT.md](FORMAT.md).

---

## Notation

The grammar uses EBNF. `|` separates alternatives. `[...]` denotes optional
elements. `{...}` denotes zero or more repetitions. `(...)` groups.

---

## Source Files

Each source file begins with a `package` declaration followed by zero or more
`import` declarations, followed by top-level declarations.

```
SourceFile = PackageClause ";" { ImportDecl ";" } { TopLevelDecl ";" } .
```

File extension is `.mx`.

### Platform-Specific Files

Files are included or excluded by name suffix and build constraints:

- `_linux.mx` - included only when targeting Linux
- `_darwin.mx` - included only when targeting Darwin
- `_wasm.mx` - included only when targeting js/wasm
- `_amd64.mx` - included only when targeting amd64
- `_arm64.mx` - included only when targeting arm64
- `_unix.mx` - included on all Unix targets
- `_test.mx` - excluded from normal builds, included by `moxie test`

Build constraints use `//go:build` syntax on the first line of the file (this
directive syntax is inherited and will be changed in a future release):

```moxie
//go:build linux && amd64
```

The following build tags are always set by the compiler:

| Tag | Meaning |
|-----|---------|
| `moxie` | Moxie compiler (always set) |
| `moxie.unicore` | Single-core cooperative execution |
| `scheduler.none` | No preemptive scheduler |

Standard platform tags (`linux`, `darwin`, `amd64`, `arm64`, `js`, `wasm`)
are set according to the target.

---

## Modules

A module is a directory tree of `.mx` source files rooted at a `moxie.mod`
file. The module file declares the module path:

```
module example.com/myproject
```

Each line is a directive. The only directive currently recognized is `module`.

---

## Lexical Elements

### Comments

```
// line comment
/* block comment */
```

### Keywords

```
break    case     chan     const    continue
default  defer    else    for      func
goto     if       import  interface  map
package  range    return  select   struct
switch   type     var
```

### Identifiers

Letter or underscore followed by letters, digits, or underscores. Unicode
letters are allowed.

An identifier is exported if its first character is a UTF-8 uppercase letter
(Lu category). This includes ASCII A-Z and uppercase letters from any script
that has case distinction.

For scripts without case distinction (CJK, Arabic, Hebrew, Thai, etc.), the
`#` prefix marks an identifier as exported:

```moxie
func #计算(x, y int32) (sum int32) {    // exported
    return x + y
}

func 内部处理() {                   // unexported
}
```

The `#` is part of the export mechanism, not part of the identifier name. It
does not appear in references to the symbol after import - callers use the
bare name:

```moxie
import "example/math"
result := math.计算(1, 2)
```

Unexported identifiers are visible only within the declaring package.

#### Predeclared Identifiers

The following identifiers are predeclared and may not be used as variable,
function, type, or constant names at any scope. Shadowing a predeclared
identifier is a compile error.

Types:

```
bool    byte    error   float32   float64
int8    int16   int32   int64     rune
string  uint8   uint16  uint32    uint64
comparable
```

Constants:

```
true    false   nil     iota
```

Functions:

```
append  cap     clear   close   copy    delete
free    len     max     min     panic   print
println recover spawn
```

#### No Scope Shadowing

Redeclaring an identifier that is already in scope is a compile error. This
applies at every level - a variable declared in a function body cannot be
redeclared in an `if`, `for`, `switch`, or any nested block:

```moxie
func f() {
    x := 1
    if true {
        x := 2         // compile error: x already declared in enclosing scope
    }
    for i := range s {
        i := i + 1     // compile error: i already declared in this scope
    }
}
```

To update a variable in a nested scope, assign to it:

```moxie
func f() {
    x := 1
    if true {
        x = 2          // legal: assignment, not redeclaration
    }
}
```

This rule eliminates an entire class of bugs where an inner declaration
silently hides an outer one, and the programmer reads the outer name while the
compiler reads the inner one.

### Operators and Punctuation

```
+    &     +=    &=     &&    ==    !=    (    )
-    |     -=    |=     ||    <     <=    [    ]
*    ^     *=    ^=           >     >=    {    }
/    <<    /=    <<=    <-    :=    ...   ;    :
%    >>    %=    >>=    &^    &^=         .    ,
~    !
```

The `|` operator has dual meaning depending on operand types:

| Context | Meaning |
|---------|---------|
| Integer operands | Bitwise OR (standard) |
| Slice or string operands | Concatenation - returns a slice containing elements from both operands |

The `+` operator is not defined on string or slice types. Use `|` for all
text and slice concatenation:

```moxie
msg := "hello " | name | "!"
s |= " suffix"
combined := sliceA | sliceB
```

The `...` token is **only valid in variadic parameter declarations**
(`func f(args ...T)`). It is not a spread/unpack operator. `append(a, b...)`
and `f(slice...)` are compile errors. For slice-to-slice concatenation, use
`a | b`.

### Integer Literals

```
42        // decimal
0600      // octal (leading zero)
0o600     // octal (explicit prefix)
0xBadFace // hexadecimal
0b101010  // binary
1_000_000 // underscores for readability
```

### Floating-Point Literals

```
3.14
.5
1e10
1.5e-3
0x1p10    // hexadecimal float
```

### Rune Literals

```
'a'
'\n'
'\x41'
'A'
'\U00000041'
```

Rune literals have type `rune` (alias for `int32`). A rune is strictly a
Unicode code point value, not a byte or text fragment. `rune.String()` returns
a hexadecimal representation (e.g. `'a'.String()` returns `"0x0061"`), not a
UTF-8 encoding. To produce the UTF-8 byte sequence for a rune, use
`unicode/utf8.AppendRune` or `unicode/utf8.EncodeRune`.

Appending a rune directly to a string or `[]byte` with `|` is a compile error.
The programmer must explicitly convert the rune to its UTF-8 encoding first.

### String Literals

```
"hello\nworld"    // interpreted string literal
`hello\nworld`    // raw string literal (backslash is literal)
```

String literals produce values of type `string`. String constants are stored as
UTF-8 encoded byte sequences. The literal `"hello"` is five bytes of UTF-8; the
literal `"é"` is two bytes (`0xc3 0xa9`). To tokenize a string into runes
or process characters beyond raw bytes, iterate with `unicode/utf8`.

The `string` keyword is a representation pragma - it is the same type as
`[]byte` at the language level, but on the WASM target the compiler uses the
source spelling to choose between a native JS string (`string`) and a
byte-array object (`[]byte`). See [WASM String Pragma](#wasm-string-pragma).

### Slice Size Literals

```moxie
buf := []byte{:1024}          // slice of length 1024
table := []int32{:0:100}      // slice of length 0, capacity 100
```

The leading `:` after `{` distinguishes size literals from composite literals.
This is the only way to allocate a slice by size.

### Channel Literals

```moxie
ch := chan int32{}             // unbuffered channel
ch := chan int32{10}           // buffered channel, capacity 10
```

This is the only way to create channels.

### Composite Literals

```moxie
p := Point{X: 1, Y: 2}
s := []int32{1, 2, 3}
m := map[string]int32{"a": 1, "b": 2}
```

---

## Types

### Boolean

```
bool
```

Values: `true`, `false`.

### Numeric Types

| Type | Size | Description |
|------|------|-------------|
| `int8` | 1 byte | Signed 8-bit integer |
| `int16` | 2 bytes | Signed 16-bit integer |
| `int32` | 4 bytes | Signed 32-bit integer |
| `int64` | 8 bytes | Signed 64-bit integer |
| `uint8` | 1 byte | Unsigned 8-bit integer |
| `uint16` | 2 bytes | Unsigned 16-bit integer |
| `uint32` | 4 bytes | Unsigned 32-bit integer |
| `uint64` | 8 bytes | Unsigned 64-bit integer |
| `float32` | 4 bytes | IEEE 754 32-bit float |
| `float64` | 8 bytes | IEEE 754 64-bit float |
| `byte` | 1 byte | Alias for `uint8` |
| `rune` | 4 bytes | Alias for `int32` |

`int` and `uint` are **illegal types** - they do not exist in Moxie. Always use
explicit widths (`int32`, `int64`, `uint32`, `uint64`). Slice/array indexes and
`len()`/`cap()` return `int32`.

`uintptr` is available only in packages that `import "unsafe"`. It is an alias
for `uint64`.

Complex number types do not exist.

#### Builtin Type Stringers

All builtin numeric and boolean types implement `String() string`, satisfying
`fmt.Stringer`. This is how `fmt` works without reflection or empty interfaces -
`fmt` variadics use `...Stringer` not `...any`/`...interface{}`, and each
argument's `.String()` method is called directly:

```moxie
n := int32(42)
msg := "count: " | n.String() | " items"
```

Output formats:

| Type | Example | Output |
|------|---------|--------|
| `bool` | `true.String()` | `"true"` |
| `int8`..`int64` | `int32(42).String()` | `"42"` |
| `uint8`..`uint64` | `uint64(255).String()` | `"255"` |
| `float32`, `float64` | `float64(3.14).String()` | `"+3.14000000000000e+000"` |
| `byte` | `byte(0xff).String()` | `"ff"` (hex pair) |
| `rune` | `'a'.String()` | `"0x0061"` (hex code point) |
| `string` | `"hi".String()` | `"hi"` (identity) |

Untyped constants resolve to their maximal typed equivalent for `.String()`:
`UntypedBool` to `bool`, `UntypedInt` to `int64`, `UntypedFloat` to `float64`,
`UntypedRune` to `rune`, `UntypedString` to `string`.

### Text Type

`string` and `[]byte` are the same type. They have identical runtime layout
(pointer, length, capacity), are mutually assignable, and are interchangeable
everywhere. There is no immutable string type - all text is a mutable byte
slice.

```moxie
name := "hello"              // type is string (= []byte)
var s string = []byte{72}    // direct assignment, no conversion
b := []byte("world")         // no-op, same layout
s = b                        // no copy
```

The keyword `string` is retained as a representation pragma. Use `string` in
signatures and struct fields for readability and correct WASM codegen:

```moxie
func greet(name string) (msg string) {
    return "hello " | name
}
```

`range` over a string yields individual bytes, not runes. To iterate over
UTF-8 code points, use `unicode/utf8.DecodeRune` or a rune iterator.

### Array Types

Fixed-size, value-type semantics.

```moxie
var a [4]int32
b := [3]string{"x", "y", "z"}
```

### Slice Types

Dynamic-length view over a backing array.

```moxie
var s []int32
buf := []byte{:1024}          // allocate with size literal
table := []int32{:0:100}      // length 0, capacity 100
items := []string{"a", "b"}   // composite literal
```

#### Slice Equality

Any slice with a comparable element type supports `==` and `!=`. Two slices
are equal if they have the same length and identical elements:

```moxie
a := []int32{1, 2, 3}
b := []int32{1, 2, 3}
println(a == b)               // true
```

Slices of comparable types can be used as map keys:

```moxie
m := map[string]int32{}
m["key"] = 42
```

#### Slice Concatenation

The `|` operator concatenates two slices of the same type:

```moxie
a := []int32{1, 2, 3}
b := []int32{4, 5}
c := a | b                    // []int32{1, 2, 3, 4, 5}
s := "hello" | " world"
s |= "!"
```

If the left operand has sufficient capacity, `|` copies the right operand into
the existing backing array. Otherwise it allocates a new backing array. The
right operand is never modified.

### Struct Types

```moxie
type Point struct {
    X, Y int32
}
```

Multiple fields of the same type can share a declaration line with
comma-separated names:

```moxie
type Rect struct {
    X, Y, W, H float64
    Label       string
}
```

Embedding, tags, and anonymous fields are supported:

```moxie
type Named struct {
    Point              // embedded
    Label string `json:"label"`
}
```

### Pointer Types

```moxie
p := &Point{X: 1, Y: 2}     // *Point
var q *int32
```

The `&` operator takes the address of any addressable value. Use `&T{}` or
declare a variable and take its address:

```moxie
p := &MyStruct{Field: value}
```

### Function Types

First-class values. Closures, variadic parameters, and multiple return values
are supported.

```moxie
type Handler func(req Request) Response

fn := func(x, y int32) (sum int32) { return x + y }
```

### Interface Types

Interfaces define method sets. Type assertions, type switches, and dynamic
dispatch work as expected.

```moxie
type Reader interface {
    Read(p []byte) (n int, err error)
}
```

The `error` interface is predeclared:

```moxie
type error interface {
    Error() (msg string)
}
```

`comparable` is a predeclared interface constraint satisfied by all comparable
types.

#### Empty Interfaces Are Banned

`interface{}` and `any` are compile errors. Every interface must declare at
least one method:

```moxie
var x any              // compile error
var y interface{}      // compile error

type Box struct {
    Value interface{}  // compile error
}
```

If a function needs to accept multiple concrete types, use a named interface
with a method, or use a type switch over the known set of types with a
populated interface:

```moxie
type Stringer interface {
    String() (s string)
}

func display(v Stringer) {
    println(v.String())
}
```

Empty interfaces erase type information. Moxie requires that the type is
always known - either statically at the call site or via an interface that
constrains the value to types with specific behavior. "Accepts anything" is
not a type contract.

Interface satisfaction is structural - a type satisfies an interface if it has
all the required methods. There is no explicit `implements` declaration. To
verify at compile time that a type satisfies an interface, use a typed nil
assertion at package scope:

```moxie
var _ Reader = (*MyReader)(nil)
```

This is zero-cost (optimized away) and fails at compile time if `*MyReader`
does not implement `Reader`.

### Map Types

Hash maps with comparable key types and arbitrary value types.

```moxie
m := map[string]int32{}
m["key"] = 42
v, ok := m["key"]
delete(m, "key")
```

Map iteration is deterministic. `range` over a map visits keys in ascending
lexicographic order of their encoded representation. This means identical maps
produce identical iteration sequences across runs and across machines.

### Channel Types

```
chan T          // bidirectional
chan<- T        // send-only
<-chan T        // receive-only
```

Channels are the primary synchronization mechanism within a domain.

---

## Declarations and Scope

### Package Scope Rules

Package-level declarations follow strict rules:

**Allowed at package scope:**
- `const` declarations (with or without values, including `iota`)
- `type` declarations
- `func` declarations (including methods)
- `var X T` - zero-value variable declarations (no initializer expression)

**Not allowed at package scope:**
- `var X = expr` - variable declarations with initializer expressions
- `var X T = expr` - variable declarations with both type and initializer

Zero-value `var` declarations are static storage placed by the linker in the
`.bss` segment. No runtime code executes. Any package-scope variable that
requires a non-zero initial value must be explicitly assigned in the package's
`init()` function.

### Constant Declarations

```moxie
const Pi = 3.14159
const (
    A = iota    // 0
    B           // 1
    C           // 2
)
const Size int32 = 256
```

`iota` is a monotonically incrementing counter within a `const` block. It
starts at 0 and advances by 1 for each constant specification. The expression
applied to `iota` is inherited by subsequent lines, so shifting, masking, and
arithmetic expressions all work:

```moxie
const (
    FlagRead  = 1 << iota  // 1
    FlagWrite              // 2
    FlagExec               // 4
)

const (
    _        = iota        // skip 0
    KB int64 = 1 << (10 * iota)  // 1024
    MB                           // 1048576
    GB                           // 1073741824
)
```

Untyped constants (no explicit type) have unlimited precision and are narrowed
to the required type at use sites.

### Type Declarations

```moxie
type Duration int64
type Point struct { X, Y float64 }
type Reader interface { Read(p []byte) (n int, err error) }
```

Named types may only be declared over value types (numeric, bool, string,
array, struct) and interface types. Named slice types and named map types are
compile errors:

```moxie
type Events []Event          // compile error
type Index map[string]int32  // compile error
```

Slices and maps are reference types - a named wrapper over a reference type
creates a receiver that cannot mutate its own elements through closures, forces
every method to take a pointer receiver for append semantics, and produces
ambiguous ownership at every call site. Methods belong on the struct that holds
the slice, not on the slice itself.

### Variable Declarations

At package scope, only zero-value declarations:

```moxie
var counter int32
var buf []byte
var ready bool
```

Inside functions, full declaration syntax is available:

```moxie
var x int32 = 42
y := compute()
```

Variables are allocated at the point of declaration in execution order. There
is no hoisting. See [Memory Management](#memory-management) for lifetime
rules.

No declaration (`var` or `:=`) may reuse a name that is already visible in
the current or any enclosing scope. See [No Scope Shadowing](#no-scope-shadowing).

### Short Variable Declarations

```moxie
x := 42
name, err := readName()
```

Available only inside functions. The same no-shadowing rule applies - every
name on the left side of `:=` must be new to the current scope and not
declared in any enclosing scope.

### Function Declarations

All return values must be named. Unnamed return values are a compile error.
The names document what the function produces and are visible at the call site
in documentation and tooling - without them the meaning of each return
position is hidden inside the function body:

```moxie
func add(x, y int32) (sum int32) {
    sum = x + y
    return
}

func swap(a, b int32) (first, second int32) {
    return b, a
}

func greet(names ...string) {
    for _, n := range names {
        println(n)
    }
}

func read(p []byte) (int, error) {    // compile error: unnamed return values
    // ...
}
```

Named return values are implicit variable declarations at the top of the
function scope, initialized to zero. They are subject to the no-shadowing
rule - no parameter, local variable, or nested declaration may reuse a
return value name. A bare `return` statement returns their current values.
An explicit `return x, y` assigns to the named returns and then returns.

### Method Declarations

All methods must use pointer receivers. Value receivers are a compile error.

```moxie
func (p *Point) Translate(dx, dy float64) {
    p.X += dx
    p.Y += dy
}

func (p Point) String() string {   // compile error: value receiver
    return "..."
}
```

A type with methods is state. The receiver is a reference to that state, not a
copy of it. Value receivers create a hidden copy that silently discards
mutations, splitting the caller's mental model between "this method changes
the object" and "this method pretends to". Pointer receivers make the
reference explicit and uniform - every method call is a mutation site or it
isn't a method.

---

## Packages

### Package Declaration

Every source file starts with a package clause:

```moxie
package mypackage
```

A `package main` declaration defines an executable program. The entry point is
`func main()`.

### Import Declarations

```moxie
import "fmt"
import (
    "bytes"
    "io"
)
import alias "some/package"
```

`import "strings"` is a compile error. Use `import "bytes"` - since `string`
and `[]byte` are the same type, the `bytes` package handles all text
operations.

### The `moxie` Package

The `moxie` package is force-imported by the compiler (like `runtime`). It
provides the `Codec` interface and built-in codec wrapper types for spawn
boundary serialization. User code imports `moxie` explicitly when using Codec
types.

### init Functions

Library packages (non-main) may declare at most one `init` function:

```moxie
func init() {
    // package initialization - assign non-zero values to package vars
}
```

Rules:

- **Main packages**: `init()` is not allowed. Use `main()` for program entry.
- **Library packages**: at most one `init()` per package.
- **Body restrictions**: `init()` must not contain `spawn()` calls or
  `select` statements. It runs during the sequential package initialization
  phase before the program's event loop starts. No event dispatch or
  inter-domain communication is possible during init.
- `init()` takes no arguments and returns no values.
- `init()` is called automatically by the runtime during program startup, in
  dependency order.

The primary purpose of `init()` is to assign non-zero initial values to
package-scope variables, since `var X = expr` at package scope is not allowed:

```moxie
var defaultTimeout int64

func init() {
    defaultTimeout = 5000
}
```

---

## Expressions

### Operands

```
Operand = Literal | OperandName | "(" Expression ")" .
```

### Primary Expressions

```moxie
x
x.f            // field or method
a[i]           // index
s[lo:hi]       // slice
s[lo:hi:max]   // full slice
f(args)        // function call
T(x)           // type conversion
x.(T)          // type assertion
```

### Type Assertions

```moxie
v := x.(int32)          // panics if x is not int32
v, ok := x.(int32)      // ok is false if x is not int32
```

### Type Switches

```moxie
switch v := x.(type) {
case int32:
    println(v)
case string:
    println(v)
default:
    println("unknown")
}
```

### Operators

Unary operators:

| Op | Meaning |
|----|---------|
| `+` | Numeric identity |
| `-` | Numeric negation |
| `!` | Logical NOT |
| `^` | Bitwise complement |
| `~` | Bitwise complement (alias for `^`) |
| `*` | Pointer dereference |
| `&` | Address-of |
| `<-` | Channel receive |

Binary operators (by precedence, highest first):

| Precedence | Operators |
|------------|-----------|
| 5 | `*` `/` `%` `<<` `>>` `&` `&^` |
| 4 | `+` `-` `\|` `^` |
| 3 | `==` `!=` `<` `<=` `>` `>=` |
| 2 | `&&` |
| 1 | `\|\|` |

The `|` operator at precedence 4 performs bitwise OR on integers and
concatenation on slices and strings.

The `+` operator is defined on numeric types only, not on strings or slices.

### Conversions

```moxie
x := int64(42)
f := float64(x)
s := string(buf)        // no-op (same type)
b := []byte(s)          // no-op (same type)
```

Conversions between `string` and `[]byte` are no-ops since they are the same
type.

---

## Statements

### Assignment

```moxie
x = 42
x += 1
x |= " more"          // string/slice append-assign
a, b = b, a            // parallel assignment
```

### Send Statement

```moxie
ch <- value
```

### If Statement

```moxie
if x > 0 {
    println("positive")
} else if x < 0 {
    println("negative")
} else {
    println("zero")
}

if err := do(); err != nil {
    return err
}
```

### For Statement

Three forms:

```moxie
// condition-only (while loop)
for x > 0 {
    x--
}

// C-style
for i := int32(0); i < 10; i++ {
    println(i)
}

// range
for i, v := range slice {
    println(i, v)
}

// infinite loop
for {
    select { ... }
}
```

#### Range Expressions

| Range over | Key type | Value type |
|------------|----------|------------|
| Array, slice | `int32` (index) | Element type |
| String | `int32` (byte index) | `uint8` (byte value) |
| Map | Key type | Value type |
| Channel | Element type | - |
| Integer | `int32` | - |

`range` over a string yields individual bytes, not runes. To iterate over
UTF-8 code points, use `unicode/utf8.DecodeRune` or a rune iterator.

### Switch Statement

```moxie
switch x {
case 1, 2:
    doOneOrTwo()
case 3:
    doThree()
default:
    doOther()
}
```

Each case is self-contained. There is no `fallthrough` statement. To handle
multiple values with shared and distinct logic, use comma-separated case
expressions with conditional subdivision inside the case body:

```moxie
switch x {
case 1, 2, 3:
    doCommon()
    if x == 1 {
        doSpecialForOne()
    }
case 4:
    doFour()
}
```

Expression switches and type switches are both supported.

### Select Statement

`select` is the central event dispatch mechanism of Moxie programming. It is
bidirectional - it can both send and receive on channels in the same statement.
All event-driven control flow within a domain reduces to `select`:

```moxie
select {
case v := <-ch1:
    handle(v)
case ch2 <- x:
    // sent
case <-timer:
    // timeout
default:
    // no channel ready
}
```

Cases are checked in the order they are written, top to bottom. If multiple
channels are ready simultaneously, the first ready case wins. This is
deterministic - there is no random selection among ready cases.

`select` multiplexes channel messages, I/O events, and timer expirations into
a single event loop. The idiomatic Moxie program structure is `for { select
{...} }` at the core of each domain. Case ordering determines priority - put
higher-priority channels first.

`select` must not appear inside `init()` functions. Event dispatch begins only
after all packages are initialized and `main()` starts.

### Defer Statement

```moxie
defer f.Close()
```

Deferred calls execute in LIFO order when the enclosing function returns,
after all local variables have been freed but before return values are handed
to the caller.

### Return Statement

```moxie
return
return x
return x, nil
```

### Branch Statements

```moxie
break
break label
continue
continue label
goto label
```

`break` and `continue` with labels reference enclosing `for` loops only.
`goto` transfers control to any label within the same function - the target
label does not need to precede a `for` loop. A bare label (not preceding a
`for` loop) is legal only if referenced by a `goto`.

### Labeled Statements

Labels on `for` loops, referenced by `break` or `continue`:

```moxie
outer:
    for i := range items {
        for j := range items[i] {
            if done(i, j) {
                break outer
            }
        }
    }
```

Labels for `goto` targets:

```moxie
func process(data []byte) (err error) {
    if len(data) == 0 {
        goto cleanup
    }
    // ... processing ...
cleanup:
    release(data)
    return nil
}
```

---

## Built-in Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `append` | `append(s []T, elems ...T) []T` | Appends individual elements to a slice. `append(a, b...)` is illegal - use `a \| b` for slice concatenation. |
| `cap` | `cap(v) int32` | Capacity of a slice, channel, or array |
| `clear` | `clear(m)` | Clears a map or zeroes a slice |
| `close` | `close(ch)` | Closes a channel |
| `copy` | `copy(dst, src []T) int32` | Copies elements between slices |
| `delete` | `delete(m, key)` | Deletes a map entry by key |
| `free` | `free(v)` | Explicitly frees a variable. Using the variable after `free` without reassignment is a compile error. See [Memory Management](#memory-management). |
| `len` | `len(v) int32` | Length of a string, slice, array, map, or channel |
| `max` | `max(x, y...) T` | Maximum of comparable values |
| `min` | `min(x, y...) T` | Minimum of comparable values |
| `panic` | `panic(v error)` | Stops execution with an error value |
| `print` | `print(args...)` | Prints to stderr (no newline) |
| `println` | `println(args...)` | Prints to stderr (with newline) |
| `recover` | `recover() error` | Catches a panic in a deferred function |
| `spawn` | `spawn(fn, args...) chan struct{}` | Creates a new isolated domain |

`len` and `cap` return `int32`, not a platform-dependent integer.

---

## Memory Management

Moxie uses a block-scoped ownership model for memory. There is no garbage
collector and no escape analysis. Every allocation has a deterministic
lifetime derived from the source structure.

### Allocation

Variables are allocated at the point of declaration in execution order. There
is no hoisting - a variable does not exist until the line that declares it
executes.

### Scope Tags

Every allocation carries a scope tag identifying the block where it was
created. The scope tag determines when the allocation is freed.

### Block-Scoped Deallocation

Every `{}` block that contains statements (`if`, `for`, `switch`, `select`,
bare blocks) is a scope boundary. All variables created within a block are
freed at the end of that block:

```moxie
func example() {
    a := int32(1)          // scope: function body
    if cond {
        b := []byte{:1024} // scope: if-block
        // ...
    }                      // b is freed here
    // a is still live
}                          // a is freed here
```

Function parameters and return values are owned by the caller, not the
callee. Parameters arrive already allocated; return values survive function
return and become the caller's responsibility.

### Scope Promotion via Pointer

When a pointer to a variable is taken and assigned to another variable, the
pointed-to variable's scope tag is promoted to the scope of the variable it
is embedded in. This prevents the inner-scope variable from being freed
while an outer-scope pointer still references it:

```moxie
func example() *Item {
    var result *Item           // scope: function body
    for _, v := range items {
        item := &Item{v}      // scope: for-block initially
        result = item         // item's scope promoted to function body
    }                         // item is NOT freed here (scope was promoted)
    return result             // result survives via return
}
```

No escape analysis is needed. The compiler sees the pointer assignment
(`result = item`) and moves item's scope tag to match result's scope. This
is a local, syntactic operation on the scope tag - not a whole-program
analysis.

### Explicit Deallocation with `free`

The `free` builtin explicitly ends a variable's lifetime before its scope
closes:

```moxie
func process() {
    buf := []byte{:65536}
    // ... use buf ...
    free(buf)
    // buf is now dead - using it here is a compile error
    buf = []byte{:128}    // legal: reassigning starts a new lifetime
}
```

After `free(v)`, using `v` without first reassigning it is a compile error.
Assigning a new value (including zero value) to a freed variable is legal
and starts a new lifetime for that variable.

`free` is useful for releasing large allocations mid-function rather than
waiting for block exit, and for making ownership transfers explicit. It is
essential for long-running event loops with `select`.

### Between Domains

Each domain has its own memory space. There is no shared memory between
domains. Values crossing the spawn boundary are serialized via `moxie.Codec` -
the child receives an independent copy. When a domain exits, all its memory
is released.

---

## spawn

```moxie
done := spawn(fn, arg1, arg2, ...)
```

`spawn` creates a new **domain** - an isolated execution context with its own
single-threaded event loop and memory space. On native targets, the domain is
an OS process created via `fork()`. On the WASM target, the domain is a Web
Worker.

`spawn` is a language builtin. No import is required.

`spawn` must not appear inside `init()` functions. Domains can only be created
after all packages are initialized and `main()` starts.

### Arguments

The first argument must be a **static function name** (not a variable, not a
closure). Remaining arguments are passed to that function in the child domain.
The return value is `chan struct{}` - a lifecycle channel that closes when the
child exits.

```moxie
import "moxie"

func worker(id moxie.Int32, scale moxie.Float64) {
    println(float64(id) * float64(scale))
}

func main() {
    done := spawn(worker, moxie.Int32(1), moxie.Float64(2.5))
    _ = done
}
```

The compiler verifies at compile time:

- Argument count matches the function's parameter count exactly.
- Each argument type matches the corresponding parameter type exactly.
- Every argument type is serializable across the spawn boundary.

### Spawn Boundary Rules

The spawn boundary is a serialization boundary. Every argument is serialized
into the child's address space via `moxie.Codec`. There is no shared memory
between domains.

All data arguments and channel element types must implement `moxie.Codec`:

```moxie
type Codec interface {
    EncodeTo(w io.Writer) (err error)
    DecodeFrom(r io.Reader) (err error)
}
```

#### Built-in Codec Types

The `moxie` package provides codec wrappers for all primitive types. Default
encoding is little-endian. Big-endian aliases exist for network protocols:

| Default (LE) | Big-endian (BE) | Size |
|-------------|-----------------|------|
| `moxie.Bool`, `moxie.Int8`, `moxie.Uint8` | - | 1 byte |
| `moxie.Int16`, `moxie.Uint16` | `moxie.BigInt16`, `moxie.BigUint16` | 2 bytes |
| `moxie.Int32`, `moxie.Uint32` | `moxie.BigInt32`, `moxie.BigUint32` | 4 bytes |
| `moxie.Int64`, `moxie.Uint64` | `moxie.BigInt64`, `moxie.BigUint64` | 8 bytes |
| `moxie.Float32` | `moxie.BigFloat32` | 4 bytes |
| `moxie.Float64` | `moxie.BigFloat64` | 8 bytes |
| `moxie.Bytes` | - | 4-byte LE length prefix + data |

User-defined structs implement `Codec` by defining `EncodeTo`/`DecodeFrom`
methods that serialize each field.

#### Allowed Types at the Boundary

| Type | Treatment |
|------|-----------|
| Types implementing `moxie.Codec` | Serialized via EncodeTo/DecodeFrom |
| `*T` where `T` implements `Codec` | Copy semantics - pointer dissolves at the wire, receiver gets a fresh allocation |
| Channels (element type must implement Codec) | Become IPC channels over socketpair (native) or MessagePort (WASM) |

#### Rejected Types (compile error)

| Type | Reason |
|------|--------|
| Raw built-in types (`int32`, `bool`, etc.) | Use `moxie.Int32`, `moxie.Bool`, etc. |
| `func(...)` | Cannot serialize code |
| Interface types | Interfaces cannot cross the spawn boundary; use concrete Codec types |
| Raw pointers to non-Codec types | Cannot share memory across domain boundary |

### Move Semantics

Non-constant values passed to `spawn` are **moved** to the child domain. Using
the variable after `spawn` is a compile error:

```moxie
x := compute()
spawn(worker, x)
println(x)            // compile error: variable used after spawn
```

Exceptions:

- Constants and constant-foldable expressions are exempt (immutable).
- Channels are exempt (both parent and child hold IPC endpoints).

### Channel Completeness

Every channel created in a function must have both a sender and a listener
(receive or `select` case). Channels that escape the function (passed to calls,
returned, stored in structs) are exempt.

```moxie
ch := chan int32{}
ch <- 42              // compile error: channel has no listener
```

### IPC Channels

Channels passed to `spawn` become IPC channels. The runtime multiplexes them
over a single socketpair with length-prefixed messages. All spawn channel
operations are non-blocking with retry-and-yield (1ms sleep, indefinite retry
until success or pipe close). Both sides of the socketpair are symmetric peers.
Backpressure is expressed by pipe buffer fullness, not by blocking.

### Native Implementation

1. `socketpair(AF_UNIX, SOCK_STREAM)` creates a bidirectional IPC pipe.
2. `fork()` creates the child process (copy-on-write memory).
3. Child closes parent's socket end, runs the function directly, exits on
   completion.
4. Parent closes child's socket end, registers domain (pid + fd), continues.

Each domain has its own single-threaded event loop and memory space. A child
that exits unexpectedly is reaped on the next scheduler tick.

### WASM Implementation

1. A new Worker is created with an isolated microtask context.
2. Slices are element-copied, maps are shallow-copied.
3. Each domain gets its own event loop.
4. IPC uses `BroadcastChannel` for inter-domain messaging.

---

## Execution Model

### Program Startup

1. All packages are initialized in dependency order. For each package, the
   zero-value `var` declarations establish static storage, then `init()` (if
   present) runs.
2. `main()` in the `main` package is called. This is where event dispatch
   begins - `select` and `spawn` are available from this point forward.

### Domains

A Moxie program is a tree of **domains**. Each domain is an OS process
(native) or Worker (WASM) running a single thread.

```
+-------------------------------------+
|              Program                |
|  +----------+     +----------+     |
|  | Domain 0  |----| Domain 1  |     |
|  | (parent)  | IPC| (child)   |     |
|  |           |sock|           |     |
|  |  select   |pair|  select   |     |
|  |  +- ch1   |    |  +- ch3   |     |
|  |  +- ch2   |    |  +- timer |     |
|  |  +- I/O   |    |           |     |
|  +-----------+    +-----------+     |
|  single thread    single thread     |
+-------------------------------------+
```

### Concurrency Within a Domain

Execution flow within a domain is controlled by three constructs:

| Construct | Behavior |
|-----------|----------|
| Unbuffered channel send | Execution transfers to the waiting `select` case. Like a function call via the channel. |
| Buffered channel send | Message queued for the next `select` iteration. |
| `select` | Event handler - blocks until a channel message, I/O event, or timer fires. |

Because there is exactly one thread per domain, there are no data races.
Mutexes are no-ops. Atomics are plain loads and stores.

### Concurrency Between Domains

`spawn` creates child domains. Communication happens exclusively through IPC
channels. Values are deep-copied across the boundary via Codec serialization.
The parent and child cannot observe each other's memory.

This is not a relaxed memory model - it is **no shared memory**. The answer to
"when does domain A see domain B's write?" is: when B sends it on a channel
and A receives it.

### I/O Model

All I/O is non-blocking and buffered. Each domain uses `epoll` (Linux) or
`kqueue` (Darwin) for I/O readiness notification. No I/O operation blocks the
domain's thread - all I/O is dispatched through the event loop via `select`.

---

## Runtime

The runtime provides:

- **Cooperative task scheduling**: FIFO round-robin for internal runtime tasks
  (deferred I/O callbacks).
- **Channel dispatch**: Lock-free select with atomic CAS for fairness.
  Unbuffered channels transfer execution directly. Buffered channels use a
  circular ring buffer.
- **I/O polling**: `epoll`/`kqueue` integration. I/O waits yield to the event
  loop, not blocking the domain.
- **Context switching**: Cooperative stack switching via saved register sets.
  Stack overflow detected via canary values.

---

## Targets

| Target | Output | Libc |
|--------|--------|------|
| linux/amd64 | Static ELF | musl |
| linux/arm64 | Static ELF | musl |
| darwin/amd64 | Mach-O | libSystem |
| darwin/arm64 | Mach-O | libSystem |
| js/wasm | WASM + JS runtime | - |

All native binaries are fully statically linked. No dynamic library
dependencies at runtime.

The WASM target outputs ES modules with a runtime library. Browser APIs (DOM,
WebSocket, IndexedDB, Service Workers) are accessible via bridge packages in
`jsruntime/`.

### JS Bridge Packages

| Bridge | Purpose |
|--------|---------|
| `dom` | Element creation, tree manipulation, events |
| `ws` | WebSocket dial/send/close |
| `sw` | Service Worker lifecycle, fetch/cache, SSE |
| `localstorage` | localStorage operations |
| `idb` | IndexedDB transactions |
| `crypto` | secp256k1 (schnorr / BIP340) via WASM |
| `subtle` | SubtleCrypto bridge |

### JS Runtime Shims

Helper modules in `jsruntime/` used as WASM host shims. Not importable from
Moxie source; wired into WASM bridge code:

| Shim | Purpose |
|------|---------|
| `aead.mjs` | XChaCha20-Poly1305 |
| `p256.mjs` | NIST P-256 ECDH / ECDSA |
| `ed25519.mjs` | Ed25519 signatures |
| `x25519.mjs` | X25519 ECDH |
| `poly1305.mjs` | Poly1305 MAC |

### Stdlib Crypto

Pure Moxie implementations that compile on both native and WASM:

| Package | Purpose |
|---------|---------|
| `crypto/ec/secp256k1` | Bitcoin curve operations |
| `crypto/ec/schnorr` | BIP340 Schnorr signatures |
| `crypto/ec/bech32` | Bech32 / Bech32m address encoding |
| `crypto/ec/ecdsa` | secp256k1 ECDSA |
| `crypto/ec/chainhash` | Double-SHA-256 helpers |
| `encoding/base58` | base58 / base58check encoding |

---

## External Dependencies

`moxie build` is network-free. It reads `.mxh` protocol headers from the cache
to type-check against external packages, but never fetches source at build
time.

Dependencies are installed separately:

```sh
moxie fetch                            # install all deps from moxie.mod
moxie install git.example.com/somepkg  # install a single package
```

### Protocol Headers (.mxh)

A `.mxh` file describes a package's codec types - exported types that implement
`moxie.Codec`, their wire layout, and method signatures. No implementation, no
unexported types.

Format:

```
// mxh v1 <fnv1a-hex-hash>
// wire: uint32-length-prefixed EncodeTo/DecodeFrom at cross-binary spawn boundaries
type Int32 int32
func (*Int32) EncodeTo(w io.Writer) (err error)
func (*Int32) DecodeFrom(r io.Reader) (err error)
```

The hash changes when any type layout changes. The compiler embeds this hash at
cross-binary spawn sites and the runtime verifies it at spawn time.

### Cross-Binary spawn

When a package is imported from the `.mxh` cache, `spawn` forks and execs the
cached binary instead of forking the current process:

```moxie
import iskradb "git.example.com/iskradb"

// same syntax whether iskradb is local source or cached .mxh
done := spawn(iskradb.InsertTriple, key, val, results)
```

At spawn time, parent and child exchange `.mxh` hashes (5s timeout). Hash
mismatch means the installed binary is stale - run `moxie install` again.

---

## Runtime Guarantees

1. **Single-threaded per domain.** No preemption.
2. **No data races.** Single-threaded execution makes races structurally
   impossible.
3. **Complete domain isolation.** `fork()` provides OS-level memory isolation.
4. **Deterministic execution.** Given the same inputs, execution follows one
   path.
5. **Static binaries.** No dynamic dependencies. Ship one file.

---

## WASM-Specific Notes

### WASM String Pragma

In the WASM target, the compiler preserves the source spelling of `string` vs
`[]byte` as a representation pragma. `string` tells the code generator to use a
native JS string at the WASM/JS boundary. `[]byte` tells it to use a byte-array
object.

A `[]byte` field receiving text at the WASM boundary will corrupt multi-byte
UTF-8 sequences (CJK, emoji) silently, because the JS runtime treats it as a
raw byte buffer rather than encoded text.

**Declare all text-holding struct fields as `string` when targeting WASM.**

This is not a type distinction - `string` and `[]byte` remain the same type
and are mutually assignable. The spelling is a hint to the code generator only.

---

## Quick Reference

| Feature | Moxie |
|---------|-------|
| File extension | `.mx` |
| Module file | `moxie.mod` |
| Text type | `string = []byte`, mutable, `string` is a WASM representation pragma |
| Text concatenation | `\|` operator |
| Integer sizes | Explicit widths only; `int`/`uint` are illegal |
| Concurrency | Channels + `select` within a domain, `spawn` for new domains |
| Process isolation | `spawn(fn, args...)` via `fork()` + socketpair |
| Serialization | `moxie.Codec` interface at spawn boundary |
| Memory model | Allocate-at-site, implicit free at function return, explicit `free()` builtin |
| Domain memory | No shared memory between domains |
| Slice equality | `==` for any comparable element type |
| Slice literals | `[]T{:n}`, `[]T{:n:c}` |
| Channel literals | `chan T{}` (unbuffered), `chan T{n}` (buffered) |
| Named slice/map types | Compile error |
| Value receivers | Compile error; pointer receivers only |
| Unnamed return values | Compile error; all returns must be named |
| `any` / `interface{}` | Compile error; interfaces must have methods |
| Scope shadowing | Compile error; no redeclaration of visible names |
| Package scope vars | `var X T` only; `var X = expr` is a compile error |
| `init()` | Library packages only, max one, no `select` or `spawn` |
| Targets | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, js/wasm |
