package main

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestSeedRerunPreservesAPersonsOwnPassword(t *testing.T) {
	const demoPassword = "shared demo password 2026"
	const personalPassword = "personal password chosen later 2026"
	t.Setenv("PLATFORMKIT_DEMO_PASSWORD", demoPassword)
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
		if _, err := c.users.Provision(ctx, system, created.ID, "root@demo.localhost", "Root",
			adminPass, []string{authcontracts.RoleAdmin}); err != nil {
			return err
		}
		return db.InTenant(ctx, system, created.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, err := c.users.ByEmail(ctx, tx, "marta@example.test")
			if err != nil {
				return err
			}
			if !person.CheckPassword(demoPassword) {
				return errors.New("the initial demo credential was not applied; cannot test preservation")
			}
			return c.users.SetPassword(ctx, tx, person.ID, personalPassword)
		})
	}); err != nil {
		t.Fatal(err)
	}

	if err := seedCommand([]string{"--config", path, "--tenant", "demo", "--as", "root@demo.localhost", "--demo"}); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, "demo.localhost")
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, err := c.users.ByEmail(ctx, tx, "marta@example.test")
			if err != nil {
				return err
			}
			if !person.CheckPassword(personalPassword) || person.CheckPassword(demoPassword) {
				t.Error("the seed rerun replaced a person's chosen password with the shared demo password")
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
