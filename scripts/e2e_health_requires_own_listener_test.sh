#!/usr/bin/env bash
# A live application process may still be blocked before it binds its port.
# Another listener's healthy answer cannot establish that this application serves.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/pkit-health-owner.XXXXXX")"
stranger=""
app_pid=""
cleanup() {
	[ -z "$app_pid" ] || { kill "$app_pid" 2>/dev/null || true; wait "$app_pid" 2>/dev/null || true; }
	[ -z "$stranger" ] || { kill "$stranger" 2>/dev/null || true; wait "$stranger" 2>/dev/null || true; }
	rm -rf "$work"
}
trap cleanup EXIT

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
mkdir -p "$work/www"
echo ok >"$work/www/health"
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work/www" </dev/null >"$work/stranger.log" 2>&1 &
stranger=$!
for _ in $(seq 1 50); do
	if curl -fsS "http://localhost:$port/health" >/dev/null 2>&1; then break; fi
	sleep 0.1
done
curl -fsS "http://localhost:$port/health" >/dev/null 2>&1 || {
	echo "the stranger never answered /health; the case asked nothing" >&2
	exit 2
}

echo "the application has not bound its port" >"$work/app.log"
sleep 30 </dev/null &
app_pid=$!
kill -0 "$app_pid" 2>/dev/null || {
	echo "the application process is not alive; the case asked the wrong question" >&2
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
	if (
		# shellcheck disable=SC1091
		. "$work/fn.sh"
		wait_healthy 2
	) </dev/null >/dev/null 2>&1; then
		echo "FAIL $script: wait_healthy accepted another listener while its application was alive but not serving" >&2
		failed=1
	else
		echo "ok   $script: a live application must serve its own health answer"
	fi
done
exit "$failed"
