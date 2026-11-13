# Phase G: the generics rewriter - implementation plan

Status: working plan. No code is changed by this document. This file is a
working artifact, not part of the change set: **delete it before committing the
implementation** (per the phase brief).

This is the first phase of the distillation described in
[docs/distillation.md](<docs/distillation.md>). It covers only the generics
rewriter (stage **G**). Later phases (interfaces, text, slices, maps, codec,
channels, core split) get their own plans.

## 0. Where this sits

```
G  generics      this plan
I  interfaces    after G, and after the placement lessons G teaches
T  text          later
S  slices        later, needs E
M  maps          later
E  codec/ABI     later, needs S/M
C  channels      later
F1 front end     later
F2 IR            later
B  LLVM driver   later
```

G and I commute in principle; G goes first because it is the smaller,
better-bounded rewrite, and because it forces the source-editing machinery
(spans, edits, imports, output tree) that every later phase reuses.

G leaves *interfaces* alone. A constraint such as `cmp.Ordered` is an ordinary
interface type; after G removes the generic declarations that used it, the
constraint type may remain in its package (possibly unused) and is phase I's
responsibility. G must not try to erase it.

## 1. Goal and definition of done

**Goal:** a command that takes a Moxie program, rewrites it into valid Moxie
with **no generic declarations and no generic instantiations left**, and whose
output compiles with the existing stage4 compiler and behaves identically to
the original program.

**Definition of done (the gate for the whole phase):**

1. `mxc lower-generics <target>` resolves the target's **entire package
   closure** and writes **only source files** to a timestamped temporary
   directory (default `${TMPDIR:-/tmp}/mxc-gen-<target>-<UTC-stamp>-<pid>`,
   `-o DIR` overrides), then prints that path and the exact build command. It
   exits 0. Every dependency is vendored under `TREE/src/<import-path>/`,
   including packages that resolve outside `MOXIEROOT`; the tree needs no
   checkout, manifest or network to build.
2. The tree is directly feedable to the normal compiler:
   `cd TREE && mxc build -a -srcdir TREE <target-import-path>` builds it with
   the real toolchain. `-srcdir` is the small source-root override that makes a
   source-only tree buildable without copying `sysroot` or a bake into it.
3. **Side by side:** the baseline binary (built from the untouched source) and
   the generated binary (built from the source-only tree) produce identical
   observable output - same exit code, same stdout, same stderr (modulo
   addresses/timings where a fixture says so), same filesystem effects, and for
   the compiler, the same output binary for a test program. This is the
   debugging oracle: the generator is made identical by making its binary behave
   identically.
4. Machine-code identity is **not** the gate. Monomorphization renames symbols,
   duplicates functions and changes section order, so byte-identical binaries
   are a bonus signal, never a failure. The harness records hashes and compares
   behaviour and, for debugging, disassembly.
5. The output contains no `TParamList` on any declaration and no instantiation
   of a generic declaration. The latter is enforced by construction: **the
   original generic declarations are deleted from the output**, so any missed
   use is a compile error, not a silent one.
6. The rewrite is deterministic in content: running it twice on the same input
   produces byte-identical trees (the temporary directory's name is not part of
   the content).
7. The compiler's own closure can be rewritten and rebuilt by the stock
   compiler, and the rebuilt compiler builds and runs a test program with the
   same behaviour as the baseline compiler.
8. Unsupported constructs fail with a diagnostic naming the construct and the
   source position. There is no best-effort mode that silently leaves generics
   behind.

Non-goal for this phase: proving a binary fixpoint. G only needs a compiler that
was *rewritten* to work; making that compiler reproduce itself is the later
self-host gate for the whole pipeline.

### 1.1 The output contract and the side-by-side oracle

The output is a **source tree and nothing else**, in a timestamped temporary
directory that defaults to `${TMPDIR:-/tmp}/mxc-gen-<target>-<UTC>-<pid>`:

- every file in it is a compiler input (`.mx`, `.c`, `.h`, `.S`/`.s`,
  `//:embed` data), vendored under `src/<import-path>/`;
- unchanged files are copied byte-for-byte; changed files are original bytes
  plus edits; instances are new `.mx` files;
- no binaries, objects, bitcode, caches, manifests, `sysroot`, runtime bake or
  symlinks;
- it is feedable to the normal compiler with one new flag, `-srcdir TREE`, which
  redirects package source resolution while the toolchain still comes from the
  real `MOXIEROOT`;
- every dependency is vendored under `TREE/src/<import-path>/`, including
  packages that resolved outside `MOXIEROOT`; no checkout, manifest or network
  is needed to build it;
- the path is printed, so the caller can build it;
- the persistent copy of each package's output lives in a new cache section,
  `lower/generics/` (§6.10), which is what later stages consume.

The reason for this shape is debugging: the generator is developed against the
compiler that already does everything in one step. The oracle is a pair of
binaries:

1. **baseline:** build the untouched source with the stock compiler, run it;
2. **generated:** build the source-only tree with the stock compiler, run it;
3. compare observable output (exit code, stdout, stderr, files written, and for
   the compiler, the binary it produces for a test program);
4. if they differ, narrow it: `-stats` and `-v` show which packages produced
   which instances and edits; the tree can be inspected and diffed against the
   original; a single package or fixture can be rewritten and compared in
   isolation; fix the generator and repeat;
5. when they match, freeze the case as a fixture.

Identical machine code is a useful extra signal but not the gate: renaming and
duplicating instances changes symbols, section order and embedded paths. The
gate is identical behaviour; the harness records hashes and, when they differ,
uses disassembly only to find the cause.

## 2. Scope

**In scope**

- generic free functions: explicit `f[T](x)` and inferred `f(x)`, same package
  and cross-package;
- generic named types: structs, interfaces, func types, and named types over
  slices/maps/chans, including nested instantiations (`Box[Box[int32]]`);
- methods on generic types, generic methods with their own type parameters, and
  method values/expressions;
- generic types used in every type position (parameters, results, fields,
  composite literals, conversions, assertions, type switches, embedded fields);
- the standard library and runtime, because a whole-link rewrite has to make
  every signature agree; the runtime's `.mx` is monomorphized like any other
  package, so no generic runtime symbol is left for a lowered caller to link
  against (the broader runtime ABI boundary is distillation §5.5). `src/runtime`
  currently declares exactly one generic, `AddCleanup[T,S]` (runtime.mx:209);
  G must erase it and the gate asserts `src/runtime` has zero generic
  declarations afterwards;
- the target's own closure under `MOXIEROOT`.

**Out of scope for G (explicitly)**

- interfaces, text, slices, maps, channels, codec - those are later phases;
- legacy: per the instruction, this phase is stage4-only. The legacy compiler
  stays able to compile the *pipeline's own source* for bootstrap, so nothing
  here may break it, but no feature is mirrored into it;
- external repositories outside `MOXIEROOT` (remote `moxie get` packages) are
  **vendored into the tree**: every package in the resolved closure is copied
  under `TREE/src/<import-path>/`, so the output is self-contained and needs no
  checkout, manifest or network at build time;
- producing an AST binary or a standalone rewriter binary. Both are deferred
  (§3 D1, §8 G7);
- incremental/cached rewriting. G is a whole-program preprocessing run.

## 3. Decisions

### D1 - start as a subcommand of `_mxc_stage4`, not a separate binary

The rewriter needs four things that live in `_mxc_stage4`'s `package main`:

- the package resolver and build-tag selection (`resolveAll`,
  `discoverPkg`, `concatSources`);
- the whole-link typecheck pre-pass and `.mxh` export data (`TypeCheckOnly`,
  `prepareDispatchIndex`);
- the only complete expression/statement checker, which is fused into the SSA
  builder (`CreatePackage`, `buildBody`);
- generic inference and body cloning (`resolveTypeArgsExpr`,
  `monomorphizeAndCall`, `genericMethodInstance`, `ensureIfaceInstMethods`).

A standalone `cmd/mxcgen` would have to import those, and they are not in a
package. Extracting the fused checker is the large enabling refactor the
distillation calls M0; doing it first would delay the generics yield by a whole
phase. So G is a **new command in the Moxie compiler**:

```
mxc lower-generics [-o DIR] [-v] [-stats] <target>
```

It is still "a code rewriter in Moxie": the compiler is Moxie, the command is a
Moxie program, and it emits Moxie source. **Write the rewriter itself without
generics**, so the G6 self-host can run G over the compiler that contains it;
otherwise the first rewrite would have to handle its own machinery before it has
been proven.

When the checker is later extracted
into a package (the core split), the command becomes a thin `main` over the same
functions and can move to `cmd/mxcgen` mechanically. Plan for that extraction
in G7, not now.

Consequence: the command's implementation lives in new files in
`_mxc_stage4/`, in `package main`. It may call any compiler internal. It must
not change the behaviour of `build`; all hooks are behind a `cctx.genMode` flag.

### D2 - no AST printer: source-preserving byte edits

The pipeline needs source output, but `pkg/syntax` has no printer, does not
retain comments, and AST nodes have no `End` position. Writing a full printer
would lose comments and pragmas and would reflow every file. Instead:

- files that do not change are **copied byte-for-byte**;
- changed files are **edited at exact byte ranges**;
- instances are emitted by **copying the original declaration's bytes** and
  applying substitutions to that copy.

This preserves comments, `//:...` pragmas, build tags, formatting, embed
directives and line numbers. It also follows the existing culture
(`legacy/mxtext`, `_mxc_stage4/ir_rewrite`) and avoids the printer that the
distillation flagged as missing.

The price is span bookkeeping (§6.3). Two small parser additions make it exact
instead of heuristic (§7.1).

### D3 - capture at the compiler's own resolution points; do not reimplement inference

The rewriter must know **every** instantiation and the concrete type arguments.
Reimplementing inference would duplicate the compiler's hardest front-end code.
Instead, run the compiler's normal SSA build in a recording mode and hook the
places where it already decides an instantiation:

- `monomorphizeAndCall` (ssa_builder.mx:11050) - free functions, explicit and
  inferred;
- `genericMethodInstance` (:7177), `tryGenericMethodCall` (:7123),
  `ensureIfaceInstMethods` (:7274) - methods and interface-forced methods;
- the instantiation points in type resolution (`tcResolveNameInline`
  ir_typecheck.mx:2251-2274, `resolveTypeAST` ssa_builder.mx:9884) - generic types
  in any type position.

The compiler's normal clone build is left on. It is what discovers *transitive*
instantiations (a generic body calling another generic with a substituted type),
and its output is discarded. The rewriter emits source alongside it.

This means G pays the generic processing cost once more in the rewrite run, and
the yield comes when the output is compiled without it. That is the expected
trade of a preprocessing phase.

### D4 - placement is chosen by nameability and import cycles, not by a fixed package

The distillation said "emit instances in the declaring package". That is the
first choice, not the rule. Source cannot name a type without importing its
package, so placement must avoid cycles:

- A generic **type** must have one identity across the program. Host it in the
  declaring package if that package can name every type argument without
  importing a package that (transitively) imports it; otherwise host it in the
  package that owns the blocking type argument. Every package that can *write*
  `B.Box[A.T]` already imports both A and B, so it can import A's instance
  without a new cycle.
- A generic **function** has no identity requirement. Host it in the caller
  package (duplicating per caller as the current compiler does), which is the
  most cycle-free choice; promote to a shared host only when safe.
- When the host is not the declaring package, the body's references to the
  declaring package's names are qualified, and unexported names get an exported
  bridge in the declaring package (`type Gen_T = t`, `func Gen_f(...) ...`).

The exact algorithm and its conservative first cut are §6.5. G0's audit decides
how much of it is needed for this tree.

### D5 - fail loud, and keep the tree for inspection

Every unsupported case is an error with file:line and a reason. Never leave a
generic declaration or a generic use behind. The run writes into a fresh
timestamped directory; on failure it prints the diagnostic and the directory
path (the partial tree is useful for debugging) and exits non-zero. It does not
pretend the tree is buildable.

### D6 - output is source only, in a timestamped temp tree

The output is **only generated source code**: every file in the tree is a source
input the compiler would accept in a normal build (`.mx`, `.c`, `.h`, `.S`/
`.s`, and the data files named by `//:embed`). Unchanged files are copied byte-for-byte;
changed files are the original bytes plus edits; instances are new `.mx` files.
There are no binaries, object files, bitcode, caches, manifests, symlinks,
`sysroot` copies or runtime bakes in the tree.

The default location is
`${TMPDIR:-/tmp}/mxc-gen-<target-base>-<YYYYmmddTHHMMSSZ>-<pid>`; `-o DIR` uses
an exact directory. The directory is kept so it can be inspected, diffed,
archived and fed to the compiler; the caller deletes it.

Layout: **every resolved package is vendored under `TREE/src/<import-path>/`**,
whatever its origin - stdlib, the target's repository, a remote checkout. The
import path is preserved, so type identity and build tags are preserved; the
tree is a complete, flat source root. A new small compiler flag, `-srcdir DIR`,
makes package source resolution use the tree while the toolchain (`sysroot`,
musl, compiler-rt, runtime bake, `jsruntime`) still comes from the real
`MOXIEROOT`. Concretely, a
`srcRoot()` helper (default `moxieRoot()`) is used by `resolveAll`/`discoverPkg`
only; `moxieRoot()` continues to answer for the toolchain, cache and host
runtime. In `-srcdir` mode the tree must win over the `$MOXIEPATH/<path>`
checkout shortcut in `discoverPkg` (main.mx:3796-3803): otherwise a vendored
remote package silently resolves to the developer's checkout. With that
priority fixed, the target is built by its resolved import path from inside the
tree. That is what makes a source-only tree buildable in normal mode without
copying anything non-source into it:

```
TREE=$(mxc lower-generics ./_mxc_stage4)     # prints the path and the target path
cd "$TREE"
mxc build -a -srcdir "$TREE" <target-import-path>
```

### D7 - delete the generic declarations

The output deletes every generic declaration it has instantiated (and every one
it has not, since nothing can use it). Replacement instances are new
declarations. This is the enforcement mechanism for DoD 4: a missed call site
fails to compile because the original symbol is gone. It also makes "no
generics" a property of the output, not a promise.

### D8 - stage4-only, no bilateral mirror

Per the instruction, the legacy compiler is not modified. The new command and
its hooks are stage4-only. The safety net is the differential test, not a
second implementation. If the command ever changes what `mxc build` emits, that
is a bug and must be reverted, not mirrored.

---

## 4. Architecture

```
mxc lower-generics [-o DIR] target
        │
        ▼
  resolveAll / discoverPkg            (existing: sources, build tags, closure)
        │
        ▼
  whole-link typecheck pre-pass       (existing: TypeCheckOnly, mxh, registry)
        │
        ▼
  cache probe                         link key from sources; hit assembles the
        │                             tree from package entries and exits
        ▼
  per-package SSA build in gen mode   (existing checker + hooks)
        ├── on generic instantiation: record request, build clone (as today),
        │   emit the instance's source into the host package buffer
        ├── on a generic use site: record a byte edit for the caller file
        └── on a type instantiation: record a type request + edit
        │
        ▼
  output writer
        ├── copy unchanged files byte-for-byte
        ├── apply recorded edits to changed files
        ├── delete generic declarations
        ├── remove orphaned imports
        ├── append zz_gen_generics.mx per package
        ├── write package entries + link index to the lower/generics cache
        └── write the source tree to TMP/mxc-gen-... and print its path
        │
        ▼
  TREE/ (source only; build with -srcdir TREE, real MOXIEROOT for toolchain)
```

### 4.1 Components

| component | file (new) | responsibility |
| --- | --- | --- |
| command driver | `_mxc_stage4/gen_main.mx` | flags, program prep, per-package gen build, summary, exit codes |
| mode state | `_mxc_stage4/gen_state.mx` | `genMode`, package outputs, requests, name registry, diagnostics |
| recording hooks | edits in `ssa_builder.mx`, `ir_typecheck.mx` | capture requests and edits while the compiler already resolves |
| instance emitter | `_mxc_stage4/gen_emit.mx` | build each instance's source from its decl bytes + recorded edits |
| source machinery | `_mxc_stage4/gen_source.mx` | line index, pos→offset, spans, edit application, import hygiene |
| type rendering | `_mxc_stage4/gen_types.mx` | canonical type key, mangled names, type→source, import manager, aliases |
| output writer | `_mxc_stage4/gen_output.mx` | source tree copy, apply edits, delete decls, generated files, path layout |
| cache | `_mxc_stage4/gen_cache.mx` | link key, package entries, link index, atomic writes, eviction |

All are `package main`, so they can call the compiler's internals directly.
Everything they do is guarded by `cctx.genMode`, which is false for `build`,
`run`, `test` and every existing command.

### 4.2 Driver flow

1. Parse flags and target.
2. Reuse the normal program preparation: `registerBuiltins`, `applyTargetFlag`,
   `resolveAll`, dedupe, cache dir, and the whole-link `TypeCheckOnly` pre-pass.
   Extract this sequence from `cmdBuild` into one function so both commands use
   the same setup (a refactor with no behaviour change; verify by comparing a
   normal build's output before/after).
3. Compute the cache link key (§6.10). On a verified hit, assemble the output
   tree from the package entries, print the path and exit; no typecheck, no SSA.
4. Otherwise set `cctx.genMode = true`, `genState.outDir`, and walk the resolved
   packages in dependency order, calling the existing per-package compile path
   with IR emission skipped. Each package's `CreatePackage` does the checking
   and body building; hooks record and emit.
5. If any diagnostic was recorded, write nothing and exit 1.
6. Finalize each package's source set, write package entries and the link
   index, assemble the output tree, and print the path and summary.

Step 3 uses the serial path first. The parallel build path (`buildPkgsParallel`,
which forks worker processes) is **not** used in G: workers would each need the
gen state and the output writer, and correctness comes before speed. Revisit
parallelism in G7 if the rewrite is slow.

**Gen mode ignores the build caches.** A cached package object would skip the
SSA build, and the instantiation requests live in that build, not in the object
file. So gen mode must:

- typecheck and scan every package in the closure, including the tier-2 packages
  the normal pre-pass skips when their `.mxh` is cached (main.mx:7983-8005);
- compile every package's SSA, ignoring `cachedBcPath` hits;
- avoid the graph-eviction path that frees per-package arenas at last use until
  the run is over, or copy every recorded type argument/name before freeing.
  The arena rule in §6.1 is the local version of this; the global version is
  "do not evict a graph that a pending request can still name".

The output tree itself is source only; it never reuses a baseline `.bc` or
`.mxh`.

### 4.3 Data model

```moxie
type genRequest struct {
    kind      int32      // genFunc | genMethod | genType
    declPkg   string     // package that declares the generic
    declName  string
    method    string     // for methods
    key       string     // canonical request key
    name      string     // generated symbol/type name
    host      string     // package that will contain the instance
    argKeys   []string
    args      []Type     // live only while the request is emitted
    state     int32      // new | building | done
    usedPkgs  []string   // packages named by the body (for placement)
    edits     []genEdit  // decl-relative edits collected while building the clone
}

type genEdit struct {
    file  string  // original file (caller domain) or "" for the declaring buffer
    start int32   // byte offset
    end   int32
    text  string
}

type genPkgOut struct {
    path    string
    dir     string   // root-relative output directory
    name    string
    files   []string
    edits   []genEdit   // edits to original files
    decls   []string    // generated declarations, sorted by name at write time
    imports map[string]string // path -> alias for the generated file
}

type genState struct {
    enabled bool
    outDir  string
    pkgs    map[string]*genPkgOut
    reqs    map[string]*genRequest      // by host + key
    names   map[string]string           // name -> key (collision check)
    current *genRequest                 // instance being built, nil outside
    diags   []string
    stats   genStats
}
```

`genEdit.start/end` are always **byte offsets**, never line/col. Positions from
the parser are converted immediately, while the buffer that produced them is
alive (§6.3). This keeps the writer free of token knowledge.

For a edit inside an instance clone, `file` is `""` and offsets are relative to
the declaration's start in the declaring package's retained buffer. The emitter
subtracts that start when applying.

## 5. Worked examples

### 5.1 Same-package function, inferred argument

Input:

```moxie
package p

func maxOf[T cmp.Ordered](a, b T) (m T) {
    if a > b { return a }
    return b
}

func use() (n int32) {
    return maxOf(int32(1), int32(2))
}
```

Output (instance hosted in `p`; the original generic decl is deleted):

```moxie
package p

func GF_maxOf_9c1f2ab34d5e6f70(a, b int32) (m int32) {
    if a > b { return a }
    return b
}

func use() (n int32) {
    return GF_maxOf_9c1f2ab34d5e6f70(int32(1), int32(2))
}
```

Notes: the type-param list is gone; `T` occurrences in the signature and body
were replaced by `int32`; the call name was replaced; everything else is the
original bytes.

### 5.2 Cross-package function with a caller-local type

Input:

```moxie
package a

import "b"

type MyInt int32

func use() (v MyInt) {
    return b.Less[MyInt](MyInt(1), MyInt(2))
}
```

Output. `b.Less`'s body cannot be hosted in `b` because `b` would need to import
`a` (a cycle: `a` imports `b`), so the instance is hosted in `a`:

```moxie
package a

type MyInt int32

func GF_Less_0a1b2c3d4e5f6071(x, y MyInt) (less bool) {
    return x < y          // copied from b, with b's names qualified if needed
}

func use() (v MyInt) {
    return GF_Less_0a1b2c3d4e5f6071(MyInt(1), MyInt(2))
}
```

The `import "b"` is gone: the rewritten call no longer names `b`, and import
hygiene (§6.7) removes it. If the copied body itself had named `b.SomeConst`,
the generated file would keep/add the import instead.

If `b.Less`'s body had called an unexported `b.slowLess`, the emitter would add
`func Gen_slowLess(x, y MyInt) (less bool) { return slowLess(x, y) }` to `b`'s
generated file and rewrite the call to `b.Gen_slowLess`.

### 5.3 Generic type with a caller-local type argument

Input:

```moxie
package b
type Box[T any] struct { V T }
func (x *Box[T]) Get() (v T) { return x.V }

package a
import "b"
type MyInt int32
func use() (v MyInt) {
    bx := b.Box[MyInt]{V: 1}
    return bx.Get()
}
```

Host in `a` (the type-argument owner), with the method emitted because the call
requested it:

```moxie
package a
type MyInt int32

type GT_Box_77aa88bb99cc00dd struct { V MyInt }
func (x *GT_Box_77aa88bb99cc00dd) Get() (v MyInt) { return x.V }

func use() (v MyInt) {
    bx := GT_Box_77aa88bb99cc00dd{V: 1}
    return bx.Get()
}
```

Again `import "b"` is removed when the rewritten declaration no longer names
`b`; if the copied type/method body needs a `b` name, the import stays.

Any other package that writes `b.Box[a.MyInt]` already imports `a` and `b`, so it
can name `a.GT_Box_...` without a new dependency. This is the invariant behind D4.

## 6. Algorithms

### 6.1 Request capture and keys

A request key is canonical and independent of spelling:

```
kind | declPkg | declName | method | argKey1 | argKey2 | ...
```

`argKey` is a recursive type key:

- basic: kind name (`int32`, `bool`, `string`, ...);
- named: `importPath.Name` plus `[argKeys]` when instantiated, plus the local
  type tag when the named type is function-local;
- pointer/slice/map/chan/func/struct: a canonical constructor spelling,
  recursively keyed; an array type is not valid Moxie and is a failure, not a
  key;
- interface: method set names and signatures, recursively keyed (anonymous
  interface type arguments are rare; if one appears, fail in G1).

Use `typeDescName` (ir_descriptors.mx:87) as the basis where it is total, since
it already encodes instantiation and package identity; append `localTypeTagOf`
(ir_descriptors.mx:608) for local types.

Instance names are exported and derived only from the key, following the
grammar in distillation §6.9:

```
G<kind>_<human>_<hash>
  kind  F (function) | T (type) | M (generic method) | A (accessor)
  human [A-Za-z0-9_], <= 24 chars, sanitized declaration/method name
  hash  16 lowercase hex of FNV-1a-64 over the length-prefixed canonical key
regex: ^G[FTMA]_[A-Za-z0-9_]{1,24}_[0-9a-f]{16}$
```

Examples: `GF_maxOf_9c1f2ab34d5e6f70`, `GT_Box_77aa88bb99cc00dd`.

- Exported, because cross-package call sites must name it.
- The canonical key is length-prefixed (`len(pkgpath) pkgpath | len(decl)
  decl | ...`) so no separator is ambiguous; `typeDescName` is the basis where
  it is total, plus `localTypeTagOf` for local types.
- Human part truncated to 24 chars; the hash is fixed-length, parsed from the
  right, so the type name cannot swallow the separator.
- Collision: keep `name -> key`; on collision, extend the hash to 32 hex and
  re-check. A remaining collision is a build error, never a silent merge. Also check the host package's existing scope
  (`hostPkg.Scope.Lookup(name)`): a user declaration may already own the name.
  Deterministic because it depends only on the key and the host's declarations.
- For functions hosted per caller, the dedupe key is `host + "|" + key`; the
  same key in two hosts is two instances (legal, distinct symbols).
- For types, dedupe is global by key, so the type has one identity; the first
  request chooses the host.

Requests are recorded in a map and, for diagnostics, in a slice. Emission
happens immediately on first request (types and functions alike), because the
type arguments are only guaranteed alive during the caller's compile. The
generated declaration text is copied into the host package's buffer, which lives
in the build arena.

**Arena rule:** every generated declaration, edit list, name and diagnostic is
copied out of the per-package compile arena into the build arena before
`CompileToIR` frees it. The request's `args []Type` is *not* copied; it is used
only during the emission call and never stored beyond it. This is the same
lifetime discipline `copyGenericsToRoot` (main.mx:5886) exists for; get it wrong
and the output contains text from a freed arena.

### 6.2 Capture points

| construct | hook | what is recorded |
| --- | --- | --- |
| explicit free call `f[T](x)` / `pkg.F[T](x)` | `tryGenericCallFromIndex` (ssa_builder.mx:11234) → `monomorphizeAndCall` (:11050) | request, use-site edit (whole `IndexExpr`) |
| inferred free call `f(x)` / `pkg.F(x)` | `tryGenericLocalCall` (:11184), `tryGenericPkgCall` (:11218) → `monomorphizeAndCall` | request, use-site edit (callee name/selector) |
| generic method call `x.M[U](...)` | `tryGenericMethodCall` (:7123), `genericMethodInstance` (:7177) | request, edit on the selector/index |
| interface-forced methods | `ensureIfaceInstMethods` (:7274) | request; no source edit (the coercion site keeps the interface call until phase I) |
| generic type in a type expression | `tcResolveNameInline` (ir_typecheck.mx:2251-2274), `resolveTypeAST` (ssa_builder.mx:9884) | type request, edit on the `IndexExpr` |
| generic type in a declaration signature | the same resolution path, during `typeCheckPkg`/`registerFunc` | type request, edit in the declaring file |
| generic function/type value (`f := F[T]`, `v := Box[T]{}`) | the type-expression path plus `buildCall` | request, edit |
| nested instantiation in a generic body | the same hooks, while `genState.current` is set | request, clone-relative edit |

To make the use-site nodes available, thread the source expression into the
helpers that currently receive only names:

- `tryGenericLocalCall(funcName string, ...)` gains `fun syntax.Expr`;
- `tryGenericPkgCall(pn *PkgName, ...)` gains the `SelectorExpr`;
- `monomorphizeAndCall(...)` gains `site syntax.Expr` and `siteKind`.

These are signature changes inside the compiler, no behaviour change.

### 6.3 Positions, spans and edits

`token.Pos` is line/column, not an offset (pkg/token/pos.mx:8-10). Edits need
byte offsets. Two additions and one helper:

**Parser additions** (small, additive, no semantic change):

- `syntax.IndexExpr.Rbrack token.Pos`, set at every construction site
  (parse_expr.mx:311, parse_type.mx:136, :461, :552). This gives the end of
  `f[T]` / `Box[T]`.
- `syntax.FuncDecl.EndPos token.Pos` and `syntax.TypeDecl.EndPos token.Pos`, set
  to the closing `}`/end of the type. This gives the declaration's copy range.
- `syntax.FuncDecl.TPLbrack`, `TPRbrack` and the same on `TypeDecl`, set around
  the type-parameter list, so removing `[T any]` is exact. (A single
  `TParamSpan` pair is enough; the parser knows both tokens.)

Verify these additions by parsing all packages and comparing ASTs before/after
(the fields are ignored by the compiler unless genMode).

**Position → offset.** For each buffer that positions refer to, build a line
index (`lineStart []int32`). `offset(pos) = lineStart[pos.Line()-1] +
pos.Col()-1`. Columns are byte columns in the scanner; identifiers and
punctuation being replaced are ASCII. If a non-ASCII byte appears inside an edit
range, fall back to scanning the source forward from the line start to the
column, counting bytes as the scanner does; the fixture suite includes a
non-ASCII comment case to pin this.

**Buffers and domains:**

- Caller files: positions come from the caller package's `cctx.tcPkgSrc`, which
  is the concatenated, const-stripped source. `stripUnusedConsts` blanks a line
  rather than deleting it, so **lines and columns are preserved**; that is what
  `remapErrorLines` (main.mx:4079) relies on today. Map `(line, col)` through
  `concatLineToFile` (main.mx:2833, backed by `cctx.concatSourceMap`,
  main.mx:2826-2830) to `(file, srcLine)`, keep `col`, then use the original
  file's line index to get the byte offset. No
  `concatSources` change and no byte-delta arithmetic are needed. Add a test
  that asserts, for every edited file, that the bytes at the computed offset
  are the identifier the AST node names.
- Declaring package buffers: `ScanGenericDecls` must retain the rewritten bytes
  it parses. `scanParse` (ir_typecheck.mx:914) currently builds `rw` and drops
  it. Change `ScanGenericDecls` to keep a copy of `rw` in `cctx.genericArena`
  (or the root arena) for packages that declare generics, keyed by package
  path. Instances copy from this buffer, so positions and text always agree.
- Clone edits: recorded as offsets in that retained buffer; when copying the
  declaration `[start,end)`, apply edits at `offset-start`.

**Edits** are `(file, start, end, text)`:

- collect per file;
- sort by start descending;
- reject overlaps unless one range fully contains another; in that case keep
  the outer replacement (it was produced by resolving the outer expression) and
  drop the inner. This happens with `Box[Box[int32]]` and with a call inside a
  replaced type expression; the outer text already accounts for the inner.
- apply with a byte buffer: copy `src[lastEnd:start]`, write text, continue.

**Span helpers** (for the few nodes without an `End`):

- identifier: start at `Name.Pos()`, end after `len(Name.Value)` bytes (validate
  the bytes match the identifier; if not, scan forward to the first non-ident
  byte and fail if it does not match).
- declaration: `EndPos` from the parser.
- type-param list: `TPLbrack`/`TPRbrack` from the parser.
- If a span is missing (older AST, synthetic node), fail with a diagnostic; do
  not guess.

### 6.4 Type rendering

`renderType(t Type, host string, out *genPkgOut) string` produces source spelling
in the **host package's** context:

- `Basic`: name.
- `Named`:
  - no `TArgs`: if its package is the host, `Name`; otherwise `alias.Name`,
    adding an import for the package to the generated file. If the named type is
    unexported outside its package, request an exported alias in its own package
    (`type GenArg_<hash> = name`) and render that alias.
  - with `TArgs`: render as the **instance name** from the request (types are
    always erased before they are named), recursing to ensure the request
    exists.
- `Pointer`: `*` + render(base) (parenthesize when needed).
- `Slice`: `[]` + render(elem). `Array`: **not a Moxie type** - fail with a
  diagnostic (a leftover array declaration must be removed by S, not printed).
- `Map`: `map[` + key + `]` + value. `Chan`: `chan ` / `<-chan ` / `chan<- `.
- `Signature`: `func(params) results` for func-typed type arguments.
- `TCStruct`/`TCInterface` anonymous: render inline; if a field/method names an
  unexported foreign type, alias it too. G1 may reject anonymous type arguments
  behind a flag if the inline renderer is not ready.
- `TypeParam`: never at emission; if seen, fail ("instance still parameterized").

Import manager per generated file: path → alias. Reuse the declaring package's
alias when the body text comes from it (so copied selectors still resolve);
otherwise choose `pkg`, `pkg2`, ... deterministically, avoiding names used by
generated declarations in that file. Emit a sorted import block.

Package qualification of the declaring package's own names (when
`host != declPkg`): the compiler resolves each identifier to an object; record
`(namePos, object)` for every reference to an object in the declaring package
during the clone build, and rewrite it to `declAlias.Name` (exported) or to the
bridge name (unexported). The alias for the declaring package is added to the
host's generated file.

### 6.5 Placement and import cycles

The problem, stated once: **source names a type only by importing its package.**
If the declaring package `B` instantiates with a type from `A`, and `A` imports
`B`, hosting the instance in `B` would require `B` to import `A` and close a
cycle. The compiler has no such problem because type identity is internal; the
source rewrite does.

The invariant that makes this solvable:

> Any package that can *write* `B.Box[A.T]` already imports both `A` and `B`.

So hosting a type instance in `A` adds no new dependency for those packages;
they already import `A`. Hosting in a package that does **not** import `B` would
force a new edge and must be checked for cycles.

**Host selection for a type instance:**

```
needed = {declPkg} ∪ {packages of named types and body-referenced objects}
candidates = [declPkg] + argOwnerPkgs + [callerPkg]
for h in candidates:
    if every n in needed is h, or already imported by h, or
       adding h -> n does not close a cycle (n does not transitively import h):
        choose h
fail with the cycle spelled out
```

Preference order matters for identity and cost:

1. **declaring package** - no qualification, no bridges, one instance;
2. **first argument-owner package** - the "naming users already import it"
   invariant, qualification of `B`'s names and bridges for unexported ones;
3. **caller package** - last resort; other users may not import it, so the
   instance may not be reachable for them. Only choose it when the request's
   only users are in the caller (which the audit can identify later; G1 uses it
   for functions, not types).

Cycle test: reachability in `pkgInfo.imports` (transitive). Cache it per build.

**Host selection for a function instance:** no identity constraint, so:

- default: caller package; duplicated per caller;
- if the caller cannot import a needed package (a cycle), try the declaring
  package, then the first argument owner;
- if none works, fail.

**Visibility is the hard limit of source-level monomorphization.** Moving a
declaration out of its package changes what can see its unexported members. This
is not a bookkeeping problem; it can make an otherwise valid program
inexpressible:

- a *type* instance `Box[A.T]` hosted in `A` has its unexported fields declared
  in `A`. If `B` (the declaring package) has non-generic code that reads
  `Box[int32].v`, that code can no longer compile: `v` is now private to `A`.
  The same applies to unexported methods;
- a *function* body copied out of `B` may construct a `B` struct with unexported
  fields or call an unexported method. An alias gives the host a name for the
  type, but not access to its private members.

Therefore visibility is a placement constraint, checked before cycles:

1. if `B` has any use that depends on unexported access to the instance, or the
   body uses unexported fields/methods of `B` types, the instance **must** be
   hosted in `B`;
- if `B` can host it (no import cycle from the type arguments), do so;
- if `B` cannot host it, **fail** with a diagnostic naming the private member
  and the cycle. Do not "solve" it by exposing the field or by rewriting it to
  `unsafe`; both change the program.

The G0 audit must classify every request on this axis:
`bodyUsesUnexportedFields`, `declPkgUsesPrivateInstanceMembers`,
`bodyUsesUnexportedMethods`. If the count of unsolvable cases is non-zero, the
phase's scope must be renegotiated before G1's cross-package work, because no
amount of edit machinery fixes a visibility change.

**Bridges for unexported names.** When `host != declPkg` and the above check
allows it, the copied body may refer to `B`'s unexported types, funcs or consts:

- type: emit `type GenArg_<hash> = name` in `B`; use `B.GenArg_<hash>`;
- func: emit an exported wrapper with the same signature in `B`; use it;
- const: emit `const GenArg_<hash> = name`;
- var: **fail** in G1. A mutable package global cannot be bridged by a copy;
  a getter/setter bridge is a language change in the body's semantics. The
  audit will show whether generic bodies touch package vars (unlikely).
- method on an unexported type: the type alias plus the method call works if
  the method is exported; an unexported method needs an exported wrapper - and
  if the wrapper is used outside `B`, that is already an unexported-access
  case above, so it must be justified by the audit.

G0's audit must report every request where `host != declPkg` and the body
references an unexported `B` name, so the bridge work is scoped before it is
built.

### 6.6 Transitive instantiations and type-parameter arguments

Generic bodies instantiate other generics with types derived from their own
parameters:

```moxie
func F[T cmp.Ordered](x T) (v T) { return maxOf(x, T(0)) }
```

The compiler builds F's clone with `typeSubst{T: int32}`, and the inner
`maxOf` request arrives with `T` already substituted. Two rules:

1. Every request's type arguments are passed through the current
   `fb.typeSubst` (a helper `genConcreteType(t, subst)`) before keying and
   rendering. A `TypeParam` in an argument means substitution did not happen;
   fail rather than emit a still-generic instance.
2. A type expression like `Box[T]` inside a body becomes a type request for
   `Box[<concrete T>]` and its source edit replaces the whole `IndexExpr`. The
   emitter renders the replacement from the *substituted* argument, never from
   the source text `T`.

The normal clone build is what walks nested bodies. The recorder marks a
request `building` before the clone body is built and `done` after its source is
emitted. A recursive reference sees `building` and records the name without
re-entering emission, so recursion terminates.

The same guard applies to types: a recursive `type Node[T] struct { next
*Node[T] }` emits `type GT_Node_... struct { next *GT_Node_... }`. The name is
assigned before emission, so a self-reference is replaced with the in-progress
name, not a fresh request. Mutually recursive generic types (`A[T]` holds
`B[T]`, `B[T]` holds `A[T]`) work for the same reason as long as both names are
assigned before either body is emitted; process type requests in dependency
order or assign names on first sight.

**Bracket the clone build with the request.** `genRecord` saves
`genState.current`, sets it to the new request, and returns a token; the caller
(the compiler's instantiation function) builds the clone body and then calls
`genEmit(token)`, which emits and restores `current`. Every edit recorded while
`current` is set belongs to that request's clone; edits recorded with no
`current` belong to the caller's own source files. Get this wrong and a nested
instantiation's edits land on the wrong body.

**Nested requests inherit the host of the instance being emitted.** This is
easy to get wrong: the compiler's clone carries `Pkg = caller`, but the body
text is copied into the *host* package, which may not be the caller (§6.5). If a
nested instance were emitted in the caller while the text lives in the host, the
copied call would name a symbol in the wrong package. So:

- `genState.current.host` overrides the compiler's `targetPkg` for hosting and
  naming while a clone is built;
- every nested generic call/type in that body is requested with
  `(host, key)`, not `(caller, key)`; duplicates across hosts are expected;
- the use-site edit inside the clone is rendered unqualified when the nested
  instance's host is the same as the current host, and qualified otherwise;
- a nested *method* instance lives with its receiver type's host, and the call
  stays `x.M()` (or `x.M_<hash>()` for a generic method); methods are never
  package-qualified.

**Qualifying the declaring package's names.** When `host != declPkg`, every
reference in the copied body to a *package-scope* object of `declPkg` must be
qualified (or bridged). The compiler resolves each identifier to an object, so
the recorder can tell the difference:

- local variables, parameters, results, imported names: leave untouched;
- package-level func/type/const/var of `declPkg`: rewrite to
  `declAlias.Name` (exported) or to the bridge name (unexported);
- package-level names of other packages: they are already qualified in the
  source (`pkg.X`); preserve the declaring package's import alias in the host's
  generated file;
- **dot imports**: an identifier that resolves to another package's object
  through a dot import has no qualifier in B's text. It must be rewritten to
  `alias.Name` and the generated file must import that package normally. The
  recorder detects it from the resolved object's package, not from the text.

If two instances in one generated file need the same import path under
different aliases, unify them: pick one alias and rewrite the copied selector
names in the instance that used the other. This is deterministic if instances
are processed in sorted order.

### 6.7 Removing generic declarations and orphaned imports

After all edits are recorded, the writer:

1. deletes every generic declaration:
   - `FuncDecl` with `TParamList`;
   - `FuncDecl` with a generic receiver (`Box[T]` in `Recv.Type`);
   - `TypeDecl` with `TParamList`;
2. deletes the original generic type's methods (they are generic-receiver
   decls);
3. extends each deletion upward over the declaration's **leading pragma and doc
   comment block** (contiguous `//...` lines immediately above, stopping at a
   blank line or the previous declaration). Leaving `//:build` or `//:linkdata`
   behind would attach it to the next declaration and silently change the
   build. This is not cosmetic;
4. handles grouped declarations: a generic `TypeDecl` inside `type ( ... )` is
   deleted from the group; an empty group is deleted entirely;
5. keeps `src/builtin/builtin.mx` untouched: its generic-looking declarations
   (`max`, `min`, `clear`) are bodyless compiler builtins, not instantiations.
   The hooks never request them (they resolve as `SSABuiltin`), but the removal
   pass must explicitly skip a bodyless declaration to avoid deleting the
   builtin signatures;
6. re-parses each changed file and removes imports whose local name has no
   selector use left. This is required: a generic function's only use of `cmp`
   may be its constraint, and deleting the function orphans the import.
   - keep blank (`_`) and dot (`.`) imports;
   - for a single import, delete its line including the newline;
   - for a grouped import, delete the spec's line range;
   - if unsure, keep the import and let the compiler report it — a loud failure
     is acceptable, a silent wrong tree is not.

The deletion is what makes a missed use a compile error. It also means the
output cannot accidentally still depend on the compiler's generic support.

### 6.8 Generated files, the source-only tree, and the build

Per package, one generated file:

```moxie
// Code generated by mxc lower-generics. DO NOT EDIT.
// Source: <declPkg>/<decl> instantiations

package <name>

import (
    "path"
)

<declarations sorted by generated name>
```

- File name `zz_gen_generics.mx`, or `zz_gen_generics_<n>.mx` if that name is
  taken in the package directory.
- No build tags: the tree is generated for one resolved target (the build tags
  were applied by `discoverPkg`). A target change needs a re-rewrite.
- Imports sorted, aliases deterministic.
- Declarations sorted by name so the run is reproducible regardless of request
  order.

**The tree is source only and fully vendored.** Every resolved package is
written under `TREE/src/<import-path>/`, including packages that resolve outside
`MOXIEROOT` - remote checkouts, the target's repository, stdlib, the runtime.
Preserving the import path preserves type identity and every package-path rule;
the flat `src/` root is the same layout `discoverPkg` already resolves
(`root/src/<path>`, main.mx:3811-3816), so the compiler reads it without a
manifest.

Per package:

- copied files: every selected `.mx`, every `.c` and `.h`, every `.S`/`.s`,
  and every data file named by `//:embed`;
- changed `.mx` files: original bytes plus edits;
- one generated instance file, `zz_gen_generics.mx`;
- **nothing else**: no `sysroot`, no runtime bake, no object/bitcode files, no
  cache, no `.gen-manifest`, no symlinks. If it is not a compiler input, it does
  not go in the tree.

Why this layout rather than repo-relative paths:

- it is a **complete vendored closure**; no checkout, manifest or network is
  consulted at build time;
- import paths are preserved, so `type` identity, build constraints and the
  package-path restrictions see the same names as the original build;
- one rule for stdlib, first-party and remote packages; no "outside root"
  exception;
- a package whose import path is literally under `vendor/` lands at
  `TREE/src/vendor/<path>`, which the loader already understands.

This **is** the vendoring: the import path is the directory path, every
dependency is present, and no manifest, pin, checkout or network is consulted.
A package that carries a prebuilt `.bc`/`.o` input cannot be represented as
source; fail with a diagnostic naming it rather than silently linking from the
real root (no package in the compiler's own closure does this).


The one resolution change needed: in `-srcdir` mode the tree must be consulted
**before** the `$MOXIEPATH/<path>` checkout shortcut (main.mx:3796-3803) and
before the remote branch, or a vendored remote package silently resolves to the
developer's checkout. That is a priority change inside `discoverPkg`, not a new
layout format.

**Default location.** `${TMPDIR:-/tmp}/mxc-gen-<target-base>-<UTC>-<pid>`, for
example `/tmp/mxc-gen-mxc_stage4-20261109T074241Z-4242/`. `-o DIR` uses an exact
directory instead. The path is printed to stdout on success (and to stderr on
failure, so the partial tree can be inspected). The tree is kept; the caller
removes it.

**Building it in normal mode.** A new small flag, `-srcdir DIR`, makes package
source resolution use `DIR` while the toolchain (`sysroot`, musl, compiler-rt,
the runtime bake, `jsruntime`) still comes from the real `MOXIEROOT`:

```
# baseline
MOXIEROOT=/path/to/real/moxie /path/to/real/mxc build -a -o /tmp/base ./_mxc_stage4

# generated
TREE=$(MOXIEROOT=/path/to/real/moxie /path/to/real/mxc lower-generics ./_mxc_stage4)
cd "$TREE"
MOXIEROOT=/path/to/real/moxie /path/to/real/mxc build -a -srcdir "$TREE" \
    -o /tmp/gen <target-import-path>
```

`<target-import-path>` is the path the resolver recorded for the target (the
generator prints it with the tree path), not the original directory spelling.
Every dependency resolves under `TREE/src`, so the build is self-contained.

The tree is complete enough that this produces a binary; that binary is the
other half of the side-by-side comparison. The generator does **not** run the
build itself: keeping generation and compilation separate makes a rewrite
failure distinguishable from a build failure, and lets the same tree be built
with different compilers or flags.

**Runtime.** If `runtime` is in the resolved closure, its source is rewritten
and compiled from the tree like any other package. If an app relies on the
prebuilt bake (the closure does not contain `runtime`), the harness must point
`RUNTIME_BC` at a bake built from the rewritten runtime once
(`mxc build-runtime -o /tmp/gen-runtime.bc` with `-srcdir "$TREE"`); the
generator never writes binaries into the source tree. For the compiler's own
build, runtime is in the closure and this does not arise.

**No sidecar metadata.** Statistics go to stderr (`-stats`), diagnostics to
stderr, the path to stdout. Nothing in the tree is a manifest of the run; the
tree is exactly what the compiler would have read from the original root.

### 6.9 Determinism

- names: key-derived (§6.1);
- file contents: declarations sorted by name, imports sorted;
- edits: sorted, applied descending;
- no timestamps, no absolute paths, no run counters;
- output tree file order has no effect on content.

A test rewrites twice and compares trees byte-for-byte.

### 6.10 The de-genericised package cache

The output tree is disposable and target-specific. The **cache** is persistent
and is what later stages consume. It stores the final simplified Moxie source
for each package after G, so a warm run and every downstream stage have
"nothing more to do" for generics.

**Location.** A new section under the compiler's existing cache root:

```
<moxieCacheDir()>/lower/generics/
    pkg/<content-hash>/src/<import-path>/...   # one package's final source set
    link/<link-key>                            # text index: pkgpath contenthash ...
```

`moxieCacheDir()` already includes the compiler version/hash/build id, the
target OS and arch (main.mx:1065-1070), so those are not repeated in the key.

**Scope: every package in the resolved closure, stdlib included.** The cache is
not a user-package cache. It contains a package entry for:

- every standard-library package the closure reads (`src/...`), because the
  generator rewrites their generic declarations and the vendored tree must
  contain them;
- the runtime when it is in the closure (a compiler build, or an app whose
  import graph reaches it), for the same reason;
- every first-party and vendored remote package;
- `src/builtin` if the closure reads it (its bodyless generics are left alone,
  but the package source is still part of the tree).

It does **not** contain:

- `unsafe`, which has no source package - the compiler implements it
  internally;
- compiler-synthesized packages that are never read from a directory;
- toolchain artifacts: `sysroot`, musl, compiler-rt, the runtime bake. Those
  are not packages; they come from the real `MOXIEROOT` at build time and are
  never copied into the tree or cached as source.

The consequence for space is that **stdlib dominates the number of entries**,
not the user code. Two things keep it bounded:

1. package entries are content-addressed, so the same stdlib output is stored
   once per compiler generation/target even when many links use it;
2. the cache root is per compiler build (`moxieCacheDir()` includes
   `compilerHash`/`compilerBuildID`), so a compiler rebuild starts a fresh
   generation and the old one is pruned as a whole. A stdlib source edit under
   the same compiler build changes the source hashes and therefore the link and
   package hashes.

If the stdlib share is still too large, the later optimization is a **base
layer**: stdlib packages whose G output carries no link-specific hosted
instance can be stored once under `lower/generics/base/<stdlib-hash>/` and
overlaid by link-specific entries for the few packages that do receive
instances. Do not build that until G6 measures the actual size; the
content-addressed store already dedupes the common case.

Future stages get sibling sections (`lower/iface/`, `lower/slice/`, ...); the
`lower/` prefix is the distillation cache, distinct from the object cache
(`pkg/`). **A stage consumes its predecessor's entries, never the original
sources**: stage I's input is the `lower/generics` package entries, stage T's is
`lower/iface`, and so on. That is what makes "the final simplified Moxie output
for every step, with nothing more to do" true: the chain composes through the
cache, and each link key is computed from the previous stage's output hashes,
not from the sources the first stage saw.

**Two levels, deliberately.**

- **Package entries are content-addressed.** A package's final source set (all
  its selected files after G: copied, edited and generated) is written to a
  temp dir, hashed, and renamed to `pkg/<hash>/`. Identical package output
  across different links is stored once. This is the thing downstream stages
  and the user's tooling can read as "the de-genericised package".
- **Link entries are tiny.** `link/<link-key>` maps `pkgpath -> content-hash`
  for one whole closure, plus the stage version and the target. The tree is
  assembled from the package entries, not stored as a second copy.

**Why the link key is whole-link for now.** A package's G output depends not
only on its own source but on the instantiation *requests* other packages make
into it (an instance hosted there). Before the rewrite, the only sound way to
know that set is to know the whole closure. So:

```
link-key = hash( stage-version, target, tags,
                 sorted[ (pkgpath, source-hash, sorted(import-paths)) ] )
```

The dependency edges, not just the paths, are in the key: hosting and placement
depend on the import graph (cycles), so two closures with the same files but
different edges must not share an entry. `tags` covers `-tags` values that can
select different files or change build-constraint evaluation.

It is computable immediately after `resolveAll`, before any typecheck or SSA, so
a hit skips the entire G run. A one-line change anywhere in the closure misses;
that is the conservative price. Once the pipeline exists, narrowing this to a
per-package specialization fingerprint (the analogue of `pkgHash`'s dispatch
fingerprint, main.mx:2488) is an optimization, not a correctness change. Do not
start there.

**Flow.**

1. Resolve the closure (sources, hashes).
2. Compute `link-key`; read `link/<key>`. If present and every referenced
   package entry exists and verifies, copy those package dirs into the
   timestamped output tree, print the path, exit 0. No typecheck, no SSA, no
   emission.
3. Otherwise run G as planned. A package's output is **not** final when its own
   SSA build ends: a caller compiled later can host an instance in it. Only
   after every package in the closure has been built (all requests known) is
   each package's source set final. Then write each set to a temp dir, hash it,
   `rename` into `pkg/<hash>/` (atomic; a crash leaves a temp dir, never a
   partial entry).
4. Write `link/<key>` last, atomically. A reader that finds it can trust every
   package hash in it; a missing or mismatched entry is a miss, not an error.
5. Copy/assemble the tree into the timestamped output directory.

**What is written is source, exactly what the next stage reads.** No objects, no
`.mxh` (the next stage typechecks the source), no binaries. A package entry is
byte-identical to what a cold run would produce; a test asserts that by
comparing a cached assembly with a fresh rewrite.

**Invalidation.** Stage version (a constant bumped when the emitter changes),
compiler build id (already in the cache path), target (already in the path),
every source file in the closure, and the dependency import set. The generator's
own code changes therefore require a stage-version bump; the plan makes that a
reviewed one-line change, not a cache-key accident.

**Eviction and space.** Package entries are immutable and shared; link entries
are bytes. Add a size cap for `lower/` (default e.g. 512 MB, override by env)
and LRU-prune package entries not referenced by any retained link, oldest link
first. The existing generation pruning (`pruneCacheGenerations`) handles old
compiler builds; `lower/` needs its own sweep. The user accepted that this can
grow: it is source, and it dedupes across links.

**Flags.** `-no-cache` (rewrite, do not read or write), `-refresh` (rewrite and
replace the entry), `-cache-key` (print the computed link key for debugging).
The default is read/write. The output tree is always written, cached or not.

**Threading and concurrency.** The compiler's cache is shared across processes;
package entries are content-addressed and immutable, so concurrent writers race
to the same rename harmlessly. The link index is written to a temp file and
renamed; the last writer wins with an identical key. No locks are needed for
correctness; a lock only avoids duplicate work.

**Why not just cache the /tmp tree?** Because it is target-specific and
ephemeral; the cache is per target already (path) but must survive and be
consumed package-by-package by the next stage. The link index is the bridge.

**Incremental scope.** The whole-link key is correct but coarse: one changed
source misses the link and reruns G. That is acceptable for a single stage that
is the first one. Downstream stages use per-package keys over the package
outputs, so an unchanged package's G output does not force I/T/S/M/E/C to rerun
for it; the hash barrier in distillation §6.10 is the rule. Do not attempt a
finer G key until a specialization fingerprint exists.


---

## 7. Hooks and compiler changes

### 7.1 Parser additions (additive only)

| file | node | field |
| --- | --- | --- |
| pkg/syntax/nodes.mx:154 | `IndexExpr` | `Rbrack token.Pos` |
| pkg/syntax/nodes.mx:75 | `FuncDecl` | `EndPos, TPLbrack, TPRbrack token.Pos` |
| pkg/syntax/nodes.mx:56 | `TypeDecl` | `EndPos, TPLbrack, TPRbrack token.Pos` |

Set them at: parse_expr.mx:311, parse_type.mx:136, :461, :552 (IndexExpr);
parse_decl.mx:476 (FuncDecl), :298 (TypeDecl). The parser already has the
closing tokens in hand.

No existing code reads these fields. A parse-all test compares declaration
names, signatures and body instruction counts before/after to prove the parser
change is inert.

### 7.2 Caller position mapping

No `concatSources` change is needed for G. Caller edits use the existing line
map + the original file's line index (§6.3). If a later rewrite introduces or
removes lines, the map must grow byte deltas then; G's rewrites (identifier,
bracket, declaration spans) do not change line counts.

### 7.3 Source retention

`ScanGenericDecls` (ir_typecheck.mx:942) keeps a copy of the rewritten source
(`rw` from `scanParse`, :914) for packages that declare generics, in
`cctx.genericArena`, keyed by package path. A package without generics keeps
nothing. Memory cost is bounded to generic packages, as the AST retention
already is.

### 7.4 Hook table

| location | change | purpose |
| --- | --- | --- |
| ssa_builder.mx:11050 `monomorphizeAndCall` | compute the request key/name; call `genEnter/Exit`; thread `site syntax.Expr` | free-function requests and their use-site edits |
| ssa_builder.mx:11184 `tryGenericLocalCall` | accept the callee expression from `buildCall` | inferred/explicit local calls |
| ssa_builder.mx:11218 `tryGenericPkgCall` | thread the `SelectorExpr` | qualified calls |
| ssa_builder.mx:11234 `tryGenericCallFromIndex` | record the `IndexExpr` span | explicit calls |
| ssa_builder.mx:7177 `genericMethodInstance` | record method request; set `current` while building | methods on generic types |
| ssa_builder.mx:7123 `tryGenericMethodCall` | thread the selector/index node | generic method calls |
| ssa_builder.mx:7274 `ensureIfaceInstMethods` | record forced method requests | interface satisfaction |
| ssa_builder.mx:9884 `resolveTypeAST` | record type requests and `IndexExpr` edits | generic types in bodies |
| ir_typecheck.mx:2199/2256-2274 `tcResolveNameInline` instantiation | record type requests/edits during declaration checking | generic types in package decls/signatures |
| ssa_builder.mx:9884 / :2275/:2297 | apply `typeSubst` before keying (`genConcreteType`) | transitive/parameterized args |
| ssa_builder.mx:5922 `buildCall`, :6807 `buildSelector`, :6870 `buildMethodValue` | pass the source node; record value-position edits | function/method values |
| ssa_builder.mx:1499 `buildBody` | set/clear `genState.current` around clone builds | clone-relative edits |
| main.mx:2826 `srcSpan` / `concatSourceMap` | map line/col to original files for edits | exact caller-file edits |
| ir_typecheck.mx:942 `ScanGenericDecls` | retain source | clone copying |
| ir_typecheck.mx:1314 `CompileToIR` | in gen mode, stop after `CreatePackage` (skip emitter) | rewrite run |
| main.mx:7554 `main` | dispatch `lower-generics` | command |
| main.mx:3999 `resolveAll` / :3780 `discoverPkg` | add `srcRoot()` (default `moxieRoot()`, `-srcdir` overrides) for package resolution only; toolchain/cache still use `moxieRoot()` | build a source-only tree in normal mode |
| main.mx:8427 `buildPkgsSerial` | gen mode calls a serial walk with the writer | driver |

`genMode` is a field on `cctx` and `buildState`; all hooks are `if
cctx.genMode { ... }` and return immediately otherwise. A normal build must
produce identical IR before and after this phase; the harness compares its
output on a fixture to prove it.

## 8. Work breakdown

Each step is independently landable and testable. "Gate" is what must pass
before the next step starts.

### G0 - scaffolding and audit (no rewriting yet)

- **G0.1** Command skeleton: `lower-generics`, `-o`, `-v`, `-stats`, target
  parsing, timestamped default directory, path printed on success; extract the
  program-preparation sequence from `cmdBuild` into one function; create the
  source tree by copying every selected file unchanged. Add the `-srcdir DIR`
  source-root override to package resolution (toolchain paths still come from
  `MOXIEROOT`).
  - Gate: the unchanged tree builds with `cd TREE && mxc build -a -srcdir TREE
    <target-import-path>` and runs identically for a no-generics fixture and for
    `_mxc_stage4`; the tree contains only compiler inputs (no binaries, caches,
    sysroot, bake); `git status` shows no changes outside the new files.
- **G0.2** Parser span fields, caller line-map helpers, generic source
  retention, line-index/position helpers. Add a debug flag
  `-reject-generics` to the base front end: it errors on any `TParamList` (and,
  once available, any instantiated `Named`), so "the output has no generics" is
  checked by the compiler, not by grep.
  - Gate: parse every package in `src/`, `pkg/`, `_mxc_stage4`; ASTs equivalent;
    a normal build's IR is unchanged; `-reject-generics` accepts the baseline
    and rejects a generic fixture.
- **G0.3** Cache skeleton: `lower/generics` package store and link index;
  compute the link key from the resolved closure; on a hit assemble the output
  tree from package entries (in G0 the identity tree is the cached content);
  `-no-cache` and `-refresh`; size cap and sweep. Gate: two runs of the identity
  tree, the second a verified cache hit, with byte-identical trees.
- **G0.4** Bypass flag: `mxc build -pipeline=off|generics` (and
  `MOXIE_PIPELINE`). `off` is the monolithic compiler; `generics` runs G and
  compiles the generated tree. This exists from M0, not after the first stage
  breaks something; every prefix is differential-tested. (Distillation §6.11.)
- **G0.5** Audit mode: record requests without emitting, and print a report:
  kind, declaring package, caller package, type args, arg packages, whether the
  declaring package can name them, cycle risk, same/cross package, and the
  visibility flags that decide placement: `bodyUsesUnexportedFields`,
  `bodyUsesUnexportedMethods`, `declPkgUsesPrivateInstanceMembers` (§6.5). Run
  it over the compiler's closure and over the fixtures.
- **Gate G0:** the report answers the placement and bridge questions in §6.5;
  the identity tree builds; the cache hit/miss paths are correct; no behaviour
  change in `build`.

**Deliverable:** a command that copies the world and tells us exactly what the
generics rewrite must handle. This is the "measure before cutting" step.

### G1 - free functions, explicit type arguments

- **G1.1** Key/name/request registry; dedupe; request state machine.
- **G1.2** Function instance emitter: copy the decl, rename it, remove the
  type-param list, apply type-param substitution edits; host in the caller
  package.
- **G1.3** Explicit use-site edits: `f[T](x)`, `pkg.F[T](x)`, `f := F[T]`.
- **G1.4** Cross-package qualification: the declaring package's own names
  qualified in the host; imports carried into the generated file; conservative
  host choice (caller first, declaring package if the caller cannot import a
  needed package, subject to the visibility constraint in §6.5).
- **G1.5** Delete generic declarations; remove orphaned imports.
- **G1.6** Fixtures: same-package explicit; cross-package explicit with a
  caller-local type; generic function value.
- **G1.7** Write package entries and the link index on a miss; verify on
  read; a second run must hit and produce a byte-identical tree; a changed
  source must miss.
- **Limit:** G1 (and G2) only accept requests whose substituted signature and
  body mention no *generic named type* that has not been erased. A generic
  function like `func F[T](b Box[T])` waits for G3. The recorder must detect
  the still-generic type and fail with "generic type instance not implemented
  until G3", not emit a half-erased body.
- **Gate G1:** the fixtures build from the generated tree (`-srcdir`), run
  identically, and contain no `TParamList`; a missed site fails the build (test
  by temporarily disabling one edit in a fixture harness variant); a
  generic-type request fails loudly.

### G2 - inference

- **G2.1** Thread source nodes through the call helpers; record inferred-call
  edits (callee identifier or selector).
- **G2.2** Function values and passing instances as arguments.
- **G2.3** Transitive nested calls in generic bodies; `genConcreteType`; the
  `building`/`done` recursion guard.
- **G2.4** Fixtures: `cmp.Less`-style inferred calls; a generic calling a
  generic; recursion.
- **Gate G2:** the fixtures pass; the audit's inferred requests all have edits;
  no `TypeParam` survives in any emitted instance.

### G3 - generic types

- **G3.1** Type request capture in declaration checking and body type
  resolution; key/name; dedupe by key.
- **G3.2** Type declaration emitter (structs, interfaces, func types, named
  slices/maps/chans): copy, rename, remove params, substitute fields, rewrite
  nested type instantiations.
- **G3.3** Type-expression edits in every position: var/const/type decls,
  parameters/results, fields, composite literals, conversions, assertions, type
  switches, embedded fields, slice/map/chan element types, pointers.
- **G3.4** Host selection per §6.5 (declaring package first, then arg owner);
  cycle test; anonymous type argument handling (inline or reject).
- **G3.5** Exported aliases for unexported named type arguments.
- **G3.6** Fixtures: `Box[int32]`; `Box[MyInt]` cross-package; nested
  `Box[Box[int32]]`; recursive `Node[T]`; mutually recursive types; generic
  interface type; generic type in a field/map/slice; generic type alias
  (expected diagnostic unless handled).
- **Gate G3:** fixtures pass; types have one identity (a value can cross a
  package boundary); no aliases collide.

### G4 - methods and generic methods

- **G4.1** Method requests from `genericMethodInstance` and
  `tryGenericMethodCall`; receiver type substitution and instantiation rewrite.
- **G4.2** Emit method instances into the host of their type; method sets stay
  coherent.
- **G4.3** Generic methods with their own type parameters: request/mangle/edit
  the selector.
- **G4.4** Interface-forced methods (`ensureIfaceInstMethods`): emit all methods
  an interface conversion needs; after G the interface call itself is still an
  interface call (phase I), but the concrete methods must exist for the
  interface check to pass.
- **G4.5** Method values/expressions; `String()` methods injected on generic
  types (treat injected methods as ordinary generic-receiver methods).
- **G4.5a** Embedded generic types and promoted methods: a struct embedding
  `Box[T]` resolves promoted methods through the outer type's method set. When
  the outer type is non-generic, the builder may never call
  `genericMethodInstance`, so no request is recorded and the output's instance
  lacks the method. Force method requests for every promoted method the outer
  type uses (record it during method-set construction), or emit all methods of
  a type instance whose methods are promoted. Audit this before G4.
- **G4.6** Fixtures: methods on `Box[T]`; generic method `M[U]`; type satisfying
  `fmt.Stringer`; method value.
- **Gate G4:** fixtures pass; no method set regression under `mxc build` of the
  output.

### G5 - cross-package edge cases and bridges

- **G5.1** Exported bridges in the declaring package for unexported types,
  funcs and consts referenced by a hosted body; rewrite references.
- **G5.2** Package vars in generic bodies: fail with a diagnostic (G1 policy),
  or implement a getter/setter bridge if the audit shows it is needed.
- **G5.3** Local (function-scoped) type arguments: lift the type to package
  scope with a generated name, or fail. Decide from the audit.
- **G5.4** Unexported generic declarations; name collisions; duplicate
  instances; aliases that already exist.
- **G5.5** Pragmas on generic declarations: strip `//:build` from generated
  copies; reject `//:linkname`, `//:extern`, `//:wasmimport` on a generic decl
  (per-instance semantics are undefined); keep `//:inline`.
- **G5.6** Skip `src/builtin` bodyless generics; verify `min`/`max`/`clear`
  still resolve. Any *other* bodyless generic declaration (an extern/FFI
  signature) cannot be instantiated at source level: fail with a diagnostic
  naming it, rather than emitting a call to a declaration with no body.
- **G5.7** Fixtures: unexported type arg; unexported helper call; local type
  arg (expected pass or documented failure); generic function with `//:inline`;
  builtin usage.
- **Gate G5:** the audit over the compiler's closure reports zero unsupported
  requests.

### G6 - whole closure, self-host and performance

- **G6.1** Rewrite the compiler's closure into a tree, build it with the stock
  compiler via `-srcdir`, and compare the result with the baseline.
- **G6.2** Use the rebuilt compiler to build and run a test program; compare
  behaviour with the baseline compiler.
- **G6.3** Run a stdlib test subset through the rewritten tree.
- **G6.3a** Assert `src/runtime` in the generated tree has **zero generic
  declarations** (notably `AddCleanup[T,S]`, runtime.mx:209) and builds; this
  is the "runtime has nothing generic" invariant, checked by the compiler via
  `-reject-generics`, not by grep.
- **G6.4** Measure: rewrite wall time (cold and warm/cache hit), output build
  time (cold `-a`), output binary size, peak RSS, cache size; compare with the
  baseline build.
- **G6.5** Determinism: two rewrites, `diff -r`, byte-identical; a cached
  assembly is byte-identical to a fresh rewrite.
- **Gate G6:** DoD 1-8 satisfied, with the performance numbers recorded in the
  changelog.

### G7 - hardening and extraction

- **G7.1** Diagnostics polish: every unsupported construct names file:line and
  the reason; `-stats` summary.
- **G7.2** Parallel rewrite, if G6 shows it matters (workers need the gen state
  and a merge of edits/generated files).
- **G7.3** Extract the gen code into a package and a `cmd/mxcgen`, once the
  checker extraction (the core split) makes it possible; the command surface
  stays the same.
- **G7.4** Documentation: a `docs/generics-rewriter.md` user page, plus update
  `docs/distillation.md` with what G taught (placement, spans, output tree,
  vendoring, cache).
- **G7.5** Delete this plan file before the implementation commit. It is a
  working artifact; the committed change is the code, the user page, and the
  distillation update.

### Effort

Rough new code, order-of-magnitude (+/-50%):

| phase | new lines | risk |
| --- | --- | --- |
| G0 scaffolding, spans, cache, bypass | 3,000-5,000 | typed-AST/audit unknowns |
| G1 explicit functions + deletion | 1,500-2,500 | placement/qualification |
| G2 inference + transitive | 500-1,000 | nested hosts |
| G3 generic types | 1,500-2,500 | identity/cycles |
| G4 methods | 1,000-2,000 | method sets/promotion |
| G5 bridges/edge cases | 1,000-2,000 | visibility |
| G6 self-host/incremental | 500-1,000 | performance |
| G7 hardening/extraction | 500-1,000 | - |
| fixtures/harness | 1,000-2,000 | - |

Total **10,000-19,000 new lines** for G alone, including the shared M0
scaffolding (spans, cache, driver, bypass) that every later stage reuses. If
G0's audit shows the visibility/cycle classes are large, the upper bound is the
honest one.

## 9. Test plan

### 9.1 Fixtures

`tests/generics/` with one directory per case. Each case has a `main.mx`, any
local packages, and a `run.sh`-free declarative file listing args and expected
stdout/exit code.

| # | case | proves |
| --- | --- | --- |
| 1 | same-package explicit `f[int32]` | base emitter + edit |
| 2 | same-package inferred `f(x)` | inference edit |
| 3 | cross-package explicit with caller-local type | host choice + qualification |
| 4 | generic type `Box[int32]` with fields | type emitter |
| 5 | nested `Box[Box[int32]]` | recursive type requests, outer-edit rule |
| 6 | methods on `Box[T]`, method value | method requests |
| 7 | generic type satisfying `fmt.Stringer` | interface-forced methods + String injection |
| 8 | generic interface type `Point[P]` | interface type declaration |
| 9 | unexported type arg from another package | alias bridge |
| 10 | generic body calling unexported helper | func/const bridge |
| 11 | recursive generic function | building/done guard |
| 12 | generic method with own params `M[U]` | method mangling |
| 13 | no generics at all | byte-identical copy |
| 14 | package var referenced in a generic body | expected diagnostic §G5.2 |
| 15 | generic function with `//:inline` | pragma carry |
| 16 | local type arg | expected pass or diagnostic |
| 17 | `min`/`max`/`clear` builtins | builtin skip |
| 18 | dependency outside `MOXIEROOT` | vendored into `TREE/src`, builds with no checkout/network |

### 9.2 Differential harness

`tests/phase9_generics.sh`:

1. build the baseline with the real compiler (cold `-a`), run, capture
   stdout/stderr/exit;
2. run `TREE=$(lower-generics ...)`; capture the printed path;
3. assert the tree contains only source inputs (no ELF, `.bc`, `.o`, `.a`,
   `sysroot`, bake) and no generic declarations (regex on declarations plus the
   deletion invariant, or the `-reject-generics` check);
4. `cd "$TREE" && mxc build -a -srcdir "$TREE" -o ... <target>`; run, compare;
5. for self-host, build the rewritten compiler from the tree and use it on a
   fixture;
6. for determinism, generate twice and `diff -r` the two trees (contents only,
   not directory names);
7. for the cache, run a second time with the same sources and assert a hit
   (no rewrite; same tree), then change one source and assert a miss, then
   `-no-cache` and assert a rewrite;
8. for vendoring, point `MOXIEPATH` at an empty directory and rebuild the tree:
   it must still build, proving no dependency is resolved from a checkout.

The harness never edits the checkout; the tree is under `tests/tmp/` or the
default `/tmp` and is removed by the harness after the comparison, unless the
harness is told to keep it.

### 9.3 Negative tests

Each must exit non-zero, write no output tree, and name file:line:

- an import that cannot be resolved anywhere (no source to vendor);
- local type argument (until implemented);
- package var in a generic body;
- anonymous struct/interface type argument (until the renderer handles it);
- `//:linkname` on a generic decl;
- a request whose host selection closes an import cycle, with the cycle printed.

### 9.4 Structural assertions

- every recorded request has a generated declaration;
- every use-site edit was applied (the writer counts applied vs recorded);
- no generated name collides;
- the output's generated files parse;
- no declaration anywhere in the tree, `src/runtime` included, has a
  type-parameter list;
- the output's imports are all used (the compiler enforces this);
- every import path in the closure has a directory under `TREE/src/`;
- no file in the tree is outside the source-input set (no ELF, `.bc`, `.o`,
  `.a`, `sysroot`, bake);
- the cache link index references only verified package entries.

### 9.5 Cache tests

- **hit:** second run with unchanged sources performs no rewrite (log line,
  timing), assembles the same tree byte-for-byte.
- **miss:** changing one source, the target, `-tags`, or a dependency import
  edge invalidates the link.
- **stage version:** bumping the constant invalidates.
- **corrupt entry:** truncate/remove a package entry; the next run misses and
  rewrites, no crash.
- **concurrent writers:** two runs of the same link at once leave one valid
  entry and a valid index.
- **space:** the sweep removes unreferenced package entries and respects the
  cap; two links sharing a package store it once.
- **no binaries:** the cache section contains source only; no `sysroot`,
  musl, compiler-rt, `.bc`, `.a` or bake.
- **stdlib included:** a compiler-build cache contains entries for stdlib
  packages, and a warm hit assembles them into `TREE/src/...` with the same
  content as a cold rewrite.
- **stdlib invalidation:** editing a stdlib source under `MOXIEROOT` (same
  compiler build) changes the link key and the package hash; the old entry is
  not reused.
- **pseudo-packages:** `unsafe` has no entry; `src/builtin`, if read, does.
- **restriction parity:** a target that exercises the Moxie restrictions
  (`+` on text, unnamed results, package-global writes, closure captures) is
  accepted or rejected identically from the original tree and the vendored
  tree.

## 10. Diagnostics and statistics

Diagnostic format: `<file>:<line>:<col>: lower-generics: <message>` with a
suggestion when one exists. Examples:

```
a/use.mx:12:9: lower-generics: cannot host Less[MyInt] in b: b -> a would close
  the import cycle a -> b; candidates tried: b, a (not a host for this request)
a/use.mx:20:3: lower-generics: type argument a.hidden is unexported outside a;
  emit an exported alias or export the type
```

`-stats` prints:

```
packages: 214  generic decls removed: 187  instances: 512 (func 470, type 31, method 11)
hosts: declaring 460, arg-owner 44, caller 8
bridges: types 3, funcs 5, consts 0
edits: 1,204  files rewritten: 143  output: 2,310 files
cache: hit (link 9f3a1c2d)  entries read 214  bytes written 0
```

On a miss the last line is `cache: miss (link ...)  entries written 214
bytes 18,204,331`.

Statistics go to stderr only; nothing is written into the tree except source.
The tree content carries no timestamps, absolute paths or run counters, so two
runs produce identical trees even though their directory names differ.

## 11. Performance

Measure, do not assume:

- **rewrite cost:** wall time and peak RSS of `lower-generics` vs a normal
  `build` of the same target (the rewrite runs the checker + SSA, no IR/clang);
- **warm hit:** time and RSS of a cache hit (resolve + hash + copy only); this
  is the number that matters for downstream stages;
- **cache size:** `lower/generics` bytes per target and per link, after the
  sweep; source is smaller than objects but monomorphized instances add up;
- **output build cost:** cold `-a` build of the baseline vs the output tree;
  this is the expected yield (no generic inference/instantiation in the output);
- **binary size:** monomorphization duplicates instances; compare stripped
  sizes. If the growth is unacceptable, the later option is a host policy that
  shares function instances when no cycle forbids it;
- **self-build:** the compiler's own numbers, recorded next to the distillation
  baseline.

The phase is successful even if the first output build is slower (more code,
more typechecking) as long as it is not pathologically so; the yield compounds
when later phases remove the representations.

## 12. Risks and mitigations

| risk | why it bites | mitigation / trigger |
| --- | --- | --- |
| **placement/import cycles** | source cannot name a type without importing its package; caller-local args are the common case | audit in G0; host rule §6.5; fail loud with the cycle printed; never silently pick a wrong host |
| **unexported references in copied bodies** | the host is not the declaring package, so unqualified private names do not resolve | record object references during clone build; qualify exported names; bridges for private ones; audit count in G0 |
| **visibility change from moving an instance** | unexported fields/methods become inaccessible to the declaring package once the instance lives elsewhere; no source rewrite can restore that | **placement constraint before cycles**: host in the declaring package; if a cycle forbids it, fail with the private member named. Audit counts these in G0 |
| **span/position fidelity** | line/col → offset across concatenated files and blanked const lines | parser End fields; `concatSourceMap` line mapping + per-file line index; edit overlap rule; a fixture with multiline calls, comments, CRLF and non-ASCII |
| **parser additions change behaviour** | touching the core parser | additive fields only; parse-all + IR-equality gate; no existing reader |
| **orphaned imports** | deleting a generic decl removes the only use of its constraint import | import hygiene pass; compiler still reports leftovers loudly |
| **type rendering** | `Named.String()` prints a full import path and ignores `TArgs`; anonymous types | dedicated renderer; import manager; alias rule; reject anonymous args until handled |
| **method sets** | a type instance must carry every method an interface needs | emit forced methods; run a fixture that converts to `fmt.Stringer` and one that calls through the interface |
| **transitive type params** | a nested instantiation may be keyed before substitution | `genConcreteType`; fail on any surviving `TypeParam`; recursive fixture |
| **duplicate instances / code size** | functions duplicated per caller | measure; share only when a host is safe; types are always shared (identity) |
| **pragmas on instances** | `//:linkname` would define one symbol many times | strip `//:build`; reject linkname/extern/wasmimport on generic decls; keep `//:inline` |
| **builtins** | `min`/`max`/`clear` look generic but are bodyless builtins | skip `src/builtin` in removal; never request them |
| **promoted methods on embedded generic types** | the builder may not instantiate a method reached only through an outer type's method set | force requests during method-set construction; fixture |
| **generic aliases** | `type A[T] = Box[T]` is neither a type decl body nor a function | reject in G1/G3 with a diagnostic unless the audit shows none |
| **exported aliases to unexported types** | if `.mxh` does not export the alias, other packages cannot name the bridge | fixture 9 builds from the generated tree; if it fails, extend the export writer, not the host policy |
| **Stringer injection on generic types** | injected `String()` is a generic-receiver method with no `TParamList` of its own | delete generic-receiver methods; emit them as method instances when requested |
| **output tree resolution** | `-srcdir` redirects source resolution; the `$MOXIEPATH` checkout shortcut can shadow a vendored package | in `-srcdir` mode consult `TREE/src` first; copy embed/C/asm; test with an empty `MOXIEPATH` |
| **cache key misses a cross-package request** | a package's G output depends on callers' instantiations, so a per-package key alone can serve a stale entry | whole-link key for G; narrow only with a specialization fingerprint |
| **cache growth** | monomorphized source duplicates per link | content-addressed packages dedupe across links; size cap + sweep; measure in G6 |
| **cache atomicity** | a crash or concurrent writer leaves a half entry | temp dir + rename, index last, verify hashes on read, miss on mismatch |
| **module context in a flat tree** | the tree has no module manifest; rules that consult module state (`targetIsMainModule`, module-main gating) may judge the target differently | import paths are preserved, so path-based rules are unchanged; add a restriction-parity fixture and, if a rule diverges, pass the module root explicitly - never copy non-source into the tree |
| **stale stage version** | generator changes but old entries are reused | one stage-version constant bumped in the same commit as emitter changes; cache path includes it |
| **determinism** | names/order/imports could depend on map iteration | key-derived names; sorted decls/imports; two-run byte compare |
| **build regression** | hooks accidentally run in normal builds | all hooks guarded by `genMode`; IR equality test for `build` |

## 13. Open questions to settle before G1

1. **Command name and surface.** `mxc lower-generics` as proposed, or a
   separate `mxc-gen` from day one? Recommendation: subcommand, extraction in
   G7 (§D1).
2. **Whole standard library in the first rewrite, or target module only?**
   Recommendation: fixtures first, then the compiler closure in G6. The command
   should always rewrite the whole resolved closure it is given, so behavior is
   uniform.
3. **Local type arguments.** Lift or fail? Recommendation: fail in G1, audit the
   count, lift only if non-zero. Lifting a function-local type to package scope
   can change its identity semantics (a local type is distinct per call); a
   lifted type would merge instances. This may be a legitimate limitation of
   source-level monomorphization.
4. **Package vars in generic bodies.** Fail or bridge? Recommendation: fail
   until the audit shows a real case; a getter bridge is a semantic change.
5. **Function duplication policy.** Per caller (simple, more code) vs shared
   when a safe host exists (less code, more placement work). Recommendation:
   per caller in G1-G5, measure in G6.
6. **Anonymous struct/interface type arguments.** Inline renderer or reject?
   Recommendation: reject in G1, implement inline only if the audit finds them.
7. **Output tree layout (settled by the brief).** Source-only, timestamped,
   defaulting to `/tmp`, fully vendored under `TREE/src/<import-path>`, built
   with `-srcdir`. The remaining sub-question is only the flag name
   (`-srcdir` vs `-src-root`); the target is built by its resolved import path
   from inside the tree, not by a rebased directory path.
8. **Target scope.** One target per rewrite (build tags resolved), as proposed.
   A multi-target rewrite would need per-target generated files.
9. **Cache cap and sweep policy.** Default cap (proposed 512 MB per target),
   LRU of link entries, and whether package entries survive link eviction while
   an older link still references them. Recommendation: reference-count via
   link indexes, sweep unreferenced packages oldest-first.
10. **Unsolvable visibility cases.** If the G0 audit finds a cross-package
   instantiation whose body or declaring-package code needs unexported access
   and whose host would have to be outside the declaring package (an import
   cycle), the phase cannot erase that instantiation at source level. Decide:
   fail the build with a precise diagnostic, exclude such packages from the
   first rewrite, or change the program's visibility. Recommendation: fail
   loudly and scope the first cut to the clean subset.

## 14. Appendix - hook and file map

### New files

| file | rough size | contents |
| --- | --- | --- |
| `_mxc_stage4/gen_main.mx` | 400-600 | command, flags, prep, per-package walk, summary |
| `_mxc_stage4/gen_state.mx` | 150-250 | gen state, requests, edits, diagnostics |
| `_mxc_stage4/gen_emit.mx` | 500-800 | function/type/method instance emission |
| `_mxc_stage4/gen_source.mx` | 300-500 | line index, offsets, spans, edit apply, import hygiene |
| `_mxc_stage4/gen_types.mx` | 300-500 | type key, names, render, import manager, aliases |
| `_mxc_stage4/gen_output.mx` | 300-500 | source tree copy, deletions, generated files, path layout |
| `_mxc_stage4/gen_cache.mx` | 300-500 | link key, content-addressed package entries, link index, atomic writes, sweep |

These are estimates for planning, not commitments.

### Existing code that is read or lightly changed

| existing | role in G |
| --- | --- |
| main.mx:7554 `main`, :3780 `discoverPkg`, :3999 `resolveAll`, :2826 `srcSpan` | command dispatch, package closure, line map |
| main.mx:7175 `prepareDispatchIndex`, :7983 pre-pass | exports and whole-link typecheck |
| ir_typecheck.mx:439 `typeCheckPkg`, :942 `ScanGenericDecls`, :914 `scanParse`, :1314 `CompileToIR` | checking, generic AST/source retention, stop after SSA |
| ssa_builder.mx:48 `CreatePackage`, :1499 `buildBody` | the checker/body builder being instrumented |
| ssa_builder.mx:11050/:11184/:11218/:11234 | free-function instantiation and call sites |
| ssa_builder.mx:7177/:7123/:7274 | method instantiation and forced methods |
| ir_typecheck.mx:2256, ssa_builder.mx:9884 | type instantiation |
| ir_descriptors.mx:87 `typeDescName`, :608 `localTypeTagOf` | canonical type keys |

### Audit snapshot (static, for sizing only)

From the current tree (grep counts, not the dynamic audit):

- 199 free generic function declarations;
- 16 generic type declarations;
- 28 methods on generic receivers;
- 47 files declaring generics;
- at least 31 explicit-instantiation call sites (most calls rely on inference,
  which is why inference is in G2, not optional).

The dynamic audit in G0 will replace these with actual instantiation counts and
the placement/bridge classification.


