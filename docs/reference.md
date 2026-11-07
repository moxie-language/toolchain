# Moxie Language Reference

Moxie is a compiled systems language for domain-isolated event-driven programs.
Programs compile to static native binaries or WebAssembly. Source files use the
`.mx` extension and are UTF-8 encoded.

This document specifies the Moxie language. Familiarity with C-family syntax is
assumed.

For canonical formatting rules and the `moxiefmt` tool, see
[format.md](format.md).

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

All identifiers are accessible cross-package. There is no access control
based on case - any package can reference any identifier in any other package.

An identifier is part of the **stable API contract** if its first character is
a UTF-8 uppercase letter (Lu category) or if it carries the `#` prefix. This
includes ASCII A-Z and uppercase letters from any script with case distinction.

For scripts without case distinction (CJK, Arabic, Hebrew, Thai, etc.), the
`#` prefix marks an identifier as part of the stable API:

```moxie
func #计算(x, y int32) (sum int32) {    // stable API
    sum = x + y
    return
}

func 内部处理() (r int32) {              // accessible, but no stability guarantee
    r = 0
    return
}
```

The `#` is an annotation, not part of the identifier name. `#foo` and `foo`
are the same identifier - declaring both in the same scope is a redeclaration
error. The `#` only appears at the declaration site. Cross-package references
use the bare name:

```moxie
import "example/math"
result := math.计算(1, 2)
```

Removing or changing the signature of a stable API symbol (Capital or `#`)
without bumping the major version in `moxie.mod` is a compile error when
building against a cached `.mxh`. Lowercase symbols carry no stability
guarantee and may change freely between versions.

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
cap     clear   close   copy    delete  len
max     min     panic   pop     print   println
push    resize  spawn
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

The `...` token is valid in variadic parameter declarations
(`func f(args ...T)`) and in variadic call sites (`f(slice...)`).
`append` does not exist in any spelling - use `push(s, v...)` for element
insertion and `a | b` for slice concatenation. The name is not predeclared and
the compiler carries no append code path: the internal alias it once lowered to
(`__moxie_append`) is gone, so neither name resolves.

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
ch := chan int32{}             // one-slot mailbox
ch := chan int32{10}           // 10 outstanding messages
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

`uintptr` is a predeclared pointer-sized integer, the type used for pointer
arithmetic with `unsafe.Pointer`; it is not a replacement for a typed pointer.

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

Named types may be declared over any type that can carry methods: numeric,
bool, string, array, struct, slice, map and interface types. A name over a
slice or map exists to give that type a METHOD SET, and that is a good reason
to have the name:

```moxie
type Events []Event
func (e *Events) Add(v Event) { *e = push(*e, v) }

type Index map[string]int32
func (x *Index) Add(k string, n int32) { (*x)[k] = (*x)[k] + n }
```

The receiver is a pointer, as for every Moxie method, and that is what makes
the mutation real: `*e = push(*e, v)` writes the caller's variable and the new
header survives the return. Go's value receiver is the reason a method on a
named slice there looks like it "forks" its receiver: element writes reach the
shared backing array while a rebinding of the receiver is lost, so some
mutations persist and some vanish. Moxie does not have that split - value
receivers are refused outright, and there is nothing concurrent here (one
thread per domain, no shared mutable state) for the copy to protect. A slice
or map is a pointer with metadata; a pointer receiver is exactly what it
needs.

An alias of such a type is the same type, method set included:
`type A = Names` makes `A` another spelling of `Names`, not a new type.

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

### External Data Bindings (`//:linkdata`)

A package-level byte slice can be bound to data that the link step supplies
instead of storage the package defines:

```moxie
//:linkdata 48 go:fipsinfo
var Linkinfo []byte
```

The compiler builds the slice header itself: the data pointer is the named
symbol, `len` is the declared length, and `cap` is `-1`. The symbol is declared
`extern_weak`, so a build in which nothing produces it still links and leaves
the data pointer null.

The directive exists because an externally-produced blob would otherwise need a
struct whose field layout has to match a foreign writer. A slice header is the
one shape the compiler can construct for data it does not define, so the layout
question never reaches a type.

The capacity word is the all-ones sentinel internally, but `cap()` reports the
length, so `0 <= len <= cap` holds for every consumer; the sentinel is what the
length-changing builtins check. The usual `len <= cap` invariant is deliberately violated, and every
builtin that would change the length refuses it:

- `push` dies with "linkdata slice: cap is foreign (copy it first)" instead of
  writing past the imported symbol - an all-ones capacity would otherwise look
  large enough and the store would silently run off the end of the blob;
- `pop` refuses, because shortening the slice is still a change of length;
- `resize` refuses any new length, including a smaller one.

Reading, indexing and slicing within `len` are unaffected.

Binding is one-way and read-only. A package-level variable cannot be assigned
outside `init`, and a store through it - `Linkinfo[0] = 0` - is rejected as a
package-global mutation, so a link-data slice cannot be written at all. Data
that has to be written belongs in a slice the application creates: in `main`
itself, or in an initializer function that something in `main` calls. A main
package cannot declare `init()` - the entry point is `main` - so the setup is
explicit and belongs to the application, not to a library's package scope.
Reads copy from the bound slice into that storage.

Constraints:

- the variable must be a slice of one-byte elements;
- the length is fixed: `push`, `pop` and `resize` all refuse the binding;
- the declared length must be positive;
- the symbol is declared `[length x i8]`, so every package that binds the same
  symbol must declare the same length - `llvm-link` merges the declarations;
- `//:linkdata` is spelled `//:linkdata` in Moxie source and the Go toolchain
  used for the bootstrap sees it rewritten as `//go:linkdata`, exactly like
  `//:linkname`.

### `//:linkname` names a location, `//:embed` carries the data

`//:linkname <local> <symbol>` binds a declaration to a symbol that already
exists at link time: the address of a function the runtime exports, or the
address of a variable another object defines. It names a **memory location**,
and that is all it can do - it carries no length, no capacity and no ownership.
A blob brought in through a linkname is an address whose size the program does
not know, so it must not be used to import binary data.

Raw data belongs to `//:embed`, which copies the file's bytes into the
compiler's output and generates the length itself:

```moxie
//:embed data/C_r16.bin
var _C_r16 []uint16
```

`//moxie:embed_le <path>` does the same for a typed table, taking the element
size from the declaration (`[]uint16`, `[]uint32`, `[]int32`, `[]int64`, and the
unicode table structs `[]Range16`, `[]Range32`, `[]CaseRange`, `[]foldPair`) and
dividing the file size by it for the element count. The result is a normal
slice whose storage is part of the module, with `len` and `cap` derived from the
file - no external producer, no capacity sentinel, no struct layout to agree on.

Byte order: embed copies bytes verbatim and never swaps them. `_le` names the
file's order, so the file must match the target's; every current target
(amd64, arm64, 386, wasm) is little-endian, and a big-endian input would need a
conversion pass first - there is no `_be` variant.

`//:embed` is understood by both compilers (the bootstrap loader rewrites it to
`//go:embed`). `//moxie:embed_le` is implemented in stage4 only: the loader
rewrites `//:` pragmas, not `//moxie:` ones, so a legacy build of a package
using it sees a zero-valued global. Recorded as a bilateral gap in
`the work log`.

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

### The Entry Point: main, spawn targets and shutdown

Two functions in a process are special, and the compiler enforces both shapes.

**`main`.** A main package cannot declare `init()`. The shape is:

```moxie
func main() {
	select {
	case <-shutdown:
		return
	default:
	}

	// setup ...

	for {
		select {
		case <-shutdown:
			cleanup()
			return
		case msg := <-in:
			handle(msg)
		}
	}
}
```

The first select satisfies the check for a batch program that runs straight
through; the loop's arm is what makes the program stoppable while it runs. The
check is mandatory - a `main` without a `case <-shutdown:` arm is a compile
error:

    main must select on shutdown: add a `case <-shutdown:` arm to its select
    (or a non-blocking one at entry for a batch program), because a program
    that cannot be told to stop has to be killed

`shutdown` is a predeclared word, not `close`: `close(ch)` remains the builtin.
The channel is created on first use in the root arena, so it outlives every
frame that selects on it, and it is only ever closed.

**Spawn targets.** A function is a spawn target when its ONE result is named
`closed` and typed `chan struct{}`:

```moxie
func worker(in chan Request, out chan Response) (closed chan struct{}) {
	for {
		select {
		case <-closed:
			return closed
		case req := <-in:
			out <- handle(req)
		}
	}
}
```

The compiler enforces that shape too: a target must select on its `closed`
breaker and return it. A target that never selects on it is rejected:

    spawn: node never selects on its closed breaker; a spawned domain must
    handle termination, so its loop selects on <-closed and returns the breaker

The breaker is a SIGNAL, not a message: nothing is ever sent on it, `close` is
the whole event, and a parent stops a child with `close(done)`. A nil breaker
means never canceled. `shutdown` for the entry point and `closed` for a spawned
domain are the same primitive, which is why the shapes repeat.

**Graceful shutdown.** The close of `shutdown` is cooperative: the platform's
stop source raises it (SIGINT/SIGTERM hosted, `module_exit` in a module, the
host's teardown on wasm, where the channel never fires), and the runtime closes
the channel in normal context at the next wait - a signal handler only raises a
flag, it cannot allocate or close a channel. "Graceful" means:

- **reach a wait.** A domain parked in a select wakes; a domain in a tight
  compute loop does not see the close until it waits, so long tasks are broken
  into units that let the loop come back around.
- **finish the current unit, then leave.** The arm runs between units, never
  mid-store - the same rule a worker follows for its `closed` breaker.
- **close children first, then flush, then return.** A parent closes each
  child's breaker (the close propagates through the pipe-bound channels), then
  releases what it owns. There is no join primitive: if a parent must know a
  child has finished, the child sends on a completion channel before returning;
  the breaker itself only signals.
- **the breaker carries no payload and no acknowledgement.** Correctness cannot
  depend on the parent hearing back.

**The involuntary exit.** `panic` is death: there is no `recover`, deferred
calls do not run on the way out, and an involuntary stop - SIGKILL, OOM, power
loss, a crashed module unloaded by the kernel, a Worker torn down by the page -
runs neither the shutdown arm nor any deferred code. The shutdown path is an
optimization for a clean exit, never the mechanism that makes state correct.
State that must survive an involuntary exit has to be recoverable without it:
write the new file and `rename` it into place; append a commit record and treat
the last complete record as the state; make replay idempotent; checksum what is
persisted so a torn tail is detected rather than adopted. A program whose state
is only consistent if the shutdown arm ran was not graceful - it was lucky.

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
| `push` | `push(s []T, elems ...T)` | Appends elements; grows when capacity is short (lazy-init floor 4, then doubling). The abandoned backing stays in the arena that allocated it. Store-back is implicit. |
| `pop` | `pop(s []T) T` | Removes and returns the last element. Panics if len == 0. Store-back is implicit. |
| `resize` | `resize(s []T, n int32)` | Sets the slice length to n. Panics if n > cap or n < 0. Store-back is implicit. |
| `cap` | `cap(v) int32` | Capacity of a slice, channel, or array |
| `clear` | `clear(m)` | Clears a map, or zeroes a slice's elements in place (length unchanged) |
| `close` | `close(ch)` | Closes a channel |
| `copy` | `copy(dst, src []T) int32` | Copies elements between slices |
| `delete` | `delete(m, key)` | Deletes a map entry by key |
| `len` | `len(v) int32` | Length of a string, slice, array, map, or channel |
| `max` | `max(x, y...) T` | Maximum of comparable values |
| `min` | `min(x, y...) T` | Minimum of comparable values |
| `panic` | `panic(v error)` | Stops execution with an error value - FATAL, no recover |
| `print` | `print(args...)` | Prints to stderr (no newline) |
| `println` | `println(args...)` | Prints to stderr (with newline) |
| `spawn` | `spawn(fn, args...) chan struct{}` (target result named `closed`) | Creates a new isolated domain thread |

`len` and `cap` return `int32`, not a platform-dependent integer.
`recover` is not callable - a panic is fatal - and no longer appears above.
The full slice survey, including the reader costs, is [docs/slices.md](slices.md).

### push/pop/resize vs append

`append` is **undefined** in Moxie - the name is not predeclared, and the
compiler has no append lowering left: the internal `__moxie_append` alias it
used to keep is gone, so a source file cannot reach append semantics under any
spelling. It was removed because it conflates two operations - allocation and
element insertion - into one call with unpredictable behavior:

- `append` sometimes returns the same backing array, sometimes a new one.
  The caller cannot know which without comparing pointers.
- `append` sometimes copies, sometimes doesn't. Code that aliases a slice
  prefix and then appends to the original may or may not corrupt the alias,
  depending on whether capacity happened to be sufficient.
- `append` hides capacity decisions from the programmer. Growth strategy is
  runtime-internal; the programmer never declares intent about expected size.

`push` separates the two:

- **Allocation** is explicit: declare capacity with `[]T{:0:cap}`. A push from
  a zero-capacity slice lazy-inits it to the first push batch (floor 4) in one
  allocation.
- **Insertion** is `push(s, v1, v2, ...)` - stores elements into the capacity
  that is there. The compiler emits the store-back to `s` implicitly (like
  `i++` modifies `i`).
- **Overflow grows, and growth is charged to the arena.** Pushing past the
  capacity doubles it and copies the elements, so a push loop into a slice
  never presized enters the growth regime; every abandoned backing stays live
  in the arena that allocated it until that arena is released. In a sovereign
  receiver's arena that is permanent. This is why the tree's growth happens
  inside free functions (their frame arena dies at return) and why
  `mxutil.Ensure`/`Reserve`/`GrowCap` exist: presize when the size is knowable,
  and let the one place that must grow be a function whose garbage dies early.

`pop` and `resize` complete the set: `pop` is the inverse of a single-element
push, and `resize` gives direct control over length without touching capacity.

```moxie
items := []int32{:0:100}      // explicit capacity
push(items, 1, 2, 3)         // items is now [1, 2, 3], len=3, cap=100
last := pop(items)            // last=3, items is now [1, 2], len=2
resize(items, 0)              // items is now [], len=0, cap still 100
```

---

## Memory Management

Moxie has no garbage collector. Memory management is deterministic and
structural: every allocation belongs to an arena, and arenas are released in
bulk. There is no individual free, no mark/sweep, no reference counting.

### Domain Root

Each domain has a single `DomainRoot` structure, created once at domain init
via `mmap`. It contains:

- A **function arena stack** (`fnStack[512]*Arena`, `fnTop` counter) - the
  active arena is always `fnStack[fnTop]`.
- A **child domain stack** for tracking spawned children.
- An inline **root arena** at `fnStack[0]` that persists for the domain lifetime.

The root arena holds package-level zero-value `var` declarations and any
allocations made by the program's top-level functions (main, event loops).

### Arena Structure

Each arena is a bump-pointer allocator backed by one or more `mmap` regions.
The `Arena` struct is embedded at offset 0 of its primary mmap region - no
external allocation needed for metadata.

```
Arena layout:

    [Arena header | region 0 data ...............]
                   ^off                    ^cap

    If region 0 fills, a new region is mmap'd:
    regions[0]: [header | data...]
    regions[1]: [data................]
    regions[2]: [data....]
```

Each arena has a flat registry of chained regions (a slice, no linked list, up
to 128 regions).
When the active region fills, a chained region is mmap'd and appended: 64MB by
default, or twice the active region's capacity (capped at 64MB) for a sovereign
group arena, so a few-KB group does not reserve a whole 64MB region.
`alloc(size)` bumps the active region's offset and returns zeroed memory.

### Per-Function Arena Lifecycle

Every function that allocates gets its **own arena**, pushed onto the domain's
function arena stack. The compiler emits:

1. **Prologue**: `FnArenaPush(capacity)` - acquires an arena from the pool
   (or creates a new one via mmap) and pushes it onto `fnStack`.
2. **Body**: all allocations go into this function's private arena.
3. **Epilogue**: return values are relocated into the caller's arena via a
   three-pass mark-compact algorithm, then the function's arena is popped
   and released back to the pool.

```moxie
func buildPair(x, y int32) (result *Pair) {
    // compiler-emitted: FnArenaPush(65536)
    result = &Pair{X: x, Y: y}
    // compiler-emitted: relocate result into caller's arena
    // compiler-emitted: FnArenaPop + ArenaRelease
    return
}
```

The relocation pass walks the return value's type graph, identifies pointers
into the function's arena, allocates fresh copies in the caller's arena, and
patches all internal pointers. After relocation, the function's entire arena
is released - either returned to the pool for reuse, or munmap'd if the pool
is full.

### Arena Pool

A bounded freelist of reset arenas (capped by both count and bytes) eliminates
mmap/munmap syscalls for short-lived function arenas. `ArenaAcquire` checks the pool first;
`ArenaRelease` resets and returns the arena to the pool if there's room.

### Ownership Boundaries

Function boundaries are ownership boundaries. A function's allocations are
invisible outside that function. Data escapes only through return values,
which are explicitly relocated. This means:

- No escape analysis needed
- No pointer tracking across call boundaries
- No use-after-free possible (returned data is always a fresh copy)
- No dangling references into a called function's arena

Three boundaries exist where memory changes hands:

| Boundary | Mechanism | Direction |
|----------|-----------|-----------|
| Function return | Relocate into caller's arena | Callee -> Caller |
| Spawn | value copy (or codec relocation) into the child arena | Parent -> Child |
| Domain exit | munmap (bulk release) | Domain -> Kernel |

### Parameters

Function parameters point into the caller's arena and are **mutable**: a callee
may write through a parameter pointer and may assign to a parameter. There is
one thread of execution per domain, so there is no concurrent mutation for
immutability to prevent, and a write through a parameter is visible to the
caller after the call.

The limit is lifetime, not permission: a callee that stores a pointer to its own
frame through a parameter hands the caller a dangling pointer, because the
callee's frame is released at return and the return protocol only relocates
return values and receiver store-backs.

Named return values are required (`func f() (n int32)`, never `func f() int32`),
and a named result is assignable like any local.

Package-scoped variables are immutable outside `init()`: every package variable
is thread-local, so a write after init would change one domain's copy while the
other domains keep the value they were born with.

### Between Domains

Each domain has its own arena tree. Domains are threads in one address space,
but there is no API that reaches another domain's memory: values crossing the
spawn boundary are copied (codec-relocated when they carry references) into the
child's arena, and a channel handle is the only shared capability. A domain's
arenas are released when its thread ends.

### Long-Running Event Loops

The per-function arena model means the outermost function of an infinite event
loop (`for { select { ... } }`) never returns and never frees its arena.
Allocations inside the loop body accumulate unless managed:

1. Factor allocation-heavy work into helper functions - their arena is
   released on return.
2. `delete(map, key)` when entries are done.
3. `slice = nil` releases the backing array reference.
4. Monotonically growing maps need rotation or bounded-size policies.

```moxie
func eventLoop(ch chan Request) {
    for {
        select {
        case req := <-ch:
            handleRequest(req)  // allocations in own arena, freed on return
        }
    }
}
```

---

## spawn

```moxie
done := spawn(fn, arg1, arg2, ...)
```

`spawn` creates a new **domain** - an isolated execution context with its own
single-threaded event loop, TLS and arena tree. On native targets the domain is
a thread in the same process; on the WASM target it is a Web Worker.

`spawn` is a language builtin. No import is required. It must not appear inside
`init()` functions.

### Arguments

The first argument must be a **static function name** (not a variable, not a
closure). The target's single result must be named `closed` and typed
`chan struct{}`; the runtime binds the domain's breaker into it and the target
selects on it to observe shutdown.

```moxie
func worker(in chan Request, out chan Response) (closed chan struct{}) {
    for {
        select {
        case <-closed:
            return closed
        case req := <-in:
            out <- handle(req)
        }
    }
}

func main() {
    in := chan Request{}
    out := chan Response{}
    done := spawn(worker, in, out)
    _ = done
}
```

The compiler verifies at compile time:

- Argument count matches the function's parameter count exactly.
- Each argument type matches the corresponding parameter type exactly.
- The target has the spawn shape (one result, named `closed`, typed
  `chan struct{}`).
- The target does not capture closures.
- Every channel argument's ring can be sized from its declaration and the
  frame budget (`cap` tags).

### Spawn Boundary Rules

Value arguments are **copied and frozen** at the call; channel arguments are
**shared handles** (both domains hold the same channel object). There is no
move: the parent may keep using a value after `spawn`.

Every variable-length dimension of a value type declares its size in a `cap`
struct tag, one number per dimension, outermost first, counted in elements of
that dimension (bytes for `string`/`[]byte`). An unstated dimension is one page
(4096 bytes).

```moxie
type Request struct {
    ID   int64
    Tags []string `cap:"32,64"`   // 32 tags, 64 bytes each
    Body []byte   `cap:"65536"`   // 65536 bytes
}
```

A value that carries references is encoded by its codec: its own
`EncodeTo`/`DecodeFrom` when it defines them, the structural codec otherwise.
A pointer-free value moves as raw bytes. A channel, interface, function value or
type parameter in the frame path is a compile error: no count can bound it.

### Channel Completeness

Every channel created in a function must have both a sender and a listener
(receive or `select` case). Channels that escape the function are exempt.

```moxie
ch := chan int32{}
ch <- 42              // compile error: channel has no listener
```

### Channels at the Boundary

Channels passed to `spawn` are the same channel objects in the child; their
rings are already in shared memory. A channel is a mailbox with the depth its
declaration gives it: `chan T{}` is one slot, `chan T{:N}` is N. A send returns
once its frame is in the ring and parks on the ring's read index (a futex wake
on the receiver's advance) when the declared depth is outstanding. Close is a
flag on the ring; a dead peer is observed through the ring state and the
domain's breaker.

### Native Implementation

1. The channel object and its SPSC rings exist in the shared address space.
2. `pthread_create` starts the domain thread (64 MB stack) with a fresh
   `DomainRoot`/TLS and root arena.
3. The child marks the child-side ring halves for each channel argument, runs
   the target directly, and closes its breaker when it returns.
4. The parent registers the channel handles and returns the lifecycle channel.
5. A dead peer is detected through the breaker and the ring liveness bits; there
   is no `waitpid` and no socketpair.

### WASM Implementation

1. A new Worker is created; the domain gets its own event loop.
2. Values are copied across the worker boundary through the host shims;
   channels are the host's message ports.
3. Channel close and worker death are signalled through the same breaker.

---

## Execution Model

### Program Startup

1. All packages are initialized in dependency order. For each package, the
   zero-value `var` declarations establish static storage, then `init()` (if
   present) runs.
2. `main()` in the `main` package is called. This is where event dispatch
   begins - `select` and `spawn` are available from this point forward.

### Domains

A Moxie program is a tree of **domains**. Each domain is a thread in one
process (native) or Worker (WASM) running a single event loop.

```
+-------------------------------------+
|              Program                |
|  +----------+     +----------+     |
|  | Domain 0  |----| Domain 1  |     |
|  | (parent)  |ring| (child)   |     |
|  |           |    |           |     |
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
| Send to `chan T{}` | Queued in the one-slot mailbox; a second send parks until the receiver takes the first. |
| Send to `chan T{:N}` | Queued while fewer than N are outstanding; the sender parks beyond that. |
| `select` | Event handler - blocks until a channel message, I/O event, or timer fires. |

Because there is exactly one thread per domain, there are no data races.
Mutexes are no-ops. Atomics are plain loads and stores.

### Concurrency Between Domains

`spawn` creates child domains. Communication happens through channel handles
whose rings are in shared memory; values are copied (codec-relocated when they
carry references) into the receiver's arena, and there is no API that lets one
domain address another's memory.

The answer to "when does domain A see domain B's write?" is: when B sends the
value on a channel and A receives it - not through shared addressable state.

### I/O Model

All I/O is non-blocking and buffered. The readiness backend in the tree is
`epoll` (`src/runtime/poll.mx`); there is no `kqueue` implementation. No I/O
operation blocks the domain's thread - all I/O is dispatched through the event
loop via `select`.

---

## Runtime

The runtime provides:

- **Cooperative task scheduling**: FIFO round-robin for internal runtime tasks
  (deferred I/O callbacks).
- **Channel dispatch**: Lock-free select with atomic CAS for fairness.
  Unbuffered channels transfer execution directly. Buffered channels use a
  circular ring buffer.
- **I/O polling**: `epoll` integration. I/O waits yield to the event
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

A `.mxh` file describes a package's codec types - types that implement
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
3. **Domain isolation is the contract.** Threads share one address space, but no
   API reaches another domain's memory; values cross only as copies (or
   codec-relocated frames) and a channel handle is the only shared capability.
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
| Domain isolation | `spawn(fn, args...)` creates a thread with its own TLS/arena tree; channel handles are the only shared capability |
| Serialization | a value's own codec or the structural codec; pointer-free values move as bytes |
| Memory model | Per-function arena (push/pop stack), relocate on return, bulk release at domain exit |
| Domain memory | One address space, no addressable cross-domain state; channel rings are shared |
| Slice equality | `==` for any comparable element type |
| Slice literals | `[]T{:n}`, `[]T{:n:c}` |
| Channel literals | `chan T{}` (one slot), `chan T{n}` (n outstanding messages) |
| Named slice/map types | Compile error |
| Value receivers | Compile error; pointer receivers only |
| Unnamed return values | Compile error; all returns must be named |
| `any` / `interface{}` | Compile error; interfaces must have methods |
| Scope shadowing | Compile error; no redeclaration of visible names |
| Package scope vars | `var X T` only; `var X = expr` is a compile error |
| `init()` | Library packages only, max one, no `select` or `spawn` |
| Targets | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, js/wasm |
