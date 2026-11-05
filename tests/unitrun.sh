#!/bin/bash
# Unit driver for the test suite.
#
#   tests/unitrun.sh                          every unit of every phase
#   tests/unitrun.sh --phases 0a,0b,1,1b      selected phases
#   tests/unitrun.sh --failed                 re-run only what failed
#   tests/unitrun.sh --phases 0a --only asn1  units matching a pattern
#   tests/unitrun.sh --units a,b,c            exactly these units, whatever
#                                             phase they belong to
#   tests/unitrun.sh --phases 0a -j 6         six units at a time
#   tests/unitrun.sh --cold                   drop the package cache first
#                                             (always serial: a cold run has no
#                                             cached artifacts to share)
#   tests/unitrun.sh --fail-fast              stop launching after the first
#                                             failure (build.sh uses this)
#   tests/unitrun.sh --wait                   wait for a running instance
#                                             instead of refusing to start
#
# Why this exists: the suite used to be one process per phase, so a phase that
# hung or crashed lost every fixture it had already run, and re-running it meant
# re-running all of them. Units are independent: each has its own temp dir, its
# own log, its own timeout and its own exit code, and results are kept under
# tmp/unitrun/, so a red unit is re-run on its own and a completed run is not
# repeated.
#
#   tmp/unitrun/<phase>/<unit>/{rc,start,end,out,err}   per-unit state
#   tmp/unitrun/summary.tsv                             one row per unit
#
# Exit: 0 when every unit passed or skipped, 1 when any unit failed. A unit
# that already passed is not run again unless --force is given.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# The compiler's scratch dirs (`./tmp/mxc-build-*`) and the fixtures' relative
# paths both resolve against the cwd, so pin it: a gate started from a
# subdirectory must build exactly what a gate started from the root builds.
cd "$ROOT" || exit 1
export MOXIEROOT="$ROOT"
MOXIE="${MOXIE:-$ROOT/moxie}"
export MOXIE

RUNDIR="${UNITRUN_DIR:-$ROOT/tmp/unitrun}"
# Fixture trees MUST live outside the checkout. The language restrictions fire
# only for a package outside the toolchain's own module, so a fixture built
# from inside the tree is not user code and the restriction phase silently
# measures nothing: `reject_closure_capture` compiled clean from
# $ROOT/tmp/... and was rejected with the expected error from /tmp/... . The
# driver's own results stay in the tree (they are what a later reader wants);
# only the throwaway build dirs go outside it.
WORKROOT="${UNITRUN_WORKROOT:-${XDG_CACHE_HOME:-$HOME/.cache}/moxie-unitrun}"
JOBS="${UNITRUN_JOBS:-4}"
# The default is per CORE, not a flat wall-clock number: a unit's wall time is
# what it costs divided by how many other units are competing for the CPU with
# it, so the same fixture needs several times as long at -j 4 as at -j 1, and a
# flat 900s turned a slow-but-fine unit into a red TIMEOUT. Measured:
# 0a/http_header_api compiles a ~250-package closure and takes 1129s at -j 1
# against a warm cache; the same unit was killed at 900s when it shared the box.
TIMEOUT="${UNITRUN_TIMEOUT:-$((1800 * $(nproc 2>/dev/null || echo 1)))}"
# A unit whose cost is a known constant rather than a function of parallelism.
# http_header_api is the one: 1129s measured alone, so 1800s is 60% headroom
# over the measurement, not a guess. Applied only while the caller has not set
# a timeout of their own - an explicit --timeout is an instruction, not a
# default to improve on.
declare -A UNIT_TIMEOUT=(
	["0a/http_header_api"]=1800
)
TIMEOUT_EXPLICIT=0
PHASES=""
ONLY=""
UNITS_LIST=""
FAILED_ONLY=0
FORCE=0
VERBOSE=0
COLD=0
FAIL_FAST=0
WAIT_LOCK=0

# Phases that can subdivide themselves into units. A phase not listed here is
# monolithic: its whole script is one unit, so it still gets its own timeout,
# log and result file. Phase 2 is a bootstrap chain, not a list of fixtures;
# phase 5 reports per-target results internally and is one unit on purpose.
declare -A PHASE_SCRIPT=(
	[0a]="regressions/run.sh"
	[0b]="restrictions/run.sh"
	[1]="phase1_packages.sh"
	[1b]="phase1b_stdlib_sweep.sh"
	[1c]="phase1c_wasm_sweep.sh"
	[5]="phase5_wasm.sh"
)
declare -A PHASE_MONO=(
	[0c]="phase0c_coldcache.sh"
	[2]="phase2_selfhost.sh"
	[7]="phase7_ui.sh"
	[3]="phase3_ir_validate.sh"
	[4]="phase4_binary_smoke.sh"
	[6]="phase6_instrumentation.sh"
	[8]="phase8_netsmoke.sh"
)
ALL_PHASES=(0a 0b 0c 1 1b 1c 2 3 4 5 6 7 8)

usage() {
	awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
	exit "${1:-0}"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--phases) PHASES="${2:-}"; shift 2 ;;
	--only)
		if [ -n "$ONLY" ]; then
			echo "--only takes one pattern; combine with a glob (e.g. --only 'reject_*')" >&2
			exit 1
		fi
		ONLY="${2:-}"
		shift 2
		;;
	--jobs | -j) JOBS="${2:-}"; shift 2 ;;
	--timeout) TIMEOUT="${2:-}"; TIMEOUT_EXPLICIT=1; shift 2 ;;
	--units)
		UNITS_LIST="${2:-}"
		shift 2
		;;
	--failed) FAILED_ONLY=1; shift ;;
	--force) FORCE=1; shift ;;
	--fail-fast) FAIL_FAST=1; shift ;;
	--wait) WAIT_LOCK=1; shift ;;
	--cold) COLD=1; JOBS=1; shift ;;
	--verbose | -v) VERBOSE=1; shift ;;
	-h | --help) usage 0 ;;
	*) echo "unknown argument: $1" >&2; usage 1 ;;
	esac
done

case "${PHASES:-all}" in
all | "") PHASE_LIST=("${ALL_PHASES[@]}") ;;
*) IFS=',' read -r -a PHASE_LIST <<<"$PHASES" ;;
esac
for p in "${PHASE_LIST[@]}"; do
	if [ -z "${PHASE_SCRIPT[$p]:-}" ] && [ -z "${PHASE_MONO[$p]:-}" ]; then
		echo "unknown phase: $p (have: ${ALL_PHASES[*]})" >&2
		exit 1
	fi
done

mkdir -p "$RUNDIR"

# One run at a time. Two runs share tmp/unitrun: the second one deletes the
# first's per-unit state and work dirs and launches the same units again, so
# each becomes the other's stray writer - measured as three bogus failures
# ("cannot create .../status: No such file or directory") in a run whose units
# had all passed. The lock is the whole fix: refuse, name the holder, and let
# the operator choose between --wait and a different --phases.
exec 9>"$RUNDIR/lock"
if ! flock -n 9; then
	holder="$(cat "$RUNDIR/lock.owner" 2>/dev/null || echo "another run")"
	if [ "$WAIT_LOCK" = 1 ]; then
		echo "unitrun: waiting for $holder to finish"
		flock 9
	else
		echo "unitrun: $holder is already running (results would collide);" >&2
		echo "         wait for it, or re-run with --wait" >&2
		exit 1
	fi
fi
printf '%s (pid %s, started %s)\n' "$(basename "$0")" "$$" "$(date '+%H:%M:%S')" >"$RUNDIR/lock.owner"
trap 'rm -f "$RUNDIR/lock.owner"' EXIT

state_dir() { echo "$RUNDIR/$1" | tr -c 'A-Za-z0-9_./-' '_'; }
unit_rc() { cat "$(state_dir "$1")/rc" 2>/dev/null || echo ""; }

# The verdict, not the exit status, decides pass/skip: the phase script writes it
# the moment it knows the outcome, while the exit status is whatever the script
# returned after that. A unit killed between the two would otherwise be reported
# by a status that no longer matches what it concluded.
# A verdict is current only for the commit that produced it. Stale verdicts are
# invalidated (treated as never run) so the driver re-runs them instead of
# reporting pass or fail for a tree that no longer exists.
verdict_current() {
	local d
	d="$(state_dir "$1")"
	[ -f "$d/verdict" ] || return 1
	local want have st
	want="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)"
	st="$(git -C "$ROOT" status --porcelain 2>/dev/null | sha256sum | cut -d' ' -f1)"
	want="$want $st"
	have="$(cat "$d/commit" 2>/dev/null || true)"
	[ -n "$want" ] && [ "$want" = "$have" ]
}

unit_verdict_of() {
	if [ -f "$(state_dir "$1")/verdict" ] && ! verdict_current "$1"; then
		echo ""
		return
	fi
	local v
	v="$(cat "$(state_dir "$1")/verdict" 2>/dev/null || echo "")"
	case "$v" in
	PASS) echo 0 ;;
	SKIP) echo 2 ;;
	"") echo "" ;;
	*) echo 1 ;;
	esac
}

if [ ! -x "$MOXIE" ]; then
	echo "FATAL: $MOXIE not found or not executable" >&2
	exit 1
fi

# A cold run is a different measurement from a warm one: it is the only way to
# see what a fresh checkout costs, and it must not run beside a warm run that is
# repopulating the very cache it just emptied. Hence -j 1 (set with the flag)
# and one explicit place that does the deletion.
if [ "$COLD" = 1 ]; then
	MOXIEPATH="${MOXIEPATH:-$HOME/moxie}"
	echo "cold run: removing $MOXIEPATH/cache"
	rm -rf "$MOXIEPATH/cache"
fi

# phase_run PHASE ARGS...: run a phase script through the shared environment.
# MOXIE and MOXIEROOT are pinned by the driver; TMPROOT is inside the tree so a
# unit's scratch survives a /tmp wipe long enough to be read after a failure.
phase_run() {
	local p="$1"
	shift
	if [ -n "${PHASE_MONO[$p]:-}" ]; then
		MOXIE="$MOXIE" MOXIEROOT="$ROOT" TMPROOT="$WORKROOT/list-$p-$$" \
			STATUSFILE="$RUNDIR/status-$p-$$" bash "$ROOT/tests/${PHASE_MONO[$p]}" "$@"
	else
		MOXIE="$MOXIE" MOXIEROOT="$ROOT" TMPROOT="$WORKROOT/list-$p-$$" \
			STATUSFILE="$RUNDIR/status-$p-$$" bash "$ROOT/tests/${PHASE_SCRIPT[$p]}" "$@"
	fi
}

# ------------------------------------------------------------------ unit list
UNITS=()
UNIT_INDEX=()
PHASE_OF=()
for p in "${PHASE_LIST[@]}"; do
	idx=0
	if [ -n "${PHASE_MONO[$p]:-}" ]; then
		UNITS+=("$p/whole-phase")
		UNIT_INDEX+=("")
		PHASE_OF+=("$p")
		continue
	fi
	while IFS= read -r u; do
		[ -n "$u" ] || continue
		if [ "$idx" -eq 0 ] && [ "$u" = "whole-phase" ]; then
			# A phase script may declare itself unsplittable.
			UNITS+=("$p/whole-phase")
			UNIT_INDEX+=("")
			PHASE_OF+=("$p")
			break
		fi
		UNITS+=("$p/$u")
		UNIT_INDEX+=("$idx")
		PHASE_OF+=("$p")
		idx=$((idx + 1))
	done < <(phase_run "$p" --list)
	# A phase that can split itself must actually declare its units. Measured:
	# phase 1b listed nothing, so the driver reported "nothing to do" for it and
	# the 45-package stdlib compile sweep counted as a pass without compiling
	# anything. A phase that names no units is an error, not a skip.
	if [ "$idx" -eq 0 ]; then
		echo "unitrun: phase $p declared no units; it would be reported as passing without running" >&2
		exit 1
	fi
done
rm -f "$RUNDIR"/status-*-$$

SELECTED=()
for i in "${!UNITS[@]}"; do
	u="${UNITS[$i]}"
	name="${u#*/}"
	if [ -n "$ONLY" ]; then
		case "$name" in
		$ONLY) ;;
		*) continue ;;
		esac
	fi
	if [ -n "$UNITS_LIST" ]; then
		case ",$UNITS_LIST," in
		*",$name,"*) ;;
		*) continue ;;
		esac
	fi
	rc="$(unit_verdict_of "$u")"
	if [ -f "$(state_dir "$u")/verdict" ] && ! verdict_current "$u"; then
		# Stale: never fall back to the old rc file, or a verdict from another
		# tree would decide this run.
		rc=""
	else
		[ -n "$rc" ] || rc="$(unit_rc "$u")"
	fi
	if [ "$FAILED_ONLY" = 1 ]; then
		# A unit never run is work not done, which is what --failed asks to
		# make up, so it counts as failed here.
		if [ -z "$rc" ] || { [ "$rc" != 0 ] && [ "$rc" != 2 ]; }; then
			SELECTED+=("$i")
		fi
		continue
	fi
	if [ "$FORCE" = 0 ] && { [ "$rc" = 0 ] || [ "$rc" = 2 ]; }; then
		continue
	fi
	SELECTED+=("$i")
done

if [ "${#SELECTED[@]}" -eq 0 ]; then
	echo "unitrun: nothing to do (${#UNITS[@]} units, all done; --force to re-run)"
	exit 0
fi

echo "=== unitrun: ${#SELECTED[@]} of ${#UNITS[@]} units, $JOBS at a time, ${TIMEOUT}s each ==="
echo "binary:  $MOXIE"
echo "results: $RUNDIR"
echo ""

# ------------------------------------------------------------------ reporting
report_unit() {
	local unit="$1" d rc status reason
	d="$(state_dir "$unit")"
	rc="$(unit_verdict_of "$unit")"
	[ -n "$rc" ] || rc="$(cat "$d/rc" 2>/dev/null || echo 1)"
	case "$rc" in
	0) status=PASS ;;
	2) status=SKIP ;;
	124 | 137) status="TIMEOUT" ;;
	*) status=FAIL ;;
	esac
	if [ -f "$d/verdict" ]; then
		status="$(cat "$d/verdict")"
	fi
	if [ "$rc" = 124 ] || [ "$rc" = 137 ]; then
		# The phase never wrote a verdict: the timeout killed it. Reporting
		# whatever status happens to be there would report a stale result.
		status="TIMEOUT"
	fi
	# Nanosecond stamps: a fast unit is not "0s", and phase 0 is judged by
	# which units are slow, which a whole-second clock cannot show.
	local start end ms
	start="$(cat "$d/start" 2>/dev/null || echo 0)"
	end="$(cat "$d/end" 2>/dev/null || echo 0)"
	ms=$(((end - start) / 1000000))
	reason="$(cat "$d/reason" 2>/dev/null | head -1)"
	if [ -z "$reason" ]; then
		reason="$(grep -m1 -iE 'error|panic|failed|no such|not found|timeout' "$d/err" "$d/out" 2>/dev/null |
			head -1 | cut -c1-200)"
	fi
	[ -n "$reason" ] || reason="(no output)"
	if [ "$status" = PASS ]; then
		printf '%-8s %-46s %6sms\n' "$status" "$unit" "$ms"
	else
		printf '%-8s %-46s %6sms  %s\n' "$status" "$unit" "$ms" "$reason"
		sed -n '1,8p' "$d/err" 2>/dev/null | sed 's/^/    | /'
		[ "$VERBOSE" = 1 ] && sed -n '1,20p' "$d/out" 2>/dev/null | sed 's/^/    > /'
	fi
	reason="${reason//$'\t'/ }"
	echo -e "$unit\t$status\t$rc\t$reason" >>"$RUNDIR/summary.tsv.tmp"
}

# -------------------------------------------------------------------- running
launch_unit() {
	local i="$1" unit="${UNITS[$1]}" phase="${PHASE_OF[$1]}" d rc ut
	d="$(state_dir "$unit")"
	ut="$TIMEOUT"
	if [ "$TIMEOUT_EXPLICIT" = 0 ] && [ -n "${UNIT_TIMEOUT[$unit]:-}" ]; then
		ut="${UNIT_TIMEOUT[$unit]}"
	fi
	# The phase script needs TMPROOT before it can name its unit dir, so the
	# path is derived here once from the unit id and exported.
	UNIT_SAFE="$(echo "$unit" | tr -c 'A-Za-z0-9_.-' '_')"
	# The flock is on fd 9 of the driver. This function runs in a subshell, so
	# it inherits the OPEN file description: without closing it here every unit
	# holds the lock open and a second driver never starts - the run looks like
	# it is working while its state directories are being clobbered.
	exec 9>&-
	rm -rf "$d" "$WORKROOT/$UNIT_SAFE"
	mkdir -p "$d"
	date +%s%N >"$d/start"
	if [ -z "${UNIT_INDEX[$i]}" ]; then
		MOXIE="$MOXIE" MOXIEROOT="$ROOT" TMPROOT="$WORKROOT/$UNIT_SAFE" \
			STATUSFILE="$d/status" \
			timeout -k 30 "$ut" bash "$ROOT/tests/${PHASE_MONO[$phase]}" >"$d/out" 2>"$d/err"
	else
		MOXIE="$MOXIE" MOXIEROOT="$ROOT" TMPROOT="$WORKROOT/$UNIT_SAFE" \
			UNIT_NAME="${unit#*/}" STATUSFILE="$d/status" \
			timeout -k 30 "$ut" bash "$ROOT/tests/${PHASE_SCRIPT[$phase]}" --unit "${UNIT_INDEX[$i]}" \
			>"$d/out" 2>"$d/err"
	fi
	rc=$?
	echo "$rc" >"$d/rc"
	date +%s%N >"$d/end"
	# A unit that failed keeps its work dir: that is where the artifact and the
	# full build log are. A unit that passed keeps only its own small logs, so a
	# full run does not leave a gigabyte of wasm behind.
	if [ "$rc" = 0 ] || [ "$rc" = 2 ]; then
		rm -rf "$WORKROOT/$UNIT_SAFE"
	fi
	touch "$d/done"
}

rm -f "$RUNDIR/summary.tsv.tmp"
declare -A SLOT_PID=()
declare -A SLOT_SEL=()
next=0
launched=0

while [ "$next" -lt "${#SELECTED[@]}" ]; do
	if [ "$FAIL_FAST" = 1 ] && [ "$launched" -gt 0 ]; then
		# Fail fast looks at what is still running or has just finished, not
		# at the index order: with -j above 1 the failure is rarely the unit
		# that happens to sit earliest in the list.
		for i in $(seq 0 $((JOBS - 1))); do
			[ -n "${SLOT_PID[$i]:-}" ] || continue
			kill -0 "${SLOT_PID[$i]}" 2>/dev/null && continue
			[ "$(unit_rc "${UNITS[${SLOT_SEL[$i]}]}")" = 1 ] || continue
			echo ""
			echo "fail-fast: ${UNITS[${SLOT_SEL[$i]}]} failed; not launching the remaining $(( ${#SELECTED[@]} - next )) unit(s)"
			break 2
		done
		for sel in "${SELECTED[@]:0:$next}"; do
			[ "$(unit_rc "${UNITS[$sel]}")" = 1 ] || continue
			echo ""
			echo "fail-fast: ${UNITS[$sel]} failed; not launching the remaining $(( ${#SELECTED[@]} - next )) unit(s)"
			break 2
		done
	fi
	for i in $(seq 0 $((JOBS - 1))); do
		[ -n "${SLOT_PID[$i]:-}" ] && continue
		[ "$next" -lt "${#SELECTED[@]}" ] || break
		sel="${SELECTED[$next]}"
		next=$((next + 1))
		launched=$((launched + 1))
		SLOT_SEL[$i]="$sel"
		launch_unit "$sel" &
		SLOT_PID[$i]=$!
	done
	# Block until any unit finishes, then free its slot. Cheaper and far less
	# noisy than polling: a poll loop over 100 units would spend the whole run
	# waking up every 200ms.
	wait -n 2>/dev/null
	for i in $(seq 0 $((JOBS - 1))); do
		[ -n "${SLOT_PID[$i]:-}" ] || continue
		if ! kill -0 "${SLOT_PID[$i]}" 2>/dev/null; then
			wait "${SLOT_PID[$i]}" 2>/dev/null
			unset "SLOT_PID[$i]"
		fi
	done
done
for i in $(seq 0 $((JOBS - 1))); do
	[ -n "${SLOT_PID[$i]:-}" ] || continue
	wait "${SLOT_PID[$i]}" 2>/dev/null
done

for sel in "${SELECTED[@]}"; do
	report_unit "${UNITS[$sel]}"
done

mv "$RUNDIR/summary.tsv.tmp" "$RUNDIR/summary.tsv" 2>/dev/null

# -------------------------------------------------------------------- summary
total=0
passed=0
failed=0
skipped=0
# Only the units this run selected count towards the exit code. A unit filtered
# out by --units or --only keeps whatever verdict it had from an earlier run, and
# counting those made a targeted run report - and fail on - work it never ran
# (a stale FAIL from a killed sweep turned a 13-fixture quickcheck red).
for sel in "${SELECTED[@]}"; do
	unit="${UNITS[$sel]}"
	rc="$(unit_verdict_of "$unit")"
	[ -n "$rc" ] || rc="$(unit_rc "$unit")"
	[ -n "$rc" ] || continue
	total=$((total + 1))
	case "$rc" in
	0) passed=$((passed + 1)) ;;
	2) skipped=$((skipped + 1)) ;;
	*) failed=$((failed + 1)) ;;
	esac
done

# ── tree cleanliness ────────────────────────────────────────────────────────
# The suite builds into $TMPROOT (outside the tree) and into the tree's own tmp/
# and cache/, which are gitignored. Anything a gate leaves in a TRACKED path is
# drift: a phase that writes into a source tree and gets "restored afterwards"
# is hiding it rather than preventing it. Measure it, do not promise it.
#
# Only the suite's own checkout is judged: the UI app is a SEPARATE repo beside
# it, and its arrangement (checked-in build products under web/static) is its
# own business, not this gate's.
DIRTY="$(git -C "$ROOT" status --porcelain 2>/dev/null | head -20)"
if [ -n "$DIRTY" ]; then
	echo ""
	echo "FAIL      tree-cleanliness"
	printf '  the suite left tracked files modified in %s:\n' "$ROOT"
	printf '%s\n' "$DIRTY" | sed 's/^/    /'
	failed=$((failed + 1))
	total=$((total + 1))
else
	echo "PASS      tree-cleanliness"
	passed=$((passed + 1))
	total=$((total + 1))
fi

echo ""
echo "=== Summary ==="
printf 'total: %d  passed: %d  failed: %d  skipped: %d\n' "$total" "$passed" "$failed" "$skipped"
if [ "$failed" -gt 0 ]; then
	echo ""
	echo "failed units (re-run with: tests/unitrun.sh --phases ${PHASE_LIST[*]} --failed):"
	for unit in "${UNITS[@]}"; do
		rc="$(unit_verdict_of "$unit")"
		[ -n "$rc" ] || rc="$(unit_rc "$unit")"
		[ "$rc" = 1 ] || [ "$rc" = 124 ] || [ "$rc" = 137 ] || continue
		printf '  %s  %s\n' "$unit" "$(sed -n '1p' "$(state_dir "$unit")/err" 2>/dev/null | cut -c1-120)"
	done
fi
[ "$failed" -gt 0 ] && exit 1
exit 0
