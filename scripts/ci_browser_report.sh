#!/usr/bin/env bash
# Say what actually killed the browser, before anybody reads the assertion.
#
# Usage: scripts/ci_browser_report.sh <label>
#
# Task T-0219 read the logs of runs 219, 225, 235, 239 and 271. Five of them report
# `locator.click: Target crashed` or `locator.screenshot: Protocol error
# (Page.captureScreenshot): Unable to capture screenshot`, and what the reader sees
# is a five-second wait that failed — a missing control. What had actually happened
# is on the host and nowhere in the case: Chromium was reclaimed. The two facts look
# identical in the log and demand opposite fixes, and nothing in the tree could
# tell them apart.
#
# So a browser step that fails calls this. It prints the kernel's own account of the
# memory pressure, the browser processes that existed and how big they were, and the
# container's own limit — every answer read from the thing that knows, in the job
# that just lost a browser. It never fails the job by itself: the suite's exit code
# is the verdict, and this is the evidence beside it.
set -uo pipefail

label="${1:-browser}"
echo "::group::${label} death report — the host's account, collected after the suite failed"

# The OOM killer writes the truth here when it takes a process. It is readable in a
# privileged container and refused in an unprivileged one, so both readings are
# attempted and a refusal says so rather than looking like an empty log.
if command -v dmesg >/dev/null 2>&1; then
	echo "--- the kernel's memory verdict ---"
	dmesg -T 2>&1 | grep -iE 'oom|out of memory|killed process|memory: cgroup' | tail -n 20 || echo "(no OOM line in the kernel ring)"
	echo "--- kernel tail ---"
	dmesg -T 2>&1 | tail -n 40 | cut -c 1-160 || echo "(dmesg refused in this job)"
fi
if [ -r /sys/fs/cgroup/memory.events ]; then
	echo "--- cgroup memory.events ---"
	cat /sys/fs/cgroup/memory.events 2>/dev/null || true
	echo "--- cgroup limit and usage ---"
	cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.current 2>/dev/null || true
fi
if [ -r /sys/fs/cgroup/memory/memory.failcnt ]; then
	echo "--- cgroup v1 memory.failcnt ---"
	cat /sys/fs/cgroup/memory/memory.failcnt 2>/dev/null || true
fi

echo "--- browser processes ---"
ps -eo pid,ppid,rss,vsz,etime,args | grep -iE 'chrom|playwright' | grep -v grep | cut -c 1-200 || echo "(no browser process is alive now)"

echo "--- editor containers on this job's network ---"
docker ps --format '{{.ID}} {{.Image}} {{.Status}} {{.Names}}' 2>/dev/null || echo "(docker not readable)"

echo "::endgroup::"
