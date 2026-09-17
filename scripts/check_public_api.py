#!/usr/bin/env python3
"""Report exported API incompatibilities between two committed source revisions."""
import argparse
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent.parent
TOOL = "golang.org/x/exp/cmd/apidiff@v0.0.0-20260908205506-85c1c2202aba"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base", help="supported stable tag or reviewed commit")
    parser.add_argument("candidate", nargs="?", default="HEAD", help="committed candidate; excludes working-tree edits")
    parser.add_argument("--baseline", type=Path, help="reviewed JSON list of known incompatibilities; exit on new or vanished entries rather than on the whole report, so a documented breaking line does not hide an accidental one")
    parser.add_argument("--write-baseline", action="store_true", help="write the measured report to --baseline instead of comparing; the resulting diff is the review, so run it deliberately and commit it alone")
    args = parser.parse_args()
    directive = re.search(r"(?m)^go (\S+)$", (ROOT / "go.mod").read_text())
    if directive is None:
        raise RuntimeError("source go.mod has no Go version directive")
    go_version = directive.group(1)
    env = dict(os.environ, GOENV="off", GOWORK="off", GOTOOLCHAIN="go" + go_version)
    selected = subprocess.check_output(["go", "env", "GOROOT"], env=env, text=True).strip()
    go = str(Path(selected) / "bin/go")
    env.update(
        PATH=str(Path(selected) / "bin") + os.pathsep + env.get("PATH", ""),
        GOTOOLCHAIN="local", GOFLAGS="-p=4 -buildvcs=false",
    )
    evidence = {"tool": TOOL, "go_version": go_version, "commits": {}, "commands": []}

    def run(command, cwd):
        result = subprocess.run(command, cwd=cwd, env=env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=300)
        evidence["commands"].append({"arguments": command, "exit_code": result.returncode})
        if result.returncode:
            raise RuntimeError(result.stdout + result.stderr)
        return result.stdout

    with tempfile.TemporaryDirectory(prefix="platformkit-api-check-") as directory:
        temporary = Path(directory)
        for label, revision in (("base", args.base), ("candidate", args.candidate)):
            commit = run(["git", "rev-parse", "--verify", "--end-of-options", revision + "^{commit}"], ROOT).strip()
            evidence["commits"][label] = commit
            source = temporary / label
            source.mkdir()
            data = subprocess.check_output(["git", "archive", commit], cwd=ROOT)
            with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
                archive.extractall(source, filter="data")
            declaration = json.loads(run([go, "mod", "edit", "-json"], source))
            if declaration.get("Replace"):
                raise RuntimeError("release API snapshots must not use local or module replacements")
            print("+ export " + label + " " + commit, file=sys.stderr, flush=True)
            run([go, "run", TOOL, "-m", "-w", str(temporary / (label + ".export")), declaration["Module"]["Path"]], source)
        output = run([go, "run", TOOL, "-m", "-incompatible", str(temporary / "base.export"), str(temporary / "candidate.export")], temporary)
        report = "\n".join(line for line in output.splitlines() if not line.startswith("Ignoring internal package ")).strip()
        evidence["incompatibilities"] = report
    evidence["temporary_fixture_removed"] = True
    reported = [line[2:] if line.startswith("- ") else line for line in report.splitlines() if line]
    if args.write_baseline:
        if not args.baseline:
            raise RuntimeError("--write-baseline needs the --baseline path it is writing")
        evidence["result"] = f"baseline written with {len(reported)} incompatibilities"
        evidence["incompatibilities"] = "\n".join("- " + line for line in reported)
        print(json.dumps(evidence, indent=2))
        args.baseline.write_text(json.dumps({
            "note": "Exported API changes already accepted on this release line, one per line, in the form "
                    "apidiff reports them. This is not a licence: an entry is a change somebody reviewed and "
                    "recorded, the gate fails on any line not listed here and on any listed line that stops "
                    "being reported, and the whole measured report stays in the evidence JSON. Delete this file "
                    "when the next stable line is published, and re-measure against that tag instead.",
            "commits": evidence["commits"],
            "incompatibilities": sorted(reported),
        }, indent=2) + "\n")
        return 0
    if args.baseline:
        reviewed = json.loads(args.baseline.read_text())
        known = [line[2:] if line.startswith("- ") else line for line in reviewed["incompatibilities"] if line]
        # Two directions, both of them a review request. A line the baseline does not
        # name is an incompatibility nobody agreed to. A line the baseline names and
        # the report no longer produces means the baseline is measuring something that
        # changed underneath it — the same rule check_budget_ratchet.sh applies to a
        # removed bucket, because a baseline that cannot go stale is a list of excuses.
        new = sorted(set(reported) - set(known))
        stale = sorted(set(known) - set(reported))
        shown = args.baseline.relative_to(ROOT) if args.baseline.is_absolute() and args.baseline.is_relative_to(ROOT) else args.baseline
        evidence["baseline"] = {"path": str(shown), "recorded": len(known), "new": new, "stale": stale,
                               "recorded_for": reviewed.get("commits", {})}
        evidence["incompatibilities"] = "\n".join("- " + line for line in reported)
        evidence["result"] = ("new incompatibilities require review" if new or stale
                              else f"no incompatibility beyond the {len(known)} recorded for this line")
        evidence["limits"] = ("exported type compatibility on this Go platform; excludes runtime, wire, "
                              "database and deployed-consumer behavior; the whole report stays in this JSON "
                              "beside the delta, which is why this gates without hiding anything")
        print(json.dumps(evidence, indent=2))
        for line in new:
            print("NEW  " + line, file=sys.stderr)
        for line in stale:
            print("GONE " + line, file=sys.stderr)
        return 1 if new or stale else 0
    evidence["result"] = "review required" if report else "no incompatibilities reported"
    evidence["limits"] = "exported type compatibility on this Go platform; excludes runtime, wire, database and deployed-consumer behavior"
    print(json.dumps(evidence, indent=2))
    return 1 if report else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, tarfile.TarError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(2)
