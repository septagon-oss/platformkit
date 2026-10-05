"""A redelivery that arrives while the terminal failure is recorded adds nothing.

Hold the poison handler at its last capped attempt for longer than the ladder's
last rung. The broker keeps its timer and redelivers while the client cannot
run, so terminal recording meets a delivery past the cap. The case must still
count the capped attempts and exactly one terminal row.
"""

from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "kit/db"))
from concurrent_index_reader_schedule_test import ROOT, run  # noqa: E402


CASE = "TestJetStreamStopsRedeliveringAPoisonEvent"
SOURCE = ROOT / "kit/events/internal_test.go"
# The case caps the handler at five attempts; skip the first four hits.
EARLIER_ATTEMPTS = 4


class PoisonEventTerminalRedelivery(unittest.TestCase):
    def test_a_redelivery_during_terminal_recording_leaves_one_row(self):
        lines = SOURCE.read_text().splitlines()
        start = next(i for i, line in enumerate(lines)
                     if line.startswith("func " + CASE + "("))
        end = next(i for i in range(start + 1, len(lines))
                   if lines[i].startswith("func "))
        boundary = next(i + 1 for i in range(start, end)
                        if 'return errors.New("this will never work")' in lines[i])
        with tempfile.TemporaryDirectory() as directory:
            binary = str(Path(directory) / "events.test")
            compiled = run(["go", "test", "-c", "-o", binary, "./kit/events"])
            self.assertEqual(compiled.returncode, 0, compiled.stdout)
            result = run([
                "gdb", "-q", "-batch",
                "-iex", "set auto-load safe-path /",
                "-ex", "set debuginfod enabled off",
                "-ex", "handle SIGURG nostop noprint pass",
                "-ex", f"break {SOURCE}:{boundary}",
                "-ex", f"ignore 1 {EARLIER_ATTEMPTS}",
                "-ex", f"run -test.run=^{CASE}$ -test.v -test.timeout=120s",
                "-ex", "disable 1",
                # Three seconds outlasts the one-second last rung, so the broker
                # redelivers while only this test process is paused.
                "-ex", "shell sleep 3",
                "-ex", "continue",
                "-ex", "quit $_exitcode",
                binary,
            ])
        self.assertIn("=== RUN   " + CASE, result.stdout, result.stdout)
        self.assertIn("hit Breakpoint 1", result.stdout,
                      "The last capped attempt was not reached:\n" + result.stdout)
        self.assertEqual(result.returncode, 0,
                         "A redelivery during terminal recording must leave the "
                         "capped attempts and one terminal row:\n" + result.stdout)


if __name__ == "__main__":
    unittest.main()
