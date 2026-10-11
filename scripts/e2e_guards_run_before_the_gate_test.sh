#!/usr/bin/env bash
# Gate 10's two port refusals are only evidence if something runs them. `make check-e2e-guards`
# must run both cases, and the CI job must run that goal where node and the browser exist (after
# setup-node and the browser install) and ahead of `make e2e`, the gate they guard.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
ci="$root/.gitea/workflows/ci.yml"
failures=0

recipe="$(make -C "$root" --no-print-directory -n check-e2e-guards </dev/null 2>&1)" || {
	echo "FAIL: make has no check-e2e-guards goal:"
	sed 's/^/    /' <<<"$recipe"
	exit 1
}
for case in scripts/e2e_unattributed_listener_test.sh scripts/e2e_port_taken_during_build_test.sh; do
	if grep -qF "$case" <<<"$recipe"; then
		echo "ok   make check-e2e-guards runs $case"
	else
		echo "FAIL: make check-e2e-guards does not run $case"
		failures=$((failures + 1))
	fi
done

line_of() { # first line of ci.yml matching the extended regex $1, or empty
	grep -nE "$1" "$ci" | head -1 | cut -d: -f1
}
node="$(line_of 'uses: actions/setup-node@')"
browser="$(line_of 'npx playwright install')"
guards="$(line_of '^[[:space:]]+run: make check-e2e-guards[[:space:]]*$')"
gate="$(line_of '^[[:space:]]+run: make e2e[[:space:]]*$')"
if [ -z "$node" ] || [ -z "$browser" ] || [ -z "$guards" ] || [ -z "$gate" ]; then
	echo "FAIL: ci.yml must install node and the browser and run both goals (setup-node=$node, browser=$browser, guards=$guards, e2e=$gate)"
	failures=$((failures + 1))
elif [ "$node" -lt "$guards" ] && [ "$browser" -lt "$guards" ] && [ "$guards" -lt "$gate" ]; then
	echo "ok   CI runs the guards after node ($node) and the browser ($browser), before make e2e ($gate), at line $guards"
else
	echo "FAIL: CI runs the guards at line $guards; node is installed at $node, the browser at $browser, make e2e at $gate"
	failures=$((failures + 1))
fi

[ "$failures" -eq 0 ]
