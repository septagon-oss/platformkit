"""Each job's timeout-minutes is at least twice the slowest run that job has had.

The workflow prices every limit as 2 x the job's own measured p95. Below twenty runs
the empirical p95 of a job is its slowest run, so a limit under twice the slowest run
the forge has recorded for that job is not the formula the file states. The spans are
the forge's own (`GET /api/v1/repos/septagon-oss/platformkit/actions/runs/{run}/jobs`,
started_at to completed_at); a job with no completed run of its own has no row.
"""

import os
from pathlib import Path
import unittest

import yaml


WORKFLOW = Path(
    os.environ.get(
        "CI_WORKFLOW_FILE",
        Path(__file__).resolve().parents[1] / ".gitea/workflows/ci.yml",
    )
)

# job -> seconds of each completed run of it on the forge, run 46460 at 35fe554c.
# go-checks ran every test (`DONE 3439 tests` in its log) before its rehearsal failed.
MEASURED = {
    "go-checks": [565],
    "race-and-vuln": [563],
    "e2e": [292],
}


class JobLimitsCoverTheirMeasuredRunsTest(unittest.TestCase):
    def test_each_job_limit_is_twice_the_slowest_run_of_that_job(self):
        jobs = yaml.safe_load(WORKFLOW.read_text())["jobs"]
        for job, spans in MEASURED.items():
            with self.subTest(job=job):
                self.assertIn(job, jobs, f"{job} is missing from {WORKFLOW}")
                limit = int(jobs[job]["timeout-minutes"]) * 60
                self.assertGreaterEqual(
                    limit,
                    2 * max(spans),
                    f"{job}: timeout-minutes is {limit // 60} ({limit}s), "
                    f"under twice its slowest measured run of {max(spans)}s",
                )


if __name__ == "__main__":
    unittest.main()
