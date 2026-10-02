#!/usr/bin/env bash
# The port an end-to-end run serves on, printed alone on stdout.
#
# 8099 is the address scripts/e2e.sh serves on and the address e2e/playwright.config.ts
# falls back to, so it is the address every e2e run on a machine asks for. Two runs then
# agree to share one address: whoever loses the bind dies holding nothing, and whoever
# wins drives a browser whose requests may be answered by whichever application holds the
# port. Measured here on 2026-10-02: a second `make e2e` beside a first reported 89 red
# tests, every one of them a journey of the first run's. So the harness asks for an
# address and takes the one it is given.
#
# PLATFORMKIT_E2E_PORT is a caller speaking for itself: honoured exactly, and refused when
# it is already listening. A caller that named a port has a reason for it — a job whose
# browser reaches that port and no other, a developer who wants the run beside their own
# server — and moving it in silence would drive something other than what they asked for.
# With nothing said, the default is kept while it is empty and stepped around the moment
# it is not: the ordinary run on an ordinary machine keeps the familiar address, and the
# two runs that collide go past each other instead of over each other.
#
# It is a script of its own, and not three lines beside the other refusals in scripts/e2e.sh,
# because its answer is also an input: the port goes into the served address, into the
# public host every mailed link is built from, and into the journeys' own environment, so
# one port decided twice is three addresses that disagree. scripts/e2e_port_test.sh drives
# each of the three branches for the price of one node process each, which a run of
# scripts/e2e.sh — build, migrate, bootstrap, serve, 118 journeys — is not.
set -euo pipefail

default="${1:?which port to serve on when nobody says otherwise}"
requested="${PLATFORMKIT_E2E_PORT:-}"
port="${requested:-$default}"

# Bound, released, and closed: `ss` reports what a machine looks like, and a listener that
# is shutting down is as unusable as one that is open. Trying the bind answers the question
# the run actually cares about, at the cost of the window between this answer and the real
# listen — see Limits in this repository's delivery note for what a lost race costs. The port
# arrives as process.argv[1], not [2]: with `node -e` the first argument after the code is
# argv[1], and reading [2] hands listen undefined, which happily binds a port of the kernel's
# choosing and reports any port on the machine as free.
is_free() {
	node -e 'const net = require("node:net");
		const server = net.createServer();
		server.once("error", () => process.exit(1));
		server.listen(process.argv[1], "127.0.0.1", () => server.close(() => process.exit(0)));' "$1"
}

if is_free "$port"; then
	printf '%s\n' "$port"
	exit 0
fi

if [ -n "$requested" ]; then
	echo "e2e: PLATFORMKIT_E2E_PORT=$port is already listening; a run there would drive whatever is behind it." >&2
	exit 1
fi

# Someone else holds the familiar address. Take a port the kernel considers spare — the
# same answer a socket gets when it asks for one — and check it back before handing it over.
for _ in 1 2 3 4 5; do
	candidate="$(node -e 'const net = require("node:net");
		const server = net.createServer();
		server.once("error", () => process.exit(1));
		server.listen(0, "127.0.0.1", () => {
			const { port } = server.address();
			server.close(() => process.stdout.write(String(port)));
		});')" || continue
	[ -n "$candidate" ] || continue
	if is_free "$candidate"; then
		echo "e2e: something is already listening on $default, so this run serves on $candidate instead." >&2
		printf '%s\n' "$candidate"
		exit 0
	fi
done

echo "e2e: no free port to serve on: $default is taken and the kernel offered nothing else." >&2
exit 1
