#!/usr/bin/env bash
# The push tier's selection, run (decision 0088).
#
# Which packages a push runs is not a list somebody keeps: it is the packages the diff reaches —
# directly, through `go list`'s dependency graph, or through the imports that exist only inside a
# package's test binary — that have a case and open no stack. The answer
# comes out of tests/inventory.json, the same table a person ratified, so a package cannot be in the
# tier by the selector's rule and out of it by the table's column.
#
# The tier is defined by what it does not open. A selection that reaches a package needing Postgres,
# a broker, the object store or the mail catcher is refused with exit 3, and no flag opens that door:
# two push jobs for one branch then hold no shared fixture, and the tier keeps its promise by
# construction rather than by locking.
#
# PKIT_BASE_REF names the ref the diff is taken against (origin/main); PKIT_TIER=all selects every
# package with a case that opens no stack instead of the ones the diff reaches.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base="${PKIT_BASE_REF:-origin/main}"
if [ "${PKIT_TIER:-}" = "all" ]; then
	base=""
elif ! git -C "$root" rev-parse --verify --quiet "${base}^{commit}" >/dev/null; then
	echo "push tier: ${base} is not a commit, so the diff reaches nothing — name a real PKIT_BASE_REF rather than run no test" >&2
	exit 2
fi

packages="$(python3 "$root/scripts/test_inventory.py" --root "$root" --tier push --base "$base")"
if [ -z "$packages" ]; then
	echo "push tier: the diff against ${base:-every package} reaches no package with a case that opens no stack"
	exit 0
fi
echo "push tier (${base:-every package}): $(tr ' ' ', ' <<<"$packages")"
go tool gotestsum --packages="$packages" -- -count=1 -timeout=10m
