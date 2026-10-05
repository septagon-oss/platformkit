"""An application booting behind another migration's composition key still serves.

The key is one for the whole database (kit/db/migrate.go), and a boot migrates before it
listens, so another package's migration holding it is a queue the boot joins. A case
that starts the application waits for /health; the wait it gives a boot has to be a wait
for the boot, not a deadline the queue in front of it can spend.
"""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "kit/db"))
from concurrent_index_reader_schedule_test import ROOT, database_url, run  # noqa: E402


CASE = "TestARefusedPersonSeesWhatIsMissingWhoCanGrantItAsksAndIsGranted"
# The competing migration's hold: longer than any fixed wait for /health the case
# could be given by a guess, and short beside the patience a queue for the key gets
# elsewhere in the tree (kit/db/migrate_test.go's migrationQueueBudget, five minutes).
HOLD_SECONDS = 45


class BootWaitsForItsCompositionKey(unittest.TestCase):
    def test_a_boot_queued_behind_another_migration_is_not_read_as_a_dead_application(self):
        admin = os.environ["PLATFORMKIT_TEST_ADMIN_URL"]
        app = os.environ["PLATFORMKIT_TEST_DATABASE_URL"]
        database = "boot_queue_" + uuid.uuid4().hex
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
                binary = str(Path(directory) / "platformkit.test")
                compiled = run(["go", "test", "-c", "-o", binary, "./apps/platformkit"])
                self.assertEqual(compiled.returncode, 0, compiled.stdout)
                with (Path(directory) / "holder.log").open("w+") as output:
                    # The competing migration joins the key's queue while the case's
                    # own install still holds it (the runner's ledger exists only inside
                    # a run), so the key passes to it next and the boot's Run, which
                    # asks after install returns, queues behind it.
                    holder = subprocess.Popen(
                        ["psql", scoped_admin, "-X", "-v", "ON_ERROR_STOP=1", "-c",
                         "DO $$ BEGIN WHILE NOT EXISTS (SELECT 1 FROM pg_class "
                         "WHERE relname = 'schema_migrations') LOOP PERFORM pg_sleep(0.01); "
                         "END LOOP; END $$",
                         "-c", "SELECT pg_advisory_lock(7240101), clock_timestamp()",
                         "-c", f"SELECT pg_sleep({HOLD_SECONDS})",
                         "-c", "SELECT pg_advisory_unlock(7240101), clock_timestamp()"],
                        cwd=ROOT, stdin=subprocess.DEVNULL,
                        stdout=output, stderr=subprocess.STDOUT,
                    )
                    result = subprocess.run(
                        [binary, "-test.run=^" + CASE + "$", "-test.v", "-test.timeout=300s"],
                        cwd=Path(ROOT) / "apps/platformkit", env=env, text=True,
                        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                        stderr=subprocess.STDOUT,
                    )
                    self.assertIn("=== RUN   " + CASE, result.stdout)
                    holder.wait(timeout=HOLD_SECONDS + 60)
                    output.seek(0)
                    self.assertEqual(holder.returncode, 0, output.read())
                self.assertEqual(
                    result.returncode, 0,
                    "A boot queued behind another migration's composition key must "
                    "not fail the case that started it:\n" + result.stdout[-4000:],
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
