package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

// A person is one record, so the seed holds one provenance row for them, written
// in the spelling the user module stores. That is what the writer's CanonicalKey
// declaration buys: drop it and the mapping keeps the file's spelling, which is a
// second name for one person and a prune candidate the moment a file respells the
// address. The assertion reads the table the kernel owns, not the plan's text,
// because the plan is allowed to print the spelling the file wore.
func TestSeededPersonIsProvenancedUnderTheAddressTheModuleStores(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const file = "seed/starter/users.yaml"
	files := fstest.MapFS{file: {Data: []byte("apiVersion: platformkit.seed/v1\nresource: users\nrecords:\n  - key: Person@Example.test\n    fields:\n      displayName: Person\n      roles: [observer]\n")}}
	service, err := seed.New(seed.Deps{
		Root: "seed", Files: files, Clock: seedClock{},
		Writers: []seed.Writer{userSeeder{users: c.users}}, Authorize: seedGrants{auth: c.auth},
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
			const wantKey = "person@example.test"
			mappings := func() (int, string) {
				var count int
				var stored string
				if err := tx.DB().Raw(`SELECT count(*), coalesce(min(key), '') FROM seed_keys
					WHERE tenant_id = ? AND module = 'user'`, db.TenantOf(tx).ID).Row().Scan(&count, &stored); err != nil {
					t.Error(err)
				}
				return count, stored
			}
			if count, stored := mappings(); count != 1 || stored != wantKey {
				t.Errorf("provenance rows for the person = %d at key %q; want 1 at %q, the address the module stores",
					count, stored, wantKey)
			}
			// The same person declared in the spelling the module stores is still one
			// person: the run writes nothing and adds no mapping beside the first.
			files[file].Data = []byte("apiVersion: platformkit.seed/v1\nresource: users\nrecords:\n  - key: " + wantKey + "\n    fields:\n      displayName: Person\n      roles: [observer]\n")
			again, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			if len(again.Items) != 1 || again.Items[0].Action != seed.Unchanged {
				t.Errorf("the same address respelled: %s; want one unchanged record", again)
			}
			if count, stored := mappings(); count != 1 || stored != wantKey {
				t.Errorf("a respelled address left %d provenance rows at key %q; want the one row at %q", count, stored, wantKey)
			}
			var people int
			if err := tx.DB().Raw(`SELECT count(*) FROM users WHERE lower(email) = ?`, wantKey).Row().Scan(&people); err != nil {
				return err
			}
			if people != 1 {
				t.Errorf("the address the seed declared holds %d people; want the one person the module stored", people)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
