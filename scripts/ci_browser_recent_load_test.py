"""Browser admission needs a free share in both recent load windows."""

import pathlib
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class BrowserRecentLoad(unittest.TestCase):
    def test_wait_admits_only_when_both_recent_loads_fit(self):
        cores = int(subprocess.check_output(["nproc"], stdin=subprocess.DEVNULL, text=True))
        cases = (
            ("quiet", 0, 0, True),
            ("at_capacity", cores, cores, True),
            ("minute_over_capacity", cores + 0.01, 0, False),
            ("five_minutes_over_capacity", 0, cores + 0.01, False),
            ("both_over_capacity", cores + 0.01, cores + 0.01, False),
        )
        with tempfile.TemporaryDirectory() as directory:
            loadavg = pathlib.Path(directory) / "loadavg"
            for name, minute, five_minutes, ready in cases:
                with self.subTest(name=name):
                    # An older busy window does not delay a host whose recent windows fit.
                    reading = f"{minute:.2f} {five_minutes:.2f} {cores * 2:.2f}"
                    loadavg.write_text(f"{reading} 1/200 99\n", encoding="utf-8")
                    result = subprocess.run(
                        ["bash", str(ROOT / "scripts/ci_browser_host_share.sh"),
                         "wait", "0", str(loadavg), "1"],
                        stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=10,
                    )
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn(f"load {reading} across {cores} cores", result.stdout)
                    self.assertEqual("the host has a free share" in result.stdout, ready, result.stdout)
                    self.assertEqual("starting anyway" in result.stdout, not ready, result.stdout)


if __name__ == "__main__":
    unittest.main()
