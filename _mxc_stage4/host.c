#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/wait.h>
#include <sys/syscall.h>
#include <sys/resource.h>
#include <sys/mman.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdatomic.h>
#include <sys/sysinfo.h>

__attribute__((constructor))
static void raise_stack_limit(void) {
    struct rlimit rl;
    getrlimit(RLIMIT_STACK, &rl);
    if (rl.rlim_cur < 268435456UL) {
        rl.rlim_cur = 268435456UL;
        setrlimit(RLIMIT_STACK, &rl);
    }
}

int32_t mxc_writefile(const char* path, int32_t pathLen, const char* data, int32_t dataLen, uint32_t mode) {
    char pbuf[4096];
    if (pathLen > 4095) pathLen = 4095;
    memcpy(pbuf, path, pathLen);
    pbuf[pathLen] = 0;
    int fd = syscall(SYS_openat, -100, pbuf, O_WRONLY|O_CREAT|O_TRUNC, mode);
    if (fd < 0) return -1;
    int32_t written = 0;
    while (written < dataLen) {
        long n = syscall(SYS_write, fd, data + written, dataLen - written);
        if (n <= 0) break;
        written += (int32_t)n;
    }
    syscall(SYS_close, fd);
    return written;
}

int32_t mxc_mkdir(const char* path, int32_t pathLen, uint32_t mode) {
    char pbuf[4096];
    if (pathLen > 4095) return -1;
    int i;
    for (i = 1; i <= pathLen; i++) {
        if (i == pathLen || path[i] == '/') {
            memcpy(pbuf, path, i);
            pbuf[i] = 0;
            syscall(SYS_mkdirat, -100, pbuf, mode);
        }
    }
    return 0;
}

int32_t mxc_readfile(const char* path, int32_t pathLen, char* buf, int32_t bufCap) {
    char pbuf[4096];
    if (pathLen > 4095) pathLen = 4095;
    memcpy(pbuf, path, pathLen);
    pbuf[pathLen] = 0;
    int fd = syscall(SYS_openat, -100, pbuf, O_RDONLY, 0);
    if (fd < 0) return -1;
    int32_t total = 0;
    while (total < bufCap) {
        long n = syscall(SYS_read, fd, buf + total, bufCap - total);
        if (n <= 0) break;
        total += (int32_t)n;
    }
    syscall(SYS_close, fd);
    return total;
}

int32_t mxc_filesize(const char* path, int32_t pathLen) {
    char pbuf[4096];
    if (pathLen > 4095) pathLen = 4095;
    memcpy(pbuf, path, pathLen);
    pbuf[pathLen] = 0;
    int fd = syscall(SYS_openat, -100, pbuf, O_RDONLY, 0);
    if (fd < 0) return -1;
    long size = syscall(SYS_lseek, fd, 0, 2); /* SEEK_END=2 */
    syscall(SYS_close, fd);
    return (int32_t)size;
}

int32_t mxc_run(const char** argv, int32_t argc) {
    int pid = fork();
    if (pid < 0) return -1;
    if (pid == 0) {
        char** args = (char**)malloc((argc + 1) * sizeof(char*));
        if (!args) _exit(127);
        int i;
        for (i = 0; i < argc; i++) args[i] = (char*)argv[i];
        args[i] = NULL;
        execvp(args[0], args);
        _exit(127);
    }
    int status;
    waitpid(pid, &status, 0);
    if (WIFEXITED(status)) return WEXITSTATUS(status);
    return -1;
}

int32_t mxc_run_async(const char** argv, int32_t argc) {
    int pid = fork();
    if (pid < 0) return -1;
    if (pid == 0) {
        char** args = (char**)malloc((argc + 1) * sizeof(char*));
        if (!args) _exit(127);
        int i;
        for (i = 0; i < argc; i++) args[i] = (char*)argv[i];
        args[i] = NULL;
        execvp(args[0], args);
        _exit(127);
    }
    return pid;
}

/* Fork without exec. Returns 0 in child, pid in parent, -1 on error.
   Child inherits the full address space (COW) so all loaded compiler
   state (importRegistry, type info, etc.) is immediately available. */
int32_t mxc_fork(void* ctx) {
    return (int32_t)fork();
}

/* Reap any child. Returns pid on exit 0, -pid on failure, -1 if no children. */
int32_t mxc_wait_any(void* ctx) {
    int status;
    int pid = waitpid(-1, &status, 0);
    if (pid < 0) return -1;
    if (WIFEXITED(status) && WEXITSTATUS(status) == 0) return pid;
    return -pid;
}

struct linux_dirent64 {
    uint64_t d_ino;
    int64_t  d_off;
    uint16_t d_reclen;
    uint8_t  d_type;
    char     d_name[];
};

int32_t mxc_listdir(const char* dir, int32_t dirLen, char* buf, int32_t bufCap) {
    char dbuf[4096];
    if (dirLen > 4095) dirLen = 4095;
    memcpy(dbuf, dir, dirLen);
    dbuf[dirLen] = 0;
    int fd = syscall(SYS_openat, -100, dbuf, 0x10000, 0); /* AT_FDCWD=-100, O_RDONLY|O_DIRECTORY */
    if (fd < 0) return 0;
    int32_t pos = 0;
    char dbytes[4096];
    for (;;) {
        long nread = syscall(SYS_getdents64, fd, dbytes, sizeof(dbytes));
        if (nread <= 0) break;
        long offset = 0;
        while (offset < nread) {
            struct linux_dirent64* ent = (struct linux_dirent64*)(dbytes + offset);
            char* name = ent->d_name;
            if (!(name[0] == '.' && (name[1] == 0 || (name[1] == '.' && name[2] == 0)))) {
                int32_t nlen = (int32_t)strlen(name);
                if (pos + nlen + 1 <= bufCap) {
                    memcpy(buf + pos, name, nlen);
                    pos += nlen;
                    buf[pos] = 0;
                    pos++;
                }
            }
            offset += ent->d_reclen;
        }
    }
    syscall(SYS_close, fd);
    return pos;
}

int32_t mxc_getenv(const char* name, int32_t nameLen, char* buf, int32_t bufCap) {
    char nbuf[256];
    if (nameLen > 255) nameLen = 255;
    memcpy(nbuf, name, nameLen);
    nbuf[nameLen] = 0;
    const char* val = getenv(nbuf);
    if (!val) return 0;
    int32_t vlen = (int32_t)strlen(val);
    if (vlen > bufCap) vlen = bufCap;
    memcpy(buf, val, vlen);
    return vlen;
}

/* ---------------------------------------------------------------
   Ring buffer - SPSC lock-free, matches src/runtime/ringbuf.mx
   protocol: big-endian length prefix, 8-byte aligned messages,
   monotonically increasing indices with mask-based wrap.

   Layout: [writeIdx:4][readIdx:4][size:4][closed:4][data:size]
   Messages: [len:4 BE][payload:len][pad to align 8]
   --------------------------------------------------------------- */

typedef struct {
    _Atomic(uint32_t) writeIdx;
    _Atomic(uint32_t) readIdx;
    uint32_t          size;
    _Atomic(uint32_t) closed;
    char              data[];
} mxc_ring;

/* Round up to next power of 2 */
static uint32_t next_pow2(uint32_t v) {
    v--;
    v |= v >> 1; v |= v >> 2; v |= v >> 4;
    v |= v >> 8; v |= v >> 16;
    return v + 1;
}

static uint32_t ring_align8(uint32_t n) { return (n + 7) & ~(uint32_t)7; }

void* mxc_ring_create(int32_t dataSize) {
    uint32_t cap = next_pow2((uint32_t)dataSize);
    if (cap < 4096) cap = 4096;
    size_t total = sizeof(mxc_ring) + cap;
    mxc_ring* r = (mxc_ring*)mmap(NULL, total, PROT_READ|PROT_WRITE,
                                  MAP_PRIVATE|MAP_ANONYMOUS, -1, 0);
    if (r == MAP_FAILED) return NULL;
    atomic_store_explicit(&r->writeIdx, 0, memory_order_relaxed);
    atomic_store_explicit(&r->readIdx,  0, memory_order_relaxed);
    r->size = cap;
    atomic_store_explicit(&r->closed, 0, memory_order_relaxed);
    return r;
}

void mxc_ring_destroy(void* ring) {
    if (!ring) return;
    mxc_ring* r = (mxc_ring*)ring;
    size_t total = sizeof(mxc_ring) + r->size;
    munmap(r, total);
}

void mxc_ring_close(void* ring) {
    mxc_ring* r = (mxc_ring*)ring;
    atomic_store_explicit(&r->closed, 1, memory_order_release);
}

int32_t mxc_ring_closed(void* ring) {
    mxc_ring* r = (mxc_ring*)ring;
    return atomic_load_explicit(&r->closed, memory_order_acquire) ? 1 : 0;
}

static void ring_write_bytes(mxc_ring* r, uint32_t pos, const char* src, uint32_t len) {
    uint32_t mask = r->size - 1;
    uint32_t off = pos & mask;
    uint32_t first = r->size - off;
    if (first >= len) {
        memcpy(r->data + off, src, len);
    } else {
        memcpy(r->data + off, src, first);
        memcpy(r->data, src + first, len - first);
    }
}

static void ring_read_bytes(mxc_ring* r, uint32_t pos, char* dst, uint32_t len) {
    uint32_t mask = r->size - 1;
    uint32_t off = pos & mask;
    uint32_t first = r->size - off;
    if (first >= len) {
        memcpy(dst, r->data + off, len);
    } else {
        memcpy(dst, r->data + off, first);
        memcpy(dst + first, r->data, len - first);
    }
}

/* Send a length-prefixed message. Big-endian length, 8-byte aligned.
   Returns 0=success, 1=full (retry), -1=closed. */
int32_t mxc_ring_send(void* ring, const char* data, int32_t len) {
    mxc_ring* r = (mxc_ring*)ring;
    if (atomic_load_explicit(&r->closed, memory_order_acquire)) return -1;
    uint32_t total = ring_align8(4 + (uint32_t)len);
    uint32_t w = atomic_load_explicit(&r->writeIdx, memory_order_relaxed);
    uint32_t rd = atomic_load_explicit(&r->readIdx, memory_order_acquire);
    uint32_t space = r->size - (w - rd);
    if (space < total) return 1; /* full */
    /* Big-endian length prefix (matches ringbuf.mx) */
    char hdr[4];
    hdr[0] = (char)((uint32_t)len >> 24);
    hdr[1] = (char)((uint32_t)len >> 16);
    hdr[2] = (char)((uint32_t)len >> 8);
    hdr[3] = (char)((uint32_t)len);
    ring_write_bytes(r, w, hdr, 4);
    if (len > 0) {
        ring_write_bytes(r, w + 4, data, (uint32_t)len);
    }
    /* Zero padding bytes for 8-byte alignment */
    uint32_t padStart = 4 + (uint32_t)len;
    uint32_t padEnd = total;
    if (padEnd > padStart) {
        char zeros[8] = {0};
        ring_write_bytes(r, w + padStart, zeros, padEnd - padStart);
    }
    atomic_store_explicit(&r->writeIdx, w + total, memory_order_release);
    return 0;
}

/* Peek at the next message length without consuming it.
   Returns payload length, 0=empty, -1=closed+empty. */
int32_t mxc_ring_peek_len(void* ring) {
    mxc_ring* r = (mxc_ring*)ring;
    uint32_t w = atomic_load_explicit(&r->writeIdx, memory_order_acquire);
    uint32_t rd = atomic_load_explicit(&r->readIdx, memory_order_relaxed);
    uint32_t avail = w - rd;
    if (avail < 4) {
        if (atomic_load_explicit(&r->closed, memory_order_acquire)) return -1;
        return 0;
    }
    char hdr[4];
    ring_read_bytes(r, rd, hdr, 4);
    uint32_t msgLen = ((uint32_t)(uint8_t)hdr[0] << 24) |
                      ((uint32_t)(uint8_t)hdr[1] << 16) |
                      ((uint32_t)(uint8_t)hdr[2] << 8)  |
                      ((uint32_t)(uint8_t)hdr[3]);
    uint32_t total = ring_align8(4 + msgLen);
    if (avail < total) return 0; /* partial message */
    return (int32_t)msgLen;
}

/* Receive a length-prefixed message. Returns payload length on success,
   0=empty, -1=closed+empty. Caller must provide buf >= msgLen (use peek_len first). */
int32_t mxc_ring_recv(void* ring, char* buf, int32_t bufcap) {
    mxc_ring* r = (mxc_ring*)ring;
    uint32_t w = atomic_load_explicit(&r->writeIdx, memory_order_acquire);
    uint32_t rd = atomic_load_explicit(&r->readIdx, memory_order_relaxed);
    uint32_t avail = w - rd;
    if (avail < 4) {
        if (atomic_load_explicit(&r->closed, memory_order_acquire)) return -1;
        return 0;
    }
    char hdr[4];
    ring_read_bytes(r, rd, hdr, 4);
    uint32_t msgLen = ((uint32_t)(uint8_t)hdr[0] << 24) |
                      ((uint32_t)(uint8_t)hdr[1] << 16) |
                      ((uint32_t)(uint8_t)hdr[2] << 8)  |
                      ((uint32_t)(uint8_t)hdr[3]);
    uint32_t total = ring_align8(4 + msgLen);
    if (avail < total) return 0; /* partial message */
    uint32_t copyLen = msgLen;
    if (copyLen > (uint32_t)bufcap) copyLen = (uint32_t)bufcap;
    if (copyLen > 0) {
        ring_read_bytes(r, rd + 4, buf, copyLen);
    }
    atomic_store_explicit(&r->readIdx, rd + total, memory_order_release);
    return (int32_t)msgLen;
}

/* ---------------------------------------------------------------
   Thread management - worker threads with status tracking
   --------------------------------------------------------------- */

/* Per-worker status: 0=running, 1=exited, 2=crashed.
   Allocated by parent, pointer shared with the thread. */
typedef struct {
    _Atomic(uint32_t) status;
} mxc_worker_status;

typedef struct {
    void (*fn)(void*);
    void* arg;
    mxc_worker_status* ws;
} mxc_thread_arg;

static void* mxc_thread_entry(void* raw) {
    mxc_thread_arg* ta = (mxc_thread_arg*)raw;
    void (*fn)(void*) = ta->fn;
    void* arg = ta->arg;
    mxc_worker_status* ws = ta->ws;
    free(ta);

    fn(arg);

    atomic_store_explicit(&ws->status, 1, memory_order_release);
    return NULL;
}

/* Spawn a worker thread.
   fn: worker entry point, called as fn(arg).
   arg: opaque argument passed to fn.
   handle_out: receives pthread_t (as uint64_t).
   status_out: receives mxc_worker_status* (as uint64_t).
   Returns 0 on success, -1 on failure. */
int32_t mxc_spawn_thread(void* fn, void* arg, void* handle_out, void* status_out) {
    mxc_worker_status* ws = (mxc_worker_status*)malloc(sizeof(mxc_worker_status));
    if (!ws) return -1;
    atomic_store_explicit(&ws->status, 0, memory_order_relaxed);

    mxc_thread_arg* ta = (mxc_thread_arg*)malloc(sizeof(mxc_thread_arg));
    if (!ta) { free(ws); return -1; }
    ta->fn  = (void (*)(void*))fn;
    ta->arg = arg;
    ta->ws  = ws;

    pthread_attr_t attr;
    pthread_attr_init(&attr);
    /* Give workers a large stack (64MB) matching the parent's raised limit */
    pthread_attr_setstacksize(&attr, 67108864UL);

    pthread_t tid;
    int err = pthread_create(&tid, &attr, mxc_thread_entry, ta);
    pthread_attr_destroy(&attr);
    if (err != 0) {
        free(ta);
        free(ws);
        return -1;
    }

    *(uint64_t*)handle_out = (uint64_t)tid;
    *(uint64_t*)status_out = (uint64_t)(uintptr_t)ws;
    return 0;
}

/* Check worker status: 1=alive, 0=exited, -1=crashed */
int32_t mxc_thread_alive(void* status_ptr) {
    mxc_worker_status* ws = (mxc_worker_status*)(uintptr_t)(*(uint64_t*)status_ptr);
    uint32_t s = atomic_load_explicit(&ws->status, memory_order_acquire);
    if (s == 0) return 1;  /* alive */
    if (s == 1) return 0;  /* exited */
    return -1;             /* crashed */
}

/* Join worker thread, then free status. */
void mxc_thread_join(void* handle_ptr, void* status_ptr) {
    pthread_t tid = (pthread_t)(*(uint64_t*)handle_ptr);
    pthread_join(tid, NULL);
    mxc_worker_status* ws = (mxc_worker_status*)(uintptr_t)(*(uint64_t*)status_ptr);
    free(ws);
}

void mxc_usleep(int32_t us) {
    usleep((useconds_t)us);
}

void mxc_exit(int32_t code) {
    _exit(code);
}

/* Worker entry trampoline: the Moxie side exports mxc_worker_entry(void*),
   and mxc_spawn_worker calls mxc_spawn_thread with that specific function. */
extern void mxc_worker_entry(void* arg);

int32_t mxc_spawn_worker(void* arg, void* handle_out, void* status_out) {
    return mxc_spawn_thread((void*)mxc_worker_entry, arg, handle_out, status_out);
}

/* TLS runtime probe: detects whether Moxie globals are thread_local.
   Spawns a probe thread that writes to a Moxie global via exported function.
   If the parent doesn't see the write, globals are TLS-isolated.
   Returns 1=TLS works, 0=globals shared (force -j 1). */
extern void mxc_tls_probe_write(void);
extern int32_t mxc_tls_probe_read(void);

static void* tls_probe_thread(void* arg) {
    (void)arg;
    mxc_tls_probe_write();
    return NULL;
}

int32_t mxc_detect_tls(void) {
    pthread_t t;
    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_setstacksize(&attr, 1048576); /* 1MB stack for probe */
    int err = pthread_create(&t, &attr, tls_probe_thread, NULL);
    pthread_attr_destroy(&attr);
    if (err != 0) return 0; /* can't create thread, assume no TLS */
    pthread_join(t, NULL);
    int32_t val = mxc_tls_probe_read();
    /* val==0 means child wrote to its own TLS copy (parent unaffected) */
    /* val==1 means globals are shared (no TLS) */
    return (val == 0) ? 1 : 0;
}

/* Online CPU count, for adaptive parallel build defaults. */
int32_t mxc_nproc(void) {
    long n = sysconf(_SC_NPROCESSORS_ONLN);
    if (n < 1) n = 1;
    return (int32_t)n;
}

/* Available physical memory in bytes (free RAM + reclaimable buffers). */
int64_t mxc_free_mem(void) {
    struct sysinfo si;
    if (sysinfo(&si) != 0) return 0;
    return (int64_t)(si.freeram + si.bufferram) * (int64_t)si.mem_unit;
}
