"""Pin the verification delivery migration's version within the composition."""

from pathlib import Path
import unittest


class VerificationMigrationVersion(unittest.TestCase):
    def test_verification_delivery_has_its_own_migration_version(self):
        root = Path(__file__).resolve().parents[2]
        deliveries = list(
            (root / "modules/auth/migrations").glob("*_verification_sent_at.up.sql")
        )
        self.assertEqual(len(deliveries), 1, "one owner of the delivery column")
        delivery = deliveries[0]
        version = int(delivery.name.split("_", 1)[0])
        shipped = list((root / "migrations").glob("*.up.sql"))
        shipped.extend((root / "modules").glob("*/migrations/*.up.sql"))
        collisions = [
            str(path.relative_to(root))
            for path in shipped
            if path != delivery and int(path.name.split("_", 1)[0]) == version
        ]
        self.assertEqual(collisions, [], "verification must not reuse a shipped version")


if __name__ == "__main__":
    unittest.main()
