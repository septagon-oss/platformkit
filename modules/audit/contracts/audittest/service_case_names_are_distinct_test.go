package audittest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit/contracts/audittest"
)

// TestServiceCaseNamesAreDistinct runs the service suite against the fake and
// requires every named case to appear once, so two split steps cannot share a
// name and hide one another in the test report.
func TestServiceCaseNamesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	audittest.RunService(t, func(t *testing.T, run func(audittest.Fixture)) {
		if seen[t.Name()] {
			t.Fatalf("case name %q is used twice", t.Name())
		}
		seen[t.Name()] = true
		run(audittest.Fixture{
			Ctx:       t.Context(),
			Service:   audittest.NewFake(),
			Published: func() []string { return nil },
		})
	})
}
