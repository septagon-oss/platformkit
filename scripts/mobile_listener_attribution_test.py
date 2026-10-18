"""The mobile runner proves exact socket ownership with either installed reader."""
from concurrent.futures import ThreadPoolExecutor
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parent.parent


class MobileListenerAttribution(unittest.TestCase):
    def setUp(self):
        workflow = yaml.safe_load((ROOT / ".gitea/workflows/mobile.yml").read_text())
        steps = workflow["jobs"]["journey"]["steps"]
        self.step = next(s for s in steps if s.get("name", "").startswith("The socket probe"))

    def run_step(self, reader, prefix=""):
        with tempfile.TemporaryDirectory() as tmp:
            scratch = Path(tmp)
            binaries = scratch / "bin"
            binaries.mkdir()
            # The device job needs no Node; each reader is exercised independently.
            for name in ("bash", "grep", "sed", "sort", "head", "sleep", "mktemp", "python3", "rm", reader):
                path = shutil.which(name)
                self.assertIsNotNone(path, f"required test tool missing: {name}")
                (binaries / name).symlink_to(path)
            observed = scratch / "observed"
            env = dict(os.environ, PATH=str(binaries), TMPDIR=tmp,
                       SOCKET_OBSERVATIONS=str(observed))
            process = subprocess.Popen(
                [str(binaries / "bash"), "-e", "-c", prefix + self.step["run"]],
                cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                text=True, start_new_session=True,
            )
            try:
                output, _ = process.communicate(timeout=15)
                observations = observed.read_text() if observed.exists() else ""
                for pid in re.findall(r"pid[= ](\d+)", output + observations):
                    with self.assertRaises(ProcessLookupError, msg=f"listener {pid} survived: {output}"):
                        os.kill(int(pid), 0)
                leftovers = {p.name for p in scratch.iterdir()} - {"bin", "observed"}
                self.assertFalse(leftovers, f"step left temporary files: {leftovers}")
                return process.returncode, output, observations
            finally:
                # This is the group this test created, including any unfinished listener.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=5)

    def test_either_reader_attributes_and_cleans_its_listener_without_node(self):
        for reader in ("ss", "lsof"):
            with self.subTest(reader=reader):
                if reader == "lsof" and shutil.which(reader) is None:
                    self.skipTest("lsof is optional; the workflow installs ss through iproute2")
                status, output, _ = self.run_step(reader)
                self.assertEqual(status, 0, output)
                self.assertRegex(output, r"the socket probe names its own listener: pid \d+ on port [1-9]\d*")

    def test_nonempty_owner_with_only_a_matching_pid_prefix_is_refused(self):
        # Ask the real socket table first, then report a different PID sharing its prefix.
        prefix = r'''
ss() {
    if [ "$1" = -V ]; then command ss "$@"; return; fi
    local answer
    answer="$(command ss "$@")" || return
    printf '%s\n' "$answer" >>"$SOCKET_OBSERVATIONS"
    printf '%s\n' "$answer" | sed -E 's/pid=([0-9]+)/pid=\10/g'
}
'''
        status, output, observed = self.run_step("ss", prefix)
        self.assertRegex(observed, r"pid=\d+", "the socket reader never reached a real listener")
        self.assertEqual(status, 1, output)
        self.assertIn("neither ss nor lsof on this runner named the listener", output)
        self.assertNotIn("the socket probe names its own listener:", output)

    def test_concurrent_steps_each_attribute_their_own_listener(self):
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(self.run_step, ("ss", "ss")))
        pids = []
        for status, output, _ in results:
            self.assertEqual(status, 0, output)
            match = re.search(r"the socket probe names its own listener: pid (\d+) on port (\d+)", output)
            self.assertIsNotNone(match, output)
            pids.append(match[1])
        self.assertNotEqual(*pids, "concurrent steps claimed the same listener")


if __name__ == "__main__":
    unittest.main()
