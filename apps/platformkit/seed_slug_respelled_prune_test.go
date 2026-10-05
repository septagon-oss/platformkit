package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
)

// A pruning file that respells a page's key the way its owner already spells the
// slug still declares that page. The run must leave the page standing: the same
// run cannot report it unchanged and delete it.
func TestRespellingAPagesKeyToItsSlugDoesNotPruneIt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const file = "seed/starter/contents.yaml"
	files := fstest.MapFS{file: {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nprune: true\nrecords:\n  - key: About The Team\n    fields: {title: About the team, kind: page}\n")}}
	service, err := seed.New(seed.Deps{
		Files: files, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{&contentSeeder{svc: c.contents}}, Authorize: seedGrants{auth: c.auth},
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
				return err
			}
			files[file].Data = []byte("apiVersion: platformkit.seed/v1\nresource: contents\nprune: true\nrecords:\n  - key: about-the-team\n    fields: {title: About the team, kind: page}\n")
			plan, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			for _, item := range plan.Items {
				if item.Action == seed.Prune {
					t.Errorf("a respelled key pruned the page it still declares: %s", plan)
					break
				}
			}
			_, standing, err := crud.List[*contentcontracts.Content](tx, crud.Query{Limit: 1, Filter: map[string]any{"slug": "about-the-team"}})
			if err != nil {
				return err
			}
			if standing != 1 {
				t.Errorf("pages at about-the-team after the run = %d; want the declared page, 1", standing)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
