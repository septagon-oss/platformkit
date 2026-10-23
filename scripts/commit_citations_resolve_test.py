"""Each commit a delivery commit cites is one a reader of the merged tree can open.

A body that points at another commit's Verified paragraph, or at the commit that priced
a ceiling, is evidence only while that commit exists where the reader looks: on the
branch the merge carries, or on a branch the forge keeps. A rebase gives every later
commit a new name, so a citation written before it names an object nothing ships.
"""

import os
import re
import unittest

from every_commit_is_inside_its_loc_budget_test import git


CITATION = re.compile(r"`([0-9a-f]{7,40})`")


def resolves(name):
    try:
        git("merge-base", "--is-ancestor", name, "HEAD")
        return True
    except Exception:
        pass
    try:
        return bool(git("branch", "-r", "--contains", name).strip())
    except Exception:
        return False


class CommitCitationsResolve(unittest.TestCase):
    def test_each_commit_a_branch_commit_cites_is_one_the_merge_or_the_forge_carries(self):
        base = git("merge-base", os.environ.get("BASE", "origin/main"), "HEAD").strip()
        commits = git("rev-list", "--reverse", "--no-merges", base + "..HEAD").split()
        self.assertTrue(commits, "No delivery commits were inspected")
        for commit in commits:
            subject = git("show", "-s", "--format=%h %s", commit).strip()
            body = git("show", "-s", "--format=%B", commit)
            for name in sorted(set(CITATION.findall(body))):
                with self.subTest(commit=subject, cites=name):
                    self.assertTrue(resolves(name),
                                    f"{subject} cites `{name}`, which is neither on this "
                                    "branch nor on any branch the forge keeps")


if __name__ == "__main__":
    unittest.main()
