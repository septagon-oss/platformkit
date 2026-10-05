package main

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSeededRolesConvergeAfterOwnerNormalization(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	files := fstest.MapFS{"seed/starter/users.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: users
records:
  - key: roles@example.test
    fields: {displayName: Roles, roles: [" Observer ", observer]}
`)}}
	service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{userSeeder{users: c.users}}, Authorize: seedGrants{auth: c.auth}})
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
			person, err := c.users.ByEmail(ctx, tx, "roles@example.test")
			if err != nil {
				return err
			}
			if len(person.Roles) != 1 || person.Roles[0] != "observer" {
				t.Fatalf("owner roles = %v; want [observer]", person.Roles)
			}
			again, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			if len(again.Items) != 1 || again.Items[0].Action != seed.Unchanged {
				t.Errorf("identical file after owner normalization: %s; want 0 created, 0 updated", again)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
