"""Plural citations remain detectable without forbidding ordinary numeric prose."""
from pathlib import Path
import runpy
import unittest


class PluralCommentCitations(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        boundary_case = runpy.run_path(str(Path(__file__).with_name(
            "comment_group_boundaries_test.py"
        )))["CommentGroupBoundaries"]
        boundary_case.setUpClass()
        cls.scanner = boundary_case.scanner

    def test_plural_citations_are_refused_across_comment_lines(self):
        for marker in ("//", "#"):
            for noun in ("rounds", "Rounds", "ROUNDS"):
                with self.subTest(marker=marker, noun=noun):
                    text = f"{marker} In {noun}\n{marker} 8 and 9 this was requested."
                    checked, refusals = self.scanner["scan"](
                        ["behavior_test.go"], lambda _: text
                    )
                    self.assertEqual(checked, 1)
                    self.assertEqual(len(refusals), 1)
                    self.assertIn("behavior_test.go:1:", refusals[0])

    def test_numeric_behavior_comments_remain_allowed(self):
        for text in ("// 5.2 to 5, with half rounded up.",
                     "// The worker rounded 5 values.",
                     "// A round trip reads 8 rows.",
                     "// Review 8 proposals before publishing."):
            with self.subTest(text=text):
                self.assertEqual(self.scanner["scan"](
                    ["behavior_test.go"], lambda _: text
                ), (1, []))


if __name__ == "__main__":
    unittest.main()
