#!/usr/bin/env bash
# A successful socket query with no ownership information must not authorize a run.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Model a restricted socket table: ss succeeds but cannot identify processes. Keep node real so the
# attribution check actually starts its own listener — only the two tools that answer about sockets
# are shadowed, and they are shadowed by executables ahead of the real PATH rather than by exported
# shell functions: a function reaches only a child bash that imports exported functions, and a model
# that never arrives leaves this case watching a run that answers /health, builds the binary and
# drives somebody else's listener, which is the shape the case exists to refuse. An executable on
# PATH reaches whatever the child turns out to be, and the child below is asked to say so for itself
# before it is timed. Case 6 of scripts/free_port_test.sh models the same silence with the same tools.
mkdir -p "$work/bin"
printf '#!/bin/sh\nexit 0\n' >"$work/bin/ss"
printf '#!/bin/sh\nexit 1\n' >"$work/bin/lsof"
chmod +x "$work/bin/ss" "$work/bin/lsof"

# The premise is asserted inside the child that runs gate 10, and not beside it: the thing that has to
# be true is that THIS shell cannot see who owns a socket. A shell whose model did not arrive refuses
# with exit 3 and its own words, so a missing model is never reported as a refusal that came late.
status=0
MODEL_BIN="$work/bin" \
	PATH="$work/bin:$PATH" \
	PLATFORMKIT_TEST_ADMIN_URL='postgres://unused:unused@127.0.0.1:1/unused' \
	PLATFORMKIT_TEST_DATABASE_URL='postgres://unused:unused@127.0.0.1:1/unused' \
	timeout 15 bash -c '
		for tool in ss lsof; do
			found="$(command -v "$tool" 2>/dev/null || true)"
			if [ "$found" != "$MODEL_BIN/$tool" ]; then
				echo "e2e: the modelled socket table is not in force here: $tool resolves to ${found:-nothing}, not $MODEL_BIN/$tool" >&2
				exit 3
			fi
		done
		exec bash "$0" "$@"
	' "$root/scripts/e2e.sh" surfaces.spec.ts --list \
	>"$work/output" 2>&1 </dev/null || status=$?

if [ "$status" -ne 1 ]; then
	echo "FAIL: unavailable socket attribution must refuse, got exit $status"
	cat "$work/output"
	exit 1
fi
if ! grep -q 'this machine cannot say which process is listening' "$work/output"; then
	echo 'FAIL: the refusal must explain the unavailable attribution'
	cat "$work/output"
	exit 1
fi
if grep -qE 'a database of its own|serving on|Listing tests:|Total: .* tests' "$work/output"; then
	echo 'FAIL: the run advanced without proof of listener ownership'
	cat "$work/output"
	exit 1
fi
echo 'ok   unavailable socket attribution refuses before database creation or browser dispatch'
