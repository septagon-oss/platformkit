package usertest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: the auth module tests against it, so it is testing against the
// same rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	usertest.RunService(t, func(t *testing.T, run func(usertest.Fixture)) {
		fake := usertest.NewFake()
		run(usertest.Fixture{Ctx: t.Context(), Service: fake, Published: fake.Published})
	})
}
