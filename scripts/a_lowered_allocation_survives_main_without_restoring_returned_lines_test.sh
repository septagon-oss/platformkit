#!/usr/bin/env bash
# Returning unused lines keeps the remaining approved allocation across a merge of main,
# but a merge cannot restore the lines returned by the branch's lowering commit.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
budget() {
	printf '{"buckets":[{"name":"go","suffixes":[".go"],"max":%s}]}\n' "$1" > loc-budget.json
	echo '{"packages":5}' > packages-budget.json
}
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1" ${2:+-m "$2"}; }
expect() {
	local want="$1" label="$2" pattern="$3" got=0 before
	shift 3
	before="$(git rev-parse HEAD)"
	bash "$script" "$@" > "$root/out" 2>&1 || got=$?
	if [ "$(git rev-parse HEAD)" != "$before" ] || [ -n "$(git status --porcelain)" ]; then
		echo "FAIL: $label: the checker changed its input repository"
		exit 1
	fi
	if [ "$got" != "$want" ] || ! grep -qE "$pattern" "$root/out"; then
		echo "FAIL: $label: exit $got, wanted $want and $pattern"
		cat "$root/out"
		exit 1
	fi
	echo "ok   $label"
}

mkdir "$root/repo" && cd "$root/repo" && git init -q -b main
git config --local user.name ratchet-test
git config --local user.email ratchet-test@example.invalid
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'base'
base="$(git rev-parse HEAD)"
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
budget 1503 && commit 'build(budget): price fifteen hundred go lines' 'Owner-approved: this allocation'
for n in $(seq 1450); do echo "// priced $n" >> a.go; done
commit 'feat: consume fourteen hundred and fifty priced lines'
budget 1453 && commit 'build(budget): return fifty unused go lines'
expect 0 'the consumed remainder needs no second approval' 'budgets preserve the reviewed ceilings' --merge-base origin/main
expect 0 'the explicit-base check accepts the consumed remainder' 'budgets preserve the reviewed ceilings' "$base"

git checkout -q main
budget 4 && commit 'build(budget): main prices one go line'
echo '// main' > b.go && commit 'feat: main consumes its priced line'
git update-ref refs/remotes/origin/main HEAD
git checkout -q branch
git merge -q --no-ff --no-commit origin/main > "$root/merge.out" 2>&1 || true
git rev-parse --verify MERGE_HEAD >/dev/null
git checkout -q --theirs loc-budget.json
commit 'Merge main carrying its numbers unchanged'
git diff --exit-code origin/main -- loc-budget.json packages-budget.json
expect 0 'carrying main retains the consumed remainder' 'budgets preserve the reviewed ceilings' --merge-base origin/main

echo '// one line of the returned allocation' >> a.go
commit 'feat: exceed the remaining allocation by one line'
expect 1 'the merge does not restore returned lines' 'go grew by 1451 lines since the merge base .*declare 1450$' --merge-base origin/main
