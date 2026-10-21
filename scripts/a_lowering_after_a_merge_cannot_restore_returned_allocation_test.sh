#!/usr/bin/env bash
# Once a branch returns part of an allocation, lowering an unpriced merge ceiling
# cannot restore those lines without another raising budget commit.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
budget() {
	printf '{"buckets":[{"name":"go","suffixes":[".go"],"max":%s}]}\n' "$1" > loc-budget.json
	echo '{"packages":5}' > packages-budget.json
}
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }
expect_unpriced() {
	local label="$1" got=0 before
	before="$(git rev-parse HEAD)"
	bash "$script" --merge-base origin/main > "$root/out" 2>&1 || got=$?
	if [ "$(git rev-parse HEAD)" != "$before" ] || [ -n "$(git status --porcelain)" ]; then
		echo "FAIL: $label: checking growth changed the repository"
		exit 1
	fi
	if [ "$got" != 1 ] || ! grep -qE 'go grew by 2 lines since the merge base .*declare 1$' "$root/out"; then
		echo "FAIL: $label: exit $got, wanted two lines refused against the one still priced"
		cat "$root/out"
		exit 1
	fi
	echo "ok   $label"
}

mkdir "$root/repo" && cd "$root/repo" && git init -q -b main
git config --local user.name ratchet-test
git config --local user.email ratchet-test@example.invalid
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'base'
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
budget 5 && commit 'build(budget): price two go lines'
echo '// one priced line' >> a.go && commit 'feat: consume one priced line'
budget 4 && commit 'build(budget): return the unused go line'
bash "$script" --merge-base origin/main > "$root/out" 2>&1 || { cat "$root/out"; exit 1; }
echo 'ok   the remaining one-line allocation passes'

git checkout -q main
budget 4 && commit 'build(budget): main prices one go line'
echo '// main' > b.go && commit 'feat: main consumes its priced line'
git update-ref refs/remotes/origin/main HEAD
git checkout -q branch
git merge -q --no-ff --no-commit origin/main > "$root/merge.out" 2>&1
git rev-parse --verify MERGE_HEAD >/dev/null
budget 14 && commit 'Merge main with an unpriced ceiling in the resolution'
echo '// one line from the returned allocation' >> a.go
commit 'feat: exceed the remaining allocation by one line'
expect_unpriced 'a merge resolution cannot restore the returned line'

budget 12 && commit 'build(budget): lower the merge ceiling by two lines'
expect_unpriced 'lowering the merge ceiling cannot restore the returned line'
