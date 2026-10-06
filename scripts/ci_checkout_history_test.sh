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
# repository object. `go-checks` reads history through two paths and is content with the one case that
# names the wider of them.
#
# `walks` does not mean `fetch the mirror`. It means the job needs a run of commits, and the two shapes
# that deliver one are a window of its own branch and every ref the forge holds. A window is the
# narrower of them and the only one head 073c88f4 got past: its `go-checks` checkout sat open 1123s at
# `fetch-depth: 0`, the job was cut at its own 19-minute ceiling with every real step reported skipped,
# and `--depth=1` checkouts of the same commit in the same run returned in 4.6s and 17.7s. So a `walks`
# row names a finite window, and the price of a window — that it can stop above the history — is paid by
# the walk itself refusing it, which the row's last column pins in the file that walks. A job whose steps
# read no git object may not ask for the repository's history, because asking is what lets one slow fetch
# eat the job. A job whose steps do read history may stop asking only by removing the reader too.
#
# `deep` is the row for a job that reads something no window carries: `public-consumption`'s report
# resolves `v1.1.0` and a pseudo-version's suffix commit, so it needs `+refs/tags/*` — and it is the one
# workflow in this tree that runs on a schedule rather than on somebody's pull request.
#
# `race-and-vuln` is the third kind, and head b7ead19b is what moved it. It was classed with the readers
# because `./modules/admin/...` contains a stamp case that runs git, and the fetch that class justified
# was the whole repository: this job's checkout ran 1129s, took `##[error]context deadline exceeded`
# inside `Fetching the repository`, and reported every real step skipped — the run's other runner was
# taken the same way by the same fetch, so nothing else in the run even got to start. What that case asks
# of git is `git rev-parse HEAD`: the checked-out commit, one object, which a one-commit fetch delivers
# as well as a whole-mirror fetch does. Reading one object is not the same claim as walking history, and
# the two classes below say which of the two a job may ask for. A job classed `reads` must still fetch
# shallow, and it loses the class the moment a history walk reaches the file its steps run — so this is a
# narrowing of what a job may fetch, not an excuse for one.
#
# Why `reads` is worth a class of its own: at `fetch-depth: 0` actions/checkout hands git
# `+refs/heads/*` and `+refs/tags/*` unconditionally (v4.4.0 src/ref-helper.ts:69, where
# getRefSpecForAllHistory ignores the `fetch-tags` input), so the deep fetch is every branch and every tag
# the forge holds — 247 heads and 250 tags when this was written, the tags one `v1.1.2-proof.<date>.<sha>`
# per delivery beside the release line. Above depth zero the same action fetches one refspec and passes
# `--no-tags` (src/git-command-manager.ts:281), so a number is what narrows the shape: a window of one
# branch, which is all a walk of `base..HEAD` can ever read. `0` is then left to the one job that reads a
# ref outside its own branch, and that is the whole of the `deep` row: `git describe`, `refs/tags` and
# `git tag` appear in no line of `scripts`, `tools`, `modules`, `kit`, `apps` or the Makefile except
# `public-consumption.yml`, whose exported-surface step compares against `v1.1.0` and resolves a
# pseudo-version's suffix commit.
#
# The four rows for ci.yml are the four jobs T-0219 split the old `check` job into, and each names a
# file its own steps run: `go-checks` walks `base..HEAD` in the budget ratchet, so it fetches a window of
# its own branch wide enough for the deepest branch on the forge, and refuses one that stops above the
# base; `race-and-vuln` runs `./modules/admin/...`, whose stamp case asks `git rev-parse HEAD`, so
# it is classed with the readers rather than claimed blind; `design-editor` and `e2e` read no git
# object at all and go shallow, which is the fix head b6f1e93 bought — that head's design job was
# still fetching history nothing reads when its minutes ran out.
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

# A history walk, as opposed to a read of the one commit that happens to be checked out: the shape no
# shallow fetch can answer.
history_walk='rev-list|merge-base|git log|describe|\.\.HEAD'

# line_matching PATH REGEX — print the first matching line, or return non-zero when there is none.
line_matching() {
	local hit
	hit="$(grep -nE "$2" "$1" 2>/dev/null | head -1)" || hit=''
	[ -n "$hit" ] || return 1
	printf '%s' "$hit"
}

while IFS='|' read -r workflow job how source regex guard; do
	[ -n "$job" ] || continue
	path="$root/.gitea/workflows/$workflow.yml"
	depth="$(depth_of "$path" "$job")"
	if [ -z "$depth" ]; then
		echo "FAIL: $workflow names no fetch-depth for job $job"
		failures=$((failures + 1))
		continue
	fi
	if [ "$how" = walks ]; then
		if ! hit="$(line_matching "$root/$source" "$regex")"; then
			echo "FAIL: $job is classed as walking history, and no line of $source matches /$regex/ — name what it walks or class the job again"
			failures=$((failures + 1))
		elif [ -z "$guard" ]; then
			echo "FAIL: $job is classed as walks, so its row must name the line of $source that refuses a window stopping above the base"
			failures=$((failures + 1))
		elif ! guard_hit="$(line_matching "$root/$source" "$guard")"; then
			echo "FAIL: $job fetches a window, but $source names no refusal (/$guard/) of one that stops above the base — the walk would read over commits the checkout never fetched"
			failures=$((failures + 1))
		elif ! printf '%s' "$depth" | grep -qE '^[1-9][0-9]*$'; then
			echo "FAIL: $job walks history and fetches '$depth' — a walk needs a window of its own branch, and every ref and tag is the shape head 073c88f4 was still waiting for when its minutes ran out"
			failures=$((failures + 1))
		else
			echo "ok   $job fetches $depth commits of one branch: $source line ${hit%%:*} walks them, line ${guard_hit%%:*} refuses a window above the base"
		fi
	elif [ "$how" = deep ]; then
		if ! hit="$(line_matching "$root/$source" "$regex")"; then
			echo "FAIL: $job is classed as reading what only every ref delivers, and no line of $source matches /$regex/ — nothing needs the tags any more, so narrow its fetch"
			failures=$((failures + 1))
		elif [ "$depth" = 0 ]; then
			echo "ok   $job fetches every ref and tag; $source line ${hit%%:*} reads one"
		else
			echo "FAIL: $job fetches depth $depth, but $source line ${hit%%:*} reads a ref no window of one branch carries"
			failures=$((failures + 1))
		fi
	elif [ "$how" = reads ]; then
		if ! hit="$(line_matching "$root/$source" "$regex")"; then
			echo "FAIL: $job is classed as reading only the checked-out commit, and no line of $source matches /$regex/ — name the read or class the job again"
			failures=$((failures + 1))
		elif walk="$(line_matching "$root/$source" "$history_walk")"; then
			echo "FAIL: $job fetches depth $depth, but $source line ${walk%%:*} walks history (/$history_walk/): class it as walks and fetch every commit"
			failures=$((failures + 1))
		elif [ "$depth" = 1 ]; then
			echo "ok   $job fetches one commit; $source line ${hit%%:*} reads only the checked-out commit, which that fetch delivers"
		else
			echo "FAIL: $job reads only $source line ${hit%%:*}, one object every fetch shape delivers, and still fetches depth $depth — an unbounded fetch is what ate this job's whole ceiling at head b7ead19b"
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
ci|go-checks|walks|scripts/check_budget_ratchet.sh|git rev-list --no-merges|is-shallow-repository
ci|race-and-vuln|reads|modules/admin/internal/review_round1_build_stamp_test.go|rev-parse.*HEAD|
ci|design-editor|none|tools/designexport/openpencil|git|
ci|e2e|none|scripts/e2e.sh|git|
mobile|journey|none|scripts/mobile_e2e.sh|git|
public-consumption|report|deep|.gitea/workflows/public-consumption.yml|rev-parse --verify|
CASES

[ "$failures" -eq 0 ]
