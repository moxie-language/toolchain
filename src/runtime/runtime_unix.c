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
void moxie_register_fatal_signals(void);

// The handler's own stack, installed at registration. A fault raised while the
// handler runs - and this handler re-raises the signal itself - would otherwise
// run on the alternate stack too, and a SIGSEGV on the alternate stack becomes
// an immediate kernel abort with no report at all.
static char handler_stack[1 << 20] __attribute__((aligned(16)));

static void signal_handler(int sig, siginfo_t *info, void *context) {
	// Secure allocator defence: synchronously wipe every registered guarded
	// arena and notify the lockdown pipe BEFORE doing anything else. Must
	// happen before the handler returns so no attacker code can run between
	// fault detection and secret erasure. If no arenas are registered this
	// is a no-op and the existing fatal path is unchanged.
	moxie_secalloc_on_fatal_signal();

	ucontext_t *uctx = (ucontext_t *)context;
	// Report the INSTRUCTION that faulted, never si_addr. si_addr is the
	// operand: 0 for a jump or call through a wild pointer, the bad address for
	// a bad field read. It never resolves to a function, and every report in
	// this project that fed it to addr2line named a function that did not
	// fault - the symbol changed between builds because only the test harness
	// was changing. The instruction resolves to the function whose body
	// faulted whenever that body is not a leaf of -O2 inlining.
	uintptr_t fault_addr = 0;
	#if __APPLE__
		#if __arm64__
			fault_addr = uctx->uc_mcontext->__ss.__pc;
		#elif __x86_64__
			fault_addr = uctx->uc_mcontext->__ss.__rip;
		#endif
	#elif __linux__
		#if __arm__
			fault_addr = uctx->uc_mcontext.arm_pc;
		#elif __i386__
			fault_addr = uctx->uc_mcontext.gregs[REG_EIP];
		#elif __x86_64__
			fault_addr = uctx->uc_mcontext.gregs[REG_RIP];
		#else
			fault_addr = uctx->uc_mcontext.pc;
		#endif
	#endif
	// No chain is printed: this runtime emits no frame pointers, and the
	// stack scan that would reconstruct one cannot tell a data word from an
	// address in these binaries.
	moxie_handle_fatal_signal(sig, fault_addr);
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
	// One alternate stack per thread, so a stack-overflow SIGSEGV can still be
	// reported, and so the report never runs on the faulting stack.
	stack_t ss = { 0 };
	ss.ss_sp = handler_stack;
	ss.ss_size = sizeof(handler_stack);
	ss.ss_flags = 0;
	sigaltstack(&ss, NULL);
	act.sa_flags |= SA_ONSTACK;

	sigaction(SIGBUS, &act, NULL);
	sigaction(SIGILL, &act, NULL);
	sigaction(SIGSEGV, &act, NULL);
}
