#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/syscall.h>
#include <sys/stat.h>
#include <fcntl.h>

int32_t mxc_readfile(const char* path, int32_t pathLen, char* buf, int32_t bufCap) {
    char pbuf[4096];
    if (pathLen > 4095) pathLen = 4095;
    memcpy(pbuf, path, pathLen);
    pbuf[pathLen] = 0;
    int fd = open(pbuf, O_RDONLY);
    if (fd < 0) return -1;
    int32_t total = 0;
    while (total < bufCap) {
        long n = read(fd, buf + total, bufCap - total);
        if (n <= 0) break;
        total += (int32_t)n;
    }
    close(fd);
    return total;
}

int32_t mxc_filesize(const char* path, int32_t pathLen) {
    char pbuf[4096];
    if (pathLen > 4095) return -1;
    memcpy(pbuf, path, pathLen);
    pbuf[pathLen] = 0;
    struct stat st;
    if (syscall(SYS_newfstatat, -100, pbuf, &st, 0) != 0) return -1;
    return (int32_t)st.st_size;
}
