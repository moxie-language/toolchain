# Bootstrap Plan - 2026-05-27

## Architecture

The bootstrap is a staged self-compilation chain:

```
gen0: TinyGo/moxie compiles bootstrap/*.mx -> compile.so (C-shared library)
gen1: compile.so compiles its own 23 source files -> gen1 IR -> gen1.so
gen2: gen1.so compiles the same source -> gen2 IR (must match gen1 = fixed point)
```

`builder/bootstrap.go` integrates compile.so into the moxie build pipeline. Gate: `MOXIE_BOOTSTRAP_COMPILE=1`. The .so loads via purego, registers imports from `go/types`, concatenates .mx sources, emits LLVM IR text, parses it with `LLVMParseIRInContext` into `llvm.Module`, then feeds into the existing transform/optimize/emit pipeline.

End state: a standalone `moxie-bootstrap` binary that compiles Moxie without the Go compiler toolchain. Only needs libLLVM at link time.

## Current State (verified 2026-05-27)

### What works

- **Gen0 compile.so**: 1336/1336 probe tests pass. 17,400 lines across 24 .mx files in `compile/`.
- **Gen1 gen1.so**: 1336/1336 probe tests pass. Compiled by gen0, linked with gen1_runtime.c.
- **Incremental self-compilation**: all 23 .mx files compile through gen0. Gen1 self-compilation (gen2) works for pos.mx (176 lines) but crashes (SIGABRT/SIGSEGV) on larger inputs due to gen1 runtime limitations (leaking allocator, no stack growth).
- **Import registration**: 13 external packages registered (bytes, fmt, go/build/constraint, go/constant, go/token, io, math/big, os, runtime, strconv, unicode, unicode/utf8, unsafe). Supports types, functions, variables, constants, interfaces, methods.
- **Builder integration**: `builder/bootstrap.go` (280 lines) loads compile.so, registers imports, concatenates sources, parses IR. Currently main-package-only.
- **Purego LLVM bindings**: `llvmpure/` (3,909 lines) wraps libLLVM-21 via purego. No cgo/C++ needed at build time.
- **LLVM glue**: `llvm-glue/build.sh` builds `libmoxie-llvm-glue.so` from IRBindings.cpp + backports.cpp + shims.c.

### Supporting .so modules (built, not all tested on LLVM 21)

- `typecheck.so` (2.1MB, May 22)
- `mxssa.so` (2.5MB, May 22)
- `loader.so` (2.1MB, May 22)
- `compile.so` (3.0MB, May 27 - latest)

### Codegen bugs found and fixed (in working tree)

1. **Short-circuit && / ||** (Bug 3): `OpLand`/`OpLor` were lowered to eager `SSABinOp`. Fixed: proper SSA branching with `SSAIf` + `SSAPhi`. `compile/ssa_builder.mx:1657-1692`.

2. **Stack alloca zero-initialization** (Bug 2): bare `alloca` without zeroinitializer left struct fields as garbage. Fixed: `store elemType zeroinitializer, ptr reg` after alloca. `compile/ir_emit.mx:740-743`.

3. **Type assertion double-deref for pointer types** (Bug 1): extractvalue data field then load treated the pointer as an address. Fixed: when `assertedType == "ptr"`, use extractvalue result directly. `compile/ir_emit.mx:3325-3328`.

4. **Interface coercion in buildStore** (Bug 4): SelectorExpr, global, and deref store paths were missing `coerceToInterface()`. Partially fixed - SelectorExpr path now calls coercion. `compile/ssa_builder.mx:647-664`.

5. **TypeID constant merging**: all 135 typeid globals collapsed to `i8 0` at link time because they were all defined as `@main.typeid.X = global i8 0`. LLVM's constant merge pass treated them as identical. Fixed: each typeid now has a distinct initializer.

6. **Scanner +/++ miscompilation**: the scanner was misidentifying `+` vs `++` tokens. Fix was in progress when the previous session ended. **Needs verification before Phase 2.**

7. **Tail-call optimization**: rewrites `return f(args)` to loop for self-calls in tail position. `compile/ssa_builder.mx`. Disabled when defers present.

8. **PHI predecessor tracking**: `blockExitLabel` map in ir_emit.mx tracks actual IR labels when invoke dispatch splits SSA blocks. Fixes PHI predecessor mismatches.

9. **Integer literal type mismatch**: `iotaVal = 0` emitted `store i32 0` to an `i64` alloca. Untyped `0` defaults to i32 in gen0 codegen. Fixed: explicit `int64(0)` cast. `compile/ir_emit.mx:4048`.

10. **Missing load in slice-of-pointer field access**: `slice[idx].field` where slice is `[]*T` emitted `SSAIndexAddr` (getting `**T`) then directly GEPed as `*T`, skipping element pointer dereference. Fixed: detect pointer element type and emit load before `SSAFieldAddr`. `compile/ssa_builder.mx:2252-2256`.

### Known remaining codegen bugs (from gen1 debugging sessions)

11. **Gen1 self-compilation crashes**: gen1 runtime (leaking allocator, fixed-size stack) cannot handle compiling inputs larger than ~200 lines. Manifests as SIGABRT/SIGSEGV. Not a codegen bug - runtime limitation. Blocks gen2 fixed-point verification.

### Known runtime bugs

12. **defer broken**: deferred functions never execute. Both Go compiler path and compile.so path affected. Pre-existing runtime issue in how the defer linked list is walked at function return.

**Defer exposure in the 23 compiler source files**: 57 `defer` sites total. 46 are in `parser.mx` gated behind `const trace = false` (dead code - the compiler eliminates the entire `if trace { ... }` block). The 11 real defers are:
- `compile/syntax.mx` (3): error recovery via `recover()`, `f.Close()`
- `compile/tc_decl.mx` (3): scope save/restore - `defer func() { c.localScope = saved }()`
- `compile/tc_types.mx` (1): recursive type resolution guard
- `compile/tc_stmt.mx` (1): scope save/restore

The `tc_decl.mx` and `tc_stmt.mx` scope restores are critical for correct type checking. If defer is broken, `c.localScope` never restores after processing type declarations with type parameters, causing scope corruption. This will affect gen1 type checking of any code that exercises those paths.

**Mitigation**: the scope save/restore pattern can be manually inlined (explicit `c.localScope = saved` before every return) if defer remains broken during Phase 2. The `syntax.mx` error recovery defers are less critical - parse errors just won't be caught cleanly.

## Plan

### Phase 1: Commit & Stabilize

The 2,888-line uncommitted diff across 58 files must be committed. It contains the bug fixes that make gen1 possible.

1. Verify `make test-compile` passes (1336/1336 - confirmed 2026-05-27)
2. Commit the working tree
3. Verify compile.so still works from clean state

**Build commands:**
```
cd ~/s/moxie/bootstrap
LD_LIBRARY_PATH=/usr/lib/llvm-21/lib:$(pwd)/llvm-glue:$LD_LIBRARY_PATH \
  LLVM_LIB_PATH=/usr/lib/llvm-21/lib/libLLVM-21.so \
  make test-compile
```

### Phase 2: Gen1 Link & Test

Goal: compile.so compiles its own 23 .mx files, link the output into gen1.so, run tests against gen1.so.

**Step 1 - Extract gen1 IR**

The probe_compile incremental test already compiles all 23 files through gen0. Capture the full-concat IR:

```
# Modify probe_compile or write a small extractor that:
# 1. Loads compile.so via purego
# 2. Registers all 13 external imports
# 3. Concatenates all 23 .mx files + "func main() {}"
# 4. Calls moxie_compile_to_ir
# 5. Writes IR to gen1.ll
```

The concat order matters (same as probe_compile line 14933-14939):
```
pos.mx source.mx tokens.mx nodes.mx syntax.mx scanner.mx parser.mx
tc_scope.mx tc_package.mx tc_info.mx tc_object.mx tc_types.mx tc_universe.mx
tc_const.mx tc_resolve.mx tc_checker.mx tc_decl.mx tc_expr.mx tc_stmt.mx tc_assign.mx
ssa_types.mx ssa_builder.mx ir_emit.mx
```

Note: `compile_exports.mx` is NOT in the concat list - it contains the C-shared export wrappers which reference `unsafe.Pointer` and purego patterns. Gen1 needs its own export layer or the exports need to be added as a 24th file.

**Step 2 - Fix known gen1 IR issues**

Bugs 9-11 from the audit. These are gen0 codegen bugs that manifest in gen1 output:

- TypeID prefix: ensure all invoke dispatch chains in gen0's ir_emit.mx use `ptr.` prefix when the dispatched value is interface-boxed
- Instruction types: add SSAUnOp/SSABinOp/SSAAlloc/SSACall/SSAPhi to all SSAValue dispatch chains in gen0
- Interface stores: ensure all interface field stores construct `{ptr, ptr}` pairs

Fix these in gen0 source, rebuild compile.so, re-extract gen1 IR.

**Step 3 - Verify scanner +/++ fix**

Bug 6 - verify the scanner correctly distinguishes `+` from `++`. Write a probe test with both tokens adjacent. If not fixed, fix it in gen0 scanner.mx.

**Step 4 - Link gen1.so**

```
clang -O1 -shared -fPIC gen1.ll -o gen1.so -lm
```

This will fail if gen1 IR references undefined symbols. Expected issues:
- Runtime functions (`runtime.alloc`, `runtime.stringEqual`, etc.) - need stubs or link against runtime
- External package functions (the 13 registered imports) - need stubs or link against stdlib
- The `main` function - gen1 needs compile_exports.mx or equivalent

**Step 5 - Test gen1.so**

Run gen1.so through the probe_compile test suite:
```
cd bootstrap/probe_compile && go run . ../gen1.so
```

Compare gen0 vs gen1 output for same inputs. Any difference is a codegen bug.

**Step 6 - Iterate**

Every gen1 crash/mismatch reveals a gen0 codegen bug. Fix in gen0 source, rebuild gen0, re-extract gen1, re-test. This is the core work of the bootstrap. History from previous sessions: each round exposes 2-3 new bugs in type dispatch, interface boxing, or struct layout.

**Defer risk**: if gen1 type checking hits the scope save/restore defers (tc_decl.mx lines 130, 162, 200; tc_stmt.mx line 14), and defer is broken, scope corruption will cause silent miscompilation. If this happens, manually inline the scope restores: replace `defer func() { c.localScope = saved }()` with explicit `c.localScope = saved` before every return in those functions.

### Phase 3: Multi-Package Bootstrap

Goal: compile.so can compile arbitrary packages, not just main.

1. **Extend builder/bootstrap.go import registration**:
   - Add `*types.TypeName` -> calls `moxie_register_type` + `moxie_register_method` for each method
   - Add `*types.Const` -> calls `moxie_register_const` or `moxie_register_const_string`
   - Add interface type registration -> calls `moxie_register_iface`
   - The C-shared exports already exist in compile_exports.mx

2. **Remove the main-package restriction** at `builder/build.go:364`:
   ```
   // Current:
   if useBootstrap && len(pkg.CgoFiles) == 0 && pkg.ImportPath == lprogram.MainPkg().ImportPath {
   // Target:
   if useBootstrap && len(pkg.CgoFiles) == 0 {
   ```

3. **Test**: `MOXIE_BOOTSTRAP_COMPILE=1 ./moxie-new build ./bootstrap/hello/` should produce a working binary compiled entirely through compile.so.

4. **Test at scale**: `MOXIE_BOOTSTRAP_COMPILE=1 ./moxie-new build ./cmd/moxie/` - bootstrap compiling the compiler itself.

### Phase 4: Standalone Bootstrap Binary

Goal: `moxie-bootstrap` binary that compiles Moxie without the Go compiler.

1. **Fix syntax.so build**: `ConstVal` type is defined in `compile/tc_object.mx` but referenced in `syntax/nodes.mx:254`. Either:
   - Define `type ConstVal = interface{ String() string }` in syntax/nodes.mx (type alias, zero cost)
   - Or restructure so TypeAndValue doesn't need ConstVal in the syntax layer

2. **Fix LLVM 21 compat**: `LLVMConstMul` was removed in LLVM 21. The purego bindings need to handle this (conditional symbol resolution or alternative API).

3. **Wire the full chain**: The standalone binary needs:
   - Source loading (loader.so or equivalent)
   - Parsing (already in compile.so)
   - Type checking (already in compile.so)
   - SSA building (already in compile.so)
   - IR emission (already in compile.so)
   - LLVM optimization (llvmpure + libLLVM)
   - Object code emission (llvmpure target machine)
   - Linking (invoke system linker or lld)

4. **Build**: compile the bootstrap binary itself through the bootstrap chain (gen2 test).

### Phase 5: Stdlib Var Purge Completion

Independent of Phases 2-4. Can be done in parallel.

1. **Category C - Identity Sentinels** (7 vars): NoBody, DefaultServeMux, sentinelHandler, multipartByReader, ErrNotSupported, goexitPanicValue, reserved. Each becomes zero-value var + accessor function.

2. **Category B - CPU Feature Booleans** (5 vars): supportADX, hasADX, hasVX (x2), useFMA. Each becomes zero-value var + explicit `_init` function called from `initAll()`.

3. **Category A - Runtime Scalar Constants** (6 vars): xorshift32State, xorshift64State, randState, inf, ChildPipeFd, secLockdownFd. Either compiler-recognized `var X T = <const-expr>` as data-segment init, or move assignments into `run()`.

4. **Phase 9 - Enforcement**: compiler rejects `var X = expr` in non-exempt packages.

### Phase 6: defer Fix

Independent of everything else but affects correctness of any code using defer.

The deferred function linked list walk at function return is broken in the moxie runtime. Neither the Go compiler path nor compile.so path executes deferred functions. This is a runtime bug, not a codegen bug.

Fix location: likely in the runtime's `_defer` struct management or the function epilogue that walks the defer chain.

**Impact on Phase 2**: 11 real defer sites in the compiler source. 8 are scope save/restore in the type checker. If Phase 2 gen1 testing hits scope corruption, the workaround is manual inlining of scope restores (see Phase 2, Defer risk).

## File Inventory

### Bootstrap compiler source (compile/)
```
compile_exports.mx    348 lines  C-shared export wrappers, import registry
ir_emit.mx           4666 lines  LLVM IR text emitter
ssa_builder.mx       3129 lines  SSA construction from typed AST
ssa_types.mx          924 lines  SSA type definitions
parser.mx            2945 lines  Recursive descent parser
scanner.mx            854 lines  Lexer
source.mx             197 lines  Source buffer
pos.mx                176 lines  Position tracking
nodes.mx              450 lines  AST node definitions
syntax.mx              82 lines  Parse entry points
tokens.mx             143 lines  Token definitions
tc_*.mx (12 files)  ~3200 lines  Type checker
                     -----
Total:              17400 lines
```

### Parallel implementations
- `mxssa/` (12,144 lines) - SSA builder + exports, no IR emitter. Shares parser/scanner/typechecker structure with compile/ but has its own ssa_exports.mx for the standalone mxssa.so.
- `syntax/` - standalone parser .so
- `typecheck/` - standalone type checker .so
- `loader/` - source file loader .so

### Test harnesses
- `probe_compile/main.go` (15,121 lines) - 1336 tests + incremental self-compilation
- `probe_mxssa/`, `probe_syntax/`, `probe_typecheck/`, `probe_loader/` - individual module tests

### Build
```
make compile.so         # gen0 build
make test-compile       # run 1336 tests
make all                # build all .so modules
make test               # run all test suites
```

Required environment:
```
LD_LIBRARY_PATH=/usr/lib/llvm-21/lib:bootstrap/llvm-glue:$LD_LIBRARY_PATH
LLVM_LIB_PATH=/usr/lib/llvm-21/lib/libLLVM-21.so
```

The `moxie-new` binary (built May 26) is the LLVM 21 compatible compiler. The `moxie` binary (May 24) may have LLVM version issues.

## Verification Criteria

- **Phase 1 complete**: `make test-compile` passes 1336/1336 from clean committed state
- **Phase 2 complete**: gen1.so passes 1336/1336 tests AND produces identical IR to gen0 for the same inputs
- **Phase 3 complete**: `MOXIE_BOOTSTRAP_COMPILE=1 ./moxie-new build ./bootstrap/hello/` produces a working binary
- **Phase 4 complete**: `./moxie-bootstrap build ./bootstrap/hello/` works without the Go compiler
- **Phase 5 complete**: `MOXIEROOT=~/s/moxie ./moxie build -o /dev/null ~/s/smesh/` builds clean with no init() in non-exempt packages
- **Phase 6 complete**: `defer cleanup("msg")` in a test program actually executes cleanup
