#!/bin/bash
# Phase 0c: the compiler must be correct on a COLD package cache.
#
# Every other phase shares the tree's package cache, so every build it makes is
# warm, and round 369 found exactly what that hid. pkgHash - the OBJECT key -
# folds in the dispatch fingerprint, which exists only after
# measureDispFingerprints has run. The export blob (mxh) was named from that
# same key, so the dependency pre-pass wrote it under the pre-measurement name
# (link-set fallback) and the compile phase read the post-measurement one. On a
# cache generation that had never been written - the first build after any
# compiler upgrade, since the generation directory is per compiler build - the
# read missed, a worker compiled with an empty sub-registry, and the interface
# dispatch table it emitted was missing every impl from the root package (fmt
# carried 78 of the 86 impls). The symptom is that fmt prints '?' for every
# Stringer, which the fixture below makes a hard failure.
#
# MOXIEPATH moves the package cache, so a cold cache is one directory away and
# this phase does not depend on what the rest of the suite has warmed.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 0c: cold package cache ==="

FIXTURE="$SCRIPT_DIR/regressions/should_compile/builtin_string_render.mx"
if [ ! -f "$FIXTURE" ]; then
	log_fail "coldcache:fixture" "missing fixture: $FIXTURE"
	exit 1
fi
td="$(tempdir "coldcache")"; rm -rf "$td"; mkdir -p "$td/pkg"
cp "$FIXTURE" "$td/pkg/main.mx"

# -j 4 on purpose: the loss happened in the worker processes' registry, and a
# serial build has a whole-link pre-pass in one process and does not need the
# mxh to be found at all.
case_one() { # case_one <label> <MOXIEPATH> <extra build args...>
	local label="$1" mpath="$2"; shift 2
	local log="$td/$label.log" out="$td/$label.out" brc=0 rrc=0
	mkdir -p "$mpath"
	if ! MOXIEPATH="$mpath" MOXIEROOT="$ROOT" "$MOXIE" build -j 4 "$@" \
		-o "$td/$label.bin" "$td/pkg" >"$log" 2>&1; then
		brc=1
	fi
	if [ "$brc" -ne 0 ]; then
		log_fail "coldcache:$label" "build failed: $(first_error "$log")"
		return 1
	fi
	"$td/$label.bin" >"$out" 2>&1 || rrc=$?
	if [ "$rrc" -ne 0 ] || grep -q '^FAIL ' "$out"; then
		log_fail "coldcache:$label" "$(grep '^FAIL ' "$out" | head -1)"
		return 1
	fi
	log_pass "coldcache:$label" "printed every Stringer correctly"
}

fail=0
# 1. Nothing in the cache: the pre-pass writes the export blob, the workers read
#    it back. This is the exact configuration that was broken.
case_one cold "$td/cache-cold" || fail=1
# 2. The same cache again, forced rebuild so the workers still do the work.
case_one warm "$td/cache-cold" -a || fail=1
# 3. A second, independent cold cache. A name that is only stable because a file
#    already exists is the bug, so the pass must not depend on run order.
case_one cold2 "$td/cache-cold2" || fail=1

exit "$fail"
