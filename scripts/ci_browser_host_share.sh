#!/usr/bin/env bash
# A browser suite starts when the host can give it a core, and prints the numbers it waited through.
#
# Why this file exists. Chromium dies on these runners, and the case that loses it reports a browser
# death instead of an assertion: `locator.evaluate: Target crashed`, `page.evaluate: Target crashed`,
# `page.screenshot: Protocol error (Page.captureScreenshot): Unable to capture screenshot`. Every one
# of those refusals this program has recorded belongs to a browser step of
# .gitea/workflows/ci.yml, across five different test files, and the same suite passes here every
# time it is asked: `real EmptyState keeps enlarged copy and its keyboard action usable at 320px`
# measures 1.7 s on this checkout and died 4.7 s into its own run at job 64116.
#
# What the deaths have in common is that none of them is about this tree. A runner answering
# `pkit-ci-linux-amd64` is one of three that share one machine, so a Chromium started here can wait
# on a core for seconds at a time and a renderer can die before the page it never reached has asked
# one question — job 51505 printed load average 24.62 one minute before such a death and 37.26
# thirty seconds after it, with 23 GB still available. Two of the five are shared-memory shaped, a
# screenshot that could not be taken, and Docker gives a job container 64 MB of /dev/shm unless a
# step says otherwise, which is where Chromium puts a renderer's frame buffers. Job 64116 died with
# the sibling job's sampler printing 22 GB free and load 9, so the host was neither out of memory
# nor queue-starved in the minute it died, and the one number that could have indicted the pool that
# death happened in belongs to a step that printed none.
#
# So a browser step calls `wait` before its first Chromium and wraps its suite in `watch`. A suite
# that starts on a host with a free share answers about this tree; one that starts at load 37 answers
# about the host. Neither command fails a job, and neither retries, skips, narrows or reorders a
# test: what a suite that started on a quiet host reports still has to pass.
#
# Usage:
#   ci_browser_host_share.sh wait [MAX_WAIT_SECONDS] [LOADAVG_FILE]
#       Return once the 1-minute and 5-minute load averages are at most one per core, printing each
#       wait. The second term is what makes the first one mean something: a queue that has been over
#       one runnable task per core for five minutes is still being worked off whatever the last
#       minute looked like, and a renderer cannot outwait it. After
#       MAX_WAIT_SECONDS (default 720) it returns 0 anyway and says so: refusing would report no
#       verdict in place of the one it distrusts, and the queue belongs to nobody here. Reads
#       LOADAVG_FILE (default /proc/loadavg) so a case can ask both answers of a file.
#   ci_browser_host_share.sh report [MEMINFO_FILE]
#       Print the host's available memory and load average, this container's own cgroup ceiling and
#       failure counter where the host exposes one, and the shared-memory pool. Each unreadable
#       source prints a sentence rather than silence: a sampler that printed nothing and a host with
#       nothing to say look the same to whoever reads the log next. The container's memory accounting
#       answers its question in words either way — a readable host prints its counters under a line
#       naming the ceiling it read them from, one that exposes none says so — so the ceiling is asked
#       of every sample and the answer never depends on which layout the host happens to run.
#   ci_browser_host_share.sh watch [INTERVAL_SECONDS] COMMAND [ARG...]
#       Run COMMAND, printing `report` once before it starts and then every INTERVAL_SECONDS (default
#       30) while it runs, and return COMMAND's own status. The sample before COMMAND starts is the
#       step's own rather than the sampler's: a suite answered before the scheduler gives the sampler
#       its turn would otherwise leave a run watched by a sampler that never spoke, which is the
#       silence this file refuses everywhere else. The highest /dev/shm usage seen is printed at the
#       end, because a pool that filled and emptied between two samples would otherwise leave no trace.
set -euo pipefail

mem_report() { # the host's own memory, in MB available
    local meminfo="${1:-/proc/meminfo}"
    if [ -r "$meminfo" ]; then
        awk '/MemAvailable/{printf "runner MemAvailable %d MB\n", $2/1024}' "$meminfo"
    else
        printf 'runner reports no readable %s\n' "$meminfo"
    fi
}

load_report() {
    if [ -r /proc/loadavg ]; then
        cut -d' ' -f1-3 /proc/loadavg | sed 's/^/runner load average /'
    else
        echo 'runner reports no readable /proc/loadavg'
    fi
}

cgroup_report() { # what this container answers to, in its cgroup's own words
    # `MemAvailable` is the host's and says nothing about the ceiling this container answers to, so a
    # run whose own cgroup was out while the host had 23 GB free would print a number that cleared
    # memory. `memory.events` carries `oom_kill`, the only record of that death that survives it. v1
    # and v2 paths are both asked, because the image does not choose the host's layout.
    #
    # The counter lines are headed by a line naming the ceiling, because the ceiling is what this block
    # exists to answer: where a counter was read the numbers are the answer, and where none was the
    # sentence is. Both open with the words `memory ceiling`, so the sample itself says which of the
    # two a reader is looking at on whichever layout the host happens to run.
    local seen=0 file
    for file in /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory/memory.limit_in_bytes \
                /sys/fs/cgroup/memory.current /sys/fs/cgroup/memory/memory.usage_in_bytes \
                /sys/fs/cgroup/memory.events /sys/fs/cgroup/memory/memory.failcnt; do
        if [ -r "$file" ]; then
            if [ "$seen" = 0 ]; then
                printf 'job container memory ceiling, read from its own cgroup:\n'
            fi
            printf 'job container %s %s\n' "${file##*/cgroup/}" "$(tr '\n' ' ' < "$file" | sed 's/ $//')"
            seen=1
        fi
    done
    if [ "$seen" = 0 ]; then echo 'job container reports no readable memory ceiling'; fi
}

shm_line_kb() { # the buffer pool a renderer dies in: total KB, used KB, percent full; -1 when unreadable
    df -Pk /dev/shm 2>/dev/null | awk 'NR == 2 && $3 ~ /^[0-9]+$/ { print $2, $3, $5; found = 1 } END { if (!found) print "-1 -1 -1" }'
}

shm_used_kb() {
    shm_line_kb | awk '{ print $2 }'
}

shm_report() {
    local total used pct
    read -r total used pct <<EOF
$(shm_line_kb)
EOF
    if [ "$used" = "-1" ]; then
        echo 'job container reports no readable /dev/shm'
    else
        printf 'job container /dev/shm %s MB used of %s MB (%s full)\n' "$(( used / 1024 ))" "$(( total / 1024 ))" "$pct"
    fi
}

report() { # one sample of everything a death will be attributed to
    mem_report "${1:-/proc/meminfo}"
    load_report
    cgroup_report
    shm_report
}

peak_of() { # <samples file> the fullest the pool ever looked, which is the number a death is named by
    local peak count
    read -r peak count <<EOF
$(awk 'BEGIN { m = -1; n = 0 } { n++; if ($1 > m) m = $1 } END { printf "%d %d", m, n }' "$1" 2>/dev/null || echo '-1 0')
EOF
    if [ "${count:-0}" = 0 ]; then
        echo 'browser suite: no /dev/shm sample was taken, so its peak is unknown'
    elif [ "$peak" -lt 0 ]; then
        echo 'browser suite: /dev/shm was never readable, so its peak is unknown'
    else
        printf 'browser suite: /dev/shm peaked at %s MB used\n' "$(( peak / 1024 ))"
    fi
}

wait_for_a_share() { # <max-seconds> <loadavg-file> <poll-seconds>
    local bound="$1" loadavg="$2" poll="$3" waited=0 cores reading
    cores=$(nproc)
    while :; do
        if [ ! -r "$loadavg" ]; then
            echo "browser suite: no readable load average at $loadavg, so the host cannot be asked — starting"
            return 0
        fi
        reading=$(cut -d' ' -f1-3 "$loadavg")
        # Both terms, not the first alone. Gate 10 opened one suite on `load 12.98 16.23 19.83
        # across 16 cores` — the last minute under one task per core, the machine's last five and
        # fifteen over it — and the page it went on to lose died at load 21.74, three samples of the
        # suite's own sampler later, having never been over 16.3 in between (job 80264, 5b0dc4c0).
        # A dipped first term on a queued host is the host, not a free share.
        if awk -v cores="$cores" '{ exit ($1 <= cores && $2 <= cores) ? 0 : 1 }' "$loadavg"; then
            if [ "$waited" = 0 ]; then
                echo "browser suite: the host has a free share — load $reading across ${cores} cores"
            else
                echo "browser suite: the host has a free share after ${waited}s — load $reading across ${cores} cores"
            fi
            return 0
        fi
        if [ "$waited" -ge "$bound" ]; then
            echo "browser suite: host still has no free share after ${waited}s — load $reading across ${cores} cores; starting anyway"
            return 0
        fi
        echo "browser suite: waiting for a free core — load $reading across ${cores} cores, waited ${waited}s"
        sleep "$poll"
        waited=$((waited + poll))
    done
}

watch_suite() { # <interval> <command...>: the suite in the foreground, one sampler behind it
    local interval="$1" samples sampler rc
    shift
    samples=$(mktemp)
    # The first sample is taken here, in the step's own process, before COMMAND is started. Left to
    # the loop below it belongs to a fork the scheduler may not run before a quick suite is over and
    # this step has killed it — measured 24 refusals of 960 asks on a host at load 37, each printing a
    # peak of unknown for a suite that ran. Sampling first also names the baseline: the host and the
    # pool as the suite found them, which is what a death is compared against.
    report
    shm_used_kb >> "$samples"
    ( while :; do
        # The wait answers to /dev/null: the sampler's reports belong to the step's log, but its
        # sleeps do not. A sampler killed between two reports otherwise holds the step's output open
        # for the rest of its interval, which is a step that finished its suite and then waits.
        sleep "$interval" >/dev/null 2>&1
        report
        shm_used_kb >> "$samples"
      done ) &
    sampler=$!
    # The sampler belongs to this step and dies with it: a cancelled step would otherwise leave a
    # loop printing into a finished job's log, and the peak below would never be printed at all.
    trap 'kill "$sampler" 2>/dev/null || true; rm -f "$samples"; exit 130' INT TERM
    rc=0
    "$@" || rc=$?
    kill "$sampler" 2>/dev/null || true
    wait "$sampler" 2>/dev/null || true
    peak_of "$samples"
    rm -f "$samples"
    return "$rc"
}

case "${1:-}" in
    wait) wait_for_a_share "${2:-720}" "${3:-/proc/loadavg}" "${4:-15}" ;;
    report) report "${2:-/proc/meminfo}" ;;
    watch)
        interval="$2"
        shift 2
        if [ "$#" = 0 ]; then
            echo 'watch: no command to watch' >&2
            exit 2
        fi
        watch_suite "$interval" "$@"
        ;;
    *)
        echo 'usage: ci_browser_host_share.sh wait [MAX_WAIT_SECONDS] [LOADAVG_FILE] | report [MEMINFO_FILE] | watch [INTERVAL_SECONDS] COMMAND [ARG...]' >&2
        exit 2
        ;;
esac
