package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

// The user module stores an address the way it spells addresses (trimmed,
// lower case). A seeded person whose file writes the address in another case is
// still one person, and an identical second run writes nothing and emits nothing.
func TestSeededPersonMeetsTheOwnersSpellingOfTheirAddress(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{"seed/starter/users.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: users\nrecords:\n  - key: Person@Example.test\n    fields:\n      displayName: Person\n      roles: [observer]\n")}},
		Root:  "seed", Clock: seedClock{}, Writers: []seed.Writer{userSeeder{users: c.users}}, Authorize: seedGrants{auth: c.auth},
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
				t.Errorf("identical seed of a person after the owner spelled their address: %s; want unchanged", again)
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
