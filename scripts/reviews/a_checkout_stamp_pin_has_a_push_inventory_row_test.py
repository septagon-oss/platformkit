"""The committed checkout stamp pin stays registered in the push tier."""

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
PIN = "scripts/reviews/the_checkout_stamp_uses_its_revision_before_its_tag_test.py"


def test_checkout_stamp_pin_is_registered_for_push():
    rows = json.loads((ROOT / "tests/inventory.json").read_text(encoding="utf-8"))["rows"]
    row = next((row for row in rows if row["file"] == PIN), None)

    assert row is not None, f"{PIN} is absent from the test inventory"
    assert row["tier"] == "push", f"{PIN} is in {row['tier']!r}, not the push tier"
    assert row["kind"] == "shell_pin", f"{PIN} is classified as {row['kind']!r}"
