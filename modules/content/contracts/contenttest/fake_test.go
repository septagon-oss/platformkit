package contenttest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	contenttest.RunService(t, func(t *testing.T, run func(contenttest.Fixture)) {
		fake := contenttest.NewFake()
		run(contenttest.Fixture{Ctx: t.Context(), Service: fake, Seed: fake.Put, Published: fake.Published})
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
