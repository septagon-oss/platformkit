"""Each delivery commit states its verification and what remains unverified."""

import os
import unittest

from every_commit_is_inside_its_loc_budget_test import git


class CommitEvidenceParagraphs(unittest.TestCase):
    def test_each_branch_commit_names_verified_and_unverified_work(self):
        base = git("merge-base", os.environ.get("BASE", "origin/main"), "HEAD").strip()
        commits = git("rev-list", "--reverse", "--no-merges", base + "..HEAD").split()
        self.assertTrue(commits, "No delivery commits were inspected")
        for commit in commits:
            subject = git("show", "-s", "--format=%h %s", commit).strip()
            body = git("show", "-s", "--format=%B", commit)
            with self.subTest(commit=subject):
                self.assertRegex(body, r"(?m)^Verified:\s*$")
                self.assertRegex(body, r"(?m)^Not verified:",
                                 "Every commit must state its unverified scope, "
                                 "including when nothing remains unverified")


if __name__ == "__main__":
    unittest.main()
