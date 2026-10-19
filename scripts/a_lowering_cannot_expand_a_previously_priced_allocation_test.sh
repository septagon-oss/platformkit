#!/usr/bin/env bash
# A lowering after an unpriced merge raise cannot enlarge the branch's existing allocation.
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
	local got=0 before
	before="$(git rev-parse HEAD)"
	bash "$script" --merge-base origin/main > "$root/out" 2>&1 || got=$?
	if [ "$(git rev-parse HEAD)" != "$before" ] || [ -n "$(git status --porcelain)" ]; then
		echo "FAIL: $1: checking growth changed the repository"
		exit 1
	fi
	if [ "$got" != 1 ] || ! grep -qE 'go grew by 8 lines since the merge base .*declare 2$' "$root/out"; then
		echo "FAIL: $1: exit $got, wanted eight lines refused against the two priced"
		cat "$root/out"
		exit 1
	fi
	echo "ok   $1"
}

mkdir "$root/repo" && cd "$root/repo" && git init -q -b main
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'base'
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
budget 5 && commit 'build(budget): price two go lines'
printf '// three\n// four\n' >> a.go && commit 'feat: two priced lines'
bash "$script" --merge-base origin/main > "$root/out" 2>&1 || { cat "$root/out"; exit 1; }
echo 'ok   the original two-line allocation passes'

git checkout -q main
budget 4 && commit 'build(budget): main prices one go line'
echo '// main' > b.go && commit 'feat: main adds its priced line'
git update-ref refs/remotes/origin/main HEAD
git checkout -q branch
git merge -q --no-ff --no-commit origin/main > "$root/merge.out" 2>&1 || true
git rev-parse --verify MERGE_HEAD >/dev/null
budget 14 && commit 'Merge main with an unpriced ceiling in the resolution'
for n in $(seq 6); do echo "// extra $n" >> a.go; done
commit 'feat: six additional lines without a budget'
expect_unpriced 'the merge resolution cannot enlarge the two-line allocation'

budget 12 && commit 'build(budget): return two unused go lines'
expect_unpriced 'lowering the merge ceiling cannot enlarge the two-line allocation'
