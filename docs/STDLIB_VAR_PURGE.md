# Stdlib var Purge - Remaining 20 Vars

Status: mechanical conversion phases 0-8 COMPLETE. All init() eliminated.
~1930 vars reduced to 20 irreducible package-level vars.

Build verified clean: native and WASM both pass.

## Phase 9 Enforcement Rule

Prohibit `var X = expr` (initialized vars). Allow `var X T` (zero-value vars).

Zero-value vars are typed static storage - linker places them in .bss, no
runtime code runs. Initialized vars require evaluation code in a package
initializer function, which is the singleton/init pattern being eliminated.

The compiler should reject any `var` declaration with an initializer expression
in non-exempt packages. Zero-value declarations (`var X T`) remain legal.

## Category A: Runtime Scalar Constants (6 vars)

These have `var X T = expr` where `expr` evaluates to a constant but the
compiler currently wraps the store in a package initializer function.

| Var | File | Declaration |
|-----|------|-------------|
| xorshift32State | runtime/algorithm.mx:32 | `var xorshift32State uint32 = 1` |
| xorshift64State | runtime/algorithm.mx:52 | `var xorshift64State uint64 = 1` |
| randState | runtime/runtime_wasm.mx:29 | `var randState uint64 = 0x853c49e6748fea9b` |
| inf | runtime/float.mx:9 | `var inf = float64frombits(0x7FF0000000000000)` |
| ChildPipeFd | runtime/pipe_channel.mx:837 | `var ChildPipeFd int32 = -1` |
| secLockdownFd | runtime/secalloc.mx:57 | `var secLockdownFd int32 = 2` |

### Analysis

xorshift32State and xorshift64State are seeds overwritten by initRand() at
startup. Their initial value 1 is a safety net if hardwareRand() fails.
randState is the WASM-only fallback PRNG seed - critical non-zero value.
inf calls float64frombits() which is an unsafe.Pointer cast, not a constant
expression in the current compiler. ChildPipeFd = -1 and secLockdownFd = 2
are sentinel fd values.

### Elimination

The compiler should emit these as data-segment constants - the linker places
the initial value directly in the binary's .data section. No runtime code runs.
This is what C compilers do for `int x = 42;` at file scope.

The Moxie compiler already does this under the hood via LLVM
`@global = internal global i32 1` - the var keyword triggers the package
initializer synthetic function, but the actual value is a constant initializer
that LLVM places in the data segment anyway. The package initializer is a
no-op store of the same value.

Fix: make the compiler recognize `var X T = <const-expr>` and emit it as an
LLVM global with a constant initializer WITHOUT wrapping it in the package
initializer function. For inf, float64frombits(0x7FF0000000000000) must be
recognized as constant-foldable (it is a bitcast). If that is too complex,
move inf assignment into run() before initAll():

    func run() {
        inf = float64frombits(0x7FF0000000000000)
        initRand()
        initHeap()
        initAll()
        callMain()
        ...
    }

After compiler change, convert all 6 to zero-value + data-segment init:

    var xorshift32State uint32  // .data: 1
    var xorshift64State uint64  // .data: 1
    var randState uint64        // .data: 0x853c49e6748fea9b
    var inf float64             // .data: bitcast(0x7FF0000000000000)
    var ChildPipeFd int32       // .data: -1
    var secLockdownFd int32     // .data: 2

Or use explicit init in run() if data-segment const recognition is deferred.

## Category B: Assembly-Referenced CPU Feature Booleans (5 vars)

Assembly code reads these via linker symbol: `CMPB .supportADX(SB), $1` or
`MOVB .hasVX(SB), R1`. They are populated during package init by reading
CPU feature flags from internal/cpu.

| Var | File | Declaration | Asm Refs |
|-----|------|-------------|----------|
| supportADX | crypto/internal/fips140/bigmod/nat_asm.mx:21 | `var supportADX = cpu.X86HasADX() && cpu.X86HasBMI2()` | nat_amd64.s (3) |
| hasADX | math/big/arith_amd64.mx:11 | `var hasADX = cpu.X86.HasADX && cpu.X86.HasBMI2` | arith_amd64.s (1) |
| hasVX | math/arith_s390x.mx:170 | `var hasVX = cpu.S390X.HasVX` | stubs_s390x.s (22) |
| hasVX | math/big/arithvec_s390x.mx:11 | `var hasVX = cpu.S390X.HasVX` | arith_s390x.s (2) |
| useFMA | math/exp_amd64.mx:11 | `var useFMA = cpu.X86.HasAVX && cpu.X86.HasFMA` | exp_amd64.s (1) |

### Analysis

Assembly needs a linker symbol at a fixed address. The var must exist as static
storage. But the initializer expression (reading cpu struct fields) can be
separated from the declaration.

Alternative path - assembly reads cpu struct directly: modify assembly to use
`CMPB internal/cpu.X86+<offset>(SB), $1`. This requires knowing struct field
offsets at assembly time, which is brittle. Not recommended.

### Elimination

Keep the backing var as zero-value (`var supportADX bool`). Add an explicit
init function that sets it:

    var supportADX bool
    func _initSupportADX() {
        supportADX = cpu.X86HasADX() && cpu.X86HasBMI2()
    }

The compiler emits the init function call in initAll() before any dependent
package code. Assembly references remain unchanged - they read the same symbol.

This reduces `var X = expr` to `var X T` (zero-value, no init expression).

Apply to all 5 vars. The init functions must run after CPU detection (osInit)
which happens before initAll() in the runtime startup sequence:

    run() -> initRand() -> initHeap() -> initAll() -> callMain()

CPU detection runs during initHeap/platform init, before initAll(). So calling
_initSupportADX() from within initAll() at the right package ordering is safe.

## Category C: Identity Sentinels (7 vars)

Compared by pointer identity (x == NoBody, t == reserved).

| Var | File | Comparison Pattern |
|-----|------|--------------------|
| NoBody | net/http/http.mx:177 | `r.Body == NoBody` |
| sentinelHandler | net/http/csrf.mx:79 | pointer comparison |
| multipartByReader | net/http/request.mx:497 | pointer comparison |
| ErrNotSupported | net/http/request.mx:64 | interface comparison |
| DefaultServeMux | net/http/server.mx:2528 | `= &defaultServeMux` |
| goexitPanicValue | iter/iter.mx:472 | `== goexitPanicValue` |
| reserved | go/internal/gccgoimporter/parser.mx:447 | `== reserved` |

### Elimination

**NoBody** - noBody{} is a zero-size struct. `var NoBody = noBody{}` becomes
`var NoBody noBody` (zero-value, identical to noBody{}). No init expression
needed. Interface comparison at usage sites boxes the zero-size struct on both
sides - same dynamic type, same (zero) value, comparison works.

**DefaultServeMux** - already has `var defaultServeMux ServeMux` as backing var.
Convert to: `var defaultServeMux ServeMux` + `func DefaultServeMux() *ServeMux`.
Update ~10 call sites to add ().

**sentinelHandler** - `var sentinelHandler Handler = &noopHandler{}`. noopHandler
is zero-size. Convert to: `var _sentinelHandler noopHandler` (zero-value) +
`func sentinelHandler() Handler { return &_sentinelHandler }`. Update comparison
sites.

**multipartByReader** - `var multipartByReader = &multipart.Form{Value: ..., File: ...}`.
Has non-zero fields (empty maps). Convert to backing var + lazy-init function:

    var _multipartByReader multipart.Form
    var _multipartByReaderDone bool
    func multipartByReader() *multipart.Form {
        if !_multipartByReaderDone {
            _multipartByReaderDone = true
            _multipartByReader.Value = map[string][][]byte{}
            _multipartByReader.File = map[string][]*multipart.FileHeader{}
        }
        return &_multipartByReader
    }

**ErrNotSupported** - `var ErrNotSupported = &ProtocolError{"feature not supported"}`.
Has non-zero field. Convert to backing var + lazy-init:

    var _errNotSupported ProtocolError
    var _errNotSupportedDone bool
    func ErrNotSupported() *ProtocolError {
        if !_errNotSupportedDone {
            _errNotSupportedDone = true
            _errNotSupported.ErrorString = "feature not supported"
        }
        return &_errNotSupported
    }

**goexitPanicValue** - `var _goexitSentinel int; var goexitPanicValue any = &_goexitSentinel`.
The any interface wrapping requires runtime evaluation (boxing). Keep
`var _goexitSentinel int` as zero-value backing var. Convert to:
`func goexitPanicValue() any { return &_goexitSentinel }`. Update 6 comparison
sites in iter/iter.mx.

**reserved** - `var reserved = &struct{ types.Type }{}`. Anonymous struct with
embedded types.Type interface at zero value. Convert to:

    type reservedType struct{ types.Type }
    var _reserved reservedType
    func reserved() *reservedType { return &_reserved }

Update ~10 comparison sites in parser.mx.

## Category D: go:linkname / Bootstrap-Only (2 vars)

| Var | File | Build Tag |
|-----|------|-----------|
| overflowError | math/bits/bits_errors.mx:12 | !compiler_bootstrap |
| divideError | math/bits/bits_errors.mx:15 | !compiler_bootstrap |

### Analysis

In the non-bootstrap path, these are already zero-value declarations:

    //go:linkname overflowError runtime.overflowError
    var overflowError error

    //go:linkname divideError runtime.divideError
    var divideError error

The go:linkname aliases them to runtime symbols. The runtime does not declare
these symbols either - they are ghost symbols that exist only as linker entries.
panic() in math/bits/bits.mx passes them to the panic machinery.

In the bootstrap path (bits_errors_bootstrap.mx, tag compiler_bootstrap),
they are `var X = error(errorString("..."))` - these only exist when building
with the Go bootstrap compiler, not Moxie.

### Status

Already solved. The non-bootstrap vars are `var X error` with no initializer.
The bootstrap file is never compiled by Moxie. No changes needed.

## Execution Order

### Immediate (no compiler changes needed) - 13 vars

1. NoBody -> zero-value var declaration
2. DefaultServeMux -> backing var + function, update call sites
3. sentinelHandler -> backing var + function, update call sites
4. multipartByReader -> backing var + lazy-init function, update call sites
5. ErrNotSupported -> backing var + lazy-init function, update call sites
6. goexitPanicValue -> backing var + function, update call sites
7. reserved -> named type + backing var + function, update call sites
8. overflowError, divideError -> already done (zero-value)
9. 5 assembly vars -> zero-value + explicit _init function

### Compiler changes needed - 6 vars

10. Recognize `var X T = <const-expr>` as data-segment init (no package
    initializer function), or emit explicit assignment in run() for runtime
    package vars that can not use initAll()

### Compiler enforcement - Phase 9

11. Reject `var X = expr` in non-exempt packages
12. Allow `var X T` (zero-value) everywhere

## Build Verification

After each group of conversions:

    MOXIEROOT=/home/mleku/s/moxie ./moxie build -C /home/mleku/s/smesh -o /dev/null .
    MOXIEROOT=/home/mleku/s/moxie ./moxie build -C /home/mleku/s/smesh/web/wasm/relay-proxy -target js/wasm -o /dev/null .
