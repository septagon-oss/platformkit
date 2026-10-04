#!/usr/bin/env bash
# The port a gate run serves on, and the two contracts that hang off it: a port
# somebody is already listening on is refused rather than fought over, and the
# browser run at the end of scripts/e2e.sh is told which port the application was
# actually given.
#
# Node is the tool under test here, so it is a prerequisite and not something this
# case installs — which is how scripts/e2e.sh treats it for the same reason.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=scripts/free_port.sh
. "$root/scripts/free_port.sh"

if ! command -v node >/dev/null; then
	echo "free port: node is not installed, so nothing below could run; SKIPPED and unproven on this machine" >&2
	exit 0
fi

failures=0
held_pid=""
cleanup() { # the listener this case started, stopped by the pid it recorded
	if [ -n "$held_pid" ]; then
		kill "$held_pid" 2>/dev/null || true
		wait "$held_pid" 2>/dev/null || true
	fi
}
trap cleanup EXIT

check() { # check <case name> <expected exit> <actual exit>
	local name="$1" want="$2" got="$3"
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: exit $got, wanted $want"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

# 1. An allocation is a port a fresh listener can bind. The proof is to hand the
#    same answer straight back: bind_free_port <p> succeeding is the bind() the
#    application is about to make, and a port printed while taken is the bug that
#    this whole change is about.
allocated="$(bind_free_port)"
got=0
if [[ "$allocated" =~ ^[1-9][0-9]{0,4}$ ]]; then
	bind_free_port "$allocated" >/dev/null 2>&1 || got=$?
else
	got=1
	allocated="'$allocated' is not a port number at all"
fi
check "an allocation ($allocated) is a port a fresh listener can bind" 0 "$got"

# 2. A port somebody is listening on is refused, and refused on the status alone:
#    the caller decides what to say. The listener belongs to this script, was
#    started on a port it just took free of, and is stopped by the pid recorded here.
held_port="$(bind_free_port)"
node -e 'const net = require("node:net");
	const server = net.createServer();
	server.on("error", () => process.exit(1));
	server.listen(Number(process.argv[1]), "127.0.0.1");' "$held_port" &
held_pid=$!
sleep 1
if ! kill -0 "$held_pid" 2>/dev/null; then
	echo "FAIL: this case's own listener died, so nothing held $held_port and nothing was refused"
	failures=$((failures + 1))
else
	got=0
	stdout="$(bind_free_port "$held_port" 2>/dev/null || true)"
	bind_free_port "$held_port" >/dev/null 2>&1 || got=$?
	check "a port somebody is listening on ($held_port) is refused" 1 "$got"
	if [ -n "$stdout" ]; then
		echo "FAIL: a refused port is refused on the status and prints nothing; it printed '$stdout'"
		failures=$((failures + 1))
	else
		echo "ok   a refused port prints nothing, so the caller's message is the only one a person reads"
	fi

	# 3. An allocation never lands on a listener that is already up. Twenty asks is
	#    not a claim about the whole range; it is the question the gate asks once,
	#    asked often enough to catch a scan upward from a literal pretending to be an
	#    allocation — which is exactly what the fixed 8099 was.
	landed=""
	for _ in $(seq 1 20); do
		if [ "$(bind_free_port)" = "$held_port" ]; then landed="$held_port"; break; fi
	done
	if [ -n "$landed" ]; then
		echo "FAIL: an allocation landed on the port this case is holding ($landed)"
		failures=$((failures + 1))
	else
		echo "ok   twenty allocations all avoid the one port that is taken"
	fi
fi

# 4. scripts/e2e.sh refuses a port its caller named that somebody is holding, and
#    refuses before it touches anything. The two URLs name a Postgres that is not
#    there on purpose: if the script got as far as the database it would fail for a
#    reason that has nothing to do with the port, and the case would prove nothing.
url="postgres://nobody:nobody@127.0.0.1:1/platformkit?sslmode=disable"
run_gate() { # run_gate — gate 10 with the named port held, its output on stdout, its status as the status
	(cd "$root" && PLATFORMKIT_TEST_ADMIN_URL="$url" PLATFORMKIT_TEST_DATABASE_URL="$url" \
		PLATFORMKIT_E2E_PORT="$held_port" ./scripts/e2e.sh) 2>&1
}
out="$(run_gate || true)"
got=0
run_gate >/dev/null 2>&1 || got=$?
check "gate 10 refuses a named port somebody is listening on" 1 "$got"
case "$out" in
*"already listening on $held_port"*) echo "ok   the refusal names the port it refused" ;;
*)
	echo "FAIL: the refusal does not name $held_port; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
esac
case "$out" in
*"a database of its own"* | *"e2e: serving"*)
	echo "FAIL: gate 10 got past the port and into the run; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   the refusal happens before a database is created or a binary is built" ;;
esac

# 5. The browser run is told which port the application actually got. These two
#    greps are the whole of that contract, and it is a contract and not a detail:
#    e2e/session-recovery.spec.ts compares the URL's port against PLATFORMKIT_E2E_PORT
#    and refuses its journeys on any other address, so a run that moved the port
#    without saying so would lose those journeys, and a run that told the app one
#    port and the browser another would have the suite drive somebody else's.
got=0
grep -qF 'PLATFORMKIT_E2E_PORT="$port"' "$root/scripts/e2e.sh" || got=$?
check "gate 10 exports the port it chose, from the same variable as the URL it exports" 0 "$got"
got=0
grep -qF 'PLATFORMKIT_E2E_URL="http://localhost:$port"' "$root/scripts/e2e.sh" || got=$?
check "gate 10 points Playwright at that same port" 0 "$got"
if grep -qE 'PLATFORMKIT_E2E_PORT:-[0-9]' "$root/scripts/e2e.sh"; then
	echo "FAIL: gate 10 still assumes a port of its own; that answer belongs to the kernel"
	failures=$((failures + 1))
else
	echo "ok   gate 10 carries no literal default port"
fi

if [ "$failures" -ne 0 ]; then
	echo "free port: $failures case(s) failed" >&2
	exit 1
fi
echo "free port: an allocation is bindable, a held port is refused before anything is touched, and the browser is told which port it got"
