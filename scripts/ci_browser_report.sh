#!/usr/bin/env bash
# Say what actually killed the browser, and say it while the browser is still there to be seen.
#
# Usage:
#   scripts/ci_browser_report.sh <label>                     — the account, taken once
#   scripts/ci_browser_report.sh --watch <label> [interval]  — one line every interval, until killed
#   scripts/ci_browser_report.sh run <label> <command...>    — watch a suite, report if it fails
#
# Task T-0219 read the logs of runs 219, 225, 235, 239 and 271. Five of them report
# `locator.click: Target crashed` or `locator.screenshot: Protocol error
# (Page.captureScreenshot): Unable to capture screenshot`, and what the reader sees
# is a five-second wait that failed — a missing control. What had actually happened
# is on the host and nowhere in the case: Chromium was reclaimed. The two facts look
# identical in the log and demand opposite fixes, and nothing in the tree could
# tell them apart.
#
# The first form was the delivery's first shape, and it is the wrong one for the
# thing it is looking for: called after `npm run test:browser` returns, the suite's
# browsers have been torn down by the suite, so "browser processes" prints nothing
# almost every time, and a run the job limit reclaims prints nothing at all. The
# host's account has to be taken *during* the run, which is what `--watch` does and
# what `run` arranges: the samples stream out as they are taken, so a suite that is
# killed mid-flight leaves its memory and its browser count behind in the log anyway,
# and the failing case's own assertion is read beside a curve rather than beside a
# post-mortem. `run` changes no verdict: the wrapped suite's exit code is still the
# job's answer, and the full account is printed at the end of a failed run too.
set -uo pipefail

# The kernel's memory verdict, from the files and commands that hold it. Read twice:
# once per sample and once in the closing report, because which one is available
# depends on whether the job is privileged.
kernel_memory() {
	if [ -r /sys/fs/cgroup/memory.events ]; then
		cat /sys/fs/cgroup/memory.events 2>/dev/null || true
		echo "--- cgroup limit and usage ---"
		cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.current 2>/dev/null || true
	fi
	if [ -r /sys/fs/cgroup/memory/memory.failcnt ]; then
		echo "--- cgroup v1 memory.failcnt ---"
		cat /sys/fs/cgroup/memory/memory.failcnt 2>/dev/null || true
	fi
}

# One line, so a run of a hundred samples is a curve a reader can scan and a script
# can grep. `browsers=` counts the processes the suite owns: if it goes to zero while
# the suite is still running, the answer to "why did the click fail" is already there.
sample_line() {
	local label="$1" elapsed="$2" browsers memory oom
	browsers="$(ps -eo args 2>/dev/null | grep -cE '[c]hrom|[p]laywright' || true)"
	memory="n/a"
	if [ -r /sys/fs/cgroup/memory.current ] && [ -r /sys/fs/cgroup/memory.max ]; then
		memory="$(cat /sys/fs/cgroup/memory.current 2>/dev/null)/$(cat /sys/fs/cgroup/memory.max 2>/dev/null)"
	fi
	oom="-"
	if [ -r /sys/fs/cgroup/memory.events ]; then
		oom="$(awk '/^oom /{print $2}' /sys/fs/cgroup/memory.events 2>/dev/null)"
		oom="${oom:--}"
	fi
	echo "$label watch +${elapsed}s browsers=$browsers memory=$memory oom=$oom"
}

# The full account: what the kernel said, what the cgroup says, which browsers were
# alive, and what this job's own ceiling was.
full_report() {
	local label="$1"
	echo "::group::${label} death report — the host's account"
	if command -v dmesg >/dev/null 2>&1; then
		echo "--- the kernel's memory verdict ---"
		dmesg -T 2>&1 | grep -iE 'oom|out of memory|killed process|memory: cgroup' | tail -n 20 || echo "(no OOM line in the kernel ring)"
		echo "--- kernel tail ---"
		dmesg -T 2>&1 | tail -n 40 | cut -c 1-160 || echo "(dmesg refused in this job)"
	fi
	kernel_memory
	echo "--- browser processes ---"
	ps -eo pid,ppid,rss,vsz,etime,args | grep -iE 'chrom|playwright' | grep -v grep | cut -c 1-200 || echo "(no browser process is alive now)"
	echo "--- editor containers on this job's network ---"
	docker ps --format '{{.ID}} {{.Image}} {{.Status}} {{.Names}}' 2>/dev/null || echo "(docker not readable)"
	echo "::endgroup::"
}

# The samples, until something stops us. One second ticks so a TERM lands inside a
# second rather than at the end of an interval; the interval is how often a line goes out.
watch_loop() {
	local label="$1" interval="$2" elapsed=0 tick
	trap 'exit 0' TERM INT
	while :; do
		sample_line "$label" "$elapsed"
		tick=0
		while [ "$tick" -lt "$interval" ]; do
			sleep 1 || exit 0
			tick=$((tick + 1))
			elapsed=$((elapsed + 1))
		done
	done
}

case "${1:-}" in
	--watch)
		shift
		label="${1:-browser}"
		interval="${2:-15}"
		case "$interval" in (*[!0-9]* | '') interval=15 ;; esac
		watch_loop "$label" "$interval"
		;;
	run)
		shift
		label="${1:-}"
		[ -n "$label" ] || { echo "usage: $0 run <label> <command...>" >&2; exit 2; }
		shift
		if [ "$#" = 0 ]; then
			echo "usage: $0 run <label> <command...> — a suite to run is not optional" >&2
			exit 2
		fi
		watcher=''
		stop_watcher() {
			if [ -n "$watcher" ]; then
				kill "$watcher" 2>/dev/null || true
				wait "$watcher" 2>/dev/null || true
				watcher=''
			fi
		}
		# The watcher's own stdout is the job's: the samples are written as they are
		# taken, so a step the job limit takes down leaves them in the log regardless.
		watch_loop "$label" "${CI_BROWSER_WATCH_INTERVAL:-30}" & watcher=$!
		trap 'stop_watcher' EXIT
		"$@"
		code=$?
		stop_watcher
		if [ "$code" != 0 ]; then
			echo "--- ${label}: the samples above were taken while the suite ran; the account below is the host's, taken at its exit ---"
			full_report "$label"
		fi
		exit "$code"
		;;
	'')
		echo "usage: $0 <label> | --watch <label> [interval] | run <label> <command...>" >&2
		exit 2
		;;
	*)
		full_report "${1:-browser}"
		;;
esac
