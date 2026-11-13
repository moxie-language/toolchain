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
set -u
MEM=4G
HIGH=3500M
systemctl --user stop mxbuild.scope 2>/dev/null
if [ $# -eq 0 ]; then
	exec systemd-run --user --scope --quiet --collect --unit=mxbuild \
		-p MemoryMax=$MEM -p MemoryHigh=$HIGH -p MemorySwapMax=0 --shell
fi
exec systemd-run --user --scope --quiet --collect --unit=mxbuild \
	-p MemoryMax=$MEM -p MemoryHigh=$HIGH -p MemorySwapMax=0 -- "$@"
