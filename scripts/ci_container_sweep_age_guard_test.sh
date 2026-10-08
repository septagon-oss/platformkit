#!/usr/bin/env bash
# The age guard must reject a sweep that deletes young containers or keeps old ones.
# Reuse its Docker double by invoking the existing guard on temporary workflow copies.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "$root" <<'PY'
import pathlib
import subprocess
import sys
import tempfile

root = pathlib.Path(sys.argv[1])
guard = root / "scripts/ci_container_sweep_age_test.sh"
original = {p.name: p.read_text() for p in (root / ".gitea/workflows").glob("*.yml")}
comparison = '[ "$epoch" -le "$cutoff" ] || continue'
cases = [
    ("the committed sweeps", None, None, True),
    ("an inverted age comparison", comparison,
     '[ "$epoch" -ge "$cutoff" ] || continue', False),
    ("a missing age comparison", comparison, ":", False),
    ("a bound that retains four-hour-old containers",
     "SWEEP_OLDER_THAN: '10800'", "SWEEP_OLDER_THAN: '99999'", False),
]
for label, needle, replacement, accepted in cases:
    with tempfile.TemporaryDirectory(prefix="ci-sweep-age-guard-") as directory:
        folder = pathlib.Path(directory)
        changed = 0
        for name, text in original.items():
            if needle is not None:
                changed += text.count(needle)
                text = text.replace(needle, replacement)
            (folder / name).write_text(text)
        if needle is not None and changed == 0:
            sys.exit(f"FAIL: {label}: no sweep matched the mutation")
        result = subprocess.run(
            ["bash", str(guard), str(folder)], stdin=subprocess.DEVNULL,
            capture_output=True, text=True, timeout=120,
        )
        if accepted:
            correct = result.returncode == 0 and "ok   " in result.stdout
        else:
            correct = result.returncode == 1 and "FAIL:" in result.stdout
        if not correct:
            sys.exit(f"FAIL: {label}: exit {result.returncode}\n"
                     f"{result.stdout}{result.stderr}")
        print(f"ok   {label}: {'accepted' if accepted else 'refused'}")
PY
