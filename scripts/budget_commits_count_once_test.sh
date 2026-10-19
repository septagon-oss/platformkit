#!/usr/bin/env bash
# A budget commit may price both lines and packages, but its line allowance and
# approval threshold are counted once regardless of how many budget files it edits.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0

commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }
budget() {
	printf '{"buckets":[{"name":"go","suffixes":[".go"],"max":%s}]}\n' "$1" > loc-budget.json
	printf '{"packages":%s}\n' "$2" > packages-budget.json
}
fresh() {
	mkdir "$root/$1"
	cd "$root/$1"
	git init -q -b main
	budget 3 5
	printf 'package a\n// one\n// two\n' > a.go
	commit 'base'
	git update-ref refs/remotes/origin/main HEAD
	git checkout -q -b branch
}
expect() {
	local want="$1" label="$2" pattern="$3" got=0
	bash "$script" --merge-base origin/main > "$root/out" 2>&1 || got=$?
	if [ "$got" -ne "$want" ]; then
		echo "FAIL: $label: exit $got, wanted $want"
		cat "$root/out"
		failures=$((failures + 1))
	elif ! grep -qE "$pattern" "$root/out"; then
		echo "FAIL: $label: the verdict gives no matching reason"
		cat "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $label"
	fi
}

fresh unpriced_growth
budget 5 6
commit 'build(budget): price two go lines and one package'
printf '// three\n// four\n// five\n// six\n' >> a.go
commit 'feat: four lines against two priced'
expect 1 'editing both budget files does not double the line allowance' \
	'go grew by 4 lines since the merge base .*declare 2'

fresh approval_threshold
budget 603 6
commit 'build(budget): price six hundred go lines and one package'
expect 0 'editing both budget files does not double the approval threshold' \
	'budgets preserve the reviewed ceilings'

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'budget commits: each declaration counts once'
