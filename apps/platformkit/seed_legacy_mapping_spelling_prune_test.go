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

// Provenance can hold two names for one row: the spelling a file wore before its
// writer learned the owner's, and the spelling the owner stores. Both are the same
// page, and the file still declares it, so the run that reads the older mapping
// must forget nothing and delete nothing. A prune that judged only key text would
// report the page UNCHANGED through one mapping and delete the row through the
// other — the same destructive shape review 9's third finding had, arrived at from
// the data instead of from the file.
func TestPruneKeepsARowItsOlderMappingSpellsDifferently(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const file = "seed/starter/contents.yaml"
	files := fstest.MapFS{file: {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nprune: true\nrecords:\n  - key: about-the-team\n    fields: {title: About the team, kind: page}\n")}}
	service, err := seed.New(seed.Deps{
		Root: "seed", Files: files, Clock: seedClock{},
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
			rows, _, err := crud.List[*contentcontracts.Content](tx, crud.Query{Limit: 1, Filter: map[string]any{"slug": "about-the-team"}})
			if err != nil {
				return err
			}
			if len(rows) != 1 {
				t.Errorf("the declared page is %d rows; want one", len(rows))
				return nil
			}
			page := rows[0].ID
			// The mapping a run before this writer declared the owner's spelling
			// would have left behind: same tenant, same row, the file's key text.
			if err := tx.DB().Exec(`INSERT INTO seed_keys (tenant_id, module, entity, key, kind, record_id)
				VALUES (?, 'content', 'content', 'About The Team', 'starter', ?)`,
				db.TenantOf(tx).ID, page).Error; err != nil {
				return err
			}
			plan, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			for _, item := range plan.Items {
				if item.Action == seed.Prune {
					t.Errorf("the older spelling of a declared page was pruned: %s", plan)
				}
			}
			if _, err := crud.Get[*contentcontracts.Content](tx, page); err != nil {
				t.Errorf("the page the file still declares is gone after the run: %v", err)
			}
			var mappings int
			if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE tenant_id = ? AND module = 'content'`,
				db.TenantOf(tx).ID).Row().Scan(&mappings); err != nil {
				return err
			}
			if mappings < 1 {
				t.Errorf("the run left no provenance for its own page: plan=%s", plan)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
