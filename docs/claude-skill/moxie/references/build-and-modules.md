# Moxie Build and Modules

## Build Commands

```sh
moxie build [flags] <package>     # compile to static binary
moxie build -target js/wasm -o dir/ pkg/  # WASM output
moxie fetch                       # install all deps from moxie.mod
moxie install git.example.com/pkg # install single dep
moxie update                      # remove old version caches
moxie update --all                # also rebuild all packages
moxie cache clean                 # wipe current version cache
moxie version                     # print compiler version
```

Build flags:
- `-o <file>` - output filename
- `-a` - force rebuild all (ignore cache)
- `-target <triple>` - cross-compile target
- `-print-ir` - dump LLVM IR

`moxie build` is **network-free**. Dependencies must be pre-fetched.

## Targets

| Target | Output | Libc |
|--------|--------|------|
| linux/amd64 | Static ELF | musl |
| linux/arm64 | Static ELF | musl |
| darwin/amd64 | Mach-O | libSystem |
| darwin/arm64 | Mach-O | libSystem |
| js/wasm | WASM + JS runtime | - |

All native binaries fully statically linked. No dynamic dependencies.

## Module File (moxie.mod)

```
module github.com/user/myapp

require (
    github.com/user/libfoo v0.3.0
    git.smesh.lol/somepkg
)

replace (
    github.com/user/libfoo => ../libfoo
)
```

Directives: `module`, `require`, `replace`.

## Package Resolution Order

1. Direct paths (`.`, `./`, `../`, absolute)
2. `replace` directives from `moxie.mod`
3. Module-relative paths (module prefix match)
4. `~/moxie/<import-path>` (fetched git repos)
5. `$MOXIEROOT/src/<import-path>` (stdlib)
6. `$MOXIEROOT/src/vendor/<import-path>` (vendored)

## Platform-Specific Files

- `_linux.mx`, `_darwin.mx`, `_wasm.mx` - OS suffix
- `_amd64.mx`, `_arm64.mx` - arch suffix
- `_unix.mx` - all Unix targets
- `_test.mx` - test builds only

Build constraints: `//go:build linux && amd64` (first line).

Always-set tags: `moxie`, `moxie.unicore`, `scheduler.none`.

## Cache

Cache stored at `~/moxie/cache/mxc-<version>/`.

Files:
- `.bc` - LLVM bitcode, linked into final binary
- `.mxh` - protocol header, describes exported types/functions for cross-package type-checking

## .mxh Protocol Headers

Compact text format describing a package's public API:

```
package errors errors
type Const string
method Const Error ->string 0
const ErrUnsupported Const unsupported operation
func Is interface{},interface{}->bool
func New string->interface{}
```

Hash changes when any type layout changes. Compiler embeds hash at cross-binary spawn sites; runtime verifies at spawn time.

## Cross-Binary spawn

When a package is imported from .mxh cache, `spawn` forks and execs the cached binary:

```moxie
import iskradb "git.example.com/iskradb"
done := spawn(iskradb.InsertTriple, key, val, results)
```

Parent and child exchange .mxh hashes at spawn time (5s timeout). Hash mismatch = stale binary, run `moxie install` again.

## Directory Layout

```
moxie/
  _mxc_stage4/       compiler source (self-hosting)
  src/                standard library
  sysroot/            precompiled runtime + C libs
  llvm-glue/          LLVM C++ shim library
  jsruntime/          WASM browser API shims
  build.sh            self-hosting build script
```

## Self-Hosting

The compiler reaches fixpoint - compiling its own source produces a byte-identical binary.

```sh
MOXIEROOT=. ./moxie build -o moxie-verify ./_mxc_stage4/
cmp moxie moxie-verify && echo "fixpoint verified"
```
