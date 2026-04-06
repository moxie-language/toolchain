# The Moxie Language Reference

Moxie is a compiled systems language for domain-isolated event-driven programs. Programs compile to static native binaries (Linux, Darwin) or JavaScript (browser). Moxie descends from TinyGo; its syntax is a restricted subset of Go's, diverging where domain isolation, type unification, or the single-threaded execution model require it.

This document specifies the Moxie language. Familiarity with C-family syntax is assumed.

---

## Source Files

Source files use the `.mx` extension and are UTF-8 encoded. Each file begins with a `package` declaration. If both `file.mx` and `file.go` exist in the same directory, the `.mx` file takes precedence.

Module manifests use `moxie.mod` (preferred) or `go.mod`.

---

## Lexical Elements

### Keywords

```
break    case     chan     const    continue
default  defer    else    for      func
goto     if       import  interface map
package  range    return  select   struct
switch   type     var
```

The following keywords from Go are **compile errors** in Moxie:

| Keyword | Error |
|---------|-------|
| `fallthrough` | Each case must be self-contained. Use comma-separated case expressions. |
| `go` | There are no goroutines. Use channels + `select` for event dispatch, `spawn` for process-level parallelism. |

### Operators and Punctuation

All standard operators apply, with one addition:

| Operator | Context | Meaning |
|----------|---------|---------|
| `\|` | `[]T \| []T` | Slice concatenation. Returns a new slice containing elements from both operands. |

The `+` operator on `string` or `[]byte` values is a **compile error**. Use `|` for text concatenation:

```moxie
msg := "hello " | name | "!"
```

### Literals

String literals (`"hello"`, `` `raw` ``) produce `[]byte` values. Complex literals (`1+2i`) do not exist.

#### Slice Size Literals

```moxie
buf := []byte{:1024}        // make([]byte, 1024)
table := []int32{:0:100}    // make([]int32, 0, 100)
```

The leading `:` after `{` distinguishes size literals from composite literals.

#### Channel Literals

```moxie
ch := chan int32{}           // make(chan int32)       — unbuffered
ch := chan int32{10}         // make(chan int32, 10)   — buffered
```

These replace `make()` which is not available in user code.

---

## Types

### Boolean

```
bool
```

Two values: `true` and `false`.

### Numeric Types

```
int8    int16    int32    int64
uint8   uint16   uint32   uint64
float32 float64
byte                              // alias for uint8
int     uint                      // 32-bit on all targets
```

`int` and `uint` are always 32-bit. `len()` and `cap()` return `int32`.

The following types do **not exist**:

| Removed | Reason |
|---------|--------|
| `complex64`, `complex128` | Complex numbers not supported. |
| `uintptr` | Use explicit pointer types. Allowed in packages that `import "unsafe"`. |

### Text Type

`string` and `[]byte` are the **same type**. They have identical runtime layout (pointer, length, capacity), are mutually assignable, and are interchangeable everywhere.

```moxie
name := "hello"           // type is string (= []byte)
var s string = []byte{72} // direct assignment, no conversion
b := []byte("world")      // no-op — same layout
s = b                     // no copy
```

There is no immutable string type. All text is a mutable byte slice. The keyword `string` is retained because it reads better in signatures:

```moxie
func greet(name string) string {   // preferred
    return "hello " | name
}
```

UTF-8 encoding is a library concern. `range` over text yields **bytes**, not runes. Use an encoding library for rune-level iteration.

### Array Types

Fixed-size, value-type. Identical to standard array semantics.

```moxie
var a [4]int32
```

### Slice Types

Dynamic-length view over a backing array.

#### Slice Equality

Any slice of a comparable element type supports `==` and `!=`. Two slices are equal if they have the same length and identical elements:

```moxie
a := []int32{1, 2, 3}
b := []int32{1, 2, 3}
println(a == b)            // true

m := map[[]byte]int32{}    // slices can be map keys
m[[]byte("key")] = 42
```

#### Slice Concatenation

The `|` operator concatenates any two slices of the same type, returning a fresh slice:

```moxie
a := []int32{1, 2, 3}
b := []int32{4, 5}
c := a | b                 // []int32{1, 2, 3, 4, 5}

s := "hello" | " world"   // works on text too
s |= "!"                  // append-assign
```

`|` always allocates a new backing array. The operands are not modified.

### Struct Types

Identical to standard struct semantics. Embedding, tags, and anonymous fields work as expected.

### Pointer Types

Pointers work as expected. The `&` operator takes the address of any addressable value. `new(T)` does not exist — use `&T{}` or `var` instead.

```moxie
p := &MyStruct{X: 1}      // allocates and returns *MyStruct
```

### Function Types

First-class values. Closures, variadic parameters, and multiple return values work as expected.

### Interface Types

Method sets, type assertions, type switches, and dynamic dispatch work as expected. `any` is retained as an alias for `interface{}`.

All interface types are restricted at the `spawn` boundary. See [Spawn Boundary Rules](#spawn-boundary-rules).

### Map Types

Hash maps with arbitrary key and value types. Keys must be comparable.

### Channel Types

```
chan T          // bidirectional
chan<- T        // send-only
<-chan T        // receive-only
```

Channels are the primary synchronization mechanism within a domain. Channels passed to `spawn` become IPC channels over Unix socketpairs (native) or MessagePorts (JS).

---

## Declarations and Scope

Variable, constant, type, and function declarations follow standard syntax. Short variable declarations (`:=`) work as expected.

`new(T)` is a **compile error**. Use composite literals with `&` or `var` declarations:

```moxie
p := &MyStruct{Field: value}   // instead of new(MyStruct)
```

---

## Expressions

All standard expressions work, with these exceptions:

- `new(T)` is a compile error.
- `complex()`, `real()`, `imag()` are compile errors.
- `+` on `string`/`[]byte` is a compile error. Use `|`.
- `|` on matching slice types performs concatenation.

---

## Statements

### Switch

The `fallthrough` statement is a **compile error**. Each case must be self-contained. Use comma-separated expressions to share logic:

```moxie
switch x {
case 1, 2:
    doOneOrTwo()
case 3:
    doThree()
}
```

### Select

`select` is the event handler. It waits for channel messages, I/O events, and timer expirations. All concurrency within a domain reduces to `select`:

```moxie
select {
case v := <-ch1:
    handle(v)
case ch2 <- x:
    // sent
default:
    // no channel ready
}
```

All other statements (`defer`, `for`, `if`, `return`, `break`, `continue`, `goto`, `send`, `assign`) work as expected.

---

## Built-in Functions

### Available

| Function | Behavior |
|----------|----------|
| `append` | Appends elements to a slice. |
| `cap` | Returns capacity of a slice or channel. |
| `close` | Closes a channel. |
| `copy` | Copies elements between slices. Returns count copied. |
| `delete` | Deletes a key from a map. |
| `len` | Returns length of a string, slice, array, map, or channel. |
| `make` | Allocates and initializes slices, maps, and channels. |
| `panic` | Stops execution with an error value. |
| `print` | Prints to stderr (no newline). |
| `println` | Prints to stderr (with newline). |
| `recover` | Catches a panic in a deferred function. |
| `spawn` | Creates a new isolated domain. See [spawn](#spawn). |

### Removed (compile errors)

| Function | Alternative |
|----------|-------------|
| `new` | `&T{}` or `var x T; p := &x` |
| `complex` | Not supported. |
| `real` | Not supported. |
| `imag` | Not supported. |

---

## spawn

```moxie
done := spawn(fn, arg1, arg2, ...)
```

`spawn` creates a new **domain** — an isolated execution context with its own single-threaded event loop, heap, and garbage collector. On native targets, the domain is an OS process created via `fork()`. On the JS target, the domain is a Web Worker.

`spawn` is a language builtin. No import is required.

### Arguments

The first argument must be a **static function name** (not a variable). Remaining arguments are passed to that function in the child domain. The return value is `chan struct{}` — a lifecycle channel that closes when the child exits.

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

The spawn boundary is a serialization boundary. Every argument is serialized into the child's address space via `moxie.Codec`. There is no shared memory between domains.

All data arguments and channel element types must implement `moxie.Codec`:

```moxie
import "moxie"

type Codec interface {
    EncodeTo(w io.Writer) error
    DecodeFrom(r io.Reader) error
}
```

#### Built-in Codec Types

The `moxie` package provides codec wrappers for all primitive types. All encode **little-endian** by default (matching x86-64 and ARM64 hardware). Big-endian aliases exist for network protocols:

| Default (LE) | Big-endian (BE) | Size |
|-------------|-----------------|------|
| `moxie.Bool`, `moxie.Int8`, `moxie.Uint8` | — | 1 byte |
| `moxie.Int16`, `moxie.Uint16` | `moxie.BigInt16`, `moxie.BigUint16` | 2 bytes |
| `moxie.Int32`, `moxie.Uint32` | `moxie.BigInt32`, `moxie.BigUint32` | 4 bytes |
| `moxie.Int64`, `moxie.Uint64` | `moxie.BigInt64`, `moxie.BigUint64` | 8 bytes |
| `moxie.Float32` | `moxie.BigFloat32` | 4 bytes |
| `moxie.Float64` | `moxie.BigFloat64` | 8 bytes |
| `moxie.Bytes` | — | 4-byte LE length prefix + data |

User-defined structs implement `Codec` by defining `EncodeTo`/`DecodeFrom` methods that serialize each field.

#### Allowed Types

| Type | Treatment |
|------|-----------|
| Types implementing `moxie.Codec` | Serialized via EncodeTo/DecodeFrom. |
| Channels (element type must implement Codec) | Become IPC channels over socketpair. |
| Structs with Codec methods | User-defined serialization. |

#### Rejected Types (compile error)

| Type | Reason |
|------|--------|
| Raw built-in types (`int32`, `bool`, etc.) | Use `moxie.Int32`, `moxie.Bool`, etc. |
| `*T` (pointer) | Cannot share memory across process boundary. |
| `func(...)` | Cannot serialize code. |
| `interface{}` / any interface type | Type erasure prevents serialization. |

### Move Semantics

Non-constant values passed to `spawn` are **moved** to the child domain. Using the variable after `spawn` is a compile error:

```moxie
x := compute()
spawn(worker, x)
println(x)        // compile error: variable used after spawn
```

**Exceptions:**
- Constants and constant-foldable expressions are exempt (immutable).
- Channels are exempt (both parent and child hold IPC endpoints).

### Channel Completeness

Every channel created in a function must have both a sender and a listener (receive or select case). Channels that escape the function (passed to calls, returned, stored) are exempt.

```moxie
ch := chan int32{}
ch <- 42           // compile error: channel has no listener
```

### IPC Channels

Channels passed to `spawn` become IPC channels. The runtime multiplexes them over a single socketpair with length-prefixed messages:

```moxie
import "moxie"

func worker(results chan moxie.Int32) {
    results <- moxie.Int32(compute())
}

func main() {
    results := chan moxie.Int32{}
    spawn(worker, results)
    v := <-results
    println(v)
}
```

### Native Implementation

On Linux and Darwin:
1. `socketpair(AF_UNIX, SOCK_STREAM)` creates a bidirectional IPC pipe.
2. `fork()` creates the child process (copy-on-write memory).
3. **Child:** closes parent's socket end, runs the function, exits on completion.
4. **Parent:** closes child's socket end, registers domain (pid + fd), continues.

Each domain has its own single-threaded event loop, channel instances, and Boehm GC heap.

### JS Implementation

On the JS target:
1. `$rt.domain.spawn(async () => fn(args))` creates an isolated microtask context.
2. Slices are spread-copied (`[...arr]`), maps are `Object.assign`'d.
3. Each domain gets its own event loop via `queueMicrotask`.
4. IPC uses `BroadcastChannel` for inter-domain messaging.

---

## Packages

The `import` declaration, package paths, and `init` functions work as expected.

The `moxie` package is force-imported by the compiler (like `runtime`). User code imports `moxie` explicitly when using Codec types at spawn boundaries. The `spawn` builtin requires no import.

### Import Restrictions

| Import | Error |
|--------|-------|
| `"strings"` | Use `"bytes"` instead. With `string = []byte`, they are functionally identical. |

---

## Execution Model

### Domains

A Moxie program is a tree of **domains**. Each domain is an OS process (native) or Worker (JS) running a single thread. There are no goroutines.

```
┌─────────────────────────────────────┐
│              Program                │
│  ┌──────────┐     ┌──────────┐     │
│  │ Domain 0  │────│ Domain 1  │     │
│  │ (parent)  │ IPC│ (child)   │     │
│  │           │sock│           │     │
│  │  select   │pair│  select   │     │
│  │  ├ ch1    │    │  ├ ch3    │     │
│  │  ├ ch2    │    │  └ timer  │     │
│  │  └ I/O    │    │           │     │
│  └───────────┘    └───────────┘     │
│  single thread    single thread     │
└─────────────────────────────────────┘
```

### Concurrency Within a Domain

There are no goroutines. Execution flow within a domain is controlled by three constructs:

| Construct | Behavior |
|-----------|----------|
| Unbuffered channel send | Execution transfers to the waiting `select` case. Like a function call via the channel. |
| Buffered channel send | Message queued for the next `select` iteration. |
| `select` | Event handler — blocks until a channel message, I/O event, or timer fires. |

Because there is exactly one thread per domain, **there are no data races**. Mutexes are no-ops. Atomics are plain loads and stores.

### Concurrency Between Domains

`spawn` creates child domains. Communication happens exclusively through IPC channels. Values are deep-copied across the boundary via Codec serialization. The parent and child cannot observe each other's heap.

This is not a relaxed memory model — it is **no shared memory**. The answer to "when does domain A see domain B's write?" is: when B sends it on a channel and A receives it.

### I/O Model

Each domain blocks on `epoll` (Linux) or `kqueue` (Darwin) until I/O events arrive. The `select` statement multiplexes channel messages, I/O readiness, and timer expirations into a single event loop.

---

## Memory Model

### Within a Domain

Single-threaded. Sequential consistency by construction. No memory barriers, no fences, no happens-before complexity.

### Between Domains

Complete isolation. No shared memory. Communication through IPC channels only. Each domain has its own Boehm conservative GC heap.

---

## Runtime

The runtime provides:

- **Cooperative task scheduling**: FIFO round-robin for internal runtime tasks (e.g., deferred I/O callbacks). No user-visible goroutines.
- **Boehm GC**: Conservative mark-and-sweep garbage collection per domain.
- **Channel dispatch**: Lock-free select with atomic CAS for fairness. Unbuffered channels transfer execution directly. Buffered channels use a circular ring buffer.
- **I/O polling**: `epoll`/`kqueue` integration. I/O waits yield to the event loop, not blocking the domain.
- **Context switching**: Cooperative stack switching via saved register sets (AMD64: rbx, rbp, r12-r15; ARM64: x19-x28, d8-d15). Stack overflow detected via canary values.

---

## Targets

| GOOS/GOARCH | Output | GC | Libc |
|-------------|--------|-----|------|
| linux/amd64 | Static ELF | Boehm | musl |
| linux/arm64 | Static ELF | Boehm | musl |
| darwin/amd64 | Mach-O | Boehm | libSystem |
| darwin/arm64 | Mach-O | Boehm | libSystem |
| js/wasm | JavaScript + runtime | Browser GC | — |

All native binaries are fully statically linked. No dynamic library dependencies at runtime.

The JavaScript target outputs ES modules with a runtime library. Each domain maps to a `BroadcastChannel`-isolated context. Browser APIs (DOM, WebSocket, IndexedDB, Service Workers) are accessible via bridge packages.

### JS Bridge Packages

The JS target provides typed bridges to browser APIs:

| Bridge | Purpose |
|--------|---------|
| `dom` | Element creation, tree manipulation, events |
| `ws` | WebSocket dial/send/close |
| `sw` | Service Worker lifecycle, fetch/cache, SSE |
| `localstorage` | localStorage operations |
| `idb` | IndexedDB transactions |
| `crypto` | secp256k1 via WASM |
| `subtle` | SubtleCrypto bridge |

---

## Build Tags

The following build tags are always set:

```
moxie              // Moxie compiler
moxie.unicore      // Single-core cooperative
gc.boehm           // Boehm GC (native targets)
scheduler.none     // No goroutine scheduler
```

Standard platform tags (`linux`, `darwin`, `amd64`, `arm64`, `js`) work as expected.

---

## Building

### Prerequisites

- Go 1.25+
- LLVM 19 (clang-19, lld-19)

### Build the Compiler

```sh
./build.sh
```

Creates a patched GOROOT (for type unification), then builds the compiler with LLVM 19 linking. Produces a `./moxie` binary.

### Build a Program

```sh
export MOXIEROOT=/path/to/moxie
moxie build -o hello .
./hello
```

### CLI Reference

| Command | Purpose |
|---------|---------|
| `moxie build` | Compile to binary |
| `moxie run` | Compile and execute |
| `moxie test` | Run tests |
| `moxie clean` | Clear build cache |
| `moxie targets` | List supported targets |
| `moxie info` | Display target configuration |

Key flags: `-o output`, `-opt [0\|1\|2\|s\|z]`, `-gc [none\|leaking\|conservative\|boehm]`, `-target [linux/amd64\|js/wasm]`, `-stack-size N`.

---

## Runtime Guarantees

1. **Single-threaded per domain.** No goroutines, no preemption, no task switching.
2. **No data races.** Single-threaded execution makes races structurally impossible.
3. **Complete domain isolation.** `fork()` provides OS-level memory isolation.
4. **Deterministic execution.** Given the same inputs, execution follows one path.
5. **Static binaries.** No dynamic dependencies. Ship one file.

---

## Quick Reference

| Feature | Moxie |
|---------|-------|
| File extension | `.mx` |
| Module file | `moxie.mod` (or `go.mod`) |
| Text type | `string = []byte`. Mutable. `string` preferred for readability. |
| Text concatenation | `\|` operator. `+` is a compile error. |
| Integer sizes | `int` = `uint` = 32-bit on all targets. |
| Goroutines | Do not exist. `go` is a compile error. |
| Concurrency | Channels + `select` within a domain. `spawn` for new domains. |
| Process isolation | `spawn(fn, args...)` via `fork()` + socketpair. |
| Serialization | `moxie.Codec` interface required at spawn boundary. |
| Memory model | No shared memory between domains. |
| GC | Boehm conservative, per-domain heap. |
| Slice equality | `==` works for any comparable element type. |
| Slice literals | `[]T{:n}` (length n). `[]T{:n:c}` (length n, capacity c). |
| Channel literals | `chan T{}` (unbuffered). `chan T{n}` (buffered, capacity n). |
| `make()` | Compile error. Use literal syntax. |
| `new(T)` | Compile error. Use `&T{}`. |
| `fallthrough` | Compile error. Use `case A, B:`. |
| `import "strings"` | Compile error. Use `"bytes"`. |
| Interface at spawn | Compile error. No interface crosses the spawn boundary. |
| Targets | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, js/wasm |
