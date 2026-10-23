package audittest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit/contracts/audittest"
)

// TestEveryServiceCaseRunsAgainstAFixture counts the fixtures the service
// suite opens against the fake, so a case that drops out of the suite when it
// is split into steps changes the count.
func TestEveryServiceCaseRunsAgainstAFixture(t *testing.T) {
	opened := 0
	audittest.RunService(t, func(t *testing.T, run func(audittest.Fixture)) {
		opened++
		run(audittest.Fixture{
			Ctx:       t.Context(),
			Service:   audittest.NewFake(),
			Published: func() []string { return nil },
		})
	})
	const want = 7
	if opened != want {
		t.Fatalf("the service suite opened %d fixtures, want %d", opened, want)
	}
}
