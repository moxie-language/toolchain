#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 2: Self-hosting chain ==="

td="$(tempdir "selfhost")"; rm -rf "$td"; mkdir -p "$td"

# 2a: build stage1 with legacy compiler
name="selfhost:stage1"
t0=$(date +%s%N)
	legacy_moxie="${LEGACY_MOXIE:-$ROOT/legacy/moxie}"
	if (cd "$ROOT" && MOXIEROOT="$ROOT" "$legacy_moxie" build -a -o "$td/stage1" ./_mxc_stage4/) >"$td/stage1.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	log_fail "$name" "legacy compiler failed: $(tail -3 "$td/stage1.log")"
	rm -rf "$td"
	exit 1
fi

# 2b: verify stage1 version
name="selfhost:stage1-version"
t0=$(date +%s%N)
if "$td/stage1" version >"$td/ver1.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	log_fail "$name" "stage1 version failed: $(tail -3 "$td/ver1.log")"
	rm -rf "$td"
	exit 1
fi

# 2b2: rebuild the baked runtime from source with the freshly built compiler.
# sysroot/runtime_go.bc is an input, not a build product: nothing regenerates
# it unless asked, so it silently drifts from src/runtime and then apps fail
# to link against it. Rebuilding here keeps the bake a product of this
# bootstrap, so no runtime blob needs to be distributed.
name="selfhost:runtime-bake"
t0=$(date +%s%N)
if MOXIEROOT="$ROOT" "$td/stage1" build-runtime >"$td/runtime.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	log_fail "$name" "build-runtime failed: $(tail -3 "$td/runtime.log")"
fi

# 2c: self-host with stage1
name="selfhost:stage2"
t0=$(date +%s%N)
if MOXIEROOT="$ROOT" "$td/stage1" build -a -o "$td/stage2" "$ROOT/_mxc_stage4/" >"$td/stage2.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	log_fail "$name" "stage1 self-host failed: $(tail -3 "$td/stage2.log")"
	rm -rf "$td"
	exit 1
fi

# 2d: verify stage2
name="selfhost:stage2-version"
t0=$(date +%s%N)
if "$td/stage2" version >"$td/ver2.log" 2>&1; then
	t1=$(date +%s%N)
	log_pass "$name" "ok"
else
	log_fail "$name" "stage2 version failed: $(tail -3 "$td/ver2.log")"
	rm -rf "$td"
	exit 1
fi

# 2e: verify the fixpoint: stage2 must emit the same binary that emitted it
name="selfhost:fixpoint"
t0=$(date +%s%N)
# Every build must pin MOXIEROOT to this tree. Without it the compiler reads the
# stdlib from whatever MOXIEROOT the caller's environment carries - an installed
# tree from another revision - and then reports that tree's source against the
# sources here, which is a confusing failure that has nothing to do with the
# chain being tested.
if MOXIEROOT="$ROOT" "$td/stage2" build -a -o "$td/stage3" "$ROOT/_mxc_stage4/" >"$td/stage3.log" 2>&1; then
	if cmp -s "$td/stage2" "$td/stage3"; then
		t1=$(date +%s%N)
		log_pass "$name" "stage2 == stage3"
	else
		# Convergence can take one extra generation when the legacy compiler
		# seeds the chain (its pipeline differs, so stage1 and stage2 carry a
		# one-generation edge). What must hold is that a generation reproduces
		# itself: once stageN == stageN+1 the bootstrap has reached its fixed
		# point. Only a persistent mismatch is a failure.
		if MOXIEROOT="$ROOT" "$td/stage3" build -a -o "$td/stage4" "$ROOT/_mxc_stage4/" >"$td/stage4.log" 2>&1; then
			if cmp -s "$td/stage3" "$td/stage4"; then
				t1=$(date +%s%N)
				log_pass "$name" "stage2 != stage3; stage3 == stage4 (converged)"
			else
				log_fail "$name" "stage3 != stage4 (bootstrap does not converge)"
			fi
		else
			log_fail "$name" "stage3 self-host failed: $(tail -3 "$td/stage4.log")"
		fi
	fi
else
	log_fail "$name" "stage2 self-host failed: $(tail -3 "$td/stage3.log")"
fi

rm -rf "$td"
