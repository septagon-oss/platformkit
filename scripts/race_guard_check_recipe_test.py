"""The race guard runs from the check recipe and has a matching inventory row."""
import json
import os
from pathlib import Path
import subprocess
import unittest


class RaceGuardCheckRecipe(unittest.TestCase):
    def test_check_runs_the_inventoried_race_guard(self):
        root = Path(__file__).resolve().parents[1]
        guard = "scripts/race_boundary_guard_test.py"
        environment = dict(os.environ)
        for name in ("MAKEFLAGS", "MAKEOVERRIDES", "TEST_COUNT"):
            environment.pop(name, None)
        result = subprocess.run(
            ["make", "--no-print-directory", "-n", "check"],
            cwd=root, env=environment, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        command = "PYTHONDONTWRITEBYTECODE=1 python3 " + guard
        executable = [line for line in result.stdout.splitlines()
                      if not line.lstrip().startswith("#") and guard in line]
        self.assertEqual(executable, [command])
        inventory = json.loads((root / "tests/inventory.json").read_text())
        rows = [row for row in inventory["rows"] if row["file"] == guard]
        self.assertEqual(len(rows), 1)
        row = rows[0]
        self.assertEqual(row["id"], guard)
        self.assertEqual(row["kind"], "shell_pin")
        self.assertEqual(row["tier"], "push")
        self.assertFalse(row["needs_db"])
        self.assertFalse(row["round_named"])
        self.assertEqual(row["verdict"], "keep")


if __name__ == "__main__":
    unittest.main()
