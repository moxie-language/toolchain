//go:build none

// secalloc.c - signal-handler side of the secure allocator.
//
// Design: secalloc.mx mmap's guarded arenas and registers them here via
// moxie_secalloc_register_arena(). At init it also calls moxie_secalloc_configure()
// to hand over the noise buffer and lockdown pipe fd. When a fatal signal fires,
// runtime_unix.c's signal_handler calls moxie_secalloc_on_fatal_signal() which:
//
//   1. Wipes every registered arena with noise bytes — SYNCHRONOUSLY, before
//      the handler returns, so no attacker can observe secret contents after
//      the fault but before teardown.
//   2. Writes one byte to the lockdown pipe to notify the parent domain.
//
// Only async-signal-safe primitives are used: memcpy (pure compute) and write(2)
// (POSIX-guaranteed). No malloc, no locks, no printf. The arena registry is a
// fixed-size global populated at init; no dynamic allocation from the handler.
//
// This file is included on both Darwin and Linux.

#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <unistd.h>

#ifdef __linux__
#include <sys/mman.h>
#include <sys/syscall.h>
#ifndef SYS_memfd_secret
#define SYS_memfd_secret 447
#endif
#endif

#define MOXIE_SECALLOC_MAX_ARENAS 64

struct moxie_secalloc_arena {
	void  *base;
	size_t len;
};

static struct moxie_secalloc_arena moxie_secalloc_arenas[MOXIE_SECALLOC_MAX_ARENAS];
static int    moxie_secalloc_narenas = 0;
static const uint8_t *moxie_secalloc_noise = NULL;
static size_t moxie_secalloc_noise_len = 0;
static int    moxie_secalloc_lockdown_fd = -1;

// Moxie-facing: register a new guarded arena. Must be called from normal
// (non-signal) context, once per arena, before any secret is written into it.
// Scans for a free slot (NULL base, set by unregister) before appending, so
// rotation cycles can reuse slots and the registry doesn't grow unboundedly.
// Returns 0 on success, -1 if the table is full.
int moxie_secalloc_register_arena(void *base, size_t len) {
	for (int i = 0; i < moxie_secalloc_narenas; i++) {
		if (moxie_secalloc_arenas[i].base == NULL) {
			moxie_secalloc_arenas[i].base = base;
			moxie_secalloc_arenas[i].len  = len;
			return 0;
		}
	}
	if (moxie_secalloc_narenas >= MOXIE_SECALLOC_MAX_ARENAS) {
		return -1;
	}
	moxie_secalloc_arenas[moxie_secalloc_narenas].base = base;
	moxie_secalloc_arenas[moxie_secalloc_narenas].len  = len;
	moxie_secalloc_narenas++;
	return 0;
}

// Moxie-facing: drop a previously registered arena. Marks the slot free so
// moxie_secalloc_register_arena can reuse it on the next call. Called by
// SecureRotate after the old arena has been wiped and munmap'd. Idempotent
// — unknown bases are silently ignored. The signal-handler wipe path skips
// NULL entries so unregistered arenas are no longer touched.
void moxie_secalloc_unregister_arena(void *base) {
	for (int i = 0; i < moxie_secalloc_narenas; i++) {
		if (moxie_secalloc_arenas[i].base == base) {
			moxie_secalloc_arenas[i].base = NULL;
			moxie_secalloc_arenas[i].len  = 0;
			return;
		}
	}
}

// Moxie-facing: return current arena count so the Moxie side can check for
// full-table conditions before registering. Slot count, not live count —
// includes NULL'd-out entries waiting to be reused.
int moxie_secalloc_arena_count(void) {
	return moxie_secalloc_narenas;
}

// Moxie-facing: one-shot configuration. noise must be a buffer of noise_len
// bytes that will live for the rest of the process. lockdown_fd is the write
// end of a pipe inherited from the parent domain; the read end is watched by
// the parent's event loop. Set lockdown_fd = -1 to disable notification.
void moxie_secalloc_configure(const void *noise, size_t noise_len, int lockdown_fd) {
	moxie_secalloc_noise = (const uint8_t *)noise;
	moxie_secalloc_noise_len = noise_len;
	moxie_secalloc_lockdown_fd = lockdown_fd;
}

// Moxie-facing: update only the lockdown fd. Used after spawn when the child
// receives an inherited pipe fd from the parent domain and needs to route
// fault notifications there instead of stderr. Safe to call before or after
// moxie_secalloc_configure() — if called before, the configure call will not
// override the explicit fd (but the current implementation does set it from
// the Moxie-side secLockdownFd var, so callers should set the var too).
void moxie_secalloc_set_lockdown_fd(int fd) {
	moxie_secalloc_lockdown_fd = fd;
}

// Wipe every registered arena with noise bytes. Repeats the noise buffer if
// an arena is larger than the noise. Runs from signal context — must be
// async-signal-safe.
static void moxie_secalloc_wipe_all(void) {
	if (moxie_secalloc_noise == NULL || moxie_secalloc_noise_len == 0) {
		return;
	}
	for (int i = 0; i < moxie_secalloc_narenas; i++) {
		uint8_t *dst = (uint8_t *)moxie_secalloc_arenas[i].base;
		size_t   remaining = moxie_secalloc_arenas[i].len;
		size_t   off = 0;
		if (dst == NULL) {
			continue;
		}
		while (remaining > 0) {
			size_t n = remaining;
			if (n > moxie_secalloc_noise_len) {
				n = moxie_secalloc_noise_len;
			}
			memcpy(dst + off, moxie_secalloc_noise, n);
			off += n;
			remaining -= n;
		}
	}
}

// Write a lockdown marker to the notification fd. Non-blocking: if the
// pipe is full or invalid we just give up — the process is about to die
// anyway and the parent will observe child death as a backstop.
//
// The marker string is async-signal-safe: it's a fixed constant in .rodata,
// not heap data, and write(2) is on POSIX's async-signal-safe list. Writing
// a human-readable string (rather than a single byte) makes the milestone-1
// test observable via stderr; milestone-2 will replace this with a framed
// IPC byte on a spawn-inherited pipe.
static void moxie_secalloc_notify(void) {
	if (moxie_secalloc_lockdown_fd < 0) {
		return;
	}
	static const char marker[] = "MOXIE_SECALLOC_LOCKDOWN\n";
	ssize_t r = write(moxie_secalloc_lockdown_fd, marker, sizeof(marker) - 1);
	(void)r;
}

// Moxie-facing: run the full lockdown sequence (wipe every registered arena
// with noise, then write the lockdown marker). Shared entry point for both
// the fatal-signal handler (via moxie_secalloc_on_fatal_signal) and the
// explicit SecureLockdown primitive. One body, two triggers — keeps the
// "something fired the wipe" semantics identical regardless of who fired it.
//
// INVARIANT — DO NOT VIOLATE:
// Everything reachable from this function must be async-signal-safe. That
// currently means: memcpy (pure compute, POSIX-safe) and write(2) (on the
// POSIX async-signal-safe list). No malloc, no pthread primitives, no stdio
// (printf/fprintf), no locks, no non-reentrant libc (getenv, localtime,
// strerror, etc.). The registry and noise buffer are fixed at init and
// read-only from here.
//
// This is the constraint that lets ONE function body serve BOTH triggers:
// any signal-safe routine is also regular-safe (the signal-safe subset is
// strictly smaller). If a future modification needs logging, allocation,
// or locking, the signal-safe property breaks and the two callers must be
// split into separate code paths — moxie_secalloc_on_fatal_signal stays
// signal-safe, and the explicit path gets its own relaxed implementation.
// Do not "just add a log line here" without splitting first.
void moxie_secalloc_lockdown(void) {
	moxie_secalloc_wipe_all();
	moxie_secalloc_notify();
}

// Moxie-facing: overwrite a single caller-supplied buffer with the current
// noise pattern. Unlike moxie_secalloc_lockdown this does not touch the
// arena registry and does not write the notify marker — it is a targeted
// wipe for point-in-time residency minimization, invoked by SecureClear
// at application context-change boundaries (logout, navigation, tab
// backgrounding). The buffer need not be SecureAlloc'd; SecureClear is
// also valid on ordinary heap slices.
//
// Repeats the noise pattern if len > noise_len. No-op if the noise buffer
// has not been configured yet (pre-init caller).
void moxie_secalloc_clear(void *base, size_t len) {
	if (moxie_secalloc_noise == NULL || moxie_secalloc_noise_len == 0) {
		return;
	}
	uint8_t *dst = (uint8_t *)base;
	size_t   off = 0;
	while (len > 0) {
		size_t n = len;
		if (n > moxie_secalloc_noise_len) {
			n = moxie_secalloc_noise_len;
		}
		memcpy(dst + off, moxie_secalloc_noise, n);
		off += n;
		len -= n;
	}
}

// Called from runtime_unix.c's signal_handler at the very start of a fatal
// signal. Delegates to moxie_secalloc_lockdown so fault-triggered and
// explicit lockdowns share one code path. Must be async-signal-safe.
void moxie_secalloc_on_fatal_signal(void) {
	moxie_secalloc_lockdown();
}

// Moxie-facing: attempt to replace the anonymous data pages at addr with
// memfd_secret(2)-backed secretmem. Returns 0 on success, -1 on failure.
//
// memfd_secret was added in Linux 5.14. Pages from a secretmem mapping:
//   - are excluded from the kernel direct map — the kernel itself cannot
//     read them through /proc/<pid>/mem or ptrace(PTRACE_PEEKDATA)
//   - are never swapped (implicit mlock, no RLIMIT_MEMLOCK cost)
//   - are destroyed when the last mapping is unmapped or the fd is closed
//   - cannot be shared with another process via fork or file descriptor
//
// The sequence is: memfd_secret → ftruncate to size → mmap over the existing
// VA with MAP_SHARED|MAP_FIXED → close the fd. MAP_FIXED replaces the prior
// anonymous mapping atomically; the backing pages become secretmem while the
// VA is preserved so the guard pages on either side remain in place and the
// caller's pointer into the arena is unchanged. The fd is dropped immediately
// after mmap — the mapping keeps the underlying memfd alive until munmap.
//
// Failure is not an error. On Darwin or on Linux kernels without support the
// caller keeps its existing mmap+mlock mapping, which is still secure via
// guard pages and mlock. The secretmem path is a gold-standard upgrade, not
// a prerequisite — the secalloc API above doesn't know or care which path
// succeeded, and a mixed process (some arenas secretmem, some not) is fine.
int moxie_secalloc_try_secretmem(void *addr, size_t len) {
#ifdef __linux__
	long fd = syscall(SYS_memfd_secret, 0UL);
	if (fd < 0) {
		return -1;
	}
	if (ftruncate((int)fd, (off_t)len) != 0) {
		close((int)fd);
		return -1;
	}
	void *p = mmap(addr, len, PROT_READ | PROT_WRITE,
		MAP_SHARED | MAP_FIXED, (int)fd, 0);
	close((int)fd);
	if (p == MAP_FAILED || p != addr) {
		return -1;
	}
	return 0;
#else
	(void)addr;
	(void)len;
	return -1;
#endif
}
