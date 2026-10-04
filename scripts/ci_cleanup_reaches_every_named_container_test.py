"""Every container a CI job names is a handle that job's always() cleanup removes.

The id a step writes to $GITHUB_OUTPUT is lost when the job's deadline cuts that step
off, so the name is the handle that survives (main's 150de77, run 245). A container
started under a name the cleanup step does not pass is one nothing removes.
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
    if "scripts/ci_setup.sh" in run and "nats" in run.split():
        names.append(env.get("NATS_CONTAINER_NAME", "<unnamed broker>"))
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


class CleanupReachesEveryNamedContainerTest(unittest.TestCase):
    def test_every_container_a_job_names_is_a_handle_its_cleanup_removes(self):
        jobs = yaml.safe_load(WORKFLOW.read_text())["jobs"]
        named_somewhere = False
        for job, spec in jobs.items():
            steps = spec.get("steps") or []
            names = [n for s in steps for n in started_names(s)]
            if not names:
                continue
            named_somewhere = True
            with self.subTest(job=job):
                cleanups = [s for s in steps if "scripts/ci_cleanup.sh" in (s.get("run") or "")]
                self.assertTrue(cleanups, f"{job} starts {names} and has no cleanup step")
                self.assertEqual(
                    str(cleanups[-1].get("if", "")).strip(),
                    "always()",
                    f"{job}'s cleanup does not run when an earlier step failed",
                )
                handles = cleanup_handles(cleanups[-1])
                for name in names:
                    self.assertIn(
                        name,
                        handles,
                        f"{job} starts a container named {name} its cleanup never removes",
                    )
        self.assertTrue(named_somewhere, f"no job of {WORKFLOW} names a container")


if __name__ == "__main__":
    unittest.main()
