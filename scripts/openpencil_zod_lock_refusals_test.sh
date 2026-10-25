#!/usr/bin/env bash
# Both dependency guards refuse a Zod rollback, including when the installer inputs agree.
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
adapter = Path("tools/designexport/openpencil")
with tempfile.TemporaryDirectory(prefix="platformkit-zod-lock-") as directory:
    fixture = Path(directory)
    for relative in (
        Path("scripts/openpencil_lock_above_advisory_test.sh"),
        Path(".gitea/workflows/ci.yml"),
        adapter / "locked-dependencies.test.mjs",
        adapter / "package.json",
        adapter / "package-lock.json",
    ):
        target = fixture / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / relative, target)

    manifest_path = fixture / adapter / "package.json"
    lock_path = fixture / adapter / "package-lock.json"
    manifest = json.loads(manifest_path.read_text())
    lock = json.loads(lock_path.read_text())
    original_node = dict(lock["packages"]["node_modules/zod"])
    original_override = manifest["overrides"]["zod"]

    def run(command, expected):
        manifest_path.write_text(json.dumps(manifest))
        lock_path.write_text(json.dumps(lock))
        result = subprocess.run(
            command, cwd=fixture / adapter, stdin=subprocess.DEVNULL,
            text=True, capture_output=True, timeout=30,
        )
        output = result.stdout + result.stderr
        assert result.returncode == expected, output
        return output

    guard = ["bash", str(fixture / "scripts/openpencil_lock_above_advisory_test.sh")]
    pin = ["node", "--test", "locked-dependencies.test.mjs"]
    run(guard, 0)
    run(pin, 0)
    print("ok   both guards accept the committed Zod lock and override")

    for version in ("4.6.1", "4.6.5"):
        lock["packages"]["node_modules/zod"].update(
            version=version,
            resolved=f"https://registry.npmjs.org/zod/-/zod-{version}.tgz",
        )
        refused = run(guard, 1)
        assert any(
            line.startswith("FAIL:") and f"zod@{version}" in line
            and "GHSA-mrhx-c7xc-m3w5" in line
            for line in refused.splitlines()
        ), refused
        assert "ERR_ASSERTION" in run(pin, 1)
        manifest["overrides"]["zod"] = version
        assert "ERR_ASSERTION" in run(pin, 1)
        print(f"ok   Zod {version} is refused with either the lock alone or both inputs rolled back")
        manifest["overrides"]["zod"] = original_override

    lock["packages"]["node_modules/zod"].update(
        version="4.6.6", resolved="https://registry.npmjs.org/zod/-/zod-4.6.6.tgz",
    )
    run(guard, 0)
    print("ok   the advisory guard accepts the first version outside the affected range")
    lock["packages"]["node_modules/zod"] = original_node
    run(guard, 0)
    run(pin, 0)
    print("ok   restoring the committed inputs restores both passing verdicts")
PY
