#!/usr/bin/env python3
"""Check that each source change on this branch had its budget in place."""

import io
import json
from pathlib import PurePosixPath
import subprocess
import tarfile


def git(*args: str) -> bytes:
    return subprocess.check_output(["git", *args])


commits = git("log", "--reverse", "--format=%H", "main..HEAD", "--", "kit/httpx/fault.go").decode().splitlines()
if not commits:
    raise SystemExit(0)

parent = git("rev-parse", commits[0] + "^").decode().strip()
revisions = git("rev-list", "--first-parent", "--reverse", f"{parent}..HEAD").decode().splitlines()
failures = []
for revision in revisions:
    with tarfile.open(fileobj=io.BytesIO(git("archive", "--format=tar", revision))) as archive:
        files = {
            member.name: archive.extractfile(member).read()
            for member in archive.getmembers()
            if member.isfile()
        }

    budget = json.loads(files["loc-budget.json"])
    for bucket in budget["buckets"]:
        count = 0
        for path, data in files.items():
            directory = PurePosixPath(path).parent.name
            if bucket.get("paths") and not any(path.startswith(prefix) for prefix in bucket["paths"]):
                continue
            if any(path.startswith(prefix) for prefix in bucket.get("exclude_paths", [])):
                continue
            if not any(path.endswith(suffix) for suffix in bucket["suffixes"]):
                continue
            if any(path.endswith(suffix) for suffix in bucket.get("exclude_suffixes", [])):
                continue
            if bucket.get("dir_suffixes") and not any(directory.endswith(suffix) for suffix in bucket["dir_suffixes"]):
                continue
            if any(directory.endswith(suffix) for suffix in bucket.get("exclude_dir_suffixes", [])):
                continue
            if bucket.get("contains") and bucket["contains"].encode() not in data:
                continue
            count += data.count(b"\n")
        if count > bucket["max"]:
            failures.append(f"{revision[:7]} {bucket['name']}: {count} lines > budget {bucket['max']}")

if failures:
    raise SystemExit("source was committed before its measured budget:\n" + "\n".join(failures))
print(f"budget in place at each of {len(revisions)} source snapshots")
