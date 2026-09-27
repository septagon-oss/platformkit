package tasktest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

// Review 5. The description declines kit/porttest's tenant case (Skip[porttest.
// Elsewhere]) for all three commands, and the first half of the reason it gives
// the floor is a claim about *this* package's fake — "The fake partitions its
// store by the tenant on the context" — while the second half points at Postgres.
// contenttest and sitetest each pin the same claim about their own fake
// (TestAnotherTenantReachesNothingThroughTheFake,
// TestTheFakeKeepsEachTenantOnItsOwnRow) after review 1 found content's claim
// untrue; tasktest's claim rests today on porttest.Store's own
// TestStoreKeepsAnotherTenantsRowOutOfReach and on nothing in this package.
//
// The floor this pins: whoever holds the fake is testing against tenancy and not
// around it — a second tenant's command names a row it cannot reach and is
// answered with the same nothing as a row that never existed, and the first
// tenant's row is neither read, written, nor announced. It passes as the fake
// behaves today, and it fails the moment a command of this fake resolves a row
// outside the tenant on the context it was handed.
func TestAnotherTenantReachesNothingThroughTheFake(t *testing.T) {
	// Two tenants and one actor: the only thing that differs between the calls
	// below is the tenant, so a refusal here can only come from tenancy. The
	// policy admits Holder in every tenant, exactly as tasktest.Policy does, so
	// the store is the party that has to refuse.
	here := tenancy.Tenant{ID: uuid.New(), Slug: "acme-task", Name: "Acme"}
	elsewhere := tenancy.Tenant{ID: uuid.New(), Slug: "globex-task", Name: "Globex"}
	acme := tasktest.As(tenancy.WithTenant(t.Context(), here), tasktest.Holder)
	globex := tasktest.As(tenancy.WithTenant(t.Context(), elsewhere), tasktest.Holder)

	fake := tasktest.NewFake()
	fake.Policy = tasktest.Policy{}
	assignee := uuid.New()
	id := fake.Put(acme, &contracts.Task{Title: "chiller-2 supply temperature out of band"})

	// Reachability, reached through the fixed behaviour: the tenant that owns the
	// task reads it and works on it. A test that only watched the refusals would
	// pass against a fake that answered every tenant with nothing.
	if _, err := fake.Assign(acme, db.Tx[db.Tenant]{}, id, assignee); err != nil {
		t.Fatalf("Assign as the tenant that owns the task: %v", err)
	}
	if _, err := fake.Task(acme, id); err != nil {
		t.Fatalf("the owning tenant cannot read its own task back: %v", err)
	}
	fake.Events.Names() // the assignment above is the only news so far

	for _, call := range []struct {
		what string
		do   func(ctx context.Context) error
	}{
		{"Assign", func(ctx context.Context) error {
			_, err := fake.Assign(ctx, db.Tx[db.Tenant]{}, id, assignee)
			return err
		}},
		{"Resolve", func(ctx context.Context) error {
			_, err := fake.Resolve(ctx, db.Tx[db.Tenant]{}, id, "swapped the valve")
			return err
		}},
		{"CheckSLA", func(ctx context.Context) error { _, err := fake.CheckSLA(ctx, db.Tx[db.Tenant]{}, id); return err }},
	} {
		if err := call.do(globex); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("%s as another tenant = %v; a row of another tenant is answered with the same nothing as "+
				"a row that never existed, and it is not performed", call.what, err)
		}
	}

	if said := fake.Events.Names(); len(said) != 1 || said[0] != contracts.EventAssigned {
		t.Errorf("the three refused commands said %v; a refusal is not news", said)
	}
	stored, err := fake.Task(acme, id)
	if err != nil {
		t.Fatalf("reading the owning tenant's task back after another tenant's calls: %v", err)
	}
	if stored.Status != contracts.StatusAcknowledged || stored.AssigneeID == nil || *stored.AssigneeID != assignee {
		t.Errorf("the task is %q/%v after another tenant reached for it; it is where its own tenant left it",
			stored.Status, stored.AssigneeID)
	}
	if stored.Resolution != "" || stored.ResolvedAt != nil || stored.SLABreached {
		t.Errorf("the refused commands wrote %q/%v/breached=%v into the owning tenant's row",
			stored.Resolution, stored.ResolvedAt, stored.SLABreached)
	}
}
