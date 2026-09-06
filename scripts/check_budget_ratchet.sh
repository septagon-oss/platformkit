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
if [ -n "$problems" ]; then
	printf '%s\n' "$problems" >&2
	echo 'Budget changes require a separate owner review; this change does not preserve the baseline.' >&2
	exit 1
fi
echo "budgets preserve the reviewed ceilings at $base"
