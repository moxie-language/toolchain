# EXAMPLE_CLAUDE.md — Moxie Project Template

Drop this into your project's `CLAUDE.md` (or `.claude/CLAUDE.md`) so that Claude writes correct Moxie code. Adjust the project-specific sections to match your codebase.

---

````markdown
# CLAUDE.md

## Language: Moxie

This project uses Moxie — a compiled systems language for domain-isolated programs. Moxie descends from TinyGo but is its own language with `.mx` source files, `moxie.mod` modules, and a restricted execution model.

**Read [REFERENCE.md](REFERENCE.md) before writing any code.**

## Hard Rules

These are compile errors. Violating them wastes a build cycle.

### Type System
- `string` and `[]byte` are the **same type**. Identical 3-word layout (ptr, len, cap). Mutually assignable. Use `string` in signatures for readability.
- `int` and `uint` are always **32-bit**. `len()` and `cap()` return `int32`.
- `range` over text yields **bytes**, not runes.

### Forbidden Constructs
| Construct | Compile Error | Use Instead |
|-----------|---------------|-------------|
| `go f()` | No goroutines | Channels + `select` for dispatch, `spawn` for isolation |
| `append(...)` | Removed | `push(s, v...)` for element insertion, `a \| b` for concat |
| `make(T, ...)` | Removed | Literal syntax: `[]T{:n}`, `chan T{}`, `map[K]V{}` |
| `new(T)` | Removed | `&T{}` or `var x T; p := &x` |
| `"a" + "b"` | No `+` on text | `"a" \| "b"` (pipe operator) |
| `fallthrough` | Removed | `case A, B:` comma-separated |
| `complex64/128` | Removed | Not supported |
| `uintptr` | Removed | Explicit pointers. Allowed with `import "unsafe"` |
| `import "strings"` | Removed | `import "bytes"` (they're the same type) |

### Spawn Boundary
- All data arguments to `spawn` must implement `moxie.Codec`.
- All channel element types must implement `moxie.Codec`.
- Raw types (`int32`, `bool`, etc.) are **rejected**. Use `moxie.Int32`, `moxie.Bool`, etc.
- Pointers, functions, and all interface types are **rejected**.
- Non-constant values passed to `spawn` cannot be used afterward (move semantics).
- Every channel must have both a sender and a listener/select.

### Literal Syntax
| Moxie | Equivalent |
|-------|-----------|
| `[]T{:n}` | `make([]T, n)` |
| `[]T{:n:c}` | `make([]T, n, c)` |
| `chan T{}` | `make(chan T)` |
| `chan T{n}` | `make(chan T, n)` |

`make()` is a compile error in user code. Use literal syntax only.

## Execution Model

- Each Moxie program is a tree of **domains**. Each domain is an OS process with one thread.
- There are **no goroutines**. The `go` keyword is a compile error.
- Concurrency within a domain: **channels + select** as event dispatch.
- Concurrency between domains: **spawn** creates child processes with IPC channels.
- No shared memory between domains. Communication is serialized via `moxie.Codec`.
- No data races possible (single-threaded per domain).

## Concurrency Patterns

### Event Loop (the fundamental pattern)
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

### Fan-Out with Spawn
```moxie
import "moxie"

func worker(id moxie.Int32, results chan moxie.Int32) {
    results <- moxie.Int32(int32(id) * int32(id))
}

func main() {
    results := chan moxie.Int32{}
    for i := int32(0); i < 4; i++ {
        spawn(worker, moxie.Int32(i), results)
    }
    for i := 0; i < 4; i++ {
        println(<-results)
    }
}
```

## Codec Types Reference

Built-in types in the `moxie` package for spawn boundary serialization:

| Type | Size | Big-endian alias |
|------|------|-----------------|
| `Bool`, `Int8`, `Uint8` | 1 byte | — |
| `Int16`, `Uint16` | 2 bytes | `BigInt16`, `BigUint16` |
| `Int32`, `Uint32` | 4 bytes | `BigInt32`, `BigUint32` |
| `Int64`, `Uint64` | 8 bytes | `BigInt64`, `BigUint64` |
| `Float32` | 4 bytes | `BigFloat32` |
| `Float64` | 8 bytes | `BigFloat64` |
| `Bytes` | variable | — (4-byte LE length prefix + data) |

Default encoding is little-endian. Use `Big*` variants for network protocols (Bitcoin, etc.).

Custom Codec: implement `EncodeTo(w io.Writer) error` and `DecodeFrom(r io.Reader) error`.

## Build

```sh
export MOXIEROOT=/path/to/moxie
moxie build -o myapp .
```

Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `js/wasm`.

JS target: `moxie build -target js/wasm -o output/ .`

## Dependencies

ALLOWED:
- Moxie standard library
- Packages already in moxie.mod / go.mod

FORBIDDEN without approval:
- Any new dependency not already in the module file
- Any npm/node ecosystem tool
- Any JS framework

Before writing any import, check the module file. If the import doesn't exist in the project, ask.

## Change Discipline

- Small commits, one feature at a time
- Verify compile AND run before moving on
- Re-read modified files after changes to catch drift
- When fixing a bug, understand the root cause before writing code
- Do not refactor adjacent code while fixing a bug
- Do not create test files unless explicitly asked
- Do not introduce abstractions "for future flexibility"
- Do not modify build configuration without approval

## Code Style

- Direct, minimal, no abstraction for abstraction's sake
- If a function does one thing and is called once, inline it
- Concise variable names
- Comments only where the why isn't obvious from the what
- No boilerplate, scaffolding, or template code "you might need later"
- Prefer explicit over clever
- Prefer flat over nested
- Prefer one file over many files when the logic is related

## Architecture Reading

Before writing code in a new session:
1. Read moxie.mod/go.mod to understand dependencies
2. Read the directory structure to understand layout
3. Read the file you're about to modify, completely
4. Check imports and calling patterns before assuming interfaces

Do not assume project structure from convention. Read the actual code.

## What NOT To Do

- Never add dependencies without explicit approval
- Never refactor working code that wasn't asked about
- Never introduce abstractions for future flexibility
- Never use frameworks when stdlib suffices
- Never explain code you're about to write — just write it
- Never summarize changes after making them unless asked
- Never create files that weren't requested
- Never modify build/dependency manifests without approval
- Never assume the tech stack — read the project first
- Never optimize before it works
- Never use `go` keyword (it's a compile error)
- Never use `append()` (use `push(s, v...)`)
- Never use `+` for text concatenation (use `|`)
- Never use `make()` (use literal syntax)
- Never use `new(T)` (use `&T{}`)
- Never pass raw types across spawn boundaries (use moxie.Codec types)
````
