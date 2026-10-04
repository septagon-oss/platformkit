"""Cancellation must return an error without crashing the migration process.

Uses the existing database-backed cancellation cases under the race detector.
Every repetition must pass; a failing attempt is never retried into success.
Supply the same PLATFORMKIT_TEST_* database URLs as for the Go package.
"""

from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]


class MigrationCancellationReturnsError(unittest.TestCase):
    def test_cancelled_migrations_return_without_crashing(self):
        result = subprocess.run(
            ["go", "test", "-race", "./kit/db", "-run",
             "^(TestMigrationCancellationRollsBackAndReleasesTheLock|"
             "TestACancelledMigrationHoldsNoCompositionLockOnceItReturns)$",
             "-count=200", "-timeout=5m"],
            cwd=ROOT,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=330,
        )
        self.assertEqual(
            result.returncode, 0,
            "Cancellation must return its error, roll back, and release the "
            "composition lock without panicking:\n" + result.stdout,
        )


if __name__ == "__main__":
    unittest.main()
