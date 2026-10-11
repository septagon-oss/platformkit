package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
)

// A content record's key is spelled by its owner (contracts.Slugify), and a
// site's home page is a reference to that record. The site must open on the
// page the reference resolved to, and an identical second run must write nothing.
func TestSiteHomeReferenceMeetsTheOwnersSpellingOfTheSlug(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{
			"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: About The Team\n    fields: {title: About the team, kind: page}\n    commands:\n      - name: publish\n")},
			"seed/starter/sites.yaml":    {Data: []byte("apiVersion: platformkit.seed/v1\nresource: sites\nrecords:\n  - key: site\n    fields: {homeSlug: contents/About The Team}\n")},
		},
		Root: "seed", Clock: seedClock{},
		Writers:   []seed.Writer{&contentSeeder{svc: c.contents}, &siteSeeder{sites: c.sites}},
		Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.users, tx, adminEmail)
			if err != nil {
				return err
			}
			if _, err := service.Apply(ctx, tx, seed.Selection{}); err != nil {
				t.Errorf("a home reference to a page this seed wrote was refused: %v", err)
				return nil
			}
			settings, err := c.sites.Settings(ctx, tx)
			if err != nil {
				return err
			}
			if want := contentcontracts.Slugify("About The Team"); settings.HomeSlug != want {
				t.Errorf("site opens on %q; want the seeded page's own slug %q", settings.HomeSlug, want)
			}
			again, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			for _, item := range again.Items {
				if item.Action != seed.Unchanged {
					t.Errorf("identical rerun: %s; want every record unchanged", again)
					break
				}
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
