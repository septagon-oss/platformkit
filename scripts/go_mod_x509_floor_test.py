"""The x509 advisory independently refuses the previous toolchain release."""
import pathlib
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class X509ToolchainFloor(unittest.TestCase):
    def test_previous_release_is_refused_only_for_x509(self):
        for version, expected in (("1.27.2", 1), ("1.27.3", 0), ("1.27.4", 0)):
            with self.subTest(version=version), tempfile.TemporaryDirectory() as directory:
                pathlib.Path(directory, "go.mod").write_text(
                    "module example.test/advisory\n\ngo 1.26.6\n\n"
                    f"toolchain go{version}\n\n"
                    "require (\n\tgolang.org/x/net v0.62.0\n)\n"
                )
                result = subprocess.run(
                    ["bash", str(ROOT / "scripts/go_mod_above_advisory_test.sh"), directory],
                    stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30,
                )
                output = result.stdout + result.stderr
                self.assertEqual(result.returncode, expected, output)
                refusals = [line for line in output.splitlines() if line.startswith("FAIL: go.mod")]
                if expected:
                    self.assertEqual(len(refusals), 1, output)
                    self.assertIn("GO-2026-6809", refusals[0])
                    self.assertIn("go1.27.2", refusals[0])
                    self.assertIn("go1.27.3", refusals[0])
                else:
                    self.assertEqual(refusals, [], output)


if __name__ == "__main__":
    unittest.main()
