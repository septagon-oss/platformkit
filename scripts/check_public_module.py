#!/usr/bin/env python3
"""Compile the existing independent forms example against one public version."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

MODULE = "github.com/septagon-oss/platformkit"
ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="exact published Go version, including pseudo-versions")
    args = parser.parse_args()
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", args.version):
        parser.error("provide an exact version, not a branch, commit query or latest")
    directive = re.search(r"(?m)^go (\S+)$", (ROOT / "go.mod").read_text())
    if directive is None:
        raise RuntimeError("source go.mod has no Go version directive")
    go_version = directive.group(1)
    env = dict(os.environ, GOENV="off", GOWORK="off", GOTOOLCHAIN="go" + go_version)
    selected = subprocess.check_output(["go", "env", "GOROOT"], env=env, text=True).strip()
    go = str(Path(selected) / "bin/go")
    with tempfile.TemporaryDirectory(prefix="platformkit-public-module-") as directory:
        temporary = Path(directory)
        consumer = temporary / "consumer"
        consumer.mkdir()
        env.update(
            PATH=str(Path(selected) / "bin") + os.pathsep + env.get("PATH", ""),
            GOTOOLCHAIN="local", GOPROXY="https://proxy.golang.org", GOSUMDB="sum.golang.org",
            GOPRIVATE="none", GONOPROXY="none", GONOSUMDB="none", GOTELEMETRY="off",
            GOMODCACHE=str(temporary / "modules"), GOCACHE=str(temporary / "build"),
            GOFLAGS="-p=2 -modcacherw -buildvcs=false",
        )
        evidence = {"version": args.version, "commands": [], "consumer_sha256": {}}

        def run(*arguments):
            command = [go, *arguments]
            print("+ " + " ".join(arguments), file=sys.stderr, flush=True)
            result = subprocess.run(command, cwd=consumer, env=env, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=300)
            evidence["commands"].append({"arguments": list(arguments), "exit_code": result.returncode})
            if result.returncode:
                raise RuntimeError(result.stdout + result.stderr)
            return result.stdout

        (consumer / "go.mod").write_text(
            f"module example.test/platformkit-parts\n\ngo {go_version}\n\nrequire {MODULE} {args.version}\n")
        for name in ("main.go", "main_test.go"):
            data = (ROOT / "ui/forms/testdata/standalone" / name).read_bytes()
            (consumer / name).write_bytes(data)
            evidence["consumer_sha256"][name] = hashlib.sha256(data).hexdigest()
        downloaded = json.loads(run("mod", "download", "-json", MODULE + "@" + args.version))
        if downloaded.get("Path") != MODULE or downloaded.get("Version") != args.version:
            raise RuntimeError("the public proxy selected a different module version")
        if not downloaded.get("Sum") or not downloaded.get("GoModSum"):
            raise RuntimeError("module checksum evidence is missing")
        evidence["download"] = {key: downloaded[key] for key in ("Path", "Version", "Sum", "GoModSum", "Origin") if key in downloaded}
        evidence["go_version"] = run("version").strip()
        evidence["tests"] = run("test", "-mod=mod", "-count=1", "-v", "./...")
        run("vet", "./...")
        run("build", "-o", str(temporary / "example"), ".")
        selected_module = json.loads(run("list", "-m", "-json", MODULE))
        declaration = json.loads(run("mod", "edit", "-json"))
        if selected_module.get("Version") != args.version or selected_module.get("Replace") or declaration.get("Replace"):
            raise RuntimeError("consumer dependency selection changed or used a replacement")
        closure = run("list", "-deps", "-f", "{{.ImportPath}}", "./...").splitlines()
        evidence["runtime_dependencies"] = [name for name in closure if name]
        # This example must stay outside the application/SQL/HTTP-framework composition.
        forbidden = ("/kit/db", "/kit/crud", "/kit/httpx", "/kit/rest", "/kit/module", "/kit/app", "/ui/page",
                     "gorm.io/", "github.com/nats-io/", "github.com/jackc/", "github.com/danielgtaylor/")
        if "database/sql" in closure or any(fragment in name for name in closure for fragment in forbidden):
            raise RuntimeError("the standalone example acquired application dependencies")
        evidence["go_mod"] = (consumer / "go.mod").read_text()
        evidence["go_sum"] = (consumer / "go.sum").read_text()
        evidence["result"] = "public import, example tests and compilation passed; no server started"
    evidence["temporary_fixture_removed"] = True
    print(json.dumps(evidence, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
