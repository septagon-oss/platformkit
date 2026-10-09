#!/usr/bin/env bash
# The source-gate pins must execute successfully as children of a make recipe.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 -B - <<'PY'
from pathlib import Path
import subprocess
import tempfile

commands = subprocess.check_output(
    ["make", "--no-print-directory", "-n", "check"], text=True,
).splitlines()
pins = (
    "bash scripts/journey_and_pillar_check_wiring_test.sh",
    "bash scripts/journey_and_pillar_check_refusal_test.sh",
)
for pin in pins:
    assert pin in commands, f"make check does not execute {pin}"

with tempfile.TemporaryDirectory(prefix="journey-check-recipe-") as directory:
    recipe = Path(directory) / "Makefile"
    recipe.write_text(".PHONY: pins\npins:\n" + "".join("\t" + pin + "\n" for pin in pins))
    result = subprocess.run(
        ["make", "--no-print-directory", "-j2", "-f", str(recipe), "pins"],
        stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=90,
    )
    output = result.stdout + result.stderr
    assert result.returncode == 0, output
    for verdict in (
        "ok: make check runs python3 -B scripts/architecture_pillars_test.py",
        "ok: make check runs bash scripts/e2e_step_library_test.sh",
        "ok: the composed prerequisites are accepted",
        "ok: removing check-pillars is refused by the shared pin",
        "ok: removing check-step-library is refused by the shared pin",
    ):
        assert verdict in output, output
    print(output, end="")
print("ok: both source-gate pins execute under make's inherited flags")
PY
