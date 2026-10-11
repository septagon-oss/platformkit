#!/usr/bin/env bash
# A container this program's CI starts by hand is named, labelled, pre-removed, swept and dropped.
#
# 2026-10-06: the three kernel CI runners held 13 SeaweedFS containers between them, some 19 hours
# old, each from a job that had ended long before. The job's own ceiling is 75 minutes. They held
# memory, they kept the runners from ever reading idle, and one runner could not be restarted for
# three hours for that reason. The step that started them, `id: s3` in .gitea/workflows/ci.yml, ran
# `docker run -d` with no `--name`, so nothing after that step could point at the container again:
# the only handle was the id the step wrote to $GITHUB_OUTPUT, and a job the runner cancels or cuts
# off at its ceiling never reaches the `if: always()` cleanup that would have read it — and on that
# file as written, not even a green job read it, because the cleanup named five containers and the
# object store was not among them.
#
# Four properties keep a container from leaking again, and one bound keeps the cure from becoming a
# hazard. Each is a rule over every job of every workflow under .gitea/workflows — the ones that run:
#
#   1. named       — `--name "$VAR"` with VAR in the step's env and its value carrying
#                    github.run_id. A name the run id supplies is a handle every later step can
#                    rebuild without any step output; the id the step prints is not.
#   2. pre-removed — `docker rm -f "$VAR" … || true` earlier in the same step, before the start, so
#                    a re-run of the same run id clears its own previous attempt instead of failing
#                    on a name that is already taken.
#   3. labelled    — `--label pkit.ci.run=${{ github.run_id }}`, which is what makes a container
#                    findable by whoever has to clean up after a job that cannot speak for itself.
#   4. swept       — the first step of every job that starts a container removes labelled containers
#                    older than a bound, with `docker ps -aq --filter label=pkit.ci.run`. `-a`, not
#                    `-q`: the running-only filter does not see a stopped container, and a SeaweedFS
#                    whose process died is stopped, still on disk, still holding what it wrote.
#                    Measured on Docker 29.8.2 with two labelled containers, one created and never
#                    started and one started and left to exit: once both had stopped,
#                    `docker ps -q --filter label=pkit.ci.run` listed neither and
#                    `docker ps -aq --filter label=pkit.ci.run` listed both.
#   5. bounded     — that bound is at least twice the job's own `timeout-minutes`, so a container a
#                    live job owns is never in range. The label is a namespace the whole program
#                    shares, so a sweep reaches every container on that daemon carrying it, another
#                    repository's CI included; the bound is what makes that safe, and it is derived
#                    from the ceiling stated in the same file rather than trusted to a comment.
#   6. dropped     — the job's `if: always()` step names every container the job can create, by a
#                    handle of its own. Rule 4 bounds what a killed job leaves behind; this is what
#                    makes a finished job leave nothing at all.
#
# A rule applies to a *detached* start, and to every one of them in a step, not the first:
# `docker run busybox true` exits with the step that waited for it, so it cannot be what a killed job
# leaves behind, while a step that starts two containers leaks whichever of the two nobody looked at.
# Detached is `-d`, `--detach` or a short cluster that carries the `d` (`-dit`), and the command may be
# spelled `docker run` or `docker container run`, so the read joins a command's `\`-continuation lines
# and searches every one, rather than reading the layout of the file.
#
# What this file does not see, and does not claim: that the sweep ran on a runner that had work to do
# (no job is started here); the age comparison inside the sweep, which scripts/ci_container_sweep_age_test.sh
# runs against a stub docker instead of reading; the images a job builds, which `docker ps` does not
# list and which only the always() step removes; `services:` containers, which belong to act_runner and
# carry no label of ours; and `.github/workflows/*`, which ARCHITECTURE.md records as copies of these
# steps onto a runner destroyed with the job — a container started there dies with it, so the four rules
# above are rules over the persistent Gitea runners, and this file reads those files. If a GitHub
# workflow ever moves to a self-hosted runner, it starts leaking the moment it does.
#
# Usage: ci_container_leak_test.sh [workflows-dir]   (default: this repository's .gitea/workflows)
# With no argument the cases below run against mutated copies of the real workflows too, so that "the
# guard fails when a step reintroduces the leak" is a line of `make check` and not a hope.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
workflows="${1:-$root/.gitea/workflows}"

check_all() { # one parsed pass over every workflow in $1
	python3 - "$1" <<'PY'
import glob, os, re, sys, yaml

folder = sys.argv[1]
failures = 0

def fail(msg):
    global failures
    print(f"FAIL: {msg}")
    failures += 1

RUN = re.compile(r"\bdocker\s+(?:container\s+)?run\b")
LABEL = re.compile(r"--label\s+\"?pkit\.ci\.run=([^\n]*)")
# The label's value, stated exactly: `${{ github.run_id }}` is the only thing that makes a container
# findable by the run that owns it, so a label pointed at some other value, or a trailing comment that
# mentions the run id, is not rule 3. The spaces inside the braces are the expression language's own.
LABEL_VALUE = re.compile(r"^\$\{\{\s*github\.run_id\s*\}\}$")
NAME = re.compile(r'--name\s+"\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?([^"]*)"')
LOOP = re.compile(r"\bfor\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\s+([^;]+?)\s*(?:;\s*do|\s+do\b)")
SHORT_CLUSTER = re.compile(r"^-([A-Za-z]*d[A-Za-z]*)$")

def detached(options):
    """Whether the options of one `docker run` ask for a start that outlives its step.

    `-d`, `--detach` (or `--detach=true`) and any short cluster carrying a `d` (`-dit`, `-di`) all
    return control to the step while the container keeps running, which is what makes the container a
    thing a killed job can leave behind. `--detach=false` says the opposite and is refused back.
    """
    for token in options.split():
        if token in ("--detach", "--detach=true"):
            return True
        if token == "--detach=false":
            return False
        if SHORT_CLUSTER.match(token):
            return True
    return False

def commands(run):
    """(index of the line the command starts on, the command's whole text), `\\`-continuations joined.

    A flag moved onto the next continuation line is the same flag to `docker`, so reading physical
    lines would refuse a valid reformat of a step that satisfies every rule. Whole-line comments are
    dropped: they are prose about a start, not one.
    """
    joined, text, start = [], "", 0
    for index, line in enumerate(run.splitlines()):
        if line.lstrip().startswith("#"):
            continue
        if line.endswith("\\"):
            if not text:
                start = index
            text += line[:-1] + " "
            continue
        joined.append((start, text + line))
        text = ""
    if text:
        joined.append((start, text))
    return joined

def starts(steps):
    """One entry per detached container start of any step: where it is, what it runs, what it is named.

    Every start of the step, not its first: a step that starts a second container would otherwise
    leave that second one unnamed, unlabelled and uncounted while the step's verdict read as though
    both had been looked at.
    """
    found = []
    for i, step in enumerate(steps):
        raw = str(step.get("run") or "")
        for line, text in commands(raw):
            marks = list(RUN.finditer(text))
            for position, mark in enumerate(marks):
                stop = marks[position + 1].start() if position + 1 < len(marks) else len(text)
                if not detached(text[mark.end():stop]):
                    continue
                found.append({"step": i, "name": step.get("name") or f"step {i}",
                              "env": step.get("env") or {}, "raw": raw, "line": line,
                              "before": text[:mark.start()], "text": text})
    return found

def site(start):
    """(env var, name suffix) of the container a start names, or (None, None)."""
    match = NAME.search(start["text"])
    if not match:
        return None, None
    return match.group(1), match.group(2)

for path in sorted(glob.glob(os.path.join(folder, "*.yml"))):
    name = os.path.basename(path)
    jobs = (yaml.safe_load(open(path)) or {}).get("jobs") or {}
    for job, body in jobs.items():
        steps = body.get("steps") or []
        sites = starts(steps)
        if not sites:
            continue
        ceiling = body.get("timeout-minutes")
        if not isinstance(ceiling, int):
            fail(f"{name} job {job} starts containers with no timeout-minutes, so nothing bounds the age its sweep may remove")
            continue
        named = 0
        for start in sites:
            label = start["name"]
            env = start["env"]
            line = start["text"]
            physical = start["raw"].splitlines()
            var, tail = site(start)
            if var is None:
                fail(f"{name} job {job} step {label!r} starts a container with no --name: the id it prints is the only handle, and a job cut off before it writes $GITHUB_OUTPUT loses it")
                continue
            if var not in env:
                fail(f"{name} job {job} step {label!r} names its container after ${var}, which its env: defines no value for")
                continue
            base = str(env[var])
            if "github.run_id" not in base:
                fail(f"{name} job {job} step {label!r} names its container {base!r}, which does not carry github.run_id, so two runs compete for one name")
                continue
            cleared = re.compile(rf'docker\s+rm\s+-f\s+"?\$\{{?{var}\}}?')
            if not (cleared.search(start["before"]) or any(cleared.search(text) for text in physical[:start["line"]])):
                fail(f"{name} job {job} step {label!r} starts {base}{tail} with no `docker rm -f \"${var}\"` before the run, so re-running a run id fails on a name its own previous attempt still holds")
                continue
            labelled = LABEL.search(line)
            # The value runs to the next flag: `${{ github.run_id }}` carries a space, so a
            # `\S+` capture would stop at `${{` and read a label that is there as one that is not.
            value = re.split(r"\s+--", labelled.group(1))[0].strip().rstrip("\\").strip().strip('"\'') if labelled else ""
            if not labelled or not LABEL_VALUE.match(value):
                fail(f"{name} job {job} step {label!r} starts {base}{tail} with no --label pkit.ci.run=${{{{ github.run_id }}}}, so no sweep can find it once this job is gone")
                continue
            named += 1
        first = steps[0]
        sweep = str(first.get("run") or "")
        if not re.search(r"docker\s+ps\s+-aq\s+--filter\s+label=pkit\.ci\.run", sweep):
            fail(f"{name} job {job} starts {len(sites)} container(s) and its first step does not sweep `docker ps -aq --filter label=pkit.ci.run`: a job the runner kills never reaches its own cleanup, so somebody else's job has to remove them")
        elif "docker rm -f" not in sweep:
            fail(f"{name} job {job}'s first step lists labelled containers and never removes one")
        else:
            older = (first.get("env") or {}).get("SWEEP_OLDER_THAN")
            if older is None or not str(older).isdigit():
                fail(f"{name} job {job}'s sweep step carries no numeric SWEEP_OLDER_THAN: nothing says how old a labelled container has to be before this job may remove it")
            elif int(older) < 2 * 60 * ceiling:
                fail(f"{name} job {job}'s sweep removes containers older than {older}s, which is under twice the job's own {ceiling}-minute ceiling: a container a live job owns is in range")
        cleanups = [s for s in steps if "always()" in str(s.get("if") or "") and "docker rm -f" in str(s.get("run") or "")]
        if not cleanups:
            fail(f"{name} job {job} starts {len(sites)} container(s) and has no always() step that removes one")
            continue
        cleanup = cleanups[-1]
        cleanup_run = str(cleanup.get("run") or "")
        cleanup_env = {k: str(v) for k, v in (cleanup.get("env") or {}).items()}
        missing = []
        for start in sites:
            var, tail = site(start)
            base = str(start["env"].get(var) or "") if var else ""
            if not base:
                continue
            suffixes = [tail]
            for loop_var, words in LOOP.findall(start["raw"]):
                if f"${loop_var}" in tail or f"${{{loop_var}}}" in tail:
                    suffixes = [tail.replace(f"${{{loop_var}}}", w).replace(f"${loop_var}", w) for w in words.split()]
            key = next((k for k, value in cleanup_env.items() if value == base), None)
            if key is None:
                missing.append(f"{base}{tail}")
                continue
            for suffix in suffixes:
                if f'"${key}{suffix}"' not in cleanup_run:
                    missing.append(f"{base}{suffix}")
        if missing:
            fail(f"{name} job {job}'s always() step names no handle for {', '.join(sorted(set(missing)))}: the job leaves those containers behind even when it finishes green")
        if failures == 0:
            print(f"ok   {name} job {job}: {named} hand-started container(s), each named by github.run_id, pre-removed, labelled pkit.ci.run, swept by the job's first step, and named in its always() step")
sys.exit(1 if failures else 0)
PY
}

check_all "$workflows"
if [ "$#" -gt 0 ]; then
	# A caller that named the directory is the case pass below: its verdict is the thing under test.
	exit 0
fi

python3 - "$root" "$0" <<'PY'
import subprocess
import sys
import tempfile
from pathlib import Path

root, guard = Path(sys.argv[1]), Path(sys.argv[2])
original = {p.name: p.read_text() for p in (root / ".gitea/workflows").glob("*.yml")}
s3_run = 'docker run -d --name "$CONTAINER_NAME" --label pkit.ci.run=${{ github.run_id }} --network "$JOB_NETWORK" --network-alias s3'
s3_start = f'          container=$({s3_run} \\\n'
s3_end = ('            chrislusf/seaweedfs@sha256:4e61d15fd35994cb1e43e1e553dff106794841fd9a99ade2fc8c8bfce4d7872d server -s3 -dir=/data)\n'
          '          echo "container=$container" >> "$GITHUB_OUTPUT"\n')

def run_guard(folder):
    result = subprocess.run(
        ["bash", str(guard), str(folder)], cwd=root, stdin=subprocess.DEVNULL,
        capture_output=True, text=True, timeout=120,
    )
    return result

def mutated(workflow, needle, replacement, folder):
    if needle not in original[workflow]:
        return f"the case finds no {needle!r} in {workflow} to change: the rule it tests is no longer pinned in that file"
    for name, text in original.items():
        (folder / name).write_text(text.replace(needle, replacement, 1) if name == workflow else text)
    return None

# Every case is the leak this change fixes, or one edit away from it. Each must be refused, and by a
# sentence that names it: a mutant that passes is a rule that stopped being pinned.
CASES = [
    ("an object store with no label", "ci.yml", s3_run,
     s3_run.replace(" --label pkit.ci.run=${{ github.run_id }}", "")),
    ("an object store whose label names no run id", "ci.yml", s3_run,
     s3_run.replace("--label pkit.ci.run=${{ github.run_id }}", '--label pkit.ci.run=x # github.run_id')),
    ("an object store with no name", "ci.yml", s3_run,
     s3_run.replace('--name "$CONTAINER_NAME" ', "")),
    ("an object store that does not clear its own previous attempt", "ci.yml",
     '          docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true\n' + s3_start,
     s3_start),
    ("a container name no step's env defines", "ci.yml", s3_run,
     s3_run.replace('"$CONTAINER_NAME"', '"$OTHER_NAME"')),
    ("a second container started in the object store's step, unnamed", "ci.yml", s3_end,
     s3_end + '          docker run -d busybox sleep 99999\n'),
    ("a second container started detached by --detach, unnamed", "ci.yml", s3_end,
     s3_end + '          docker run --detach busybox sleep 99999\n'),
    ("a second container started by `docker container run -d`, unnamed", "ci.yml", s3_end,
     s3_end + '          docker container run -d busybox sleep 99999\n'),
    ("a second container started detached by a combined flag cluster", "ci.yml", s3_end,
     s3_end + '          docker run -dit busybox sleep 99999\n'),
    ("a sweep bound under twice the job's own ceiling", "ci.yml",
     "          SWEEP_OLDER_THAN: '10800'", "          SWEEP_OLDER_THAN: '1800'"),
    ("a sweep narrowed to running containers", "ci.yml",
     "for id in $(docker ps -aq --filter label=pkit.ci.run); do",
     "for id in $(docker ps -q --filter label=pkit.ci.run); do"),
    ("a sweep no longer the job's first step", "ci.yml",
     "      - name: Remove containers a killed job left on this runner",
     "      - name: A step that starts no container\n        run: true\n      - name: Remove containers a killed job left on this runner"),
    # The locator is the object store's own line, not that line together with the one after it: the
    # editor's steps stand in a job of their own, so `EDITOR_NAME` belongs to that job's `always()`
    # step and `S3_NAME` to `check`'s, and the two are no longer neighbours in this file. The mutant
    # is the same leak in the same step — a job's `always()` step left without the handle for a
    # container it can start — and it is still refused by the same sentence about the object store.
    ("an always() step told about no object store", "ci.yml",
     "          S3_NAME: platformkit-${{ github.run_id }}-s3\n", ""),
    ("one preview profile the always() step does not name", "ci.yml",
     'drop "$COMPLETE_PREVIEW_CONTAINER" "$PREVIEW_NAME-complete"',
     'drop "$COMPLETE_PREVIEW_CONTAINER" "$PREVIEW_NAME"'),
    ("a device journey whose NATS is unnamed again", "mobile.yml",
     'docker run -d --name "$CONTAINER_NAME" --label pkit.ci.run=${{ github.run_id }} --network "$JOB_NETWORK" --network-alias nats',
     'docker run -d --network "$JOB_NETWORK" --network-alias nats'),
]

# And the shapes that satisfy every rule in a spelling the guard has to follow: a start moved apart
# onto its continuation lines, a label in its quotes, `--detach` spelled out. Refusing one of these is
# the guard failing a step that leaks nothing, which is how a guard gets turned off.
ACCEPTED = [
    ("the label on its own continuation line", "ci.yml", s3_run,
     'docker run -d --name "$CONTAINER_NAME" \\\n            --label pkit.ci.run=${{ github.run_id }} \\\n            --network "$JOB_NETWORK" --network-alias s3'),
    ("the label in its quotes", "ci.yml", s3_run,
     s3_run.replace("--label pkit.ci.run=${{ github.run_id }}", '--label "pkit.ci.run=${{ github.run_id }}"')),
    ("the start detached by --detach spelled out", "ci.yml", s3_run,
     s3_run.replace("docker run -d", "docker run --detach")),
    ("the start spelled `docker container run -d`", "ci.yml", s3_run,
     s3_run.replace("docker run -d", "docker container run -d")),
    ("a shell comment inside a step that mentions a detached start", "ci.yml",
     '          echo "container=$container" >> "$GITHUB_OUTPUT"\n',
     '          echo "container=$container" >> "$GITHUB_OUTPUT"\n'
     '          # an earlier draft started a second container here: docker run -d busybox sleep 99999,\n'),
]

for label, workflow, needle, replacement in CASES:
    with tempfile.TemporaryDirectory(prefix="ci-container-leak-") as directory:
        folder = Path(directory)
        problem = mutated(workflow, needle, replacement, folder)
        if problem:
            print(f"FAIL: {problem}")
            sys.exit(1)
        result = run_guard(folder)
    if result.returncode == 0:
        print(f"FAIL: the guard accepted {label}")
        print(result.stdout)
        sys.exit(1)
    verdict = next((line for line in result.stdout.splitlines() if line.startswith("FAIL:")),
                   f"(no FAIL line, exit {result.returncode}: {result.stderr})")
    print(f"ok   {label}: refused — {verdict}")

for label, workflow, needle, replacement in ACCEPTED:
    with tempfile.TemporaryDirectory(prefix="ci-container-leak-") as directory:
        folder = Path(directory)
        problem = mutated(workflow, needle, replacement, folder)
        if problem:
            print(f"FAIL: {problem}")
            sys.exit(1)
        result = run_guard(folder)
    if result.returncode != 0:
        print(f"FAIL: the guard refused {label}, which satisfies every rule:")
        print(result.stdout)
        sys.exit(1)
    verdict = next((line for line in result.stdout.splitlines() if line.startswith("ok  ")), "(no ok line)")
    print(f"ok   {label}: accepted — {verdict}")
PY
