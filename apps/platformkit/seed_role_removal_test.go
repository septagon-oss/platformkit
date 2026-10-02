package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSeedRemovesRolesNoLongerDeclared(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	makeSeed := func(roles string) *seed.Service {
		t.Helper()
		files := fstest.MapFS{"seed/starter/users.yaml": {Data: []byte(
			"apiVersion: platformkit.seed/v1\nresource: users\nrecords:\n  - key: person@example.test\n    fields:\n      displayName: Person\n      roles: " + roles + "\n")}}
		service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
			Writers: []seed.Writer{userSeeder{users: c.users}}, Authorize: seedGrants{auth: c.auth}})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	withRole := makeSeed("[observer]")
	withoutRole := makeSeed("[]")

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
			if _, err := withRole.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			if _, err := withoutRole.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			person, err := c.users.ByEmail(ctx, tx, "person@example.test")
			if err != nil {
				return err
			}
			if len(person.Roles) != 0 {
				t.Errorf("seeded person retains roles %v after the declaration removed them", person.Roles)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
