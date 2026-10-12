package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// TestAnotherTenantIsAnsweredItsOwnSiteAndNotTheFirsts holds row-level security
// across the resolution: the colour a tenant chose is its own, the installation's
// accent is what a tenant that chose none is answered, and neither read nor write
// of the second tenant reaches the first tenant's row. The resolution runs on the
// row the transaction's tenant can see, so a tenant with no row is answered the
// palette at revision 0 however many rows other tenants hold.
func TestAnotherTenantIsAnsweredItsOwnSiteAndNotTheFirsts(t *testing.T) {
	admin, conn := dbtest.Schema(t, site.Migrations)
	svc := internal.NewService(sitetest.InstallationPalette)
	first := tenancy.Tenant{ID: uuid.New(), Slug: "first", Name: "First"}
	second := tenancy.Tenant{ID: uuid.New(), Slug: "second", Name: "Second"}
	light := sitetest.InstallationPalette.Accent(contracts.ThemeLight)

	err := db.Run(tenancy.WithTenant(t.Context(), first), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "First Works", PrimaryColor: "#b45309"})
		return err
	})
	if err != nil {
		t.Fatalf("the first tenant's save: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), second), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if got.Title != "" || got.Revision != 0 || got.PrimaryColor != light {
			t.Errorf("the second tenant reads %q at revision %d in %q; want its own empty site at 0 in the installation's %q",
				got.Title, got.Revision, got.PrimaryColor, light)
		}
		locked, err := svc.SettingsForUpdate(ctx, tx)
		if err != nil {
			return err
		}
		if locked.Title != "" || locked.Revision != 0 || locked.PrimaryColor != light {
			t.Errorf("the second tenant's locked read is %q at revision %d in %q; want its own empty site",
				locked.Title, locked.Revision, locked.PrimaryColor)
		}
		saved, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Second Works"})
		if err != nil {
			return err
		}
		if saved.Revision != 1 || saved.PrimaryColor != light {
			t.Errorf("the second tenant's first save answers revision %d in %q, want 1 in %q", saved.Revision, saved.PrimaryColor, light)
		}
		var visible int
		if err := tx.DB().Raw(`SELECT count(*) FROM site_settings`).Scan(&visible).Error; err != nil {
			return err
		}
		if visible != 1 {
			t.Errorf("the second tenant's transaction sees %d rows, want only its own", visible)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the second tenant: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), first), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if got.Title != "First Works" || got.Revision != 1 || got.PrimaryColor != "#b45309" {
			t.Errorf("after the second tenant wrote, the first reads %q at revision %d in %q; its own row moved",
				got.Title, got.Revision, got.PrimaryColor)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the first tenant's read: %v", err)
	}

	var rows int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM site_settings`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("two tenants hold %d rows, want one each", rows)
	}
}
