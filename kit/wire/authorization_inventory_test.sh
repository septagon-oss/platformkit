#!/usr/bin/env bash
# The authorization cases remain runnable stack-free tests after inventory registration.
# Pass an alternate inventory path to check a fixture without changing the repository.
set -euo pipefail

wire_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python3 -B - "$wire_root" "${1:-$wire_root/tests/inventory.json}" <<'PY'
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
rows = json.loads(Path(sys.argv[2]).read_text())["rows"]
cases = {
    "wire_test.go": (
        "TestFeatureDeclarationIsCompared",
        "TestGoldenRefusesFeatureMutationsBeforeWriting",
        "TestAllowancesCoverOnlyWhatTheyName",
        "TestAuthorizationIdentitySpellings",
    ),
    "reviewed_widening_gains_no_feature_test.go": ("TestAReviewedWideningCoversNoFeatureTheDoorGained",),
    "authorization_member_names_test.go": ("TestAuthorizationMemberNamesCannotBorrowReviewedTransitions",),
    "allowance_quoted_member_spelling_test.go": ("TestAnAllowanceOverAQuotedMemberNamesItsExactSpelling",),
}
for filename, names in cases.items():
    path = f"kit/wire/{filename}"
    source = (root / path).read_text()
    for name in names:
        identity = f"{path}#{name}"
        found = [row for row in rows if row["id"] == identity]
        assert len(found) == 1, f"{identity}: expected exactly one inventory row"
        row = found[0]
        assert re.search(rf"^func {name}\(t \*testing\.T\)", source, re.M), f"{identity}: test missing"
        assert row["tier"] == "push" and row["needs_db"] is False, f"{identity}: no longer stack-free"
        assert row["pkg"] == "kit/wire" and row["kind"] == "go", f"{identity}: wrong runner"
        assert row["verdict"] in ("keep", "keep-rewrite"), f"{identity}: no longer kept"
print("authorization inventory: every named compatibility case is kept and stack-free")
PY
