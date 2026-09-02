package audittest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit/contracts/audittest"
)

// TestTheFakeIsAService runs the whole conformance suite against the fake. A
// fake that did not pass it would be a second, quieter opinion about what the
// trail does.
func TestTheFakeIsAService(t *testing.T) {
	audittest.RunService(t, func(t *testing.T, run func(audittest.Fixture)) {
		run(audittest.Fixture{
			Ctx:     t.Context(),
			Service: audittest.NewFake(),
			// The fake publishes nothing because the module publishes nothing.
			Published: func() []string { return nil },
		})
	})
}
