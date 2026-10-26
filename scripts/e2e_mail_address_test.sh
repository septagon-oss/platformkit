#!/usr/bin/env bash
# Where a run finds the mail catcher, and where it refuses.
#
# scripts/e2e.sh derives both mail addresses from the two ports compose.yaml publishes: the SMTP host and
# port go into the configuration of the application it boots, and the HTTP API into the journeys'
# environment. The derivation belongs to the script rather than to the `e2e` recipe because `./scripts/e2e.sh
# one.spec.ts` is how somebody reruns a single journey — that caller carries the stack's ports and none of
# the recipe's names, and a recipe-side derivation left it dialling compose.yaml's default port while the
# catcher it had been given answered on another one. Every case here drives the script's own refusal, which
# is the observable form of the address it computed: the message quotes the URL it dialled.
#
# What these cases cannot reach is the SMTP half, which only a delivered message proves. That is the
# journeys themselves — e2e/invitation-mail.spec.ts and e2e/mailed-links.spec.ts read a mailed link out of
# the catcher, and neither can pass unless the application reached the same stack on the SMTP port.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
failures=0

# An address nothing listens at. 1 is not a port any service of this repository dials, so the answer is
# always "connection refused" — and always from curl, never from psql: the probe runs before this script
# creates its scratch directory, its trap or its database, which is what the third case asserts.
dead_port=1
admin_url="postgres://postgres:platformkit@localhost:$dead_port/platformkit?sslmode=disable"

run() { # run [<explicit API URL>] — prints everything the script writes and returns its exit code
	local api="${1:-}" over=()
	[ -n "$api" ] && over+=("PLATFORMKIT_E2E_MAILPIT_URL=$api")
	env -u PLATFORMKIT_E2E_MAIL_HOST -u PLATFORMKIT_E2E_MAIL_PORT -u PLATFORMKIT_E2E_MAILPIT_URL \
		PLATFORMKIT_TEST_ADMIN_URL="$admin_url" \
		PLATFORMKIT_TEST_DATABASE_URL="$admin_url" \
		PLATFORMKIT_MAILPIT_SMTP_PORT=1 PLATFORMKIT_MAILPIT_PORT="$dead_port" \
		"${over[@]}" \
		bash "$root/scripts/e2e.sh" 2>&1
}

expect_refusal() { # expect_refusal <case name> <URL the message must name> [<explicit API URL>]
	local name="$1" want="$2" api="${3:-}" out got=0
	out="$(run "$api")" || got=$?
	if [ "$got" != 1 ]; then
		echo "FAIL: $name: exit $got, wanted 1"
		printf '    %s\n' "$out"
		failures=$((failures + 1))
		return
	fi
	if ! printf '%s\n' "$out" | grep -qF "$want"; then
		echo "FAIL: $name: the refusal does not name $want"
		printf '    %s\n' "$out"
		failures=$((failures + 1))
		return
	fi
	echo "ok   $name"
	printf '%s\n' "$out" | sed 's/^/       /' | head -2
}

# 1. The catcher's API address comes from PLATFORMKIT_MAILPIT_PORT, with no other mail name set. That is
#    the whole of what a caller that runs one spec directly has to say.
expect_refusal "the stack's catcher port is the address the run dials" "http://localhost:$dead_port"

# 2. A caller that reaches the catcher by another address wins over the derivation. This is how
#    .gitea/workflows/ci.yml names a service that answers by container name, and it has to keep working:
#    the alternative is a job that can only reach localhost.
expect_refusal "an explicit API address wins over the port" "http://mailpit.invalid:8025" "http://mailpit.invalid:8025"

# 3. The refusal is asked before anything is built or created. Both URLs above are database URLs on a
#    closed port as well, so a run that reached Postgres first would say so, and a run that says only
#    this says the probe came first.
out="$(run)" || true
if printf '%s\n' "$out" | grep -qiE 'psql|database|could not connect'; then
	echo "FAIL: the catcher is asked before the database is touched: the output above names the database"
	failures=$((failures + 1))
else
	echo "ok   the catcher is asked before the database is touched"
fi

# 4. One derivation. The Makefile declares the two ports and exports them; if it grew a second copy of the
#    addresses, the two would disagree exactly as they did when this script fell back to a hard-coded port.
if grep -q 'PLATFORMKIT_E2E_MAIL' "$root/Makefile"; then
	echo "FAIL: the Makefile derives mail addresses again; scripts/e2e.sh is the one reader"
	grep -n 'PLATFORMKIT_E2E_MAIL' "$root/Makefile" | sed 's/^/    /'
	failures=$((failures + 1))
else
	echo "ok   the Makefile names the catcher by its two ports and derives nothing"
fi

# 5. The inbox the journeys read is the catcher the run probed, host spelled the same way in both.
#    The probe above asks PLATFORMKIT_E2E_MAILPIT_URL and the journeys ask PLATFORMKIT_E2E_MAIL_URL; a
#    derivation that spells the second one from a port alone reaches a loopback the job never named, and
#    then the specs that read a mailbox fail beside the ones that do not: the application mailed through
#    the named catcher, and the journeys asked a server nobody dialled. The derivation is taken from the
#    script's own text and executed here, in an environment with no PLATFORMKIT_ name set, so this asks
#    the code rather than a copy of it.
derivation="$(awk '/^mail_host=/,/^mail_url=/' "$root/scripts/e2e.sh")"
if [ -z "$derivation" ]; then
	echo "FAIL: scripts/e2e.sh no longer derives the catcher's two addresses in the block this case reads"
	failures=$((failures + 1))
else
	# resolves <name=value>... — prints the catcher's API address and the inbox address this run would
	# hand to the browser, with every mail name the caller did not pass cleared so none leaks from here.
	resolves() {
		local script="$derivation
printf '%s %s' \"\$mailpit_url\" \"\$mail_url\""
		env -u PLATFORMKIT_E2E_MAIL_HOST -u PLATFORMKIT_E2E_MAIL_PORT \
			-u PLATFORMKIT_E2E_MAILPIT_URL -u PLATFORMKIT_E2E_MAIL_URL \
			-u PLATFORMKIT_MAILPIT_PORT -u PLATFORMKIT_MAILPIT_SMTP_PORT -u PLATFORMKIT_MAIL_UI_PORT \
			"$@" bash -c "$script"
	}

	# expect_addresses <case> <wanted API> <wanted inbox> [<name=value> …]
	expect_addresses() {
		local name="$1" want_api="$2" want_inbox="$3"; shift 3
		local got api inbox
		got="$(resolves "$@")" || return
		read -r api inbox <<<"$got"
		if [ "$api" != "$want_api" ] || [ "$inbox" != "$want_inbox" ]; then
			echo "FAIL: $name: got API $api and inbox $inbox, wanted $want_api and $want_inbox"
			failures=$((failures + 1))
			return
		fi
		echo "ok   $name"
	}

	expect_addresses "a run that names nothing reaches the catcher on localhost in both spellings" \
		"http://localhost:8025" "http://localhost:8025"
	expect_addresses "a catcher named by host is read at that host by the journeys too" \
		"http://mailpit:8025" "http://mailpit:8025" PLATFORMKIT_E2E_MAIL_HOST=mailpit
	expect_addresses "the stack's API port reaches both halves" \
		"http://localhost:43105" "http://localhost:43105" PLATFORMKIT_MAILPIT_PORT=43105
	expect_addresses "an explicit inbox address wins over the derived one" \
		"http://mailpit:8025" "http://inbox.invalid:9025" \
		PLATFORMKIT_E2E_MAILPIT_URL=http://mailpit:8025 PLATFORMKIT_E2E_MAIL_URL=http://inbox.invalid:9025
fi

if [ "$failures" -ne 0 ]; then
	echo "e2e mail address: $failures case(s) failed" >&2
	exit 1
fi
echo "e2e mail address: the catcher is named by the stack's ports, and by an explicit URL when one is given"
