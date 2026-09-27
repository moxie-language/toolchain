//go:build none
//:build linux
//
// The //go:build none above is the upstream marker for a file that Go's
// toolchain must not compile on its own; these are included manually for
// Linux targets. Stage4 selects files with //:build, so without this it
// compiles them for every target - and they include Linux headers.

// This file is included on Darwin and Linux (despite the //go:build line above).

#define _GNU_SOURCE
#define _XOPEN_SOURCE
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <ucontext.h>

// Coverage dump support: read an environment variable and open a path for
// writing, with the mode bits taken from the C headers rather than hardcoded.
const char *moxie_cover_env(const char *name) {
	return getenv(name);
}

// moxie_cover_open opens the coverage dump for appending. One file accumulates
// every process and every spawn domain of a run: each test starts a fresh relay
// and a spawn domain is a fork whose copy of the counter table is private, so
// the dump has to be additive rather than a rewrite. O_APPEND makes each write
// land at the end atomically, so domains that exit at the same time cannot
// overwrite one another; the report tool sums the lines.
int32_t moxie_cover_open(const char *path) {
	return open(path, O_WRONLY | O_CREAT | O_APPEND, 0644);
}

int32_t moxie_cover_close(int32_t fd) {
	return close(fd);
}

void moxie_handle_fatal_signal(int sig, uintptr_t addr);
void moxie_secalloc_on_fatal_signal(void);

static void signal_handler(int sig, siginfo_t *info, void *context) {
	// Secure allocator defence: synchronously wipe every registered guarded
	// arena and notify the lockdown pipe BEFORE doing anything else. Must
	// happen before the handler returns so no attacker code can run between
	// fault detection and secret erasure. If no arenas are registered this
	// is a no-op and the existing fatal path is unchanged.
	moxie_secalloc_on_fatal_signal();

	ucontext_t* uctx = context;
	uintptr_t addr = 0;
	#if __APPLE__
		#if __arm64__
			addr = uctx->uc_mcontext->__ss.__pc;
		#elif __x86_64__
			addr = uctx->uc_mcontext->__ss.__rip;
		#else
			#error unknown architecture
		#endif
	#elif __linux__
		// Note: this can probably be simplified using the MC_PC macro in musl,
		// but this works for now.
		#if __arm__
			addr = uctx->uc_mcontext.arm_pc;
		#elif __i386__
			addr = uctx->uc_mcontext.gregs[REG_EIP];
		#elif __x86_64__
			addr = uctx->uc_mcontext.gregs[REG_RIP];
		#else // aarch64, mips, maybe others
			addr = uctx->uc_mcontext.pc;
		#endif
	#else
		#error unknown platform
	#endif
	moxie_handle_fatal_signal(sig, addr);
}

void moxie_register_fatal_signals(void) {
	// Ignore SIGPIPE — network servers must not die on write-to-closed-socket.
	// Go's runtime does this; moxie must too.
	signal(SIGPIPE, SIG_IGN);

	struct sigaction act = { 0 };
	// SA_SIGINFO:   we want the 2 extra parameters
	// SA_RESETHAND: only catch the signal once (the handler will re-raise the signal)
	act.sa_flags = SA_SIGINFO | SA_RESETHAND;
	act.sa_sigaction = &signal_handler;

	// Register the signal handler for common issues. There are more signals,
	// which can be added if needed.
	sigaction(SIGBUS, &act, NULL);
	sigaction(SIGILL, &act, NULL);
	sigaction(SIGSEGV, &act, NULL);
}
