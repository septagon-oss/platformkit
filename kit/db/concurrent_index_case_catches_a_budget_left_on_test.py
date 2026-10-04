"""The concurrent-index case refuses a runner that leaves its lock budget on.

TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader
says it still carries run 189's regression as the statement's own error: a
session that keeps its lock_timeout for the autocommit statement answers
SQLSTATE 55P03 and the leg fails. This copies the tracked tree, puts that
regression back (stepAwayFromTheCompositionLock no longer takes the budgets
off), and requires the case to fail on the copy.

Supply the same PLATFORMKIT_TEST_* database URLs as for the Go package.
"""

from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
CASE = "TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader"
BUDGETS_OFF = """	if err := r.setBudgets(ctx, unbudgeted); err != nil {
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
            self.assertIn(BUDGETS_OFF, source,
                          "the window no longer takes its budgets off where "
                          "this case expects; re-anchor the regression")
            migrate.write_text(source.replace(
                BUDGETS_OFF, "	return r.releaseCompositionLock(ctx)"))
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
