package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSeededContentUsesTheOwnersCanonicalSlug(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: About The Team\n    fields: {title: About the team, kind: page}\n")}},
		Root:  "seed", Clock: seedClock{}, Writers: []seed.Writer{&contentSeeder{svc: c.contents}}, Authorize: seedGrants{auth: c.auth},
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
			var before, after int
			if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox").Row().Scan(&before); err != nil {
				return err
			}
			again, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			if len(again.Items) != 1 || again.Items[0].Action != seed.Unchanged {
				t.Errorf("identical seed after owner slug normalization: %s; want unchanged", again)
			}
			if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox").Row().Scan(&after); err != nil {
				return err
			}
			if after != before {
				t.Errorf("identical rerun emitted %d extra events", after-before)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
