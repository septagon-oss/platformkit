package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSiteSeedRefusesAHomePageThatDoesNotExist(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{"seed/starter/sites.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: sites\nrecords:\n  - key: site\n    fields: {homeSlug: missing-page}\n")}},
		Root:  "seed", Clock: seedClock{},
		Writers: []seed.Writer{&siteSeeder{sites: c.sites}}, Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatal(err)
	}
	applyErr := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.users, tx, adminEmail)
			if err != nil {
				return err
			}
			_, err = service.Apply(ctx, tx, seed.Selection{})
			return err
		})
	})
	if applyErr == nil || !strings.Contains(applyErr.Error(), "missing-page") ||
		!strings.Contains(applyErr.Error(), "seed/starter/sites.yaml:") {
		t.Errorf("Apply error = %v; want a dangling-home refusal at the seed record", applyErr)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			settings, err := c.sites.Settings(ctx, tx)
			if err != nil {
				return err
			}
			var events int
			if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = 'site.settings_updated' AND payload->>'homeSlug' = 'missing-page'`).Row().Scan(&events); err != nil {
				return err
			}
			if settings.HomeSlug != "home" || events != 0 {
				t.Errorf("dangling home committed homeSlug=%q and %d outbox events; want home and none", settings.HomeSlug, events)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
