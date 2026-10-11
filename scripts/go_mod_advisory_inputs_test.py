"""Exercise the advisory guard through its public go.mod-directory input."""
import pathlib
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class AdvisoryInputs(unittest.TestCase):
    def test_release_floors_and_commented_pins(self):
        cases = (
            ("fixed", "go1.27.2", "v0.60.0", False, 0),
            ("newer", "go1.28.2", "v0.61.0", False, 0),
            ("old_net", "go1.27.2", "v0.59.0", False, 1),
            ("old_toolchain", "go1.27.1", "v0.60.0", False, 1),
            ("commented_net", "go1.27.2", "v0.60.0", True, 1),
        )
        for name, toolchain, net, commented, expected in cases:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                prefix = "// " if commented else ""
                pathlib.Path(directory, "go.mod").write_text(
                    "module example.test/guard\n\ngo 1.26.6\n\n"
                    f"toolchain {toolchain}\n\nrequire (\n"
                    f"\t{prefix}golang.org/x/net {net}\n)\n"
                )
                result = subprocess.run(
                    ["bash", str(ROOT / "scripts/go_mod_above_advisory_test.sh"), directory],
                    stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30,
                )
                self.assertEqual(result.returncode, expected, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
