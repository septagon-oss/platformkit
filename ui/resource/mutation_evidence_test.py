"""Mutation evidence requires the named Go case and its expected assertion."""
import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "command_controls_mutations", Path(__file__).with_name("command_controls_mutations_test.py")
)
MUTATIONS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MUTATIONS)


class MutationEvidence(unittest.TestCase):
    def expected_failures(self):
        return [
            subprocess.CompletedProcess([], 1, "--- FAIL: " + MUTATIONS.PIN + "\n" + reason)
            for reason in (
                "command form lost its POST method or accessible name",
                "native submits; want one each",
                "command submit must keep the quiet outline variant",
            )
        ]

    def run_mutations(self):
        case = MUTATIONS.CommandControlMutations(
            "test_control_regressions_are_rejected_by_the_native_submit_pin"
        )
        case.test_control_regressions_are_rejected_by_the_native_submit_pin()

    def test_each_named_assertion_is_accepted_as_mutation_evidence(self):
        with patch.object(MUTATIONS.subprocess, "run", side_effect=self.expected_failures()) as run:
            self.run_mutations()
        self.assertEqual(run.call_count, 3)

    def test_passes_compiler_errors_and_unrelated_assertions_are_rejected(self):
        for index, expected in enumerate(self.expected_failures()):
            invalid_results = {
                "unexpected_pass": subprocess.CompletedProcess([], 0, expected.stdout),
                "compiler_error": subprocess.CompletedProcess([], 1, "syntax error: unexpected token"),
                "unrelated_assertion": subprocess.CompletedProcess(
                    [], 1, "--- FAIL: " + MUTATIONS.PIN + "\nother assertion failed"
                ),
                "wrong_case": subprocess.CompletedProcess(
                    [], 1, expected.stdout.replace(MUTATIONS.PIN, "TestOtherCase")
                ),
            }
            for name, result in invalid_results.items():
                with self.subTest(evidence=name, mutation=index):
                    results = self.expected_failures()
                    results[index] = result
                    with patch.object(MUTATIONS.subprocess, "run", side_effect=results) as run:
                        with self.assertRaises(AssertionError):
                            self.run_mutations()
                    self.assertEqual(run.call_count, index + 1)


if __name__ == "__main__":
    unittest.main()
