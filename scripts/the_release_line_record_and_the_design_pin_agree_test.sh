#!/usr/bin/env bash
# The gate's frozen record names a full commit, and the design lock's dompurify override equals the
# version the lock resolves, so neither the release line nor the audited lock drifts from its pin.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json, re, sys
rec = json.load(open("scripts/baselines/public-api-v2.0.0-candidate.json"))
if not re.fullmatch(r"[0-9a-f]{40}", rec["frozen"]["commit"]):
    sys.exit("FAIL: the frozen commit is not a full sha")
d = "tools/designexport/openpencil/"
pkg = json.load(open(d + "package.json"))
lock = json.load(open(d + "package-lock.json"))
want = pkg["overrides"]["dompurify"]
got = lock["packages"]["node_modules/dompurify"]["version"]
if want != got:
    sys.exit(f"FAIL: override pins dompurify {want} but the lock resolves {got}")
print("ok   frozen commit is a full sha; dompurify override equals the locked version")
PY
