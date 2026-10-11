#!/usr/bin/env bash
# The budget ratchet's cases, each in a throwaway repository: a raise is accepted only when it stands alone
# in a `build(budget):` commit, and a removed bucket or a changed measurement is never excused.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_budget_ratchet.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0

budget() { # budget <go max> [<extra bucket name>]
	local extra=''
	[ -n "${2:-}" ] && extra=", {\"name\": \"$2\", \"suffixes\": [\".txt\"], \"max\": 10}"
	printf '{"buckets": [{"name": "go", "suffixes": [".go"], "max": %s}%s]}\n' "$1" "$extra" > loc-budget.json
	echo '{"packages": 5}' > packages-budget.json
}

commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }

fresh() { # a repository whose first commit is the base; prints the base sha
	local dir="$root/$1"
	mkdir -p "$dir" && cd "$dir" && git init -q -b main
	budget 100 docs
	echo 'package a' > a.go
	commit 'base'
}

expect() { # expect <exit code> <case name>
	local want="$1" name="$2" got=0
	bash "$script" "$base" >"$root/out" 2>&1 || got=$?
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: exit $got, wanted $want"
		sed 's/^/    /' "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

fresh unchanged; base="$(git rev-parse HEAD)"
echo 'package a // more' > a.go && commit 'feat: a change inside the ceiling'
expect 0 'no budget change passes'

fresh alone; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140 for the feature'
echo 'package a // the feature' > a.go && commit 'feat: the feature'
expect 0 'a raise standing alone in a build(budget) commit passes'

fresh mixed; base="$(git rev-parse HEAD)"
budget 140 docs && echo 'package a // the feature' > a.go && commit 'build(budget): go 100 -> 140 with the feature'
expect 1 'a raise sharing its commit with code is refused'

fresh subject; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'chore: go 100 -> 140'
expect 1 'a raise under any other subject is refused'

fresh twice; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140'
budget 160 docs && echo 'package a // x' > a.go && commit 'feat: and a quiet second raise'
expect 1 'one reviewed raise does not excuse a second, unreviewed one'

fresh removed; base="$(git rev-parse HEAD)"
budget 100 && commit 'build(budget): drop the docs bucket'
expect 1 'a removed bucket is refused even in a build(budget) commit'

fresh lowered; base="$(git rev-parse HEAD)"
budget 80 docs && commit 'feat: the ceiling comes down with the code'
expect 0 'a lowered ceiling passes in any commit'

# The two verdicts have to read differently. Both name the same two numbers, and only the exit
# status told them apart, so an accepted raise printed a line in the refusal's shape — and a reader
# that triages a red CI run by pattern reports that line as one of its failures. The ratchet keeps
# "raised from N to N" for the ceilings it refuses and says "priced" for the ones it accepts.
fresh wording; base="$(git rev-parse HEAD)"
budget 140 docs && commit 'build(budget): go 100 -> 140 for the feature'
accepted=0
bash "$script" "$base" >"$root/accepted" 2>&1 || accepted=$?
if [ "$accepted" != 0 ] || ! grep -q 'go ceiling 100 -> 140 is priced' "$root/accepted" \
   || grep -q ' raised from ' "$root/accepted"; then
	echo 'FAIL: an accepted raise did not say so in words no refusal shares'
	sed 's/^/    /' "$root/accepted"
	failures=$((failures + 1))
else
	echo 'ok   an accepted raise says it is priced, in words no refusal shares'
fi
budget 160 docs && commit 'chore: a second raise nobody priced'
unreviewed=0
bash "$script" "$base" >"$root/unreviewed" 2>&1 || unreviewed=$?
if [ "$unreviewed" != 1 ] || ! grep -q 'go raised from 100 to 160' "$root/unreviewed"; then
	echo 'FAIL: a refusal no longer names the raise it refused in the words a reviewer reads'
	sed 's/^/    /' "$root/unreviewed"
	failures=$((failures + 1))
else
	echo 'ok   a refusal still names the raise it refused'
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'budget ratchet: all cases hold'
