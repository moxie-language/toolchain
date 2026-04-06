# Learning Moxie

A hands-on introduction. Each section builds on the last.

---

## 1. Hello Moxie

```moxie
package main

func main() {
    println("hello, moxie")
}
```

Save as `hello.mx`. Build and run:

```sh
export MOXIEROOT=/path/to/moxie
moxie build -o hello .
./hello
```

`.mx` files are Moxie source. The compiler produces a static binary with no dependencies.

---

## 2. Variables and Types

```moxie
package main

func main() {
    x := 42              // int (always 32-bit in Moxie)
    var y float64 = 3.14
    z := x + 1
    println(x, y, z)
}
```

Moxie has the same declaration syntax you'd expect: `var`, `:=`, `const`, `type`.

Numeric types: `int8`, `int16`, `int32`, `int64`, `uint8`-`uint64`, `float32`, `float64`, `byte`, `bool`.

`int` and `uint` are always 32-bit. There is no `complex64/128`. `uintptr` is only available in packages that `import "unsafe"`.

---

## 3. Text

`string` and `[]byte` are the **same type** in Moxie. They have identical layout (pointer, length, capacity) and are interchangeable. Use `string` in signatures for readability, `[]byte` when the byte-level nature matters.

```moxie
package main

import "bytes"

func main() {
    name := "moxie"
    greeting := "hello " | name | "!"   // | concatenates text
    println(greeting)

    // bytes package works on strings (they're []byte)
    upper := bytes.ToUpper(name)
    println(upper)

    // direct byte access
    println(name[0])    // 109 (ASCII 'm')
}
```

Key rules:
- `+` on text is a **compile error**. Use `|`.
- `import "strings"` is a compile error. Use `"bytes"`.
- `range` over text yields **bytes**, not runes. Use an encoding library for Unicode iteration.

---

## 4. Functions

```moxie
package main

func add(a, b int32) int32 {
    return a + b           // + on numbers is fine
}

func divide(a, b float64) (float64, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")
    }
    return a / b, nil
}

func main() {
    println(add(3, 4))
}
```

Multiple return values, variadic parameters, closures, and method receivers all work as expected.

---

## 5. Structs and Methods

```moxie
package main

type Point struct {
    X, Y float64
}

func (p Point) Dist() float64 {
    return p.X*p.X + p.Y*p.Y
}

func main() {
    p := Point{X: 3, Y: 4}
    println(p.Dist())       // 25
}
```

Allocate on the heap with `&`:

```moxie
    p := &Point{X: 1, Y: 2}    // *Point
```

There is no `new()`. Use `&T{}` or `var x T; p := &x`.

---

## 6. Interfaces

```moxie
package main

type Shape interface {
    Area() float64
}

type Rect struct {
    W, H float64
}

func (r Rect) Area() float64 {
    return r.W * r.H
}

func printArea(s Shape) {
    println(s.Area())
}

func main() {
    printArea(Rect{W: 3, H: 4})   // 12
}
```

Type assertions and type switches work as expected:

```moxie
    switch v := s.(type) {
    case Rect:
        println("rect:", v.W, v.H)
    }
```

Interfaces work freely within a domain. They **cannot** cross a spawn boundary — see section 11.

---

## 7. Collections

### Slices

```moxie
package main

func main() {
    s := []int32{1, 2, 3}
    s = append(s, 4, 5)

    // size literals (alternative to make)
    buf := []byte{:1024}           // 1024 zero bytes
    table := []int32{:0:100}       // len=0, cap=100

    // equality works on slices
    a := []int32{1, 2}
    b := []int32{1, 2}
    println(a == b)                // true

    // concatenation
    c := a | b                     // [1, 2, 1, 2]
    println(len(c))                // 4
}
```

### Maps

```moxie
package main

func main() {
    m := map[string]int32{
        "alpha": 1,
        "beta":  2,
    }
    m["gamma"] = 3
    v, ok := m["alpha"]
    println(v, ok)                 // 1 true
    delete(m, "beta")
}
```

Slices can be map keys (if the element type is comparable).

---

## 8. Control Flow

```moxie
package main

func main() {
    // if
    x := 10
    if x > 5 {
        println("big")
    } else {
        println("small")
    }

    // for (all three forms)
    for i := 0; i < 5; i++ {
        println(i)
    }

    for x > 0 {
        x--
    }

    items := []string{"a", "b", "c"}
    for i, v := range items {
        println(i, v)
    }

    // switch (no fallthrough — use comma-separated cases)
    switch x {
    case 0:
        println("zero")
    case 1, 2, 3:
        println("small")
    default:
        println("other")
    }
}
```

`break`, `continue`, `goto`, and labeled loops all work.

---

## 9. Channels and Select — The Dispatch System

This is the heart of Moxie. There are no goroutines. All concurrency within a domain is expressed through channels and `select`.

### Unbuffered Channels — Synchronous Dispatch

An unbuffered channel send **transfers execution** to the waiting `select` case. Think of it as a function call via the channel:

```moxie
package main

func main() {
    tick := chan struct{}{}         // unbuffered dispatcher
    done := chan struct{}{}

    // In a real program, these sends would come from
    // I/O callbacks, timers, or child domain messages.

    // Event loop
    select {
    case <-tick:
        println("tick received")
    case <-done:
        println("shutting down")
    }
}
```

### Buffered Channels — Message Queues

Buffered channels enqueue messages for the next `select` iteration:

```moxie
package main

func main() {
    msgs := chan string{10}        // buffer of 10

    msgs <- "first"
    msgs <- "second"

    // Process buffered messages
    for {
        select {
        case m := <-msgs:
            println(m)
        default:
            println("queue empty")
            return
        }
    }
}
```

### Building an Event Dispatcher

The pattern: create channels for each event type, run a `select` loop.

```moxie
package main

import (
    "fmt"
    "os"
)

type Server struct {
    requests chan string
    quit     chan struct{}
}

func newServer() *Server {
    return &Server{
        requests: chan string{100},
        quit:     chan struct{}{},
    }
}

func (s *Server) run() {
    for {
        select {
        case req := <-s.requests:
            fmt.Println("handling:", req)
        case <-s.quit:
            fmt.Println("server stopped")
            return
        }
    }
}

func main() {
    s := newServer()
    s.requests <- "GET /index"
    s.requests <- "GET /about"
    close(s.quit)
    s.run()
}
```

This is the fundamental execution pattern. Every Moxie program is a tree of event loops connected by channels.

---

## 10. Timers and I/O

The runtime integrates with `epoll` (Linux) / `kqueue` (macOS). I/O operations yield to the event loop and resume when data is ready.

```moxie
package main

import (
    "fmt"
    "net"
)

func main() {
    ln, err := net.Listen("tcp", ":8080")
    if err != nil {
        panic(err)
    }
    fmt.Println("listening on :8080")
    for {
        conn, err := ln.Accept()
        if err != nil {
            continue
        }
        // Handle synchronously — one connection at a time per domain.
        // Use spawn for concurrent connection handling.
        buf := []byte{:4096}
        n, _ := conn.Read(buf)
        conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"))
        conn.Close()
        _ = buf[:n]
    }
}
```

Within a single domain, I/O is sequential. For concurrent I/O handling, spawn child domains.

---

## 11. Domains and Spawn — Process Isolation

`spawn` creates a child domain: a separate OS process with its own heap and event loop. The parent and child communicate through IPC channels.

### Basic Spawn

```moxie
package main

func worker() {
    println("hello from child domain")
}

func main() {
    done := spawn(worker)
    _ = done
    println("parent continues immediately")
}
```

### Passing Data with Codec Types

Data crossing the spawn boundary must implement `moxie.Codec`. The `moxie` package provides codec wrappers for all primitive types:

```moxie
package main

import "moxie"

func compute(id moxie.Int32, scale moxie.Float64) {
    result := float64(id) * float64(scale)
    println(result)
}

func main() {
    spawn(compute, moxie.Int32(1), moxie.Float64(2.5))
    spawn(compute, moxie.Int32(2), moxie.Float64(3.0))
    // Both children run concurrently as separate processes
}
```

### IPC Channels

Pass channels to `spawn` for bidirectional communication. Channel element types must implement `moxie.Codec`:

```moxie
package main

import "moxie"

func producer(out chan moxie.Int32) {
    for i := int32(0); i < 10; i++ {
        out <- moxie.Int32(i)
    }
    close(out)
}

func main() {
    ch := make(chan moxie.Int32)
    spawn(producer, ch)

    // Receive results from child domain
    for {
        v, ok := <-ch
        if !ok {
            break
        }
        println(v)
    }
}
```

### Custom Codec Types

Define `EncodeTo` and `DecodeFrom` for your own types:

```moxie
package main

import (
    "io"
    "moxie"
)

type Point struct {
    X moxie.Float64
    Y moxie.Float64
}

func (p Point) EncodeTo(w io.Writer) error {
    if err := p.X.EncodeTo(w); err != nil {
        return err
    }
    return p.Y.EncodeTo(w)
}

func (p *Point) DecodeFrom(r io.Reader) error {
    if err := p.X.DecodeFrom(r); err != nil {
        return err
    }
    return p.Y.DecodeFrom(r)
}

func plotWorker(pt Point) {
    println(float64(pt.X), float64(pt.Y))
}

func main() {
    spawn(plotWorker, Point{X: 1.5, Y: 2.7})
}
```

### Move Semantics

Values passed to `spawn` are **moved** — they cannot be used afterward:

```moxie
    data := computeData()
    spawn(worker, data)
    println(data)          // compile error: ownership moved to child
```

Constants and channels are exempt.

### What Cannot Cross the Boundary

| Rejected | Reason |
|----------|--------|
| Raw `int32`, `bool`, etc. | Use `moxie.Int32`, `moxie.Bool`, etc. |
| Pointers | Cannot share memory across processes. |
| Functions | Cannot serialize code. |
| Interfaces | Type erasure prevents serialization. |

---

## 12. Managing Spawned Processes

### Fan-Out Pattern

Spawn multiple workers, collect results:

```moxie
package main

import "moxie"

func worker(id moxie.Int32, results chan moxie.Int32) {
    // Simulate work
    results <- moxie.Int32(int32(id) * int32(id))
}

func main() {
    n := 4
    results := make(chan moxie.Int32)

    for i := int32(0); i < int32(n); i++ {
        spawn(worker, moxie.Int32(i), results)
    }

    for i := 0; i < n; i++ {
        v := <-results
        println(v)
    }
}
```

### Pipeline Pattern

Chain domains together:

```moxie
package main

import "moxie"

func stage1(out chan moxie.Int32) {
    for i := int32(0); i < 5; i++ {
        out <- moxie.Int32(i)
    }
    close(out)
}

func stage2(in chan moxie.Int32, out chan moxie.Int32) {
    for {
        v, ok := <-in
        if !ok {
            close(out)
            return
        }
        out <- moxie.Int32(int32(v) * 2)
    }
}

func main() {
    ch1 := make(chan moxie.Int32)
    ch2 := make(chan moxie.Int32)

    spawn(stage1, ch1)
    spawn(stage2, ch1, ch2)

    for {
        v, ok := <-ch2
        if !ok {
            break
        }
        println(v)      // 0, 2, 4, 6, 8
    }
}
```

### Lifecycle Channels

`spawn` returns `chan struct{}` that closes when the child exits. Use it to wait for completion or detect failure:

```moxie
    done := spawn(worker, args...)

    select {
    case <-done:
        println("worker finished")
    case <-timeout:
        println("worker took too long")
    }
```

---

## 13. The JS Target — Service Workers and the Browser

Moxie compiles to JavaScript for browser deployment. Each domain maps to a BroadcastChannel-isolated context.

### Building for the Browser

```sh
moxie build -target js/wasm -o output/ .
```

This produces ES modules in the output directory, including a `$runtime/` folder and `$entry.mjs`.

### Service Worker Lifecycle

The JS runtime provides bridge packages for browser APIs:

```moxie
package main

// JS bridge packages provide typed access to browser APIs.
// Import paths under jsbridge/ map directly to runtime modules.

func main() {
    // DOM operations via bridge
    // WebSocket connections via bridge
    // Service Worker lifecycle via bridge
    // IndexedDB transactions via bridge
}
```

Bridge packages available: `dom` (elements, events), `ws` (WebSocket), `sw` (Service Worker lifecycle, caching, SSE), `localstorage`, `idb` (IndexedDB), `crypto` (secp256k1), `subtle` (SubtleCrypto).

### How Channels Map to the Browser

Within the JS target:
- **Channels** become Promise-based queues. Send/receive are `async`/`await`.
- **Select** shuffles cases for fairness, tries non-blocking first, then awaits.
- **Spawn** creates BroadcastChannel-isolated contexts with deep-copied data.
- **I/O** maps to fetch, WebSocket, and other browser APIs.

The compiler automatically marks functions as `async` when they contain channel operations, propagating transitively up the call chain.

---

## 14. Literal Syntax Summary

| Moxie | Equivalent | Notes |
|-------|-----------|-------|
| `"hello"` | `[]byte("hello")` | String literals produce `[]byte` |
| `a \| b` | `concat(a, b)` | Slice concatenation (any matching slice type) |
| `[]T{:n}` | `make([]T, n)` | Slice with length n |
| `[]T{:n:c}` | `make([]T, n, c)` | Slice with length n, capacity c |
| `chan T{}` | `make(chan T)` | Unbuffered channel |
| `chan T{n}` | `make(chan T, n)` | Buffered channel with capacity n |
| `&T{...}` | (heap allocation) | Replaces `new(T)` |

---

## 15. What Moxie Removes

| Removed | Alternative | Why |
|---------|------------|-----|
| `go f()` | `spawn` + channels | Hidden execution paths. |
| `new(T)` | `&T{}` | Redundant. |
| `+` on text | `\|` | Unifies slice concatenation. |
| `fallthrough` | `case A, B:` | Implicit control flow. |
| `complex64/128` | Separate float fields | Rarely needed. |
| `uintptr` | Explicit pointers | Raw pointer arithmetic is `unsafe` only. |
| `import "strings"` | `import "bytes"` | `string = []byte` makes `strings` redundant. |
| `interface{}` at spawn | `moxie.Codec` types | Serialization requires concrete types. |

---

## Next Steps

- [REFERENCE.md](REFERENCE.md) — Complete language specification
- [docs/spawn.md](docs/spawn.md) — Spawn quick reference
- [docs/architecture.md](docs/architecture.md) — Compiler pipeline and runtime internals
- [docs/PORTING.md](docs/PORTING.md) — Migrating existing code to Moxie
- [EXAMPLE_CLAUDE.md](EXAMPLE_CLAUDE.md) — CLAUDE.md template for AI-assisted Moxie development
