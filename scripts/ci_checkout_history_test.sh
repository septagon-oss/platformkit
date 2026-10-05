#!/usr/bin/env bash
# Which history each CI job fetches, and which of its steps reads it.
#
# `Run actions/checkout@…` is the line the forge reports when a job's own ceiling expires inside the
# fetch: that fetch carries no deadline of its own, so it is still running when the minutes run out,
# every step that never started is reported skipped, and the one step that was open reads as the
# failure. Head b6f1e93 was refused on exactly that line — `failed step: Run actions/checkout@11d5960…`
# with `##[error]context deadline exceeded` written into the job's log at 22:15:51 inside
# `Fetching the repository`, 45 minutes after the design job began. What that fetch had been asked
# for was `fetch-depth: 0`: every branch and every tag in the repository — 128 branches and 199 tags,
# including the `v1.1.2-proof.*` tag every delivery leaves behind — for a job whose four steps
# install, audit, build the Go programs its tests embed and drive Chromium. The same action, in that
# same log, fetched the one ref its post step needed in 26 seconds. The fetch is not large (14 MB;
# 32 s from a workstation while nothing else is on the forge); it is one network operation with no
# bound, and a job that reads none of it has no reason to spend its ceiling waiting for it.
#
# So both directions are checked against the tree rather than written down as prose. A job whose steps
# read no git object may not ask for the repository's history, because asking is what lets one slow
# fetch eat the job. A job whose steps do read history may stop asking only by removing the reader
# too: a narrowed fetch in front of a step that still walks `base..HEAD` would read the budget ratchet
# green over commits it never fetched, which is the quiet form of the same failure.
#
# Each case below names the file that decides it. `make mobile-e2e` is one line calling one script, so
# the journey's scan is that script; the rest of its steps are apt, docker and adb, which read no
# repository object. `check` reads history through two paths and is content with the one case that
# names the wider of them.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
failures=0

# depth_of WORKFLOW JOB — the fetch-depth the checkout step of JOB asks for, or empty. A workflow's
# jobs are the two-space keys under `jobs:`, and the first fetch-depth inside that block is checkout's.
depth_of() {
	awk -v job="$2" '
		$0 ~ "^  " job ":[[:space:]]*$" { inside = 1; next }
		inside && $0 ~ "^  [^[:space:]#]" { inside = 0 }
		inside && $0 ~ /^[[:space:]]+fetch-depth:/ { sub(/^[^:]*:[[:space:]]*/, ""); print; exit }
	' "$1"
}

# line_matching PATH REGEX — print the first matching line, or return non-zero when there is none.
line_matching() {
	local hit
	hit="$(grep -nE "$2" "$1" 2>/dev/null | head -1)" || hit=''
	[ -n "$hit" ] || return 1
	printf '%s' "$hit"
}

while IFS='|' read -r workflow job how source regex; do
	[ -n "$job" ] || continue
	path="$root/.gitea/workflows/$workflow.yml"
	depth="$(depth_of "$path" "$job")"
	if [ -z "$depth" ]; then
		echo "FAIL: $workflow names no fetch-depth for job $job"
		failures=$((failures + 1))
		continue
	fi
	if [ "$how" = reads ]; then
		if hit="$(line_matching "$root/$source" "$regex")"; then
			if [ "$depth" = 0 ]; then
				echo "ok   $job fetches every commit; $source line ${hit%%:*} reads them"
			else
				echo "FAIL: $job fetches depth $depth, but $source line ${hit%%:*} walks history (/$regex/): a commit past the shallow edge would go unseen"
				failures=$((failures + 1))
			fi
		else
			echo "FAIL: $job fetches depth $depth while no line of $source matches /$regex/ — nothing reads that history any more, and the fetch is what eats the job's minutes"
			failures=$((failures + 1))
		fi
	else
		if hit="$(grep -rnwi --exclude-dir=node_modules -- "$regex" "$root/$source" 2>/dev/null | head -1)"; then
			echo "FAIL: every step of $job is expected to read no git object, and $hit names one; deepen its checkout or remove the read"
			failures=$((failures + 1))
		elif [ "$depth" = 1 ]; then
			echo "ok   $job fetches one commit; no step of it reads a git object ($source names none)"
		else
			echo "FAIL: $job fetches depth $depth, but no step of it reads a git object ($source names none) — fetch-depth: 0 is the shape that was still fetching when the job ran out of minutes"
			failures=$((failures + 1))
		fi
	fi
done <<'CASES'
ci|check|reads|scripts/check_budget_ratchet.sh|git rev-list --no-merges
ci|design|none|tools/designexport/openpencil|git
mobile|journey|none|scripts/mobile_e2e.sh|git
public-consumption|report|reads|.gitea/workflows/public-consumption.yml|git rev-parse
CASES

[ "$failures" -eq 0 ]
