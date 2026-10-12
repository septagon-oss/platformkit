"""An allowance test's path change preserves its source and inventory decisions."""
import json
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]
BEFORE = "1418fb10a4599ea21eeee1d9e1a54f3b333f2257"
OLD = "kit/wire/reviewed_widening_gains_no_feature_test.go"
NEW = "kit/wire/an_allowance_pair_over_the_kind_covers_no_feature_the_door_gained_test.go"
CASE = "TestAReviewedWideningCoversNoFeatureTheDoorGained"


def committed(path):
    return subprocess.check_output(
        ["git", "show", f"{BEFORE}:{path}"], cwd=ROOT,
        stdin=subprocess.DEVNULL, text=True, timeout=30,
    )


class AllowanceRenamePreservesAssertions(unittest.TestCase):
    def test_renamed_allowance_case_keeps_every_source_line(self):
        self.assertEqual((ROOT / NEW).read_text(), committed(OLD))
        self.assertFalse((ROOT / OLD).exists())
        consumer = (ROOT / "kit/wire/authorization_inventory_test.sh").read_text()
        self.assertIn(f'"{Path(NEW).name}": ("{CASE}",)', consumer)
        self.assertNotIn(Path(OLD).name, consumer)

    def test_renamed_allowance_row_keeps_its_decisions_and_assertions(self):
        previous = json.loads(committed("tests/inventory.json"))["rows"]
        current = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        original = [row for row in previous if row["id"] == OLD + "#" + CASE]
        renamed = [row for row in current if row["id"] == NEW + "#" + CASE]
        self.assertEqual(len(original), 1)
        self.assertEqual(len(renamed), 1)
        expected = dict(original[0])
        expected.update(file=NEW, id=NEW + "#" + CASE,
                        rename_to=Path(NEW).name, round_named=False)
        expected.setdefault("rule_present", False)
        self.assertEqual(renamed[0], expected)
        self.assertFalse(any(row["file"] == OLD for row in current))


if __name__ == "__main__":
    unittest.main()
