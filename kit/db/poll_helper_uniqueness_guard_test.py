"""The poll-helper guard accepts distinct helpers and rejects duplicate names."""

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


GUARD = Path(__file__).with_name("one_poll_helper_per_name_test.py")


class PollHelperUniquenessGuard(unittest.TestCase):
    def test_distinct_helpers_pass_and_either_duplicate_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            guard = fixture / GUARD.name
            guard.write_bytes(GUARD.read_bytes())
            for name in ("waitUntil", "waitForPoolStat"):
                (fixture / f"{name}_test.go").write_text(
                    f"package db_test\n\nfunc {name}() {{}}\n"
                )
            duplicate = fixture / "duplicate_test.go"
            for name in (None, "waitUntil", "waitForPoolStat"):
                with self.subTest(duplicate=name):
                    if name:
                        duplicate.write_bytes((fixture / f"{name}_test.go").read_bytes())
                    result = subprocess.run(
                        [sys.executable, "-B", str(guard)],
                        cwd=fixture, stdin=subprocess.DEVNULL,
                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                        text=True, timeout=30,
                    )
                    self.assertIn("Ran 1 test", result.stdout)
                    self.assertNotIn("ERROR", result.stdout)
                    self.assertEqual(result.returncode, 1 if name else 0, result.stdout)
                    if name:
                        self.assertIn(f"'{name}': 2", result.stdout)


if __name__ == "__main__":
    unittest.main()
