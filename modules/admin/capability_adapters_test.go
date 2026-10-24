package admin

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type headerLookup func(context.Context, db.Tx[db.Tenant], uuid.UUID) (*usercontracts.User, error)

func (f headerLookup) Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*usercontracts.User, error) {
	return f(ctx, tx, id)
}

type switcherList func(context.Context, db.Tx[db.System]) ([]*tenantcontracts.Tenant, error)

func (f switcherList) List(ctx context.Context, tx db.Tx[db.System]) ([]*tenantcontracts.Tenant, error) {
	return f(ctx, tx)
}

func TestHeaderReadsOnlyLookupInTheRequestTransaction(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	tenant, id := tenancy.Tenant{ID: uuid.New()}, uuid.New()
	calls := 0
	people := callerOf(headerLookup(func(_ context.Context, tx db.Tx[db.Tenant], got uuid.UUID) (*usercontracts.User, error) {
		calls++
		if got != id || db.TenantOf(tx).ID != tenant.ID {
			t.Fatal("lookup lost request scope")
		}
		return &usercontracts.User{Email: "ada@example.test", DisplayName: "Ada"}, nil
	}))
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		person, err := people.Person(ctx, id)
		if err == nil && (person.DisplayName != "Ada" || person.Email != "ada@example.test") {
			t.Fatalf("header person = %+v", person)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || callerOf(nil) != nil {
		t.Fatal("optional header lookup was not preserved")
	}
}

func TestSwitcherPrefersNarrowListAndKeepsSuspendedTenants(t *testing.T) {
	legacy := tenanttest.NewFake()
	narrow := switcherList(func(context.Context, db.Tx[db.System]) ([]*tenantcontracts.Tenant, error) {
		return []*tenantcontracts.Tenant{{Status: tenantcontracts.StatusSuspended}}, nil
	})
	rows, err := tenantList(Deps{TenantList: narrow, Tenants: legacy}).List(t.Context(), db.Tx[db.System]{})
	if err != nil || len(rows) != 1 || rows[0].Status != tenantcontracts.StatusSuspended {
		t.Fatalf("switcher = %v, %v", rows, err)
	}
	if tenantList(Deps{Tenants: legacy}) != legacy {
		t.Fatal("legacy fallback was lost")
	}
}
