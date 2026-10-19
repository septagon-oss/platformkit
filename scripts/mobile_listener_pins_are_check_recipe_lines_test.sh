#!/usr/bin/env bash
# The device journey's two listener cases run where the inventory files them: as lines of `make check`.
# tests/inventory.json files every scripts/ pin at tier push by its path alone, so nothing else refuses
# a recipe that dropped one of them; this case reads the recipe make would run and refuses that.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import subprocess

PINS = (
    "PYTHONDONTWRITEBYTECODE=1 python3 scripts/mobile_listener_attribution_test.py",
    "PYTHONDONTWRITEBYTECODE=1 python3 scripts/mobile_listener_step_policy_test.py",
)


def check_recipe(recipe):
    lines = [line.strip() for line in recipe.splitlines()]
    for pin in PINS:
        assert pin in lines, "make check does not run: " + pin


recipe = subprocess.check_output(["make", "-n", "check"], text=True, stdin=subprocess.DEVNULL)
check_recipe(recipe)
print("ok: make check runs both listener cases")

# Each missing line must be detected; mutate only the in-memory recipe.
for pin in PINS:
    try:
        check_recipe(recipe.replace(pin, "true"))
    except AssertionError:
        print("ok: refuses a recipe without " + pin.split()[-1])
    else:
        raise AssertionError("accepted a recipe without " + pin.split()[-1])
PY
