"""A watcher whose first sample is late still samples a live suite, and passes.

The rehearsal's live-sample case asks whether a sample is taken while the
watched suite is alive. A host that schedules the watcher's first sample after
the suite's first output has still sampled a live suite: the suite keeps
running for two more seconds. This case holds the watcher's first sample until
the suite has printed its marker, through files rather than a delay, and
requires the rehearsal to stay green.
"""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class BrowserLateFirstSample(unittest.TestCase):
    def test_a_late_first_sample_of_a_live_suite_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            startup = fixture / "startup.bash"
            # `label` is sample_line's local, visible to the `ps` it calls.
            # Only the live case's first sample waits, and never past five
            # seconds, so a rehearsal that orders the suite after the sample
            # is not deadlocked by it.
            startup.write_text('''
echo() {
    builtin echo "$@"
    if [[ $# == 1 && $1 == MARKER ]]; then
        : > "$BROWSER_LATE_DIR/marker"
    fi
}
ps() {
    if [[ ${label:-} == live && ! -e $BROWSER_LATE_DIR/released ]]; then
        : > "$BROWSER_LATE_DIR/released"
        local waited=0
        while [[ ! -e $BROWSER_LATE_DIR/marker && $waited -lt 50 ]]; do
            command sleep 0.1
            waited=$((waited + 1))
        done
    fi
    command ps "$@"
}
''')
            result = subprocess.run(
                ["bash", "scripts/ci_browser_report_test.sh"],
                cwd=ROOT,
                env={**os.environ, "BASH_ENV": str(startup),
                     "BROWSER_LATE_DIR": str(fixture)},
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                timeout=60,
            )
            reached = (fixture / "released").exists()
        self.assertTrue(reached, "the live case's watcher never sampled:\n"
                        + result.stdout)
        self.assertEqual(
            result.returncode, 0,
            "A watcher that samples a live suite late must still pass the "
            "CI rehearsal:\n" + result.stdout,
        )


if __name__ == "__main__":
    unittest.main()
