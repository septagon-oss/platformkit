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
# command here is a sleep that prints a marker — which is exactly the shape that
# matters: a sample line has to precede the marker, or the report is a post-mortem.
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

# case 1 — a suite that passes is left alone: its exit code, and no death report.
code=0; out="$(CI_BROWSER_WATCH_INTERVAL=1 bash "$report" run quiet bash -c 'echo alive' 2>&1)" || code=$?
[ "$code" = 0 ] || fail "a passing suite: exit $code, want 0"
[ "$out" = "alive" ] || fail "a passing suite printed something beside its own output: $out"
grep -qi 'death report' <<<"$out" && fail "a passing suite got a death report"
say_ok "a passing suite keeps its exit code and prints no report"

start=$failed
# case 2 — the sample is taken while the suite is alive: the line must precede the
# marker the suite prints one second in, not after it.
code=0; out="$(CI_BROWSER_WATCH_INTERVAL=1 bash "$report" run live bash -c 'sleep 1; echo MARKER; sleep 2' 2>&1)" || code=$?
[ "$code" = 0 ] || fail "a passing suite under watch: exit $code, want 0"
before="$(awk '/MARKER/{exit} / watch \+[0-9]+s browsers=/{n++} END{print n+0}' <<<"$out")"
[ "$before" -ge 1 ] || fail "no sample was printed while the suite was still running, before its own marker: $out"
after="$(awk '/MARKER/{f=1; next} f && / watch \+[0-9]+s browsers=/{n++} END{print n+0}' <<<"$out")"
[ "$after" -ge 1 ] || fail "watching stopped at the marker rather than running to the suite's exit: $out"
grep -qi 'death report' <<<"$out" && fail "a passing suite got a death report at the end"
say_ok "the samples are taken while the watched suite is alive, and none after it ends"

start=$failed
# case 3 — a failing suite: the suite's own code, the account beside it, and the
# samples still there above it.
code=0; out="$(CI_BROWSER_WATCH_INTERVAL=1 bash "$report" run lost bash -c 'sleep 1; exit 3' 2>&1)" || code=$?
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
before_procs="$(ps -eo args | grep -c '[c]i_browser_report.sh' || true)"
CI_BROWSER_WATCH_INTERVAL=1 bash "$report" run gone true >/dev/null 2>&1 || true
sleep 0.2
after_procs="$(ps -eo args | grep -c '[c]i_browser_report.sh' || true)"
[ "$after_procs" -le "$before_procs" ] || fail "a watcher survived its suite: $before_procs before, $after_procs after"
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
