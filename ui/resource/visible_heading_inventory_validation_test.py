"""The visible-heading inventory assertion rejects missing or unrunnable cases."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "visible_heading_case_inventory", Path(__file__).with_name("visible_heading_case_inventory_test.py")
)
INVENTORY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(INVENTORY)


class VisibleHeadingInventoryValidation(unittest.TestCase):
    def test_missing_duplicate_or_unrunnable_heading_cases_are_rejected(self):
        file = "e2e/empty-list-visible-headings.spec.ts"
        rows = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        row = next(row for row in rows if row["file"] == file)
        source = (ROOT / file).read_text()
        case = INVENTORY.VisibleHeadingCaseInventory(
            "test_visible_empty_headings_have_one_runnable_journey_row"
        )
        variants = {
            "registered": ([row], source, False),
            "missing": ([], source, True),
            "duplicate": ([row, row], source, True),
            "wrong_tier": ([{**row, "tier": "push"}], source, True),
            "no_database": ([{**row, "needs_db": False}], source, True),
            "deleted": ([{**row, "verdict": "delete"}], source, True),
            "renamed_test": ([row], source.replace(row["name"], "another heading case"), True),
        }
        for name, (entries, declared, refused) in variants.items():
            with self.subTest(case=name), tempfile.TemporaryDirectory() as scratch:
                root = Path(scratch)
                (root / "tests").mkdir()
                (root / "e2e").mkdir()
                (root / "tests/inventory.json").write_text(json.dumps({"rows": entries}))
                (root / file).write_text(declared)
                with patch.object(INVENTORY, "ROOT", root):
                    if refused:
                        with self.assertRaises(AssertionError):
                            case.test_visible_empty_headings_have_one_runnable_journey_row()
                    else:
                        case.test_visible_empty_headings_have_one_runnable_journey_row()


if __name__ == "__main__":
    unittest.main()
