"""The migration inventory pin refuses rewritten verdicts and execution tiers."""
import copy
import json
from pathlib import Path
import runpy
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
PIN = runpy.run_path(str(ROOT / "modules/audit/applied_migration_inventory_test.py"))


class AppliedMigrationDecisionRefusals(unittest.TestCase):
    def test_owned_inventory_pin_refuses_changed_migration_verdicts_and_tiers(self):
        inventory_path = ROOT / "tests/inventory.json"
        document = json.loads(inventory_path.read_text())
        paths = {"scripts/check_applied_migrations_test.sh",
                 "scripts/applied_migration_merge_base_test.sh"}
        owned = [row for row in document["rows"]
                 if row["file"].startswith("modules/audit/applied_migration")
                 or row["file"] in paths]
        self.assertTrue(owned)
        original_read = Path.read_text

        def check(candidate):
            def read(path, *args, **kwargs):
                if path == inventory_path:
                    return json.dumps(candidate)
                return original_read(path, *args, **kwargs)

            case = PIN["AppliedMigrationInventory"](
                "test_composed_inventory_retains_applied_migration_tests_and_decisions")
            result = unittest.TestResult()
            with patch.object(Path, "read_text", read):
                case.run(result)
            self.assertEqual(result.testsRun, 1)
            self.assertEqual(result.errors, [])
            self.assertEqual(result.skipped, [])
            return result

        self.assertTrue(check(document).wasSuccessful())
        for row in owned:
            for field, choices in (("verdict", ("keep", "delete")),
                                   ("tier", ("push", "merge"))):
                with self.subTest(test=row["id"], field=field):
                    candidate = copy.deepcopy(document)
                    changed = next(item for item in candidate["rows"]
                                   if item["id"] == row["id"])
                    changed[field] = next(value for value in choices if value != row[field])
                    self.assertEqual(len(check(candidate).failures), 1)


if __name__ == "__main__":
    unittest.main()
