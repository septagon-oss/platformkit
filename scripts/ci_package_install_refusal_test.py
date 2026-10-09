"""A refused package install ends each workflow before it invokes installed tools."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import yaml


class PackageInstallRefusal(unittest.TestCase):
    def test_failed_install_stops_both_workflow_steps(self):
        root = Path(__file__).resolve().parent.parent
        source = (root / "scripts/ci_apt_index_test.sh").read_text()
        # Reuse the existing apt double, including its independent install refusal.
        stub = source.split("<<'STUB'\n", 1)[1].split("\nSTUB\n", 1)[0]
        for workflow, job in (("ci", "check"), ("mobile", "journey")):
            with self.subTest(workflow=workflow), tempfile.TemporaryDirectory() as tmp:
                scratch = Path(tmp)
                steps = yaml.safe_load((root / f".gitea/workflows/{workflow}.yml").read_text())["jobs"][job]["steps"]
                step = next(s for s in steps if "bash scripts/ci_apt_index.sh" in s.get("run", ""))
                for name in ("bash", "sleep"):
                    (scratch / name).symlink_to(shutil.which(name))
                (scratch / "apt-get").write_text(stub)
                (scratch / "apt-get").chmod(0o755)
                for name in ("psql", "ss", "python3"):
                    tool = scratch / name
                    tool.write_text('#!/bin/sh\nprintf "called\\n" >>"$TOOL_CALLS"\nexit 0\n')
                    tool.chmod(0o755)
                env = dict(os.environ, PATH=tmp, APT_STUB_UPDATE="ok",
                           APT_STUB_INSTALL="refused", APT_STUB_LOG=str(scratch / "apt.log"),
                           TOOL_CALLS=str(scratch / "tools.log"), PKIT_APT_INDEX_WAITS="0 0")
                result = subprocess.run([str(scratch / "bash"), "-e", "-c", step["run"]],
                                        cwd=root, env=env, stdin=subprocess.DEVNULL,
                                        capture_output=True, text=True, timeout=15)
                calls = (scratch / "apt.log").read_text().splitlines()
                self.assertEqual(calls[0], "apt-get update")
                self.assertEqual(len(calls), 2, calls)
                self.assertTrue(calls[1].startswith("apt-get install "), calls)
                self.assertEqual(result.returncode, 100, result.stdout + result.stderr)
                self.assertFalse((scratch / "tools.log").exists(), "tools ran after install refusal")


if __name__ == "__main__":
    unittest.main()
