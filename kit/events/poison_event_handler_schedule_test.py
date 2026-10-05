"""A descheduled poison handler still reaches the policy's terminal state.

The broker continues its backoff while the client cannot run. Resume the client
before the case's dead-letter deadline, then require its existing delivery-count
and terminal-row assertions to pass. A scheduling pause changes no policy.
"""

from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "kit/db"))
from concurrent_index_reader_schedule_test import ROOT, run  # noqa: E402


CASE = "TestJetStreamStopsRedeliveringAPoisonEvent"
SOURCE = ROOT / "kit/events/internal_test.go"


class PoisonEventHandlerSchedule(unittest.TestCase):
    def test_a_descheduled_poison_handler_still_records_one_terminal_failure(self):
        lines = SOURCE.read_text().splitlines()
        start = next(i for i, line in enumerate(lines)
                     if line.startswith("func " + CASE + "("))
        end = next(i for i in range(start + 1, len(lines))
                   if lines[i].startswith("func "))
        # The poison handler must return an error on a corrected branch too.
        # Reachability does not depend on any assertion or error text from the
        # broken timing check. Disable the breakpoint after its first hit.
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
                "-ex", f"run -test.run=^{CASE}$ -test.v -test.timeout=120s",
                "-ex", "disable 1",
                # This is an adverse schedule, not a readiness wait. Only the
                # recorded test process is paused; the shared broker keeps running.
                "-ex", "shell sleep 2",
                "-ex", "continue",
                "-ex", "quit $_exitcode",
                binary,
            ])
        self.assertIn("=== RUN   " + CASE, result.stdout, result.stdout)
        self.assertIn("hit Breakpoint 1", result.stdout,
                      "The handler schedule boundary was not reached:\n" + result.stdout)
        self.assertEqual(result.returncode, 0,
                         "Descheduling a poison handler must not fail a case whose "
                         "delivery cap and terminal row are correct:\n" + result.stdout)


if __name__ == "__main__":
    unittest.main()
