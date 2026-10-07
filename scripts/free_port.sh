#!/usr/bin/env bash
# Which loopback port a gate run may serve its application on. This file is
# sourced and never run: scripts/e2e.sh asks before it writes the config the
# application serves from, and scripts/free_port_test.sh is the pin.
#
# The answer comes from the operating system rather than from a literal. The
# browser gate served a hard-coded 8099 for as long as this script has existed,
# and a run cannot know that another wants the same number: two `make e2e` on one
# host competed for it, the loser printed "something is already listening on
# 8099", and a gate that had nothing to do with the tree under test was refused by
# a listener somebody else's run had left open (the program's gate log of
# 2026-10-04). The CONTRIBUTING line that told people that concurrent runs "need
# distinct PLATFORMKIT_E2E_PORT values" described the symptom as if it were the design.
#
# Node is already this gate's prerequisite — it parses the PostgreSQL URLs — so
# the answer costs no new tool, and `listen(0)` is the kernel's own allocation
# rather than a scan of a range that something else may be holding.
#
# What is NOT closable here: the window between this socket closing and the
# application binding the same number. Nothing portable hands a bound socket to a
# process about to be exec'd, so the caller is given the number it got and tries
# again when the bind refuses; scripts/e2e.sh does, and only for a port it chose
# for itself.
#
# What the window does make necessary is answered below. A run that cannot keep the
# port it was offered must not find out by driving somebody else's application, and
# an HTTP 200 on /health says only that *something* answers: it is the shape of the
# probe, not the signature of the process behind it. A listener that took the port
# while gate 10 was building its binary answered /health, the run reported `serving
# on` that port, ninety-odd browser journeys passed against `python3 -m http.server`
# and the gate exited 0 (the program's review of 2026-10-04). So the second question
# is asked of the socket table, and it is asked of the kernel rather than of the
# answer: whose socket is this?

port_listeners() { # [port] — every pid the operating system attributes to a listener on the loopback port, one per line; nothing when nobody holds it; exit 2 when this machine answers no question about a port
	local port="${1:-}" raw="" rc=0
	case "$port" in
	'' | *[!0-9]*)
		echo "free port: port_listeners asks about a port number, not '$port'" >&2
		return 2
		;;
	esac
	# iproute2 first: it is the tool scripts/e2e.sh used for this question before it
	# stopped asking it at all. `-p` names the process behind each socket, which an
	# ordinary user is told about their own processes and no one else's — exactly the
	# reach the question needs, since the answer this gate acts on is "is it mine?".
	if command -v ss >/dev/null 2>&1; then
		if raw="$(ss -ltnHpn "sport = :$port" 2>/dev/null)"; then
			printf '%s\n' "$raw" | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | sort -u
			return 0
		fi
	fi
	if command -v lsof >/dev/null 2>&1; then
		raw="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null)" || rc=$?
		if [ "$rc" -le 1 ]; then # 1 is lsof's own "no files selected": an answer, not a failure
			printf '%s\n' "$raw" | sed -n '/^[0-9][0-9]*$/p' | sort -u
			return 0
		fi
	fi
	echo "free port: neither ss nor lsof will answer who is listening on $port" >&2
	return 2
}

# Before an empty answer is read as "that port is not mine", the machine has to have
# proved it can answer at all. ss without `-p`, a container with no /proc, a stripped
# lsof: each returns nothing for every question, and a gate that trusts the silence
# refuses runs it should serve. So the mechanism is tried against a listener this
# script started itself, on a port it just took from the same allocator, and the run
# keeps whichever answer comes back.
port_attribution_works() { # exit 0 when this machine attributes a listening socket to the process holding it, 1 when it does not
	local probe="" pid="" tries=0
	if ! probe="$(bind_free_port)"; then
		return 1
	fi
	node -e 'const net = require("node:net");
		const server = net.createServer();
		server.on("error", () => process.exit(1));
		server.listen(Number(process.argv[1]), "127.0.0.1");' "$probe" >/dev/null 2>&1 &
	pid=$!
	while [ "$tries" -lt 20 ]; do
		if port_listeners "$probe" 2>/dev/null | grep -qx "$pid"; then
			kill "$pid" 2>/dev/null || true
			wait "$pid" 2>/dev/null || true
			return 0
		fi
		if ! kill -0 "$pid" 2>/dev/null; then break; fi
		sleep 0.2
		tries=$((tries + 1))
	done
	kill "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
	return 1
}

bind_free_port() { # [wanted] — print a loopback port a fresh listener can bind: `wanted` when it is free, otherwise an allocated one; exit 1 when `wanted` is taken
	node -e 'const net = require("node:net");
		const wanted = Number(process.argv[1] || 0);
		if (!Number.isInteger(wanted) || wanted < 0 || wanted > 65535) {
			console.error(`free port: ${process.argv[1]} is not a port number`);
			process.exit(2);
		}
		const server = net.createServer();
		server.on("error", (error) => {
			// A taken port and a port this user may not bind are both refusals the
			// caller acts on by moving. Anything else is a broken toolchain, and a
			// gate that prints one message for both hides the cause in the noise.
			if (!["EADDRINUSE", "EACCES"].includes(error.code)) console.error(`free port: ${error.code}`);
			process.exit(1);
		});
		server.listen(wanted, "127.0.0.1", () => {
			const { port } = server.address();
			server.close(() => process.stdout.write(String(port)));
		});' "${1:-}"
}
