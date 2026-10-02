package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/content"
)

func TestSeedCommandRequiresAnAuthenticatedOperator(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			home, err := c.contents.Public(ctx, tx, "home")
			if err != nil {
				return err
			}
			_, err = content.Spec.UpdateRow(ctx, tx, home.ID, map[string]any{"title": "Operator credential required"})
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_EMAIL", "")
	t.Setenv("PLATFORMKIT_SEED_OPERATOR_PASSWORD", "")

	err = seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail})
	if err == nil {
		t.Errorf("seed command with no operator credential = %v; want operator authentication refusal", err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			home, err := c.contents.Public(ctx, tx, "home")
			if err == nil && home.Title != "Operator credential required" {
				t.Errorf("unauthenticated seed command changed the page title to %q", home.Title)
			}
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
}
