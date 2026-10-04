"""The rehearsal counts its own watchers, not every watcher on the host.

Case 4 of scripts/ci_browser_report_test.sh asks whether the watcher outlives
the suite it was started for. The host runs other checkouts' `make check`
beside this one, and each of them starts the same report's watcher. This case
starts a neighbour's watcher (a copy of the report, as another checkout would
run it) in the gap between case 4's two counts, and requires the rehearsal to
stay green: the neighbour is not this rehearsal's watcher and did not survive
anything of this rehearsal's.
"""

import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class BrowserWatcherCountIsItsOwn(unittest.TestCase):
    def test_a_neighbours_watcher_is_not_counted_as_this_rehearsals(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            neighbour = fixture / "neighbour" / "scripts" / "ci_browser_report.sh"
            neighbour.parent.mkdir(parents=True)
            shutil.copy(ROOT / "scripts" / "ci_browser_report.sh", neighbour)
            pid_file = fixture / "neighbour.pid"
            startup = fixture / "startup.bash"
            # Case 4's settle is the one `sleep 0.2` of the rehearsal; the
            # neighbour starts there, once, without this BASH_ENV.
            startup.write_text('''
sleep() {
    if [[ $# == 1 && $1 == 0.2 && ! -e $NEIGHBOUR_PID_FILE ]]; then
        env -u BASH_ENV setsid bash "$NEIGHBOUR_REPORT" --watch neighbour 1 \\
            > /dev/null 2>&1 < /dev/null &
        builtin echo $! > "$NEIGHBOUR_PID_FILE"
    fi
    command sleep "$@"
}
''')
            try:
                result = subprocess.run(
                    ["bash", "scripts/ci_browser_report_test.sh"],
                    cwd=ROOT,
                    env={**os.environ, "BASH_ENV": str(startup),
                         "NEIGHBOUR_REPORT": str(neighbour),
                         "NEIGHBOUR_PID_FILE": str(pid_file)},
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    timeout=60,
                )
            finally:
                if pid_file.exists():
                    try:
                        os.killpg(int(pid_file.read_text()), signal.SIGTERM)
                    except ProcessLookupError:
                        pass
            started = pid_file.exists()
        self.assertTrue(started, "the neighbour never started:\n" + result.stdout)
        self.assertEqual(
            result.returncode, 0,
            "Another checkout's watcher must not fail this rehearsal:\n"
            + result.stdout,
        )


if __name__ == "__main__":
    unittest.main()
