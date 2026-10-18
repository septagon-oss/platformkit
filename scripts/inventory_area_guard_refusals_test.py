"""The area-total guard refuses missing, duplicated or miscounted document rows."""
import importlib.util
from pathlib import Path
import re
import unittest
from unittest.mock import patch


class InventoryAreaGuardRefusals(unittest.TestCase):
    def test_area_guard_detects_document_changes(self):
        source = Path(__file__).with_name("inventory_area_totals_match_document_test.py")
        spec = importlib.util.spec_from_file_location("area_guard", source)
        guard = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(guard)
        document_path = guard.ROOT / "tests/INVENTORY.md"
        document = document_path.read_text()
        heading = "### Round-named verdicts by area\n"
        prefix, section = document.split(heading, 1)
        row = re.search(r"^\| (\w+) \| (\d+) \| (\d+) \| (\d+) \| (\d+) \|$",
                        section, re.MULTILINE)
        self.assertIsNotNone(row)
        original = row.group(0)
        miscounted = original.replace("| " + row.group(2) + " |",
                                      "| " + str(int(row.group(2)) + 1) + " |", 1)
        omitted = re.search(r"`(\w+)` \((\d+) files\b", section)
        self.assertIsNotNone(omitted)
        missing_population = section[:omitted.start()] + section[omitted.end():]
        table_case = "test_area_table_preserves_file_counts_and_verdict_totals"
        prose_case = "test_omitted_areas_retain_their_population_in_prose"
        cases = (
            ("unchanged table", table_case, section, 0),
            ("unchanged prose", prose_case, section, 0),
            ("wrong population", table_case, section.replace(original, miscounted, 1), 1),
            ("missing row", table_case, section.replace(original, "", 1), 1),
            ("duplicate row", table_case, section.replace(original, original + "\n" + original, 1), 1),
            ("missing prose population", prose_case, missing_population, 1),
        )
        read_text = Path.read_text
        for label, case, changed_section, failures in cases:
            with self.subTest(change=label):
                changed = prefix + heading + changed_section
                self.assertEqual(changed == document, failures == 0)

                def substituted(path, *args, **kwargs):
                    if path == document_path:
                        return changed
                    return read_text(path, *args, **kwargs)

                result = unittest.TestResult()
                with patch.object(Path, "read_text", substituted):
                    guard.InventoryAreaTotalsMatchDocument(case).run(result)
                self.assertEqual(result.testsRun, 1)
                self.assertEqual(result.errors, [])
                self.assertEqual(result.skipped, [])
                self.assertEqual(len(result.failures), failures)


if __name__ == "__main__":
    unittest.main()
