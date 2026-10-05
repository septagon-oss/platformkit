"""The cancellation case waits for its shared composition lock before testing SQL."""

import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
import uuid

from concurrent_index_reader_schedule_test import ROOT, database_url, run


CASE = "TestMigrationCancellationRollsBackAndReleasesTheLock"


class CancellationCaseWaitsForItsLock(unittest.TestCase):
    def test_a_queued_migration_is_not_mistaken_for_failed_cancellation(self):
        admin = os.environ["PLATFORMKIT_TEST_ADMIN_URL"]
        app = os.environ["PLATFORMKIT_TEST_DATABASE_URL"]
        database = "cancel_queue_" + uuid.uuid4().hex
        control = database_url(admin, "postgres")
        created = run(["psql", control, "-v", "ON_ERROR_STOP=1", "-c",
                       f"CREATE DATABASE {database}"])
        self.assertEqual(created.returncode, 0, created.stdout)
        holder = None
        try:
            scoped_admin = database_url(admin, database)
            env = {**os.environ,
                   "PLATFORMKIT_TEST_ADMIN_URL": scoped_admin,
                   "PLATFORMKIT_TEST_DATABASE_URL": database_url(app, database)}
            with tempfile.TemporaryDirectory() as directory:
                binary = str(Path(directory) / "db.test")
                compiled = run(["go", "test", "-c", "-o", binary, "./kit/db"])
                self.assertEqual(compiled.returncode, 0, compiled.stdout)
                # This is deliberate competing work, not a readiness sleep.
                # The queue is observed below before the case starts, and ends
                # well inside the five-minute queue budget its retry already uses.
                with (Path(directory) / "holder.log").open("w+") as output:
                    holder = subprocess.Popen(
                        ["psql", scoped_admin, "-X", "-v", "ON_ERROR_STOP=1", "-c",
                         "SELECT pg_advisory_lock(7240101); SELECT pg_sleep(15); "
                         "SELECT pg_advisory_unlock(7240101)"],
                        cwd=ROOT, stdin=subprocess.DEVNULL,
                        stdout=output, stderr=subprocess.STDOUT,
                    )
                    deadline = time.monotonic() + 10
                    while True:
                        state = run([
                            "psql", scoped_admin, "-XAt", "-v", "ON_ERROR_STOP=1",
                            "-c", "SELECT count(*) FROM pg_locks WHERE "
                            "locktype='advisory' AND granted AND objid=7240101 "
                            "AND database=(SELECT oid FROM pg_database "
                            "WHERE datname=current_database())",
                        ])
                        self.assertEqual(state.returncode, 0, state.stdout)
                        if state.stdout.strip() == "1":
                            break
                        self.assertLess(time.monotonic(), deadline,
                                        "The competing migration never held its lock")
                        time.sleep(0.02)
                    result = run([binary, "-test.run=^" + CASE + "$",
                                  "-test.v", "-test.timeout=60s"], env=env)
                    self.assertIn("=== RUN   " + CASE, result.stdout)
                    holder.wait(timeout=20)
                    output.seek(0)
                    self.assertEqual(holder.returncode, 0, output.read())
                self.assertEqual(
                    result.returncode, 0,
                    "A finite wait for another migration's composition lock "
                    "must not fail the cancellation case before its SQL starts:\n"
                    + result.stdout,
                )
        finally:
            if holder is not None and holder.poll() is None:
                holder.terminate()
                holder.wait(timeout=10)
            dropped = run(["psql", control, "-v", "ON_ERROR_STOP=1", "-c",
                           f"DROP DATABASE {database} WITH (FORCE)"])
            self.assertEqual(dropped.returncode, 0, dropped.stdout)


if __name__ == "__main__":
    unittest.main()
