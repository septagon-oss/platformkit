#!/usr/bin/env bash
# A branch prices its own growth in a build(budget) commit, then merges main and carries main's
# loc-budget.json unchanged, as CONTRIBUTING.md's "Keep budgets honest" tells it to. Its growth since
# the new merge base is still what its own build(budget) commit declared, so `make check-loc` — the
# ratchet's --merge-base mode — accepts it. A walk that drops the branch's side of that merge loses
# the declaration and refuses growth the branch priced.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

budget() { # budget <go max>
	printf '{"buckets": [{"name": "go", "suffixes": [".go"], "max": %s}, {"name": "docs", "suffixes": [".txt"], "max": 10}]}\n' "$1" > loc-budget.json
	echo '{"packages": 5}' > packages-budget.json
}
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }
lines() { local n; for n in $(seq "$2"); do echo "// $n" >> "$1"; done; }
fail() { echo "FAIL: $1"; sed 's/^/    /' "$root/out"; exit 1; }

mkdir -p "$root/repo" && cd "$root/repo" && git init -q -b main
git config --local user.name ratchet-test
git config --local user.email ratchet-test@example.invalid
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'main at its ceiling'
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
budget 5 && commit 'build(budget): go 3 -> 5'
lines a.go 2 && commit 'feat: two lines, priced'

: > "$root/out"
bash "$script" --merge-base origin/main >"$root/out" 2>&1 || fail 'the branch is refused before it merges main'
echo 'ok   the branch passes before it merges main'

git checkout -q main
budget 6 && commit 'build(budget): go 3 -> 6'
lines b.go 3 && commit 'feat: three lines main priced'
git update-ref refs/remotes/origin/main HEAD
git checkout -q branch
git merge -q --no-edit origin/main >/dev/null 2>&1 || true
git checkout -q --theirs loc-budget.json
git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm 'Merge origin/main, carrying its numbers unchanged'
git diff --quiet origin/main -- loc-budget.json packages-budget.json \
	|| { git diff origin/main -- loc-budget.json > "$root/out"; fail 'the merge did not carry main'"'"'s numbers'; }
echo 'ok   the merge carries main'"'"'s numbers unchanged'

got=0
bash "$script" --merge-base origin/main >"$root/out" 2>&1 || got=$?
if [ "$got" != 0 ] || ! grep -q 'budgets preserve the reviewed ceilings' "$root/out"; then
	fail "a branch whose growth its own build(budget) commit priced is refused after carrying main's numbers: exit $got"
fi
echo 'ok   the growth the branch priced is still priced after it carries main'"'"'s numbers'
