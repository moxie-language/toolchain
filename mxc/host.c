#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/wait.h>
#include <sys/syscall.h>
#include <fcntl.h>

int32_t mxc_run(const char** argv, int32_t argc) {
    int pid = fork();
    if (pid < 0) return -1;
    if (pid == 0) {
        char* args[64];
        int i;
        for (i = 0; i < argc && i < 63; i++) args[i] = (char*)argv[i];
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
