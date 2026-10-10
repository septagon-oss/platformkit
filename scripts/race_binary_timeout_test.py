"""The race recipe's stated bound reaches Go's test binary, after flag parsing."""
import os
import pathlib
import re
import shlex
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class RaceBinaryTimeout(unittest.TestCase):
    def binary_arguments(self, makefile):
        env = os.environ.copy()
        for name in ("TEST_COUNT", "MAKEFLAGS", "MAKEOVERRIDES", "GOFLAGS"):
            env.pop(name, None)
        env["GOTOOLCHAIN"] = next(
            line.split()[1] for line in (ROOT / "go.mod").read_text().splitlines()
            if line.startswith("toolchain ")
        )
        recipe = subprocess.run(
            ["make", "--no-print-directory", "-n", "-f", str(makefile),
             "check-race", "RACE_PACKAGES=./ui/components"],
            cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=30, check=True,
        )
        commands = [shlex.split(line) for line in recipe.stdout.splitlines()
                    if line.startswith("go test ")]
        self.assertEqual(len(commands), 1, recipe.stdout)
        self.assertIn("-race", commands[0])
        dry = subprocess.run(
            commands[0][:2] + ["-n"] + commands[0][2:],
            cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=120, check=True,
        )
        invocations = [shlex.split(line) for line in
                       (dry.stdout + dry.stderr).splitlines()
                       if line.startswith("$WORK/") and ".test " in line]
        self.assertEqual(len(invocations), 1, dry.stdout + dry.stderr)
        return invocations[0]

    def assert_stated_bound(self, makefile):
        arguments = self.binary_arguments(makefile)
        self.assertIn("-test.count=1", arguments)
        self.assertIn("-test.timeout=30m0s", arguments)

    def test_race_binary_receives_the_stated_bound(self):
        self.assert_stated_bound(ROOT / "Makefile")

    def test_removing_the_bound_restores_gos_ten_minute_default(self):
        with tempfile.TemporaryDirectory() as directory:
            makefile = pathlib.Path(directory, "Makefile")
            # The bound is dropped from the race recipe wherever the recipe writes it: where the
            # clock sits on that line is the three text guards' concern, and this case is only
            # ever about whether the recipe states one at all. A mutation that matched nothing
            # leaves the stated clock in place, which the assertion below cannot miss.
            text = re.sub(r"(?m)^\tgo test -race \$\(TEST_COUNT\) -timeout=30m\b",
                          "\tgo test -race $(TEST_COUNT)", (ROOT / "Makefile").read_text(), count=1)
            self.assertNotEqual(text, (ROOT / "Makefile").read_text())
            makefile.write_text(text)
            arguments = self.binary_arguments(makefile)
            self.assertIn("-test.count=1", arguments)
            self.assertIn("-test.timeout=10m0s", arguments)
            self.assertNotIn("-test.timeout=30m0s", arguments)


if __name__ == "__main__":
    unittest.main()
