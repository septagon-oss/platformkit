"""The lock guard refuses the last vulnerable sanitizer and accepts its fix."""

import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
GUARD = Path("scripts/openpencil_lock_above_advisory_test.sh")
LOCK = Path("tools/designexport/openpencil/package-lock.json")
WORKFLOW = Path(".gitea/workflows/ci.yml")


class SanitizerAdvisoryBoundary(unittest.TestCase):
    def test_the_last_vulnerable_release_is_refused_and_the_patched_release_passes(self):
        for version, expected in (("3.4.16", 1), ("3.4.17", 0)):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as directory:
                fixture = Path(directory)
                for relative in (GUARD, LOCK, WORKFLOW):
                    target = fixture / relative
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(ROOT / relative, target)
                lock = json.loads((fixture / LOCK).read_text())
                lock["packages"].setdefault("node_modules/dompurify", {})["version"] = version
                (fixture / LOCK).write_text(json.dumps(lock))

                result = subprocess.run(
                    ["bash", str(fixture / GUARD)],
                    stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30,
                )
                output = result.stdout + result.stderr
                self.assertEqual(result.returncode, expected, output)
                failures = "\n".join(
                    line for line in result.stdout.splitlines() if line.startswith("FAIL:")
                )
                if expected:
                    self.assertIn("dompurify@3.4.16", failures)
                    self.assertIn("GHSA-mrv2-3pqr-j5jr", failures)
                    self.assertIn("GHSA-c6gr-qvv4-5v3f", failures)
                    self.assertIn("GHSA-cg2g-xh64-v4q7", failures)
                else:
                    self.assertEqual(failures, "", output)


if __name__ == "__main__":
    unittest.main()
