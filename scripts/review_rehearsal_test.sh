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
# SAMPLE_MS. Everything the step says about a lock wait is that product, so the
# premise worth testing is whether the step's own invocation fills that file.
#
# The first version of this case ran `-o FILE` with the query and `\watch` as two
# separate -c arguments and concluded from one sample in three seconds that the step
# could measure nothing. That premise is false about this tree, and the case's
# failure text repeated it: `psql -c <query> -c '\watch 0.1'` answers the query once
# and then prints "\watch cannot be used with an empty query" (a query given to -c is
# gone from the query buffer by the time the next -c runs, and \watch repeats the
# buffer) — measured on host psql 18.6 and on psql 16.15 inside this project's own
# postgres:16 container, one usable line either way — while the step's real
# invocation, which reads the query and its \watch from one file with -f, put thirty
# usable lines in three seconds. The defect that case was written to force is fixed,
# and proved by a contended run end to end (`lock waits: 50 sample(s) of 100ms ≈
# 5000ms`, `LOCK WAIT 5000ms > 1000ms`, exit 3).
#
# So this runs the step's own line: the builder that writes the watch file is read
# out of the step and executed, which is where the \watch interval comes from — the
# step's query is replaced by a count that is never zero, because the step's filters
# on the candidate's application_name and nothing here carries it. The invocation,
# the sample file and the interval are the step's; only the query is the case's.
# scripts/check_architecture_test.sh holds the same two premises inside `make check`;
# this file is the one that asks how many of the lines are *usable*.
# ---------------------------------------------------------------------------
admin_url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
if [ -z "$admin_url" ]; then
	echo "SKIP case 2: PLATFORMKIT_TEST_ADMIN_URL is unset, and the watcher is a psql session" >&2
elif ! watcher="$(grep -m1 -F -- '-At -o "$waits" -f "$watch_sql"' "$script")" || [ -z "$watcher" ]; then
	fail "could not read the watcher's invocation out of $script; the step no longer reads its query and its \watch from one file, which is the shape that makes the sample file hold more than one line"
else
	sample_ms="$(sed -n 's/^SAMPLE_MS=\([0-9]\{1,\}\)$/\1/p' "$script")"
		sample_s="$(printf '%d.%03d' "$((sample_ms / 1000))" "$((sample_ms % 1000))")"
	builder="$(awk '/^watch_sql=/{f=1} f{print} /^\t"\$SAMPLE_S" >"\$watch_sql"$/{f=0}' "$script")"
	if [ -z "$builder" ] || [ -z "$sample_ms" ]; then
		fail "could not read the watch file's builder or SAMPLE_MS out of $script, so this case has no interval to sample at and no floor to hold the count to"
	else
		# The step's own builder, with its own SAMPLE_S, writing to a file of this case.
		{
			printf 'work=%s\nAPPNAME=review_rehearsal_test\n' "$(printf '%q' "$fixture")"
			printf 'SAMPLE_S=%s\n' "$sample_s"
			printf '%s\n' "$builder"
		} >"$fixture/build-watch.sh"
		bash "$fixture/build-watch.sh"
		interval="$(sed -n 's/^\\watch //p' "$fixture/watch.sql")"
		if [ -z "$interval" ]; then
			fail "the file the step's own builder writes holds no \\watch line, so it samples once and the lock-wait finding can never fire"
		else
			# Keep the \watch line the step wrote; the query is the case's.
			{ echo 'SELECT count(*) FROM pg_stat_activity'; sed -n '/^\\watch/p' "$fixture/watch.sql"; } >"$fixture/case.sql"
			{
				printf 'run_url=%s\nwaits=%s\nwatch_sql=%s\n' \
					"$(printf '%q' "$admin_url")" "$(printf '%q' "$fixture/waits")" "$(printf '%q' "$fixture/case.sql")"
				printf '%s\n' "$watcher"
				printf 'watcher=$!\nsleep 3\nkill "$watcher" 2>/dev/null || true\nwait "$watcher" 2>/dev/null || true\n'
			} >"$fixture/run-watch.sh"
			bash "$fixture/run-watch.sh"
			samples=$(grep -c '^[1-9]' "$fixture/waits" 2>/dev/null || true)
			# A quarter of the samples the window could hold: the step's floor is half,
			# and this case pays for psql's own connect on top of what it watches.
			floor=$((3000 / sample_ms / 4))
			if [ "${samples:-0}" -lt "$floor" ]; then
				fail "three seconds of the step's watcher invocation ($watcher) put ${samples:-0} usable sample(s) in the file it counts, and $floor is the fewest ${sample_ms}ms samples that window could hold; the reported lock waits are then always ~0ms and the LOCK WAIT finding can never fire"
			else
				echo "ok: the watcher read its query from the step's file: $samples usable samples in three seconds ($interval apart, floor $floor)"
			fi
		fi
	fi
fi

exit "$failed"
