#!/usr/bin/env bash
# The journey's fetch step reads the shell build with the repository secret first, the job token second.
#
# The step's GITHUB_TOKEN is an `a || b` expression: the forge evaluates it left to right and takes the
# first non-empty operand. The job token is never empty, so an expression that names it first never
# reaches the repository secret, and the download keeps the 404 a job token gets on this forge.
# scripts/mobile_journey_fetch_test.sh runs the step with a token it chooses and only asks that both
# names appear; this asks their order, which is what decides which token the forge receives.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$root/.gitea/workflows/mobile.yml" <<'ORDER'
import re
import sys

import yaml

doc = yaml.safe_load(open(sys.argv[1]))
steps = doc["jobs"]["journey"]["steps"]
fetch = [s for s in steps if "PK_MOBILE_APK=file://" in s.get("run", "")]
if len(fetch) != 1:
    sys.exit("FAIL: expected one step exporting PK_MOBILE_APK as a file:// URL, found %d" % len(fetch))
expr = str((fetch[0].get("env") or {}).get("GITHUB_TOKEN", ""))
inner = re.fullmatch(r"\$\{\{\s*(.*?)\s*\}\}", expr.strip())
if not inner:
    sys.exit("FAIL: the fetch step's GITHUB_TOKEN is not one expression: %r" % expr)
operands = [o.strip() for o in inner.group(1).split("||")]
if len(operands) < 2 or operands[-1] != "secrets.GITHUB_TOKEN":
    sys.exit("FAIL: the job token is not the last fallback of %r" % expr)
if "secrets.GITHUB_TOKEN" in operands[:-1] or not re.fullmatch(r"secrets\.[A-Z0-9_]+", operands[0]):
    sys.exit("FAIL: %r reaches the job token before the repository secret, so the secret is never read"
             % expr)
print("ok: the fetch step asks the repository secret before the job token (%s)" % " || ".join(operands))
ORDER
