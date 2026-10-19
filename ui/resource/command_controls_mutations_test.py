"""The native command-control pin rejects regressions in the rendered controls."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PIN = "TestQuietCommandControlsKeepTheirNativeSubmitAndAccessibleName"


class CommandControlMutations(unittest.TestCase):
    def test_control_regressions_are_rejected_by_the_native_submit_pin(self):
        source = ROOT / "ui/resource/commands.go"
        original = source.read_text()
        mutations = {
            "accessible_name": (
                "Action: c.Action(item), Label: label",
                'Action: c.Action(item), Label: ""',
                "command form lost its POST method or accessible name",
            ),
            "native_submit": (
                'Label: button, Type: "submit"',
                'Label: button, Type: "button"',
                "native submits; want one each",
            ),
            "quiet_variant": (
                'Variant: "outline", Size: "md"',
                'Variant: "primary", Size: "md"',
                "command submit must keep the quiet outline variant",
            ),
        }
        toolchain = next(line.split()[1] for line in (ROOT / "go.mod").read_text().splitlines()
                         if line.startswith("toolchain "))
        for name, (before, after, refusal) in mutations.items():
            with self.subTest(control=name), tempfile.TemporaryDirectory() as scratch:
                self.assertEqual(original.count(before), 1)
                changed = Path(scratch) / "commands.go"
                changed.write_text(original.replace(before, after))
                overlay = Path(scratch) / "overlay.json"
                overlay.write_text(json.dumps({"Replace": {str(source): str(changed)}}))
                result = subprocess.run(
                    ["go", "test", "-overlay", str(overlay), "./ui/resource",
                     "-run", "^" + PIN + "$", "-count=1", "-v", "-timeout=60s"],
                    cwd=ROOT, env={**os.environ, "GOTOOLCHAIN": toolchain},
                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT, text=True, timeout=120,
                )
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn("--- FAIL: " + PIN, result.stdout)
                self.assertIn(refusal, result.stdout)


if __name__ == "__main__":
    unittest.main()
