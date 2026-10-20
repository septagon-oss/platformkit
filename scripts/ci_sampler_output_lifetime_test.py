"""A completed suite releases its captured output without waiting for a sample."""

import os
from pathlib import Path
import signal
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]


class SamplerOutputLifetime(unittest.TestCase):
    def test_finished_suite_releases_output_and_preserves_its_status(self):
        for status in (0, 37):
            with self.subTest(status=status):
                process = subprocess.Popen(
                    [
                        "bash",
                        str(ROOT / "scripts/ci_browser_host_share.sh"),
                        "watch",
                        "30",
                        "sh",
                        "-c",
                        f"printf 'suite-finished\\n'; exit {status}",
                    ],
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    start_new_session=True,
                )
                try:
                    try:
                        output, _ = process.communicate(timeout=10)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        output, _ = process.communicate(timeout=5)
                        self.fail(f"finished suite retained its output: {output}")
                finally:
                    # The process group includes any interval wait the sampler left behind.
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                self.assertEqual(process.returncode, status, output)
                lines = output.splitlines()
                self.assertIn("suite-finished", lines, output)
                before_suite = lines[: lines.index("suite-finished")]
                self.assertTrue(
                    any(line.startswith("runner ") for line in before_suite), output
                )
                self.assertIn("browser suite: /dev/shm peaked at", output)


if __name__ == "__main__":
    unittest.main()
