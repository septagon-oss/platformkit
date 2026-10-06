"""The boot harness watches the key a migration actually takes.

kit/app's waitFor keeps a queued boot alive only while Postgres shows a request
waiting on the composition key. It spells that key out because kit/db's constant
is unexported, so a key that moves in kit/db/migrate.go would leave the harness
watching nothing and judging a queued boot dead on its grace clock again.

The key is the pair (class, hashtext(namespace)) — the class is the constant below,
the namespace is the one the run's own session resolves to — so what the harness
watches is the class, and what a harness outside kit/db holds has to be the pair of
the schema its boot migrates into. Both halves are pinned here, because either one
drifting apart from kit/db leaves the watch watching a queue nobody joins.
"""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]
CONSTANT = re.compile(r"^const compositionLockKey = (\d+)$", re.MULTILINE)


def key(path):
    found = CONSTANT.findall((ROOT / path).read_text())
    if len(found) != 1:
        raise AssertionError(f"{path} names compositionLockKey {len(found)} times")
    return int(found[0])


class BootWaitsForTheMigrationKey(unittest.TestCase):
    def test_the_boot_wait_watches_the_key_migration_takes(self):
        taken = key("kit/db/migrate.go")
        migrate = (ROOT / "kit/db/migrate.go").read_text()
        self.assertIn("SELECT pg_advisory_lock($1, hashtext(COALESCE(current_schema()",
                      migrate)
        self.assertIn("ExecContext(ctx, compositionLockSQL, int32(compositionLockKey))",
                      migrate)
        self.assertEqual(key("kit/app/app_test.go"), taken,
                         "kit/app's waitFor must watch the key kit/db migration takes")
        harness = (ROOT / "kit/app/app_test.go").read_text()
        self.assertRegex(harness, r"classid::bigint = \$1[\s\S]*?NOT l\.granted OR a\.application_name[\s\S]*?compositionLockKey\)",
                         "waitFor must ask Postgres for a queued request on that class, or one "
                         "this boot's own schema holds")
        # The class is the first half of pg_advisory_lock(int, int), so it has to fit an
        # int4 — that is what makes pg_locks report a composition under this class.
        self.assertLess(taken, 1 << 31)

    def test_a_harness_that_holds_a_boots_key_names_its_namespace(self):
        # A composition key names the namespace a run applies into, so a harness that has
        # to hold one before the boot exists has to name the namespace that boot will
        # migrate into. dbtest takes it from PLATFORMKIT_TEST_SCHEMA; these three harnesses
        # are its only readers, and one of them going quiet about the variable would leave
        # it holding the key of a schema nobody is about to migrate.
        for path in ("kit/db/cancellation_case_waits_for_its_lock_test.py",
                     "kit/app/boot_waits_for_migration_queue_test.py",
                     "apps/platformkit/boot_waits_for_its_composition_key_test.py"):
            text = (ROOT / path).read_text()
            self.assertIn("PLATFORMKIT_TEST_SCHEMA", text,
                          f"{path} must name the schema whose key it holds")
            self.assertIn("hashtext(", text,
                          f"{path} must take the pair kit/db takes")
        self.assertIn("PLATFORMKIT_TEST_SCHEMA",
                      (ROOT / "kit/db/dbtest/dbtest.go").read_text())


if __name__ == "__main__":
    unittest.main()
