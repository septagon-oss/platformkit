#!/usr/bin/env bash
# A browser run drives the application it started, or none. wait_healthy in
# scripts/e2e.sh and scripts/mobile_e2e.sh answers whether that application
# served its probe; a /health answered by some other process on the same port,
# while the run's own application has already exited, is not that answer.
#
# The case starts a stranger that answers /health on a free port, starts the
# run's application as a process that exits without binding anything, and asks
# each script's own wait_healthy (extracted from the committed file, not
# retyped). The run must be refused.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
stranger=""
cleanup() {
	if [ -n "$stranger" ]; then kill "$stranger" 2>/dev/null || true; fi
	rm -rf "$work"
}
trap cleanup EXIT

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
mkdir -p "$work/www"
echo ok >"$work/www/health"
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work/www" </dev/null >"$work/stranger.log" 2>&1 &
stranger=$!
for _ in $(seq 1 50); do
	curl -fsS "http://localhost:$port/health" >/dev/null 2>&1 && break
	sleep 0.1
done
curl -fsS "http://localhost:$port/health" >/dev/null 2>&1 || {
	echo "the stranger never answered /health on port $port; the case asked nothing" >&2
	exit 2
}

failed=0
for script in scripts/e2e.sh scripts/mobile_e2e.sh; do
	sed -n '/^wait_healthy()/,/^}/p' "$root/$script" >"$work/fn.sh"
	if [ ! -s "$work/fn.sh" ]; then
		echo "FAIL $script: no wait_healthy to ask" >&2
		failed=1
		continue
	fi
	echo "the application never bound its port" >"$work/app.log"
	if (
		# shellcheck disable=SC1091
		. "$work/fn.sh"
		sh -c 'exit 3' &
		app_pid=$!
		wait "$app_pid" 2>/dev/null || true
		wait_healthy 3
	) </dev/null >/dev/null 2>&1; then
		echo "FAIL $script: wait_healthy accepted a stranger's /health on port $port after the run's own application had exited" >&2
		failed=1
	else
		echo "ok   $script: a stranger's /health is not the run's application serving"
	fi
done
exit "$failed"
