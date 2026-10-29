#!/usr/bin/env bash
# Compare reviewed ceilings with one explicit base commit, or with the merge base of a ref:
#   check_budget_ratchet.sh BASE_COMMIT [repository]
#   check_budget_ratchet.sh --merge-base REF [repository]
# CI supplies the pull request base or the push's previous revision. Consumers
# call this implementation from their resolved foundation dependency.
# With --merge-base (what `make check-loc` runs against origin/main) a bucket may
# also grow, since the merge base, by no more than this branch's own build(budget)
# commits declare; main's growth is not this branch's to price.
# Requires Git and jq, and Go for --merge-base. An unavailable baseline fails the check.
set -euo pipefail

tool_root="$(cd "$(dirname "$0")/.." && pwd)"
merge_base_ref=''
if [ "${1:-}" = --merge-base ]; then
	if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
		echo 'budget ratchet: --merge-base needs one ref' >&2
		exit 2
	fi
	merge_base_ref="$2"
	cd "${3:-.}"
	if ! base="$(git merge-base "$merge_base_ref" HEAD 2>/dev/null)"; then
		echo "budget ratchet: $merge_base_ref has no merge base with HEAD; fetch it first (git fetch origin main)" >&2
		exit 2
	fi
else
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
fi

# Missing or malformed baselines are errors, never evidence of unchanged
# ceilings. Removing a bucket or changing what it measures also needs review.
base_loc="$(git show "$base:loc-budget.json")"
head_loc="$(cat loc-budget.json)"
# A ceiling that moved and a ceiling that no longer measures what it measured are two verdicts, so
# they are collected apart: the first is a decision an owner can take and a `build(budget):` commit
# can price, the second is never excused. Every moved ceiling is kept as one `file|name|from|to`
# row, which is what lets the accept verdict and the refuse verdict say the same two numbers in
# different words.
hard_problems="$(jq -nr --argjson base "$base_loc" --argjson head "$head_loc" '
    $base.buckets[] as $before
    | [$head.buckets[] | select(.name == $before.name)] as $after
    | if ($after | length) != 1 then
        "loc-budget.json: bucket \($before.name) was removed or duplicated"
      elif ($before | del(.max)) != ($after[0] | del(.max)) then
        "loc-budget.json: measurement changed for \($before.name)"
      else empty end
')"
moved="$(jq -nr --argjson base "$base_loc" --argjson head "$head_loc" '
    $base.buckets[] as $before
    | ([$head.buckets[] | select(.name == $before.name)] | .[0]) // empty
    | select(.max > $before.max)
    | "loc-budget.json|\($before.name)|\($before.max)|\(.max)"
')"
if git cat-file -e "$base:packages-budget.json" 2>/dev/null; then
	base_packages="$(git show "$base:packages-budget.json")"
	head_packages="$(cat packages-budget.json)"
	package_moved="$(jq -nr --argjson base "$base_packages" --argjson head "$head_packages" '
        if ($base.packages | type) != "number" or ($head.packages | type) != "number" then
            error("packages-budget.json: packages must be a number")
        elif $head.packages > $base.packages then
            "packages-budget.json||\($base.packages)|\($head.packages)"
        else empty end
    ')"
	if [ -n "$package_moved" ]; then
		moved="${moved:+$moved$'\n'}$package_moved"
	fi
fi
problems="$hard_problems"
raised=''
while IFS='|' read -r file name from to; do
	[ -n "$file" ] || continue
	raised="${raised:+$raised$'\n'}$file: ${name:+$name }raised from $from to $to"
done <<< "$moved"
if [ -n "$raised" ]; then
	problems="${problems:+$problems$'\n'}$raised"
fi
# CONTRIBUTING.md's "separate owner budget commit before the implementation", made checkable: a raise
# passes when every commit between the base and HEAD that touches a budget file is a `build(budget):`
# commit touching nothing else, so the raise stands alone in history where a reviewer reads it. A removed
# bucket or a changed measurement is never excused this way. CI checks out the window its own commits
# fill, not the whole mirror; a history that cannot be walked refuses, below.
#
# The walk prints no commit before its own children (--topo-order), which is what makes "newest" below
# mean nearest to HEAD rather than latest by a clock a rebase moved, and it names no paths. A path-limited
# rev-list applies git's history simplification: where a merge's budget files match one parent, git follows
# that parent alone, so a branch that merges main and carries main's loc-budget.json loses every
# build(budget) commit on its own side and is then refused for growth it did price. What decides whether a
# commit belongs here is that commit's own diff, which this one call prints for every non-merge commit
# between the base and HEAD, main's own commits included in no range because the merge base excludes them.
# One commit is named once, not once per budget file it edits: a commit is asked whether it is a
# build(budget) commit and what it declares, and a commit that prices lines and packages together edits
# two files while declaring one line allowance. Duplicated here, its allowance is added twice and its
# own raise counted twice against the owner's threshold, so two priced lines admit four and a raise of
# 600 reads as 1,200. The `!seen[sha]++` guard runs only on a budget filename, so a commit that touches
# neither file still prints nothing.
budget_files='loc-budget.json packages-budget.json'
	# Nothing above needs history: a ceiling, a removed bucket and a changed measurement are all read
	# from two files at the base, which is why a one-commit CI checkout answers them after fetching that
	# one commit by name. Excusing a raise is the one read that walks, so a shallow window (`fetch-depth:
	# <n>`, see .gitea/workflows/ci.yml) has to reach the fork point before this point is passed — but only
	# where a raise stands. A checkout with nothing to excuse asks the walk for nothing: its verdict is in
	# the two files already, and a one-commit checkout of a branch that lowered a ceiling or moved no
	# number at all is an ordinary CI run, its base an island of one commit fetched by SHA. Where a raise
	# does stand, a window that stops above the fork point fails here, by name, rather than excusing growth
	# from a range it never fetched: the walk answers over such a gap, and the answer it gives names commits
	# that never touched a budget file and misses ones that did, which is not a footing for a ceiling.
	if [ -n "$raised" ] && [ "$(git rev-parse --is-shallow-repository)" = true ] \
		&& ! git merge-base "$base" HEAD >/dev/null 2>&1; then
		echo 'budget ratchet: a raise is excused only by the commits that carry it, and this shallow checkout cannot find where the base and HEAD diverge' >&2
		exit 2
	fi
if ! touched="$(git -c color.ui=false log --no-merges --topo-order --format='commit %H' --name-only "$base..HEAD" 2>/dev/null \
	| awk -v files="$budget_files" '
		BEGIN { split(files, names, " "); for (i in names) budget[names[i]] = 1 }
		/^commit [0-9a-f]{40}$/ { sha = substr($0, 8); next }
		($0 in budget) && !seen[sha]++ { print sha }
	')"; then
	echo 'budget ratchet: the history between the base and HEAD cannot be read' >&2
	exit 2
fi
# budget_commit COMMIT — whether COMMIT is a build(budget) commit touching only budget files.
budget_commit() {
	local others
	others="$(git diff-tree --no-commit-id --name-only -r "$1" | grep -vxE 'loc-budget\.json|packages-budget\.json' || true)"
	[[ "$(git log -1 --format=%s "$1")" == build\(budget\):* ]] && [ -z "$others" ]
}
if [ -n "$problems" ] && ! printf '%s\n' "$problems" | grep -qv ' raised from '; then
	unreviewed=''
	for commit in $touched; do
		if ! budget_commit "$commit"; then
			unreviewed="${unreviewed}${unreviewed:+$'\n'}${commit:0:12} $(git log -1 --format=%s "$commit")"
		fi
	done
	if [ -n "$touched" ] && [ -z "$unreviewed" ]; then
		# An accepted raise is printed in the accept verdict's own words, never in the refusal's. Both
		# verdicts have to name the same two numbers, so nothing is hidden here; what changed is that only
		# a refusal still reads "raised from N to N". This step prints the last lines a CI job writes, and
		# a reader that triages a red run by pattern quotes any line in that shape as one of its failures —
		# which is how four ceilings an owner had priced came to be reported as refusals of this branch.
		# A verdict that cannot be told from its opposite in the log is the defect.
		while IFS='|' read -r file name from to; do
			[ -n "$file" ] || continue
			echo "$file: ${name:+$name }ceiling $from -> $to is priced in its own build(budget) commit"
		done <<< "$moved"
		echo "budget ratchet: the raises above stand alone in build(budget) commits: $(printf '%s ' $touched | cut -c1-200)"
		problems=''
	elif [ -n "$unreviewed" ]; then
		problems="${problems}"$'\n'"budget ratchet: these commits change a budget file and are not a build(budget) commit touching only budget files:"$'\n'"${unreviewed}"
	fi
fi
# What this branch declares, one row per bucket: the ceiling its own most recent build(budget) commit
# wrote for that bucket, less the ceiling main's own history already held at that commit — `git
# merge-base BASE COMMIT`, which is BASE where the commit descends from the base and the branch's fork
# point everywhere else. A commit is asked whether it wrote the bucket, in its own diff against its own
# parent, and what it wrote is measured against main. Measuring it against main rather than against the
# commit before it is what keeps one declaration one declaration: a branch that merges main carries
# main's loc-budget.json, the commit after it re-raises main's ceiling to where the branch's own priced
# ceiling stood, so summing every commit's own delta prices those same lines once per merge, admits
# growth nothing priced and asks for `Owner-approved:` trailers on a raise that never passed 1,000 lines.
# A merge is never the declaration, so a raise written only in a merge resolution declares nothing. A
# lowering declares no growth and is never a refusal — it is the tidying CONTRIBUTING prescribes, not a
# promise to shrink. Whether a commit raised its bucket or brought it down is read off its own parent, so
# the row carries both numbers: what the commit wrote over main's, and whether it raised it. A budget
# commit that arrives on a merged sibling is the branch's own for this: it declares what it wrote, where
# it was written.
raised=''
for commit in $touched; do
	budget_commit "$commit" && git cat-file -e "$commit:loc-budget.json" 2>/dev/null || continue
	anchor="$(git merge-base "$base" "$commit" 2>/dev/null || printf '%s' "$base")"
	rows="$(jq -nr --arg c "$commit" \
		--argjson parent "$(git show "$commit^:loc-budget.json" 2>/dev/null || echo '{"buckets": []}')" \
		--argjson main "$(git show "$anchor:loc-budget.json" 2>/dev/null || echo '{"buckets": []}')" \
		--argjson wrote "$(git show "$commit:loc-budget.json")" '
	    $wrote.buckets[] as $a
	    | ([$parent.buckets[] | select(.name == $a.name)] | .[0]) as $before
	    | ([$main.buckets[] | select(.name == $a.name)] | .[0]) as $held
	    | select($before != null and $held != null and $before.max != $a.max)
	    | "\($c) \($a.name) \($a.max - $held.max) \(if $a.max > $before.max then 1 else 0 end)"
	')"
	raised="${raised}${raised:+${rows:+$'\n'}}${rows}"
done
# Each row is `<commit> <bucket> <lines over main where it was written> <1 when the commit raises the
# bucket over its own parent, 0 when it brings it down>`. The walk is topological and prints the commit
# nearest HEAD first, so a bucket's rows read back to front are this branch's declarations in the order
# they became effective, and one bucket's allowance is their running total: a raising commit lifts it to
# what it wrote over main's where it was written, and a commit that brings the ceiling down pulls it down
# to the smaller number it wrote and never up.
#
# The order is the rule, and no single row carries it. Reading only the newest row grants whatever that
# commit wrote over main's, so a branch that merged main with a higher ceiling written in the resolution
# alone is granted that ceiling as soon as one of its own commits lowers it — including the lines an
# earlier lowering of its own ceiling had already returned. Reading only the largest raise ever written
# keeps a branch that returned half of an approved allocation on the hook for the whole of it. The
# running total is the reading both documents describe: a raise buys what it declares over main's, a
# bring-down returns lines to the pool — down to none, when the branch takes the whole raise back — and a
# number that reached the branch only through a merge is tidied, never spent, until a raise owns it.
allowances="$(awk '
	$2 != "" { count[$2]++; over[$2, count[$2]] = $3 + 0; up[$2, count[$2]] = $4 }
	END {
		for (bucket in count) {
			held = 0
			for (i = count[bucket]; i >= 1; i--) {
				wrote = over[bucket, i]
				if (up[bucket, i] == 1) { if (wrote > held) held = wrote }
				else if (wrote < held) held = wrote > 0 ? wrote : 0
			}
			print bucket, held
		}
	}' <<< "$raised")"
# A raise of more than 1,000 lines in one bucket is the owner's, not only the reviewer's: every commit
# that declares that bucket more than 1,000 lines over main names the approval in an `Owner-approved:`
# trailer. A raise the branch wrote before it crossed the line stayed within review's reach and owes none:
# asking it of them retroactively made every branch that crossed in steps rewrite history it had been told
# never to rewrite (T-0720, T-0424, T-0452 on 2026-10-25), and decision 0100 puts the owner's line on the
# build(budget) commit that prices the approved raise. The check reads that the
# claim is recorded where review reads it; it cannot read that the owner made it. A commit that brings a
# ceiling down spends no allocation — it returns lines to the pool — so it never owed a trailer: the raise
# that took the allocation carries one, and refusing the tidying would hold an allocation open past the
# day its owner priced it. What asks for the trailer is the largest raise this branch wrote, not the
# ceiling standing at HEAD, so a lowering cannot disown an approval the raise still needs.
for bucket in $(awk '$4 == 1 && $3 > 1000 { print $2 }' <<< "$raised" | sort -u); do
	for commit in $(awk -v b="$bucket" '$2 == b && $4 == 1 && $3 > 1000 { print $1 }' <<< "$raised"); do
		if [ -z "$(git log -1 --format='%(trailers:key=Owner-approved,valueonly)' "$commit" | tr -d '[:space:]')" ]; then
			problems="${problems:+$problems$'\n'}loc-budget.json: $bucket rises over 1,000 lines on this branch and ${commit:0:12} carries no Owner-approved: trailer"
		fi
	done
done
# With --merge-base, each bucket's growth since the merge base is held to what this branch declares.
# The merge base's tree is counted by the same tool in a throwaway clone the trap removes, never in a
# worktree, which would register in the .git this host's other checkouts share.
if [ -n "$merge_base_ref" ]; then
	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT
	if ! (cd "$tool_root" && go build -o "$tmp/locbudget" ./tools/locbudget) \
		|| ! git clone --quiet --shared --no-checkout "$(cd "$(git rev-parse --git-common-dir)" && pwd)" "$tmp/base" \
		|| ! git -C "$tmp/base" checkout --quiet --detach "$base" \
		|| ! base_counts="$("$tmp/locbudget" --root "$tmp/base")" \
		|| ! head_counts="$("$tmp/locbudget" --root .)"; then
		echo "budget ratchet: the tree at the merge base ${base:0:12} cannot be checked out and counted" >&2
		exit 2
	fi
	while read -r name grew; do
		# What pays for a line is the allowance the branch's own build(budget) commits last left standing for
		# the bucket: a merge of main neither widens it nor restores the part of it a lowering returned.
		allowed="$(awk -v b="$name" '$1 == b { print $2; exit }' <<< "$allowances")"
		[ -n "$allowed" ] || allowed=0
		if [ "$grew" -gt "$allowed" ]; then
			problems="${problems:+$problems$'\n'}loc-budget.json: $name grew by $grew lines since the merge base ${base:0:12}; this branch's build(budget) commits declare $allowed"
		fi
	done < <(awk 'NR == FNR { if ($3 == "/") at_base[$1] = $2; next } $3 == "/" && ($1 in at_base) { print $1, $2 - at_base[$1] }' \
		<(printf '%s\n' "$base_counts") <(printf '%s\n' "$head_counts"))
fi
if [ -n "$problems" ]; then
	printf '%s\n' "$problems" >&2
	echo 'Budget changes require their own build(budget) commit, touching only budget files, reviewed apart from the change; this change does not preserve the baseline.' >&2
	exit 1
fi
echo "budgets preserve the reviewed ceilings at $base"
