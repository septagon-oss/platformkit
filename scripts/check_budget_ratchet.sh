#!/usr/bin/env bash
# Compare reviewed ceilings with one explicit base commit:
#   check_budget_ratchet.sh BASE_COMMIT [repository]
# CI supplies the pull request base or the push's previous revision. Consumers
# call this implementation from their resolved foundation dependency.
# Requires Git and jq. An unavailable baseline fails the check.
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [[ ! "$1" =~ ^[0-9a-fA-F]{40}$ ]] || [[ "$1" =~ ^0+$ ]]; then
	echo 'budget ratchet: supply a nonzero full base commit SHA' >&2
	exit 2
fi
base="$1"
cd "${2:-.}"
if ! git cat-file -e "$base^{commit}" 2>/dev/null; then
	if ! git fetch --quiet --no-tags --depth=1 origin "$base" 2>/dev/null; then
		echo "budget ratchet: could not fetch base commit $base" >&2
		exit 2
	fi
fi
git cat-file -e "$base^{commit}"

# Missing or malformed baselines are errors, never evidence of unchanged
# ceilings. Removing a bucket or changing what it measures also needs review.
base_loc="$(git show "$base:loc-budget.json")"
head_loc="$(cat loc-budget.json)"
problems="$(jq -nr --argjson base "$base_loc" --argjson head "$head_loc" '
    $base.buckets[] as $before
    | [$head.buckets[] | select(.name == $before.name)] as $after
    | if ($after | length) != 1 then
        "loc-budget.json: bucket \($before.name) was removed or duplicated"
      elif ($before | del(.max)) != ($after[0] | del(.max)) then
        "loc-budget.json: measurement changed for \($before.name)"
      elif $after[0].max > $before.max then
        "loc-budget.json: \($before.name) raised from \($before.max) to \($after[0].max)"
      else empty end
')"
if git cat-file -e "$base:packages-budget.json" 2>/dev/null; then
	base_packages="$(git show "$base:packages-budget.json")"
	head_packages="$(cat packages-budget.json)"
	package_problem="$(jq -nr --argjson base "$base_packages" --argjson head "$head_packages" '
        if ($base.packages | type) != "number" or ($head.packages | type) != "number" then
            error("packages-budget.json: packages must be a number")
        elif $head.packages > $base.packages then
            "packages-budget.json: raised from \($base.packages) to \($head.packages)"
        else empty end
    ')"
	if [ -n "$package_problem" ]; then
		problems="${problems:+$problems$'\n'}$package_problem"
	fi
fi
# CONTRIBUTING.md's "separate owner budget commit before the implementation", made checkable: a raise
# passes when every commit between the base and HEAD that touches a budget file is a `build(budget):`
# commit touching nothing else, so the raise stands alone in history where a reviewer reads it. A removed
# bucket or a changed measurement is never excused this way. CI checks out the window its own commits
# fill, not the whole mirror; a history that cannot be walked refuses, below.
budget_files='loc-budget.json packages-budget.json'
if [ -n "$problems" ] && ! printf '%s\n' "$problems" | grep -qv ' raised from '; then
	# Nothing above needs history: a ceiling, a removed bucket and a changed measurement are all read
	# from two files at the base, which is why a one-commit CI checkout answers them after fetching that
	# one commit by name. Excusing a raise is the one read that walks, so the shallow window CI fetches
	# (`fetch-depth: <n>`, see .gitea/workflows/ci.yml) has to reach the fork point before this point is
	# passed. It can stop above it — the base then arrives as an island of one commit, fetched by SHA —
	# and `rev-list base..HEAD` still answers, over the gap: it names commits that never touched a budget
	# file and misses ones that did. A shallow checkout where git cannot say where the base and HEAD
	# diverge fails here, by name, rather than reading a range it never fetched.
	if [ "$(git rev-parse --is-shallow-repository)" = true ] && ! git merge-base "$base" HEAD >/dev/null 2>&1; then
		echo 'budget ratchet: a raise is excused only by the commits that carry it, and this shallow checkout cannot find where the base and HEAD diverge' >&2
		exit 2
	fi
	if ! touched="$(git rev-list --no-merges "$base..HEAD" -- $budget_files 2>/dev/null)"; then
		echo 'budget ratchet: the history between the base and HEAD cannot be read' >&2
		exit 2
	fi
	unreviewed=''
	for commit in $touched; do
		subject="$(git log -1 --format=%s "$commit")"
		others="$(git diff-tree --no-commit-id --name-only -r "$commit" | grep -vxE 'loc-budget\.json|packages-budget\.json' || true)"
		if [[ "$subject" != build\(budget\):* ]] || [ -n "$others" ]; then
			unreviewed="${unreviewed}${unreviewed:+$'\n'}${commit:0:12} ${subject}"
		fi
	done
	if [ -n "$touched" ] && [ -z "$unreviewed" ]; then
		printf '%s\n' "$problems"
		echo "budget ratchet: the raises above stand alone in build(budget) commits: $(printf '%s ' $touched | cut -c1-200)"
		problems=''
	elif [ -n "$unreviewed" ]; then
		problems="${problems}"$'\n'"budget ratchet: these commits change a budget file and are not a build(budget) commit touching only budget files:"$'\n'"${unreviewed}"
	fi
fi
if [ -n "$problems" ]; then
	printf '%s\n' "$problems" >&2
	echo 'Budget changes require their own build(budget) commit, touching only budget files, reviewed apart from the change; this change does not preserve the baseline.' >&2
	exit 1
fi
echo "budgets preserve the reviewed ceilings at $base"
