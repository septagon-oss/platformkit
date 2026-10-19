"""Composing the migration guard preserves the base inventory's decisions."""
import json
from pathlib import Path
import runpy
import unittest


ROOT = Path(__file__).resolve().parents[2]
PIN = runpy.run_path(str(ROOT / "modules/audit/applied_migration_inventory_test.py"))
INVENTORY = PIN["INVENTORY"]


class AppliedMigrationCompositionInventory(unittest.TestCase):
    def test_composition_preserves_base_rows_and_adds_only_migration_cases(self):
        base = INVENTORY["git"]("merge-base", "HEAD", "origin/main")
        previous = INVENTORY["rows"](base)
        current = {row["id"]: row for row in
                   json.loads((ROOT / "tests/inventory.json").read_text())["rows"]}
        accepted = INVENTORY["rows"](PIN["ACCEPTED"])
        paths = {"scripts/check_applied_migrations_test.sh",
                 "scripts/applied_migration_merge_base_test.sh"}
        additions = {identifier for identifier, row in accepted.items()
                     if row["file"].startswith("modules/audit/applied_migration")
                     or row["file"] in paths}
        additions.add("modules/audit/applied_migration_conversion_index_test.go#"
                      "TestAppliedMigrationGateChecksRawBytesAndTheIndexTogether")
        self.assertEqual(set(current), set(previous) | additions)
        for identifier, row in previous.items():
            with self.subTest(test=identifier):
                self.assertEqual(current[identifier], row)


if __name__ == "__main__":
    unittest.main()
