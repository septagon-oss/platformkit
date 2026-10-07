#!/usr/bin/env bash
# The lock the design job audits sits above the advisories that job was refused for.
#
# Why this file exists. The `design` job runs three npm commands in `tools/designexport/openpencil`:
# a locked install, an audit of what that install produced, and the native suites. At `0bfae63` the
# middle one exited 1 (forge job 51838 at 2026-10-06T05:17:44Z, the run's only failed step, which the
# forge names `Native design conformance and dependency audit`):
#
#   source-map-js  1.0.0 - 1.2.1  Severity: high  GHSA-68fv-2mgg-jv7q                     (pinned 1.2.1)
#   dompurify      <=3.4.15                        GHSA-p98j-92pf-mc4p, GHSA-6688-9rhm-gjv2 (3.4.14)
#
# and `--audit-level=high` is what turns an audit report of that shape into a red tick rather than a
# paragraph. Nothing in this repository moved: forge run 51055 on `main` reports the same step `success`
# over lockfile bytes byte-identical to the ones that head carries (`git diff f8bad33 0bfae63 -- tools/
# designexport/openpencil/package-lock.json` answers nothing), and both fixed releases postdate the
# commit that last wrote this lock on 2026-09-11 — source-map-js 1.2.2 published 2026-09-30, dompurify
# 3.4.16 on 2026-09-23. npm's advisory feed is what moved under the runner — which is why this case
# reads the lock and never npm's feed. The one question a working tree can answer about a feed it does
# not hold is whether the version it pinned falls inside a range npm has said it will report.
#
# Both fixes were already inside the range the parent declares, so the cure is the lock and not a new
# `overrides` entry (which this directory's package.json reserves for the two packages whose published
# *identity* has to be replaced) and not `npm audit fix --force` (which the tool's own README forbids,
# because it is free to downgrade the pinned SDK past the source-hash checks):
#
#   @open-pencil/core -> unifont -> css-tree -> source-map-js ^1.2.1   -> 1.2.2
#   @open-pencil/core -> jspdf (optional) -> dompurify ^3.3.1          -> 3.4.16
#
# The parent's range is not re-derived here, because `npm ci` on the line above the audit already
# refuses a lock that fails one and names the offending edge; re-checking that fact would be a number
# nothing reads. What `npm ci` cannot see is which side of an advisory a locked version lands on.
#
# A package that has left the lock is not a finding: jspdf and unifont bring these two in, and a
# vulnerability that leaves with its parent is gone rather than fixed. The finding is a package this
# file names, present, at a version inside a range npm reports.
#
# Both verdicts go through one function each, and the mutations at the end feed that same function: a
# version printed as "locked above" without the comparison behind it prints `ok` about a lock nobody
# compared. The first draft of this file did exactly that, and the mutation below is what caught it.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

python3 - "$root" <<'PY'
import json
import re
import sys
from pathlib import Path

import yaml

# `since`/`upto` are npm's own range edges, inclusive; `since` None is npm's `<=`. `parent` is the
# package that declares the edge, kept for the message a person reads when this fires.
ADVISORIES = (
    {
        "package": "source-map-js",
        "since": (1, 0, 0),
        "upto": (1, 2, 1),
        "ids": ("GHSA-68fv-2mgg-jv7q",),
        "severity": "high",
        "parent": "css-tree",
    },
    {
        "package": "dompurify",
        "since": None,
        "upto": (3, 4, 15),
        "ids": ("GHSA-p98j-92pf-mc4p", "GHSA-6688-9rhm-gjv2"),
        "severity": "low",
        "parent": "jspdf",
    },
)

# The three commands of the gate, in the order the design job runs them.
GATE = ("npm ci --ignore-scripts", "npm audit --omit=dev --audit-level=high", "npm test")
AUDIT_AT_HIGH = re.compile(r"^\s*npm audit[^\n]*--audit-level=high\s*$", re.M)
COMMANDS = re.compile(r"^\s*(npm (?:ci|audit|test)[^\n]*)\s*$", re.M)


def version(text):
    """(3, 4, 15) from "3.4.15". No suffix handling: npm reports both advisories on stable ranges
    and no version this lock holds carries one."""
    parts = re.match(r"(\d+)\.(\d+)\.(\d+)", text)
    if not parts:
        sys.exit(f"FAIL: cannot read the version {text!r} as major.minor.patch")
    return tuple(int(p) for p in parts.groups())


def range_text(advisory):
    """npm's own range for one advisory, the way its audit report prints it."""
    upto = ".".join(str(p) for p in advisory["upto"])
    if advisory["since"] is None:
        return f"<={upto}"
    return f"{'.'.join(str(p) for p in advisory['since'])} - {upto}"


def inside(v, advisory):
    """Whether one version tuple falls in npm's inclusive range for an advisory."""
    lower = advisory["since"]
    return (lower is None or v >= lower) and v <= advisory["upto"]


def lock_verdict(packages):
    """`(ok lines, failures)` for one parsed package-lock.json."""
    ok, failures = [], []
    for advisory in ADVISORIES:
        name = advisory["package"]
        node = packages.get(f"node_modules/{name}") or {}
        locked = node.get("version")
        if locked is None:
            ok.append(f"{name} is not in this lock ({advisory['parent']} no longer brings it in), "
                      f"so nothing sits inside {range_text(advisory)}")
            continue
        if inside(version(locked), advisory):
            failures.append(
                f"{name}@{locked} is inside the range npm reports as vulnerable "
                f"({name}  {range_text(advisory)}, {', '.join(advisory['ids'])}, severity "
                f"{advisory['severity']}); it reaches this tree through {advisory['parent']}, "
                "and the design job's `npm audit --omit=dev --audit-level=high` refuses it")
            continue
        ok.append(f"{name} {locked} is locked above {name}  {range_text(advisory)} "
                  f"({', '.join(advisory['ids'])})")
    return ok, failures


def audit_steps(steps):
    """The run steps of one job that audit at the level that makes a high finding red."""
    return [s for s in steps if AUDIT_AT_HIGH.search(str(s.get("run") or ""))]


def gate_verdict(steps, directory="tools/designexport/openpencil"):
    """`(ok lines, failures)` for one job's run steps."""
    gates = audit_steps(steps)
    if len(gates) != 1:
        return [], [f"the design job runs {len(gates)} steps at --audit-level=high; exactly one is "
                    "what makes a high finding red there, and this lock is only audited where it does"]
    gate = gates[0]
    commands = COMMANDS.findall(str(gate.get("run")))
    if tuple(commands) != GATE:
        return [], [f"the design job's audit gate is not the three commands this lock is audited "
                    f"between: {commands}"]
    if gate.get("working-directory") != directory:
        return [], [f"the audit gate runs in {gate.get('working-directory')!r}, not in {directory}, "
                    "so it does not audit the lock read above"]
    return [f"the design job installs that lock, audits it at --audit-level=high, then runs its "
            f"tests, in {directory}"], []


def report(ok_lines, failures):
    for line in ok_lines:
        print(f"ok   {line}")
    for line in failures:
        print(f"FAIL: {line}")
    return not failures


root = Path(sys.argv[1])
with open(root / "tools/designexport/openpencil/package-lock.json") as f:
    lock = json.load(f)
packages = lock.get("packages") or {}
ok_lines, failures = lock_verdict(packages)

# The comparison has to bite, and bite means the same function a person's `ok` line came out of. Each
# advisory's last reported-vulnerable version goes into a copy of the real lock and back through
# `lock_verdict`, which is the path that printed the lines above.
for advisory in ADVISORIES:
    node = f"node_modules/{advisory['package']}"
    if node not in packages:
        continue
    mutated = json.loads(json.dumps(lock))
    mutated["packages"][node]["version"] = ".".join(str(p) for p in advisory["upto"])
    _, mutated_failures = lock_verdict(mutated["packages"])
    if not any(advisory["package"] in line for line in mutated_failures):
        sys.exit(f"FAIL: this file does not refuse {advisory['package']} at the last version npm "
                 f"reports: the ok lines above promise nothing")
ok_lines.append("a lock at the last version npm reports is refused by this file's own comparison")

with open(root / ".gitea/workflows/ci.yml") as f:
    jobs = (yaml.safe_load(f) or {}).get("jobs") or {}
design_steps = [s for s in ((jobs.get("design") or {}).get("steps") or []) if s.get("run")]
gate_ok, gate_failures = gate_verdict(design_steps)
ok_lines += gate_ok
failures += gate_failures

# The same question of the gate: the step with its audit line loosened to the level that would let a
# high finding pass must stop being a gate to this file, and its command list must stop matching.
if audit_steps(design_steps):
    loosened = {**audit_steps(design_steps)[0],
                "run": str(audit_steps(design_steps)[0].get("run")).replace(
                    "--audit-level=high", "--audit-level=critical")}
    loosened_ok, loosened_failures = gate_verdict([loosened])
    if loosened_ok or not loosened_failures:
        sys.exit("FAIL: an audit line loosened past --audit-level=high still reads as a gate to this "
                 "file: the line above that says it is enforced promises nothing")
    ok_lines.append("an audit line loosened past --audit-level=high is no longer a gate to this file")

sys.exit(0 if report(ok_lines, failures) else 1)
PY
