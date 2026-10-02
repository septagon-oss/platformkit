package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Demo people can follow a walkthrough immediately after the demo seed runs.
// Their roles and password come through the user owner's commands, not from
// fields the seed inserts directly into the users table.
func TestDemoPeopleHaveTheirRolesAndPassword(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo", Name: "Demo", Host: "demo.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		_, err = c.users.Provision(ctx, system, created.ID, "root@demo.localhost", "Root",
			adminPass, []string{authcontracts.RoleAdmin})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const password = "demo walkthrough password 2026"
	t.Setenv("PLATFORMKIT_DEMO_PASSWORD", password)
	if err := seedCommand([]string{"--config", path, "--tenant", "demo", "--as", "root@demo.localhost", "--demo"}); err != nil {
		t.Fatalf("seed the demo tenant: %v", err)
	}

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, "demo.localhost")
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for _, email := range []string{"marta@example.test", "devin@example.test", "amara@example.test"} {
				person, err := c.users.ByEmail(ctx, tx, email)
				if err != nil {
					t.Errorf("seeded person %s: %v", email, err)
					continue
				}
				if len(person.Roles) == 0 {
					t.Errorf("seeded person %s has no role for the demo journey", email)
				}
				if !person.CanSignIn() || !person.CheckPassword(password) {
					t.Errorf("seeded person %s cannot sign in with the demo password", email)
				}
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
