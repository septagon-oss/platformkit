"""kit/crud refuses an operand's kind and leaves its width to the column.

The operand rule reads a Go kind and no range: how wide a column is belongs to the
migration that made it, and this package reads no schema. So a number of an admitted
kind that the column cannot hold is refused by the driver on the way to the wire, and
Classify passes that refusal on unclassified — never an ErrInvalid.

Every case that pins that path asserts a refusal and no answer, and cannot tell the
driver's refusal from a guard of this package's own: an int4 guard written here would
keep all of them green. So the invariant that no statement here names a column's
width, and the sentence that tells a caller who refuses, are held here instead.
"""
import pathlib
import re
import unittest

HERE = pathlib.Path(__file__).parent

# A column's width, spelled as a Go range constant, a literal wide enough to be one, or
# a SQL type name. None belongs in this package: the width lives in the migration that
# made the column, and crud reads no schema. Only code is asked — a comment is free to
# name the width it is talking about.
WIDTH = re.compile(r"MaxInt(8|16|32|64)\b|\b\d{9,}\b|\bint4\b|\bbigint\b|\bsmallint\b|\bnumeric\b")

# Who the package says refuses an operand of the admitted kind and the wrong width: the
# driver, and not ErrInvalid. The two words sit in one paragraph wherever the rule is
# stated — in the Query doc, and above operandFault.
WHO_REFUSES = re.compile(r"driver.{0,400}ErrInvalid|ErrInvalid.{0,400}driver", re.S)


class OperandWidthRefusal(unittest.TestCase):
    def production(self):
        return sorted(p for p in HERE.glob("*.go") if not p.name.endswith("_test.go"))

    def statements(self, path):
        return [(n, line) for n, line in enumerate(path.read_text().splitlines(), 1)
                if not line.strip().startswith("//")]

    def test_no_statement_names_a_columns_width(self):
        named = {}
        for path in self.production():
            hits = [f"{n}: {line.strip()}" for n, line in self.statements(path) if WIDTH.search(line)]
            if hits:
                named[path.name] = hits
        self.assertEqual(named, {})

    def test_the_package_says_the_driver_refuses_the_width(self):
        stated = [path.name for path in self.production() if WHO_REFUSES.search(path.read_text())]
        self.assertIn("predicate.go", stated)
        self.assertIn("crud.go", stated)


if __name__ == "__main__":
    unittest.main()
