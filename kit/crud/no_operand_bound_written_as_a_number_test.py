"""kit/crud bounds an operand by shape and never by a number written in its own source.

The width of a column belongs to the migration that made it, so this package reads no
schema and holds no range: a number of an admitted kind that the column cannot encode is
refused by the driver on the way to the wire, and Classify passes that refusal on
unclassified. The scanner beside this file refuses the words a width would be spelled
with (MaxInt32, int4, bigint); this one refuses the number itself, which the words miss.

Two spellings of that guard answer the other scanner green and are the ones a reader
reaches for: a bound at a two-byte column (32767) and a bound at a four-byte one written
with Go's digit separators (2_147_483_647). Both are a column's width written into the
package that says it reads none, so neither may appear in a statement here.
"""
import pathlib
import re
import unittest

HERE = pathlib.Path(__file__).parent

# A number of the size a column's width is, written as digits with or without Go's
# separators, or named as Go's own range constant. Deliberate bounds of this package —
# a page size, a list length, a parameter count — are counts of rows and marks, four
# digits at most, and are named rather than compared inline.
QUOTED = re.compile(r'"(?:[^"\\]|\\.)*"')

WIDTH_NUMBER = re.compile(r"\b\d[\d_]{4,}\b|\bmath\.(Max|Min)(Int|Uint)(8|16|32|64)\b")

# A number only means a column's width where the code compares against it. A number
# assigned to a name, or returned, is a value, and a value this package returns about a
# column would be the same refusal told in a different voice — which is what an operator
# bound is not allowed to be.
COMPARES = re.compile(r"[<>=]=?|\bgreater\b|\bless(er)?\b")


class OperandBoundIsNeverANumber(unittest.TestCase):
    def production(self):
        return sorted(p for p in HERE.glob("*.go") if not p.name.endswith("_test.go"))

    def statements(self, path):
        # A comment may discuss a width it does not enforce, and a number inside a
        # quoted string is text on the wire — a SQLSTATE a caller reads — and never a
        # bound the code compares against.
        out = []
        for n, line in enumerate(path.read_text().splitlines(), 1):
            if line.strip().startswith("//"):
                continue
            out.append((n, QUOTED.sub('""', line)))
        return out

    def test_no_statement_compares_against_a_column_sized_number(self):
        named = {}
        for path in self.production():
            hits = [f"{n}: {line.strip()}" for n, line in self.statements(path)
                    if WIDTH_NUMBER.search(line) and COMPARES.search(line)]
            if hits:
                named[path.name] = hits
        self.assertEqual(named, {})


if __name__ == "__main__":
    unittest.main()
