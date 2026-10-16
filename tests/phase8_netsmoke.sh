#!/bin/bash
# Phase 8: the net stack end to end, over real sockets, on the channel handler
# path.
#
# The shell owns the socket, the framing and the keep-alive loop; a handler is a
# pure stream function that receives decoded request messages and sends response
# messages. Both dispatch policies are exercised, because the messages are the
# same in both and that is the point: spawning is a scheduling choice.
#
#   inline   every handler runs in the connection's own domain, no spawn at all
#            - the deterministic mode, asserted by repeated identical requests
#   spawned  a fixed pool of handler routines pulls from its queue, sized from
#            the measured cost of a spawn (spawncost fixture)
#
# A registered route must serve: curl (a foreign client) and the Moxie client
# both assert the handler's body, and the client resolves a host name first
# (net.LookupHost over the hosts file, the path that SIGSEGVed before the
# root-arena fix in src/net/hosts.mx).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export MOXIEROOT="$ROOT"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 8: network smoke (channel handler path) ==="

td="$(tempdir "netsmoke")"; rm -rf "$td"; mkdir -p "$td"

build_ok=1
"$MOXIE" build -o "$td/server" "$SCRIPT_DIR/netsmoke/server" >"$td/server-build.log" 2>&1 || build_ok=0
"$MOXIE" build -o "$td/client" "$SCRIPT_DIR/netsmoke/client" >"$td/client-build.log" 2>&1 || build_ok=0
"$MOXIE" build -o "$td/spawncost" "$SCRIPT_DIR/netsmoke/spawncost" >"$td/spawncost-build.log" 2>&1 || build_ok=0

if [ "$build_ok" = 0 ]; then
	log_fail "netsmoke:build" "$(grep -m1 -E 'error|^; ' "$td/server-build.log" "$td/client-build.log" "$td/spawncost-build.log" 2>/dev/null | head -1)"
	rm -rf "$td"
	exit 0
fi
log_pass "netsmoke:build" "server, client and spawn-cost fixtures built"

# The pool size is a bound, not a guess: this is what a spawn costs here.
if "$td/spawncost" >"$td/spawncost.out" 2>&1; then
	log_pass "netsmoke:spawn-cost" "$(cat "$td/spawncost.out")"
else
	log_fail "netsmoke:spawn-cost" "$(tail -2 "$td/spawncost.out" 2>/dev/null)"
fi

# The box runs a resolver and other services, so a fixed port is checked rather
# than assumed free.
free_port() {
	local p
	for p in 39128 39129 39130 39131 39132; do
		if ! ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$p\$"; then
			echo "$p"
			return 0
		fi
	done
	return 0
}

run_mode() {
	local mode="$1" port pid listening=0 out1 out2 out3
	port="$(free_port)"
	if [ -z "$port" ]; then
		log_fail "netsmoke:port-$mode" "no free port in 39128-39132"
		return 0
	fi
	"$td/server" "127.0.0.1:$port" "$mode" >"$td/server-$mode.out" 2>&1 &
	pid=$!
	for _ in $(seq 1 50); do
		if ! kill -0 "$pid" 2>/dev/null; then
			break
		fi
		if ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$port\$"; then
			listening=1
			break
		fi
		sleep 0.2
	done
	if [ "$listening" = 0 ]; then
		log_fail "netsmoke:listen-$mode" "$(tail -3 "$td/server-$mode.out" 2>/dev/null)"
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
		return 0
	fi
	log_pass "netsmoke:listen-$mode" "listening on 127.0.0.1:$port"

	# 8a: a foreign client sees the route the fixture registered.
	out1="$(curl -s -m 15 "http://127.0.0.1:$port/smoke" 2>&1 || true)"
	if [ "$out1" = "hello path=/smoke" ]; then
		log_pass "netsmoke:curl-$mode" "200 with the handler body"
	else
		log_fail "netsmoke:curl-$mode" "curl got: $out1"
	fi

	# Keep-alive: two requests on ONE connection. curl reuses the connection
	# when it is given two URLs, so this is the shell's per-connection loop and
	# not two accept cycles.
	ka="$(curl -s -m 15 "http://127.0.0.1:$port/one" "http://127.0.0.1:$port/two" 2>&1 || true)"
	want_ka="hello path=/one
hello path=/two"
	if [ "$ka" = "$want_ka" ]; then
		log_pass "netsmoke:keepalive-$mode" "two requests on one connection"
	else
		log_fail "netsmoke:keepalive-$mode" "got: [$ka]"
	fi

	# The single-domain mode is the deterministic one: the same request must
	# produce byte-identical responses, every time.
	out2="$(curl -s -m 15 "http://127.0.0.1:$port/smoke" 2>&1 || true)"
	out3="$(curl -s -m 15 "http://127.0.0.1:$port/smoke" 2>&1 || true)"
	if [ "$out1" = "$out2" ] && [ "$out2" = "$out3" ]; then
		log_pass "netsmoke:repeatable-$mode" "three identical requests, identical responses"
	else
		log_fail "netsmoke:repeatable-$mode" "responses differ: [$out1] [$out2] [$out3]"
	fi

	# 8b: the Moxie client resolves a name and completes a real request. The
	# timeout is here because the client's own transport is the known blocker
	# (see docs/OPEN_WORK.md): it calls readLoop then writeLoop with no
	# concurrency, so it resolves, connects, and then waits for a response to a
	# request it never writes. That is a CLIENT defect, not the resolver (which
	# resolves here) and not this handler path (curl serves in both modes).
	# The unit says which of the two it is instead of blaming the resolver.
	crc=0
	timeout 20 "$td/client" "http://127.0.0.1:$port/smoke" >"$td/client-$mode.out" 2>&1 || crc=$?
	if [ "$crc" = 0 ]; then
		log_pass "netsmoke:client-$mode" "$(grep -c 'addr=' "$td/client-$mode.out" 2>/dev/null || true) address(es), status 200, body matched"
	elif grep -q 'addr=' "$td/client-$mode.out" 2>/dev/null && ! grep -q 'status=' "$td/client-$mode.out" 2>/dev/null; then
		log_fail "netsmoke:client-$mode" "resolver ok, request never completed (rc=$crc): Transport.dialConn runs readLoop before writeLoop, so the request is never written"
	else
		log_fail "netsmoke:client-$mode" "rc=$crc: $(tail -3 "$td/client-$mode.out" 2>/dev/null)"
	fi

	kill "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
}

run_mode inline
run_mode spawned

rm -rf "$td"

# A phase that cannot go red is not a gate: this script's log showed two FAIL
# verdicts (the client transport blocker, which is its own defect) while the
# process exited 0, so the driver recorded a green phase over a red log. Exit
# non-zero when anything failed, the same way phase 3 does.
if [ -f "$STATUSFILE" ] && [ "$(grep -c '^FAIL' "$STATUSFILE" 2>/dev/null || echo 0)" -gt 0 ]; then
	exit 1
fi
