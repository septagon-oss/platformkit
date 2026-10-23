"""The sign-in link refusal stays registered for the composition test gate."""
import json
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
CASE = (
    "apps/platformkit/sign_in_again_not_claimable_by_link_test.go"
    "#TestALinkCannotClaimThatASessionEnded"
)


class SignInLinkEvidence(unittest.TestCase):
    def test_link_refusal_remains_in_the_merge_suite(self):
        inventory = json.loads((ROOT / "tests/inventory.json").read_text())
        matches = [row for row in inventory["rows"] if row["id"] == CASE]
        self.assertEqual(len(matches), 1, "the sign-in link refusal needs one row")
        row = matches[0]
        self.assertEqual(row["tier"], "merge")
        self.assertEqual(row["layer"], "composition")
        self.assertEqual(row["pkg"], "apps/platformkit")
        self.assertEqual(row["verdict"], "keep")
        self.assertTrue(row["needs_db"])


if __name__ == "__main__":
    unittest.main()
