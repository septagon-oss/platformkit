"""The two locale test renames preserve executable source and inventory decisions."""
import json
from pathlib import Path
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
MERGE = "b9395d4d6b69abf2d8d6ddc317d8a85fbeb3e8bb"
RENAMES = (
    (
        "review_round1_percent_test.go",
        "acopy_that_escapes_its_percent_sign_loads_and_prints_it_test.go",
        "// A reviewer's case, T-0111 review round 1.\n",
        "// A catalogue that escapes a percent sign loads, and prints the one argument it is\n// given.\n",
    ),
    (
        "review_r5_merged_arity_test.go",
        "alater_source_that_re_words_akey_keeps_the_arguments_its_other_locales_carry_test.go",
        "// A reviewer's case, T-0111 review round 5 (2026-09-29).\n",
        "// A later source that re-words a key keeps the arguments that key's other\n// languages already carry.\n",
    ),
)


def committed(ref, path):
    return subprocess.check_output(
        ["git", "show", f"{ref}:{path}"], cwd=ROOT,
        stdin=subprocess.DEVNULL, text=True, timeout=30,
    )


class TestRenameCarriesAssertions(unittest.TestCase):
    def test_locale_rename_changes_only_the_provenance_header(self):
        directory = "kit/locale/providers/xtext/"
        for old, new, old_header, new_header in RENAMES:
            with self.subTest(file=new):
                before = committed(MERGE + "^2", directory + old)
                after = (ROOT / (directory + new)).read_text()
                self.assertEqual(before.count(old_header), 1)
                self.assertEqual(after, before.replace(old_header, new_header, 1))
                self.assertFalse((ROOT / (directory + old)).exists())

    def test_merge_keeps_parent_inventory_decisions_across_locale_renames(self):
        current = json.loads((ROOT / "tests/inventory.json").read_text())
        by_id = {row["id"]: row for row in current["rows"]}
        for parent in (MERGE + "^1", MERGE + "^2"):
            previous = json.loads(committed(parent, "tests/inventory.json"))
            for row in previous["rows"]:
                identifier = row["id"]
                for old, new, _, _ in RENAMES:
                    identifier = identifier.replace("/" + old + "#", "/" + new + "#")
                if parent.endswith("^2") and identifier not in by_id:
                    continue
                with self.subTest(parent=parent, test=identifier):
                    self.assertIn(identifier, by_id)
                    retained = by_id[identifier]
                    for field in ("verdict", "merge_into", "rename_to", "rule", "rule_present",
                                  "unique_lines", "unique_reach", "unique_reach_kind",
                                  "assertions_in_file"):
                        default = False if field == "rule_present" else None
                        self.assertEqual(row.get(field, default), retained.get(field, default), field)


if __name__ == "__main__":
    unittest.main()
