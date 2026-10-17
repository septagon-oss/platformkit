package main

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// TestTheRepairWalksActiveTenantsAndWritesOnlyWhatItsSeederWrote reaches this command
// with two tenants, which no committed case does: one dead grant in both, and a run
// must move acme's row and leave globex's alone. See $STATE/REVIEW.md, item 2b.
func TestTheRepairWalksActiveTenantsAndWritesOnlyWhatItsSeederWrote(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	acme := bootstrappedTenant(t, c, conn)
	var globex tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, e := c.tenants.Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: "globex", Name: "Globex", Host: globexHost,
		})
		if e == nil {
			globex = created.Tenancy()
		}
		return e
	})
	if err != nil {
		t.Fatalf("create the customer tenant: %v", err)
	}
	holds := func(w tenancy.Tenant, yes bool) {
		t.Helper()
		if got := slices.Contains(rolesOf(t, c, conn, w)[authcontracts.RoleAdmin], "ghost:read"); got != yes {
			t.Errorf("%s's administrator holds ghost:read is %v, want %v", w.Slug, got, yes)
		}
	}
	status := func(w tenancy.Tenant, want string) {
		t.Helper()
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec("UPDATE tenants SET status = ? WHERE id = ?", want, w.ID).Error
		})
		if err != nil {
			t.Fatalf("set %s %q: %v", w.Slug, want, err)
		}
	}
	repair := func() {
		t.Helper()
		if e := repairRoles([]string{"--config", path, "--remove"}); e != nil {
			t.Fatalf("repair-roles --remove: %v", e)
		}
	}
	plant(t, conn, acme, authcontracts.RoleAdmin, "ghost:read")
	plant(t, conn, globex, authcontracts.RoleAdmin, "ghost:read")
	status(globex, tenantcontracts.StatusSuspended)
	repair()
	holds(acme, false)  // the one active tenant's row moved, so the run reached a row
	holds(globex, true) // and wrote nowhere else: a suspended tenant is not in its walk
	status(globex, tenantcontracts.StatusActive)
	repair()
	holds(globex, true) // walked this time: its administrator was seeded the wildcard and nothing else
}
