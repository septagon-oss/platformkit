"""The generated empty-list assertion rejects unreadable or missing guidance."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PIN = "TestAnEmptyGeneratedListDrawsNoParagraphSizeAListWithRowsDoesNot"


class EmptyGuidanceFloorMutations(unittest.TestCase):
    def test_larger_guidance_and_missing_paragraph_are_rejected(self):
        source = ROOT / "ui/components/data_list.go"
        original = source.read_text()
        paragraph = "h.P(h.Class(clDataCount.Compile()), g.Text(p.State.Text))"
        self.assertEqual(original.count(paragraph), 1)
        toolchain = next(line.split()[1] for line in (ROOT / "go.mod").read_text().splitlines()
                         if line.startswith("toolchain "))
        variants = {
            "original": (original, None),
            "larger_guidance": (
                original.replace(paragraph, paragraph.replace("clDataCount", "clEmptyTitle")),
                "which the list with rows never uses",
            ),
            "missing_paragraph": (
                original.replace(paragraph, paragraph.replace("h.P(", "h.Span(")),
                "no paragraph of the list with no rows says it is empty",
            ),
            "missing_guidance": (
                original.replace(paragraph, paragraph.replace("p.State.Text", '""')),
                "no paragraph of the list with no rows says it is empty",
            ),
        }
        for name, (content, refusal) in variants.items():
            with self.subTest(guidance=name), tempfile.TemporaryDirectory() as scratch:
                changed = Path(scratch) / "data_list.go"
                changed.write_text(content)
                overlay = Path(scratch) / "overlay.json"
                overlay.write_text(json.dumps({"Replace": {str(source): str(changed)}}))
                result = subprocess.run(
                    ["go", "test", "-overlay", str(overlay), "./ui/resource",
                     "-run", "^" + PIN + "$", "-count=1", "-v", "-timeout=60s"],
                    cwd=ROOT, env={**os.environ, "GOTOOLCHAIN": toolchain},
                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT, text=True, timeout=120,
                )
                if refusal is None:
                    self.assertEqual(result.returncode, 0, result.stdout)
                    self.assertIn("--- PASS: " + PIN, result.stdout)
                else:
                    self.assertNotEqual(result.returncode, 0, result.stdout)
                    self.assertIn("--- FAIL: " + PIN, result.stdout)
                    self.assertIn(refusal, result.stdout)


if __name__ == "__main__":
    unittest.main()
