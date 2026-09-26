#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MOXIEROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export STATUSFILE="${STATUSFILE:-/tmp/moxie-test-status-$$}"
export TMPROOT="${TMPROOT:-/tmp/moxie-tests-$$}"
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
