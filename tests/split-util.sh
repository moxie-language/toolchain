#!/bin/bash
# Unit layer for the test suite.
#
# The suite used to be one process per phase: a phase ran its fixtures in a
# loop, and anything that made it stop - a fixture that hung the compiler, a
# crash that took the runner with it, a ctrl-C because 40 minutes had passed -
# threw away every fixture already run, including the 60 that had passed. The
# phase was the unit of work AND the unit of loss.
#
# A phase now offers its fixtures as units. Each unit is one compiler
# invocation in its own temp dir with its own log and its own exit code, and
# the driver (tests/unitrun.sh) runs them independently, in parallel, with a
# per-unit timeout, keeping each unit's result on disk. A unit that fails or
# hangs costs exactly that unit; the rest still run and still report.
#
# A phase script supporting units handles:
#   --list          print one unit name per line
#   --name IDX      print unit IDX (0-based, the --list order)
#   --unit IDX      run unit IDX; exit 0 pass, 1 fail, 2 skip
# Inside --unit the phase script sees UNIT_NAME already set, so it dispatches
# on the name and never re-derives its own ordering.

# ---------------------------------------------------------------- unit lists
# A phase's units are the entries of its UNITS array, in order. The driver asks
# for the list, then addresses a unit by its index; the phase script turns that
# index back into a name and dispatches on the name.
unit_list() {
	local u
	for u in "${UNITS[@]}"; do echo "$u"; done
}

unit_name() {
	local i="${1:-}"
	[ -n "$i" ] || { echo "usage: $0 --name IDX" >&2; return 1; }
	[ "$i" -ge 0 ] && [ "$i" -lt "${#UNITS[@]}" ] || {
		echo "unit index $i out of range (0..$((${#UNITS[@]} - 1)))" >&2
		return 1
	}
	echo "${UNITS[$i]}"
}

# Dispatch shared by every phase script:
#   unit_args "$@"
# returns 0 when it handled the arguments and the caller must exit with $?.
unit_args() {
	local mode="${1:-}" arg="${2:-}"
	case "$mode" in
	--list) unit_list; exit 0 ;;
	--name) unit_name "$arg"; exit $? ;;
	--unit)
		[ -n "$arg" ] || { echo "usage: $0 --unit IDX" >&2; exit 1; }
		UNIT_INDEX="$arg"
		UNIT_NAME="$(unit_name "$arg")" || exit 1
		export UNIT_INDEX UNIT_NAME
		return 0
		;;
	esac
	if [ -z "$mode" ]; then
		UNIT_NAME="${UNIT_NAME:-}"
		return 0
	fi
	echo "unknown phase argument: $mode" >&2
	exit 1
}

# ------------------------------------------------------------------ one unit
# Everything one unit needs: an isolated work dir plus the status file the
# shared log_* helpers write to. UNIT_NAME is the fixture; two fixture names
# that differ only in characters unusable in a path are the same fixture.
#
# The work dir is NOT removed here. It holds the build log, which is the
# evidence for a unit that failed; the driver removes it only for a unit that
# passed, and reports the path for one that did not.
unit_begin() {
	UNIT_WORK="$(tempdir "u_$(echo "$UNIT_NAME" | tr -c 'A-Za-z0-9_.-' '_')")"
	rm -rf "$UNIT_WORK"
	mkdir -p "$UNIT_WORK"
	# The verdict is written beside the STATUSFILE the driver set, before
	# unit_begin replaces it with the work dir's own. The driver reports what is
	# in ITS state dir; the phase script's work dir is the fixture's, not the
	# driver's.
	UNIT_STATE="$(dirname "$STATUSFILE")"
	export STATUSFILE="$UNIT_WORK/status"
	: >"$STATUSFILE"
}

# ------------------------------------------------------------------- verdict
# The verdict is written to two places on purpose. The shared log_* helpers
# write human lines to stdout and append to STATUSFILE, which is how the driver
# tells pass from fail; the `verdict`/`reason` fields beside STATUSFILE carry
# the one-line summary the driver reports. Parsing the phase script's stdout for
# it would make the summary depend on log formatting, which is not a contract.
unit_verdict() {
	local v="$1" reason="$2" dir="${UNIT_STATE:-$(dirname "$STATUSFILE")}"
	printf '%s\n' "$v" >"$dir/verdict"
	printf '%s\n' "$reason" | tr '\n\t' '  ' >"$dir/reason"
	# Bind the verdict to the tree it describes. A verdict that can outlive its
	# commit is the pass/fail version of a dead diagnostic sink: it made the
	# driver report a FAIL from before the fixture was repaired.
	local c st
	c="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)"
	st="$(git -C "$ROOT" status --porcelain 2>/dev/null | sha256sum | cut -d' ' -f1)"
	printf '%s %s\n' "$c" "$st" >"$dir/commit"
}

# unit_ok NAME REASON / unit_no NAME REASON / unit_skipped NAME REASON: the only
# way a unit reports. One call per unit run.
unit_ok() {
	log_pass "$1" "$2"
	unit_verdict PASS "$2"
}
unit_no() {
	log_fail "$1" "$2"
	unit_verdict FAIL "$2"
}
unit_skipped() {
	log_skip "$1" "$2"
	unit_verdict SKIP "$2"
}

# unit_exit: derive the exit code the driver records from the verdict. 0 pass,
# 1 fail, 2 skip.
unit_exit() {
	local v
	v="$(cat "${UNIT_STATE:-$(dirname "$STATUSFILE")}/verdict" 2>/dev/null || echo FAIL)"
	case "$v" in
	PASS) return 0 ;;
	SKIP) return 2 ;;
	*) return 1 ;;
	esac
}

# first_error LOG [EXTRA_GREP]: the first line of a build log that says what
# went wrong. The compiler prefixes real failures with "; ", "mxc:" or
# "error:"; an LLVM verifier complaint is prefixed with neither and only shows
# up in a tail, which is why the tail fallback exists.
first_error() {
	local log="$1" extra="${2:-}" line=""
	if [ "$extra" != "" ]; then
		line="$(grep -E "$extra" "$log" 2>/dev/null | head -1 || true)"
	fi
	if [ -z "$line" ]; then
		line="$(grep -E '^; |^mxc:|error:' "$log" 2>/dev/null |
			grep -v '^  compile errors$' | head -1 || true)"
	fi
	if [ -z "$line" ]; then
		line="$(grep -E 'panic:|caught signal|Assertion|defined with type' "$log" 2>/dev/null | head -1 || true)"
	fi
	if [ -z "$line" ]; then
		line="$(tail -3 "$log" 2>/dev/null | head -1 || true)"
	fi
	echo "$line"
}
