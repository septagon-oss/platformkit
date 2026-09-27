package sitetest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/porttest"
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

// TestTheSuiteRunsTheseCases pins every case name, in order. A case name is a
// requirement's evidence in a client repository, so renaming one has to be a
// diff somebody reads; tasktest has pinned its list since the first port, and
// this closes the gap review 1 named (finding 10) for the other two ports.
//
// It is also the record of what the kit/porttest description bought here: eight
// cases before it, eighteen after. Nothing of the eight was renamed away except
// the loops that held several assertions each — "a colour is #rrggbb and a theme
// is one of three", "a link points inside this site" and "a navigation is
// bounded and so is a title" are now one named refusal apiece, and "a home page
// is named by a slug" keeps its name for the refusal while the success beside it
// is "a site with no home page is saved".
func TestTheSuiteRunsTheseCases(t *testing.T) {
	want := []string{
		"Settings: the operation says what it did",
		"Save: the operation says what it did",
		"saving what is already stored says nothing",
		"a colour that is not #rrggbb is refused",
		"a three-digit colour is refused",
		"a colour in another notation is refused",
		"a theme outside the three is refused",
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
