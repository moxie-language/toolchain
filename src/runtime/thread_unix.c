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
// live on this stack for the life of the domain.
int32_t moxie_thread_spawn(void *arg) {
    pthread_attr_t attr;
    if (pthread_attr_init(&attr) != 0) {
        return -1;
    }
    pthread_attr_setstacksize(&attr, 67108864UL);
    pthread_t tid;
    int err = pthread_create(&tid, &attr, moxie_thread_tramp, arg);
    pthread_attr_destroy(&attr);
    return err == 0 ? 0 : -1;
}
