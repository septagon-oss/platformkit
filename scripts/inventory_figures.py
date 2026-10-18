"""Write the generated sections of tests/INVENTORY.md from the files that answer them.

Each section sits between `<!-- BEGIN GENERATED: scripts/inventory_figures.py <block> -->` and
`<!-- END GENERATED -->` on lines of their own, and is rewritten whole; the prose around it is a
person's. `make generate` runs this offline: it reads tests/inventory.json and loc-budget.json and
nothing else, so two runs on one tree write the same bytes.

The figures come from the same derivation `scripts/test_inventory.py --check` refuses a stale quote
against, so the document and the check cannot disagree about what a row means. Only ceilings and
counts the rows answer are rendered here — never a figure measured once at a named revision, which
stays in the prose beside the run set or revision that produced it.

Refusals exit 2 and leave the file untouched: a BEGIN without its END or an END without a BEGIN, a
block this writer does not render, a block it renders that the file lacks or holds twice, and an
input that is missing or does not carry what a block reads.
"""
from collections import Counter, defaultdict
import json
from pathlib import Path
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))

import test_inventory

ROOT = Path(__file__).resolve().parents[1]
DOCUMENT = ROOT / "tests" / "INVENTORY.md"
TOOL = "scripts/inventory_figures.py"
BEGIN = re.compile(r"^<!-- BEGIN GENERATED: " + re.escape(TOOL) + r" (\S+) -->$")
END = "<!-- END GENERATED -->"
REWRITE = ("keep-rewrite", "merge", "delete")
MERGE_GROUPS = 15
MERGED_NAMES = 3


def figure(figures, section, label):
    """The numbers test_inventory expects beside one row label, refused when the row is absent."""
    if (section, label) not in figures:
        raise ValueError("test_inventory answers no figure for %s / %s" % (section, label))
    return figures[(section, label)]


def scale(rows, figures, budget):
    """The Scale table: every count of the census, with the measurement that filled each cell."""
    rows_out = ["| quantity | value | measured by |", "|---|---|---|",
        "| test rows | %s | one per Go test function, Playwright test, Maestro flow, pin script, `node --test` case |"
        % " / ".join(str(n) for n in figure(figures, "Scale", "test rows")),
        "| files | %s | `git ls-files` over `*_test.go`, `e2e/**`, `scripts/*_test.*`, `*.test.{js,mjs,ts}` |"
        % " / ".join(str(n) for n in figure(figures, "Scale", "files")),
        "| Go test functions | %s | `^func (Test\\|Benchmark\\|Fuzz\\|Example)` |"
        % " / ".join(str(n) for n in figure(figures, "Scale", "Go test functions")),
        "| files named for a round (`review*`, `round*`, `probe*`) | %s | filename prefix |"
        % " / ".join(str(n) for n in figure(figures, "Scale", "files named for a round (review, round, probe)")),
        "| rows inside them | %s | same walk |" % " / ".join(str(n) for n in figure(figures, "Scale", "rows inside them")),
        "| Playwright specs / Maestro flows / `scripts/` pins / node tests | %s | same walk |"
        % " / ".join(str(n) for n in figure(figures, "Scale", "Playwright specs / Maestro flows / scripts/ pins / node tests")),
    ]
    return rows_out


def layers(rows, figures, budget):
    """The Layers table: rows per 0088 layer, in LAYERS order, each with the tier that reads it."""
    tiers = {
        "contract": ("push, first", "asserts a golden or the published API set"),
        "behaviour": ("push or merge", "one package's rules — Go, or one design-tooling case; push when the package opens no stack"),
        "composition": ("merge", "package `main` of the reference app: it boots the composition (every `scripts/` pin is filed here too, at tier push)"),
        "journey": ("nightly", "`e2e/`: browser or device"),
    }
    rows_out = ["| layer (0088) | rows | tier | predicate that put it there |", "|---|---|---|---|"]
    for layer in test_inventory.LAYERS:
        tier, predicate = tiers[layer]
        rows_out.append("| %s | %d | %s | %s |" % (layer, figure(figures, "Layers and tiers", layer)[0], tier, predicate))
    return rows_out


def verdicts(rows, figures, budget):
    """The Verdicts table: files and rows per verdict, in VERDICTS order, each with its rule."""
    meanings = {
        "keep": "not named for a round: 0088 rule 2 does not reach it",
        "keep-rewrite": "named for a round, and the only file in its package reaching something — rewritten and renamed for the rule in the same commit (72)",
        "merge": "a rule a kept sibling in the same package already reaches — `merge_into` names it",
        "delete": "nothing in the file judges anything, and the file is not a TestMain harness, a benchmark or an `// Output:` example",
    }
    rows_out = ["| verdict | files | rows | meaning |", "|---|---|---|---|"]
    for verdict in test_inventory.VERDICTS:
        files, counted = figure(figures, "Verdicts", verdict)
        rows_out.append("| `%s` | %d | %d | %s |" % (verdict, files, counted, meanings[verdict]))
    return rows_out


def merges(rows, figures, budget):
    """The fifteen (package, target) groups holding most `merge` rows, largest first."""
    groups = defaultdict(set)
    for row in rows:
        if row["verdict"] == "merge":
            groups[(row["pkg"], row.get("merge_into") or "")].add(Path(row["file"]).name)
    ordered = sorted(groups.items(), key=lambda item: (-len(item[1]), item[0][0], target_text(item[0][1])))
    rows_out = ["| package | files | into |", "|---|---|---|"]
    for (pkg, target), names in ordered[:MERGE_GROUPS]:
        shown = sorted(names)[:MERGED_NAMES]
        listed = ", ".join(shown) + (" …" if len(names) > MERGED_NAMES else "")
        rows_out.append("| `%s` | %d: %s | %s |" % (pkg, len(names), listed, target_text(target)))
    return rows_out


def target_text(merge_into):
    """The merge target as the document names it: a base name, or the package itself when none does."""
    return "`%s`" % Path(merge_into).name if merge_into else "(none in package)"


def area_verdicts(rows, figures, budget):
    """Every area that holds a verdict other than `keep`, with its file population and the rows of
    each verdict, ordered by population; then the columns' sums and the areas left out."""
    areas = defaultdict(list)
    for row in rows:
        areas[row["area"]].append(row)
    order = sorted(areas, key=lambda a: (-len({r["file"] for r in areas[a]}), a))
    lines = ["| area | files | keep-rewrite | merge | delete |", "|---|---|---|---|---|"]
    omitted = []
    for area in order:
        files = len({r["file"] for r in areas[area]})
        counts = Counter(r["verdict"] for r in areas[area])
        if any(counts[v] for v in REWRITE):
            lines.append("| %s | %d | %s |" % (area, files, " | ".join(str(counts[v]) for v in REWRITE)))
        else:
            omitted.append("`%s` (%d files)" % (area, files))
    totals = Counter(r["verdict"] for r in rows)
    lines.append("")
    lines.append("The verdict columns sum to %d keep-rewrite, %d merge and %d delete. Areas that hold no verdict, "
                 "with their file populations: %s." % (*(totals[v] for v in REWRITE), ", ".join(omitted) or "none"))
    return lines


def go_test_ceiling(rows, figures, budget):
    """The ceiling loc-budget.json declares for go_test, never the count, which moves with every test."""
    ceilings = [b["max"] for b in budget["buckets"] if b.get("name") == "go_test"]
    if len(ceilings) != 1 or not isinstance(ceilings[0], int):
        raise ValueError("loc-budget.json declares no single integer ceiling for go_test")
    return ["The `go_test` bucket declares a ceiling of %d lines in `loc-budget.json`; `make check-loc` refuses a "
            "branch's growth in it beyond what the branch prices in its own `build(budget):` commits since its "
            "merge base with `origin/main`." % ceilings[0]]


BLOCKS = {"area-verdicts": area_verdicts, "go-test-ceiling": go_test_ceiling, "layers": layers,
          "merges": merges, "scale": scale, "verdicts": verdicts}


def render(text, rows, figures, budget):
    """The document with every block rewritten, or ValueError naming the first marker problem."""
    out, seen, block = [], set(), None
    for number, line in enumerate(text.split("\n"), start=1):
        begin = BEGIN.match(line)
        if begin:
            if block is not None:
                raise ValueError("line %d: BEGIN %s inside %s, whose END is missing" % (number, begin.group(1), block))
            block = begin.group(1)
            if block not in BLOCKS:
                raise ValueError("line %d: %s renders no block named %s" % (number, TOOL, block))
            if block in seen:
                raise ValueError("line %d: block %s appears twice" % (number, block))
            seen.add(block)
            out.append(line)
            out.extend(BLOCKS[block](rows, figures, budget))
        elif line == END:
            if block is None:
                raise ValueError("line %d: END without a BEGIN" % number)
            block = None
            out.append(line)
        elif block is None:
            out.append(line)
    if block is not None:
        raise ValueError("block %s has no END" % block)
    missing = sorted(set(BLOCKS) - seen)
    if missing:
        raise ValueError("the document holds no block named %s" % ", ".join(missing))
    return "\n".join(out)


def main():
    try:
        doc = test_inventory.load(str(ROOT / "tests" / "inventory.json"))
        rows = doc["rows"]
        figures = test_inventory.expected_figures(rows, test_inventory.summarise(rows, doc["summary"], doc["packages"],
                                                                                doc["summary"]["round_named_ceiling"])[0])
        budget = json.loads((ROOT / "loc-budget.json").read_text(encoding="utf-8"))
        text = DOCUMENT.read_text(encoding="utf-8")
        new = render(text, rows, figures, budget)
    except (OSError, KeyError, TypeError, ValueError) as err:
        print("inventory figures: %s; tests/INVENTORY.md is unchanged" % err, file=sys.stderr)
        return 2
    if new != text:
        DOCUMENT.write_text(new, encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())
