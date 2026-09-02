package tasktest_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	tasktest.RunService(t, func(t *testing.T, run func(tasktest.Fixture)) {
		fake := tasktest.NewFake()
		run(tasktest.Fixture{Ctx: t.Context(), Service: fake, Seed: fake.Put, Published: fake.Published})
	})
}

// TestFakeRecordsWhatItWouldPublish: the one thing the fake offers over the real
// service, for a consumer asserting on what a task did rather than on what it is.
func TestFakeRecordsWhatItWouldPublish(t *testing.T) {
	fake := tasktest.NewFake()
	id := fake.Put(&contracts.Task{Title: "chiller"})
	who := uuid.New()

	for range 2 { // the second one is idempotent, so it publishes nothing
		if _, err := fake.Assign(t.Context(), db.Tx[db.Tenant]{}, id, who); err != nil {
			t.Fatalf("Assign: %v", err)
		}
	}
	if _, err := fake.Resolve(t.Context(), db.Tx[db.Tenant]{}, id, "swapped the valve"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := []string{contracts.EventAssigned, contracts.EventResolved}
	got := fake.Published()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the fake published %v, want %v", got, want)
	}
	if stored := fake.Tasks()[id]; stored.Status != contracts.StatusResolved {
		t.Errorf("the store holds %q, want the resolved task", stored.Status)
	}
}
