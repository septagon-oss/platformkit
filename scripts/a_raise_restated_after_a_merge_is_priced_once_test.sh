#!/usr/bin/env bash
# A branch prices its growth in a build(budget) commit, merges main twice carrying main's
# loc-budget.json, and after each merge re-raises main's ceiling to where its own priced ceiling
# stood: one declaration, restated. Its ceiling at HEAD stands N lines above the merge base, so
# the ratchet allows N lines of growth and asks for no Owner-approved: trailer while N is at most
# 1,000. Summing every restatement grants the branch N lines once per merge: it then admits growth
# nothing priced and refuses a raise of N for a trailer the threshold never asked for.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0

budget() { # budget <go max>
	printf '{"buckets": [{"name": "go", "suffixes": [".go"], "max": %s}]}\n' "$1" > loc-budget.json
	echo '{"packages": 5}' > packages-budget.json
}
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }
lines() { local n; for n in $(seq "$2"); do echo "// $n" >> "$1"; done; }
ceiling() { jq -r '.buckets[] | select(.name == "go") | .max' loc-budget.json; }

# main_moves <file> — main prices and adds ten lines of its own, and origin/main follows it.
main_moves() {
	git checkout -q main
	budget $(($(ceiling) + 10)) && commit "build(budget): main's ten lines in $1"
	lines "$1" 10 && commit "feat: main's ten lines in $1"
	git update-ref refs/remotes/origin/main HEAD
	git checkout -q branch
}
# merge_and_restate <raise> — the branch merges main carrying main's numbers, then restates its raise.
merge_and_restate() {
	git merge -q --no-edit origin/main > /dev/null 2>&1 || true
	git checkout -q --theirs loc-budget.json
	commit 'Merge origin/main, carrying its numbers unchanged'
	budget $(($(ceiling) + $1)) && commit "build(budget): the merged tree's ceilings, measured over main's"
}
# history <name> <raise> <growth> — a branch that prices <raise> once, grows by <growth> and restates
# the raise after each of two merges of main.
history() {
	mkdir "$root/$1" && cd "$root/$1" && git init -q -b main
	budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'main at its ceiling'
	git update-ref refs/remotes/origin/main HEAD
	git checkout -q -b branch
	budget $((3 + $2)) && commit "build(budget): price $2 go lines"
	lines a.go "$3" && commit "feat: $3 go lines"
	main_moves b.go && merge_and_restate "$2"
	main_moves c.go && merge_and_restate "$2"
}
verdict() {
	local got=0
	bash "$script" --merge-base origin/main > "$root/out" 2>&1 || got=$?
	echo "$got"
}
report() { echo "FAIL: $1"; sed 's/^/    /' "$root/out"; failures=$((failures + 1)); }

history restated_within_threshold 400 400
[ "$(($(ceiling) - $(git show origin/main:loc-budget.json | jq -r '.buckets[0].max')))" = 400 ] \
	|| { echo 'FAIL: the fixture does not stand 400 lines above main'; exit 1; }
got="$(verdict)"
if [ "$got" != 0 ] || grep -q 'Owner-approved' "$root/out" || ! grep -q 'budgets preserve the reviewed ceilings' "$root/out"; then
	report "a raise of 400 restated after two merges is refused as if it were 1,200: exit $got"
else
	echo 'ok   a raise restated after each merge counts once against the owner threshold'
fi

history restated_growth_beyond_it 300 800
got="$(verdict)"
if [ "$got" != 1 ] || ! grep -qE 'go grew by 800 lines since the merge base .*declare 300' "$root/out"; then
	report "800 lines against a raise of 300 restated after two merges are not refused as 500 unpriced: exit $got"
else
	echo 'ok   a raise restated after each merge buys its lines once'
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'budget ratchet: a restated raise is priced once'
