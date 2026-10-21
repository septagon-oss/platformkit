"""The empty-sheet contract rejects treating an allocated sheet as painted CSS."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PIN = "TestGeneratedIdentityKeepsItsLayoutClaimWhenConsumerExtraPaintsNothing"


class ConsumerEmptySheetContract(unittest.TestCase):
    def test_empty_sheet_case_distinguishes_allocation_from_paint(self):
        source = ROOT / "ui/export/export.go"
        original = source.read_text()
        condition = 'overridden = overridden || sheet.CSS() != ""'
        self.assertEqual(original.count(condition), 1)
        toolchain = next(line.split()[1] for line in (ROOT / "go.mod").read_text().splitlines()
                         if line.startswith("toolchain "))
        variants = {
            "original": (original, "pass"),
            "allocated_sheet_is_paint": (
                original.replace(condition, "overridden = overridden || sheet != nil"),
                "fail",
            ),
        }
        for name, (content, outcome) in variants.items():
            with self.subTest(condition=name), tempfile.TemporaryDirectory() as scratch:
                changed = Path(scratch) / "export.go"
                changed.write_text(content)
                overlay = Path(scratch) / "overlay.json"
                overlay.write_text(json.dumps({"Replace": {str(source): str(changed)}}))
                result = subprocess.run(
                    ["go", "test", "-overlay", str(overlay), "./ui/export",
                     "-run", "^" + PIN + "$", "-count=1", "-json", "-timeout=4m"],
                    cwd=ROOT, env={**os.environ, "GOTOOLCHAIN": toolchain},
                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT, text=True, timeout=300,
                )
                events = [json.loads(line) for line in result.stdout.splitlines()
                          if line.startswith("{")]
                actions = [event["Action"] for event in events if event.get("Test") == PIN]
                self.assertIn(outcome, actions, result.stdout)
                self.assertEqual(result.returncode, 0 if outcome == "pass" else 1, result.stdout)
                if outcome == "fail":
                    output = "".join(event.get("Output", "") for event in events
                                     if event.get("Test") == PIN)
                    self.assertIn("empty sheet: a consumer that paints nothing withdrew the layout claim",
                                  output)
        self.assertEqual(source.read_text(), original)


if __name__ == "__main__":
    unittest.main()
