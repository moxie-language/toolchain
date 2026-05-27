//go:build none

// spawn_unix.c - C helpers for spawn that call libc functions.
// These avoid duplicate //export declarations in Go.

#include <sys/types.h>
#include <sys/wait.h>
#include <signal.h>
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

// Pipe I/O — wraps read/write for the IPC channel bridge.
// Uses libc on all platforms since these are standard POSIX.

int moxie_write(int fd, const void *buf, int count) {
    return (int)write(fd, buf, (size_t)count);
}

int moxie_read(int fd, void *buf, int count) {
    return (int)read(fd, buf, (size_t)count);
}

int moxie_waitpid(int pid, int *status, int options) {
    return (int)waitpid((pid_t)pid, status, options);
}

int moxie_kill(int pid, int sig) {
    return kill((pid_t)pid, sig);
}

// Pipe creation — wraps libc pipe(2). Returns 0 on success, -1 on failure.
// fds[0] is the read end, fds[1] is the write end. Used by user code that
// wants to set up an inherited fd for the secure-allocator lockdown channel
// (or any other parent/child notification path).
int moxie_pipe(int fds[2]) {
    return pipe(fds);
}

// moxie_setnonblock sets O_NONBLOCK on fd via fcntl. Returns 0 on success, -1 on error.
// Used by evloop to set fds non-blocking without going through the syscall package's
// F_GETFL/F_SETFL two-step when we know the fd is freshly created.
#include <fcntl.h>
int moxie_setnonblock(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) return flags;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

// moxie_peek does a non-blocking peek read on fd to detect data /
// remote close. Returns: 0 if remote closed (EOF), -1 if no data
// available right now (EAGAIN/EWOULDBLOCK or transient error), >0 if
// data is available. Uses recvfrom(MSG_PEEK|MSG_DONTWAIT) via raw
// syscall so we don't pull in musl networking dependencies. The
// child-side fd in spawn machinery is always a socketpair, so MSG_PEEK
// is supported.
//
// Linux raw recvfrom: 6-arg syscall with src_addr=NULL, addrlen=NULL.
// Darwin: use libc recvfrom (always linked).

#ifdef __APPLE__

#include <sys/socket.h>
#include <errno.h>
int moxie_peek(int fd, void *buf, int count) {
    ssize_t n = recvfrom(fd, buf, (size_t)count, 0x42 /* MSG_PEEK | MSG_DONTWAIT */, 0, 0);
    if (n == 0) return 0;
    if (n > 0) return (int)n;
    if (errno == 35 /* EAGAIN on Darwin */) return -1;
    return -1;
}

#else // Linux — raw recvfrom syscall.

#if defined(__x86_64__)
#define SYS_RECVFROM 45

long __moxie_syscall6(long n, long a, long b, long c, long d, long e, long f);
__asm__(
".global __moxie_syscall6\n"
"__moxie_syscall6:\n"
"    mov %rdi, %rax\n"     // syscall number
"    mov %rsi, %rdi\n"     // arg1 (fd)
"    mov %rdx, %rsi\n"     // arg2 (buf)
"    mov %rcx, %rdx\n"     // arg3 (len)
"    mov %r8,  %r10\n"     // arg4 (flags) — syscall uses r10 not rcx
"    mov %r9,  %r8\n"      // arg5 (src_addr)
"    mov 8(%rsp), %r9\n"   // arg6 (addrlen) — from stack
"    syscall\n"
"    ret\n"
);

#elif defined(__aarch64__)
#define SYS_RECVFROM 207

long __moxie_syscall6(long n, long a, long b, long c, long d, long e, long f);
__asm__(
".global __moxie_syscall6\n"
"__moxie_syscall6:\n"
"    mov x8, x0\n"         // syscall number
"    mov x0, x1\n"
"    mov x1, x2\n"
"    mov x2, x3\n"
"    mov x3, x4\n"
"    mov x4, x5\n"
"    mov x5, x6\n"
"    svc #0\n"
"    ret\n"
);

#else
#error "Unsupported architecture for moxie_peek"
#endif

int moxie_peek(int fd, void *buf, int count) {
    // MSG_PEEK = 2, MSG_DONTWAIT = 0x40 on Linux (per <bits/socket_type.h>).
    long n = __moxie_syscall6(SYS_RECVFROM, fd, (long)buf, (long)count, 0x42, 0, 0);
    if (n == 0) return 0;          // remote closed
    if (n > 0) return (int)n;      // data available
    // Linux returns negative errno on syscall failure. -EAGAIN = -11.
    return -1;
}

// moxie_send_nb does a non-blocking send via sendto(MSG_DONTWAIT).
// Returns: bytes sent (>0), 0 if would block (EAGAIN), -1 on other
// error. Used by trySend on pipe-bound channels so chanSelect's
// recheck loop never blocks the parent.
//
// sys_sendto = 44 on x86_64, 206 on aarch64. Same 6-arg shape as
// recvfrom (dest_addr=NULL, addrlen=0).
#if defined(__x86_64__)
#define SYS_SENDTO 44
#elif defined(__aarch64__)
#define SYS_SENDTO 206
#endif

int moxie_send_nb(int fd, const void *buf, int count) {
    long n = __moxie_syscall6(SYS_SENDTO, fd, (long)buf, (long)count, 0x40 /* MSG_DONTWAIT */, 0, 0);
    if (n >= 0) return (int)n;
    // Linux returns negative errno. -EAGAIN = -11, -EWOULDBLOCK = -11.
    if (n == -11) return 0;
    return -1;
}

#endif // __APPLE__

#ifdef __APPLE__
// Darwin: libc sendto with MSG_DONTWAIT.
int moxie_send_nb(int fd, const void *buf, int count) {
    ssize_t n = sendto(fd, buf, (size_t)count, 0x80 /* MSG_DONTWAIT on Darwin */, 0, 0);
    if (n >= 0) return (int)n;
    if (errno == 35 /* EAGAIN on Darwin */) return 0;
    return -1;
}
#endif

// moxie_set_spawn_bufs bumps SO_SNDBUF and SO_RCVBUF on both ends of a
// freshly created socketpair to 8MB. The default kernel buffer (~212KB)
// saturates under event bursts, blocking the parent's epoll loop.
#define SPAWN_SOCK_BUF (8 << 20)

#ifdef __APPLE__
void moxie_set_spawn_bufs(int fd0, int fd1) {
    int sz = SPAWN_SOCK_BUF;
    setsockopt(fd0, SOL_SOCKET, SO_SNDBUF, &sz, sizeof(sz));
    setsockopt(fd0, SOL_SOCKET, SO_RCVBUF, &sz, sizeof(sz));
    setsockopt(fd1, SOL_SOCKET, SO_SNDBUF, &sz, sizeof(sz));
    setsockopt(fd1, SOL_SOCKET, SO_RCVBUF, &sz, sizeof(sz));
}
#else // Linux — raw setsockopt syscall.
// SOL_SOCKET=1  SO_SNDBUF=7  SO_RCVBUF=8
// SYS_SETSOCKOPT: x86_64=54, aarch64=208
#if defined(__x86_64__)
#define SYS_SETSOCKOPT 54
#elif defined(__aarch64__)
#define SYS_SETSOCKOPT 208
#endif
static void set_sock_buf(int fd, int optname) {
    int sz = SPAWN_SOCK_BUF;
    __moxie_syscall6(SYS_SETSOCKOPT, fd, 1, optname, (long)&sz, 4, 0);
}
void moxie_set_spawn_bufs(int fd0, int fd1) {
    set_sock_buf(fd0, 7); // SO_SNDBUF
    set_sock_buf(fd0, 8); // SO_RCVBUF
    set_sock_buf(fd1, 7);
    set_sock_buf(fd1, 8);
}
#endif // __APPLE__

// moxie_fd_can_send returns 1 if the fd's send buffer has space (write
// would not block), 0 otherwise. Uses raw poll syscall with POLLOUT and
// zero timeout — returns immediately without libc.
// struct pollfd layout: int fd (4), short events (2), short revents (2)
// POLLOUT = 4. poll syscall: x86_64=7, aarch64=73.
#ifdef __APPLE__
#include <poll.h>
int moxie_fd_can_send(int fd) {
    struct pollfd pfd = { fd, 4 /* POLLOUT */, 0 };
    int r = poll(&pfd, 1, 0);
    return (r > 0 && (pfd.revents & 4)) ? 1 : 0;
}
#elif defined(__x86_64__)
#define SYS_POLL 7
int moxie_fd_can_send(int fd) {
    struct { int fd; short events; short revents; } pfd = { fd, 4, 0 };
    long r = __moxie_syscall4(SYS_POLL, (long)&pfd, 1, 0, 0);
    return (r > 0 && (pfd.revents & 4)) ? 1 : 0;
}
#elif defined(__aarch64__)
#define SYS_PPOLL 73
/* ppoll(fds, nfds, tspec=NULL, sigmask=NULL) where tspec=NULL means timeout=0 */
int moxie_fd_can_send(int fd) {
    struct { int fd; short events; short revents; } pfd = { fd, 4, 0 };
    long r = __moxie_syscall4(SYS_PPOLL, (long)&pfd, 1, 0, 0);
    return (r > 0 && (pfd.revents & 4)) ? 1 : 0;
}
#endif

// moxie_exec_binary closes all fds except stdin/stdout/stderr and ipc_fd,
// sets MOXIE_IPC_FD in the environment, then replaces the process image
// with the binary at path. Never returns on success.
// Returns -1 on failure (execve failed; caller should exit the child).
#include <sys/resource.h>
#include <stdio.h>
#include <stdlib.h>  // setenv, environ
int moxie_exec_binary(const char *path, int ipc_fd) {
    // Close all fds except stdin(0), stdout(1), stderr(2), and ipc_fd.
    struct rlimit rl;
    int max_fd = 1024;
    if (getrlimit(RLIMIT_NOFILE, &rl) == 0)
        max_fd = (int)rl.rlim_cur;
    for (int fd = 3; fd < max_fd; fd++) {
        if (fd != ipc_fd)
            close(fd);
    }
    char fd_str[32];
    snprintf(fd_str, sizeof(fd_str), "%d", ipc_fd);
    setenv("MOXIE_IPC_FD", fd_str, 1);
    char *argv[] = { (char *)path, NULL };
    extern char **environ;
    execve(path, argv, environ);
    return -1; // execve failed
}

// moxie_spawn_call invokes the raw goroutine-style wrapper generated by
// compiler.createGoroutineStartWrapper. That wrapper has the C prototype
// `void (*)(void *)` — it takes the packed-args bundle as its single
// argument. We can't express this call in Moxie because `func(unsafe.Pointer)`
// is a fat pointer (funcptr+context); casting a uintptr into that type
// reads past the end of the allocation and calls garbage.
void moxie_spawn_call(void (*fn)(void *), void *args) {
    fn(args);
}

// Atomic operations for SPSC ring buffer (cross-process via MAP_SHARED).
// Acquire/release semantics ensure visibility across fork'd processes.
#include <stdint.h>

uint32_t moxie_atomic_load_u32(volatile uint32_t *p) {
    return __atomic_load_n(p, __ATOMIC_ACQUIRE);
}

void moxie_atomic_store_u32(volatile uint32_t *p, uint32_t val) {
    __atomic_store_n(p, val, __ATOMIC_RELEASE);
}
