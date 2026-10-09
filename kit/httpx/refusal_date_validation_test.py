"""Exercise the refusal Date assertion with fixed exchanges and malformed replies."""

from pathlib import Path
import re
import subprocess
import tempfile
import unittest


SOURCE = Path(__file__).with_name("review_round5_refusal_shape_test.go")
HARNESS = r'''
package datecheck

import (
    "net/http"
    "testing"
    "time"
)

type dateReporter interface {
    Helper()
    Errorf(string, ...any)
}

type dateErrors struct { count int }
func (*dateErrors) Helper() {}
func (r *dateErrors) Errorf(string, ...any) { r.count++ }

func TestDateBelongsToItsOwnExchange(t *testing.T) {
    instant := time.Date(2026, time.October, 9, 4, 38, 34, 0, time.UTC)
    stamp := func(offset time.Duration) string { return instant.Add(offset).Format(http.TimeFormat) }
    for _, tc := range []struct {
        name string
        dates []string
        sent, read time.Duration
        valid bool
    }{
        {"whole second", []string{stamp(0)}, 0, time.Millisecond, true},
        {"truncated second", []string{stamp(0)}, 900*time.Millisecond, 950*time.Millisecond, true},
        {"slow exchange", []string{stamp(12*time.Second)}, 0, 20*time.Second, true},
        {"next exchange", []string{stamp(time.Second)}, time.Second, 2*time.Second, true},
        {"missing", nil, 0, time.Second, false},
        {"duplicate", []string{stamp(0), stamp(0)}, 0, time.Second, false},
        {"malformed", []string{"not a date"}, 0, time.Second, false},
        {"stale", []string{stamp(-2*time.Second)}, 0, time.Second, false},
        {"future", []string{stamp(2*time.Second)}, 0, time.Second, false},
    } {
        t.Run(tc.name, func(t *testing.T) {
            reporter := &dateErrors{}
            round5Date(reporter, tc.name, round5Answer{
                header: http.Header{"Date": tc.dates},
                sentAt: instant.Add(tc.sent), readAt: instant.Add(tc.read),
            })
            if got := reporter.count == 0; got != tc.valid {
                t.Fatalf("accepted = %v, want %v", got, tc.valid)
            }
        })
    }
}
'''


class RefusalDateValidation(unittest.TestCase):
    def test_date_assertion_accepts_exchange_stamps_and_rejects_invalid_dates(self):
        source = SOURCE.read_text()
        declaration = re.search(r"(?ms)^type round5Answer struct \{.*?^\}", source)
        helper = re.search(r"(?ms)^func round5Date\(.*?^\}", source)
        self.assertIsNotNone(declaration, "the response fixture moved")
        self.assertIsNotNone(helper, "the Date assertion moved")
        # Replace only the reporter type so expected refusals can be counted.
        body = helper.group().replace("t *testing.T", "t dateReporter", 1)
        self.assertNotEqual(body, helper.group(), "the reporter signature moved")
        with tempfile.TemporaryDirectory() as directory:
            candidate = Path(directory) / "date_test.go"
            candidate.write_text(HARNESS + declaration.group() + "\n" + body)
            result = subprocess.run(
                ["go", "test", "-v", str(candidate)],
                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, text=True, timeout=120,
            )
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("--- PASS: TestDateBelongsToItsOwnExchange", result.stdout)


if __name__ == "__main__":
    unittest.main()
