#!/usr/bin/env bash
# Gate 10 never drives an application it did not start. scripts/e2e.sh asks whether
# its port is free before it builds the binary and the Storybook and bootstraps a
# tenant — minutes — and then takes any answer on /health as its own application's.
# Here a listener that answers /health takes the named port while gate 10 is still
# building. The application this run starts cannot bind it, so the run must stop
# before the browser suite: never hand Playwright an address its own binary is not
# serving.
#
# Needs the Postgres `make e2e` uses (the two PLATFORMKIT_TEST_* URLs, Makefile
# defaults below) and node; the browser suite is only listed (`--list`), never run.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=scripts/free_port.sh
. "$root/scripts/free_port.sh"

: "${PLATFORMKIT_TEST_ADMIN_URL:=postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable}"
: "${PLATFORMKIT_TEST_DATABASE_URL:=postgres://platformkit_app:platformkit@localhost:5432/platformkit?sslmode=disable}"
export PLATFORMKIT_TEST_ADMIN_URL PLATFORMKIT_TEST_DATABASE_URL

work="$(mktemp -d)"
gate_pid=""
decoy_pid=""
cleanup() { # only what this case started, by the pids it recorded
	for pid in "$decoy_pid" "$gate_pid"; do
		if [ -n "$pid" ]; then
			kill "$pid" 2>/dev/null || true
			wait "$pid" 2>/dev/null || true
		fi
	done
	rm -rf "$work"
}
trap cleanup EXIT

port="$(bind_free_port)"
mkdir -p "$work/decoy"
echo ok >"$work/decoy/health"

(cd "$root" && PLATFORMKIT_E2E_PORT="$port" ./scripts/e2e.sh surfaces.spec.ts --list </dev/null) >"$work/gate.log" 2>&1 &
gate_pid=$!

# The port check is behind the run once it has started on its database, and the
# application does not bind until the tenant is bootstrapped; take the port between.
for _ in $(seq 1 6000); do
	grep -q 'a database of its own' "$work/gate.log" && break
	kill -0 "$gate_pid" 2>/dev/null || break
	sleep 0.1
done
if ! grep -q 'a database of its own' "$work/gate.log"; then
	echo "FAIL: gate 10 never reached its database, so the case proves nothing; it said:"
	sed 's/^/    /' "$work/gate.log"
	exit 2
fi
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work/decoy" >"$work/decoy.log" 2>&1 </dev/null &
decoy_pid=$!

status=0
wait "$gate_pid" || status=$?
gate_pid=""

# Reachability: the listener this case started held the port for the whole run. If it
# lost the bind to gate 10's own application, nothing foreign was ever on the port.
if ! kill -0 "$decoy_pid" 2>/dev/null; then
	echo "FAIL: the decoy lost the bind, so this case proves nothing; it and gate 10 said:"
	sed 's/^/    /' "$work/decoy.log" "$work/gate.log"
	exit 2
fi

failures=0
if [ "$status" -eq 0 ]; then
	echo "FAIL: gate 10 finished green with somebody else's listener on its port $port"
	failures=$((failures + 1))
else
	echo "ok   gate 10 refused once another listener held its port (exit $status)"
fi
if grep -qE '^Listing tests:|Total: [0-9]+ tests? in' "$work/gate.log"; then
	echo "FAIL: gate 10 handed Playwright port $port, which its own application was not serving"
	failures=$((failures + 1))
else
	echo "ok   the browser suite was never pointed at a port the run did not serve"
fi
if [ "$failures" -ne 0 ]; then
	sed 's/^/    /' "$work/gate.log"
	exit 1
fi
