"""The committed composition describes person lookup as an optional edge."""

import json
from pathlib import Path
import unittest


class OptionalPersonCompositionTest(unittest.TestCase):
    def test_person_lookup_is_optional_and_names_its_provider(self):
        for environment in ("development", "production"):
            with self.subTest(environment=environment):
                path = Path(__file__).with_name(f"COMPOSITION.{environment}.json")
                document = json.loads(path.read_text())
                self.assertTrue(document["resolved"])
                modules = document["modules"]
                admin = next(module for module in modules if module["name"] == "admin")
                user = next(module for module in modules if module["name"] == "user")
                contract = "usercontracts.Service"
                self.assertIn({"contract": contract, "from": "user"}, admin["uses"])
                self.assertNotIn(contract, [edge["contract"] for edge in admin["needs"]])
                self.assertIn(contract, user["provides"])
                self.assertIn("user", admin["after"])
                self.assertLess(modules.index(user), modules.index(admin))


if __name__ == "__main__":
    unittest.main()
