"""The race-bound comparison requires a stated bound on the ordinary suite too."""

from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class RaceGuardSuiteBound(unittest.TestCase):
    def test_missing_suite_bound_is_refused(self):
        source = (ROOT / "Makefile").read_text()
        recipe = next(line for line in source.splitlines()
                      if line.startswith("\tgo tool gotestsum --packages='./...'"))
        unbounded, count = re.subn(r" -timeout=\S+", "", recipe)
        self.assertEqual(count, 1, "the mutation must remove exactly one suite bound")
        with tempfile.TemporaryDirectory() as directory:
            makefile = Path(directory) / "Makefile"
            for name, text, expected in (
                ("stated", source, 0),
                ("missing", source.replace(recipe, unbounded, 1), 1),
            ):
                with self.subTest(name=name):
                    makefile.write_text(text)
                    result = subprocess.run(
                        ["bash", str(ROOT / "scripts/race_bound_not_below_check_bound_test.sh"),
                         str(makefile)],
                        cwd=ROOT, stdin=subprocess.DEVNULL,
                        capture_output=True, text=True, timeout=30,
                    )
                    self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
                    if expected:
                        self.assertIn("make check states no per-package bound", result.stderr)


if __name__ == "__main__":
    unittest.main()
