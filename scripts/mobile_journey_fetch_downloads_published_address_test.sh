#!/usr/bin/env bash
# The journey's fetch step downloads the address the release document publishes for the pinned
# asset, never the pin itself, and never prints its token.
#
# The pin is how the mobile owner names a build; the release the API returns is where the forge says
# that build is. On this forge the two addresses are the same today, so a step that read the release
# and then downloaded the pin anyway would pass every behavioural case and stop working the day the
# forge publishes assets elsewhere on its own host. The token is a repository secret: a refusal or a
# trace that prints it puts it in a job log.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$root/.gitea/workflows/mobile.yml" <<'SHAPE'
import re
import sys

import yaml

steps = yaml.safe_load(open(sys.argv[1]))["jobs"]["journey"]["steps"]
fetch = [s["run"] for s in steps if "PK_MOBILE_APK=file://" in s.get("run", "")]
if len(fetch) != 1:
    sys.exit("FAIL: expected one step exporting PK_MOBILE_APK as a file:// URL, found %d" % len(fetch))
run = fetch[0]
lines = run.splitlines()
download = [l for l in lines if l.lstrip().startswith("if ! curl ") and '-o "$dest"' in l]
if len(download) != 1:
    sys.exit("FAIL: expected one curl writing the build to $dest, found %d" % len(download))
if '"$asset_url"' not in download[0] or "$PK_MOBILE_APK" in download[0]:
    sys.exit("FAIL: the build is downloaded from %r, not from the address the release publishes"
             % download[0].strip())
assigned = re.search(r'asset_url="\$\(python3 [^\n]*<<\'(\w+)\'\n(.*?)\n\s*\1\n', run, re.S)
if not assigned or "browser_download_url" not in assigned.group(2):
    sys.exit("FAIL: asset_url is not read from the release document's browser_download_url")
if re.search(r"^\s*set\s+-[a-z]*x", run, re.M):
    sys.exit("FAIL: the fetch step traces its commands, which prints the Authorization header")
for l in lines:
    if l.lstrip().startswith("echo ") and "GITHUB_TOKEN" in l:
        sys.exit("FAIL: a line of the fetch step prints its token: %r" % l.strip())
print("ok: the fetch step downloads the address the release publishes and prints no token")
SHAPE
