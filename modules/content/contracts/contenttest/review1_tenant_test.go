package contenttest_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
)

// Review 1, finding 4. The description skips kit/porttest's tenant case
// (porttest.Elsewhere) for all three of this port's commands, and the reason it
// gives the floor is "the world is one tenant's transaction … The fake holds one
// tenant's rows". The fake holds every tenant's rows in one map and reads the
// tenant on the context nowhere: a second tenant reads the first tenant's page
// and archives it. That is the same shape the kernel's own
// TestTenantCaseFailsWhenTheStoreIsShared exists to catch, and
// modules/task/contracts/tasktest already has the cure — porttest.Store, which
// is partitioned by the tenant on the context.
//
// The floor this pins: a consumer that takes the fake instead of a Postgres is
// testing against tenancy, not around it. It passes as soon as the fake's rows
// are partitioned by the tenant on the context, whether through porttest.Store
// or by hand.
func TestAnotherTenantReachesNothingThroughTheFake(t *testing.T) {
	fake := contenttest.NewFake()
	acme := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"})
	other := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "other"})

	id := fake.Put(&contracts.Content{Slug: "about-us", Title: "About us", Kind: contracts.KindPage})
	if _, err := fake.Publish(acme, db.Tx[db.Tenant]{}, id); err != nil {
		t.Fatalf("Publish as the tenant that owns it: %v", err)
	}
	// Reachability, through what the fixed behaviour prints and not the broken
	// one: the tenant that owns the page still reads it back.
	if _, err := fake.Public(acme, db.Tx[db.Tenant]{}, "about-us"); err != nil {
		t.Fatalf("the owning tenant cannot read its own page: %v", err)
	}

	if got, err := fake.Public(other, db.Tx[db.Tenant]{}, "about-us"); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("another tenant read the page: %v, %v; a row of another tenant is not in this tenant's store at all", got, err)
	}
	if _, err := fake.Archive(other, db.Tx[db.Tenant]{}, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("another tenant archived the page: %v; a write across tenants is refused, not performed", err)
	}
	if stored := fake.Contents()[id]; stored.Status != contracts.StatusPublished {
		t.Errorf("the page is %q after another tenant's call; it is still published, and it is still this tenant's", stored.Status)
	}
}
