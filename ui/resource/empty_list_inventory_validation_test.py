"""The empty-list inventory pin rejects missing or unrunnable typography cases."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "empty_list_floor_case_inventory", Path(__file__).with_name("empty_list_floor_case_inventory_test.py")
)
INVENTORY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(INVENTORY)


class EmptyListInventoryValidation(unittest.TestCase):
    def test_missing_duplicate_or_unrunnable_typography_cases_are_rejected(self):
        name = "TestAnEmptyGeneratedListDrawsNoParagraphSizeAListWithRowsDoesNot"
        rows = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        row = next(row for row in rows if row["name"] == name)
        source = (ROOT / row["file"]).read_text()
        case = INVENTORY.EmptyListFloorCaseInventory(
            "test_empty_list_typography_has_one_runnable_push_row"
        )
        variants = {
            "registered": ([row], source, False),
            "missing": ([], source, True),
            "duplicate": ([row, row], source, True),
            "wrong_tier": ([{**row, "tier": "nightly"}], source, True),
            "needs_database": ([{**row, "needs_db": True}], source, True),
            "deleted": ([{**row, "verdict": "delete"}], source, True),
            "renamed_function": ([row], source.replace("func " + name, "func Disabled" + name), True),
        }
        for name, (entries, declared, refused) in variants.items():
            with self.subTest(case=name), tempfile.TemporaryDirectory() as scratch:
                root = Path(scratch)
                (root / "tests").mkdir()
                (root / row["file"]).parent.mkdir(parents=True)
                (root / "tests/inventory.json").write_text(json.dumps({"rows": entries}))
                (root / row["file"]).write_text(declared)
                with patch.object(INVENTORY, "ROOT", root):
                    if refused:
                        with self.assertRaises(AssertionError):
                            case.test_empty_list_typography_has_one_runnable_push_row()
                    else:
                        case.test_empty_list_typography_has_one_runnable_push_row()


if __name__ == "__main__":
    unittest.main()
