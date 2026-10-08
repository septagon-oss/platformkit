"""Parallel CI jobs may start and remove only their own named containers.

The helpers below read the workflow: the name a step hands `docker run --name`, after
the runner would expand it, and the handles its cleanup step passes on. They lived in
scripts/ci_cleanup_reaches_every_named_container_test.py, which root's ruling of
2026-10-08 retired with the four-way job split whose shape it pinned; the ownership
question it asks is the split's and survives it, so the two helpers come with it.
"""

import os
from pathlib import Path
import re
import shlex
import unittest

import yaml


WORKFLOW = Path(
    os.environ.get(
        "CI_WORKFLOW_FILE",
        Path(__file__).resolve().parents[1] / ".gitea/workflows/ci.yml",
    )
)
RUN_ID = "4242"


def expand(text, env):
    """Expand ${{ github.run_id }} and $VAR / ${VAR} the way the runner and bash would."""
    text = re.sub(r"\$\{\{\s*github\.run_id\s*\}\}", RUN_ID, text)
    return re.sub(
        r"\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?",
        lambda m: env.get(m.group(1), m.group(0)),
        text,
    )


def step_env(step):
    return {k: expand(str(v), {}) for k, v in (step.get("env") or {}).items()}


def started_names(step):
    """The names a step hands `docker run --name`, one per iteration of its loop."""
    env = step_env(step)
    run = step.get("run") or ""
    names = []
    loops = dict(re.findall(r"for (\w+) in ([^;\n]+);", run))
    for expr in re.findall(r'docker run [^\n]*?--name "([^"]+)"', run):
        variables = [v for v in loops if f"${v}" in expr or f"${{{v}}}" in expr]
        if not variables:
            names.append(expand(expr, env))
            continue
        for word in loops[variables[0]].split():
            names.append(expand(expr, {**env, variables[0]: word}))
    return names


def cleanup_handles(step):
    env = step_env(step)
    run = (step.get("run") or "").replace("\\\n", " ")
    handles = []
    for line in run.splitlines():
        if "scripts/ci_cleanup.sh" not in line:
            continue
        words = shlex.split(line, comments=True)
        rest = words[words.index("scripts/ci_cleanup.sh") + 1 :]
        for word in rest:
            if word in ("||", "&&", ";"):
                break
            handles.append(expand(word, env).strip())
    return handles




class ParallelJobsOwnTheirContainers(unittest.TestCase):
    def test_parallel_jobs_never_start_or_remove_a_neighbours_container(self):
        jobs = yaml.safe_load(WORKFLOW.read_text())["jobs"]
        owners = {}
        cleanups = {}
        for job, spec in jobs.items():
            steps = spec.get("steps") or []
            names = [name for step in steps for name in started_names(step)]
            cleanups[job] = [
                handle for step in steps for handle in cleanup_handles(step)
            ]
            for name in names:
                if name in owners:
                    self.assertEqual(
                        owners[name], job,
                        f"{job} and {owners[name]} start the same container {name}",
                    )
                owners[name] = job

        self.assertTrue(owners, "No named container reaches the ownership check")
        for job, handles in cleanups.items():
            for handle in handles:
                if handle in owners:
                    self.assertEqual(
                        owners[handle], job,
                        f"{job}'s cleanup removes {handle}, owned by {owners[handle]}",
                    )


if __name__ == "__main__":
    unittest.main()
