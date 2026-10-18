package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestSeededPagesStayInTheirTenant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		first, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		second, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "second", Name: "Second", Host: "second.localhost",
		})
		if err != nil {
			return err
		}
		var firstID uuid.UUID
		if err := db.InTenant(ctx, system, first, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			page, err := c.contents.Public(ctx, tx, "home")
			if err == nil {
				firstID = page.ID
			}
			return err
		}); err != nil {
			return err
		}
		if err := db.InTenant(ctx, system, second.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			page, err := c.contents.Public(ctx, tx, "home")
			if err != nil {
				return err
			}
			if page.ID == firstID {
				t.Error("two seeded tenants share one home page ID")
			}
			if _, err := crud.Get[*contentcontracts.Content](tx, firstID); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("tenant B read tenant A's page: %v", err)
			}
			if stale, err := content.Spec.UpdateRow(ctx, tx, firstID, map[string]any{"title": "Cross-tenant title"}); err == nil || stale != nil {
				t.Errorf("tenant B updated tenant A's page or received a stale row: row=%v, error=%v", stale, err)
			}
			return nil
		}); err != nil {
			return err
		}
		return db.InTenant(ctx, system, first, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			page, err := c.contents.Public(ctx, tx, "home")
			if err == nil && page.Title != "Home" {
				t.Errorf("tenant A's page title became %q after tenant B's write", page.Title)
			}
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
}
