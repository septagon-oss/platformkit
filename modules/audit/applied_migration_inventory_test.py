"""The applied-migration tests retain their inventory decisions after composition."""
import json
from pathlib import Path
import runpy
import unittest


ROOT = Path(__file__).resolve().parents[2]
INVENTORY = runpy.run_path(str(ROOT / "scripts/inventory_merge_preserves_decisions_test.py"))
ACCEPTED = "58bf6055f8392bde5913cf85f38fc344566aa75b"


class AppliedMigrationInventory(unittest.TestCase):
    def test_composed_inventory_retains_applied_migration_tests_and_decisions(self):
        previous = INVENTORY["rows"](ACCEPTED)
        current = {row["id"]: row for row in
                   json.loads((ROOT / "tests/inventory.json").read_text())["rows"]}
        paths = {
            "scripts/check_applied_migrations_test.sh",
            "scripts/applied_migration_merge_base_test.sh",
        }
        owned = {key: row for key, row in previous.items()
                 if row["file"].startswith("modules/audit/applied_migration")
                 or row["file"] in paths}
        self.assertTrue(owned)
        for identifier, row in owned.items():
            with self.subTest(test=identifier):
                self.assertIn(identifier, current)
                self.assertEqual(current[identifier], row)
        path = "modules/audit/applied_migration_conversion_index_test.go"
        name = "TestAppliedMigrationGateChecksRawBytesAndTheIndexTogether"
        adopted = current[path + "#" + name]
        self.assertIn("func " + name + "(", (ROOT / path).read_text())
        for field, value in {"verdict": "keep", "tier": "merge",
                             "layer": "behaviour", "kind": "go"}.items():
            with self.subTest(field=field):
                self.assertEqual(adopted[field], value)


if __name__ == "__main__":
    unittest.main()
