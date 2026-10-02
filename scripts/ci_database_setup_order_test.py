"""The database gates must create the application role before running tests."""

import os
from pathlib import Path
import re
import unittest


WORKFLOW = Path(
    os.environ.get(
        "CI_WORKFLOW_FILE",
        Path(__file__).resolve().parents[1] / ".gitea/workflows/ci.yml",
    )
)


class DatabaseSetupOrderTest(unittest.TestCase):
    def test_database_gates_create_the_application_role_before_the_suite(self):
        lines = WORKFLOW.read_text().splitlines()
        for job, goal in (
            ("go-checks", "make check"),
            ("race-and-vuln", "make check-race"),
            ("e2e", "make e2e"),
        ):
            with self.subTest(job=job):
                marker = f"  {job}:"
                self.assertIn(marker, lines, f"{job} is missing from {WORKFLOW}")
                steps = []
                for line in lines[lines.index(marker) + 1 :]:
                    if re.fullmatch(r"  [a-z0-9-]+:", line):
                        break
                    step = line.strip()
                    steps.append(step.removeprefix("- "))

                setup = "run: bash scripts/ci_setup.sh nats app-role"
                gate = f"run: {goal}"
                self.assertIn(setup, steps, f"{job} does not create the application role")
                self.assertIn(gate, steps, f"{job} does not run {goal}")
                self.assertLess(
                    steps.index(setup),
                    steps.index(gate),
                    f"{job} creates the application role after {goal}",
                )


if __name__ == "__main__":
    unittest.main()
