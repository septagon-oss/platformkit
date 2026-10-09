#!/usr/bin/env bash
# The pin that executes both source-gate guards under make's inherited flags must itself be in check.
#
# scripts/journey_check_recipe_test.sh was written by a review, adopted byte-for-byte at f676a6e, and
# left unwired: correct, run by nobody, and a gate nobody runs is prose. Wiring it is one line, and the
# line needs the one thing the previous two lines have — a case that refuses its disappearance — because
# `go tool gotestsum --packages='./...'` sees no shell script, so a recipe line that leaves this list
# leaves nothing else red.
#
# This case asks the dry-run recipe for the pinned command, then asks whether that request would notice:
# it copies this Makefile into a temporary tree, deletes the one line, and requires the same assertion to
# fail there. Both questions read text and start nothing — no node, no browser, no database, no listener,
# and no second nested make beyond the dry run `scripts/check_architecture_test.sh` already runs from
# inside this same recipe.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
python3 -B - "$root" <<'PY'
from pathlib import Path
import subprocess
import sys
import tempfile

root = Path(sys.argv[1])
pin = "bash scripts/journey_check_recipe_test.sh"


def recipe(tree):
    """Every line `make check` would run for this tree, as make prints it."""
    done = subprocess.run(
        ["make", "--no-print-directory", "-n", "check"], cwd=tree,
        stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60,
    )
    assert done.returncode == 0, done.stdout + done.stderr
    return done.stdout.splitlines()


def require_pin(tree):
    assert pin in recipe(tree), f"make check no longer runs {pin}"


require_pin(root)
print(f"ok: make check runs {pin}")

with tempfile.TemporaryDirectory(prefix="journey-check-recipe-wiring-") as directory:
    tree = Path(directory)
    # go.mod reduced to what the Makefile reads at parse time, the shape
    # scripts/make_check_count_default_test.sh and scripts/check_architecture_test.sh already copy.
    declared = tuple(line for line in (root / "go.mod").read_text().splitlines(keepends=True)
                     if line.startswith(("module ", "go ", "toolchain ")))
    (tree / "go.mod").write_text("".join(declared))
    original = (root / "Makefile").read_text()
    lines = original.splitlines(keepends=True)
    kept = [line for line in lines if line.strip() != pin]
    assert len(kept) == len(lines) - 1, f"expected exactly one recipe line running {pin}"
    (tree / "Makefile").write_text(original)
    require_pin(tree)
    print("ok: the copied recipe still runs it")

    (tree / "Makefile").write_text("".join(kept))
    try:
        require_pin(tree)
    except AssertionError as refusal:
        print(f"ok: a recipe that lost it is refused: {refusal}")
    else:
        raise SystemExit(f"FAIL: a check recipe that lost {pin} was accepted")
PY
