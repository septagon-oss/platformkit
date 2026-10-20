"""The composition cases load the committed document `make generate` writes the goldens from.

compositionConfig is a path, not a configuration: a case that wrote the constant itself
as YAML would load a string and test nothing the goldens describe.
"""

from pathlib import Path
import re
import unittest

HERE = Path(__file__).parent
ROOT = HERE.parent.parent


class CompositionConfigNamesTheGeneratedDocument(unittest.TestCase):
    def test_the_constant_names_the_file_make_generate_reads(self):
        declared = re.search(r'^const compositionConfig = "([^"\n]+)"$',
                             (HERE / "composition_file_test.go").read_text(), re.MULTILINE)
        self.assertIsNotNone(declared, "composition_file_test.go declares no compositionConfig path")
        path = declared.group(1)
        self.assertRegex(path, r"\.ya?ml$")
        self.assertRegex((HERE / path).read_text(), r"(?m)^server:$")

        generate = re.search(r"^generate:.*\n((?:\t.*\n)+)", (ROOT / "Makefile").read_text(), re.MULTILINE)
        self.assertIsNotNone(generate, "the Makefile has no generate recipe")
        self.assertIn(f"composition -config apps/platformkit/{path} ", generate.group(1))

        for case in sorted(HERE.glob("*_test.go")):
            with self.subTest(case=case.name):
                self.assertFalse("[]byte(compositionConfig)" in case.read_text(),
                                 f"{case.name} writes the path compositionConfig names as if it were the configuration")


if __name__ == "__main__":
    unittest.main()
