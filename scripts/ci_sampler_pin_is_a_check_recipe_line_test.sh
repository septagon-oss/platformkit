#!/usr/bin/env bash
# The sampler's output-lifetime case runs where the inventory files it: as a line of `make check`.
# `make test` runs gotestsum, which sees no Python, and tests/inventory.json files every scripts/ pin at
# tier push by its path alone, so nothing else refuses a recipe that dropped the line; this case reads
# the recipe make would run and refuses that.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import subprocess

PIN = "PYTHONDONTWRITEBYTECODE=1 python3 scripts/ci_sampler_output_lifetime_test.py"


def check_recipe(recipe):
    lines = [line.strip() for line in recipe.splitlines()]
    assert PIN in lines, "make check does not run: " + PIN


recipe = subprocess.check_output(["make", "-n", "check"], text=True, stdin=subprocess.DEVNULL)
check_recipe(recipe)
print("ok: make check runs the sampler's output-lifetime case")

# The missing line must be detected; mutate only the in-memory recipe.
try:
    check_recipe(recipe.replace(PIN, "true"))
except AssertionError:
    print("ok: refuses a recipe without " + PIN.split()[-1])
else:
    raise AssertionError("accepted a recipe without " + PIN.split()[-1])
PY
