"""Generated-screen floor assertions stay discoverable in the stack-free tier."""
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


class FloorCaseInventory(unittest.TestCase):
    def test_generated_floor_assertions_are_kept_as_runnable_push_cases(self):
        rows = json.loads((ROOT / "tests/inventory.json").read_text())["rows"]
        cases = {
            "ui/resource/command_controls_floor_test.go":
                "TestQuietCommandControlsKeepTheirNativeSubmitAndAccessibleName",
            "ui/resource/generated_screen_floor_test.go":
                "TestAGeneratedScreenFillsOneActionAndBoundsItsBodySizes",
            "ui/style/semantic_colors_test.go":
                "TestGeneratedScreenTextPairsMeetTheContrastFloor",
        }
        for file, name in cases.items():
            with self.subTest(case=name):
                matches = [row for row in rows if row["name"] == name]
                self.assertEqual(len(matches), 1, "floor assertion needs exactly one inventory row")
                row = matches[0]
                expected = {
                    "id": file + "#" + name,
                    "file": file,
                    "pkg": str(Path(file).parent),
                    "kind": "go",
                    "layer": "behaviour",
                    "tier": "push",
                    "needs_db": False,
                    "verdict": "keep",
                }
                self.assertEqual({key: row[key] for key in expected}, expected)
                self.assertRegex(
                    (ROOT / file).read_text(),
                    r"(?m)^func " + re.escape(name) + r"\(t \*testing\.T\) \{",
                )


if __name__ == "__main__":
    unittest.main()
