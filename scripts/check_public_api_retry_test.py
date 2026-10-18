"""A recovered tool fetch must not change the API comparison's verdict."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "check_public_api", Path(__file__).with_name("check_public_api.py")
)
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)


class FetchRecoveryPreservesVerdict(unittest.TestCase):
    def compare(self, report, known):
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w"):
            pass
        exports = 0

        def check_output(command, **kwargs):
            if command[:3] == ["go", "env", "GOROOT"]:
                return "/toolchain\n"
            self.assertEqual(command[:2], ["git", "archive"])
            return archive.getvalue()

        def run(command, **kwargs):
            nonlocal exports
            result = subprocess.CompletedProcess(command, 0, "", "")
            if command[:2] == ["git", "rev-parse"]:
                result.stdout = "0123456789abcdef\n"
            elif command[1:3] == ["mod", "edit"]:
                result.stdout = json.dumps({"Module": {"Path": "example.org/library"}})
            elif "-w" in command:
                exports += 1
                if exports <= 2:
                    result.returncode = 1
                    result.stderr = "go: loading deprecation: dial tcp: i/o timeout"
            else:
                self.assertIn("-incompatible", command)
                result.stdout = report
            return result

        with tempfile.TemporaryDirectory() as directory:
            baseline = Path(directory) / "baseline.json"
            baseline.write_text(json.dumps({"incompatibilities": known}))
            out, err = io.StringIO(), io.StringIO()
            with (patch.object(sys, "argv", ["check_public_api.py", "base", "head",
                                            "--baseline", str(baseline)]),
                  patch.object(gate.subprocess, "check_output", side_effect=check_output),
                  patch.object(gate.subprocess, "run", side_effect=run),
                  patch.object(gate.time, "sleep") as sleep,
                  contextlib.redirect_stdout(out), contextlib.redirect_stderr(err)):
                status = gate.main()
            self.assertEqual([call.args for call in sleep.call_args_list], [(5,), (20,)])
            self.assertEqual(exports, 4)  # Three attempts for base, one for candidate.
            evidence = json.loads(out.getvalue())
            self.assertTrue(any(command["exit_code"] == 0 and command["fetch_retries"] == 2
                                for command in evidence["commands"]))
            return status, evidence["baseline"]

    def test_recovered_fetch_still_refuses_new_incompatibility(self):
        status, baseline = self.compare("- example.org/library.Value: removed\n", [])
        self.assertEqual(status, 1)
        self.assertEqual(baseline["new"], ["example.org/library.Value: removed"])

    def test_recovered_fetch_still_refuses_stale_baseline(self):
        status, baseline = self.compare("", ["example.org/library.Value: removed"])
        self.assertEqual(status, 1)
        self.assertEqual(baseline["stale"], ["example.org/library.Value: removed"])

    def test_recovered_fetch_accepts_matching_baseline(self):
        status, baseline = self.compare("- example.org/library.Value: removed\n",
                                        ["example.org/library.Value: removed"])
        self.assertEqual(status, 0)
        self.assertEqual(baseline["new"], [])
        self.assertEqual(baseline["stale"], [])


if __name__ == "__main__":
    unittest.main()
