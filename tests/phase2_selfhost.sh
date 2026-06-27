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
	legacy_moxie="${LEGACY_MOXIE:-$HOME/.local/bin/moxie}"
	if (cd "$ROOT" && MOXIEROOT="$ROOT" "$legacy_moxie" build -internal-nodwarf -o "$td/stage1" ./_mxc_stage4/) >"$td/stage1.log" 2>&1; then
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

# 2c: self-host with stage1
name="selfhost:stage2"
t0=$(date +%s%N)
if MOXIEROOT="$ROOT" "$td/stage1" build -o "$td/stage2" "$ROOT/_mxc_stage4/" >"$td/stage2.log" 2>&1; then
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

rm -rf "$td"
