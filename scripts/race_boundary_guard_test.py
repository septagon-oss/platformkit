"""The CI race guard rejects losing the detector, fresh count or package timeout."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class RaceBoundaryGuard(unittest.TestCase):
    def test_missing_race_boundary_parts_are_refused(self):
        root = Path(__file__).resolve().parents[1]
        source = (root / "Makefile").read_text()
        recipe = next(line for line in source.splitlines()
                      if line.startswith("\tgo test -race $(TEST_COUNT)"))
        environment = dict(os.environ)
        for name in ("MAKEFLAGS", "MAKEOVERRIDES", "TEST_COUNT"):
            environment.pop(name, None)
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory)
            (fixture / "scripts").mkdir()
            shutil.copy2(root / "go.mod", fixture / "go.mod")
            shutil.copytree(root / ".gitea", fixture / ".gitea")
            guard = fixture / "scripts/make_check_count_default_test.sh"
            shutil.copy2(root / "scripts" / guard.name, guard)
            for removed in (None, " -race", " $(TEST_COUNT)", " -timeout=30m"):
                with self.subTest(removed=removed):
                    changed = recipe if removed is None else recipe.replace(removed, "", 1)
                    (fixture / "Makefile").write_text(source.replace(recipe, changed, 1))
                    result = subprocess.run(
                        ["bash", str(guard)], cwd=fixture, env=environment,
                        stdin=subprocess.DEVNULL, capture_output=True, text=True,
                        timeout=30,
                    )
                    self.assertEqual(result.returncode, 0 if removed is None else 1,
                                     result.stdout + result.stderr)
                    if removed is not None:
                        self.assertIn("FAIL: make check-race", result.stderr)


if __name__ == "__main__":
    unittest.main()
