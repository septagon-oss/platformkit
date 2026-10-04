"""A successful suite remains successful when the watcher is scheduled first."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class BrowserSamplesAllowSuccess(unittest.TestCase):
    def test_successful_command_accepts_a_live_sample(self):
        # Rendezvous on the sample instead of relying on either process winning
        # the scheduler. BASH_ENV adds no failure to the command being watched.
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            sample = fixture / "sample"
            os.mkfifo(sample)
            startup = fixture / "startup.bash"
            startup.write_text('''
echo() {
    if [[ $# == 1 && $1 == alive ]]; then
        IFS= read -r sampled < "$BROWSER_SAMPLE_FIFO"
    fi
    builtin echo "$@"
    if [[ $* == 'quiet watch +0s browsers='* ]]; then
        builtin echo sampled > "$BROWSER_SAMPLE_FIFO"
    fi
}
''')
            result = subprocess.run(
                ["bash", "scripts/ci_browser_report_test.sh"],
                cwd=ROOT,
                env={**os.environ, "BASH_ENV": str(startup),
                     "BROWSER_SAMPLE_FIFO": str(sample)},
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                timeout=30,
            )
        self.assertEqual(
            result.returncode, 0,
            "A successful command and a live watcher sample must pass the "
            "CI rehearsal regardless of scheduling:\n" + result.stdout,
        )


if __name__ == "__main__":
    unittest.main()
