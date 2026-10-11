#!/usr/bin/env bash
# Which failures of the API gate say something about the tree, and which say something about the host.
#
# `make check-apidiff` runs `go run golang.org/x/exp/cmd/apidiff@<pinned>` against two `git archive`
# exports. Asking the module proxy for that pinned tool — the module's retirement notice, which `go run
# <module>@<version>` wants before it builds anything — is the only step in the target that needs a
# network. On a host whose resolver stopped answering for a few seconds that one lookup ended the whole
# `make check` with `loading deprecation for golang.org/x/exp: … dial tcp: lookup proxy.golang.org: i/o
# timeout`, four gate runs in a row, with nothing wrong in the tree and nothing else in the output. So
# the script now waits and tries again — and what that bound is has to be pinned, because the two ways to
# get it wrong are a gate that gives up on the first DNS blip (what this branch had) and a gate that
# retries forever (a hang nobody reads as a hang).
#
# What these cases cannot reach is the verdict itself — whether an exported signature moved. That is the
# baseline diff in check_public_api.py's own output, which is why the fourth case drives the real script
# end to end: it has to show that a fetch which never heals still exits non-zero. A retry that could turn
# a refusal into a pass would be a removed gate, and it cannot: with -m -incompatible apidiff exits 0
# whatever it finds (cmd/apidiff/main.go exits non-zero only when it cannot write its report), so the
# three commands the retry wraps carry no verdict at all.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
failures=0

# The error the loop's gate died on, word for word from gate-implement.log at 02:30Z on 2026-10-07.
# Trailing whitespace only ever appears at the end of it, so it has to be trimmed.
TIMEOUT='go: golang.org/x/exp/cmd/apidiff@v0.0.0-20260908205506-85c1c2202aba: loading deprecation for golang.org/x/exp: module golang.org/x/exp: Get "https://proxy.golang.org/golang.org/x/exp/@v/list": dial tcp: lookup proxy.golang.org: i/o timeout'

classification() { # classification <output the go command wrote> <tries already spent> — prints "wait <seconds>" or "final"
	python3 - "$2" "$1" <<'PY'
import importlib.util
import sys

spec = importlib.util.spec_from_file_location("check_public_api", "scripts/check_public_api.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
after, output = int(sys.argv[1]), sys.argv[2]
wait = module.fetch_wait(after, output)
print(f"wait {wait}" if wait is not None else "final")
PY
}

expect() { # expect <want> <case name> <after> <output>
	local want="$1" name="$2" after="$3" output="$4" got
	got="$(classification "$output" "$after")"
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: '$got', wanted '$want'"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

# 1. The observed blip is retried, and the wait before the next try is the first one.
expect "wait 5" "a proxy lookup that timed out is retried" 0 "$TIMEOUT"

# 2. So is a proxy that answered 5xx and a dial that was refused: the same class, a different wording.
expect "wait 5" "a proxy that answered 502 is retried" 0 \
	'go: golang.org/x/exp/cmd/apidiff@v0.0.0-20260908205506-85c1c2202aba: golang.org/x/exp@v0.0.0-20260908205506-85c1c2202aba: reading https://proxy.golang.org/golang.org/x/exp/@v/list: 502 Bad Gateway'
expect "wait 5" "a refused dial is retried" 0 \
	'go: module lookup disabled: reading http://127.0.0.1:1/golang.org/x/exp/@v/list: dial tcp 127.0.0.1:1: connect: connection refused'

# 3. The retry is bounded, and a fact about the tree is never retried at all. The second pair is what a
#    package that will not compile writes; the third is the last wait spent.
expect "final" "a compile error is reported on the first try" 0 \
	'apidiff: golang.org/x/exp/cmd/apidiff: packages.Load error: err: exit status 1: -: missing import path'
expect "wait 20" "the second blip waits the longer wait" 1 "$TIMEOUT"
expect "final" "the third blip is the failure the run reports" 2 "$TIMEOUT"

# 4. End to end: with every proxy unreachable, the real target still refuses. GOPROXY names a closed
#    port, so each of the three go runs reaches the retry and each of them is refused; the exit code is
#    the one `make check-apidiff` must return, and the two retry lines are the evidence the loop ran.
#    25 s of waiting is the bound itself, paid here rather than asserted as a number.
out="$(GOPROXY='http://127.0.0.1:1' GOFLAGS= timeout 180 \
	python3 scripts/check_public_api.py v1.1.0 HEAD \
	--baseline scripts/baselines/public-api-v1.1.0.json 2>&1)" && got=0 || got=$?
if [ "$got" != 2 ]; then
	echo "FAIL: an unreachable proxy still refuses the gate: exit $got, wanted 2"
	printf '    %s\n' "$out" | tail -20
	failures=$((failures + 1))
else
	for line in "+ retry 1" "+ retry 2"; do
		if ! printf '%s\n' "$out" | grep -qF "$line"; then
			echo "FAIL: an unreachable proxy: no '$line' line"
			printf '    %s\n' "$out" | tail -20
			failures=$((failures + 1))
		fi
	done
	if ! printf '%s\n' "$out" | tail -1 | grep -qi 'proxy.golang.org\|127.0.0.1'; then
		echo "FAIL: an unreachable proxy: the last line does not name the proxy it could not reach"
		printf '    %s\n' "$out" | tail -3
		failures=$((failures + 1))
	fi
	[ "$failures" -eq 0 ] && echo "ok   an unreachable proxy still refuses the gate, after both waits"
fi

if [ "$failures" -ne 0 ]; then
	echo "check_public_api fetch: $failures case(s) failed" >&2
	exit 1
fi
echo "check_public_api fetch: a proxy that will not answer is retried twice and then refused; anything else is refused at once"
