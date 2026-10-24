#!/usr/bin/env bash
# The release guard refuses old request dependencies even when both installer inputs agree.
set -euo pipefail

directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
python3 - "$directory" <<'PY'
import copy
import json
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

source = Path(sys.argv[1])
manifest = json.loads((source / "package.json").read_text())
lock = json.loads((source / "package-lock.json").read_text())
reported = {
    "undici": {
        "version": "8.10.2",
        "resolved": "https://registry.npmjs.org/undici/-/undici-8.10.2.tgz",
        "integrity": "sha512-/y4/bH9YNU5hi9NIrpOuvGXFcxrj3CMrV+/AYpowAYTpHn8gX/XPFjNy766FPoYY0miQhdW977JFWKGNhBdwyQ==",
    },
    "zod": {
        "version": "4.6.1",
        "resolved": "https://registry.npmjs.org/zod/-/zod-4.6.1.tgz",
        "integrity": "sha512-341aRWQsve0rvronKNTqZpjmzdbUDlFuzHaI/XLg/Ej82qffDJRRfBTCuv7+9q/rMjB6LSLyEBnW4InJeMtt/Q==",
    },
}

with tempfile.TemporaryDirectory(prefix="platformkit-request-dependencies-") as directory:
    fixture = Path(directory)
    shutil.copyfile(source / "locked-dependencies.test.mjs", fixture / "locked-dependencies.test.mjs")

    def check(manifest, lock, expected):
        (fixture / "package.json").write_text(json.dumps(manifest))
        (fixture / "package-lock.json").write_text(json.dumps(lock))
        result = subprocess.run(
            ["node", "--test", "locked-dependencies.test.mjs"], cwd=fixture,
            stdin=subprocess.DEVNULL, text=True, capture_output=True, timeout=30,
        )
        assert result.returncode == expected, result.stdout + result.stderr
        if expected:
            assert "ERR_ASSERTION" in result.stdout, result.stdout + result.stderr

    check(manifest, lock, 0)
    print("ok   the release guard accepts the current installer inputs")
    for name, entry in reported.items():
        changed_lock = copy.deepcopy(lock)
        changed_lock["packages"][f"node_modules/{name}"].update(entry)
        check(manifest, changed_lock, 1)
        print(f"ok   the release guard refuses {name} {entry['version']} in the lock")

        changed_manifest = copy.deepcopy(manifest)
        changed_manifest["overrides"][name] = entry["version"]
        check(changed_manifest, changed_lock, 1)
        print(f"ok   the release guard refuses matching {name} {entry['version']} installer inputs")
PY
