---
name: moxie
description: This skill should be used when writing, debugging, reviewing, or discussing Moxie language code (.mx files). Moxie is a compiled systems language descending from TinyGo with .mx source files, moxie.mod modules, no goroutines, no GC, explicit-width integers, string=[]byte unification, pipe operator for concatenation, spawn for process isolation, and channels+select for event dispatch.
---

# Moxie Language Expert

## When to Use

Activate when:
- Writing or editing `.mx` files
- Debugging Moxie programs
- Discussing Moxie language features, restrictions, or patterns
- Working with `moxie build`, `moxie fetch`, or related tooling
- Porting Go code to Moxie

## Critical Compile Errors (memorize these)

| What you write | Error | Fix |
|----------------|-------|-----|
| `int` or `uint` | illegal type | `int32`, `int64`, `uint32`, `uint64` |
| `"a" + "b"` | no `+` on text | `"a" \| "b"` (pipe operator) |
| `go f()` | no goroutines | channels + `select`, or `spawn` |
| `make(T, ...)` | removed | `[]T{:n}`, `chan T{}`, `map[K]V{}` |
| `new(T)` | removed | `&T{}` |
| `interface{}` / `any` | banned | named interface with methods |
| `append(...)` | removed | `push(s, v...)` or `a \| b` for concat |
| `import "strings"` | removed | `import "bytes"` (string=[]byte) |
| `fallthrough` | removed | `case A, B:` comma-separated |
| value receiver | banned | pointer receivers only |
| unnamed returns | banned | all return values must be named |
| `var x = expr` at pkg scope | banned | `var x T` + assign in `init()` |
| scope shadowing | banned | no redeclaration of visible names |
| named slice/map types | banned | `type S []T` is a compile error |

## Core Type Rules

- `string` and `[]byte` are the **same type**. Identical layout, mutually assignable.
- `len()` and `cap()` return `int32`. Array/slice indexes are `int32`.
- All builtins (`int32`, `bool`, `float64`, etc.) have `String() string` methods.
- `fmt` variadics use `...Stringer`, not `...any`. No reflection.
- `rune` is `int32`. `rune.String()` returns hex (`"0x0061"`), not UTF-8.
- Slice equality: `[]T` supports `==`/`!=` when `T` is comparable.

## Execution Model

- Each program is a tree of **domains** (OS processes / WASM Workers).
- One thread per domain. No data races possible.
- **Within domain**: channels + `select` for event dispatch.
- **Between domains**: `spawn(fn, args...)` creates child with IPC channels.
- No shared memory between domains. All cross-domain data serialized via `moxie.Codec`.

## The Pipe Operator `|`

- On integers: bitwise OR (standard).
- On strings/slices: **concatenation**. Returns new slice with elements from both.
- `|=` is append-assign: `s |= " suffix"`.
- `...` is variadic params only, never spread. Use `a | b` for slice concat.

## Literal Syntax (replaces make)

```moxie
[]T{:n}           // slice length n
[]T{:n:c}         // slice length n, capacity c
chan T{}           // unbuffered channel
chan T{n}          // buffered channel, capacity n
map[K]V{}         // empty map
```

## Memory Management

No GC. Per-function arena stack. Each function gets its own arena via `FnArenaPush`.
- Function entry pushes a fresh arena; return relocates results to caller's arena, then pops+releases.
- No individual free. Reclamation is bulk: arena release per function, munmap per domain.
- Arena pool (8 entries) recycles arenas without syscalls.
- Function params are borrowed references into caller's arena.
- `spawn` moves non-constant values (use-after-spawn = compile error).

## spawn Boundary

```moxie
done := spawn(fn, arg1, arg2)  // returns chan struct{}
```

- First arg must be a static function name (not closure).
- All data args must implement `moxie.Codec`.
- Raw types rejected - use `moxie.Int32`, `moxie.Bool`, etc.
- `*T` ok when T implements Codec (copy semantics).
- Channels become IPC. Element types must implement Codec.
- Functions, interfaces, raw pointers to non-Codec types rejected.

## Select (Event Dispatch)

```moxie
for {
    select {
    case req := <-requests:
        handle(req)
    case <-quit:
        return
    }
}
```

Cases checked top-to-bottom. First ready wins (deterministic, not random).

## Function Signatures

```moxie
func add(x, y int32) (sum int32) { return x + y }
func (p *Point) Move(dx float64) { p.X += dx }  // pointer receiver only
```

## Build Commands

```sh
moxie build -o out .              # native binary
moxie build -target js/wasm -o dir/ pkg/  # WASM
moxie fetch                       # install all deps from moxie.mod
moxie install git.example.com/pkg # install single dep
```

Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `js/wasm`.

## Reference Materials

For detailed information, consult:

- **references/type-system.md** - Numeric types, string=[]byte, builtin stringers, slice equality, comparable, named type restrictions
- **references/execution-model.md** - Domains, spawn, IPC channels, Codec types, move semantics, cooperative scheduler
- **references/memory-management.md** - Per-function arena stack, FnArenaPush/Pop, relocate on return, domain isolation
- **references/syntax-quick-ref.md** - Operators, literals, statements, keywords, control flow, all removed constructs
- **references/build-and-modules.md** - Build commands, module file, targets, cache, dependencies, .mxh headers
- **references/wasm-target.md** - String pragma, JS bridge packages, jsruntime shims, crypto
- **references/formatting.md** - moxiefmt rules, 80-column single-line rule, tab indentation, vertical alignment
