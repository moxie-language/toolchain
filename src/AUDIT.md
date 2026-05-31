# Stdlib Audit: Moxie .mx vs Go upstream

Reference: Go 1.25.3 at ~/go/src/

## Batch 1: runtime/, os/, syscall/ (2026-05-29)

### runtime/ (97 files)
- **Status: OK** - No mangling detected
- 15 files have Go counterparts, all semantically correct
- ~22 files are Moxie-original (custom GC variants, pipe_channel, ringbuf, spawn, secalloc, domain)
- Known transforms present: make->literal, new->&T{}, no goroutines, no init()
- hashmap.mx is a complete custom implementation (not based on Go's map_*.go)

### os/ (42 files)
- **Status: OK with style issues**
- Core file operations, constants, error types all correct
- `const` declarations (O_RDONLY, O_WRONLY, etc.) reference syscall constants - resolved at compile time, not affected by init/var issue
- 13 lines use `+` for string concatenation instead of `|` - works (compiler accepts both) but should be converted for style consistency
- filesystem.mx, file_anyos.mx, types_anyos.mx, osexec.mx are Moxie-only additions
- dirinfo uses raw pointer instead of Go's atomic.Pointer - correct for single-threaded Moxie
- dir_unix.mx lacks sync.Mutex - correct for single-threaded Moxie

### syscall/ (51 files)
- **Status: OK** - No mangling detected
- All errno constants, signal types, syscall numbers match Go upstream
- Platform consolidated to linux/amd64 + linux/arm64 + js/wasm (intentional)
- init() functions converted to ensureRlimit() flag-based pattern
- sync.RWMutex removed (single-threaded) - correct
- Moxie-only additions: env_nonhosted, file_emulated, file_hosted, proc_emulated, proc_hosted, syscall_nonhosted, syscall_linux_moxie, syscall_moxie, tables_nonhosted

### Cross-cutting finding: string `+` concatenation
65 files across the stdlib still use `+` instead of `|` for string concatenation.
The compiler accepts both (ir_emit.mx line 1783: OpAdd and OpOr both dispatch to emitSliceConcat for string types).
Not a bug, but should be converted for language consistency.
Packages affected: os/, net/, net/http/, crypto/x509/, archive/, debug/, go/, syscall/js/, vendor/

## Batch 2-7: pending
