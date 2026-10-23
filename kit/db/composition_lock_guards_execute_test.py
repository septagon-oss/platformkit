"""The retained namespace-lock guards must execute after helper refactoring."""

import os
from pathlib import Path
import subprocess
import sys
import unittest


ROOT = Path(__file__).resolve().parents[2]


class CompositionLockGuardsExecute(unittest.TestCase):
    def test_retained_guards_reach_their_database_assertions(self):
        environment = dict(os.environ)
        # Exercise this checkout even when a caller used the observation pin's
        # source override to compare another revision earlier in the session.
        environment.pop("MIGRATION_WAIT_SOURCE", None)
        for filename in (
            "composition_key_observation_test.py",
            "composition_key_observation_rejects_wrong_locks_test.py",
        ):
            with self.subTest(guard=filename):
                result = subprocess.run(
                    [sys.executable, "-B", str(ROOT / "kit/db" / filename)],
                    cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                    text=True, timeout=400,
                )
                self.assertEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main()
