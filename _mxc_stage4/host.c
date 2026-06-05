#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/wait.h>
#include <sys/syscall.h>
#include <sys/resource.h>
#include <fcntl.h>

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
