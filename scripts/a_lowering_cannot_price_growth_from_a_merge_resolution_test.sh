#!/usr/bin/env bash
# A merge resolution declares no growth, and lowering its ceiling cannot turn it into a declaration.
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
	if [ "$got" != 1 ] || ! grep -qE 'go grew by 8 lines since the merge base .*declare 0' "$root/out"; then
		echo "FAIL: $1: exit $got, wanted refusal of eight unpriced lines"
		cat "$root/out"
		exit 1
	fi
	echo "ok   $1"
}

mkdir "$root/repo" && cd "$root/repo" && git init -q -b main
# name the committer here rather than per invocation: a merge commits with the repository's own identity, and a CI container names none
git config --local user.name ratchet-test
git config --local user.email ratchet-test@example.invalid
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'base'
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
sed -i 's/one/uno/' a.go && commit 'feat: a change that grows nothing'
git checkout -q main
budget 4 && commit 'build(budget): main prices one go line'
echo '// main' > b.go && commit 'feat: main adds its priced line'
git update-ref refs/remotes/origin/main HEAD
git checkout -q branch
git merge -q --no-ff --no-commit origin/main
budget 13 && commit 'Merge main, changing the ceiling only in the resolution'
for n in $(seq 8); do echo "// $n" >> a.go; done
commit 'feat: eight lines no branch budget commit priced'
expect_unpriced 'the merge resolution prices no growth'

budget 12 && commit 'build(budget): return one unused go line'
expect_unpriced 'lowering an unpriced merge ceiling still prices no growth'
