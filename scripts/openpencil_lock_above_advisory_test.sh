#!/usr/bin/env bash
# Keep the design lock above the published source-map-js, DOMPurify, Zod and Undici advisories.
# The audit threshold and its place between install and conformance stay enforced.
# Mutated copies of the lock exercise the same comparison as the real lock;
# a removed package is safe, but a present vulnerable version is refused.
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
        "upto": (3, 4, 16),
        "ids": ("GHSA-cg2g-xh64-v4q7", "GHSA-mrv2-3pqr-j5jr", "GHSA-c6gr-qvv4-5v3f"),
        "severity": "critical",
        "parent": "jspdf",
    },
    {
        "package": "dompurify",
        "since": None,
        "upto": (3, 4, 16),
        "ids": ("GHSA-mrv2-3pqr-j5jr", "GHSA-c6gr-qvv4-5v3f"),
        "severity": "high",
        "parent": "jspdf",
    },
    {
        "package": "dompurify",
        "since": (3, 4, 4),
        "upto": (3, 4, 16),
        "ids": ("GHSA-cg2g-xh64-v4q7",),
        "severity": "critical",
        "parent": "jspdf",
    },
    {
        # A peer dependency rather than a nested one: `@ai-sdk/provider-utils` declares
        # `zod ^3.25.76 || ^4.1.8`, so `npm ci` installs it at the top of the tree and
        # `--omit=dev` audits it, because omitting dev-dependencies does not omit a peer.
        "package": "zod",
        "since": None,
        "upto": (4, 6, 5),
        "ids": ("GHSA-mrhx-c7xc-m3w5",),
        "severity": "high",
        "parent": "@ai-sdk/provider-utils",
    },
    {
        # npm reports one range for all three ids, so this edge stands for each of them.
        "package": "undici",
        "since": (8, 0, 0),
        "upto": (8, 12, 1),
        "ids": ("GHSA-4qv8-2p9j-2685", "GHSA-f72v-6w45-q472", "GHSA-xjj6-f33q-929c"),
        "severity": "high",
        "parent": "unifont",
    },
)

# The three commands of the gate, in the order the design job runs them. The audit may stand behind
# scripts/ci_npm_audit.sh, which retries only a registry that would not answer; anything else between
# this file and the audit line is not the gate, which is why the prefix is spelled out rather than
# matched loosely.
GATE = ("npm ci --ignore-scripts", "npm audit --omit=dev --audit-level=high", "npm test")
AUDIT_AT_HIGH = re.compile(r"^\s*(?:bash\s+\S*scripts/ci_npm_audit\.sh\s+)?npm audit[^\n]*--audit-level=high\s*$", re.M)
COMMANDS = re.compile(r"^\s*(?:bash\s+\S*scripts/ci_npm_audit\.sh\s+)?(npm (?:ci|audit|test)[^\n]*)\s*$", re.M)


def version(text):
    """(3, 4, 15) from "3.4.15". No suffix handling: every advisory below reports a stable range,
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

    # The retry this step is allowed, given to something that throws the answer away. A gate whose
    # verdict can be swallowed is a gate that prints a paragraph, which is what --audit-level is for.
    swallowed = {**audit_steps(design_steps)[0],
                 "run": str(audit_steps(design_steps)[0].get("run")).replace(
                     "--audit-level=high", "--audit-level=high || true")}
    swallowed_ok, swallowed_failures = gate_verdict([swallowed])
    if swallowed_ok or not swallowed_failures:
        sys.exit("FAIL: an audit line whose exit is thrown away still reads as a gate to this file")
    ok_lines.append("an audit line whose exit is thrown away is no longer a gate to this file")

sys.exit(0 if report(ok_lines, failures) else 1)
PY
