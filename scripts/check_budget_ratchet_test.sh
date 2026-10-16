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

fresh_branch unfetched
git update-ref -d refs/remotes/origin/main
expect_merge_base 2 'a branch with no merge base with origin/main is refused'
if ! grep -q 'no merge base' "$root/out"; then
	echo 'FAIL: the refusal does not say there is no merge base'
	failures=$((failures + 1))
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'budget ratchet: all cases hold'
