package contenttest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	contenttest.RunService(t, func(t *testing.T, run func(contenttest.Fixture)) {
		fake := contenttest.NewFake()
		run(contenttest.Fixture{Ctx: t.Context(), Service: fake,
			Seed: fake.Put, Content: fake.Content, Published: fake.Published})
	})
}

// TestFakeRecordsWhatItWouldPublish: the one thing the fake offers over the real
// service, for a consumer asserting on what a page did rather than on what it is.
func TestFakeRecordsWhatItWouldPublish(t *testing.T) {
	fake := contenttest.NewFake()
	id := fake.Put(&contracts.Content{Slug: "About Us", Title: "About us"})

	for range 2 { // the second one is idempotent, so it publishes nothing
		if _, err := fake.Publish(t.Context(), db.Tx[db.Tenant]{}, id); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	if _, err := fake.Archive(t.Context(), db.Tx[db.Tenant]{}, id); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	want := []string{contracts.EventPublished, contracts.EventArchived}
	got := fake.Published()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the fake published %v, want %v", got, want)
	}
	if stored := fake.Contents()[id]; stored.Slug != "about-us" || stored.PublishedAt != nil {
		t.Errorf("the store holds %+v; the slug is normalised and an archived page is not published", stored)
	}
}

// TestTheSuiteRunsTheseCases pins every case name, in order. A case name is a
// requirement's evidence in a client repository, so renaming one has to be a
// diff somebody reads; tasktest has pinned its list since the first port, and
// this closes the gap review 1 named (finding 10) for the other two ports.
//
// It is also the record of what the kit/porttest description bought here: nine
// cases before it, eighteen after. The one name of the nine that is gone is the
// parent "an unknown id is not found", whose three assertions are now three
// named cases beside the three commands that owe them.
func TestTheSuiteRunsTheseCases(t *testing.T) {
	want := []string{
		"Publish: the operation says what it did",
		"publishing twice does not move the publication time",
		"Publish: an unknown row is not found",
		"archived content is not published from the archive",
		"Unpublish: the operation says what it did",
		"Unpublish: the same command twice writes nothing and says nothing",
		"Unpublish: an unknown row is not found",
		"Archive: the operation says what it did",
		"Archive: the same command twice writes nothing and says nothing",
		"Archive: an unknown row is not found",
		"Public: the operation says what it did",
		"an unused slug is not found",
		"publishing serves it and records when",
		"unpublishing clears the publication time",
		"unpublishing takes content out of the archive",
		"archiving keeps it and serves it to nobody",
		"only published content is served publicly",
		"a slug is stored and looked up the same way",
	}
	// Names runs no case, so the suite needs no harness to answer.
	got := porttest.Names(contenttest.Suite(nil))
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
