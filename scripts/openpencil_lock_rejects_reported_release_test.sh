#!/usr/bin/env bash
# The advisory guard accepts the current lock and identifies each advisory on a reported release.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "$root" <<'PY'
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

root = Path(sys.argv[1])
with tempfile.TemporaryDirectory(prefix="platformkit-advisory-rollback-") as directory:
    fixture = Path(directory)
    for relative in (
        "scripts/openpencil_lock_above_advisory_test.sh",
        ".gitea/workflows/ci.yml",
        "tools/designexport/openpencil/package-lock.json",
    ):
        target = fixture / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / relative, target)

    def check(expected):
        result = subprocess.run(
            ["bash", str(fixture / "scripts/openpencil_lock_above_advisory_test.sh")],
            stdin=subprocess.DEVNULL, text=True, capture_output=True, timeout=30,
        )
        assert result.returncode == expected, result.stdout + result.stderr
        return result.stdout.splitlines()

    check(0)
    print("ok   the advisory guard accepts the current lock")
    path = fixture / "tools/designexport/openpencil/package-lock.json"
    lock = json.loads(path.read_text())
    lock["packages"]["node_modules/dompurify"]["version"] = "3.4.16"
    path.write_text(json.dumps(lock))
    refused = check(1)
    for advisory in ("GHSA-cg2g-xh64-v4q7", "GHSA-mrv2-3pqr-j5jr", "GHSA-c6gr-qvv4-5v3f"):
        assert any(line.startswith("FAIL:") and advisory in line for line in refused), refused
        print(f"ok   the advisory guard refuses DOMPurify 3.4.16 and names {advisory}")
PY
