package sitetest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/porttest"
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

// TestTheSuiteRunsTheseCases pins every case name, in order. A case name is a
// requirement's evidence in a client repository, so renaming one has to be a diff
// somebody reads, and it is the only thing that notices when a description stops
// declaring a case the harness had been running.
//
// It is also the record of what the kit/porttest description bought here: eight
// cases before it, thirty-three after. Nothing of the eight was renamed away except
// the loops that held several assertions each — "a colour is #rrggbb and a theme
// is one of three", "a link points inside this site" and "a navigation is
// bounded and so is a title" are now one named refusal apiece, and "a home page
// is named by a slug" keeps its name for the refusal while the success beside it
// is "a site with no home page is saved".
//
// Eight of the thirty-three arrived with the port growing an accent that must read
// against both canvases: six of the refused colours are named refusals, because a
// refusal is what the description states and each then gets the floor's own check
// that the site is unchanged and silent; the accepted half, the trim-and-lower-case
// spelling, the read's answer saved back, the theme that moves while the accent is
// still nobody's, the two doors' two reads and the ratio arithmetic are Own cases,
// each with the reason the description cannot state it. The count is written here
// as a number so this comment reddens beside the list if the port grows again.
func TestTheSuiteRunsTheseCases(t *testing.T) {
	want := []string{
		"Settings: the operation says what it did",
		"Save: the operation says what it did",
		"saving what is already stored says nothing",
		"a colour that is not #rrggbb is refused",
		"a three-digit colour is refused",
		"a colour in another notation is refused",
		"a colour of spaces and nothing is refused",
		"a theme outside the three is refused",
		"an accent too dark for the dark canvas is refused",
		"an accent as bright as the light canvas is refused",
		"an accent of paper on paper is refused",
		"an accent of the dark canvas on itself is refused",
		"an accent that only works in the light is refused",
		"an accent a hair under the band's edge is refused",
		"a link to another site is refused",
		"a link that is not rooted is refused",
		"a link to nowhere is refused",
		"a link with nothing to say is refused",
		"a navigation is bounded",
		"a title is bounded",
		"a home page is named by a slug",
		"a tenant that has configured nothing has the defaults",
		"saving records what was configured",
		"changing one thing says so",
		"a site with no home page is saved",
		"an accent inside the band is accepted",
		"a colour typed with room and case is the colour it means",
		"the read's answer, saved back, is not a change",
		"the theme can change while the accent stays unchosen",
		"an accent the tenant chose survives every door",
		"the locked read says what the unlocked read says",
		"the installation's own accent is not a tenant's colour",
		"the ratio the document quotes is the ratio the rule used",
	}
	// Names runs no case, so the suite needs no harness to answer.
	got := porttest.Names(sitetest.Suite(nil))
	if len(got) != len(want) {
		t.Fatalf("the suite runs %d cases:\n%s\nwant %d:\n%s",
			len(got), strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("case %d is %q, want %q", i, got[i], want[i])
		}
	}
}
