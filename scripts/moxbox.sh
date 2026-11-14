#!/bin/sh
# moxbox - run a command inside the 4 GB memory box.
#
# Every compiler build, every test, every soak, and every binary the compiler
# produced runs through this. The box is a transient systemd scope with
# MemoryMax=4G, MemoryHigh=3500M and MemorySwapMax=0.
#
# Swap is disabled on purpose. With swap the cgroup reclaims to stay under Max:
# the run stays inside 4 GB by paging, the fan runs all day, and nothing fails.
# That bounds the symptom instead of catching the bug. With MemorySwapMax=0 the
# limit is an immediate OOM kill - a fast, loud failure you can read.
#
# MemoryHigh sits just below Max so the run is throttled before it is killed.
# That window is where you attach gdb or read the arena accounting while the
# process is still alive; Max is the backstop for when nobody is watching.
#
# 4G, not 6G: a 64-bit build that cannot fit in 32 bits of address space is
# broken, and whatever pushed it past 4G is the defect to find.
#
#   scripts/moxbox.sh <command...>   run a command in the box
#   scripts/moxbox.sh                interactive shell already inside the box
#
# Exit codes, which decide how you read the result:
#   137  SIGKILL - the cgroup OOM killer, or `timeout -s KILL`
#   139  SIGSEGV - a fault in the program
#   134  SIGABRT - a panic or an assertion
#
# Before concluding the compiler has a leak, check WHICH process died. LLVM's
# `opt` and `ld.lld` take 1-2 GB on a large module, and the kernel names
# whichever process was biggest. The OOM message is in the kernel journal, not
# the user one: journalctl -k | grep -i oom, or sudo dmesg | tail.
#
# The unit is named so it can be polled while it runs:
#   systemctl --user show mxbuild.scope -p MemoryCurrent,MemoryPeak
# A transient scope keeps its name for a moment after it exits, so a new run
# destroys the old unit first rather than failing to start.
#
# CPUs: the box is pinned to TWO hardware threads with a cpuset. That is the
# resource boundary, and it is deliberately not a job-count knob - `-j`, the
# worker pool and TasksMax all describe how the work is scheduled, and the whole
# point of the box is that it must fit in what it is given. The scheduler is
# then free to put those two threads wherever it likes on the two CPUs and
# nothing else may use them.
#
# MXBOX_CPUS names the set (systemd cpuset syntax). Default 0-1.
#
# The pin is applied TWICE on purpose: AllowedCPUs on the scope sets the cgroup
# cpuset, and `taskset -c` sets the affinity mask of the process systemd spawns,
# which every descendant inherits. Measured: AllowedCPUs alone left
# Cpus_allowed_list at 0-7 for the command running inside the scope, and
# `taskset -c 0-1` inside it reports 0-1. Both are kept so the cgroup and the
# process tree agree.
#
# This also sizes the compiler's pool without a flag: it asks nproc, and a
# process pinned to two CPUs answers 2, so its heuristic (nproc-1) selects one
# worker. The work fits what it is given - which is the point of the box.
#
# TasksMax is not set: at 2 even `sh -c 'echo $(ls /proc/self/task)'` fails with
# "fork: Resource temporarily unavailable" because the limit counts every task,
# and at 8 LLVM's linker dies with std::system_error. It is not the lever.
set -u
MEM=4G
HIGH=3500M
CPUS=${MXBOX_CPUS:-0-1}
systemctl --user stop mxbuild.scope 2>/dev/null
if [ $# -eq 0 ]; then
	exec systemd-run --user --scope --quiet --collect --unit=mxbuild \
		-p MemoryMax=$MEM -p MemoryHigh=$HIGH -p MemorySwapMax=0 \
		-p AllowedCPUs=$CPUS -- taskset -c $CPUS "${SHELL:-/bin/sh}"
fi
exec systemd-run --user --scope --quiet --collect --unit=mxbuild \
	-p MemoryMax=$MEM -p MemoryHigh=$HIGH -p MemorySwapMax=0 \
		-p AllowedCPUs=$CPUS -- taskset -c $CPUS "$@"
