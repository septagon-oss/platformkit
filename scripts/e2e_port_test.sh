#!/usr/bin/env bash
# Which address an end-to-end run serves on, and when it refuses to serve at all.
#
# scripts/e2e.sh used to write 8099 into the configuration it generates and then refuse to
# start when something already listened there — right for the one run on a machine, and a red
# gate for every other run on the same one, because the journey port is the single port the
# task harness does not allocate per checkout. The port now comes from scripts/e2e_port.sh:
# an explicit one honoured exactly, a busy default stepped around, a free default kept.
#
# A port is only busy if something is listening on it, so the fixture binds a socket and asks
# the script about it from inside the same process, and reports that a second bind on that port
# was refused — the case proves its own premise. The first draft of this file backgrounded a
# holder instead and two cases passed by accident, because the holder had already exited and
# "busy" had come to mean nothing. Each case is one node process holding one socket and
# spawning the script against it: no database, no binary, no browser, nothing left running.
#
# What this cannot reach is that the port a run serves on is the port the journeys are told
# about. e2e/session-recovery.spec.ts refuses to write to a harness whose URL does not name the
# port it was given, so the last of the `make e2e` journeys checks that half.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="$root/scripts/e2e_port.sh"
work="$(mktemp -d)"
failures=0
trap 'rm -rf "$work"' EXIT

cat >"$work/ask.cjs" <<'JAVASCRIPT'
// ask.cjs <script> <port to hold, or "none"> <explicit PLATFORMKIT_E2E_PORT, or "-"> <default to ask for>
// Holds a port while the script is asked about it, records that the hold is real, and reports
// everything the script answered.
const net = require('node:net');
const { spawnSync } = require('node:child_process');
const [script, hold, explicit, requested] = process.argv.slice(2);

let server = null;

const answer = (fields) =>
	process.stdout.write(
		JSON.stringify({ hold: hold === 'none' ? null : Number(hold), ...fields }) + '\n',
	);

// conflict is what a second bind on the held port answered, or null when nothing was held.
const ask = (conflict) => {
	const env = { ...process.env };
	if (explicit === '-') delete env.PLATFORMKIT_E2E_PORT;
	else env.PLATFORMKIT_E2E_PORT = explicit;
	// spawnSync blocks the event loop, which is what keeps the socket open across the question.
	const run = spawnSync('bash', [script, requested], { env, encoding: 'utf8' });
	const report = () =>
		answer({
			conflict,
			status: run.status,
			stdout: (run.stdout ?? '').trim(),
			stderr: (run.stderr ?? '').trim(),
		});
	// Released before the answer is written. A listener left bound keeps this process alive
	// forever, so the shell reading its stdout would wait on a process that had already
	// answered; the first draft of this file hung exactly there.
	if (server === null) report();
	else server.close(report);
};

if (hold === 'none') {
	ask(null);
} else {
	server = net.createServer();
	server.once('error', (error) => {
		process.stderr.write(`the fixture could not hold port ${hold}: ${error.code}\n`);
		process.exit(2);
	});
	server.listen(Number(hold), '127.0.0.1', () => {
		// A listener that lets a second listener bind the same port holds nothing. Node sets
		// SO_REUSEADDR but not SO_REUSEPORT, so the second bind answers EADDRINUSE — the fact
		// every case below rests on, recorded rather than assumed.
		const rival = net.createServer();
		rival.once('error', (error) => rival.close(() => ask(error.code)));
		rival.listen(Number(hold), '127.0.0.1', () => rival.close(() => ask('bound anyway')));
	});
}
JAVASCRIPT

# A port nothing holds: the kernel's spare, released again before anything is asked about it.
free_port() {
	node -e 'const net = require("node:net");
		const server = net.createServer();
		server.listen(0, "127.0.0.1", () => {
			const { port } = server.address();
			server.close(() => process.stdout.write(String(port)));
		});'
}

bindable() { # bindable <port> — something can still listen there
	node -e 'const net = require("node:net");
		const server = net.createServer();
		server.once("error", () => process.exit(1));
		server.listen(process.argv[1], "127.0.0.1", () => server.close(() => process.exit(0)));' "$1"
}

# ask <port to hold, or none> <explicit PLATFORMKIT_E2E_PORT, or -> <default to ask for>
# Sets `answer` to what ask.cjs reported and `rc` to the script's own exit status.
answer='{"status":null,"stdout":"","stderr":""}'
rc=null
ask() {
	answer="$(node "$work/ask.cjs" "$script" "$1" "$2" "$3")" ||
		answer='{"status":2,"stdout":"","stderr":"the fixture could not run"}'
	rc="$(field status)"
}

field() { # field <status|stdout|stderr|conflict> — one field of the last answer
	printf '%s' "$answer" | node -e 'let s = "";
		process.stdin.on("data", (d) => (s += d)).on("end", () => process.stdout.write(String(JSON.parse(s)[process.argv[1]])));' "$1"
}

fail() {
	printf 'FAIL: %s\n' "$1"
	printf '    %s\n' "$answer"
	failures=$((failures + 1))
}

taken="$(free_port)"
spare="$(free_port)"

# 0. The premise under every case below: the port a case calls taken really does refuse a second
#    bind while the case is being asked. A holder that had already exited would turn the two
#    cases about a port in use into two cases about free ports, which pass whatever the code does.
ask "$taken" '-' "$taken"
if [ "$(field conflict)" = "EADDRINUSE" ]; then
	echo "ok   the port a case holds refuses a second bind while the case is asked"
else
	fail "the port a case holds refuses a second bind while the case is asked (a second bind: $(field conflict))"
fi

# 1. The ordinary run: nothing listens on the default, so the default is what the run gets. The
#    served address, the public host every mailed link is built from and the browser's own
#    address bar keep the familiar localhost:8099.
ask none '-' "$spare"
if [ "$rc" = 0 ] && [ "$(field stdout)" = "$spare" ]; then
	echo "ok   a default nobody holds is the address the run keeps"
else
	fail "a default nobody holds is the address the run keeps (asked for $spare, answered $(field stdout), exit $rc)"
fi

# 2. Two runs at once — the case the gate actually hits. The default belongs to somebody else,
#    and the answer is a different port that really can be served on. stdout carries the number
#    alone, because scripts/e2e.sh captures it into a value that goes into a configuration file;
#    the explanation goes to stderr, where a person reads it, and names the port it stepped over.
ask "$taken" '-' "$taken"
moved="$(field stdout)"
told="$(field stderr)"
if [ "$rc" = 0 ] && [ "$moved" != "$taken" ] && printf '%s' "$told" | grep -qF "$taken" && bindable "$moved"; then
	echo "ok   a default somebody else holds is stepped around, and the run says which it passed"
else
	fail "a default somebody else holds is stepped around (asked for $taken, answered $moved, exit $rc)"
fi

# 3. An explicit port is not moved. This branch is the whole of what the script used to be —
#    refuse rather than drive somebody else's application — and it still refuses, now naming the
#    variable that asked for the port so the caller knows which line of its own to change.
ask "$taken" "$taken" "$spare"
if [ "$rc" = 1 ] && [ -z "$(field stdout)" ] && printf '%s' "$(field stderr)" | grep -qF "PLATFORMKIT_E2E_PORT=$taken"; then
	echo "ok   an explicit port somebody else holds is still refused, by name"
else
	fail "an explicit port somebody else holds is still refused (exit $rc)"
fi

# 4. And a free explicit port wins over the default, whatever the default is: whoever names an
#    address is served at that address, which is how a CI job reaches a runner of its own.
ask "$taken" "$spare" "$taken"
if [ "$rc" = 0 ] && [ "$(field stdout)" = "$spare" ]; then
	echo "ok   an explicit port is honoured over the default"
else
	fail "an explicit port is honoured over the default (wanted $spare, answered $(field stdout), exit $rc)"
fi

# 5. A default nobody wrote down is not a port to serve on: the script answers with usage rather
#    than a number, so a call site that forgets its default fails at the call rather than on a
#    guess somebody else's server is sitting on.
usage="$(bash "$script" 2>&1)" && usage_rc=0 || usage_rc=$?
if [ "$usage_rc" != 0 ] && ! printf '%s' "$usage" | grep -qE '^[0-9]+$'; then
	echo "ok   a call with no default is refused, not guessed"
else
	fail "a call with no default is refused, not guessed (exit $usage_rc, output $usage)"
fi

# 6. One port, one decider. scripts/e2e.sh asks this script, exports the answer to the journeys
#    so the URL they are given and the port they check it against are one sentence, and keeps no
#    second copy of the default and no second check of its own.
drift=()
grep -q 'scripts/e2e_port.sh' "$root/scripts/e2e.sh" || drift+=('it never asks')
grep -q -- '-8099' "$root/scripts/e2e.sh" && drift+=('it keeps a default of its own')
grep -q 'ss -ltn' "$root/scripts/e2e.sh" && drift+=('it checks the port itself')
grep -q 'PLATFORMKIT_E2E_PORT="$port"' "$root/scripts/e2e.sh" || drift+=('the journeys are not told which port')
if [ ${#drift[@]} -eq 0 ]; then
	echo "ok   scripts/e2e.sh decides the port once, and tells the journeys which it decided"
else
	printf 'FAIL: scripts/e2e.sh and scripts/e2e_port.sh disagree: %s\n' "${drift[*]}"
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "e2e port: $failures case(s) failed" >&2
	exit 1
fi
echo "e2e port: a free default is kept, a busy one stepped around, an explicit one honoured or refused by name"
