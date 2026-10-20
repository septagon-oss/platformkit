"""The race-clock guard accepts the recipes and refuses either missing clock or drift."""

from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class RaceClockGuardTest(unittest.TestCase):
    def test_suite_clocks_are_required_and_equal(self):
        source = (ROOT / "Makefile").read_text()
        cases = (
            ("committed", None, None, 0),
            ("race_clock_missing", "check-race", "", 1),
            ("check_clock_missing", "check", "", 1),
            ("race_clock_differs", "check-race", "-timeout=20m", 1),
        )
        for name, goal, replacement, expected in cases:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                text = source
                if goal:
                    pattern = (rf"(?m)^({re.escape(goal)}:[^\n]*\n"
                               rf"(?:\t[^\n]*\n)*?\tgo [^\n]*?) -timeout=\S+")
                    text, count = re.subn(pattern, rf"\g<1> {replacement}", text)
                    self.assertEqual(count, 1, "mutation must reach exactly one recipe")
                makefile = Path(directory) / "Makefile"
                makefile.write_text(text)
                result = subprocess.run(
                    ["bash", str(ROOT / "scripts/race_gate_states_the_check_clock_test.sh"),
                     str(makefile)],
                    stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30,
                )
                self.assertEqual(result.returncode, expected, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
