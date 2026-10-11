#!/usr/bin/env bash
# One exact Go cache key has one job that saves it.
#
# A save step in .gitea/workflows/ci.yml runs only when its restore was not an exact hit, so once an
# archive exists under a key no job ever saves under that key again. On act_runner's cache server the
# entry that answers an exact key is fixed by whichever save it kept: the v2 protocol refuses a second
# reservation of an existing (key, version) and the client skips the upload, so the first job to
# finish owns the key; the v1 protocol keeps the newest, so the last job to finish owns it
# (act/artifactcache/handler_v2.go, handler.go `findExactCache`, `evictSuperseded`). A restore stamps
# `UsedAt`, so retention never clears an entry that is read every run.
#
# Two jobs that run side by side (no `needs:` between them) and save different trees under one exact
# key therefore leave the key holding whichever tree came first or last, and the other job restores
# it, takes an exact hit and never saves its own: its build stays cold until go.sum or the toolchain
# moves. This case refuses any two saving jobs whose save keys resolve to the same text: the same
# naming command, the same environment for it, and the same `key:` expression.
#
# It passes when only one job saves, or when each saving job's key is its own (a job name in the key
# expression, an argument to the naming script, or a different naming command).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
ci="$root/.gitea/workflows/ci.yml"

python3 - "$ci" <<'PY'
import re
import sys

import yaml

ci = sys.argv[1]
with open(ci) as f:
    jobs = (yaml.safe_load(f) or {}).get("jobs") or {}

savers = {}
for name, job in jobs.items():
    steps = job.get("steps") or []
    by_id = {s.get("id"): s for s in steps if s.get("id")}
    for step in steps:
        uses = str(step.get("uses") or "")
        if not uses.startswith("actions/cache/save@") and not re.match(r"actions/cache@", uses):
            continue
        key = str((step.get("with") or {}).get("key") or "")
        # The key a job saves under is the expression plus whatever the steps it reads ran.
        identity = [key]
        for ref in re.findall(r"steps\.([A-Za-z0-9_-]+)\.outputs", key):
            producer = by_id.get(ref) or {}
            identity.append(str(producer.get("run") or producer.get("uses") or ""))
            identity.append(repr(sorted((producer.get("env") or {}).items())))
            identity.append(repr(sorted((producer.get("with") or {}).items())))
        if "github.job" in key:
            identity.append(name)
        savers.setdefault(tuple(identity), []).append(name)

shared = {k: v for k, v in savers.items() if len(v) > 1}
if shared:
    for identity, names in shared.items():
        print(f"FAIL: jobs {', '.join(names)} save the Go cache under one exact key ({identity[0]}, formed by {identity[1]!r}): "
              "the first or last of them to finish owns the key, and the other restores that archive as an exact hit and never saves its own")
    sys.exit(1)
print(f"ok   each exact cache key in {ci.rsplit('/', 1)[-1]} has one saving job ({len(savers)} key(s))")
PY
