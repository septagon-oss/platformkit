#!/usr/bin/env bash
# The editor's containers belong to the editor job: it sweeps first, labels every start and drops every
# handle it starts, and check's always() step names none of them, because check never starts them.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "${1:-$root/.gitea/workflows}/ci.yml" <<'PY'
import re
import sys

import yaml

jobs = yaml.safe_load(open(sys.argv[1]))["jobs"]
failures = []

def cleanup(job):
    steps = [s for s in jobs[job]["steps"] if "always()" in str(s.get("if", ""))]
    return steps[0] if steps else None

editor = jobs["editor"]["steps"]
if editor[0].get("name") != "Remove containers a killed job left on this runner":
    failures.append("editor job's first step is not the sweep")
starts = [s for s in editor if re.search(r"docker (container )?run -d", s.get("run") or "")]
if not starts:
    failures.append("editor job starts no container: the relocation was lost")
for step in starts:
    for line in re.findall(r"docker (?:container )?run -d[^\n]*", step["run"]):
        if "--label pkit.ci.run=${{ github.run_id }}" not in line:
            failures.append(f"editor step {step['name']!r} starts a container without the run label")
editor_cleanup = cleanup("editor")
if editor_cleanup is None:
    failures.append("editor job has no always() step")
else:
    env = editor_cleanup.get("env") or {}
    for handle in ("EDITOR_CONTAINER", "PREVIEW_CONTAINER", "COMPLETE_PREVIEW_CONTAINER", "EDITOR_NAME", "PREVIEW_NAME"):
        if handle not in env:
            failures.append(f"editor job's always() step does not name {handle}")
check_cleanup = cleanup("check")
if check_cleanup is None:
    failures.append("check job has no always() step")
else:
    text = str(check_cleanup.get("env")) + (check_cleanup.get("run") or "")
    for handle in ("EDITOR_", "PREVIEW_"):
        if handle in text:
            failures.append(f"check job's always() step names {handle}*, a container another job owns")
    for handle in ("NATS_NAME", "VALKEY_NAME", "MAILPIT_NAME", "S3_NAME"):
        if handle not in (check_cleanup.get("env") or {}):
            failures.append(f"check job's always() step does not name {handle}")

for failure in failures:
    print(f"FAIL: {failure}")
if failures:
    sys.exit(1)
print("ok   editor job sweeps first, labels its starts and drops its own handles; check names only its own")
PY
