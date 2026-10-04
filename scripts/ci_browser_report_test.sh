#!/usr/bin/env bash
# ci_browser_report_test.sh — the cases scripts/ci_browser_report.sh must pass.
#
# The report exists because five runs of one job said "the control never appeared"
# when the host had reclaimed the browser, and nothing in the log could tell the two
# apart (task T-0219, runs 219, 225, 235, 239, 271). That makes a diagnostic
# load-bearing on the failure path of the tree's slowest job — and a diagnostic that
# only runs after the suite exits sees no browser at all, because the suite closed
# them. So the contract this file rehearses is the one the name does not state: the
# samples must exist *while the thing being watched is still running*, the report must
# change no verdict, and the watcher must not outlive the suite.
#
# Nothing here launches a browser or a container. `run` takes any command, and the
# command here is a suite that prints a marker — which is exactly the shape that
# matters: a sample line has to precede the marker, or the report is a post-mortem.
# The marker is printed when a sample has been *seen*, in the file the report mirrors
# its samples into, and never on a stopwatch: the case that asks whether the watcher
# sampled a live suite must not itself be settled by which process the scheduler ran
# first (that is the defect class, and both scripts/ci_browser_samples_allow_success_
# test.py and scripts/ci_browser_late_first_sample_test.py are pins against it).
#
# `make check` runs this file beside the architecture, budget and setup rehearsals.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
report="$root/scripts/ci_browser_report.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
# `fail` counts rather than setting a flag, because `say_ok` asks whether the count moved
# since the case began: with a flag, the second failing case onwards prints `ok` beside its
# own FAIL. Seen on ci.yml run 46460 in the file beside this one, which shares the shape.
failed=0
fail() { echo "FAIL: $*" >&2; failed=$((failed + 1)); }
say_ok() { if [ "$failed" = "${start:-0}" ]; then echo "ok   $*"; fi; }
start=$failed

# The three cases that capture a run's output send it to a file rather than into a
# command substitution, and put a deadline on the run. Both halves are load-bearing.
# A substitution makes the harness wait for EOF on a pipe, and the watcher holds a
# write end of that pipe: measured on 2026-10-04, a watcher that is not stopped (the
# defect case 4 asks about) kept `out="$(… 2>&1)"` open for as long as the watcher
# lived, so the file under review hung for five minutes instead of refusing. A file
# costs nothing to a process that outlives its reader. The deadline then does what it
# looks like it does — 40 s is further out than anything here has to take (the whole
# rehearsal is ~5 s), and a run that reaches it arrives as exit 124 rather than as a
# step the job limit takes down. The watched suite sets its own, shorter ceiling on
# waiting for a sample, so the ordinary failure of a report that never samples is the
# suite's own exit code.
capture() {
	# capture <file> <command…> -> $out and $code, from the run itself
	local file="$1"
	shift
	: > "$file"
	code=0
	timeout 40 "$@" >"$file" 2>&1 || code=$?
	out="$(cat "$file")"
}

# case 1 — a suite that passes is left alone: its exit code, its own line, and no death
# report. What may sit beside that line is the watcher's own samples and nothing else.
#
# `out` is not compared to `alive`: the watcher is started before the suite and writes to
# the same descriptor, so a suite that prints immediately races its first sample and can
# lose it. Case 2 makes that ordering a requirement (a sample must land while the suite
# is alive), so requiring the watcher to be silent here contradicts it and leaves the
# pair to be settled by the scheduler — which is a case that fails on timing alone, the
# one defect class this task exists to remove. What this case can answer without a clock
# is whether anything the report invented reached a passing suite.
capture "$fixture/case1.out" env CI_BROWSER_WATCH_INTERVAL=1 bash "$report" run quiet bash -c 'echo alive'
[ "$code" = 0 ] || fail "a passing suite: exit $code, want 0"
grep -qx 'alive' <<<"$out" || fail "a passing suite lost its own output: $out"
grep -qi 'death report' <<<"$out" && fail "a passing suite got a death report"
beside="$(grep -vE -e '^alive$' -e '^quiet watch \+[0-9]+s browsers=[0-9]+ memory=[^ ]+ oom=[^ ]+$' <<<"$out" || true)"
[ -z "$beside" ] || fail "a passing suite got output beside its own and the watcher's samples: $beside"
say_ok "a passing suite keeps its exit code and prints no report"

start=$failed

# The watched suite for the two cases that ask about samples. It is not a `sleep`
# with a marker at the end: it waits for the watcher's sample to appear in the file
# the report mirrors them into (CI_BROWSER_WATCH_SAMPLE_FILE), prints its marker once
# one has landed, and stays alive until one more has. So the ordering the two cases
# check is observed rather than timed, and a host that keeps the watcher's first
# sample late still passes — the marker follows the sample, not a stopwatch. Both
# waits are bounded: a report that never samples is the failure these cases exist to
# catch, and it arrives as an exit code rather than as a hang.
cat > "$fixture/suite.sh" <<'SUITE'
#!/usr/bin/env bash
set -uo pipefail
seen() {
	local want="$1" have=0
	[ -n "${CI_BROWSER_WATCH_SAMPLE_FILE:-}" ] || return 1
	[ -f "$CI_BROWSER_WATCH_SAMPLE_FILE" ] || return 1
	have="$(wc -l < "$CI_BROWSER_WATCH_SAMPLE_FILE")" || have=0
	[ "${have:-0}" -ge "$want" ]
}
wait_for_samples() {
	local want="$1" ticks=0
	while ! seen "$want"; do
		sleep 0.1 || return 1
		ticks=$((ticks + 1))
		if [ "$ticks" -gt "${CI_BROWSER_SUITE_CEILING_TICKS:-100}" ]; then return 1; fi
	done
	return 0
}
wait_for_samples 1 || { echo "SUITE GAVE UP WAITING FOR THE WATCHER'S FIRST SAMPLE"; exit 4; }
case "${1:-}" in
	# Print the marker once a sample has landed, and hold on until a second one has:
	# what sits either side of MARKER is then something the suite saw, not something
	# the scheduler decided.
	mark)
		echo MARKER
		wait_for_samples 2 || { echo "SUITE GAVE UP WAITING FOR A SECOND SAMPLE"; exit 5; }
		exit 0
		;;
	# A suite that fails as soon as it has been seen: its own code, and the account
	# taken at its exit, with the sample that proved it was alive above them.
	fail)
		exit 3
		;;
esac
echo "SUITE: no such mode: ${1:-}"
exit 2
SUITE

# case 2 — the sample is taken while the suite is alive: the line must precede the
# marker the suite prints once the first sample has reached it, and at least one more
# must follow it, because the suite holds on until it has seen a second one. Nothing
# here is a duration: 88bb015 made the passing suite's verdict not depend on who won
# the scheduler, and a neighbour of this case (scripts/
# ci_browser_late_first_sample_test.py) showed the same defect left in this one — the
# marker used to sit after a `sleep 1`, so a watcher that sampled the suite two
# seconds before its end was refused for being 200 ms late out of the gate.
capture "$fixture/case2.out" env CI_BROWSER_WATCH_INTERVAL=1 \
	CI_BROWSER_WATCH_SAMPLE_FILE="$fixture/case2.samples" \
	bash "$report" run live bash "$fixture/suite.sh" mark
[ "$code" = 0 ] || fail "a passing suite under watch: exit $code, want 0 — a suite that gave up waiting for the watcher's samples means the watcher never sampled: $out"
before="$(awk '/MARKER/{exit} / watch \+[0-9]+s browsers=/{n++} END{print n+0}' <<<"$out")"
[ "$before" -ge 1 ] || fail "no sample was printed while the suite was still running, before its own marker: $out"
after="$(awk '/MARKER/{f=1; next} f && / watch \+[0-9]+s browsers=/{n++} END{print n+0}' <<<"$out")"
[ "$after" -ge 1 ] || fail "watching stopped at the marker rather than running to the suite's exit: $out"
grep -qi 'death report' <<<"$out" && fail "a passing suite got a death report at the end"
say_ok "the samples are taken while the watched suite is alive, and none after it ends"

start=$failed
# case 3 — a failing suite: the suite's own code, the account beside it, and the
# samples still there above it. The suite exits on the first sample it is shown, so
# "the samples are missing" is a fact about the watcher and not about the schedule.
capture "$fixture/case3.out" env CI_BROWSER_WATCH_INTERVAL=1 \
	CI_BROWSER_WATCH_SAMPLE_FILE="$fixture/case3.samples" \
	bash "$report" run lost bash "$fixture/suite.sh" fail
[ "$code" = 3 ] || fail "a failing suite: exit $code, want the 3 it returned"
grep -qi 'lost death report' <<<"$out" || fail "a failing suite got no death report: $out"
grep -q 'lost watch +0s browsers=' <<<"$out" || fail "a failing suite's samples are missing: $out"
grep -qi 'browser processes' <<<"$out" || fail "the report names no browser processes: $out"
# The report is the last thing printed, so the samples read as the curve that precedes it.
account_at="$(grep -n 'lost death report' <<<"$out" | head -1 | cut -d: -f1)"
sample_at="$(grep -n 'lost watch +' <<<"$out" | tail -1 | cut -d: -f1)"
[ "$sample_at" -lt "$account_at" ] || fail "the closing account came before the last sample, so a reader cannot read them in order"
say_ok "a failing suite keeps its code, and the host's account arrives beside it"

start=$failed
# case 4 — the watcher does not outlive the suite it was started for. A leftover
# `watch_loop` would keep writing lines into a step that had moved on.
#
# What is asked about is this rehearsal's own watchers, named by the run that started
# them. The count used to be `ps -eo args | grep -c ci_browser_report.sh`, which is
# every watcher on the host: this machine runs several checkouts' `make check` beside
# each other and each of them starts this same watcher, so a neighbour's watcher that
# happened to start inside the settle was a red here — the shared-state class this
# task exists to remove, in the process table
# (scripts/ci_browser_watcher_count_is_its_own_test.py).
watchers="$fixture/watchers"; : > "$watchers"
CI_BROWSER_WATCH_PID_FILE="$watchers" CI_BROWSER_WATCH_INTERVAL=1 bash "$report" run gone true >/dev/null 2>&1 || true
sleep 0.2
[ -s "$watchers" ] || fail "the report named no watcher it started, so nothing here could be asked whether it survived: $watchers is empty"
leaked=''
while read -r pid; do
	if kill -0 "$pid" 2>/dev/null; then leaked="$leaked $pid"; fi
done < "$watchers"
[ -z "$leaked" ] || fail "a watcher survived its suite: pids$leaked"
say_ok "no watcher is left running once the suite it watched has exited"

start=$failed
# case 5 — `run` with nothing to run is a usage error, before a watcher starts.
code=0; out="$(bash "$report" run empty 2>&1)" || code=$?
[ "$code" = 2 ] || fail "run with no command: exit $code, want 2"
grep -q 'not optional' <<<"$out" || fail "run with no command did not say what is missing: $out"
code=0; out="$(bash "$report" 2>&1)" || code=$?
[ "$code" = 2 ] || fail "no arguments at all: exit $code, want 2"
grep -q 'usage:' <<<"$out" || fail "no arguments at all printed no usage: $out"
say_ok "run with no suite, and no arguments at all, are refused with the usage"

start=$failed
# case 6 — the one-shot form the workflow also calls directly still answers, with the
# memory files named and the group markers that keep a long report out of the way.
code=0; out="$(bash "$report" preview 2>&1)" || code=$?
[ "$code" = 0 ] || fail "the one-shot form: exit $code, want 0 whatever the host allows"
grep -q '::group::preview death report' <<<"$out" || fail "the one-shot form opens no group: $out"
grep -q '::endgroup::' <<<"$out" || fail "the one-shot form never closes its group: $out"
say_ok "the one-shot report still prints the host's account between its markers"

if [ "$failed" != 0 ]; then
	echo "ci browser report: $failed case(s) failed" >&2
	exit 1
fi
echo "ci browser report: every case holds"
