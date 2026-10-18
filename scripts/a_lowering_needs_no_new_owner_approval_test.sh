#!/usr/bin/env bash
# Returning part of an approved allocation needs no new approval, even when over 1,000 lines remain.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0
budget() {
	printf '{"buckets":[{"name":"go","suffixes":[".go"],"max":%s}]}\n' "$1" > loc-budget.json
	echo '{"packages":5}' > packages-budget.json
}
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1" ${2:+-m "$2"}; }
expect_accepted() {
	local label="$1" got=0 before
	shift
	before="$(git rev-parse HEAD)"
	bash "$script" "$@" > "$root/out" 2>&1 || got=$?
	if [ "$(git rev-parse HEAD)" != "$before" ] || [ -n "$(git status --porcelain)" ]; then
		echo "FAIL: $label: checking approval changed the repository"
		exit 1
	fi
	if [ "$got" != 0 ] || ! grep -q 'budgets preserve the reviewed ceilings' "$root/out"; then
		echo "FAIL: $label: exit $got, wanted acceptance"
		cat "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $label"
	fi
}

mkdir "$root/repo" && cd "$root/repo" && git init -q -b main
budget 3 && printf 'package a\n// one\n// two\n' > a.go && commit 'base'
base="$(git rev-parse HEAD)"
git update-ref refs/remotes/origin/main HEAD
git checkout -q -b branch
budget 2003 && commit 'build(budget): price two thousand go lines' 'Owner-approved: this allocation'
expect_accepted 'the original allocation is approved' --merge-base origin/main

budget 1503 && commit 'build(budget): return five hundred unused go lines'
expect_accepted 'lowering the approved allocation passes the merge-base check' --merge-base origin/main
expect_accepted 'lowering the approved allocation passes the explicit-base check' "$base"

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
