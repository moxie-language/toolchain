//go:build none
//:build linux
//
// The //go:build none above is the upstream marker for a file that Go's
// toolchain must not compile on its own; these are included manually for
// Linux targets. Stage4 selects files with //:build, so without this it
// compiles them for every target - and they include Linux headers.

// Minimal ELF shared library loader for mxh FFI.
// Provides dlopen/dlsym without requiring the dynamic linker.
// x86_64 Linux only. Handles simple .so files with R_X86_64_RELATIVE
// and R_X86_64_GLOB_DAT relocations.

#include <elf.h>
#include <fcntl.h>
#include <stdint.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

struct ffi_lib {
	uint8_t *base;
	uint64_t bias;
	Elf64_Sym *symtab;
	const char *strtab;
	uint32_t nsyms;
};

#define MAX_FFI_LIBS 16
static struct ffi_lib libs[MAX_FFI_LIBS];
static int nlibs;

static ssize_t full_read(int fd, void *buf, size_t n) {
	size_t done = 0;
	while (done < n) {
		ssize_t r = read(fd, (char *)buf + done, n - done);
		if (r <= 0) return -1;
		done += r;
	}
	return done;
}

void *dlopen(const char *path, int flags) {
	(void)flags;
	if (!path || nlibs >= MAX_FFI_LIBS) return 0;

	int fd = open(path, O_RDONLY);
	if (fd < 0) return 0;

	Elf64_Ehdr ehdr;
	if (full_read(fd, &ehdr, sizeof(ehdr)) < 0) goto fail;
	if (memcmp(ehdr.e_ident, ELFMAG, SELFMAG) != 0) goto fail;
	if (ehdr.e_type != ET_DYN) goto fail;
	if (ehdr.e_machine != EM_X86_64) goto fail;

	int phnum = ehdr.e_phnum;
	if (phnum > 128) goto fail;
	Elf64_Phdr phdrs[128];
	if (lseek(fd, ehdr.e_phoff, SEEK_SET) < 0) goto fail;
	if (full_read(fd, phdrs, sizeof(Elf64_Phdr) * phnum) < 0) goto fail;

	uint64_t lo = UINT64_MAX, hi = 0;
	for (int i = 0; i < phnum; i++) {
		if (phdrs[i].p_type != PT_LOAD) continue;
		uint64_t s = phdrs[i].p_vaddr & ~(uint64_t)0xFFF;
		uint64_t e = (phdrs[i].p_vaddr + phdrs[i].p_memsz + 0xFFF) & ~(uint64_t)0xFFF;
		if (s < lo) lo = s;
		if (e > hi) hi = e;
	}
	if (lo >= hi) goto fail;
	uint64_t span = hi - lo;

	uint8_t *base = mmap(0, span, PROT_NONE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0);
	if (base == MAP_FAILED) goto fail;

	for (int i = 0; i < phnum; i++) {
		if (phdrs[i].p_type != PT_LOAD) continue;
		uint64_t vaddr = phdrs[i].p_vaddr;
		uint64_t page = vaddr & ~(uint64_t)0xFFF;
		uint64_t off = vaddr - page;
		uint64_t foff = phdrs[i].p_offset - off;
		uint64_t mapsz = (phdrs[i].p_filesz + off + 0xFFF) & ~(uint64_t)0xFFF;

		int prot = 0;
		if (phdrs[i].p_flags & PF_R) prot |= PROT_READ;
		if (phdrs[i].p_flags & PF_W) prot |= PROT_WRITE;
		if (phdrs[i].p_flags & PF_X) prot |= PROT_EXEC;

		void *seg = mmap(base + page - lo, mapsz, prot,
			MAP_PRIVATE | MAP_FIXED, fd, foff);
		if (seg == MAP_FAILED) {
			munmap(base, span);
			goto fail;
		}

		uint64_t memsz_aligned = (phdrs[i].p_memsz + off + 0xFFF) & ~(uint64_t)0xFFF;
		if (memsz_aligned > mapsz) {
			void *bss = mmap(base + page - lo + mapsz,
				memsz_aligned - mapsz, prot,
				MAP_PRIVATE | MAP_FIXED | MAP_ANONYMOUS, -1, 0);
			if (bss == MAP_FAILED) {
				munmap(base, span);
				goto fail;
			}
		}
	}
	close(fd);
	fd = -1;

	Elf64_Dyn *dyn = 0;
	for (int i = 0; i < phnum; i++) {
		if (phdrs[i].p_type == PT_DYNAMIC) {
			dyn = (Elf64_Dyn *)(base + phdrs[i].p_vaddr - lo);
			break;
		}
	}

	Elf64_Rela *rela = 0;
	uint64_t relasz = 0;
	Elf64_Rela *jmprel = 0;
	uint64_t pltrelsz = 0;
	Elf64_Sym *symtab = 0;
	const char *strtab = 0;
	uint64_t strsz = 0;
	uint32_t *hashtab = 0;

	if (dyn) {
		for (int i = 0; dyn[i].d_tag != DT_NULL; i++) {
			switch (dyn[i].d_tag) {
			case DT_RELA:
				rela = (Elf64_Rela *)(base + dyn[i].d_un.d_ptr - lo);
				break;
			case DT_RELASZ:
				relasz = dyn[i].d_un.d_val;
				break;
			case DT_JMPREL:
				jmprel = (Elf64_Rela *)(base + dyn[i].d_un.d_ptr - lo);
				break;
			case DT_PLTRELSZ:
				pltrelsz = dyn[i].d_un.d_val;
				break;
			case DT_SYMTAB:
				symtab = (Elf64_Sym *)(base + dyn[i].d_un.d_ptr - lo);
				break;
			case DT_STRTAB:
				strtab = (const char *)(base + dyn[i].d_un.d_ptr - lo);
				break;
			case DT_STRSZ:
				strsz = dyn[i].d_un.d_val;
				break;
			case DT_HASH:
				hashtab = (uint32_t *)(base + dyn[i].d_un.d_ptr - lo);
				break;
			}
		}
	}

	// Process .rela.dyn
	if (rela && relasz) {
		int count = relasz / sizeof(Elf64_Rela);
		for (int i = 0; i < count; i++) {
			uint64_t *target = (uint64_t *)(base + rela[i].r_offset - lo);
			int type = ELF64_R_TYPE(rela[i].r_info);
			if (type == 8) { // R_X86_64_RELATIVE
				*target = (uint64_t)(base - lo) + rela[i].r_addend;
			} else if (type == 6 || type == 7) { // GLOB_DAT, JUMP_SLOT
				*target = 0;
			}
		}
	}

	// Process .rela.plt
	if (jmprel && pltrelsz) {
		int count = pltrelsz / sizeof(Elf64_Rela);
		for (int i = 0; i < count; i++) {
			uint64_t *target = (uint64_t *)(base + jmprel[i].r_offset - lo);
			int type = ELF64_R_TYPE(jmprel[i].r_info);
			if (type == 7) { // JUMP_SLOT
				*target = 0;
			} else if (type == 8) { // RELATIVE
				*target = (uint64_t)(base - lo) + jmprel[i].r_addend;
			}
		}
	}

	uint32_t nsyms = 0;
	if (hashtab) {
		nsyms = hashtab[1]; // nchain in DT_HASH
	} else if (symtab && strtab && strsz > 0) {
		// No hash table; estimate from strtab position
		nsyms = ((uint8_t *)strtab - (uint8_t *)symtab) / sizeof(Elf64_Sym);
	}

	struct ffi_lib *lib = &libs[nlibs++];
	lib->base = base;
	lib->bias = lo;
	lib->symtab = symtab;
	lib->strtab = strtab;
	lib->nsyms = nsyms;
	return lib;

fail:
	if (fd >= 0) close(fd);
	return 0;
}

void *dlsym(void *handle, const char *name) {
	if (!handle || !name) return 0;
	struct ffi_lib *lib = handle;
	if (!lib->symtab || !lib->strtab || !lib->nsyms) return 0;

	for (uint32_t i = 0; i < lib->nsyms; i++) {
		Elf64_Sym *sym = &lib->symtab[i];
		if (sym->st_shndx == SHN_UNDEF) continue;
		int type = ELF64_ST_TYPE(sym->st_info);
		if (type != STT_FUNC && type != STT_OBJECT) continue;
		if (strcmp(lib->strtab + sym->st_name, name) == 0) {
			return lib->base + sym->st_value - lib->bias;
		}
	}
	return 0;
}
