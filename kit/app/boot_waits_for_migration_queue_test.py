"""A boot waiting for another migration is still a correct boot.

Use the existing boot case and database fixture helpers. A separate database
keeps unrelated suites out of this finite composition-lock queue.
"""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "db"))
from concurrent_index_reader_schedule_test import ROOT, database_url, run


CASE = "TestBootMigratesAndServes"
HOLD_SECONDS = 35


class BootWaitsForMigrationQueue(unittest.TestCase):
    def test_a_boot_waits_for_the_finite_queue_before_judging_the_listener(self):
        admin = os.environ["PLATFORMKIT_TEST_ADMIN_URL"]
        app = os.environ["PLATFORMKIT_TEST_DATABASE_URL"]
        database = "boot_wait_" + uuid.uuid4().hex
        control = database_url(admin, "postgres")
        created = run(["psql", control, "-X", "-v", "ON_ERROR_STOP=1", "-c",
                       f"CREATE DATABASE {database}"])
        self.assertEqual(created.returncode, 0, created.stdout)
        holder = child = None
        try:
            scoped = database_url(admin, database)
            env = {**os.environ,
                   "PLATFORMKIT_TEST_ADMIN_URL": scoped,
                   "PLATFORMKIT_TEST_DATABASE_URL": database_url(app, database)}
            with tempfile.TemporaryDirectory() as directory:
                fixture = Path(directory)
                binary = str(fixture / "app.test")
                built = run(["go", "test", "-c", "-o", binary, "./kit/app"])
                self.assertEqual(built.returncode, 0, built.stdout)

                def lock_count(granted):
                    state = run(["psql", scoped, "-XAt", "-v", "ON_ERROR_STOP=1",
                                 "-c", "SELECT count(*) FROM pg_locks WHERE "
                                 "locktype='advisory' AND objid=7240101 "
                                 f"AND granted={granted} AND database=(SELECT oid "
                                 "FROM pg_database WHERE datname=current_database())"])
                    self.assertEqual(state.returncode, 0, state.stdout)
                    return int(state.stdout.strip())

                with (fixture / "holder.log").open("w+") as held, \
                        (fixture / "boot.log").open("w+") as boot:
                    holder = subprocess.Popen(
                        ["psql", scoped, "-X", "-v", "ON_ERROR_STOP=1", "-c",
                         "SELECT pg_advisory_lock(7240101); "
                         f"SELECT pg_sleep({HOLD_SECONDS}); "
                         "SELECT pg_advisory_unlock(7240101)"],
                        stdin=subprocess.DEVNULL, stdout=held, stderr=subprocess.STDOUT,
                    )
                    deadline = time.monotonic() + 10
                    while not lock_count("true"):
                        self.assertIsNone(holder.poll(), "The lock holder exited")
                        self.assertLess(time.monotonic(), deadline,
                                        "The holder never acquired its composition key")
                        time.sleep(0.02)
                    child = subprocess.Popen(
                        [binary, f"-test.run=^{CASE}$", "-test.v", "-test.timeout=90s"],
                        cwd=ROOT / "kit/app", env=env, stdin=subprocess.DEVNULL,
                        stdout=boot, stderr=subprocess.STDOUT,
                    )
                    deadline = time.monotonic() + 15
                    queued = False
                    while child.poll() is None and time.monotonic() < deadline:
                        if lock_count("false"):
                            queued = True
                            break
                        time.sleep(0.02)
                    child.wait(timeout=100)
                    holder.wait(timeout=HOLD_SECONDS + 10)
                    held.seek(0)
                    self.assertEqual(holder.returncode, 0, held.read())
                    boot.seek(0)
                    output = boot.read()
                self.assertIn("=== RUN   " + CASE, output, output)
                self.assertEqual(
                    child.returncode, 0,
                    f"A finite {HOLD_SECONDS}s migration queue must not fail a correct "
                    f"boot (Postgres observed this boot queued: {queued}):\n" + output,
                )
        finally:
            for process in (child, holder):
                if process is not None and process.poll() is None:
                    process.terminate()
                    process.wait(timeout=10)
            dropped = run(["psql", control, "-X", "-v", "ON_ERROR_STOP=1", "-c",
                           f"DROP DATABASE {database} WITH (FORCE)"])
            self.assertEqual(dropped.returncode, 0, dropped.stdout)


if __name__ == "__main__":
    unittest.main()
