# Porting Go Code to Moxie

This guide covers the mechanical changes needed to convert Go source files to Moxie syntax. Moxie is a restricted subset of Go with domain-isolated cooperative concurrency. The compiler enforces these restrictions at build time.

## Quick Reference

| Go | Moxie |
|----|-------|
| `file.go` | `file.mx` |
| `"hello"` | `"hello"` (auto-wrapped to `[]byte`) |
| `s + t` (strings) | `s \| t` |
| `s += t` | `s = s \| t` |
| `new(T)` | `&T{}` |
| `complex64`, `complex128` | Not supported |
| `complex(r, i)` | Not supported |
| `real(z)`, `imag(z)` | Not supported |
| `uintptr` | Not supported (unless importing `unsafe`) |
| `fallthrough` | Merge cases: `case A, B:` |
| `make(chan T)` | `chan T{}` |
| `make(chan T, n)` | `chan T{n}` |
| `make([]T, n)` | `[]T{:n}` |
| `make([]T, n, c)` | `[]T{:n:c}` |
| `import "strings"` | `import "bytes"` (preferred) |

## Step 1: Rename File

Rename `.go` to `.mx`. The compiler finds `.mx` files transparently — if both `.go` and `.mx` exist, `.mx` wins.

## Step 2: Text Concatenation

Replace all string `+` with `|` (pipe operator):

```go
// Go
msg := "hello " + name + "!"
s += " suffix"

// Moxie
msg := "hello " | name | "!"
s = s | " suffix"
```

The compiler rewrites `|` on `[]byte` operands to `__moxie_concat()` calls. The `+` operator on text types is a compile error.

**String literals are auto-wrapped.** The compiler rewrites `"hello"` to `[]byte("hello")` automatically. You don't need to change string literals.

**`string` is preferred.** With type unification, `string` and `[]byte` are the same type — same layout, mutually assignable. Use `string` in signatures and declarations for readability; use `[]byte` when the byte-level nature matters.

## Step 3: Remove `new()`

```go
// Go
p := new(MyStruct)
p.Field = 42

// Moxie — two options:
p := &MyStruct{}        // composite literal
p.Field = 42

var x MyStruct           // var declaration
x.Field = 42
p := &x
```

## Step 4: Remove `fallthrough`

Flatten cascading cases:

```go
// Go
switch x {
case 1:
    fallthrough
case 2:
    doSomething()
case 3:
    fallthrough
case 4:
    doOther()
}

// Moxie
switch x {
case 1, 2:
    doSomething()
case 3, 4:
    doOther()
}
```

If the cases have distinct logic before fallthrough, extract to a function:

```go
// Go
case 1:
    setup()
    fallthrough
case 2:
    doWork()

// Moxie
case 1:
    setup()
    doWork()
case 2:
    doWork()
```

## Step 5: Remove Complex Numbers

Delete or stub any code using `complex64`, `complex128`, `complex()`, `real()`, `imag()`.

For stdlib packages that had complex number support, the convention is to replace the body with `panic("moxie: complex numbers not supported")` and remove the complex types from signatures:

```go
// Go
func FormatComplex(c complex128, ...) string { ... }

// Moxie — stub with panic, remove complex from signature
func FormatComplex(re, im float64, ...) string {
    panic("moxie: complex numbers not supported")
}
```

For type switch cases:

```go
// Go
case complex64:
    formatComplex(...)
case complex128:
    formatComplex(...)

// Moxie — remove the cases entirely, or comment
// complex64, complex128 cases removed — not supported in moxie
```

## Step 6: Remove `uintptr`

Replace `uintptr` with explicit pointer types. Exception: packages that `import "unsafe"` are allowed to use `uintptr` (needed for `unsafe.Pointer` arithmetic).

If the package legitimately does pointer arithmetic, add `import _ "unsafe"` to get the exemption.

## Step 7: Slice and Channel Literals (Optional)

These syntactic shortcuts are available but not required:

```go
// Go
ch := make(chan int)
ch := make(chan int, 10)
s := make([]byte, 256)
s := make([]byte, 0, 1024)

// Moxie (optional syntax)
ch := chan int{}
ch := chan int{10}
s := []byte{:256}
s := []byte{:0:1024}
```

Both forms compile. The Moxie syntax is rewritten to `make()` calls before parsing.

## Step 8: Prefer `bytes` over `strings`

With `string` = `[]byte`, the `strings` and `bytes` packages are functionally identical. Prefer `bytes`:

```go
// Go
import "strings"
strings.Contains(s, "foo")

// Moxie
import "bytes"
bytes.Contains(s, "foo")
```

`import "strings"` is a compile error — use `bytes` for all text operations.

## What the Compiler Does Automatically

You do NOT need to manually:

- Wrap string literals in `[]byte()` — the compiler does this
- Convert `len()`/`cap()` return types — the compiler handles int32 truncation
- Change `string` to `[]byte` in type declarations — they're the same type
- Rewrite `+=` on text — the compiler detects and rewrites this
- Handle `string` in function signatures — assignability works both ways

## Common Compiler Errors and Fixes

| Error | Cause | Fix |
|-------|-------|-----|
| `moxie: '+' is not allowed for text concatenation: use \| operator` | String `+` | Change `+` to `\|` |
| `moxie: 'new' is not allowed` | `new(T)` call | Use `&T{}` |
| `moxie: type 'complex128' is not allowed` | Complex type in code | Remove or stub with panic |
| `moxie: type 'uintptr' is not allowed` | uintptr without unsafe | Add `import _ "unsafe"` or use pointer types |
| `moxie: 'fallthrough' is not allowed` | fallthrough statement | Merge cases with commas |
| `moxie: variable used after spawn` | Using moved variable | Don't reference args after spawn call |
| `does not implement moxie.Codec` | Raw type at spawn boundary | Use `moxie.Int32`, `moxie.Float64`, etc. |

## Exempt Packages

These packages are permanently exempt from restrictions (they implement low-level primitives):

- `runtime/*`, `internal/*`, `unsafe`, `reflect`
- `os/*`, `syscall/*`

Code in these packages can use `new()`, `fallthrough`, `uintptr`, and native `string` type freely.

## Porting Checklist

For each `.go` file:

1. [ ] Rename to `.mx`
2. [ ] `+` on strings → `|`
3. [ ] `+=` on strings → `= ... |`
4. [ ] `new(T)` → `&T{}`
5. [ ] `fallthrough` → merge cases
6. [ ] `complex64/128` → remove or stub
7. [ ] `uintptr` → explicit pointers (or add `_ "unsafe"`)
8. [ ] `import "strings"` → `import "bytes"` (optional but preferred)
9. [ ] Build: `moxie build ./...`
10. [ ] Run tests if applicable

## Type Switch Deduplication

With `int` = `int32` and `uint` = `uint32`, type switches with both cases are duplicates:

```go
// Go — both cases exist
case int:
    handleInt(v)
case int32:
    handleInt32(v)

// Moxie — keep only one (they're the same type)
case int:
    handleInt(v)
```

Same for constraint unions:
```go
// Go
type Signed interface { ~int | ~int8 | ~int16 | ~int32 | ~int64 }

// Moxie — remove int32 (duplicate of int)
type Signed interface { ~int | ~int8 | ~int16 | ~int64 }
```

## Build and Test

```bash
# Build the compiler (needed once, uses patched GOROOT for string=[]byte)
./build.sh

# Set environment
export MOXIEROOT=/path/to/moxie
export PATH=$MOXIEROOT:$PATH

# Build your code
moxie build -o myapp ./cmd/myapp/

# Run tests
moxie test ./pkg/...
```
