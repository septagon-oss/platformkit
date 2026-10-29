#!/usr/bin/env bash
# What `make check-loc` measures is the tree on disk, not the tree the index holds: a test file
# no commit has written yet already counts into its bucket, so the growth is refused before the
# file can arrive, and the raise that pays for it has to stand in a `build(budget):` commit on its
# own. A branch can therefore leave a test file unwritten-in-history while it prices it — the
# order the budget commit and the case commit go in — but it can never land one for free.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0

budget() { printf '{"buckets": [{"name": "go_test", "suffixes": ["_test.go"], "max": %s}]}\n' "$1" >loc-budget.json; }
commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }
# A raise is priced by a commit that carries budget files alone, which is the rule the ratchet
# itself enforces; `git add -A` would carry the test file with it and answer a different question.
budget_commit() { git add loc-budget.json && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }

fresh() {
	mkdir -p "$root/$1" && cd "$root/$1" || return 1
	git init -q -b main
	git config --local user.name counted-test
	git config --local user.email counted-test@example.invalid
	budget 100
	printf 'package a\n' >held_test.go
	commit 'base'
	git update-ref refs/remotes/origin/main HEAD
	git checkout -q -b branch
}

expect() { # expect <exit code> <case name> <pattern>
	local want="$1" name="$2" pattern="$3" got=0
	bash "$script" --merge-base origin/main >"$root/out" 2>&1 || got=$?
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: exit $got, wanted $want"
		sed 's/^/    /' "$root/out"
		failures=$((failures + 1))
	elif [ -n "$pattern" ] && ! grep -qE "$pattern" "$root/out"; then
		echo "FAIL: $name: the verdict gives no matching reason"
		sed 's/^/    /' "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

# Four lines that exist only on disk, with nothing priced: the refusal names the growth, so the
# branch is told what to price rather than left with a ceiling it cannot reach.
fresh unheld_growth
printf 'package a\n// one\n// two\n// three\n' >held_again_test.go
expect 1 'a test file no commit holds counts into the bucket' \
	'go_test grew by 4 lines since the merge base .*declare 0'

# The same four lines, priced by a budget commit while the file is still unwritten in history:
# what the ceiling answers is the tree, so paying for it ahead of the commit is legitimate order.
fresh priced_before_it_lands
printf 'package a\n// one\n// two\n// three\n' >held_again_test.go
budget 104 && budget_commit 'build(budget): price four lines of a test file no commit holds yet'
expect 0 'a raise priced before the file lands pays for it' ''

# And once the file is committed, the same raise still answers for it: the growth is measured from
# the merge base either way, so nothing asks for a second raise when the file arrives.
fresh priced_then_landed
printf 'package a\n// one\n// two\n// three\n' >held_again_test.go
budget 104 && budget_commit 'build(budget): price four lines of a test file no commit holds yet'
commit 'test: the file lands'
expect 0 'the raise priced before the file lands still pays for it after' ''

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'budget ratchet: what is on disk counts, whether or not a commit holds it yet'
