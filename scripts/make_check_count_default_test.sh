#!/usr/bin/env bash
# CI runs `make check` and `make check-race` with TEST_COUNT absent from its environment and from the
# command line. This case asks the Makefile for exactly that expansion — no TEST_COUNT anywhere — so a
# later assignment that empties the variable after its `?=` default (which a grep for the default line,
# or a dry run that names TEST_COUNT on the command line, both still accept) is refused. Each of the two
# suite goals is asked for two parts, not one: the count that makes the run fresh, and the stated
# per-package clock that keeps a loaded runner from killing a package that is still working. Both live on
# the line, because the line is the whole of what CI runs.
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
# The race goal carries the same two parts the goal above does, and for the same two reasons: the count
# because CI must never read a verdict the commit did not run, the bound because go test's ten-minute
# per-package default is a clock nobody here chose. Forge runs 521 and 519 each killed
# `apps/platformkit` at that default — 600.295s and 603.370s, under `panic: test timed out after 10m0s`
# with one case two seconds into its own start and the rest of the package parked in t.Parallel — over
# against 421.7s, 426.4s and 438.0s for the same package on the three race runs before them. The number
# and the measurements behind it are written above the line in the Makefile; what this case asks is that
# CI's own expansion carry both parts, so a goal that keeps the count and loses the bound, or the bound
# and the count, is refused here rather than in the race step at 20:00:43Z.
if [[ "$race" != 'go test -race -count=1 -timeout=30m ./'* ]]; then
	printf 'FAIL: make check-race without TEST_COUNT is not the fresh, bounded race line CI depends on:\n%s\n' "$race" >&2
	exit 1
fi
if grep -rn 'TEST_COUNT' "$root/.gitea" >&2; then
	echo 'FAIL: a workflow names TEST_COUNT; CI must take the Makefile default' >&2
	exit 1
fi
echo 'test count: with TEST_COUNT unset, check and check-race run every test fresh inside a stated bound, and no workflow sets it'
