#!/usr/bin/env bash
# The published journey library's tests retain their classifications after an inventory merge.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 -B - <<'PY'
import copy
import json
from pathlib import Path

specs = (
    "detail-field-name", "header-floor-refusals", "kernel-session-form-steps",
    "sign-in-fragment-question", "sign-in-return-address", "sign-in-return-fragment",
    "step-response-headers",
)
pins = (
    "e2e_step_library", "invitation_response_wait", "journey_and_pillar_check_refusal",
    "journey_and_pillar_check_wiring", "journey_check_recipe", "journey_check_recipe_wiring",
    "journey_wiring_guard_recipe",
)
expected = {
    **{f"e2e/{name}.spec.ts": ("playwright", "journey", "nightly", True) for name in specs},
    **{f"scripts/{name}_test.sh": ("shell_pin", "composition", "push", False) for name in pins},
}

def validate(rows):
    for file, classification in expected.items():
        assert Path(file).is_file(), f"published test missing: {file}"
        matches = [row for row in rows if row["file"] == file]
        assert len(matches) == 1, f"expected one inventory row for {file}, got {len(matches)}"
        row = matches[0]
        actual = tuple(row[key] for key in ("kind", "layer", "tier", "needs_db"))
        assert actual == classification, f"classification changed for {file}: {actual}"
        assert row["verdict"] == "keep", f"published test is not kept: {file}"

rows = json.loads(Path("tests/inventory.json").read_text())["rows"]
validate(rows)
file = next(iter(expected))
missing = [row for row in rows if row["file"] != file]
duplicate = rows + [next(row for row in rows if row["file"] == file)]
changed = copy.deepcopy(rows)
next(row for row in changed if row["file"] == file)["tier"] = "push"
for description, fixture in (("missing row", missing), ("duplicate row", duplicate), ("changed tier", changed)):
    try:
        validate(fixture)
    except AssertionError as error:
        print(f"ok: refuses {description}: {error}")
    else:
        raise AssertionError(f"accepted {description}")
print("ok: the journey library's own test rows survive an inventory merge")
PY
