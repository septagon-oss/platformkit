#!/usr/bin/env bash
# The budget ratchet's cases, each in a throwaway repository: a raise is accepted only when it stands alone
# in a `build(budget):` commit, and a removed bucket or a changed measurement is never excused.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0

budget() { # budget <go max> [<extra bucket name>]
	local extra=''
	[ -n "${2:-}" ] && extra=", {\"name\": \"$2\", \"suffixes\": [\".txt\"], \"max\": 10}"
	printf '{"buckets": [{"name": "go", "suffixes": [".go"], "max": %s}%s]}\n' "$1" "$extra" > loc-budget.json
	echo '{"packages": 5}' > packages-budget.json
}

commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1" ${2:+-m "$2"}; }

fresh() { # a repository whose first commit is the base; prints the base sha
	local dir="$root/$1"
	mkdir -p "$dir" && cd "$dir" && git init -q -b main
	# name the committer here rather than per invocation: a merge commits with the repository's own identity, and a CI container names none
	git config --local user.name ratchet-test
	git config --local user.email ratchet-test@example.invalid
	budget 100 docs
	echo 'package a' > a.go
	commit 'base'
}

expect() { # expect <exit code> <case name>
	local want="$1" name="$2" got=0
	bash "$script" "$base" >"$root/out" 2>&1 || got=$?
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: exit $got, wanted $want"
		sed 's/^/    /' "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

fresh unchanged; base="$(git rev-parse HEAD)"
echo 'package a // more' > a.go && commit 'feat: a change inside the ceiling'
expect 0 'no budget change passes'

fresh alone; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140 for the feature'
echo 'package a // the feature' > a.go && commit 'feat: the feature'
expect 0 'a raise standing alone in a build(budget) commit passes'

fresh mixed; base="$(git rev-parse HEAD)"
budget 140 docs && echo 'package a // the feature' > a.go && commit 'build(budget): go 100 -> 140 with the feature'
expect 1 'a raise sharing its commit with code is refused'

fresh subject; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'chore: go 100 -> 140'
expect 1 'a raise under any other subject is refused'

fresh twice; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140'
budget 160 docs && echo 'package a // x' > a.go && commit 'feat: and a quiet second raise'
expect 1 'one reviewed raise does not excuse a second, unreviewed one'

fresh removed; base="$(git rev-parse HEAD)"
budget 100 && commit 'build(budget): drop the docs bucket'
expect 1 'a removed bucket is refused even in a build(budget) commit'

fresh lowered; base="$(git rev-parse HEAD)"
budget 80 docs && commit 'feat: the ceiling comes down with the code'
expect 0 'a lowered ceiling passes in any commit'

# The window CI fetches (`fetch-depth: <n>`, one refspec, no tag) instead of every branch and tag in the
# mirror. Both edges of a window are cases: the one that reaches the branch's own history still walks it,
# and the one that stops above it fails by name. The short clone is the dangerous shape, because the
# ratchet's own rescue — `git fetch --depth=1 origin $base` — can hand over the base commit as an island
# of one commit, and `rev-list base..HEAD` answers over the gaps either way.
fresh window-src; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140'
echo 'package a // x' > a.go && commit 'feat: the feature'
echo 'package a // y' > a.go && commit 'feat: more'
echo 'package a // z' > a.go && commit 'feat: more still'

window() { # window <name> <depth> <expected exit> — a clone of that branch at that fetch depth
	local dir="$root/window-$2"
	rm -rf "$dir"
	git clone -q --depth="$2" "file://$root/window-src" "$dir" && cd "$dir"
	expect "$3" "$1"
}

window 'a window that reaches the history the branch fills still walks it' 5 0
window 'a window that stops above it refuses' 2 2

# The two verdicts have to read differently. Both name the same two numbers, and only the exit
# status told them apart, so an accepted raise printed a line in the refusal's shape — and a reader
# that triages a red CI run by pattern reports that line as one of its failures. The ratchet keeps
# "raised from N to N" for the ceilings it refuses and says "priced" for the ones it accepts.
fresh wording; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140 for the feature'
accepted=0
bash "$script" "$base" >"$root/accepted" 2>&1 || accepted=$?
if [ "$accepted" != 0 ] || ! grep -q 'go ceiling 100 -> 140 is priced' "$root/accepted" \
   || grep -q ' raised from ' "$root/accepted"; then
	echo 'FAIL: an accepted raise did not say so in words no refusal shares'
	sed 's/^/    /' "$root/accepted"
	failures=$((failures + 1))
else
	echo 'ok   an accepted raise says it is priced, in words no refusal shares'
fi
budget 160 docs && commit 'chore: a second raise nobody priced'
unreviewed=0
bash "$script" "$base" >"$root/unreviewed" 2>&1 || unreviewed=$?
if [ "$unreviewed" != 1 ] || ! grep -q 'go raised from 100 to 160' "$root/unreviewed"; then
	echo 'FAIL: a refusal no longer names the raise it refused in the words a reviewer reads'
	sed 's/^/    /' "$root/unreviewed"
	failures=$((failures + 1))
else
	echo 'ok   a refusal still names the raise it refused'
fi

# The merge-base cases, which are `make check-loc`: a bucket may grow since the merge base with
# origin/main by what this branch's own build(budget) commits declare and no more, and a ceiling that
# main's own growth made stale is not this branch's to restate.
lines() { local n; for n in $(seq "$2"); do echo "// $n" >> "$1"; done; }
fresh_branch() { # main holds a.go at its go ceiling of 3, origin/main points there, HEAD is a branch off it
	fresh "$1"
	budget 3 docs && printf 'package a\n// one\n// two\n' > a.go && commit 'main at its ceiling'
	git update-ref refs/remotes/origin/main HEAD
	git checkout -q -b branch
}
on_main() { git checkout -q main; "$@"; git update-ref refs/remotes/origin/main HEAD; git checkout -q branch; }
main_raises_then_overruns() { # main prices go 3 -> 5, then grows to 7 lines: its own ceiling is stale
	budget 5 docs && commit 'build(budget): go 3 -> 5'
	lines b.go 4 && commit 'feat: main grows past its own raise'
}
expect_merge_base() { # expect_merge_base <exit code> <case name>
	local want="$1" name="$2" got=0
	bash "$script" --merge-base origin/main >"$root/out" 2>&1 || got=$?
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: exit $got, wanted $want"
		sed 's/^/    /' "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

fresh_branch moved_merged
on_main main_raises_then_overruns
sed -i 's/one/uno/' a.go && commit 'feat: a change that grows nothing'
git merge -q --no-edit origin/main
expect_merge_base 0 'a branch that merged main after a main-side raise passes without touching loc-budget.json'

fresh_branch moved_unmerged
sed -i 's/one/uno/' a.go && commit 'feat: a change that grows nothing'
on_main main_raises_then_overruns
expect_merge_base 0 'a branch whose origin/main moved past a raise passes unmerged'

fresh_branch unpriced
lines a.go 2 && commit 'feat: two lines nobody priced'
expect_merge_base 1 'a branch growing a bucket without a build(budget) commit is refused'
if ! grep -q 'go grew by 2 lines since the merge base .* declare 0' "$root/out"; then
	echo 'FAIL: the refusal does not name the bucket, its growth and what was declared'
	failures=$((failures + 1))
fi

fresh_branch inside
budget 8 docs && commit 'build(budget): go 3 -> 8'
lines a.go 4 && commit 'feat: four lines inside the five priced'
expect_merge_base 0 'growth inside a declared raise passes'

fresh_branch beyond
budget 5 docs && commit 'build(budget): go 3 -> 5'
lines a.go 4 && commit 'feat: four lines against two priced'
expect_merge_base 1 'growth beyond a declared raise is refused'

# A ceiling that comes down is tidying, not a promise to shrink. The branch that lowers one to what its
# own tree counts grows by nothing, so the ratchet accepts the commit that priced it; and the same
# lowering buys no room to grow, because an allowance is what the branch raised, never the sum of what it
# moved. Reading a signed lowering against the allowance refused the rebaseline command CONTRIBUTING
# prescribes, on a branch whose code had not moved at all.
budget_docs() { # budget_docs <go max> <docs max> — the two ceilings of `budget 3 docs`, each given
	printf '{"buckets": [{"name": "go", "suffixes": [".go"], "max": %s}, {"name": "docs", "suffixes": [".txt"], "max": %s}]}\n' "$1" "$2" > loc-budget.json
	echo '{"packages": 5}' > packages-budget.json
}

fresh_branch lowering_prices_nothing
budget_docs 3 8 && commit 'build(budget): the docs ceiling comes down to what the tree counts'
expect_merge_base 0 'a build(budget) commit that only lowers a ceiling is never a refusal'

fresh_branch lowering_buys_nothing
budget_docs 3 8 && commit 'build(budget): the docs ceiling comes down to what the tree counts'
lines a.go 2 && commit 'feat: two lines the lowering did not price'
expect_merge_base 1 'a lowering declares no growth'

# Two readings of the same pair of commits, on a branch that raised a bucket and then brought it down
# again: what pays for the lines is the raise it was priced at, or what the budget file last carried for
# the bucket. The second is what `make check-loc` holds, because a ceiling the file no longer states is a
# ceiling no reviewer agreed to. So the branch that lowers the bucket it had raised is refused for its
# growth, and the one that keeps enough of it stands on what is left of the raise.
fresh_branch raise_then_lower_declares_nothing
budget 8 docs && commit 'build(budget): go 3 -> 8'
budget 3 docs && commit 'build(budget): go comes back down to 3'
lines a.go 2 && commit 'feat: two lines of a raise the branch took back'
expect_merge_base 1 'a raise the branch lowers back is no longer priced'

fresh_branch lower_keeps_what_is_left
budget 8 docs && commit 'build(budget): go 3 -> 8'
lines a.go 4 && commit 'feat: four of the five priced lines'
budget 7 docs && commit 'build(budget): go 8 -> 7, one line came back out'
expect_merge_base 0 'the ceiling the branch last wrote is what pays for its growth'

fresh_branch resolution
on_main main_raises_then_overruns
git merge -q --no-ff --no-commit origin/main >/dev/null
budget 20 docs && commit 'merge main, raising go in the resolution'
lines a.go 4 && commit 'feat: four lines the merge resolution claimed to price'
expect_merge_base 1 'a raise declared only in a merge resolution declares nothing'

fresh_branch unapproved
budget 1004 docs && commit 'build(budget): go 3 -> 1004'
expect_merge_base 1 'a raise over 1,000 lines without Owner-approved is refused'

fresh_branch approved
budget 1004 docs && commit 'build(budget): go 3 -> 1004' 'Owner-approved: the owner, for this raise'
expect_merge_base 0 'a raise over 1,000 lines with Owner-approved passes'

fresh_branch approved_in_steps
budget 903 docs && commit 'build(budget): go 3 -> 903'
budget 1004 docs && commit 'build(budget): go 903 -> 1004' 'Owner-approved: the owner, for the whole raise'
expect_merge_base 0 'the approval goes on the commit that crosses 1,000 lines, not on the raises before it'

fresh_branch crossing_unapproved
budget 903 docs && commit 'build(budget): go 3 -> 903' 'Owner-approved: an early word'
budget 1004 docs && commit 'build(budget): go 903 -> 1004'
expect_merge_base 1 'the commit that takes a bucket past 1,000 lines carries the approval itself'

fresh_branch unfetched
git update-ref -d refs/remotes/origin/main
expect_merge_base 2 'a branch with no merge base with origin/main is refused'
if ! grep -q 'no merge base' "$root/out"; then
	echo 'FAIL: the refusal does not say there is no merge base'
	failures=$((failures + 1))
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'budget ratchet: all cases hold'
