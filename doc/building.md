# Building Moxie

## System Requirements

| Component | Version | Notes |
|-----------|---------|-------|
| Go | 1.25+ | Host compiler |
| LLVM | 19 | Must be exactly 19 (go-llvm bindings are version-specific) |
| clang | 19 | For compiling C runtime components (musl, bdwgc, compiler-rt) |
| lld | 19 | LLVM linker for final binary linking |

### Arch Linux

```sh
sudo pacman -S llvm19 clang19 lld19
```

### Ubuntu/Debian

```sh
# Add LLVM apt repository for version 19, then:
sudo apt install llvm-19-dev clang-19 lld-19
```

## Building the Compiler

```sh
cd /path/to/moxie

# Set CGO flags to find LLVM 19
export CGO_CPPFLAGS="-I/usr/lib/llvm19/include"
export CGO_LDFLAGS="-L/usr/lib/llvm19/lib $(/usr/lib/llvm19/bin/llvm-config --ldflags --libs all) -lclang -lstdc++ -lz -lzstd"

# Build (GOWORK=off if parent go.work doesn't include moxie)
GOWORK=off go build -tags llvm19 -o moxie ./main.go
```

The binary is statically linked against LLVM 19 libraries.

## Using the Compiler

```sh
# Set MOXIEROOT to the source tree (contains src/, lib/)
export MOXIEROOT=/path/to/moxie

# LLVM 19 tools must be in PATH
export PATH="/usr/lib/llvm19/bin:$PATH"

# Build a program
./moxie build -o hello hello.go

# Run
./hello
```

### Build Flags

```
-o <file>       output filename
-opt <level>    optimization: 0, 1, 2, s, z (default "z")
-gc <gc>        garbage collector: none, leaking, conservative, boehm (default from target)
-scheduler <s>  scheduler: none, tasks (default "tasks")
-panic <mode>   panic strategy: print, trap (default "print")
-stack-size <n> goroutine stack size in bytes
-tags <tags>    extra build tags
-x              print commands as they execute
-work           keep temporary build directory
```

### Subcommands

```
moxie build     compile a program
moxie run       compile and run
moxie test      compile and run tests
moxie clean     clean build cache
moxie version   print version
moxie env       print environment variables
moxie info      show target information
moxie list      list available targets
moxie help      show help
```

## Output Binaries

Moxie produces fully static binaries:
- **Linux**: statically linked against musl libc, Boehm GC, compiler-rt
- **Darwin**: linked against minimal macOS SDK
- No glibc, no dynamic libraries, no threads

Typical sizes:
- Hello world: ~400KB
- With spawn: ~420KB

## C Libraries

The following C libraries live under `lib/` and are compiled automatically:

| Library | Purpose | Source |
|---------|---------|--------|
| `lib/musl` | C standard library (static) | tinygo-org/musl-libc |
| `lib/bdwgc` | Boehm garbage collector | ivmai/bdwgc @ 7577ca7 |
| `lib/compiler-rt-builtins` | LLVM runtime builtins | symlink to system tinygo |

Libraries are compiled once and cached in `~/.cache/moxie/`.
