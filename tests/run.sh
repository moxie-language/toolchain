#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MOXIEROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Two different lifetimes, two different places:
#   STATUSFILE lives in the TREE's ./tmp (git-ignored) so a half-finished gate's
#   status survives a /tmp wipe; /tmp is wiped between sessions here.
#   TMPROOT holds the throwaway fixture copies and MUST stay OUTSIDE the tree:
#   a fixture inside the tree is not a user package, so the language
#   restrictions do not fire (measured: the same fixture is rejected from /tmp
#   and from $HOME, and ACCEPTED from $MOXIEROOT/tmp). $HOME rather than /tmp so
#   a phase's logs can still be read after a wipe.
export STATUSFILE="${STATUSFILE:-$MOXIEROOT/tmp/moxie-test-status-$$}"
export TMPROOT="${TMPROOT:-${HOME:-/tmp}/moxie-tests-$$}"
export MOXIEROOT
> "$STATUSFILE"
mkdir -p "$TMPROOT"

source "$SCRIPT_DIR/util.sh"

PHASE="${1:---quick}"
MOXIE="${MOXIE:-$SCRIPT_DIR/../moxie}"
export MOXIE

if [ ! -x "$MOXIE" ]; then
	echo "FATAL: $MOXIE not found or not executable"
	exit 1
fi

echo "=== Moxie Test Suite ==="
echo "binary: $MOXIE"
echo "version: $($MOXIE version 2>&1 | head -1)"
echo "mode: $PHASE"

# Phases now run through the unit driver (tests/unitrun.sh), which runs each
# fixture as an independent unit with its own temp dir, timeout and result file.
# That is what makes a red fixture cheap: it is re-run on its own instead of
# re-running the phase it happened to be in.
# 7 is the sample UI app. It is in the QUICK set on purpose: the stdlib web
# framework's only real consumer is that app, so "the framework still works for a
# real UI" is a basic gate, not a long-tail one. It costs ~3.5 minutes because it
# builds the nine bundles; a warm package cache brings the app bundle build to
# seconds and the rest is the browser suite.
UNIT_PHASES_QUICK="0a,0b,1,7"
UNIT_PHASES_FULL="all"

case "$PHASE" in
--quick)
	exec bash "$SCRIPT_DIR/unitrun.sh" --phases "$UNIT_PHASES_QUICK"
	;;
--full)
	exec bash "$SCRIPT_DIR/unitrun.sh" --phases "$UNIT_PHASES_FULL"
	;;
--unit)
	# tests/run.sh --unit ... passes straight through to the driver.
	shift
	exec bash "$SCRIPT_DIR/unitrun.sh" "$@"
	;;
--phase)
	if [ -z "${2:-}" ]; then
		echo "usage: run.sh --phase N"
		exit 1
	fi
	# Historical phase numbers: 0 was regressions+restrictions, 1b/1c were
	# their own scripts. They are units now (0a, 0b, 1, 1b, 1c).
	case "$2" in
		0) PHASES="0a,0b" ;;
		1) PHASES="1" ;;
		1b) PHASES="1b" ;;
		1c) PHASES="1c" ;;
		2) PHASES="2" ;;
		3) PHASES="3" ;;
		4) PHASES="4" ;;
		5) PHASES="5" ;;
		6) PHASES="6" ;;
		7) PHASES="7" ;;
		*) echo "phase $2 not found (0-7)" ; exit 1 ;;
	esac
	exec bash "$SCRIPT_DIR/unitrun.sh" --phases "$PHASES"
	;;
*)
	echo "usage: run.sh [--quick|--full|--phase N|--unit ...]"
	exit 1
	;;
esac
