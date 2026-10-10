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

case "$PHASE" in
--quick)
	bash "$SCRIPT_DIR/regressions/run.sh"
	bash "$SCRIPT_DIR/restrictions/run.sh"
	bash "$SCRIPT_DIR/phase1_packages.sh"
	;;
--full)
	bash "$SCRIPT_DIR/regressions/run.sh"
	bash "$SCRIPT_DIR/restrictions/run.sh"
	bash "$SCRIPT_DIR/phase1_packages.sh"
	bash "$SCRIPT_DIR/phase1b_stdlib_sweep.sh"
	bash "$SCRIPT_DIR/phase1c_wasm_sweep.sh"
	bash "$SCRIPT_DIR/phase2_selfhost.sh"
	bash "$SCRIPT_DIR/phase3_ir_validate.sh"
	bash "$SCRIPT_DIR/phase4_binary_smoke.sh"
	bash "$SCRIPT_DIR/phase5_wasm.sh"
	bash "$SCRIPT_DIR/phase6_instrumentation.sh"
	;;
--phase)
	if [ -z "${2:-}" ]; then
		echo "usage: run.sh --phase N"
		exit 1
	fi
	case "$2" in
		0) bash "$SCRIPT_DIR/regressions/run.sh"; bash "$SCRIPT_DIR/restrictions/run.sh" ;;
		1) bash "$SCRIPT_DIR/phase1_packages.sh" ;;
		1b) bash "$SCRIPT_DIR/phase1b_stdlib_sweep.sh" ;;
		1c) bash "$SCRIPT_DIR/phase1c_wasm_sweep.sh" ;;
		2) bash "$SCRIPT_DIR/phase2_selfhost.sh" ;;
		3) bash "$SCRIPT_DIR/phase3_ir_validate.sh" ;;
		4) bash "$SCRIPT_DIR/phase4_binary_smoke.sh" ;;
		5) bash "$SCRIPT_DIR/phase5_wasm.sh" ;;
		6) bash "$SCRIPT_DIR/phase6_instrumentation.sh" ;;
		*) echo "phase $2 not found (0-6)" ; exit 1 ;;
	esac
	;;
*)
	echo "usage: run.sh [--quick|--full|--phase N]"
	exit 1
	;;
esac

print_summary
failed="$?"
if [ "$failed" -gt 0 ]; then
	exit 1
fi
