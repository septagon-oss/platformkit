"""Area verdicts retain their totals when zero-verdict areas move into prose."""
from collections import Counter, defaultdict
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
VERDICTS = ("keep-rewrite", "merge", "delete")


class InventoryAreaTotalsMatchDocument(unittest.TestCase):
    def setUp(self):
        self.rows = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        document = (ROOT / "tests/INVENTORY.md").read_text()
        self.section = document.split("### Round-named verdicts by area\n", 1)[1].split("\n## ", 1)[0]
        self.areas = defaultdict(list)
        for row in self.rows:
            self.areas[row["area"]].append(row)

    def test_area_table_preserves_file_counts_and_verdict_totals(self):
        actual = {}
        for line in self.section.splitlines():
            cells = [cell.strip() for cell in line.strip("|").split("|")]
            if len(cells) == 5 and cells[1].isdigit():
                self.assertNotIn(cells[0], actual)
                actual[cells[0]] = tuple(int(cell) for cell in cells[1:])
        expected = {}
        for area, rows in self.areas.items():
            counts = Counter(row["verdict"] for row in rows)
            if any(counts[verdict] for verdict in VERDICTS):
                expected[area] = (len({row["file"] for row in rows}),
                                  *(counts[verdict] for verdict in VERDICTS))
        self.assertEqual(actual, expected)
        totals = Counter(row["verdict"] for row in self.rows)
        for column, verdict in enumerate(VERDICTS, start=1):
            self.assertEqual(sum(values[column] for values in actual.values()), totals[verdict])

    def test_omitted_areas_retain_their_population_in_prose(self):
        prose = "\n".join(line for line in self.section.splitlines() if not line.startswith("|"))
        omitted = []
        for area, rows in self.areas.items():
            if all(row["verdict"] == "keep" for row in rows):
                omitted.append(area)
                count = len({row["file"] for row in rows})
                self.assertRegex(prose, rf"`{re.escape(area)}` \({count} files\b")
        self.assertTrue(omitted, "the prose must cover at least one omitted area")


if __name__ == "__main__":
    unittest.main()
