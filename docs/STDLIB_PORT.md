# Moxie Stdlib Port Plan

## Status: 165/173 public packages compile. 8 fail (6 unfixable, 2 flaky in batch).

Previous: 167/173 (before reflect/gob changes).

Full count including internal: 165/241 pass. 68 internal packages fail
(most are "use of internal package not allowed" when tested from outside).

## Remaining Public Failures (8)

| Package | Reason | Fixable? |
|---------|--------|----------|
| crypto/rsa | batch-run cache flake (passes individually) | N/A |
| crypto/tls | batch-run cache flake (passes individually) | N/A |
| debug/buildinfo | uses unexported runtime internals | No |
| hash/maphash | needs runtime hash seed | No |
| plugin | requires dlopen, incompatible with static linking | No |
| runtime/coverage | not in moxie's goroot (instrumentation) | No |
| runtime/race | not in moxie's goroot (sanitizer) | No |
| time/tzdata | missing linker symbol time.registerLoadFromEmbeddedTZData | No |

## What Was Done

### Phase 1: Socket Syscalls (complete)
`$MOXIEROOT/src/syscall/socket_hosted.go` — full BSD socket API via libc exports.
Constants, types, and functions for socket(), bind(), listen(), accept4(),
connect(), setsockopt/getsockopt, epoll, netlink, etc.

### Phase 2: net overlay (complete)
125 Go stdlib files copied to `$MOXIEROOT/src/net/`.
`cgo_stub_moxie.go` provides cgoAvailable=false stubs.
Pure Go resolver only — no cgo DNS.

### Phase 3: crypto/tls overlay (complete)
`common.go` expanded with full ConnectionState fields, cipher suite constants,
and Config.Clone() method. `tls.go` provides stub Conn type.
`ticket.go` provides SessionState/ClientSessionState stubs.

### Phase 4: net/http overlay (complete)
33 Go stdlib files + 6 sub-packages (cgi, cookiejar, fcgi, httptest, httputil, pprof)
copied to `$MOXIEROOT/src/net/http/`. httptrace and internal also present.

### Phase 5: reflect stripped to read-only + write wrappers (complete)
- reflect/value.go: read-only core (type inspection, field/index/map access)
  plus minimal write wrappers needed by stdlib (Set, New, MakeSlice, MakeMap,
  MakeMapWithSize, Append, AppendSlice, Copy, Call, Recv, etc.)
- reflect/makefunc.go: MakeFunc panic stub (compiles but panics at runtime)
- reflect/iter.go: deleted (Seq/Seq2 as panic stubs on Value)
- reflect/type.go: StructOf, MapOf, FuncOf, ChanOf removed
- No dynamic type construction. No Send, Select, MakeChan.
- Write wrappers exist solely to bridge reflect.Value ↔ reflectlite.Value
  types so stdlib consumers (encoding/json, encoding/xml, etc.) compile.

### Phase 6: fmt overlay (complete)
Copied from Go stdlib, patched scan.go to remove reflect.MakeSlice dependency.
Default scan case returns error instead of attempting reflection-based scanning.

### Phase 7: encoding/gob removed (complete)
Stub package at `$MOXIEROOT/src/encoding/gob/gob.go` provides Encoder/Decoder
types and Register/RegisterName functions. All operations panic at runtime.
net/rpc compiles but gob-based codecs will panic.

### Cascade unlocks
Once net compiled, these all fell into place automatically:
net/textproto, net/mail, net/smtp, mime/multipart, crypto/tls, crypto/x509,
net/http (+ cgi, cookiejar, fcgi, httptest, httptrace, httputil, pprof),
expvar, net/rpc, net/rpc/jsonrpc, log/syslog, text/tabwriter

## goroot merge entries added

```
"encoding/":     true,   // merge parent so encoding/gob/ can be overridden
"encoding/gob/": false,  // replaced with panic stubs
"fmt/":          false,  // patched scan.go (no write-reflect in scanning)
```

## Concurrency Model for net/http

```
acceptor domain (single thread, owns epoll):
  epoll_wait() -> ready fds
  for each ready fd:
    if listener fd -> accept() new conn
    if conn fd -> read into buffer
    if request complete -> send to work channel
    if work channel full -> spawn(worker, work_chan)

worker domain (single thread, owns assigned connections):
  for req := range work_chan:
    parse HTTP request
    call handler
    write response
    return conn to acceptor or close
```

Backpressure drives scaling. No thread pool. No work-stealing.
Spawn when full, retire when idle.
