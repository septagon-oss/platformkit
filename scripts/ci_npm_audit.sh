#!/usr/bin/env bash
# Run an npm command again when the registry would not answer, and once when it answered.
#
# This file stands between a quiet registry and a red pull request, for the one npm command in this
# repository whose whole job is to ask a server a question: `npm audit`. Forge run 8105, job 77227
# (2026-10-22T02:47:02Z, head 0ba67102) turned the `design` job red at `Native design conformance and
# dependency audit` with:
#
#   npm warn audit network timeout at: https://registry.npmjs.org/-/npm/v1/security/advisories/bulk
#   npm error audit endpoint returned an error
#
# after `npm ci --ignore-scripts` in the same step had already spent five minutes on that registry
# (its own line says `added 119 packages in 5m`). No advisory was reported, no version was named and
# nothing in the tree was measured: the endpoint never answered. `main` passed the same step over the
# same lockfile bytes twenty minutes earlier (run 75584, job 77138), which is the same fact npm's own
# advisory feed cannot deny. scripts/openpencil_lock_above_advisory_test.sh holds the other half — the
# versions this lock pins against the ranges npm has said it will report — and reads no feed.
#
# What gets retried is a route failure, and the bound is the point: the loop tries
# `1 + ${#npm_retry_waits}` times at most, so a registry that stays down ends the step with npm's own
# words in the log rather than as a job that ran out of its 45 minutes. What never gets retried is an
# audit that *answered* — a report naming a vulnerable version exits non-zero on the first try and
# stays refused, because the second thing this file exists to prevent is a retry loop that turns a
# real advisory into a paragraph. The finding markers below are checked before the route markers, so
# an audit that printed both cannot be talked out of its verdict.
#
# Sourcing this file defines npm_audit_waits, both marker lists and npm_audit_classify without running
# anything, which is how scripts/ci_npm_audit_test.sh asks about each failure text without a registry
# to break, and runs the loop against a stub command that prints a planted answer.
set -euo pipefail

# The waits, in seconds, before the second and third try. The measured blip is a minute of a step
# whose job holds 45; two waits outlast a feed that is rolling and a third try makes it a bound.
npm_audit_waits=(30 60)

# Cases shorten this list so the bound can be run rather than waited for; one case pays the real
# seconds, so the numbers above stay pinned by behaviour and not by a comment.
if [ -n "${PKIT_NPM_AUDIT_WAITS:-}" ]; then
	read -r -a npm_audit_waits <<<"$PKIT_NPM_AUDIT_WAITS"
fi

# What npm writes when it looked at an advisory and did not like what it saw. Lower-cased output is
# matched against these, and they outrank the route markers below.
npm_audit_findings=(
	"severity:"
	"vulnerabilities found"
	"github advisory"
	"fix available"
)

# What npm writes when the registry, rather than the lock, is the thing that failed.
npm_audit_transient=(
	"audit endpoint returned an error"
	"network timeout at"
	"err_connection_reset"
	"err_socket_timeout"
	"econnrefused"
	"econnreset"
	"etimedout"
	"socket hang up"
	"enotfound"
	"eai_again"
	"could not resolve host"
	"unable to connect"
	"unexpected eof"
	"bad gateway"
	"service unavailable"
	"gateway time-out"
	"internal server error"
)

npm_audit_classify() { # <what npm wrote> — "found", "transient" or "final"
	local lowered="${1,,}" marker
	for marker in "${npm_audit_findings[@]}"; do
		case "$lowered" in *"$marker"*) printf 'found\n'; return 0 ;; esac
	done
	for marker in "${npm_audit_transient[@]}"; do
		case "$lowered" in *"$marker"*) printf 'transient\n'; return 0 ;; esac
	done
	printf 'final\n'
}

npm_audit_main() { # <command and its arguments> — the command's own exit code, its own output
	local try=1 output="" status=0 pause plural
	while true; do
		status=0
		output="$("$@" 2>&1)" || status=$?
		if [ "$status" -eq 0 ]; then
			[ -z "$output" ] || printf '%s\n' "$output"
			if [ "$try" -gt 1 ]; then
				plural=ies
				[ "$try" -ne 2 ] || plural=y
				echo "npm: answered on try $try, after $((try - 1)) retr$plural"
			fi
			return 0
		fi
		if [ "$(npm_audit_classify "$output")" != transient ] || [ "$try" -gt "${#npm_audit_waits[@]}" ]; then
			[ -z "$output" ] || printf '%s\n' "$output" >&2
			if [ "$try" -gt 1 ]; then
				echo "npm: the command above is refused after $try tries, of the $((1 + ${#npm_audit_waits[@]})) this step allows at most" >&2
			fi
			return "$status"
		fi
		[ -z "$output" ] || printf '%s\n' "$output"
		pause="${npm_audit_waits[$((try - 1))]}"
		echo "+ try $try did not reach the registry, waiting ${pause}s: ${output##*$'\n'}" >&2
		sleep "$pause"
		try=$((try + 1))
	done
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
	npm_audit_main "$@"
fi
