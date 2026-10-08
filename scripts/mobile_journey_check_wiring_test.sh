#!/usr/bin/env bash
# The source gate runs the authenticated-download case, with its YAML reader installed in CI.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import shlex
import subprocess
import yaml


def check_wiring(recipe, steps):
    command = "bash scripts/mobile_journey_fetch_test.sh"
    assert command in recipe.splitlines(), "make check does not run the download case"
    gate = next(i for i, step in enumerate(steps) if
                "make check" in step.get("run", "").splitlines())
    installed = False
    for step in steps[:gate]:
        for line in step.get("run", "").splitlines():
            if not line.strip().startswith("apt-get install "):
                continue
            words = shlex.split(line)
            if "apt-get" in words and "install" in words and "python3-yaml" in words:
                installed = True
    assert installed, "CI does not install python3-yaml before make check"


recipe = subprocess.check_output(["make", "-n", "check"], text=True)
with open(".gitea/workflows/ci.yml") as source:
    jobs = yaml.safe_load(source)["jobs"]
steps = jobs["check"]["steps"]
check_wiring(recipe, steps)
print("ok: make check runs the download case and CI installs its YAML reader first")

# Both missing halves must be detected; mutate only in-memory inputs.
for name, changed_recipe, changed_steps in (
    ("unwired case", recipe.replace("bash scripts/mobile_journey_fetch_test.sh", "true"), steps),
    ("missing YAML reader", recipe,
     [dict(step, run=step.get("run", "").replace("python3-yaml", "")) for step in steps]),
):
    try:
        check_wiring(changed_recipe, changed_steps)
    except AssertionError:
        print("ok: refuses " + name)
    else:
        raise AssertionError("accepted " + name)
PY
