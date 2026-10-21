"""The empty-list typography assertion remains a runnable stack-free case."""
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class EmptyListFloorCaseInventory(unittest.TestCase):
    def test_empty_list_typography_has_one_runnable_push_row(self):
        file = "ui/resource/generated_screen_floor_test.go"
        name = "TestAnEmptyGeneratedListDrawsNoParagraphSizeAListWithRowsDoesNot"
        rows = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        matches = [row for row in rows if row["name"] == name]
        self.assertEqual(len(matches), 1, "empty-list typography needs exactly one inventory row")
        expected = {
            "id": file + "#" + name,
            "file": file,
            "name": name,
            "pkg": "ui/resource",
            "kind": "go",
            "layer": "behaviour",
            "tier": "push",
            "needs_db": False,
            "verdict": "keep",
        }
        self.assertEqual({key: matches[0][key] for key in expected}, expected)
        self.assertRegex(
            (ROOT / file).read_text(),
            r"(?m)^func " + re.escape(name) + r"\(t \*testing\.T\) \{",
        )


if __name__ == "__main__":
    unittest.main()
