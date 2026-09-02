package notificationtest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

// TestTheFakeIsAService runs the whole conformance suite against the fake. A
// fake that did not pass it would be a second, quieter opinion about what being
// notified means.
func TestTheFakeIsAService(t *testing.T) {
	notificationtest.RunService(t, func(t *testing.T, run func(notificationtest.Fixture)) {
		fake := notificationtest.NewFake()
		run(notificationtest.Fixture{Ctx: t.Context(), Service: fake, Published: fake.Published})
	})
}
