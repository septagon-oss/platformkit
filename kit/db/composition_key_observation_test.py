"""The cancellation cases observe their own lock, including a positive control."""

import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
import uuid


ROOT = Path(__file__).resolve().parents[2]
SOURCE = Path(os.environ.get("MIGRATION_WAIT_SOURCE", ROOT / "kit/db/migrate_test.go"))


class CompositionKeyObservation(unittest.TestCase):
    def test_observation_distinguishes_own_namespace_and_lock_form(self):
        source = SOURCE.read_text().split("func holdsCompositionKey(", 1)[1]
        body = source.split("\n}", 1)[0]
        parts = re.findall(r"`([^`]+)`", body)
        self.assertEqual(len(parts), 2, "read the helper's SQL, not a second implementation")
        key = int(re.search(r"const compositionLockKey = (\d+)",
                            (ROOT / "kit/db/migrate.go").read_text())[1])
        query = str(key).join(parts)
        schema = "lock_observation_" + uuid.uuid4().hex
        url = os.environ.get("PLATFORMKIT_TEST_ADMIN_URL",
                             "postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable")
        # These statements acknowledge each hold before observing it. All objects
        # roll back and all session locks disappear when this psql process exits.
        sql = f"""
BEGIN;
SET LOCAL statement_timeout = '20s';
CREATE SCHEMA {schema};
SET LOCAL search_path = {schema};
SET LOCAL application_name = '{schema}';
SELECT 'empty=' || ({query});
SELECT pg_advisory_lock({key}, 'pg_catalog'::regnamespace::oid::int);
SELECT 'other_namespace=' || ({query});
SELECT pg_advisory_lock(({key}::bigint << 32) | '{schema}'::regnamespace::oid::bigint);
SELECT 'other_form=' || ({query});
SELECT pg_advisory_lock({key}, '{schema}'::regnamespace::oid::int);
SELECT 'own_namespace=' || ({query});
SELECT pg_advisory_unlock_all();
SELECT 'released=' || ({query});
ROLLBACK;
"""
        with tempfile.TemporaryDirectory() as directory:
            script = Path(directory) / "observe.sql"
            script.write_text(sql)
            result = subprocess.run(
                ["psql", url, "-XqAt", "-v", "ON_ERROR_STOP=1", "-f", str(script)],
                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, text=True, timeout=60,
            )
        self.assertEqual(result.returncode, 0, result.stdout)
        observations = [line for line in result.stdout.splitlines() if "=" in line]
        self.assertEqual(observations, ["empty=0", "other_namespace=0", "other_form=0",
                                        "own_namespace=1", "released=0"])


if __name__ == "__main__":
    unittest.main()
