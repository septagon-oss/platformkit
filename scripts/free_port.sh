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
# What is NOT closed here: the window between this socket closing and the
# application binding the same number. Nothing portable hands a bound socket to a
# process about to be exec'd, so the caller is given the number it got and tries
# again when the bind refuses; scripts/e2e.sh does, and only for a port it chose
# for itself.

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
