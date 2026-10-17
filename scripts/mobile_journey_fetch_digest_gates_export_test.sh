#!/usr/bin/env bash
# The journey's fetch step exports the shell build only after its digest matches, and every refusal
# names the address it tried.
#
# The step's file:// export is what hands the build to the harness; a digest check placed after it,
# or one that does not exit, would install a build the pin does not name. A download refusal that
# drops the address leaves the reader of a red run guessing which route the token was refused on.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$root/.gitea/workflows/mobile.yml" <<'ORDER'
import sys

import yaml

steps = yaml.safe_load(open(sys.argv[1]))["jobs"]["journey"]["steps"]
fetch = [s["run"] for s in steps if "PK_MOBILE_APK=file://" in s.get("run", "")]
if len(fetch) != 1:
    sys.exit("FAIL: expected one step exporting PK_MOBILE_APK as a file:// URL, found %d" % len(fetch))
lines = fetch[0].splitlines()
export = next(i for i, l in enumerate(lines) if "PK_MOBILE_APK=file://" in l)
check = [i for i, l in enumerate(lines) if "sha256sum --check" in l and "PK_MOBILE_APK_SHA256" in l]
if len(check) != 1 or check[0] > export:
    sys.exit("FAIL: the digest is not checked once before the file:// export (check at %r, export at %d)"
             % (check, export))
if not lines[check[0]].lstrip().startswith("if ! ") or "exit 1" not in "\n".join(lines[check[0]:export]):
    sys.exit("FAIL: a digest mismatch does not stop the step before the export: %r" % lines[check[0]])
curls = [i for i, l in enumerate(lines) if l.lstrip().startswith("if ! curl ")]
for i in curls:
    refusal = next((l for l in lines[i + 1:i + 4] if l.lstrip().startswith("echo ")), "")
    if "$release_url" not in refusal and "$asset_url" not in refusal:
        sys.exit("FAIL: the refusal after %r does not name the address it tried" % lines[i].strip())
if len(curls) != 2:
    sys.exit("FAIL: expected the release read and the download, found %d curl reads" % len(curls))
print("ok: the fetch step checks the digest before it exports the build, and each refusal names its address")
ORDER
