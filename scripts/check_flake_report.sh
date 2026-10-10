#!/usr/bin/env bash
# The nightly tier's two reports: the slowest cases, and which case is a flake (decision 0088).
#
# tests/inventory.json carries `duration` and a flake column that say `unmeasured`, because no step
# in this repository wrote the report those columns read: Makefile names GOTESTSUM_JSONFILE in a
# comment and nothing writes it, so the forge answers job clocks and nothing finer. This goal is
# that missing writer. It runs the whole push tier — every package with a case that opens no stack,
# which is the tier that can be run five times over without a database and without waiting behind a
# heavy slot — FLAKE_RUNS times in one `go` invocation, and reads what came back.
#
# A flake is what the word means and no more: the same tree answered both ways. The forge's job
# clocks cannot say that, because a run whose own change was broken fails for a reason that is not
# flakiness at all; a repeated run of one revision can. The two numbers answer different questions
# and this file prints only the second.
#
# Usage: check_flake_report.sh [runs]   (default 5; PKIT_FLAKE_PACKAGES overrides the selection)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runs="${1:-5}"
case "$runs" in (''|*[!0-9]*) echo "usage: $(basename "$0") [runs]" >&2; exit 2 ;; esac
report_dir="${PKIT_REPORT_DIR:-$root/reports}"
json="$report_dir/push-tier.jsonl"
mkdir -p "$report_dir"

packages="${PKIT_FLAKE_PACKAGES:-}"
if [ -z "$packages" ]; then
	packages="$(python3 "$root/scripts/test_inventory.py" --root "$root" --tier push --base '')"
fi
[ -n "$packages" ] || { echo "no package with a case that opens no stack: nothing to measure" >&2; exit 1; }

go tool gotestsum --jsonfile "$json" --packages="$packages" -- -count="$runs" -timeout=20m

echo
echo "slowest cases of $runs runs over $(tr -cd ' ' <<<"$packages" | wc -w | tr -d ' ') package(s):"
go tool gotestsum tool slowest --jsonfile "$json" --num 25

python3 - "$json" "$runs" <<'PY'
import json, collections, sys
path, runs = sys.argv[1], int(sys.argv[2])
outcomes = collections.defaultdict(set)
for line in open(path, encoding="utf-8", errors="replace"):
    try:
        e = json.loads(line)
    except json.JSONDecodeError:
        continue
    test, action = e.get("Test"), e.get("Action")
    if not test or action not in ("pass", "fail", "skip"):
        continue
    outcomes[test].add(action)
flakes = {t: o for t, o in outcomes.items() if "fail" in o}
print(f"\nflake report, {runs} runs of one revision: {len(outcomes)} case(s) answered, {len(flakes)} that failed at least once")
for t, o in sorted(flakes.items()):
    print(f"  {t}: {'+'.join(sorted(o))} — the same tree answered both ways; the inventory's flake column is this line")
if not flakes:
    print("  none: nothing answered both ways, which is the reading that lets a prune trust a green run")
PY
