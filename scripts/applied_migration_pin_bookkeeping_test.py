import json
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
ACCEPTED = "471714851c050318aab835e1294deb02fe58f54f"
PRICE = "333c9ef2137ae806867edc5afdef2f4286ccaacf"
ADOPTION = "e7913cd658b9c355c968ab6fd7d17c5d896afa21"
PIN = "migrations/applied_migration_bytes_test.go"
NAME = "TestAppliedModuleMigrationsKeepTheirOriginalBytes"


def git(*args):
    return subprocess.check_output(
        ["git", *args], cwd=ROOT, stdin=subprocess.DEVNULL, timeout=30
    )


class AppliedMigrationPinBookkeeping(unittest.TestCase):
    def test_the_applied_migration_pin_is_in_the_database_tier(self):
        inventory = json.loads((ROOT / "tests/inventory.json").read_text())
        rows = [row for row in inventory["rows"] if row["id"] == PIN + "#" + NAME]
        self.assertEqual(len(rows), 1)
        for key, value in {
            "file": PIN,
            "name": NAME,
            "pkg": "migrations",
            "kind": "go",
            "layer": "behaviour",
            "tier": "merge",
            "needs_db": True,
            "round_named": False,
            "verdict": "keep",
        }.items():
            with self.subTest(field=key):
                self.assertEqual(rows[0][key], value)

    def test_the_adoption_prices_only_its_own_test_lines_before_the_test(self):
        self.assertEqual(git("rev-parse", PRICE + "^").decode().strip(), ACCEPTED)
        self.assertEqual(git("rev-parse", ADOPTION + "^").decode().strip(), PRICE)
        self.assertEqual(
            git("diff-tree", "--no-commit-id", "--name-only", "-r", PRICE).splitlines(),
            [b"loc-budget.json"],
        )
        before = json.loads(git("show", ACCEPTED + ":loc-budget.json"))
        priced = json.loads(git("show", PRICE + ":loc-budget.json"))
        added_lines = git("show", ADOPTION + ":" + PIN).count(b"\n")
        for old, new in zip(before["buckets"], priced["buckets"], strict=True):
            expected = dict(old)
            if old["name"] == "go_test":
                expected["max"] += added_lines
            with self.subTest(bucket=old["name"]):
                self.assertEqual(new, expected)


if __name__ == "__main__":
    unittest.main()
