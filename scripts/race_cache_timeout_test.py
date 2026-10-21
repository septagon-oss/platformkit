"""Opting into cached tests preserves the race suite's per-package clock."""

import pathlib
import tempfile
import unittest

import race_binary_timeout_test as race_pin


class RaceCacheTimeout(unittest.TestCase):
    def test_cached_and_repeated_runs_keep_the_stated_timeout(self):
        source = (race_pin.ROOT / "Makefile").read_text()
        with tempfile.TemporaryDirectory() as directory:
            makefile = pathlib.Path(directory, "Makefile")
            for count in ("", "-count=2"):
                with self.subTest(count=count):
                    makefile.write_text(source + "\nTEST_COUNT = " + count + "\n")
                    arguments = race_pin.RaceBinaryTimeout().binary_arguments(makefile)
                    self.assertIn("-test.timeout=30m0s", arguments)
                    counts = [arg for arg in arguments if arg.startswith("-test.count=")]
                    self.assertEqual(counts, [] if not count else ["-test.count=2"])


if __name__ == "__main__":
    unittest.main()
