# spawn

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

`spawn` is a language builtin (like `push`, `copy`). No import required. Registered in the type universe alongside other Moxie builtins.

```go
done := spawn(funcName, arg1, arg2, ...)
done := spawn("local", funcName, arg1, arg2, ...)  // explicit transport
```

Creates a new domain — an isolated OS process (native) or Worker (JS) — running `funcName(arg1, arg2, ...)` with its own cooperative scheduler and heap. Returns `chan struct{}` (lifecycle channel).

## Codec Requirement

All data arguments and channel element types must implement `moxie.Codec`:

```go
import "moxie"

type Codec interface {
    EncodeTo(w io.Writer) error
    DecodeFrom(r io.Reader) error
}
```

Built-in codec types encode little-endian by default. Big-endian aliases exist for protocols like Bitcoin:

| Default (LE) | Big-endian (BE) | Size |
|-------------|-----------------|------|
| `moxie.Bool`, `moxie.Int8`, `moxie.Uint8` | — | 1 byte |
| `moxie.Int16`, `moxie.Uint16` | `moxie.BigInt16`, `moxie.BigUint16` | 2 bytes |
| `moxie.Int32`, `moxie.Uint32` | `moxie.BigInt32`, `moxie.BigUint32` | 4 bytes |
| `moxie.Int64`, `moxie.Uint64` | `moxie.BigInt64`, `moxie.BigUint64` | 8 bytes |
| `moxie.Float32` | `moxie.BigFloat32` | 4 bytes |
| `moxie.Float64` | `moxie.BigFloat64` | 8 bytes |
| `moxie.Bytes` | — | 4-byte LE length prefix + data |

User-defined structs implement Codec by defining EncodeTo/DecodeFrom methods that encode each field.

## Example

```go
import "moxie"

func worker(n moxie.Int32, scale moxie.Float64) {
    println(float64(n) * float64(scale))
}

func main() {
    spawn(worker, moxie.Int32(42), moxie.Float64(2.5))
}
```

## Type Checking

The type checker (patched go/types) validates:
- First argument must be a function (or transport string + function)
- Result type is `chan struct{}`

The compiler's SSA validator additionally enforces:
- All data argument types implement `moxie.Codec`
- All channel element types implement `moxie.Codec`
- No `interface{}` values cross the boundary
- No pointers, functions, or nested channels
- Only unbuffered channels allowed

## Move Semantics

Non-constant, non-channel values passed to `spawn` cannot be used afterward:

```go
x := compute()
spawn(worker, x)
println(x)        // compile error: ownership moved to child
```

## Channel Behavior

All spawn channel operations are non-blocking with cooperative yield. The runtime peeks for data (recv) or attempts a non-blocking write (send). If the pipe buffer is empty/full, the runtime yields for 1ms and retries indefinitely until the operation succeeds or the pipe closes. Both sides of the socketpair are symmetric - no parent/child behavioral distinction. Backpressure is expressed by pipe buffer fullness, not by blocking.

## Native Implementation

`socketpair()` + `fork()`. Child gets COW copy of parent memory. Each domain has its own arena (bump allocator), run queue, and task stacks. No GC - arena released via `munmap` on domain exit.

## JS Implementation

`$rt.domain.spawn(async () => fn(args))`. Slices spread-copied, maps `Object.assign`'d. Isolated microtask scheduler per domain.
