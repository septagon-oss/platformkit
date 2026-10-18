#!/usr/bin/env bash
# The file:// address the fetch step exports is the PK_MOBILE_APK that make mobile-e2e reads.
#
# The fetch step writes PK_MOBILE_APK=file://… to $GITHUB_ENV, and the harness reads PK_MOBILE_APK
# from its environment. A step after the fetch that names PK_MOBILE_APK in its own env:, or writes it
# to $GITHUB_ENV again, competes with that export exactly as a job-level entry would: if it wins, the
# harness's plain curl is handed the sign-in-gated release address back, the failure the fetch step
# exists to remove. Nothing else checks this, and a dispatched run that stops before the harness
# downloads cannot show it.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$root/.gitea/workflows/mobile.yml" "$root/scripts/mobile_e2e.sh" <<'HANDOFF'
import re
import sys

import yaml

workflow, harness_script = sys.argv[1:3]
steps = yaml.safe_load(open(workflow))["jobs"]["journey"]["steps"]
runs = [str(step.get("run", "")) for step in steps]
fetch = [i for i, run in enumerate(runs) if "PK_MOBILE_APK=file://" in run and "GITHUB_ENV" in run]
if len(fetch) != 1:
    sys.exit("FAIL: expected one step exporting PK_MOBILE_APK as a file:// URL, found %d" % len(fetch))
harness = [i for i, run in enumerate(runs) if run.strip() == "make mobile-e2e"]
if len(harness) != 1 or harness[0] < fetch[0]:
    sys.exit("FAIL: make mobile-e2e does not run once, after the step that exports the build")
for i in range(fetch[0] + 1, harness[0] + 1):
    name = steps[i].get("name") or "step %d" % i
    if "PK_MOBILE_APK" in (steps[i].get("env") or {}):
        sys.exit("FAIL: step %r declares PK_MOBILE_APK in its own env:, which competes with the "
                 "fetch step's file:// export" % name)
    if re.search(r"PK_MOBILE_APK=.*GITHUB_ENV", runs[i]):
        sys.exit("FAIL: step %r writes PK_MOBILE_APK to $GITHUB_ENV after the fetch step" % name)
if not re.search(r'^apk="\$\{PK_MOBILE_APK:-\}"$', open(harness_script).read(), re.M):
    sys.exit("FAIL: scripts/mobile_e2e.sh no longer reads the build's address from PK_MOBILE_APK")
print("ok: no step between the fetch and make mobile-e2e re-declares PK_MOBILE_APK, so the harness "
      "reads the file:// export")
HANDOFF
