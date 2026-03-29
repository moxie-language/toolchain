# The Moxie Language Reference

Moxie is a restricted subset of Go designed for domain-isolated cooperative concurrency. Programs compile to static native binaries (Linux, Darwin) or JavaScript (browser, Node.js).

This document specifies where Moxie diverges from the [Go specification](https://go.dev/ref/spec). Anything not mentioned here follows Go's rules exactly.

---

## Notation

Productions use Go spec EBNF. Moxie uses Go's parser and type checker unmodified — restrictions are enforced after SSA construction.

---

## Source Code Representation

Identical to Go. UTF-8 encoded. `.go` file extension. Standard `package` declarations.

---

## Lexical Elements

### Keywords

Moxie recognizes all Go keywords. The following have restricted or altered semantics:

| Keyword | Status |
|---------|--------|
| `fallthrough` | **Compile error.** Each case must be self-contained. |
| `go` | Starts a goroutine within the current domain. |

All other keywords (`break`, `case`, `chan`, `const`, `continue`, `default`, `defer`, `else`, `for`, `func`, `goto`, `if`, `import`, `interface`, `map`, `package`, `range`, `return`, `select`, `struct`, `switch`, `type`, `var`) behave identically to Go.

### Operators and Punctuation

Identical to Go.

### Literals

Identical to Go, with these exceptions:

- Complex literals (`1+2i`) are not supported since the complex types do not exist.
- String literals (`"hello"`, `` `raw` ``) produce `[]byte` values, not `string`. See [Text Type](#text-type).

---

## Types

### Boolean Type

```
bool
```

Identical to Go.

### Numeric Types

```
int8    int16    int32    int64
uint8   uint16   uint32   uint64
float32 float64
byte    // alias for uint8
int     uint     // platform-sized, as in Go
```

The following numeric types do **not exist** in Moxie:

| Removed | Reason |
|---------|--------|
| `complex64` | Complex numbers not supported. Use separate float fields. |
| `complex128` | Complex numbers not supported. |
| `uintptr` | Use explicit pointer types. Raw integer-pointer conversion is unsafe. |

Note: `rune` is not a language-level type. Unicode code point encoding and decoding is a library concern. Use `int32` for numeric code point values and an encoding library for UTF-8 operations.

### Text Type

There is no `string` type. Text is `[]byte`.

String literals produce `[]byte` values:

```go
name := "hello"          // name has type []byte
raw := `raw\nstring`     // raw has type []byte
```

To get a pointer: `&[]byte` works like any other address-of expression:

```go
var greeting []byte = "hello"
p := &greeting            // p has type *[]byte
```

UTF-8 encoding and rune iteration are library operations, not language primitives. There is no implicit `range` over runes — ranging over `[]byte` yields bytes.

### Array Types

```
ArrayType = "[" ArrayLength "]" ElementType .
```

Identical to Go. Fixed-size, value-type.

### Slice Types

```
SliceType = "[" "]" ElementType .
```

Identical to Go. Dynamic-length view over a backing array.

### Struct Types

```
StructType = "struct" "{" { FieldDecl ";" } "}" .
```

Identical to Go. Embedding, tags, and anonymous fields work as in Go.

### Pointer Types

```
PointerType = "*" BaseType .
```

Pointers work identically to Go. The `&` operator takes the address of any addressable value. `new(T)` does not exist — use `&` instead.

```go
var x int32 = 42
p := &x                  // p has type *int32

s := &MyStruct{X: 1}     // allocates and returns *MyStruct

arr := &[3]int32{1,2,3}  // *[3]int32
```

Dereferencing (`*p`), nil checks, and pointer comparison work as in Go.

### Function Types

```
FunctionType = "func" Signature .
```

Identical to Go.

### Interface Types

```
InterfaceType = "interface" "{" { MethodSpec ";" } "}" .
```

Identical to Go. Type assertions, type switches, and method dispatch work as in Go.

`any` is retained as an alias for `interface{}`.

Note: all interface types (including `any`) are restricted at the spawn boundary. See [Spawn Boundary Rules](#spawn-boundary-rules).

### Map Types

```
MapType = "map" "[" KeyType "]" ElementType .
```

Identical to Go.

### Channel Types

```
ChannelType = ( "chan" | "chan" "<-" | "<-" "chan" ) ElementType .
```

Identical to Go. Channels are the primary synchronization mechanism within a domain. Channels passed to `spawn` become IPC channels backed by a socketpair (native) or MessagePort (JS).

---

## Declarations and Scope

Identical to Go, with the exception that `new(T)` is not available. Use `var` declarations or composite literals with `&`:

```go
// Instead of: p := new(int)
var x int
p := &x

// Instead of: p := new(MyStruct)
p := &MyStruct{Field: value}
```

### Constant Declarations

Identical to Go. The predeclared `iota` works as in Go. Complex constants are not available.

### Type Declarations

Identical to Go.

### Variable Declarations

Identical to Go. Short variable declarations (`:=`) work as in Go.

### Function Declarations

Identical to Go. Methods, closures, and variadic functions work as in Go.

---

## Expressions

Identical to Go, with these exceptions:

- Complex arithmetic expressions are not available (no `complex`, `real`, `imag`).
- The `new(T)` expression is a compile error. Use `&T{}` or `var` instead.

All other expressions (arithmetic, comparison, logical, address, receive, type assertion, function calls, conversions) work identically to Go.

---

## Statements

Identical to Go, with one exception:

### Switch Statements

The `fallthrough` statement is a **compile error**. Each case clause must be self-contained.

```go
switch x {
case 1:
    doOne()
    // fallthrough ← compile error
case 2:
    doTwo()
}
```

To share logic between cases, use comma-separated expressions:

```go
switch x {
case 1, 2:
    doOneOrTwo()
case 3:
    doThree()
}
```

Or call a shared function:

```go
switch x {
case 1:
    shared()
    doOne()
case 2:
    shared()
    doTwo()
}
```

All other statements (`go`, `defer`, `select`, `for`, `if`, `return`, `break`, `continue`, `goto`, `send`, `assign`, `block`) work identically to Go.

---

## Built-in Functions

### Available Builtins

| Function | Behavior |
|----------|----------|
| `append` | Identical to Go. |
| `cap` | Identical to Go. |
| `close` | Identical to Go. |
| `copy` | Identical to Go. |
| `delete` | Identical to Go. |
| `len` | Identical to Go. |
| `make` | Identical to Go. |
| `panic` | Identical to Go. |
| `print` | Identical to Go. |
| `println` | Identical to Go. |
| `recover` | Identical to Go. |
| `spawn` | **Moxie builtin.** See [spawn](#spawn). |

### Removed Builtins

| Function | Compile Error |
|----------|---------------|
| `new` | Use `&T{}` or `var` declaration. |
| `complex` | Complex numbers not supported. |
| `real` | Complex numbers not supported. |
| `imag` | Complex numbers not supported. |

---

## spawn

```go
func spawn(fn FuncType, args ...T) chan struct{}
```

`spawn` creates a new **domain** — an isolated execution context with its own cooperative scheduler, heap, and goroutine run queue. On native targets, the domain is an OS process created via `fork()`. On JavaScript targets, the domain is a Web Worker or Service Worker.

`spawn` is a language builtin. No import is required.

### Syntax

```go
done := spawn(worker, arg1, arg2, ...)
```

The first argument must be a **static function name** (not a variable holding a function value). The remaining arguments are passed to that function in the child domain. The return value is a lifecycle channel (`chan struct{}`) that will close when the child domain exits.

The compiler verifies at compile time that:
- The number of arguments matches the function's parameter count exactly.
- The type of each argument matches the corresponding parameter type exactly.
- Every argument type is serializable across the spawn boundary.

```go
func worker(x int32, y float64) { ... }

spawn(worker, int32(1), float64(2.5))  // OK
spawn(worker, int32(1))                // compile error: expects 2 arguments, got 1
spawn(worker, 1, 2.5)                  // compile error: int != int32
spawn(worker, float64(1), int32(2))    // compile error: argument 1 has type float64, expects int32
```

### Example

```go
func producer(id int32, scale float64) {
    for i := int32(0); i < 10; i++ {
        println(id, float64(i)*scale)
    }
}

func main() {
    done := spawn(producer, int32(1), float64(2.5))
    _ = done
    println("parent continues immediately")
}
```

The parent does not block. The child runs concurrently as a separate process.

### Spawn Boundary Rules

The spawn boundary is a serialization boundary. Every argument is deep-copied into the child's address space. There is no shared memory between domains.

#### Allowed Types

| Type | Behavior |
|------|----------|
| `int8` .. `int64`, `uint8` .. `uint64` | Copied by value. |
| `float32`, `float64` | Copied by value. |
| `bool`, `byte` | Copied by value. |
| `[N]T` (array) | Copied by value. Element type must be allowed. |
| `struct{...}` | Copied by value. All fields must be allowed types. |
| `[]T` (slice) | Deep copied. Element type must be allowed. |
| `map[K]V` | Deep copied. Key and value types must be allowed. |
| `chan T` | **Top-level only.** Becomes an IPC channel. Element type must be allowed. |

#### Rejected Types (compile error)

| Type | Reason |
|------|--------|
| `*T` (pointer) | Cannot share memory across process boundary. |
| `func(...)` | Cannot serialize code pointers. |
| `interface{}` | Type erasure makes serialization ambiguous. |
| Any interface type | Same reason. All interface types are rejected. |
| Nested channel | Channels inside structs, arrays, slices, or maps are rejected. Channels must be top-level parameters. |

The validation is recursive: a struct containing a pointer field, or a slice of interfaces, or a map with function values, will all be rejected at compile time.

### Move Semantics

Non-constant values passed to `spawn` are **moved** to the child domain. Using the variable after the `spawn` call is a compile error:

```go
x := compute()
spawn(worker, x)
println(x)    // compile error: variable used after spawn
```

This prevents the programmer from assuming that modifications to `x` after spawn will be visible in the child. There is no shared memory.

**Exceptions:**
- Constants and constant-foldable expressions are exempt (they're immutable).
- Channels are exempt (both parent and child hold endpoints for communication).

```go
spawn(worker, int32(42))    // OK: constant
println(42)                  // OK: constant, no move

ch := make(chan int32)
spawn(reader, ch)
ch <- 99                     // OK: channel, exempt from move
```

### Native Implementation

On Linux and Darwin:

1. `socketpair(AF_UNIX, SOCK_STREAM)` creates a bidirectional IPC pipe.
2. `fork()` creates the child process (copy-on-write memory).
3. **Child:** closes parent's socket end, drains inherited run queue, starts the wrapper function as a cooperative task, runs the scheduler, exits when the task completes.
4. **Parent:** closes child's socket end, registers the domain (pid + fd), continues execution.

Each domain has its own:
- Cooperative scheduler and run queue
- Goroutines and channel instances
- Boehm GC heap
- Stack space (8 KB default per goroutine)

### Browser Implementation

In the browser:

1. The spawn call emits `$rt.domain.spawn(async () => fn(args))`.
2. The runtime creates an isolated microtask context backed by a BroadcastChannel.
3. Slices are spread-copied (`[...arr]`), maps are `Object.assign`'d — the child gets independent data.
4. Each domain gets its own cooperative goroutine scheduler via `queueMicrotask`.

---

## Packages

Identical to Go. The `import` declaration, package paths, and init functions work as in Go.

The `moxie` package path is reserved for compiler internals and should not be imported by user code. The `spawn` builtin is available without import.

---

## Concurrency

### Goroutines

The `go` statement works identically to Go:

```go
go func() {
    // runs concurrently within this domain
}()
```

Goroutines within a domain are cooperatively scheduled on a single thread. There is no preemption. Goroutines yield at:

- Channel send and receive operations
- `runtime.Gosched()` calls
- Blocking system calls (via the runtime's poll mechanism)

Because there is exactly one thread per domain, **there are no data races within a domain**. Mutexes are no-ops. Atomics are plain loads and stores. The Go race detector is inapplicable.

### Channels

Channel types and operations are identical to Go:

```go
ch := make(chan int32)      // unbuffered
ch := make(chan int32, 10)  // buffered

ch <- value                  // send
value := <-ch                // receive
value, ok := <-ch            // receive with closed check

close(ch)                    // close

select {                     // select
case v := <-ch1:
case ch2 <- x:
default:
}
```

#### IPC Channels

Channels passed to `spawn` become IPC channels. Sends serialize the value over a socketpair (native) or MessagePort (JS). Receives block the goroutine until data arrives from the other domain.

```go
func worker(results chan int32) {
    results <- compute()
}

func main() {
    results := make(chan int32)
    spawn(worker, results)
    v := <-results
    println(v)
}
```

The channel element type must be a serializable value type (no pointers, no interfaces, no functions, no nested channels).

---

## Memory Model

### Within a Domain

Single-threaded cooperative scheduling. All goroutines see the same consistent memory. No memory barriers, no fences, no happens-before complexity. A goroutine's writes are visible to all other goroutines in the same domain at the next yield point.

### Between Domains

Complete isolation. No shared memory. Communication happens exclusively through IPC channels. Values are deep-copied across the boundary. The parent and child cannot observe each other's heap.

This is not a relaxed memory model — it is no shared memory model. The question "when does domain A see domain B's write?" has one answer: when domain B sends it on a channel and domain A receives it.

---

## Targets

| GOOS/GOARCH | Output | Scheduler | GC | Libc |
|-------------|--------|-----------|-----|------|
| linux/amd64 | Static ELF | Cooperative | Boehm | musl |
| linux/arm64 | Static ELF | Cooperative | Boehm | musl |
| darwin/amd64 | Mach-O | Cooperative | Boehm | libSystem |
| darwin/arm64 | Mach-O | Cooperative | Boehm | libSystem |
| js/browser | JavaScript + runtime | Cooperative | Browser GC | — |

All native binaries are fully statically linked. No dynamic library dependencies. No libc dependency at runtime (musl is compiled in).

The JavaScript target runs in a browser environment only (Web Workers, BroadcastChannel). Node.js and Deno are not supported — if you can run a native binary, use one.

---

## Runtime Guarantees

1. **Single-threaded per domain.** `runtime.NumCPU()` always returns 1.
2. **No preemption.** A goroutine runs until it yields. Infinite loops without channel ops or `Gosched()` will starve other goroutines.
3. **No data races.** The cooperative scheduler makes races structurally impossible within a domain.
4. **Complete domain isolation.** `fork()` provides OS-level memory isolation. A bug in one domain cannot corrupt another domain's heap.
5. **Deterministic scheduling.** Given the same inputs and yield points, goroutines execute in the same order. The scheduler is FIFO.

---

## Build Tags

Moxie programs can use build tags. The following are always set:

```
moxie              // Moxie compiler (always true)
moxie.unicore      // Single-core cooperative (always true)
gc.boehm           // Boehm GC (native targets)
scheduler.tasks    // Cooperative scheduler (always true)
```

Standard Go build tags (`linux`, `darwin`, `amd64`, `arm64`, `js`) work as expected.

---

## Differences from Go: Summary

| Feature | Go | Moxie |
|---------|-----|-------|
| `new(T)` | Allocates and returns `*T` | **Compile error.** Use `&T{}`. |
| `string` | Immutable UTF-8 type | **Does not exist.** Text is `[]byte`. |
| `rune` | Alias for `int32` | Not a language type. Use `int32` + encoding library. |
| `any` | Alias for `interface{}` | **Same.** Retained as generic shorthand. |
| `complex64/128` | Complex number types | **Do not exist.** |
| `complex()`, `real()`, `imag()` | Complex builtins | **Compile error.** |
| `uintptr` | Integer type for pointers | **Does not exist.** |
| `fallthrough` | Falls through to next case | **Compile error.** |
| `go` | OS thread or M:N scheduled | Cooperative, single-threaded. |
| Goroutine scheduling | Preemptive (Go 1.14+) | Cooperative. Yields at channel ops. |
| `sync.Mutex` | Blocks thread | No-op. Single-threaded. |
| Memory model | Happens-before, memory fences | Single-threaded. Sequential consistency. |
| `spawn` | Does not exist | **Moxie builtin.** Creates isolated domain. |
| Concurrency model | Shared memory + locks | Domain isolation. No shared memory. |
| Binary output | Dynamically linked | Fully static. |
| Targets | All platforms | Linux, Darwin, browser JS. |
