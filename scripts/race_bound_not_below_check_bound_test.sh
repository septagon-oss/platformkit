#!/usr/bin/env bash
# `make check-race` runs packages `make check` also runs, with the detector on top, so the
# per-package bound the race line states can never be below the one the suite line states: a
# race line that promised less would bring back, under -race, the cut-off that stating a bound
# exists to end. The two guards that pin the race line accept any `-timeout=<n>m`; this case
# reads both numbers out of the dry runs and compares them. Dry runs only: nothing is compiled,
# started or tested. A first argument names a Makefile to ask instead of the repository's own,
# which is how the refusals below are reached.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
sed -n '/^module[[:space:]]/p; /^go[[:space:]]/p; /^toolchain[[:space:]]/p' "$root/go.mod" > "$temporary/go.mod"

ci_line() { # <makefile> <goal> <prefix>: the goal's suite line as CI's environment expands it
	env -u TEST_COUNT -u MAKEFLAGS -u MAKEOVERRIDES make --no-print-directory -n -C "$temporary" -f "$1" "$2" </dev/null |
		sed -n "/^$3 /s/[[:blank:]]*\$//p"
}

minutes_of() { # <line>: the whole minutes of the line's -timeout=<n>m, or nothing when it names none
	printf '%s\n' "$1" | sed -n 's/.*[[:blank:]]-timeout=\([0-9][0-9]*\)m\([[:blank:]].*\)\{0,1\}$/\1/p'
}

# bounds <makefile>: 0 when the race goal states a bound not below the suite goal's; 1 otherwise.
bounds() {
	local makefile="$1" suite race suite_minutes race_minutes
	suite="$(ci_line "$makefile" check 'go tool gotestsum')"
	race="$(ci_line "$makefile" check-race 'go test')"
	suite_minutes="$(minutes_of "$suite")"
	race_minutes="$(minutes_of "$race")"
	if [[ -z "$suite_minutes" ]]; then
		printf 'FAIL: make check states no per-package bound in minutes:\n%s\n' "$suite" >&2
		return 1
	fi
	if [[ -z "$race_minutes" ]]; then
		printf 'FAIL: make check-race states no per-package bound in minutes:\n%s\n' "$race" >&2
		return 1
	fi
	if ((race_minutes < suite_minutes)); then
		printf 'FAIL: make check-race promises %sm where make check promises %sm; the detector cannot finish sooner than the suite without it:\n%s\n' \
			"$race_minutes" "$suite_minutes" "$race" >&2
		return 1
	fi
	printf 'ok   make check-race states %sm, not below the %sm make check states\n' "$race_minutes" "$suite_minutes"
}

if [[ $# -gt 0 ]]; then
	bounds "$1"
	exit
fi

bounds "$root/Makefile"

# The refusals, each asked of a copy of the Makefile with only the race recipe's bound changed.
# Each copy is read back through the same dry run before it is judged, so a sed that matched
# nothing cannot pass as a refusal of the unchanged line. The equal fixture may be byte-identical
# to the committed file when the two bounds already coincide; it is the boundary of the rule.
suite_minutes="$(minutes_of "$(ci_line "$root/Makefile" check 'go tool gotestsum')")"
race_recipe='^\tgo test -race \$(TEST_COUNT) -timeout=[0-9][0-9]*m '

mutate() { # <name> <the bound to write, with its trailing blank, or nothing> <the minutes to read back>
	local read_back
	sed "s/${race_recipe}/\tgo test -race \$(TEST_COUNT) $2/" "$root/Makefile" > "$temporary/$1.mk"
	read_back="$(minutes_of "$(ci_line "$temporary/$1.mk" check-race 'go test')")"
	if [[ "$read_back" != "$3" ]]; then
		printf 'FAIL: the %s fixture reads back a bound of "%s" minutes, want "%s"; the race recipe did not match %s\n' \
			"$1" "$read_back" "$3" "$race_recipe" >&2
		exit 1
	fi
}

mutate below "-timeout=$((suite_minutes - 1))m " "$((suite_minutes - 1))"
if out="$(bounds "$temporary/below.mk" 2>&1)"; then
	printf 'FAIL: a race bound one minute below the suite bound was accepted:\n%s\n' "$out" >&2
	exit 1
fi
printf 'ok   a race bound below the suite bound: refused — %s\n' "$out"

mutate absent '' ''
if out="$(bounds "$temporary/absent.mk" 2>&1)"; then
	printf 'FAIL: a race line with no bound was accepted:\n%s\n' "$out" >&2
	exit 1
fi
printf 'ok   a race line that states no bound: refused — %s\n' "$out"

mutate equal "-timeout=${suite_minutes}m " "$suite_minutes"
if ! out="$(bounds "$temporary/equal.mk" 2>&1)"; then
	printf 'FAIL: a race bound equal to the suite bound was refused:\n%s\n' "$out" >&2
	exit 1
fi
printf 'ok   a race bound equal to the suite bound: accepted — %s\n' "$out"

mutate above "-timeout=$((suite_minutes + 1))m " "$((suite_minutes + 1))"
if ! out="$(bounds "$temporary/above.mk" 2>&1)"; then
	printf 'FAIL: a race bound above the suite bound was refused:\n%s\n' "$out" >&2
	exit 1
fi
printf 'ok   a race bound above the suite bound: accepted — %s\n' "$out"
