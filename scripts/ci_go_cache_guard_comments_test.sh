#!/usr/bin/env bash
# Comments outside a cache input must not make its guard fail through SIGPIPE.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
python3 - "$root" <<'PY'
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

root = Path(sys.argv[1])

with tempfile.TemporaryDirectory(prefix="ci-go-cache-comments-") as directory:
    tree = Path(directory)
    shutil.copytree(root / "scripts", tree / "scripts")
    shutil.copytree(root / ".gitea/workflows", tree / ".gitea/workflows")
    for name in ("Makefile", "go.mod", "go.sum"):
        shutil.copyfile(root / name, tree / name)
    workflow = tree / ".gitea/workflows/ci.yml"
    original = workflow.read_text()
    # Larger than a pipe buffer, so an early reader exit cannot depend on whether
    # the writer happened to finish before the reader closed its end.
    comments = ("          # " + "cache input explanation " * 12 + "\n") * 1024

    def run_guard(label, expected, reason=""):
        result = subprocess.run(
            ["bash", "scripts/ci_go_cache_test.sh"], cwd=tree,
            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=90,
        )
        if result.returncode != expected or reason not in result.stdout:
            sys.exit(
                f"FAIL: {label}: exit {result.returncode}, expected {expected}\n"
                f"{result.stdout}{result.stderr}"
            )
        print(f"ok   {label}: exit {expected}")

    boundaries = {
        "restore keys": "            ${{ steps.gocache.outputs.prefix-os }}\n",
        "cache paths": "            ${{ steps.gocache.outputs.gocache }}\n",
    }
    for label, boundary in boundaries.items():
        if boundary not in original:
            sys.exit(f"FAIL: no {label} boundary in the workflow")
        padded = original.replace(boundary, boundary + comments, 1)
        workflow.write_text(padded)
        run_guard(f"large comments after {label} preserve a valid workflow", 0)

        # A genuine path mismatch still needs a useful refusal, not exit 141.
        marker = "      - name: Save the Go module and build cache\n"
        if marker not in padded:
            sys.exit("FAIL: no save step in the workflow")
        before, save = padded.split(marker, 1)
        pair = (
            "            ${{ steps.gocache.outputs.modcache }}\n"
            "            ${{ steps.gocache.outputs.gocache }}\n"
        )
        if pair not in save:
            sys.exit("FAIL: no two-path save input in the workflow")
        reversed_pair = "".join(reversed(pair.splitlines(keepends=True)))
        workflow.write_text(before + marker + save.replace(pair, reversed_pair, 1))
        run_guard(
            f"large comments after {label} preserve the path mismatch refusal",
            1, "save step caches a different path list",
        )
PY
