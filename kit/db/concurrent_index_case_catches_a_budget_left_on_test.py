"""The concurrent-index case refuses a runner that leaves its lock budget on.

TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader
says it still carries run 189's regression as the statement's own error: a
session that keeps its lock_timeout for the autocommit statement answers
SQLSTATE 55P03 and the leg fails. This copies the tracked tree, puts that
regression back (stepAwayFromTheCompositionLock no longer puts the lock budget
down), and requires the case to fail on the copy.

The window put *both* budgets off until b4b68ee (2026-10-08) — "a
nontransactional file waits its own wait; the lock budget does not reach it"
— and now puts the lock budget off while leaving the statement budget exactly
as the deployment configured it. The regression this case reproduces is the
lock half, which is the half that cancels a build waiting to be outlived, so
the mutation is the same one and only its anchor moved with that commit.

Supply the same PLATFORMKIT_TEST_* database URLs as for the Go package.
"""

from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
CASE = "TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader"
LOCK_BUDGET_OFF = """	if err := r.setBudgets(ctx, noLockBudget); err != nil {
		return err
	}
	return r.releaseCompositionLock(ctx)"""


class ConcurrentIndexCaseCatchesABudgetLeftOn(unittest.TestCase):
    def test_the_case_fails_when_the_statement_keeps_its_lock_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            copy = Path(directory)
            files = subprocess.run(["git", "ls-files", "-z"], cwd=ROOT,
                                   check=True, stdout=subprocess.PIPE).stdout
            pack = subprocess.run(["tar", "--null", "-T", "-", "-c"], cwd=ROOT,
                                  input=files, check=True,
                                  stdout=subprocess.PIPE).stdout
            subprocess.run(["tar", "-x"], cwd=copy, input=pack, check=True)
            migrate = copy / "kit" / "db" / "migrate.go"
            source = migrate.read_text()
            self.assertIn(LOCK_BUDGET_OFF, source,
                          "the window no longer puts its lock budget down "
                          "where this case expects; re-anchor the regression")
            migrate.write_text(source.replace(
                LOCK_BUDGET_OFF, "	return r.releaseCompositionLock(ctx)"))
            result = subprocess.run(
                ["go", "test", "./kit/db", "-run", "^" + CASE + "$",
                 "-count=1", "-v", "-timeout=4m"],
                cwd=copy,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                timeout=300,
            )
        self.assertIn("=== RUN   " + CASE, result.stdout,
                      "the case never ran:\n" + result.stdout)
        self.assertNotEqual(
            result.returncode, 0,
            "With the lock budget left on the session for the autocommit "
            "statement, the case must fail, and it passed:\n" + result.stdout,
        )


if __name__ == "__main__":
    unittest.main()
