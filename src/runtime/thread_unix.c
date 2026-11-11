//go:build none
//:build linux
//
// The //go:build none above is the upstream marker for a file that Go's
// toolchain must not compile on its own; these are included manually for
// Linux targets. Stage4 selects files with //:build, so without this it
// compiles them for every target - and they include pthread headers.
//
// A Moxie domain is a THREAD, not a process. The runtime's whole concurrency
// model is cooperative - one thread of execution per domain, no shared-memory
// window between them, an event loop that observes its breaker between tasks -
// and a thread is exactly that: a second execution context in the same address
// space with its own TLS (domain root, arenas, package globals) and its own
// stack. Forking instead gave each domain a PID it never needed, made
// termination a signal problem (pdeathsig, waitpid, a reaper, a pid table) and
// let a wedged domain outlive its owner as a stray process.
//
// The thread gets its own TLS block, so InitCShared runs again inside it: fresh
// domain root, fresh arenas, and the package initialisers, so the child's
// globals are its own.

#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>

// Domain stack sizes are per role, not one constant. A worker domain that runs
// a package's initialisers and a deep codec needs a large stack; an event-loop
// domain that dispatches short handlers does not, and 64 MB per domain was
// simply committed address space times 9-15 threads. The default stays
// conservative and a program chooses the per-role size with MOXIE_STACK_BYTES:
//
//   MOXIE_STACK_BYTES=16777216 normgr
//
// or, for a finer split, MOXIE_STACK_BYTES_MAIN (the main domain) and
// MOXIE_STACK_BYTES_WORKER (every spawned domain). A value below the minimum or
// above the cap is refused, not clamped: a stack the runtime cannot honour is a
// crash waiting for the frame that needs it.
#define MOXIE_STACK_MIN (256UL * 1024UL)
#define MOXIE_STACK_MAX (512UL * 1024UL * 1024UL)
#define MOXIE_STACK_DEFAULT (16UL * 1024UL * 1024UL)

static uint32_t moxie_stack_bytes_from(const char *name) {
    const char *v = getenv(name);
    if (v == NULL || *v == '\0') {
        return 0;
    }
    char *end = NULL;
    unsigned long long n = strtoull(v, &end, 10);
    if (end == v || (end != NULL && *end != '\0')) {
        return 0;
    }
    if (n < MOXIE_STACK_MIN || n > MOXIE_STACK_MAX) {
        return 0;
    }
    return (uint32_t)n;
}

// moxie_domain_stack_bytes is the stack a spawned domain gets.
uint32_t moxie_domain_stack_bytes(void) {
    uint32_t n = moxie_stack_bytes_from("MOXIE_STACK_BYTES");
    if (n == 0) {
        n = moxie_stack_bytes_from("MOXIE_STACK_BYTES_WORKER");
    }
    if (n == 0) {
        n = (uint32_t)MOXIE_STACK_DEFAULT;
    }
    return n;
}

// moxie_runtime_thread_entry is the Moxie side of a spawn: it initialises the
// domain (InitCShared), binds the breaker and the channel rings, then calls the
// target function. Exported from src/runtime/spawn.mx.
extern void moxie_runtime_thread_entry(void *arg);

static void *moxie_thread_tramp(void *arg) {
    moxie_runtime_thread_entry(arg);
    return NULL;
}

// moxie_thread_spawn starts one domain thread. The stack matches the compiler's
// worker threads: a package's type graph and IR are deep, and a domain's frames
// live on this stack for the life of the domain - but the size is the
// program's choice (moxie_domain_stack_bytes), not a constant in the runtime.
int32_t moxie_thread_spawn(void *arg) {
    pthread_attr_t attr;
    if (pthread_attr_init(&attr) != 0) {
        return -1;
    }
    pthread_attr_setstacksize(&attr, (size_t)moxie_domain_stack_bytes());
    pthread_t tid;
    int err = pthread_create(&tid, &attr, moxie_thread_tramp, arg);
    pthread_attr_destroy(&attr);
    return err == 0 ? 0 : -1;
}
