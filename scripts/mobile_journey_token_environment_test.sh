#!/usr/bin/env bash
# The job-token probe is handed the fetch's pin, the harness's digest and no credential but the job token.
#
# The probe decides which credential the journey keeps: a byte read that prints pin_match=true with the
# job token alone removes PK_MOBILE_TOKEN, anything else keeps the owner's replacement route.
# scripts/mobile_journey_fetch_test.sh runs the probe's body with an environment it writes by hand, so
# it proves the body's decision and not what the workflow gives it. This reads that environment as the
# runner composes it — the job's env:, then the step's — and asks three things of it: the pin is the one
# the fetch resolves and the digest the one the harness checks, so a readable build can print
# pin_match=true; the only secret that reaches the step, by env: or by an expression in its body, is the
# job token, so a pass is never the repository secret's; and the step runs unconditionally before the
# first step that needs a device, so a host without one still records the measurement.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "$root/.gitea/workflows/mobile.yml" <<'ENVIRONMENT'
import re
import sys

import yaml

doc = yaml.safe_load(open(sys.argv[1]))
job = doc["jobs"]["journey"]
steps = job["steps"]
problems = []


def effective(step):
    composed = dict(job.get("env") or {})
    composed.update(step.get("env") or {})
    return {name: str(value) for name, value in composed.items()}


def named(text):
    return [i for i, step in enumerate(steps) if step.get("name") == text]


probes = named("Probe the pinned asset with only the job token")
fetch = [i for i, step in enumerate(steps) if "PK_MOBILE_APK=file://" in step.get("run", "")]
harness = [i for i, step in enumerate(steps) if step.get("run", "").strip() == "make mobile-e2e"]
device = [i for i, step in enumerate(steps) if re.search(r"\b(adb|emulator)\b", step.get("run", ""))]
if len(probes) != 1 or len(fetch) != 1 or len(harness) != 1:
    sys.exit("FAIL: expected one probe, one fetch and one harness step, found %d, %d and %d"
             % (len(probes), len(fetch), len(harness)))
probe = steps[probes[0]]
given = effective(probe)

if given.get("PK_MOBILE_APK") != effective(steps[fetch[0]]).get("PK_MOBILE_APK"):
    problems.append("the probe's PK_MOBILE_APK (%r) is not the pin the fetch resolves (%r)"
                    % (given.get("PK_MOBILE_APK"), effective(steps[fetch[0]]).get("PK_MOBILE_APK")))
digest = effective(steps[harness[0]]).get("PK_MOBILE_APK_SHA256")
if not digest or given.get("PK_MOBILE_APK_SHA256") != digest:
    problems.append("the probe's PK_MOBILE_APK_SHA256 (%r) is not the digest the harness checks (%r), "
                    "so no read can print pin_match=true" % (given.get("PK_MOBILE_APK_SHA256"), digest))
if given.get("GITHUB_TOKEN") != "${{ secrets.GITHUB_TOKEN }}":
    problems.append("the probe's GITHUB_TOKEN is %r, not the job token alone" % given.get("GITHUB_TOKEN"))
secrets = set(re.findall(r"secrets\.([A-Za-z0-9_]+)", " ".join(given.values())))
secrets |= set(re.findall(r"secrets\.([A-Za-z0-9_]+)", probe.get("run", "")))
if secrets - {"GITHUB_TOKEN"}:
    problems.append("secrets other than the job token reach the probe: %s" % ", ".join(sorted(secrets - {"GITHUB_TOKEN"})))
if "${{" in probe.get("run", ""):
    problems.append("the probe's body carries an expression the runner substitutes before it runs")
if "if" in probe:
    problems.append("the probe carries a condition, so a run can end without the measurement")
if fetch[0] < probes[0] or (device and device[0] < probes[0]):
    problems.append("the probe runs after the fetch or after a step that needs a device")
if not device:
    problems.append("no step names adb or the emulator, so the probe's place before them is unchecked")

if problems:
    sys.exit("".join("FAIL: %s\n" % p for p in problems).rstrip("\n"))
print("ok: the job-token probe is handed the fetch's pin, the harness's digest and no credential but the "
      "job token, ahead of the device steps")
ENVIRONMENT
