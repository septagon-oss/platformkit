package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

// errRollback ends a case's transaction without committing it.
var errRollback = errors.New("rolled back on purpose")

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real cross-tenant transaction. Two
// implementations, one specification.
func TestServiceConforms(t *testing.T) {
	tenanttest.RunService(t, func(t *testing.T, run func(tenanttest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService(nil)
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			run(tenanttest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc,
				Published: func() []string { return outbox(t, tx) },
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// outbox is what has been published in this transaction, in order. It is what
// makes the suite's silence assertions real: an idempotent command that
// published a second time is visible here and nowhere else.
func outbox(t *testing.T, tx db.Tx[db.System]) []string {
	t.Helper()
	var names []string
	err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// TestTheCreateHookRunsInTheSameTransaction is the mechanism the composition
// hangs on: a module above this one seeds a new tenant's rows, in the
// transaction that created the tenant, so an administrator created in the same
// breath finds their role already there. A hook that fails takes the tenant
// with it.
func TestTheCreateHookRunsInTheSameTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	boom := errors.New("the hook refused")
	var seen uuid.UUID

	svc := internal.NewService([]contracts.Hook{
		func(_ context.Context, tx db.Tx[db.System], created *contracts.Tenant) error {
			seen = created.ID
			// Anything the hook writes is in the same transaction, so this row
			// is either there with the tenant or with neither.
			return tx.DB().Exec("INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, '{}')",
				created.ID, "seeded").Error
		},
	})
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		return err
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var roles int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM roles WHERE tenant_id = $1 AND name = 'seeded'`, seen).Scan(&roles); err != nil {
		t.Fatalf("count the seeded roles: %v", err)
	}
	if roles != 1 {
		t.Errorf("the hook wrote %d rows, want the one it wrote in the create's transaction", roles)
	}

	// A hook that fails leaves no tenant behind.
	failing := internal.NewService([]contracts.Hook{
		func(context.Context, db.Tx[db.System], *contracts.Tenant) error { return boom },
	})
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := failing.Create(ctx, tx, contracts.NewTenant{Slug: "globex", Name: "Globex", Host: "globex.example.com"})
		return err
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Create with a failing hook = %v, want the hook's error", err)
	}
	var tenants int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM tenants WHERE slug = 'globex'`).Scan(&tenants); err != nil {
		t.Fatalf("count the tenants: %v", err)
	}
	if tenants != 0 {
		t.Errorf("a failed hook left %d tenants behind", tenants)
	}
}

// TestATenantTransactionSeesOnlyItsOwnRow is the control-plane exemption made
// exact. tenants and tenant_hosts are read across tenants by the loader, under
// system access; an ordinary tenant transaction sees one row — its own — which
// is the same guarantee every other table gives, reached by naming the row
// instead of a column on it.
func TestATenantTransactionSeesOnlyItsOwnRow(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService(nil)

	var acme, globex *contracts.Tenant
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		if acme, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"}); err != nil {
			return err
		}
		globex, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "globex", Name: "Globex", Host: "globex.example.com"})
		return err
	})
	if err != nil {
		t.Fatalf("create two tenants: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme.Tenancy()), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var slugs []string
		if err := tx.DB().Table("tenants").Order("slug").Pluck("slug", &slugs).Error; err != nil {
			return err
		}
		if len(slugs) != 1 || slugs[0] != "acme" {
			t.Errorf("a tenant transaction sees %v, want only its own row", slugs)
		}
		var hosts []string
		if err := tx.DB().Table("tenant_hosts").Order("host").Pluck("host", &hosts).Error; err != nil {
			return err
		}
		if len(hosts) != 1 || hosts[0] != "acme.example.com" {
			t.Errorf("a tenant transaction sees hosts %v, want only its own", hosts)
		}
		// And it may not write them: the control plane is changed through the
		// routes that hold the capability, not from inside a tenant.
		err := tx.DB().Exec("UPDATE tenants SET name = 'Stolen' WHERE id = ?", globex.ID).Error
		if err == nil {
			var name string
			_ = tx.DB().Raw("SELECT name FROM tenants WHERE id = ?", globex.ID).Row().Scan(&name)
			if name == "Stolen" {
				t.Error("a tenant transaction renamed another tenant")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the tenant transaction: %v", err)
	}
}

// TestBootstrapRefusesASecondInstallation: the one write with no caller to
// authorize is safe because it can only ever happen once.
func TestBootstrapRefusesASecondInstallation(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService(nil)
	first := contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"}

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := internal.Bootstrap(ctx, tx, svc, first)
		return err
	})
	if err != nil {
		t.Fatalf("the first bootstrap: %v", err)
	}
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{Slug: "globex", Name: "Globex", Host: "globex.example.com"})
		return err
	})
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("the second bootstrap = %v, want ErrConflict", err)
	}
}
