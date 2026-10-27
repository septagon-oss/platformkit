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
# script started itself, on a port the kernel allocated for it, and the run keeps
# whichever answer comes back.
#
# One node process is the allocation and the listener together: a separate `bind_free_port` would pay
# for a second start of the toolchain to ask a question whose answer is already in hand. The listener
# writes the port it was given to a private file and stays up, so the wait is for that line rather
# than for a fixed sleep and the ownership question is first asked at the moment a socket really
# exists to be asked about. The grace after it is short and bounded because a socket in LISTEN state
# sits in the kernel's table already: /proc either maps it to its owner or cannot read itself at all,
# and a machine in the second state has to be refused before the run builds anything rather than after
# it has waited a while to be sure.
port_attribution_works() { # exit 0 when this machine attributes a listening socket to the process holding it, 1 when it does not
	local announce="" probe="" pid="" tries=0 attributed="no"
	if ! announce="$(mktemp)"; then
		echo "free port: no private place for the probe listener to write its port" >&2
		return 1
	fi
	node -e 'const fs = require("node:fs");
		const net = require("node:net");
		const server = net.createServer();
		server.on("error", () => process.exit(1));
		server.listen(0, "127.0.0.1", () => {
			fs.writeFileSync(process.argv[1], `${server.address().port}\n`);
		});' "$announce" >/dev/null 2>&1 &
	pid=$!
	# The listener is up when it has said which port it got. `read` with a redirection asks the file
	# itself, so the wait costs a syscall per look rather than a pipeline.
	while [ "$tries" -lt 25 ]; do
		probe=""
		IFS= read -r probe <"$announce" 2>/dev/null || true
		case "$probe" in
		'' | *[!0-9]*) probe="" ;;
		esac
		if [ -n "$probe" ]; then break; fi
		if ! kill -0 "$pid" 2>/dev/null; then break; fi
		sleep 0.2
		tries=$((tries + 1))
	done
	tries=0
	while [ -n "$probe" ] && [ "$tries" -lt 8 ]; do
		if port_listeners "$probe" 2>/dev/null | grep -qx "$pid"; then
			attributed="yes"
			break
		fi
		if ! kill -0 "$pid" 2>/dev/null; then break; fi
		sleep 0.25
		tries=$((tries + 1))
	done
	kill "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
	rm -f "$announce"
	if [ "$attributed" = "yes" ]; then
		return 0
	fi
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
