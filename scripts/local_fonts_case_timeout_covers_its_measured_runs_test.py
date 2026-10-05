"""The local-fonts browser case is given ten times its measured p99, as every bound is.

The rule the split states for a bound that must exist is 10 x the measured p99 on the
CI runner. The case `Core and schema-generated forms inherit native … through local
fonts, history and two worker saves` has a measured p99, written beside
`localFontsToggle` in the file itself: 285 samples in the forge's job logs, p99
96.3 s, slowest 97.6 s. Its own `{ timeout: … }` is the bound that ends the case on a
slow runner, so it is the bound the rule prices.
"""

import os
from pathlib import Path
import re
import unittest


SUITE = Path(
    os.environ.get(
        "EDITOR_REPLACEMENT_SUITE",
        Path(__file__).resolve().parents[1]
        / "tools/designexport/openpencil/editor/replacement.test.mjs",
    )
)

# The case's p99 in milliseconds over its 285 samples in the job logs of runs 100 on.
MEASURED_P99_MS = 96_303

CASE = re.compile(
    r"^test\(`Core and schema-generated forms inherit native .*?through local fonts, "
    r"history and two worker saves.*?\{ timeout: ([0-9_]+) \}",
    re.M,
)


class LocalFontsCaseTimeoutCoversItsMeasuredRunsTest(unittest.TestCase):
    def test_the_local_fonts_case_is_given_ten_times_its_measured_p99(self):
        found = CASE.findall(SUITE.read_text())
        self.assertEqual(len(found), 1, f"the local-fonts case's declaration in {SUITE}")
        timeout = int(found[0].replace("_", ""))
        self.assertGreaterEqual(
            timeout,
            10 * MEASURED_P99_MS,
            f"the local-fonts case is cut off at {timeout} ms, "
            f"{timeout / MEASURED_P99_MS:.2f} x its measured p99 of {MEASURED_P99_MS} ms",
        )


if __name__ == "__main__":
    unittest.main()
