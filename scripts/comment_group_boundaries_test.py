"""Comment citations stay detectable across wrapping without joining separate groups."""
from pathlib import Path
import unittest


class CommentGroupBoundaries(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = Path(__file__).with_name(
            "test_comments_explain_what_they_protect_test.sh"
        ).read_text()
        python = source.split("<<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        definitions = python.split("\ntracked =", 1)[0]
        cls.scanner = {}
        exec(compile(definitions, str(Path(__file__)), "exec"), cls.scanner)

    def scan(self, text):
        checked, refusals = self.scanner["scan"](
            ["behavior_test.go"], lambda _: text
        )
        self.assertEqual(checked, 1)
        return refusals

    def test_wrapped_citation_at_end_of_file_is_refused(self):
        for marker in ("//", "#"):
            for newline in ("\n", "\r\n"):
                with self.subTest(marker=marker, newline=newline):
                    text = f"\n\t{marker} Review{newline}\t{marker} round 14"
                    refusals = self.scan(text)
                    self.assertEqual(len(refusals), 1)
                    self.assertIn("behavior_test.go:2:", refusals[0])

    def test_code_and_blank_lines_separate_comment_groups(self):
        for separator in ("\n", "func helper() {}\n"):
            with self.subTest(separator=separator):
                self.assertEqual(self.scan("// Review\n" + separator + "// policy"), [])
                groups = self.scanner["comment_runs"](
                    "// Review\n" + separator + "// policy"
                )
                self.assertEqual([body for _, body in groups], ["Review", "policy"])

    def test_domain_round_trip_and_review_are_allowed(self):
        self.assertEqual(self.scan(
            "// Review the proposal after a round trip.\n"
            "// A background worker rounded 5 values."
        ), [])


if __name__ == "__main__":
    unittest.main()
