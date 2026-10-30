# Building Moxie

## System Requirements

Moxie pins **LLVM 22** across the whole stack. This is not a preference: the
legacy Go compiler links `libLLVM-22`, stage4 emits LLVM IR and invokes
`clang-22`, `llvm-link-22`, `ld.lld-22` and `opt` (for the per-package
middle-end), and LLVM
bitcode is not compatible across major versions. Mixing majors produces
bitcode that one of the tools cannot read, and the failure surfaces late, at
link time.

| Component | Version | Used for | Required |
|-----------|---------|----------|----------|
| clang | 22 | `.ll` -> `.bc` -> `.o` compilation | yes |
| ld.lld | 22 | final link of every binary | yes |
| llvm-link | 22 | merging the runtime bake | yes |
| opt | 22 | `-passes=default<O2>` on compiled packages | yes |
| llvm-extract | 22 | extracting functions during IR validation | yes |
| libLLVM | 22 | linked into the legacy Go compiler | bootstrap only |
| Go | 1.25+ | building the legacy bootstrap compiler | bootstrap only |
| git | any | `fetch`/`update` of module dependencies | yes |
| wasm-ld | 22 | linking WASM targets (browser) | WASM only |
| musl | - | `sysroot/musl/lib.a` and `crt1.o` | prebuilt in tree |

Note on tool names: many distributions ship LLVM 22 as the *unversioned*
toolchain, because 22 is a current release. Others install only versioned
names. `build.sh` accepts either but **verifies the major version reported by
`--version`** rather than trusting the name, so a machine whose unversioned
`clang` is actually 20 fails immediately with a list of what is wrong, rather
than producing unreadable bitcode halfway through a build.

### Arch Linux

LLVM 22 is in `extra` as the current release, so the unversioned packages are
what you want:

```sh
sudo pacman -S llvm clang lld
```

Verify:

```sh
clang --version | head -1   # expect 22.x
ld.lld --version            # expect 22.x
```

### Debian / Ubuntu

If your release carries LLVM 22, the versioned packages exist:

```sh
sudo apt install llvm-22 clang-22 lld-22
```

On releases that predate it, use the official LLVM APT repository, which is
the supported way to get a specific major version
(<https://apt.llvm.org>):

```sh
wget -qO- https://apt.llvm.org/llvm-snapshot.gpg.key \
  | sudo tee /etc/apt/trusted.gpg.d/apt.llvm.org.asc >/dev/null
. /etc/os-release
sudo add-apt-repository \
  "deb http://apt.llvm.org/${VERSION_CODENAME}/ llvm-toolchain-${VERSION_CODENAME}-22 main"
sudo apt update
sudo apt install llvm-22 clang-22 lld-22
```

### Fedora / RHEL / CentOS Stream

Fedora carries LLVM 22 in its main repositories:

```sh
sudo dnf install llvm clang lld
```

On RHEL-family releases whose base repositories stop at an older LLVM, enable
a newer LLVM module stream, or enable EPEL, which often carries a newer LLVM
than the base distribution:

```sh
sudo dnf install epel-release
sudo dnf install llvm clang lld
```

### Any distribution, or building LLVM from source

Prebuilt binaries for many platforms are published at
<https://releases.llvm.org>; LLVM 22 is announced at
<https://www.phoronix.com/news/LLVM-Clang-22.1-Released>. If you build from
source, put the resulting `bin` directory on `PATH`. The build scripts locate
`libLLVM-22` through `llvm-config-22` or the usual `/usr/lib/llvm-22`-style
prefixes, and fall back to an unversioned `llvm-config` whose reported major
version is 22.

### musl and compiler-rt

The native target links `sysroot/musl/lib.a`, `sysroot/musl/crt1.o` and
`sysroot/compiler-rt/lib.a`. These are prebuilt artifacts carried in the tree.
They are target libraries rather than build tools, so a normal build does not
need a musl toolchain installed - only regenerating those artifacts does.

## Building the bootstrap compiler

Moxie is self-hosting: the compiler is written in Moxie, so a working compiler
is needed to build one. The legacy Go compiler in `legacy/` breaks that cycle,
and it is the only reason Go is required.

```sh
cd legacy
./build.sh          # produces legacy/moxie, links libLLVM-22
cd ..
```

Then run the bootstrap protocol documented in the repository root `CLAUDE.md`
under "Bootstrap Build Protocol". In short: legacy builds the stage4 compiler,
that compiler bakes the runtime, and then builds itself until a generation
reproduces its own binary byte for byte.

## Building from Source

The Moxie compiler is self-hosting. You need an existing moxie binary to bootstrap.

```sh
# from a release tarball or git clone
./build.sh
```

`build.sh` finds a moxie binary (in order: `./moxie-new`, `./moxie`,
`./legacy/moxie`, then `$PATH`), compiles the compiler source
(`_mxc_stage4/`), verifies the output, then rebuilds the cached packages with
the new compiler.

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
  sysroot/            target libraries and the runtime bake
    musl/             musl libc (static): lib.a, crt1.o
    compiler-rt/      LLVM builtins (static)
    obj/              precompiled assembly objects
    runtime_go.bc     runtime bake (arena, channels, spawn) - generated,
                      NOT tracked; regenerate with `moxie build-runtime`
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

Moxie produces fully static binaries linked against musl and compiler-rt.
No glibc, no dynamic libraries, no threads, no GC.

Typical sizes:
- Hello world: ~400KB
- Compiler (mxc): ~2.5MB

## Runtime Bake

`sysroot/runtime_go.bc` is the runtime compiled to bitcode. It is an **input**
to application links, used when the application links against the runtime
rather than compiling it from source - the compiler itself always compiles the
runtime from source, which is why a stale bake does not break the bootstrap.

It is a cache, and it is compiler-specific: a blob produced by the legacy
compiler does not satisfy a stage4 link. Regenerate it whenever the runtime
source or the compiler changes:

```sh
moxie build-runtime
```

The bootstrap protocol runs this step, so a normal bootstrap keeps the bake in
sync. If an application fails to link with undefined `moxie_*` or `runtime.*`
symbols, a stale bake is the first thing to check.

## Self-Hosting Verification

The compiler reaches a fixpoint: some generation emits the identical binary
that emitted it. Verification is bootstrap-against-bootstrap, not against the
legacy compiler:

```sh
export MOXIEROOT=$PWD

# legacy builds the first bootstrap
legacy/moxie build -a -o mxc-stage4 ./_mxc_stage4

# bake the runtime with the compiler just built
./mxc-stage4 build-runtime

# the bootstrap must reproduce itself
./mxc-stage4   build -a -o mxc-stage4-2 ./_mxc_stage4
./mxc-stage4-2 build -a -o mxc-stage4-3 ./_mxc_stage4
cmp mxc-stage4-2 mxc-stage4-3 && echo "fixpoint verified"
```

Convergence can take one extra generation, because the legacy pipeline differs
(run `interp` + LowerInterfaces + globaldce) and the first bootstrap inherits
that difference. A mismatch between the legacy-built compiler and the first
bootstrap is expected and carries no signal; only a generation failing to
reproduce itself is a failure.

`tests/phase2_selfhost.sh` runs this whole chain, including the runtime bake.
