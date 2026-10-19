"""The socket guard checks the journey step's execution and failure policy."""
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parent.parent


class MobileListenerStepPolicy(unittest.TestCase):
    def run_guard(self, field):
        self.assertIsNotNone(shutil.which("node"), "the full socket guard requires Node")
        with tempfile.TemporaryDirectory() as tmp:
            scratch = Path(tmp)
            for name in ("scripts/free_port_test.sh", "scripts/free_port.sh", "scripts/e2e.sh",
                         ".gitea/workflows/ci.yml", ".gitea/workflows/mobile.yml"):
                target = scratch / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            workflow = scratch / ".gitea/workflows/mobile.yml"
            source = workflow.read_text()
            steps = yaml.safe_load(source)["jobs"]["journey"]["steps"]
            receipt = next(s for s in steps if s.get("name", "").startswith("The socket probe"))
            if field:
                marker = f"      - name: {receipt['name']}\n"
                self.assertEqual(source.count(marker), 1)
                workflow.write_text(source.replace(marker, marker + f"        {field}\n"))
                changed = yaml.safe_load(workflow.read_text())["jobs"]["journey"]["steps"]
                changed_receipt = next(s for s in changed if s.get("name") == receipt["name"])
                self.assertEqual(changed_receipt["run"], receipt["run"])

            process = subprocess.Popen(
                ["bash", "scripts/free_port_test.sh"], cwd=scratch,
                env=dict(os.environ, TMPDIR=tmp), stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                text=True, start_new_session=True,
            )
            try:
                output, _ = process.communicate(timeout=45)
                self.assertNotIn("SKIPPED", output)
                return process.returncode, output
            finally:
                # Stop only the process group this case started, including unfinished listeners.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=5)

    def test_disabled_or_forgiven_receipt_refuses_the_complete_guard(self):
        for field in ("if: ${{ false }}", "continue-on-error: true"):
            with self.subTest(field=field):
                status, output = self.run_guard(field)
                self.assertEqual(status, 1, output)
                failures = [line for line in output.splitlines() if line.startswith("FAIL:")]
                self.assertEqual(len(failures), 1, output)
                self.assertIn("receipt step", failures[0])
                self.assertIn(field, output)

    def test_default_and_explicit_stop_on_failure_policies_pass(self):
        for field in ("", "continue-on-error: false"):
            with self.subTest(field=field):
                status, output = self.run_guard(field)
                self.assertEqual(status, 0, output)
                self.assertNotIn("FAIL:", output)


if __name__ == "__main__":
    unittest.main()
