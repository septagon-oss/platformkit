"""kit/db's test files declare each poll helper once: the merge of main's
transaction-owner work once put two waitUntil bodies in one package."""
import pathlib, re, collections, unittest

class OnePollHelperPerName(unittest.TestCase):
    def test_waitUntil_and_waitForPoolStat_are_declared_once(self):
        here = pathlib.Path(__file__).parent
        seen = collections.Counter()
        for f in here.glob("*_test.go"):
            for m in re.finditer(r"^func (waitUntil|waitForPoolStat)\(", f.read_text(), re.M):
                seen[m.group(1)] += 1
        self.assertEqual({k: v for k, v in seen.items() if v != 1}, {})

if __name__ == "__main__":
    unittest.main()
