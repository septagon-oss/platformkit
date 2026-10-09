#!/usr/bin/env bash
# The shared wiring pin must refuse either source gate disappearing from check.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
python3 -B - "$root" <<'PY'
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

root = Path(sys.argv[1])
guard = "scripts/journey_and_pillar_check_wiring_test.sh"
with tempfile.TemporaryDirectory(prefix="journey-check-wiring-") as directory:
    tree = Path(directory)
    (tree / "scripts").mkdir()
    for name in ("Makefile", "go.mod", guard):
        shutil.copyfile(root / name, tree / name)
    original = (tree / "Makefile").read_text()

    def run(expected_reason=None):
        result = subprocess.run(
            ["bash", guard], cwd=tree, stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=30,
        )
        output = result.stdout + result.stderr
        if expected_reason is None:
            assert result.returncode == 0, output
        else:
            assert result.returncode != 0, "missing gate was accepted"
            assert expected_reason in output, output

    run()
    print("ok: the composed prerequisites are accepted")
    for target, command in (
        ("check-pillars", "python3 -B scripts/architecture_pillars_test.py"),
        ("check-step-library", "bash scripts/e2e_step_library_test.sh"),
    ):
        lines = original.splitlines(keepends=True)
        declarations = [i for i, line in enumerate(lines) if line.startswith("check:")]
        assert len(declarations) == 1, "expected one check prerequisite declaration"
        index = declarations[0]
        assert target in lines[index].split(), f"missing prerequisite {target}"
        lines[index] = lines[index].replace(" " + target, "", 1)
        (tree / "Makefile").write_text("".join(lines))
        run("make check no longer runs " + command)
        print(f"ok: removing {target} is refused by the shared pin")
PY
