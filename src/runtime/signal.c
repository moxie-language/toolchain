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

// Termination by signal. The handler does ONE thing: raise a flag. It may
// interrupt an allocation, a channel operation or a held lock, so it must not
// allocate, close a channel, or call anything that does. The domain polls the
// flag at every point where it waits, closes its termination channel there
// (normal context, its own state), and its select on `shutdown` fires like a
// receive on any closed channel.
static volatile sig_atomic_t shutdown_pending = 0;

static void shutdown_flag_handler(int sig) {
    (void)sig;
    shutdown_pending = 1;
}

// moxie_shutdown_install routes the signals that mean "stop" to the flag:
// SIGINT from the terminal and SIGTERM from a supervisor. SA_RESTART keeps an
// interrupted syscall resuming normally, so the flag stays the only change.
void moxie_shutdown_install(void) {
    struct sigaction act = { 0 };
    act.sa_handler = &shutdown_flag_handler;
    act.sa_flags = SA_RESTART;
    sigaction(SIGINT, &act, NULL);
    sigaction(SIGTERM, &act, NULL);
}

// moxie_shutdown_pending reports whether a stop signal has arrived.
int32_t moxie_shutdown_pending(void) {
    return (int32_t)shutdown_pending;
}

// A program that owns resources it must release before dying can ask for the
// immediate path instead: it overrides moxie_on_interrupt, and the handler runs
// that and leaves. _exit is used because the handler must not run atexit
// handlers or flush stdio that a nested fault could be holding.
//
// Installing this REPLACES the graceful flag handler for the signals it
// installs. That is the program's decision, and it is one-way: the runtime
// installs the flag handler at startup, so a program opts out only by asking.
__attribute__((weak)) void moxie_on_interrupt(void) {}

static void interrupt_handler(int sig) {
    moxie_on_interrupt();
    _exit(128 + sig);
}

void moxie_install_interrupt_handler(void) {
    struct sigaction act = { 0 };
    act.sa_handler = &interrupt_handler;
    act.sa_flags = 0;
    sigaction(SIGINT, &act, NULL);
    sigaction(SIGTERM, &act, NULL);
}
