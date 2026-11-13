#!/bin/sh
# moxbox - run a command inside the 4 GB memory box.
#
# Every compiler build, every test, every soak, and every binary the compiler
# produced runs through this. The box is a transient systemd scope with
# MemoryMax=4G and MemorySwapMax=0: a run that needs more is OOM-killed inside
# the scope and the machine keeps its memory (exit 137).
#
# 4G, not 6G: a 64-bit build that cannot fit in 32 bits of address space is
# broken, and whatever pushed it past 4G has to be found before anything else.
# The cap is a diagnostic, not a budget.
#
# A scope, not a container: the build is one process tree, cgroups are
# hierarchical, and a scope already caps every descendant. Nothing is
# installed and no filesystem is remounted - the programs are the host's.
#
#   scripts/moxbox.sh <command...>   run a command in the box
#   scripts/moxbox.sh                interactive shell already inside the box
#
# Exit status is the command's own, so a caller can tell a build failure from
# an OOM kill.
set -u
MEM=4G
if [ $# -eq 0 ]; then
	exec systemd-run --user --scope --quiet -p MemoryMax=$MEM -p MemorySwapMax=0 --shell
fi
exec systemd-run --user --scope --quiet -p MemoryMax=$MEM -p MemorySwapMax=0 -- "$@"
