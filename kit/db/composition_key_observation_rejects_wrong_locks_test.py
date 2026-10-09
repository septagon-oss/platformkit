"""The lock observation case rejects loss of either namespace or lock form."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
PIN = ROOT / "kit/db/composition_key_observation_test.py"
SOURCE = ROOT / "kit/db/migrate_test.go"


class CompositionKeyObservationRejectsWrongLocks(unittest.TestCase):
    def test_observation_case_rejects_broader_lock_predicates(self):
        source = SOURCE.read_text()
        namespace = "AND l.objid = coalesce(to_regnamespace(a.application_name), 0)::oid"
        lock_form = "AND l.objsubid = 2"
        for clause in (namespace, lock_form):
            self.assertEqual(source.count(clause), 1, "re-anchor the changed predicate")
        variants = {
            "unchanged": source,
            "without_namespace": source.replace(namespace, ""),
            "without_lock_form": source.replace(lock_form, ""),
            "without_either": source.replace(namespace, "").replace(lock_form, ""),
        }
        with tempfile.TemporaryDirectory() as directory:
            candidate = Path(directory) / "migrate_test.go"
            for name, contents in variants.items():
                with self.subTest(predicate=name):
                    candidate.write_text(contents)
                    result = subprocess.run(
                        [sys.executable, "-B", str(PIN)], cwd=ROOT,
                        env={**os.environ, "MIGRATION_WAIT_SOURCE": str(candidate)},
                        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                        stderr=subprocess.STDOUT, text=True, timeout=90,
                    )
                    if name == "unchanged":
                        self.assertEqual(result.returncode, 0, result.stdout)
                    else:
                        self.assertEqual(result.returncode, 1, result.stdout)
                        self.assertIn("Lists differ:", result.stdout)
                        self.assertNotIn("ERROR:", result.stdout)


if __name__ == "__main__":
    unittest.main()
