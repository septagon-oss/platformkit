"""make check and both CI workflows still run the applied-migration gate.

The gate is only as good as its wiring: a merge that drops it from the check goal or a
workflow leaves every installation's boot to whoever remembers to run it, and no other
case fails.
"""
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOWS = (".github/workflows/ci.yml", ".gitea/workflows/ci.yml")


def goal(makefile, name):
    """The prerequisites and the recipe of the goal NAME in MAKEFILE's text."""
    lines = makefile.splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith(name + ":"))
    end = next((i for i in range(start + 1, len(lines))
                if lines[i].strip() and not lines[i].startswith("\t")), len(lines))
    prerequisites = lines[start][len(name) + 1:].split("##")[0].split()
    return prerequisites, [line.strip() for line in lines[start + 1:end]]


def steps_running(workflow, command):
    """Each step of WORKFLOW's text whose run line is COMMAND, as its lines."""
    lines = workflow.splitlines()
    found = []
    for i, line in enumerate(lines):
        if line.strip() != "run: " + command:
            continue
        top = next(j for j in range(i, -1, -1) if lines[j].lstrip().startswith("- "))
        found.append(lines[top:i + 1])
    return found


class AppliedMigrationGateWiring(unittest.TestCase):
    def test_make_check_and_both_ci_workflows_run_the_applied_migration_gate(self):
        makefile = (ROOT / "Makefile").read_text()
        prerequisites, recipe = goal(makefile, "check")
        with self.subTest(goal="check", needs="check-applied-migrations"):
            self.assertIn("check-applied-migrations", prerequisites)
        for case in ("bash scripts/check_applied_migrations_test.sh",
                     "bash scripts/applied_migration_merge_base_test.sh"):
            with self.subTest(goal="check", runs=case):
                self.assertIn(case, recipe)
        _, gate = goal(makefile, "check-applied-migrations")
        with self.subTest(goal="check-applied-migrations"):
            self.assertIn("./scripts/check_applied_migrations.sh $(MIGRATIONS_BASE)", gate)
        for path in WORKFLOWS:
            with self.subTest(workflow=path):
                steps = steps_running((ROOT / path).read_text(), "make check-applied-migrations")
                self.assertEqual(len(steps), 1)
                self.assertTrue(any(re.match(r"\s*MIGRATIONS_BASE: \S", line)
                                    for line in steps[0]))


if __name__ == "__main__":
    unittest.main()
