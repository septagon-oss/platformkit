"""The install guard rejects swallowed refusals in both consuming workflows."""
import io
from pathlib import Path
import runpy
import subprocess
import unittest
from unittest.mock import patch


class PackageInstallRefusalGuard(unittest.TestCase):
    def test_swallowed_install_failure_fails_both_workflow_cases(self):
        case = runpy.run_path(str(Path(__file__).with_name(
            "ci_package_install_refusal_test.py"
        )))["PackageInstallRefusal"]
        original_run = subprocess.run

        def swallow_install_failure(args, **kwargs):
            args = list(args)
            args[-1] = "\n".join(
                line + " || true" if line.strip().startswith("apt-get install ")
                else line for line in args[-1].splitlines()
            )
            return original_run(args, **kwargs)

        for mutated in (False, True):
            with self.subTest(swallowed=mutated):
                output = io.StringIO()
                runner = unittest.TextTestRunner(stream=output)
                suite = unittest.defaultTestLoader.loadTestsFromTestCase(case)
                replacement = swallow_install_failure if mutated else original_run
                with patch.object(subprocess, "run", replacement):
                    result = runner.run(suite)
                self.assertEqual(result.testsRun, 1, output.getvalue())
                self.assertEqual(result.errors, [], output.getvalue())
                self.assertEqual(len(result.failures), 2 if mutated else 0,
                                 output.getvalue())
                self.assertEqual(result.skipped, [], output.getvalue())


if __name__ == "__main__":
    unittest.main()
