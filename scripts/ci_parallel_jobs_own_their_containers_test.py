"""Parallel CI jobs may start and remove only their own named containers."""

import unittest

import yaml

from ci_cleanup_reaches_every_named_container_test import (
    WORKFLOW,
    cleanup_handles,
    started_names,
)


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
