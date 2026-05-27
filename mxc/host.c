#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/wait.h>

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
