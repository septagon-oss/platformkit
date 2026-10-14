"""The inventory merge retains each parent's test rows and ratified decisions."""
import json
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
MERGE = "7bc328f214e3d5658e6ce2cd731fed3af2b9d745"


def git(*args):
    return subprocess.check_output(
        ["git", *args], cwd=ROOT, stdin=subprocess.DEVNULL,
        text=True, timeout=30,
    ).strip()


def rows(ref):
    document = json.loads(git("show", ref + ":tests/inventory.json"))
    return {row["id"]: row for row in document["rows"]}


def normalized(row):
    result = dict(row)
    result.setdefault("rule_present", False)
    return result


class InventoryMergePreservesDecisions(unittest.TestCase):
    def test_inventory_retains_both_parents_rows_without_rewriting_decisions(self):
        first, second = git("rev-parse", MERGE + "^1", MERGE + "^2").split()
        base = git("merge-base", first, second)
        before, incoming, ancestor = map(rows, (first, second, base))
        retained = rows(MERGE)
        expected = dict(before)
        expected.update({key: row for key, row in incoming.items() if key not in ancestor})
        self.assertEqual(set(retained), set(expected))
        for identifier, row in expected.items():
            with self.subTest(test=identifier):
                self.assertIn(identifier, retained)
                self.assertEqual(normalized(retained[identifier]), normalized(row))

    def test_merge_retains_the_incoming_budget_files_whole(self):
        for path in ("loc-budget.json", "packages-budget.json"):
            with self.subTest(file=path):
                self.assertEqual(git("show", MERGE + ":" + path),
                                 git("show", MERGE + "^2:" + path))


if __name__ == "__main__":
    unittest.main()
