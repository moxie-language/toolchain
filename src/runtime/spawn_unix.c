//go:build none

// spawn_unix.c - C helpers for spawn that call libc functions.
// These avoid duplicate //export declarations in Go.

#include <sys/types.h>
#include <unistd.h>

int moxie_fork(void) {
    return (int)fork();
}

int moxie_close(int fd) {
    return close(fd);
}

// socketpair implementation: use libc on Darwin, raw syscall on Linux.

#ifdef __APPLE__

#include <sys/socket.h>
int moxie_socketpair(int domain, int type, int protocol, int fds[2]) {
    return socketpair(domain, type, protocol, fds);
}

#else // Linux — raw syscall to avoid musl networking deps.

#if defined(__x86_64__)

#define SYS_SOCKETPAIR 53

long __moxie_syscall4(long n, long a, long b, long c, long d);
__asm__(
".global __moxie_syscall4\n"
"__moxie_syscall4:\n"
"    mov %rdi, %rax\n"     // syscall number
"    mov %rsi, %rdi\n"     // arg1
"    mov %rdx, %rsi\n"     // arg2
"    mov %rcx, %rdx\n"     // arg3
"    mov %r8, %r10\n"      // arg4 (syscall uses r10 not rcx)
"    syscall\n"
"    ret\n"
);

#elif defined(__aarch64__)

#define SYS_SOCKETPAIR 199

long __moxie_syscall4(long n, long a, long b, long c, long d);
__asm__(
".global __moxie_syscall4\n"
"__moxie_syscall4:\n"
"    mov x8, x0\n"         // syscall number
"    mov x0, x1\n"         // arg1
"    mov x1, x2\n"         // arg2
"    mov x2, x3\n"         // arg3
"    mov x3, x4\n"         // arg4
"    svc #0\n"
"    ret\n"
);

#else
#error "Unsupported architecture for moxie spawn syscall"
#endif

int moxie_socketpair(int domain, int type, int protocol, int fds[2]) {
    return (int)__moxie_syscall4(SYS_SOCKETPAIR, domain, type, protocol, (long)fds);
}

#endif // __APPLE__
