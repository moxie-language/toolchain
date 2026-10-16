#!/bin/bash
# Phase 8: the net stack end to end, over real sockets.
#
# Two paths, both compiled by the compiler under test and both run here:
#
#   8a  a stdlib http.Server accepts real connections. The phase starts the
#       server, waits for the listener, and connects twice: once with a foreign
#       client (curl) and once with the Moxie client. Before the
#       embedded-interface dispatch arm existed, Serve panicked on the first
#       connection ("no impl for Accept") because onceCloseListener reaches
#       Accept only by promotion from its embedded net.Listener.
#
#   8b  a client resolves a host name and completes a real HTTP request. The
#       lookup is net.LookupHost over the hosts file, the path that SIGSEGVed
#       before the root-arena fix in src/net/hosts.mx, and the request is a real
#       TCP connection to the server above; the client asserts the body.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export MOXIEROOT="$ROOT"
source "$SCRIPT_DIR/util.sh"

echo ""
echo "=== Phase 8: network smoke (real sockets) ==="

td="$(tempdir "netsmoke")"; rm -rf "$td"; mkdir -p "$td"

# The box runs a resolver and other services, so a fixed port is checked rather
# than assumed free.
port=""
for p in 39128 39129 39130 39131 39132; do
	if ! ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$p\$"; then
		port="$p"
		break
	fi
done

if [ -z "$port" ]; then
	log_fail "netsmoke:port" "no free port in 39128-39132"
else
	build_ok=1
	"$MOXIE" build -o "$td/server" "$SCRIPT_DIR/netsmoke/server" >"$td/server-build.log" 2>&1 || build_ok=0
	"$MOXIE" build -o "$td/client" "$SCRIPT_DIR/netsmoke/client" >"$td/client-build.log" 2>&1 || build_ok=0

	if [ "$build_ok" = 0 ]; then
		log_fail "netsmoke:build" "$(grep -m1 -E 'error|panic' "$td/server-build.log" "$td/client-build.log" 2>/dev/null | head -1)"
	else
		log_pass "netsmoke:build" "server and client built"

		"$td/server" "127.0.0.1:$port" >"$td/server.out" 2>&1 &
		srvpid=$!
		trap 'kill "$srvpid" 2>/dev/null || true' EXIT

		listening=0
		for _ in $(seq 1 50); do
			if ! kill -0 "$srvpid" 2>/dev/null; then
				break
			fi
			if ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$port\$"; then
				listening=1
				break
			fi
			sleep 0.2
		done

		if [ "$listening" = 0 ]; then
			log_fail "netsmoke:listen" "no listener on 127.0.0.1:$port: $(tail -3 "$td/server.out" 2>/dev/null)"
		else
			log_pass "netsmoke:listen" "http.Server accepting on 127.0.0.1:$port"

			# 8a: a foreign client proves the socket is a real HTTP server.
			curlout="$(curl -s -m 15 "http://127.0.0.1:$port/smoke" 2>&1 || true)"
			if [ "$curlout" = "hello path=/smoke" ]; then
				log_pass "netsmoke:curl" "200 with the handler body"
			else
				log_fail "netsmoke:curl" "curl got: $curlout"
			fi

			# 8b: the Moxie client resolves a name and completes the request.
			if "$td/client" "http://127.0.0.1:$port/smoke" >"$td/client.out" 2>&1; then
				addrs="$(grep -c 'addr=' "$td/client.out" 2>/dev/null || true)"
				log_pass "netsmoke:moxie-client" "$addrs address(es), status 200, body matched"
			else
				log_fail "netsmoke:moxie-client" "$(tail -3 "$td/client.out" 2>/dev/null)"
			fi
		fi

		kill "$srvpid" 2>/dev/null || true
		wait "$srvpid" 2>/dev/null || true
		trap - EXIT
	fi
fi

rm -rf "$td"
