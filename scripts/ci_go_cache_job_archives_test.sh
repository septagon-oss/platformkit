#!/usr/bin/env bash
# Execute the workflow's naming commands: each Go job owns a distinct archive,
# and a dependency change restores that job's previous archive before its peers.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "$root" <<'PY'
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

import yaml

root = Path(sys.argv[1])
jobs = yaml.safe_load((root / ".gitea/workflows/ci.yml").read_text())["jobs"]


def require(condition, message):
    if not condition:
        sys.exit(f"FAIL: {message}")


def resolve(expression, outputs):
    def replace(match):
        name = match.group(1)
        require(name in outputs, f"workflow reads missing output {name}")
        return outputs[name]

    value = re.sub(r"\$\{\{\s*steps\.gocache\.outputs\.([\w-]+)\s*\}\}", replace, expression)
    require("${{" not in value, f"unresolved cache expression {value}")
    return value.strip()


with tempfile.TemporaryDirectory(prefix="ci-go-job-archives-") as directory:
    scratch = Path(directory)
    (scratch / "scripts").mkdir()
    for name in ("go.mod", "go.sum", "scripts/ci_go_cache_key.sh"):
        shutil.copyfile(root / name, scratch / name)

    def name_cache(job):
        producer = next(step for step in jobs[job]["steps"] if step.get("id") == "gocache")
        output = scratch / "outputs"
        output.write_text("")
        result = subprocess.run(
            ["bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", producer["run"]],
            cwd=scratch, env=dict(os.environ, GITHUB_OUTPUT=str(output)),
            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60,
        )
        require(result.returncode == 0, f"{job} naming failed: {result.stdout}{result.stderr}")
        outputs = dict(line.split("=", 1) for line in output.read_text().splitlines())
        require(bool(outputs.get("key")), f"{job} did not emit a cache key")
        return outputs

    before = {job: name_cache(job) for job in ("check", "design")}
    require(before["check"]["key"] != before["design"]["key"],
            "check and design emit the same exact key; one job would own both archives")
    require(before["check"]["prefix-version"] == before["design"]["prefix-version"],
            "the jobs cannot warm each other on the same toolchain")

    for job, outputs in before.items():
        require(name_cache(job) == outputs, f"{job}'s unchanged tree does not name the same archive")

    with (scratch / "go.sum").open("a") as f:
        f.write("\n// dependency change\n")

    after = {job: name_cache(job) for job in before}
    for job, outputs in after.items():
        steps = jobs[job]["steps"]
        restore = next(step for step in steps if str(step.get("uses", "")).startswith("actions/cache/restore@"))
        save = next(step for step in steps if str(step.get("uses", "")).startswith("actions/cache/save@"))
        key = resolve(save["with"]["key"], outputs)
        require(key == resolve(restore["with"]["key"], outputs), f"{job} saves a key it cannot restore")
        require(key != before[job]["key"], f"{job}'s key survives a dependency change")
        require(resolve(save["with"]["path"], outputs).splitlines() ==
                resolve(restore["with"]["path"], outputs).splitlines(),
                f"{job} restore/save paths differ in content or order")
        prefixes = resolve(restore["with"]["restore-keys"], outputs).splitlines()
        require(prefixes[0] == before[job]["prefix-job"], f"{job} does not restore its own lineage first")
        require(before[job]["key"].startswith(prefixes[0]), f"{job} cannot restore its previous dependency key")
        peer = "design" if job == "check" else "check"
        require(not before[peer]["key"].startswith(prefixes[0]), f"{job}'s first prefix also matches {peer}")
        require(prefixes[1:] == [outputs["prefix-version"], outputs["prefix-os"]],
                f"{job} lost its shared toolchain/platform fallback")
        print(f"ok   {job} repeats its own exact key, then warms its own lineage after a dependency change")

    require(after["check"]["key"] != after["design"]["key"], "dependency change gives both jobs one archive")
    print("ok   workflow commands emit distinct job archives with matching restore/save paths")
PY
