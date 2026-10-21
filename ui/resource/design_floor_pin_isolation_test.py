"""Design-floor pins read their checkout without rewriting its evidence."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class DesignFloorPinIsolation(unittest.TestCase):
    def test_pins_run_outside_the_checkout_and_leave_their_inputs_unchanged(self):
        pins = ("empty_list_inventory_validation_test.py", "reference_gate_reports_test.py")
        inputs = [ROOT / "ui/resource" / name for name in pins]
        inputs.extend(ROOT / name for name in (
            "ui/resource/empty_list_floor_case_inventory_test.py",
            "ui/resource/generated_screen_floor_test.go",
            "e2e/generated-pages-design-gate.spec.ts",
            "tests/inventory.json", "tests/INVENTORY.md", "loc-budget.json",
        ))
        before = {path: path.read_bytes() for path in inputs}
        with tempfile.TemporaryDirectory() as scratch:
            for name in pins:
                with self.subTest(pin=name):
                    result = subprocess.run(
                        [sys.executable, "-B", str(ROOT / "ui/resource" / name), "-v"],
                        cwd=scratch, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                        stderr=subprocess.STDOUT, text=True, timeout=60,
                    )
                    self.assertEqual(result.returncode, 0, result.stdout)
                    self.assertIn("Ran 1 test", result.stdout)
        for path, original in before.items():
            self.assertEqual(path.read_bytes(), original, str(path.relative_to(ROOT)))


if __name__ == "__main__":
    unittest.main()
