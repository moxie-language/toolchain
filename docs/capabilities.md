---

## moxie capability scheme (revised)

**the pipe primitive**

`pipe` is `chan byte` with constraints. a single declaration carries direction, protocol, and scope:

```
var r <-pipe[Msg1 | Msg2]{:4096:"/path/to/prefix"}
var w ->pipe[HttpReq | HttpResp]{::"tcp://*:443"}
```

three layers compose orthogonally:

| layer     | position                     | enforced                                         |
| --------- | ---------------------------- | ------------------------------------------------ |
| direction | `<-pipe` / `->pipe` / `pipe` | compile-time                                     |
| protocol  | `[Type1 | Type2]`            | compile-time via codec                           |
| scope     | `{:buf:"prefix"}`            | compile-time for literals, runtime for variables |

braces are positional: `{:bufferCapacity:"prefix"}`. the first colon marks the channel metadata block. missing values: `{::"prefix"}`, `{:4096:}`, or `{}` for neither. the two colons always present even when values are absent -- same delimiter pattern as spawn.

**builtins**

```
len(p)     // buffered elements waiting
cap(p)     // total buffer capacity
prefix(p)  // scope string ("" if un-prefixed)
```

**stdio**

`stdin` (`<-pipe`), `stdout` (`->pipe`), `stderr` (`->pipe`) are per-actor keywords bound at spawn, immutable for the actor's lifetime. no import. `fmt.Println` writes to the actor's `stdout` automatically.

**spawn**

```
spawn(worker)                              // inherit all stdio from parent
spawn[:logPipe:](worker)                   // override only stdout
spawn[::errPipe](worker)                   // override only stderr
spawn[inPipe:outPipe:errPipe](worker)      // all three overridden
```

brackets appear only when overriding. absent means inherit. positional: `[stdin:stdout:stderr]`. colons always present. parentheses hold the spawned function and any additional typed channels passed to the child.

**attenuation**

extracting a directional end narrows permission. no wrapper types:

```
var readOnly <-pipe = cap.readEnd
var httpOnly ->pipe = net.Cap.Ports(80, 443)
```

a function receiving only the read end cannot write. no annotation, no struct.

the net.Cap type above is a convenience method for generating a path specification, in the example above that would be:

    *://*:[(80)|(43)]

**filesystem & network**

the filesystem actor creates a `->pipe[FsMsg]{::"/home/app"}` and hands it to the caller. `open()` sends a message through the pipe; the actor resolves the realpath and rejects escapes (`..`, symlinks) outside the prefix. network works identically: prefix `tcp://*:443` constrains host and port, DNS resolved before containment check.

**receive-side type matching**

the variable's type selects which message to dequeue. mismatch skips:

```
select {
case req := <-pipe[HttpRequest]:
    // fires only if buffer head is HttpRequest
case resp := <-pipe[HttpResponse]:
    // fires only if buffer head is HttpResponse
}
```

the codec inspects the buffer head. no enum switch, no type tag on the wire.

## Errors

the `Error` type is implicitly part of every pipe's receive union. no declaration needed -- `pipe[Msg1 | Msg2]` silently includes `Error`. the codec produces it when the bytes don't decode: wrong size prefix, truncated message, corrupt type tag, bytes that deserialize into no valid union member.

at the receive side:

```
select {
case req := <-pipe[HttpRequest]:
    // handle request
case resp := <-pipe[HttpResponse]:
    // handle response
case err := <-pipe[Error]:
    // corrupt bytes at offset err.Offset, reason err.Msg
}
```

the `Error` value carries:
- `Offset int64` -- byte position in the stream where decode failed
- `Msg string` -- what went wrong (truncated, bad size prefix, unknown type tag)

**resync behavior**

the pipe codec is responsible for recovery. for size-prefixed protocols, the codec skips `sizePrefix` bytes and resumes at the next boundary. for self-synchronizing codecs (protobuf-style varint delimited), it scans forward to the next valid frame. for raw `chan byte` with no protocol constraint, errors propagate as raw byte dumps with an error marker -- the receiver gets whatever was in the buffer.

the codec emits one `Error` per corrupt frame, then continues. the pipe stays open. the caller decides whether to close.

**send side**

no error type on send. the codec only encodes valid union members -- the compiler already rejected invalid types. send-side failure is only about the channel buffer being full or the remote end closed, which the nonblocking send already surfaces:

```
select {
case pipe <- msg:
    // sent
default:
    // buffer full, remote not consuming
}
```

**revision to the scheme text** -- add after "receive-side type matching":

the `Error` type is implicit in every pipe's receive union. no declaration needed. the codec produces it when bytes don't decode. an `Error` carries offset and message. the codec skips the corrupt frame and resumes. the pipe stays open. the caller's select chooses whether to log, retry, or close.

**backward compatibility**

all Go-style code using `os.Stdout`, `fmt.Fprintln`, `io.Reader`, `io.Writer` compiles unchanged. the interfaces become thin wrappers around native pipe syntax. rewire the internals; the surface stays the same.
