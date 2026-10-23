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

# The scratch line, as the record describes it. The tag is never published here — the
# absent tag is the state a line is in until the owner cuts it — so the record's frozen
# commit is the base, and the candidate descends from it, which is main's shape.
FROZEN = "f" * 40
CANDIDATE = "c" * 40


class FetchRecoveryPreservesVerdict(unittest.TestCase):
    def judge(self, report, ancestor=None):
        """Run the gate's own main() with the pinned tool answered from memory.

        `report` is what apidiff would have written; `ancestor` is the merge base the
        repository would answer for the candidate, which is the freeze for main and an
        older commit for a branch cut before the freeze. Every retry of the fetch is kept,
        so the verdict is read from a run that needed the proxy twice and healed.
        """
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
                if command[-1].startswith("refs/tags/"):
                    result.returncode, result.stdout = 1, ""  # the line's tag is unpublished
                else:
                    result.stdout = CANDIDATE if command[-1].startswith("HEAD") else FROZEN
            elif command[:2] == ["git", "merge-base"]:
                result.stdout = ancestor or command[2]
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
            record = Path(directory) / "line.json"
            record.write_text(json.dumps({"note": "the line record", "line": "v2.0.0",
                                          "frozen": {"commit": FROZEN}}))
            out, err = io.StringIO(), io.StringIO()
            with (patch.object(sys, "argv", ["check_public_api.py", "--line-record",
                                             str(record), "HEAD"]),
                  patch.object(gate.subprocess, "check_output", side_effect=check_output),
                  patch.object(gate.subprocess, "run", side_effect=run),
                  patch.object(gate.time, "sleep") as sleep,
                  contextlib.redirect_stdout(out), contextlib.redirect_stderr(err)):
                status = gate.main()
            self.assertEqual([call.args for call in sleep.call_args_list], [(5,), (20,)])
            self.assertEqual(exports, 4)  # Three attempts for the base, one for the candidate.
            evidence = json.loads(out.getvalue())
            self.assertTrue(any(command["exit_code"] == 0 and command["fetch_retries"] == 2
                                for command in evidence["commands"]))
            return status, evidence, err.getvalue()

    def test_a_fetch_that_healed_still_refuses_every_reported_line(self):
        status, evidence, err = self.judge("- example.org/library.Value: removed\n")
        self.assertEqual(status, 1)
        self.assertEqual(evidence["result"], "the exported API changed on a frozen line: 1")
        self.assertEqual(evidence["incompatibilities"], "- example.org/library.Value: removed")
        self.assertIn("BREAK example.org/library.Value: removed", err)

    def test_a_fetch_that_healed_accepts_a_clean_report(self):
        status, evidence, err = self.judge("")
        self.assertEqual(status, 0)
        self.assertEqual(evidence["result"], "no exported incompatibility with the frozen line")
        self.assertNotIn("BREAK", err)

    def test_a_fetch_that_healed_names_the_revision_it_judged(self):
        _, evidence, _ = self.judge("")
        self.assertEqual(evidence["line"]["wanted"], FROZEN)
        self.assertFalse(evidence["line"]["tag_exists"])
        self.assertEqual(evidence["line"]["used"], FROZEN)
        self.assertEqual(evidence["line"]["judged"], "the frozen surface")

    def test_a_branch_cut_before_the_freeze_is_judged_on_its_own_delta(self):
        _, evidence, _ = self.judge("", ancestor="b" * 40)
        self.assertEqual(evidence["line"]["used"], "b" * 40)
        self.assertEqual(evidence["line"]["judged"], "this change's own delta")


if __name__ == "__main__":
    unittest.main()
