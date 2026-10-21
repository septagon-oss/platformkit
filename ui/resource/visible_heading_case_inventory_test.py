"""The empty-list heading journey stays registered in the browser tier."""
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class VisibleHeadingCaseInventory(unittest.TestCase):
    def test_visible_empty_headings_have_one_runnable_journey_row(self):
        file = "e2e/empty-list-visible-headings.spec.ts"
        name = "task empty list column headings remain visible in both languages and widths"
        rows = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        matches = [row for row in rows if row["file"] == file]
        self.assertEqual(len(matches), 1, "heading journey needs exactly one inventory row")
        expected = {
            "id": file,
            "file": file,
            "name": name,
            "pkg": "e2e",
            "kind": "playwright",
            "layer": "journey",
            "tier": "nightly",
            "needs_db": True,
            "verdict": "keep",
        }
        self.assertEqual({key: matches[0][key] for key in expected}, expected)
        self.assertRegex(
            (ROOT / file).read_text(),
            r"(?m)^test\('" + re.escape(name) + r"', async ",
        )


if __name__ == "__main__":
    unittest.main()
