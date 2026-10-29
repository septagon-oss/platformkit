#!/usr/bin/env bash
# docs/MODULE-GRAPH.md is written by scripts/module_graph.sh from the aggregate report
# `go run ./tools/importgate --modules .` answers, and that report is collected out of maps: each of
# its lists stands in order only because moduleGraph sorts what it collected. `make check-generated`
# writes once and diffs, so a writer whose lists move between two runs of one tree is judged by a coin
# toss — the checked-in bytes match whenever the one run happened to agree. This asks the writer twice
# over one tree, in a copy of it so nothing tracked is rewritten, and refuses a row whose source kinds
# are named out of the order of the kinds or named twice.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
fail() { # fail REASON [EVIDENCE_FILE]
	echo "FAIL: $1"
	[ -s "${2:-}" ] && sed 's/^/    /' "$2"
	exit 1
}

# The writer rewrites the document it measures, so it runs over a copy. `tar` rather than an export of
# HEAD: `.gitignore` keeps `reports/` out of a fresh index, and a tree that loses the fixture under it
# cannot be measured at all.
mkdir -p "$root/tree"
tar -C "$repo" --exclude=./.git --exclude=./node_modules -cf - . | tar -x -C "$root/tree"
[ -s "$root/tree/scripts/module_graph.sh" ] || fail 'the copied tree holds no generator to measure'

for run in first second; do
	: > "$root/out"
	bash "$root/tree/scripts/module_graph.sh" "$root/tree" > "$root/out" 2>&1 \
		|| fail "the generator refused the tree ($run)" "$root/out"
	cp "$root/tree/docs/MODULE-GRAPH.md" "$root/$run.md" \
		|| fail "the generator wrote no document ($run)" "$root/out"
done
echo 'ok   the generator measures one tree twice'

diff "$root/first.md" "$root/second.md" > "$root/moved" || fail \
	"one tree answered the module document twice with $(grep -c '^[<>]' "$root/moved") differing lines" "$root/moved"
echo 'ok   one tree answers the module document the same way twice'

python3 - "$root/first.md" > "$root/cells" 2>&1 <<'PY' || fail 'a row names its source kinds out of the order of the kinds' "$root/cells"
import sys

checked, refused = 0, []
for line in open(sys.argv[1], encoding="utf-8"):
    if not line.startswith("| `"):
        continue
    cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
    if len(cells) != 7:
        continue  # the module table has three cells; only the edge and construction tables have seven
    kinds = [kind.strip() for kind in cells[5].split(",")]
    checked += 1
    if not all(kinds):
        refused.append(f"{cells[0]} -> {cells[1]} names an empty source kind in {cells[5]!r}")
    elif kinds != sorted(kinds) or len(set(kinds)) != len(kinds):
        refused.append(f"{cells[0]} -> {cells[1]} names its source kinds as {cells[5]!r}")
if not checked:
    sys.exit(f"{sys.argv[1]} holds no row between modules to read")
if refused:
    sys.exit("\n".join(refused))
print(f"ok   every one of the {checked} rows names its source kinds once and in the order of the kinds")
PY
cat "$root/cells"
