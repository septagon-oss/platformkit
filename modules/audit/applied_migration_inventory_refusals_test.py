"""The composition inventory pin refuses lost or rewritten base decisions."""
import copy
import json
from pathlib import Path
import runpy
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
PIN = runpy.run_path(str(ROOT / "modules/audit/applied_migration_composition_inventory_test.py"))


class AppliedMigrationInventoryRefusals(unittest.TestCase):
    def test_composition_pin_refuses_lost_rewritten_and_unrelated_rows(self):
        inventory_path = ROOT / "tests/inventory.json"
        document = json.loads(inventory_path.read_text())
        base = PIN["INVENTORY"]["git"]("merge-base", "HEAD", "origin/main")
        identifier = next(iter(PIN["INVENTORY"]["rows"](base)))
        original_read = Path.read_text

        def check(candidate):
            def read(path, *args, **kwargs):
                if path == inventory_path:
                    return json.dumps(candidate)
                return original_read(path, *args, **kwargs)

            case = PIN["AppliedMigrationCompositionInventory"](
                "test_composition_preserves_base_rows_and_adds_only_migration_cases")
            result = unittest.TestResult()
            with patch.object(Path, "read_text", read):
                case.run(result)
            self.assertEqual(result.errors, [])
            return result

        self.assertTrue(check(document).wasSuccessful())
        removed = copy.deepcopy(document)
        removed["rows"] = [row for row in removed["rows"] if row["id"] != identifier]
        rewritten = copy.deepcopy(document)
        row = next(row for row in rewritten["rows"] if row["id"] == identifier)
        row["verdict"] = "keep" if row["verdict"] != "keep" else "delete"
        unrelated = copy.deepcopy(document)
        row = copy.deepcopy(document["rows"][0])
        row["id"] += "-unrelated"
        unrelated["rows"].append(row)
        for name, candidate in (("removed base row", removed),
                                ("rewritten base verdict", rewritten),
                                ("unrelated addition", unrelated)):
            with self.subTest(change=name):
                self.assertTrue(check(candidate).failures)


if __name__ == "__main__":
    unittest.main()
