#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 3: LLVM IR validation ==="

if ! command -v opt >/dev/null 2>&1; then
	echo "  SKIP: 'opt' not found (install llvm)"
	exit 0
fi

td="$(tempdir "irval")"; rm -rf "$td"; mkdir -p "$td"

# pkg/types imports pkg/syntax, pkg/token and pkg/mxutil, so ONE forced build
# emits the IR of all four and every module can be verified.
#
# -a is required: the cache answers every package otherwise, no module is
# emitted, and the phase has nothing to check.
# -work is required: the compiler unlinks each .ll as soon as clang has read
# it, so the emitted IR only survives in a kept scratch directory.
#
# The link is expected to fail - these are libraries, not commands, and the
# tree has no main.main. Compilation is what this phase is about, so the
# verdict comes from whether each module was emitted and whether opt accepts
# it, never from the exit status alone.
log="$td/build.log"
MOXIEROOT="$ROOT" "$MOXIE" build -a -work -o /dev/null "$ROOT/pkg/types" >"$log" 2>&1 || true

workdir=$(sed -n 's/^WORK=//p' "$log" | tail -1)
if [ -z "$workdir" ] || [ ! -d "$workdir" ]; then
	log_fail "ir:build" "$(tail -1 "$log" 2>/dev/null || echo 'no build log')"
	rm -rf "$td"
else
	bad=""
	verified=0
	# The .ll files live in a link-set-named subdirectory of the scratch dir
	# (stableLlPath, so the path clang embeds in the cached bitcode is the same
	# for every build of a link set). Globbing "$workdir"/*.ll directly found
	# ONE file - the phase's own initall.ll - and reported every package
	# "FAIL only 1 module(s) emitted" while still exiting 0, so a red log sat
	# under a green gate. Recurse.
	for ll in $(find "$workdir" -name '*.ll' | sort); do
		[ -f "$ll" ] || continue
		verified=$((verified + 1))
		if ! opt -passes=verify -disable-output "$ll" >"$td/verify.log" 2>&1; then
			bad="$(basename "$ll"): $(tail -1 "$td/verify.log")"
			break
		fi
	done
	# One verdict per emitted module so a red one names its package. The set is
	# derived from the modules actually emitted, not from a list written here:
	# a list would go stale (as it did: it still named pkg/rewrite after that
	# package was deleted) and a missing module would go unnoticed.
	for pkg in $(sed -n 's|^  compile .*/pkg/\([a-z0-9_]*\)$|\1|p' "$log" | sort -u); do
		name="ir:$pkg"
		if [ -n "$bad" ]; then
			log_fail "$name" "$bad"
		elif [ "$verified" -lt 4 ]; then
			log_fail "$name" "only $verified module(s) emitted"
		elif ! grep -q "compile .*/pkg/$pkg\b" "$log"; then
			log_fail "$name" "package never compiled"
		else
			log_pass "$name" "ok"
		fi
	done
	rm -rf "$workdir"
	rm -rf "$td"
fi

# A phase that cannot go red is not a gate: this script's log showed six FAIL
# verdicts while the process exited 0 (the driver records the exit status), so a
# red log sat under a green run. Exit non-zero when anything failed.
if [ -f "$STATUSFILE" ] && [ "$(grep -c '^FAIL' "$STATUSFILE" 2>/dev/null || echo 0)" -gt 0 ]; then
	exit 1
fi
