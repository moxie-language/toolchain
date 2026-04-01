//go:build none

// Ignore the //go:build above. This file is manually included on Linux and
// MacOS to provide os/signal support.

#include <stdint.h>
#include <signal.h>
#include <time.h>
#include <unistd.h>

// Signal handler in the runtime.
void moxie_signal_handler(int sig);

// Enable a signal from the runtime.
void moxie_signal_enable(uint32_t sig) {
    struct sigaction act = { 0 };
    act.sa_handler = &moxie_signal_handler;
    act.sa_flags = SA_RESTART;
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
