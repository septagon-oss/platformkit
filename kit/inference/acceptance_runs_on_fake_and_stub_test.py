"""Check that each behaviour the port's acceptance names has a case in the one
suite, and that the suite and the prompt redaction run on the fake and the stub."""

from pathlib import Path
import re
import unittest


PACKAGE = Path(__file__).resolve().parent

# Each acceptance behaviour, matched against the suite's subtest names.
BEHAVIOURS = {
    "deadline": r"deadline",
    "cancellation": r"cancel",
    "oversize answer": r"answer over the bound",
    "cap exhaustion": r"allowance is spent",
}


def body(source, name):
    """The text of func name, up to the next top-level func."""
    match = re.search(rf"^func {name}\(.*?(?=^func |\Z)", source, re.M | re.S)
    if not match:
        raise AssertionError(f"{name} is not declared")
    return match.group(0)


class AcceptanceRunsOnFakeAndStub(unittest.TestCase):
    def setUp(self):
        self.tests = (PACKAGE / "inference_test.go").read_text()

    def test_each_named_behaviour_has_a_case_in_the_one_suite(self):
        suite = (PACKAGE / "inferencetest" / "conformance.go").read_text()
        cases = re.findall(r'\bt\.Run\("([^"]+)"', suite)
        for behaviour, pattern in BEHAVIOURS.items():
            with self.subTest(behaviour=behaviour):
                self.assertTrue([c for c in cases if re.search(pattern, c)], cases)

    def test_the_suite_and_the_redaction_run_on_both_legs(self):
        self.assertIn("inference.NewScript(", body(self.tests, "scriptLeg"))
        self.assertIn("httptest.NewServer(", body(self.tests, "openaiLeg"))
        suite = body(self.tests, "TestEveryAdapterRunsTheOneSuite")
        self.assertRegex(suite, r"Conformance\(t, scriptLeg, openaiLeg\)")
        redaction = body(self.tests, "TestNoPictureReachesATraceAttributeEventOrError")
        self.assertRegex(redaction, r"\{scriptLeg, openaiLeg\}")
        self.assertRegex(redaction, r"range \[\]string\{prompt\b")
        self.assertIn("span.Attributes()", redaction)


if __name__ == "__main__":
    unittest.main()
