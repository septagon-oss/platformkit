"""Execute the browser steps' wrapper commands with controlled suite verdicts."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]


class BrowserStepsPreserveVerdict(unittest.TestCase):
    def test_each_wrapped_suite_keeps_its_arguments_and_exit_status(self):
        workflow = yaml.safe_load((ROOT / ".gitea/workflows/ci.yml").read_text())
        cases = (
            ("design", "Native browser observations", "npm", ["run", "test:browser"]),
            ("editor", "Verify editing and downloaded FIG saves in the built editor",
             "node", ["--import", "./register.mjs", "--test", "--test-concurrency=1"]),
        )
        for job, name, executable, prefix in cases:
            step = next(s for s in workflow["jobs"][job]["steps"] if s.get("name") == name)
            # Setup owns installation and containers; exercise the final invocation
            # verbatim, including shell glob expansion and the real report script.
            lines = step["run"].splitlines()
            start = next(i for i, line in enumerate(lines)
                         if line.startswith("bash ../../../scripts/ci_browser_report.sh run "))
            command = "\n".join(lines[start:])
            cwd = ROOT / step["working-directory"]
            expected = prefix[:]
            if executable == "node":
                expected += sorted(str(p.relative_to(cwd)) for p in (cwd / "editor").glob("*.test.mjs"))
                self.assertGreater(len(expected), len(prefix), "the editor suite must name tests")
            for status in (0, 23):
                with self.subTest(job=job, status=status), tempfile.TemporaryDirectory() as directory:
                    fixture = Path(directory)
                    suite = fixture / executable
                    suite.write_text(
                        f"#!{sys.executable}\nimport json, sys\n"
                        "print('SUITE_ARGUMENTS=' + json.dumps(sys.argv[1:]), flush=True)\n"
                        f"sys.exit({status})\n"
                    )
                    suite.chmod(0o755)
                    watcher = fixture / "watcher"
                    result = subprocess.run(
                        ["bash", "-e", "-c", command], cwd=cwd,
                        env={**os.environ, "PATH": directory + os.pathsep + os.environ["PATH"],
                             "CI_BROWSER_WATCH_PID_FILE": str(watcher)},
                        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                        stderr=subprocess.STDOUT, text=True, timeout=30,
                    )
                    self.assertEqual(result.returncode, status, result.stdout)
                    arguments = [line.removeprefix("SUITE_ARGUMENTS=")
                                 for line in result.stdout.splitlines()
                                 if line.startswith("SUITE_ARGUMENTS=")]
                    self.assertEqual(len(arguments), 1, result.stdout)
                    self.assertEqual(json.loads(arguments[0]), expected)
                    self.assertEqual("death report" in result.stdout, status != 0, result.stdout)
                    self.assertTrue(watcher.exists(), "the real wrapper must start its watcher")
                    for pid in watcher.read_text().splitlines():
                        with self.assertRaises(ProcessLookupError):
                            os.kill(int(pid), 0)


if __name__ == "__main__":
    unittest.main()
