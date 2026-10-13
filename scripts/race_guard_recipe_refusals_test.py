"""The recipe pin refuses a disabled, swallowed, missing or duplicated race guard."""
import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


class RaceGuardRecipeRefusals(unittest.TestCase):
    def test_recipe_pin_detects_changes_to_the_guard_command(self):
        source = Path(__file__).with_name("race_guard_check_recipe_test.py")
        spec = importlib.util.spec_from_file_location("recipe_pin", source)
        pin = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(pin)
        run = subprocess.run
        command = "PYTHONDONTWRITEBYTECODE=1 python3 scripts/race_boundary_guard_test.py"
        for replacement in (command, "# " + command, command + " || true", "", command + "\n" + command):
            with self.subTest(replacement=replacement):
                def changed_recipe(*args, **kwargs):
                    result = run(*args, **kwargs)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertEqual(result.stdout.splitlines().count(command), 1)
                    result.stdout = "\n".join(
                        replacement if line == command else line
                        for line in result.stdout.splitlines()
                    )
                    return result

                result = unittest.TestResult()
                with patch.object(pin.subprocess, "run", side_effect=changed_recipe):
                    pin.RaceGuardCheckRecipe("test_check_runs_the_inventoried_race_guard").run(result)
                self.assertEqual(result.testsRun, 1)
                self.assertEqual(result.errors, [])
                self.assertEqual(len(result.failures), 0 if replacement == command else 1)


if __name__ == "__main__":
    unittest.main()
