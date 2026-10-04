"""The concurrent-index regression case tolerates a descheduled test process.

Run with PLATFORMKIT_TEST_ADMIN_URL and PLATFORMKIT_TEST_DATABASE_URL, as for
Go tests. Requires go, gdb and psql. No Go source is modified: gdb pauses the
existing case just before it starts timing the migration. Its real PostgreSQL
reader continues to run during that pause.
"""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from urllib.parse import urlsplit, urlunsplit
import uuid


ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "kit/db/migrate_allow_leg_concurrent_index_test.go"
CASE = "TestTheRuleTablesAllowLegAppliesItsConcurrentIndexWhileTheDatabaseHasAReader"


def database_url(url, database):
    parts = urlsplit(url)
    return urlunsplit(parts._replace(path="/" + database))


def run(args, **kwargs):
    return subprocess.run(
        args, cwd=ROOT, stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, timeout=180, **kwargs,
    )


class ConcurrentIndexReaderSchedule(unittest.TestCase):
    def test_reader_wait_survives_a_descheduled_caller(self):
        admin = os.environ["PLATFORMKIT_TEST_ADMIN_URL"]
        app = os.environ["PLATFORMKIT_TEST_DATABASE_URL"]
        database = "index_wait_" + uuid.uuid4().hex
        control = database_url(admin, "postgres")
        # A database of its own prevents another package's composition-lock
        # queue from accidentally satisfying the test's elapsed-time minimum.
        created = run(["psql", control, "-v", "ON_ERROR_STOP=1", "-c",
                       f"CREATE DATABASE {database}"])
        self.assertEqual(created.returncode, 0, created.stdout)
        try:
            env = {**os.environ,
                   "PLATFORMKIT_TEST_ADMIN_URL": database_url(admin, database),
                   "PLATFORMKIT_TEST_DATABASE_URL": database_url(app, database)}
            with tempfile.TemporaryDirectory() as directory:
                binary = str(Path(directory) / "db.test")
                compiled = run(["go", "test", "-c", "-o", binary, "./kit/db"])
                self.assertEqual(compiled.returncode, 0, compiled.stdout)
                # Reuse the existing dbtest-backed case without a second copy
                # of its migrations, reader, or index-validity assertions.
                lines = SOURCE.read_text().splitlines()
                start = next(i + 1 for i, line in enumerate(lines)
                             if "started := time.Now()" in line)
                result = run([
                    "gdb", "-q", "-batch",
                    "-iex", "set auto-load safe-path /",
                    "-ex", "set debuginfod enabled off",
                    "-ex", "handle SIGURG nostop noprint pass",
                    "-ex", f"break {SOURCE}:{start}",
                    "-ex", f"run -test.run=^{CASE}$ -test.v -test.timeout=90s",
                    # This is the adverse schedule, not a readiness wait.
                    "-ex", "shell sleep 6",
                    "-ex", "continue",
                    "-ex", "quit $_exitcode",
                    binary,
                ], env=env)
                self.assertIn("hit Breakpoint 1", result.stdout,
                              "Did not reach the schedule boundary:\n" + result.stdout)
                self.assertEqual(
                    result.returncode, 0,
                    "A scheduling pause must not fail a correct concurrent "
                    "index migration:\n" + result.stdout,
                )
        finally:
            dropped = run(["psql", control, "-v", "ON_ERROR_STOP=1", "-c",
                           f"DROP DATABASE {database} WITH (FORCE)"])
            self.assertEqual(dropped.returncode, 0, dropped.stdout)


if __name__ == "__main__":
    unittest.main()
