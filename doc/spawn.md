# spawn

See [REFERENCE.md](../REFERENCE.md) for the complete language reference.

`spawn` is a language builtin. No import required.

```go
done := spawn(funcName, arg1, arg2, ...)
```

Creates a new domain — an isolated OS process (native) or Worker (JS) — running `funcName(arg1, arg2, ...)` with its own cooperative scheduler and heap. Returns `chan struct{}` (lifecycle channel).

## Example

```go
func worker(n int32, scale float64) {
    println(float64(n) * scale)
}

func main() {
    spawn(worker, int32(42), float64(2.5))
}
```

## Allowed Parameter Types

Integers, floats, bools, strings, arrays, structs, slices, maps, and bare channels. All checked recursively — every leaf type must be serializable.

## Rejected Parameter Types (compile error)

Pointers, functions, all interface types, and channels nested inside compound types.

## Move Semantics

Non-constant, non-channel values passed to `spawn` cannot be used afterward:

```go
x := compute()
spawn(worker, x)
println(x)        // compile error: ownership moved to child
```

## Native Implementation

`socketpair()` + `fork()`. Child gets COW copy of parent memory. Each domain has its own Boehm GC heap, run queue, and goroutine stacks.

## JS Implementation

`$rt.domain.spawn(async () => fn(args))`. Slices spread-copied, maps `Object.assign`'d. Isolated microtask scheduler per domain.
