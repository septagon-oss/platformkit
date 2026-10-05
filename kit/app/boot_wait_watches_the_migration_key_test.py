"""The boot harness watches the key a migration actually takes.

kit/app's waitFor keeps a queued boot alive only while Postgres shows a request
waiting on the composition key. It spells that key out because kit/db's constant
is unexported, so a key that moves in kit/db/migrate.go would leave the harness
watching nothing and judging a queued boot dead on its grace clock again.
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


class BootWaitWatchesTheMigrationKey(unittest.TestCase):
    def test_the_boot_wait_watches_the_key_migration_takes(self):
        taken = key("kit/db/migrate.go")
        self.assertIn('"SELECT pg_advisory_lock($1)", compositionLockKey',
                      (ROOT / "kit/db/migrate.go").read_text())
        self.assertEqual(key("kit/app/app_test.go"), taken,
                         "kit/app's waitFor must watch the key kit/db migration takes")
        harness = (ROOT / "kit/app/app_test.go").read_text()
        self.assertRegex(harness, r"objid = \$1 AND NOT granted[\s\S]*?compositionLockKey\)",
                         "waitFor must ask Postgres for an ungranted request on that key")
        # The key is a single bigint below 2^32, so pg_locks carries it whole in objid.
        self.assertLess(taken, 1 << 32)


if __name__ == "__main__":
    unittest.main()
