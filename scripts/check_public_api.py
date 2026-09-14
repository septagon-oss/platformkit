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
