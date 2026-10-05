"""A cut-off drain preserves both its cancellation and the underlying failure.

Reuse the kernel's deterministic error-chain cases. The negative controls remove
one half at a time on temporary overlays, so the test does not need a driver race
to establish that those cases can detect a broken report.
"""

import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from concurrent_index_reader_schedule_test import ROOT


SOURCE = ROOT / "kit/db/backfill.go"
CASE = "TestACutOffDrainNamesTheCutAndWhatItFound"
REPORT = ('return fmt.Errorf("%w: the drain was still running when this run\'s '
          'context ended: %w", ctx.Err(), err)')


def check_report(overlay=None):
    command = ["go", "test"]
    if overlay:
        command += ["-overlay", str(overlay)]
    command += ["./kit/db", "-run", "^" + CASE + "$", "-count=1", "-v",
                "-timeout=2m"]
    return subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL,
                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                          text=True, timeout=180)


class DrainCancellationReport(unittest.TestCase):
    def test_a_cut_off_drain_keeps_the_reason_and_the_driver_error(self):
        correct = check_report()
        self.assertIn("=== RUN   " + CASE, correct.stdout, correct.stdout)
        self.assertEqual(correct.returncode, 0, correct.stdout)
        source = SOURCE.read_text()
        self.assertEqual(source.count(REPORT), 1,
                         "Re-anchor the negative controls to the error-chain join")
        for lost, replacement in [("cancellation", "return err"),
                                  ("driver error", "return ctx.Err()")]:
            with self.subTest(lost=lost), tempfile.TemporaryDirectory() as directory:
                fixture = Path(directory)
                mutated = fixture / "backfill.go"
                mutated.write_text(source.replace(REPORT, replacement))
                overlay = fixture / "overlay.json"
                overlay.write_text(json.dumps({"Replace": {
                    str(SOURCE): str(mutated),
                }}))
                broken = check_report(overlay)
            # The same test must have reached its assertions; a build error is
            # not evidence that dropping either half of the report was caught.
            self.assertIn("=== RUN   " + CASE, broken.stdout, broken.stdout)
            self.assertIn("--- FAIL: " + CASE, broken.stdout, broken.stdout)
            self.assertNotEqual(broken.returncode, 0,
                                "Dropping " + lost + " escaped the error-chain case")


if __name__ == "__main__":
    unittest.main()
