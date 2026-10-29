package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

type operatorHost func(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error)

func (f operatorHost) ByHost(ctx context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error) {
	return f(ctx, tx, host)
}

func TestSeedOperatorNeedsOnlyScopedLookupsAndExplicitPermission(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	fake := usertest.NewFake()
	person, err := fake.Invite(t.Context(), db.Tx[db.Tenant]{}, "operator@example.test", "Operator")
	if err != nil {
		t.Fatal(err)
	}
	password := "a sufficiently long password"
	if err := fake.SetPassword(t.Context(), db.Tx[db.Tenant]{}, person.ID, password); err != nil {
		t.Fatal(err)
	}
	operator := tenancy.Tenant{ID: uuid.New(), Operator: true}
	cfg := config.Config{}
	cfg.Server.InstallationHost = "ops.example.test"
	cfg.Seed.OperatorEmail = person.Email
	cfg.Seed.OperatorPassword = password
	for _, explicit := range []bool{false, true} {
		reads := 0
		c := composition{addresses: identityAddress{fake}, hostLookup: operatorHost(func(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
			if host != cfg.Server.InstallationHost {
				t.Fatalf("host = %s", host)
			}
			return operator, nil
		}), permissions: identityPermissions(func(_ context.Context, tx db.Tx[db.Tenant], _ []string) ([]string, error) {
			reads++
			if db.TenantOf(tx).ID != operator.ID {
				t.Fatal("permission read escaped installation tenant")
			}
			if explicit {
				return []string{tenantcontracts.PermissionTenantManage}, nil
			}
			return []string{"*"}, nil
		})}
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error { return seedOperator(ctx, c, tx, cfg) })
		if (err == nil) != explicit || reads != 1 {
			t.Fatalf("explicit=%v: %v; reads=%d", explicit, err, reads)
		}
	}
}

type seedTenantList func(context.Context, db.Tx[db.System]) ([]*tenantcontracts.Tenant, error)

func (f seedTenantList) List(ctx context.Context, tx db.Tx[db.System]) ([]*tenantcontracts.Tenant, error) {
	return f(ctx, tx)
}

func TestSeedTenantSelectionNeedsOnlyTheControlPlaneList(t *testing.T) {
	id := uuid.New()
	calls := 0
	list := seedTenantList(func(context.Context, db.Tx[db.System]) ([]*tenantcontracts.Tenant, error) {
		calls++
		return []*tenantcontracts.Tenant{{Slug: "other", ID: uuid.New()}, {Slug: "target", ID: id}}, nil
	})
	found, err := seedTenant(t.Context(), list, db.Tx[db.System]{}, "target")
	if err != nil || found.ID != id || calls != 1 {
		t.Fatalf("selection = %v, %v; calls=%d", found, err, calls)
	}
	if _, err := seedTenant(t.Context(), list, db.Tx[db.System]{}, "absent"); err == nil {
		t.Fatal("missing tenant selected")
	}
}
