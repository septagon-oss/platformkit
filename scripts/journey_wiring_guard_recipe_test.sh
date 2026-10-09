#!/usr/bin/env bash
# The recipe wiring guard must keep its acceptance and refusal under a parent make.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 -B - <<'PY'
from pathlib import Path
import subprocess
import tempfile

pin = "bash scripts/journey_check_recipe_wiring_test.sh"
commands = subprocess.check_output(
    ["make", "--no-print-directory", "-n", "check"],
    stdin=subprocess.DEVNULL, text=True, timeout=30,
).splitlines()
assert pin in commands, f"make check does not execute {pin}"

with tempfile.TemporaryDirectory(prefix="journey-wiring-guard-") as directory:
    recipe = Path(directory) / "Makefile"
    recipe.write_text(".PHONY: pins\npins:\n\t" + pin + "\n")
    result = subprocess.run(
        ["make", "--no-print-directory", "-j2", "-f", str(recipe), "pins"],
        stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=90,
    )
    output = result.stdout + result.stderr
    assert result.returncode == 0, output
    for verdict in (
        "ok: make check runs bash scripts/journey_check_recipe_test.sh",
        "ok: the copied recipe still runs it",
        "ok: a recipe that lost it is refused:",
    ):
        assert verdict in output, output
    print(output, end="")
print("ok: the recipe wiring guard accepts and refuses under a parallel parent make")
PY
