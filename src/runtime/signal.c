//go:build none
//:build linux
//
// The //go:build none above is the upstream marker for a file that Go's
// toolchain must not compile on its own; these are included manually for
// Linux targets. Stage4 selects files with //:build, so without this it
// compiles them for every target - and they include Linux headers.

// Ignore the //go:build above. This file is manually included on Linux and
// MacOS to provide os/signal support.

#include <stdint.h>
#include <signal.h>
#include <time.h>
#include <unistd.h>
#include <fcntl.h>

// Signal delivery for a program that polls a self-pipe:
// moxie_signal_pipe_init creates the pipe, moxie_signal_enable routes a signal
// into it, and the program drains it with moxie_signal_pipe_read.

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

// moxie_signal_enable routes a signal into the self-pipe. Without the pipe
// there is nowhere to deliver it, so the default action is left in place
// rather than swallowing the signal.
void moxie_signal_enable(uint32_t sig) {
    if (signal_pipe[1] < 0) return;
    struct sigaction act = { 0 };
    act.sa_handler = &signal_pipe_handler;
    act.sa_flags = 0;
    sigaction(sig, &act, NULL);
}

// Interrupt handling for a program that owns resources it must clean up before
// dying. The program overrides moxie_on_interrupt; the default does nothing.
// _exit is used because the handler must not run atexit handlers or flush
// stdio that a nested fault could be holding.
__attribute__((weak)) void moxie_on_interrupt(void) {}

static void interrupt_handler(int sig) {
    moxie_on_interrupt();
    _exit(128 + sig);
}

// moxie_install_interrupt_handler cleans up on the signals that mean "stop":
// SIGINT from the terminal and SIGTERM from a supervisor.
void moxie_install_interrupt_handler(void) {
    struct sigaction act = { 0 };
    act.sa_handler = &interrupt_handler;
    act.sa_flags = 0;
    sigaction(SIGINT, &act, NULL);
    sigaction(SIGTERM, &act, NULL);
}
