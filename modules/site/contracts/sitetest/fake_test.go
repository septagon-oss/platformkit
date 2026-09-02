package sitetest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	sitetest.RunService(t, func(t *testing.T, run func(sitetest.Fixture)) {
		fake := sitetest.NewFake()
		run(sitetest.Fixture{Ctx: t.Context(), Service: fake, Published: fake.Published})
	})
}
