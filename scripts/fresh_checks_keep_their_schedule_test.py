"""Local test selectors cannot change the full check's measured schedule."""

from pathlib import Path
import shlex
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]


class FreshChecksKeepTheirSchedule(unittest.TestCase):
    def test_local_selectors_cannot_shrink_or_reschedule_the_fresh_suite(self):
        for goals in (("check",), ("test", "check"), ("check", "test")):
            with self.subTest(goals=goals):
                result = subprocess.run(
                    ["make", "--no-print-directory", "-n", *goals,
                     "TEST_PACKAGES=./design", "TEST_FLAGS=-run Nothing -p=1 -short",
                     "TEST_OPTIONS=--watch"],
                    cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True,
                    text=True, timeout=30,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                commands = [shlex.split(line) for line in result.stdout.splitlines()
                            if line.startswith("go tool gotestsum ")]
                fresh = [args for args in commands if "-count=1" in args]
                self.assertEqual(len(fresh), 1, commands)
                args = fresh[0]
                self.assertIn("--packages=./...", args)
                self.assertIn("-p=8", args)
                self.assertIn("-timeout=25m", args)
                for local_option in ("--watch", "-run", "Nothing", "-p=1", "-short"):
                    self.assertNotIn(local_option, args)
                local = [args for args in commands if "--watch" in args]
                self.assertEqual(len(local), int("test" in goals), commands)
                if local:
                    self.assertIn("--packages=./design", local[0])
                    self.assertIn("-p=1", local[0])


if __name__ == "__main__":
    unittest.main()
