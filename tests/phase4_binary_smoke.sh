#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export MOXIEROOT="$ROOT"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 4: Binary smoke tests ==="

td="$(tempdir "smoke")"; rm -rf "$td"; mkdir -p "$td"

# 4a: compile and run hello.mx
name="smoke:hello"
t0=$(date +%s%N)
if compile_and_run "$SCRIPT_DIR/data/hello.mx" "$td" "hello"; then
	t1=$(date +%s%N)
	out=$(cat "$td/run.log" 2>/dev/null)
	if echo "$out" | grep -q "hello"; then
		log_pass "$name" "ok"
	else
		log_fail "$name" "wrong output: $out"
	fi
else
	log_fail "$name" "$(tail -3 "$td/build.log" 2>/dev/null)"
fi

# 4b: a panic prints to the process's stderr and aborts (status 134). The
# breaker does not carry a reason: the process ending is the event.
name="smoke:panic-stderr"
pdir="$td/panic"
mkdir -p "$pdir/pkg"
cp "$SCRIPT_DIR/data/panicstderr/main.mx" "$pdir/pkg/main.mx"
if MOXIEROOT="$MOXIEROOT" "$MOXIE" build -o "$pdir/panic" "$pdir/pkg" >"$pdir/build.log" 2>&1; then
	rc=0
	"$pdir/panic" >"$pdir/stdout" 2>"$pdir/stderr" || rc=$?
	out="$(cat "$pdir/stdout" 2>/dev/null)"
	err="$(cat "$pdir/stderr" 2>/dev/null)"
	if [ "$rc" = "134" ] && [ "$err" = "panic: worker died" ] && [ -z "$out" ]; then
		log_pass "$name" "stderr carried the panic, exit 134"
	else
		log_fail "$name" "rc=$rc stdout=[$out] stderr=[$err]"
	fi
else
	log_fail "$name" "$(tail -3 "$pdir/build.log" 2>/dev/null)"
fi

rm -rf "$td"
