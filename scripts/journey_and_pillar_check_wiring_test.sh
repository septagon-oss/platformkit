#!/usr/bin/env bash
# Both source contracts remain checked when their Makefile prerequisites merge.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 -B - <<'PY'
import subprocess

recipe = subprocess.check_output(["make", "-n", "check"], text=True)
commands = recipe.splitlines()
for command in (
    "python3 -B scripts/architecture_pillars_test.py",
    "bash scripts/e2e_step_library_test.sh",
):
    assert command in commands, f"make check no longer runs {command}"
    print(f"ok: make check runs {command}")
PY
