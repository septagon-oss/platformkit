"""Every commit of this branch is inside the line budget its own tree names.

The workspace prices a budget in its own build(budget) commit first, so the
code that needs the room never lands over its ceiling: each commit's tree, on
its own, passes `go run ./tools/locbudget --check`. This walks the branch's
own commits (merge base with BASE, default origin/main, to HEAD; merges are
left out) and runs that check on each commit's tracked files.
"""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


def git(*args):
    return subprocess.run(["git", *args], cwd=ROOT, check=True, text=True,
                          stdout=subprocess.PIPE).stdout


class EveryCommitIsInsideItsLocBudget(unittest.TestCase):
    def test_each_commit_of_the_branch_passes_the_budget_check(self):
        base = git("merge-base", os.environ.get("BASE", "origin/main"), "HEAD").strip()
        commits = git("rev-list", "--reverse", "--no-merges", base + "..HEAD").split()
        self.assertTrue(commits, "the branch has no commits of its own above " + base)
        over = []
        for commit in commits:
            with tempfile.TemporaryDirectory() as directory:
                tree = Path(directory)
                archive = subprocess.run(["git", "archive", commit], cwd=ROOT,
                                         check=True, stdout=subprocess.PIPE).stdout
                subprocess.run(["tar", "-x"], cwd=tree, input=archive, check=True)
                # locbudget counts what `git ls-files` names.
                subprocess.run(["git", "init", "-q"], cwd=tree, check=True)
                subprocess.run(["git", "add", "-A"], cwd=tree, check=True)
                result = subprocess.run(
                    ["go", "run", "./tools/locbudget", "--check"], cwd=tree,
                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT, text=True, timeout=300)
            if result.returncode != 0:
                subject = git("log", "-1", "--format=%h %s", commit).strip()
                lines = [l for l in result.stdout.splitlines() if "OVER BUDGET" in l]
                over.append(subject + "\n    " + "\n    ".join(lines or [result.stdout]))
        self.assertEqual(over, [], "commits over the budget their own tree names:\n"
                         + "\n".join(over))


if __name__ == "__main__":
    unittest.main()
