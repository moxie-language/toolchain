# Building Moxie

## System Requirements

| Component | Version | Notes |
|-----------|---------|-------|
| LLVM | 21 | llvm-link, llvm-extract |
| clang | 21 | .ll to .bc compilation |
| lld | 21 | Final binary linking |

### Arch Linux

```sh
pacman -S llvm21 clang21 lld21
```

### Ubuntu/Debian

```sh
apt install llvm-21 clang-21 lld-21
```

## Building from Source

The Moxie compiler is self-hosting. You need an existing moxie binary to bootstrap.

```sh
# from a release tarball or git clone
./build.sh
```

`build.sh` finds a moxie binary (in order: `./moxie`, `./legacy/moxie`, `$PATH`),
compiles the compiler source (`_mxc_stage4/` or `compiler/`), verifies the output,
then runs `moxie update --all` to rebuild all cached packages with the new compiler.

To bootstrap with a specific binary:

```sh
MOXIE=/path/to/moxie ./build.sh
```

## Installing

### From Release Tarball

```sh
curl -fsSL https://git.smesh.lol/moxie-releases/raw/install.sh | bash
```

The installer downloads the latest release, extracts to `/opt/moxie` (or `$MOXIE_INSTALL_DIR`),
and rebuilds all cached packages with the new compiler version.

Add to your shell profile:

```sh
export PATH="/opt/moxie/bin:$PATH"
export MOXIEROOT="/opt/moxie"
```

### From Git

```sh
git clone https://git.smesh.lol/moxie
cd moxie
./build.sh
cp moxie ~/.local/bin/
export MOXIEROOT="$(pwd)"
```

## Subcommands

### `moxie build`

Compile a Moxie program to a statically linked binary.

```
moxie build [flags] <package>

Flags:
  -o <file>      output filename (default: package basename)
  -a             force rebuild all packages (ignore cache)
  -print-ir      print LLVM IR to stdout
```

External dependencies are auto-fetched during build if not present locally.
Package resolution order:

1. Direct paths (`.`, `./`, `../`, absolute)
2. `replace` directives from `moxie.mod`
3. Module-relative paths (module prefix match)
4. `~/moxie/<import-path>` (fetched git repos)
5. `$MOXIEROOT/src/<import-path>` (stdlib)
6. `$MOXIEROOT/src/vendor/<import-path>` (vendored)

### `moxie fetch`

Fetch all dependencies declared in `moxie.mod`.

```
moxie fetch
```

Reads `require` directives from `moxie.mod`, clones repos via HTTPS into
`~/moxie/<import-path>`, checks out the specified version tag.
Recursively fetches transitive dependencies.

Per-repository lock files prevent concurrent version checkouts from parallel builds.

### `moxie update`

Remove cached build artifacts from older compiler versions.

```
moxie update          # remove old version caches
moxie update --all    # also rebuild all packages with current compiler
```

Cache is stored under `~/moxie/cache/mxc-<version>/`. When the compiler is
updated, `moxie update` removes cache directories from previous versions.
With `--all`, it also recompiles every cached package so subsequent builds
are instant.

### `moxie cache clean`

Remove all cached build artifacts for the current compiler version.

```
moxie cache clean
```

### `moxie version`

Print the compiler version.

```
moxie version
```

## Directory Layout

### Source Tree

```
moxie/
  _mxc_stage4/       compiler source (self-hosting)
  src/                standard library source
  sysroot/            precompiled runtime + C libs
    runtime.bc        runtime bitcode
    musl/             musl libc (static)
    bdwgc/            Boehm GC (static)
    compiler-rt/      LLVM builtins (static)
    obj/              precompiled object files
  llvm-glue/          LLVM C++ shim library
  jsruntime/          WASM browser API shims
  build.sh            self-hosting build script
  release.sh          release tarball builder
  install.sh          one-line installer
```

### User Directory (`~/moxie/`)

```
~/moxie/
  github.com/user/pkg/      fetched git repo (inspectable, cd-able)
  git.smesh.lol/somepkg/    fetched git repo
  cache/
    mxc-1.7.0/              current compiler version cache
      pkg/
        errors/
          af275306.bc        compiled bitcode
          af275306.mxh       type header (protocol)
        fmt/
          ...
    mxc-1.6.0/              old version (removed by `moxie update`)
```

### Cache Files

- `.bc` - LLVM bitcode, linked into the final binary
- `.mxh` - Moxie type header, a compact text format describing a package's
  exported types, functions, constants, and methods. Used to type-check
  against a dependency without recompiling its source.

Versioned dependencies include the version tag in the filename:
`<hash>_v1.2.3.bc`, `<hash>_v1.2.3.mxh`.

## Module File (`moxie.mod`)

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

- `module` - declares the module's import path
- `require` - lists dependencies with optional version tags
- `replace` - redirects imports to local paths or alternate repos

## .mxh Format

Protocol header files describe a package's public API:

```
package errors errors
type Const string
method Const Error ->string 0
const ErrUnsupported Const unsupported operation
func Is interface{},interface{}->bool
func New string->interface{}
```

Line format:
- `package <path> <name>` - package declaration
- `func <name> <params->results>` - function signature
- `var <name> <type>` - variable
- `const <name> <type> <value>` - constant
- `type <name> <underlying>` - named type
- `method <type> <name> <sig> <ptrRecv>` - method (ptrRecv: 0 or 1)
- `iface <name> <method1=sig1;method2=sig2>` - interface type

Type descriptors use the same compact format as the compiler's internal
`parseTypeDesc`: `int32`, `string`, `[]uint8`, `*int32`, `interface{}`,
`bool`, etc. Signature descriptors: `param1,param2->result1,result2`.

## Output Binaries

Moxie produces fully static binaries linked against musl, Boehm GC, and
compiler-rt. No glibc, no dynamic libraries, no threads.

Typical sizes:
- Hello world: ~400KB
- Compiler (mxc): ~2.5MB

## Self-Hosting Verification

The compiler reaches fixpoint - compiling its own source produces a
byte-identical binary. To verify:

```sh
MOXIEROOT=. LLVM_GLUE_PATH=./llvm-glue/libmoxie-llvm-glue.so \
    ./moxie build -o moxie-verify ./_mxc_stage4/
cmp moxie moxie-verify && echo "fixpoint verified"
```

The release script (`release.sh`) performs this verification automatically.
