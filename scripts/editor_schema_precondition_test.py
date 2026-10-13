"""The editor job refuses a missing desktop schema before starting its container."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]
SCHEMA = "/usr/share/glib-2.0/schemas/gschemas.compiled"


class EditorSchemaPrecondition(unittest.TestCase):
    def setUp(self):
        workflow = yaml.safe_load((ROOT / ".gitea/workflows/ci.yml").read_text())
        self.steps = workflow["jobs"]["editor"]["steps"]
        self.verify = next(step for step in self.steps if step.get("id") == "editor")

    def test_schema_install_precedes_the_browser_step_and_cannot_be_ignored(self):
        installs = [step for step in self.steps
                    if "apt-get install" in step.get("run", "")
                    and "gsettings-desktop-schemas" in step["run"]]
        self.assertEqual(len(installs), 1)
        install = installs[0]
        self.assertLess(self.steps.index(install), self.steps.index(self.verify))
        self.assertNotIn("if", install)
        self.assertNotIn("continue-on-error", install)
        self.assertNotIn("working-directory", install)
        self.assertIn("bash scripts/ci_apt_index.sh", install["run"])

    def test_missing_schema_refuses_before_any_container_command(self):
        self.run_browser_step(schema_present=False)

    def test_readable_schema_reaches_the_container_command(self):
        self.run_browser_step(schema_present=True)

    def test_failed_schema_install_refuses_the_step(self):
        install = next(step for step in self.steps
                       if "apt-get install" in step.get("run", "")
                       and "gsettings-desktop-schemas" in step["run"])
        with tempfile.TemporaryDirectory(prefix="schema-install-") as directory:
            scratch = Path(directory)
            calls = scratch / "apt-calls"
            apt = scratch / "apt-get"
            apt.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$SCHEMA_APT_CALLS"\n'
                           'if [ "$1" = install ]; then exit 100; fi\n')
            apt.chmod(0o700)
            env = dict(os.environ, PATH=str(scratch) + os.pathsep + os.environ["PATH"],
                       SCHEMA_APT_CALLS=str(calls))
            result = subprocess.run(
                ["/bin/bash", "-e", "-c", install["run"]], cwd=ROOT, env=env,
                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(result.returncode, 100, result.stdout + result.stderr)
            self.assertEqual(calls.read_text().splitlines(),
                             ["update", "install --yes --no-install-recommends gsettings-desktop-schemas"])

    def run_browser_step(self, schema_present):
        with tempfile.TemporaryDirectory(prefix="editor-schema-") as directory:
            scratch = Path(directory)
            schema = scratch / "gschemas.compiled"
            if schema_present:
                schema.write_bytes(b"fixture")
            calls = scratch / "docker-calls"
            docker = scratch / "docker"
            docker.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$SCHEMA_DOCKER_CALLS"\nexit 73\n')
            docker.chmod(0o700)
            body = self.verify["run"].replace(SCHEMA, str(schema))
            body = body.replace("${{ github.run_id }}", "schema-fixture")
            env = dict(os.environ, PATH=str(scratch), JOB_NETWORK="fixture-network",
                       CONTAINER_NAME="fixture-editor", IMAGE="fixture-image",
                       SCHEMA_DOCKER_CALLS=str(calls))
            result = subprocess.run(
                ["/bin/bash", "-e", "-c", body], cwd=ROOT, env=env,
                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=10,
            )
            if schema_present:
                self.assertEqual(result.returncode, 73, result.stdout + result.stderr)
                commands = calls.read_text().splitlines()
                self.assertEqual(len(commands), 2)
                self.assertTrue(commands[0].startswith("rm -f "))
                self.assertTrue(commands[1].startswith("run -d "))
            else:
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("no compiled desktop schema", result.stderr)
                self.assertFalse(calls.exists(), "the refused step must touch no container")


if __name__ == "__main__":
    unittest.main()
