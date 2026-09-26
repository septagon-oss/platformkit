package tasktest_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	tasktest.RunService(t, func(t *testing.T, run func(tasktest.Fixture)) {
		// The fake's store is partitioned by the tenant on the context and
		// refuses one that names none, so the world names a tenant exactly as a
		// request transaction does.
		ctx := tenancy.WithTenant(t.Context(), acme)
		fake := tasktest.NewFake()
		run(tasktest.Fixture{
			Ctx: ctx, Service: fake,
			Seed:      func(task *contracts.Task) uuid.UUID { return fake.Put(ctx, task) },
			Task:      func(id uuid.UUID) (contracts.Task, error) { return fake.Task(ctx, id) },
			Published: fake.Events.Names,
		})
	})
}

// acme is the tenant the fake's cases run in. One tenant is enough here: what a
// second one cannot reach is a row-level-security question, and
// modules/task/internal asks it over Postgres.
var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// TestFakeRecordsWhatItWouldPublish: the one thing the fake offers over the real
// service, for a consumer asserting on what a task did rather than on what it is.
func TestFakeRecordsWhatItWouldPublish(t *testing.T) {
	ctx := tenancy.WithTenant(t.Context(), acme)
	fake := tasktest.NewFake()
	id := fake.Put(ctx, &contracts.Task{Title: "chiller"})
	who := uuid.New()

	for range 2 { // the second one is idempotent, so it publishes nothing
		if _, err := fake.Assign(ctx, db.Tx[db.Tenant]{}, id, who); err != nil {
			t.Fatalf("Assign: %v", err)
		}
	}
	if _, err := fake.Resolve(ctx, db.Tx[db.Tenant]{}, id, "swapped the valve"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := []string{contracts.EventAssigned, contracts.EventResolved}
	got := fake.Events.Names()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the fake published %v, want %v", got, want)
	}
	stored, err := fake.Task(ctx, id)
	if err != nil {
		t.Fatalf("reading the task back: %v", err)
	}
	if stored.Status != contracts.StatusResolved {
		t.Errorf("the store holds %q, want the resolved task", stored.Status)
	}
}

// TestTheSuiteRunsTheseCases pins every case name, in order. A case name is a
// requirement's evidence, so a change to one should be a diff somebody reads —
// and this list is also the record of what the kit/porttest description bought:
// ten cases before it, eighteen after, with no name of the ten lost except the
// parent "an unknown id is not found", whose three assertions are now three
// cases of their own.
func TestTheSuiteRunsTheseCases(t *testing.T) {
	want := []string{
		"Assign: the operation says what it did",
		"assign is idempotent for the same assignee",
		"Assign: an unknown row is not found",
		"assign requires an assignee",
		"a resolved task cannot be assigned",
		"a closed task cannot be assigned",
		"Resolve: the operation says what it did",
		"Resolve: the same command twice writes nothing and says nothing",
		"Resolve: an unknown row is not found",
		"resolve refuses a different resolution",
		"CheckSLA: the operation says what it did",
		"CheckSLA: the same command twice writes nothing and says nothing",
		"CheckSLA: an unknown row is not found",
		"assign acknowledges an open task",
		"resolve records the resolution and the time",
		"resolve is idempotent for the same resolution",
		"check-sla flags an overdue task once",
		"check-sla leaves a future deadline alone",
	}
	// Names runs no case, so the suite needs no harness to answer.
	got := porttest.Names(tasktest.Suite(nil))
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
