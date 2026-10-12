package sitetest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps — the same palette
// included, because a resolution rule held by only one of the two would be a rule
// the consumer's tests never see.
func TestFakeConforms(t *testing.T) {
	sitetest.RunService(t, func(t *testing.T, run func(sitetest.Fixture)) {
		fake := sitetest.NewFake()
		fake.Palette = sitetest.InstallationPalette
		run(sitetest.Fixture{
			Ctx: t.Context(), Service: fake, Locked: fake,
			Palette: fake.Palette, Published: fake.Published,
		})
	})
}
