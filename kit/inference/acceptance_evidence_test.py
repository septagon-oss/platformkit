"""Check the inference README's local links and executable acceptance evidence."""

import json
from pathlib import Path
import re
import unittest


PACKAGE = Path(__file__).resolve().parent
ROOT = PACKAGE.parents[1]


class InferenceAcceptanceEvidence(unittest.TestCase):
    def test_local_documentation_links_resolve(self):
        readme = (PACKAGE / "README.md").read_text()
        for target in re.findall(r"\]\(([^)]+)\)", readme):
            if "://" in target:
                continue
            path, _, anchor = target.partition("#")
            with self.subTest(target=target):
                destination = (PACKAGE / path).resolve()
                self.assertTrue(destination.is_relative_to(ROOT))
                self.assertTrue(destination.is_file(), target)
                if anchor:
                    headings = re.findall(r"^#+ (.+)$", destination.read_text(), re.M)
                    slugs = [re.sub(r"[^\w -]", "", title.lower()).replace(" ", "-") for title in headings]
                    self.assertIn(anchor, slugs, target)

    def test_acceptance_names_tests_in_the_maintained_inventory(self):
        readme = (PACKAGE / "README.md").read_text()
        self.assertIn("## Acceptance\n", readme)
        acceptance = readme.split("## Acceptance\n", 1)[1].split("\n## ", 1)[0]
        rows = [line for line in acceptance.splitlines() if line.startswith("| ")]
        claims = [line for line in rows[1:] if not re.fullmatch(r"[\s|:-]+", line)]
        self.assertTrue(claims, "acceptance must name observable behaviour")
        inventory = json.loads((ROOT / "tests/inventory.json").read_text())
        registered = {row["id"]: row for row in inventory["rows"]}
        for claim in claims:
            with self.subTest(claim=claim):
                paths = re.findall(r"\]\(([^)]+_test\.go)\)", claim)
                self.assertTrue(paths, "acceptance must link a Go test file")
                found = set()
                for path in paths:
                    source = (PACKAGE / path).resolve()
                    self.assertTrue(source.is_relative_to(PACKAGE))
                    names = re.findall(r"^func (Test\w+)\(t \*testing\.T\)", source.read_text(), re.M)
                    self.assertTrue(names, path)
                    found.update(names)
                    for name in names:
                        key = f"{source.relative_to(ROOT).as_posix()}#{name}"
                        self.assertIn(key, registered)
                        self.assertEqual(registered[key]["kind"], "go")
                        self.assertIn(registered[key]["tier"], ("push", "merge"))
                        self.assertNotEqual(registered[key]["verdict"], "delete")
                for name in re.findall(r"`(Test\w+)`", claim):
                    self.assertIn(name, found, "named evidence must exist in a linked test file")


if __name__ == "__main__":
    unittest.main()
