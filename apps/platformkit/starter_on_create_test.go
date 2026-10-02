package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// A newly created tenant has useful content before anyone runs a separate
// command. Both the bootstrap and control-plane create use the tenant module's
// creation path, so neither should leave a person looking at an empty site.
func TestNewTenantOpensWithStarterAndDemoContent(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		bootstrapTenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "walkthrough", Name: "Walkthrough", Host: "walkthrough.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		for _, want := range []struct {
			tenant   tenancy.Tenant
			demoKeys int
		}{
			// The count is the demo half of the reference seed, record for record:
			// five pages, three people, three pieces of work and one image
			// (seed/demo/{contents,users,tasks,files}.yaml). A tenant whose own row
			// does not say demo is created with none of them.
			{tenant: bootstrapTenant, demoKeys: 0},
			{tenant: created.Tenancy(), demoKeys: 12},
		} {
			if err := db.InTenant(ctx, system, want.tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				for _, slug := range []string{"home", "about"} {
					if _, err := c.contents.Public(ctx, tx, slug); err != nil {
						t.Errorf("new tenant %s has no published starter page %s: %v", want.tenant.Slug, slug, err)
					}
				}
				var demoKeys int
				if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE kind = 'demo'`).Row().Scan(&demoKeys); err != nil {
					return err
				}
				if demoKeys != want.demoKeys {
					t.Errorf("new tenant %s has %d demo records; want %d", want.tenant.Slug, demoKeys, want.demoKeys)
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
