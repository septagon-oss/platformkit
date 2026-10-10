#!/usr/bin/env bash
# CI re-checks the test inventory against the tree (decision 0088).
#
# tests/inventory.json is the table a person ratified; this refuses a table the tree no longer
# answers. It asks six things of the checkout, and refuses on every one of them by name:
#
#   a test with no row                — a test arrived unclassified
#   a row with no test                — a test left and its verdict stayed behind
#   a round-named count above ceiling — the prune went backwards; --write lowers, a rise is a
#                                       build(budget): commit's own, as scripts/check_budget_ratchet.sh
#   a verdict outside the four        — keep, keep-rewrite, merge, delete
#   a merge naming no file in the tree — a verdict cannot point at nothing
#   a delete beside an unmeasured column — the measurement, not a person, stops this one
#
# plus the figures tests/INVENTORY.md quotes, which re-derive from the rows or the quote is refused
# (the same guard scripts/tests/test_bespoke*.py runs over the README's figures).
#
# It reads the tree and the file, never a database: a fake tree is enough to run it, which is why
# scripts/test_inventory_test.sh pins these refusals at all.
#
# Usage: check_test_inventory.sh [--write [--ceiling N]]
#   --write lowers the mechanical columns and adds rows for tests that have none; it never moves a
#   verdict, so re-running it on a tree nobody edited writes the bytes it read.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 "$root/scripts/test_inventory.py" --root "$root" "$@"
