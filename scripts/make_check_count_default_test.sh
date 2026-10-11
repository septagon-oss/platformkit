#!/usr/bin/env bash
# CI runs `make check` and `make check-race` with TEST_COUNT absent from its environment and from the
# command line. This case asks the Makefile for exactly that expansion — no TEST_COUNT anywhere — so a
# later assignment that empties the variable after its `?=` default (which a grep for the default line,
# or a dry run that names TEST_COUNT on the command line, both still accept) is refused.
# Dry runs only: nothing is compiled, started or tested.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
sed -n '/^module[[:space:]]/p; /^go[[:space:]]/p; /^toolchain[[:space:]]/p' "$root/go.mod" > "$temporary/go.mod"

ci_line() { # <goal> <prefix>: the goal's suite line as CI's environment expands it
	env -u TEST_COUNT -u MAKEFLAGS -u MAKEOVERRIDES make --no-print-directory -n -C "$temporary" -f "$root/Makefile" "$1" </dev/null |
		sed -n "/^$2 /s/[[:blank:]]*\$//p"
}

suite="$(ci_line check 'go tool gotestsum')"
# The bound this line carries is not a count: -timeout=30m is the per-package clock the Makefile's own
# comment sizes (go test's 10-minute default killed a working apps/platformkit on 2026-10-06). It is
# written after $(TEST_COUNT), not before, because scripts/ci_go_cache_test.sh reads the count variable as
# it sits directly after the option separator. What this case is about is the count: with TEST_COUNT absent
# from
# the environment the expansion must still carry -count=1, and it must be this line and not one that
# narrowed it.
if [[ "$suite" != "go tool gotestsum --packages='./...' -- -count=1 -timeout=30m" ]]; then
	printf 'FAIL: make check without TEST_COUNT is not the fresh suite line CI depends on:\n%s\n' "$suite" >&2
	exit 1
fi
race="$(ci_line check-race 'go test')"
# The race line carries a bound beside its count, and it is pinned here for the reason written at
# `check`'s line above: go test's ten-minute default is what stopped a working apps/platformkit four
# times on 2026-10-10 (runs 57644, 57686, 57718 and 57727, each panic naming whichever case happened
# to be open at the tenth minute). The count, the bound and the package list the bound precedes are
# the three parts a later edit may not drop; what it may change is everything around them.
case "$race" in
	'go test -race -count=1 -timeout='*m' ./'*) ;;
	*)
		printf 'FAIL: make check-race without TEST_COUNT is not the fresh, bounded race line CI depends on:\n%s\n' "$race" >&2
		exit 1 ;;
esac
if grep -rn 'TEST_COUNT' "$root/.gitea" >&2; then
	echo 'FAIL: a workflow names TEST_COUNT; CI must take the Makefile default' >&2
	exit 1
fi
echo 'test count: with TEST_COUNT unset, check and check-race run every test fresh, and no workflow sets it'
