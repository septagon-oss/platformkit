package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestDemoTenantWithoutConfiguredPasswordGetsUsableSignIn(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	t.Setenv("PLATFORMKIT_DEMO_PASSWORD", "")
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo-no-password", Name: "Demo", Host: "demo-no-password.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, created.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, err := c.users.ByEmail(ctx, tx, "marta@example.test")
			if err != nil {
				return err
			}
			if !person.CanSignIn() {
				t.Error("demo tenant's seeded person has no sign-in credential when no password was configured")
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
