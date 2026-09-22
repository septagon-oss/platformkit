#!/usr/bin/env bash
# review_rehearsal_test.sh — the review's cases for the rehearsal step.
#
# A separate file, and not a case inside scripts/check_architecture_test.sh,
# because that suite is the delivery's own gate and aborts at its first failure: a
# reviewer's failing case there would hide every case after it. Nothing in the
# repository runs this file; `bash scripts/review_rehearsal_test.sh` does. It needs
# PLATFORMKIT_TEST_ADMIN_URL for the watcher case, the same owner connection the
# step itself insists on.
#
# Both cases are about the two things scripts/rehearse_migrations.sh exists to
# report — a contended file, and a lock wait — and both run the step's own
# programs rather than restatements of them.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$root/scripts/rehearse_migrations.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
failed=0
fail() {
	echo "FAIL: $*" >&2
	failed=1
}

# ---------------------------------------------------------------------------
# Case 1 — the CONTENDED branch.
#
# migrations/README.md: "exits 0 (applied inside both budgets), 1 (a migration
# failed, rule refusals included), 2 (it could not run) or 3 (a budget was overrun
# or a file came back contended). A contended file is a finding and never a pass".
# The step decides that with one grep over the candidate's log. This runs that
# grep — read out of the step, not restated — over a log line the runner really
# wrote, on the run that produced the finding:
#
#   bash scripts/rehearse_migrations.sh --base-ref 75b04d1^ --seed /tmp/wide-seed.sql
#   with a session holding ACCESS SHARE on the table the candidate's file was
#   rewriting, so the file waited out its 5s lock_timeout:
#
#   platformkit: db: migrate: platformkit/000099_review_probe.up.sql: db: migration
#     is contended: … (lock_timeout 5s, statement_timeout 0): ERROR: canceling
#     statement due to lock timeout (SQLSTATE 55P03)
#   rehearse: lock waits: 0 sample(s) of 100ms ≈ 0ms
#   rehearse: failed: 0 finding(s); exit 1
# ---------------------------------------------------------------------------
cat >"$fixture/run.log" <<'LOG'
{"time":"2026-09-22T13:13:44+01:00","level":"INFO","msg":"db: applied migration","owner":"platformkit","version":21,"name":"000021_limits.up.sql","phase":"expand","duration_ms":3}
platformkit: db: migrate: platformkit/000099_review_probe.up.sql: db: migration is contended: it could not take a lock within its budget; nothing this run had not already applied was applied, and it may be run again (lock_timeout 5s, statement_timeout 0): ERROR: canceling statement due to lock timeout (SQLSTATE 55P03)
LOG
program="$(sed -n "s/^contended=\\\$(grep -o '\\(.*\\)'.*/\\1/p" "$script")"
if [ -z "$program" ]; then
	fail "could not read the contention program out of $script; the step no longer greps for a contended file at all, and this case has to be rewritten to say what it does instead"
elif [ -z "$(grep -o "$program" "$fixture/run.log" | tail -1 || true)" ]; then
	fail "the step's contention program '$program' matches nothing in a log the runner really wrote, so CONTENDED never prints, the finding count stays 0 and a contended release exits 1 (\"a migration failed\") rather than 3. A POSIX bracket expression reads [^\\n] as \"not a backslash and not the letter n\", so the pattern cannot span the n inside the very words it is meant to match: the branch is unreachable"
else
	echo "ok: the step finds a contended file in the runner's own log"
fi

# ---------------------------------------------------------------------------
# Case 2 — the lock-wait watcher.
#
# The step samples pg_stat_activity in a backgrounded psql, writes the samples to
# $waits, and turns them into its reported number with `grep -c '^[1-9]'` times
# SAMPLE_MS. This runs that same invocation shape — `-o FILE`, the query and
# `\watch` as separate -c arguments, backgrounded, then killed — for three seconds
# with a query that answers a nonzero count every time, and asks how many usable
# samples arrived. Four is what three seconds at 100ms resolves to; a file that
# holds one sample cannot measure a wait of any length, and `--max-lock-ms`
# (default 5000) can never be exceeded.
# ---------------------------------------------------------------------------
admin_url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
if [ -z "$admin_url" ]; then
	echo "SKIP case 2: PLATFORMKIT_TEST_ADMIN_URL is unset, and the watcher is a psql session" >&2
	exit 2
fi
watcher="$(grep -m1 -F -e '-At -o "$waits"' "$script" || true)"
if [ -z "$watcher" ]; then
	fail "could not read the watcher's invocation out of $script"
else
	# The step's own invocation shape, with its query replaced by a count that is
	# never zero: what is under test is whether \watch fills the sample file at
	# all, not whether pg_stat_activity has rows.
	psql "$admin_url" -At -o "$fixture/waits" \
		-c 'SELECT count(*) FROM pg_stat_activity' -c '\watch 0.1' >/dev/null 2>&1 &
	watch_pid=$!
	sleep 3
	kill "$watch_pid" 2>/dev/null || true
	wait "$watch_pid" 2>/dev/null || true
	samples=$(grep -c '^[1-9]' "$fixture/waits" 2>/dev/null || true)
	if [ "${samples:-0}" -lt 4 ]; then
		fail "three seconds of the step's own watcher invocation ($watcher …) put ${samples:-0} usable sample(s) in the file it counts; the reported lock waits are therefore always ~0ms and the LOCK WAIT finding can never fire. Reproduced end to end against a migration that really waited five seconds on a table lock (SQLSTATE 55P03): 'rehearse: lock waits: 0 sample(s) of 100ms ~ 0ms'"
	else
		echo "ok: the watcher filled its sample file: $samples samples in three seconds"
	fi
fi

exit "$failed"
