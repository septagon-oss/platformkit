"""A live suite remains valid when two samples precede its next output."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class BrowserDelayedMarker(unittest.TestCase):
    def test_a_suite_descheduled_before_its_marker_still_passes(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            startup = fixture / "startup.bash"
            reached = fixture / "two-samples"
            # Hold only the suite's marker until two real watcher samples have
            # arrived. This models descheduling between seen(1) and echo, without
            # changing the report, suite exit code, samples or their timestamps.
            startup.write_text('''
echo() {
    if [[ $# == 1 && $1 == MARKER ]]; then
        local attempts=0 count=0
        while (( attempts < 200 )); do
            if [[ -f ${CI_BROWSER_WATCH_SAMPLE_FILE:-} ]]; then
                count=$(wc -l < "$CI_BROWSER_WATCH_SAMPLE_FILE")
                if (( count >= 2 )); then
                    : > "$BROWSER_MARKER_REACHED"
                    break
                fi
            fi
            command sleep 0.05
            attempts=$((attempts + 1))
        done
    fi
    builtin echo "$@"
}
''')
            result = subprocess.run(
                ["bash", "scripts/ci_browser_report_test.sh"], cwd=ROOT,
                env={**os.environ, "BASH_ENV": str(startup),
                     "BROWSER_MARKER_REACHED": str(reached)},
                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, text=True, timeout=60,
            )
            self.assertTrue(reached.exists(),
                            "The suite never observed two live samples:\n"
                            + result.stdout)
        self.assertEqual(
            result.returncode, 0,
            "Descheduling a passing suite before its next output must not "
            "fail the browser rehearsal:\n" + result.stdout,
        )


if __name__ == "__main__":
    unittest.main()
