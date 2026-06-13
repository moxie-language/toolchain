//go:build none

// Ignore the //go:build above. This file is manually included on Linux and
// MacOS to provide os/signal support.

#include <stdint.h>
#include <signal.h>
#include <time.h>
#include <unistd.h>
#include <fcntl.h>

// Signal handler in the runtime.
void moxie_signal_handler(int sig);

// Self-pipe for waking epoll on signal delivery.
static int signal_pipe[2] = {-1, -1};

// moxie_signal_pipe_init creates the self-pipe. Returns read fd, or -1 on error.
int32_t moxie_signal_pipe_init(void) {
    if (signal_pipe[0] >= 0) return signal_pipe[0];
    if (pipe(signal_pipe) < 0) return -1;
    fcntl(signal_pipe[0], F_SETFL, O_NONBLOCK);
    fcntl(signal_pipe[1], F_SETFL, O_NONBLOCK);
    return signal_pipe[0];
}

// moxie_signal_pipe_read drains the pipe. Returns number of bytes drained.
int32_t moxie_signal_pipe_read(void) {
    char buf[16];
    int n = 0, r;
    while ((r = read(signal_pipe[0], buf, sizeof(buf))) > 0) n += r;
    return n;
}

static void signal_pipe_handler(int sig) {
    char c = (char)sig;
    write(signal_pipe[1], &c, 1);
}

// Enable a signal from the runtime.
void moxie_signal_enable(uint32_t sig) {
    struct sigaction act = { 0 };
    act.sa_handler = signal_pipe[1] >= 0 ? &signal_pipe_handler : &moxie_signal_handler;
    act.sa_flags = 0;
    sigaction(sig, &act, NULL);
}

void moxie_signal_ignore(uint32_t sig) {
    struct sigaction act = { 0 };
    act.sa_handler = SIG_IGN;
    sigaction(sig, &act, NULL);
}

void moxie_signal_disable(uint32_t sig) {
    struct sigaction act = { 0 };
    act.sa_handler = SIG_DFL;
    sigaction(sig, &act, NULL);
}
