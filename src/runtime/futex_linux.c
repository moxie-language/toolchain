//go:build none
//:build linux
//
// The //go:build none above is the upstream marker for a file that Go's
// toolchain must not compile on its own; these are included manually for
// Linux targets. Stage4 selects files with //:build, so without this it
// compiles them for every target - and they include Linux headers.

// This file is manually included, to avoid CGo which would cause a circular
// import.

#include <limits.h>
#include <stdint.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

#define FUTEX_WAIT         0
#define FUTEX_WAKE         1
#define FUTEX_PRIVATE_FLAG 128

void moxie_futex_wait(uint32_t *addr, uint32_t cmp) {
    syscall(SYS_futex, addr, FUTEX_WAIT|FUTEX_PRIVATE_FLAG, cmp, NULL, NULL, 0);
}

void moxie_futex_wait_timeout(uint32_t *addr, uint32_t cmp, uint64_t timeout) {
    struct timespec ts = {0};
    ts.tv_sec = timeout / 1000000000;
    ts.tv_nsec = timeout % 1000000000;
    syscall(SYS_futex, addr, FUTEX_WAIT|FUTEX_PRIVATE_FLAG, cmp, &ts, NULL, 0);
}

// Cross-process futex on a MAP_SHARED word: FUTEX_PRIVATE_FLAG keys the wait
// queue per mm, so a child's wake would never reach the parent's wait.
void moxie_futex_wait_timeout_shared(uint32_t *addr, uint32_t cmp, uint64_t timeout) {
    struct timespec ts = {0};
    ts.tv_sec = timeout / 1000000000;
    ts.tv_nsec = timeout % 1000000000;
    syscall(SYS_futex, addr, FUTEX_WAIT, cmp, &ts, NULL, 0);
}

void moxie_futex_wake_all_shared(uint32_t *addr) {
    syscall(SYS_futex, addr, FUTEX_WAKE, INT_MAX, NULL, NULL, 0);
}

void moxie_futex_wake(uint32_t *addr) {
    syscall(SYS_futex, addr, FUTEX_WAKE|FUTEX_PRIVATE_FLAG, 1, NULL, NULL, 0);
}

void moxie_futex_wake_all(uint32_t *addr) {
    syscall(SYS_futex, addr, FUTEX_WAKE|FUTEX_PRIVATE_FLAG, INT_MAX, NULL, NULL, 0);
}
