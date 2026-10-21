#!/usr/bin/env bash
# A fresh raise can buy back some returned lines; a later merge and lowering cannot
# buy back the rest. Check both sides of the new allowance without changing the input.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
budget() {
	printf '{"buckets":[{"name":"go","suffixes":[".go"],"max":%s}]}\n' "$1" > loc-budget.json
	echo '{"packages":5}' > packages-budget.json
}
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }
expect() {
	local want="$1" label="$2" pattern="$3" got=0 before
	before="$(git rev-parse HEAD)"
	bash "$script" --merge-base origin/main > "$root/out" 2>&1 || got=$?
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
main_grows() {
	git checkout -q main
	budget "$1" && commit 'build(budget): main prices one line'
	echo '// main' > "$2" && commit 'feat: main consumes its line'
	git update-ref refs/remotes/origin/main HEAD
	git checkout -q branch
	git merge -q --no-ff --no-commit origin/main > "$root/merge.out" 2>&1 || true
	git rev-parse --verify MERGE_HEAD >/dev/null
}

mkdir "$root/repo" && cd "$root/repo" && git init -q -b main
git config --local user.name ratchet-test
git config --local user.email ratchet-test@example.invalid
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'base'
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
budget 9 && commit 'build(budget): price six lines'
printf '// first\n// second\n// third\n' >> a.go && commit 'feat: consume three lines'
budget 6 && commit 'build(budget): return three unused lines'
expect 0 'the remaining three lines stay priced' 'budgets preserve the reviewed ceilings'

main_grows 4 b.go
git checkout -q --theirs loc-budget.json
commit 'Merge main carrying its numbers unchanged'
budget 8 && commit 'build(budget): reprice one of the three returned lines'
echo '// fourth' >> a.go && commit 'feat: consume the repriced line'
expect 0 'a fresh raise can reprice part of the returned allocation' 'budgets preserve the reviewed ceilings'

main_grows 5 c.go
budget 15 && commit 'Merge main with an unpriced ceiling in the resolution'
budget 11 && commit 'build(budget): lower the unpriced merge ceiling'
expect 0 'the repriced allocation survives the later lowering' 'budgets preserve the reviewed ceilings'

echo '// fifth, still unpriced' >> a.go && commit 'feat: exceed the partly repriced allocation'
expect 1 'the lowering cannot revive the rest of the original allocation' 'go grew by 5 lines since the merge base .*declare 4$'
