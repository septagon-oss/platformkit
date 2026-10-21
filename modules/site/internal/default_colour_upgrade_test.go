package internal_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

func TestDefaultColourUpgradePreservesExistingChoicesAndEvents(t *testing.T) {
	files, err := fs.ReadDir(site.Migrations.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	previous := fstest.MapFS{}
	for _, file := range files {
		if file.Name() == "000054_site_default_accent.up.sql" {
			continue
		}
		data, err := fs.ReadFile(site.Migrations.Files, file.Name())
		if err != nil {
			t.Fatal(err)
		}
		previous[file.Name()] = &fstest.MapFile{Data: data}
	}
	source := site.Migrations
	source.Files = previous
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, source); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, adminURL)
	conn, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// The palette the installation composes its own sheets from: Settings answers a
	// row that names no accent with this pair's accent, and the colours saved below
	// are read back unchanged whatever the pair.
	pair := design.Default()
	svc := internal.NewService(contracts.PaletteFunc(func(mode string) string {
		if mode == contracts.ThemeDark {
			return pair.Dark.AccentDefault
		}
		return pair.Light.AccentDefault
	}))
	tenants := []tenancy.Tenant{{ID: uuid.New()}, {ID: uuid.New()}}
	colours := []string{"#2563eb", "#b45309"}
	for i, tenant := range tenants {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Existing", PrimaryColor: colours[i]})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, site.Migrations); err != nil {
		t.Fatalf("upgrade the existing installation: %v", err)
	}
	for i, tenant := range tenants {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			got, err := svc.Settings(ctx, tx)
			if err != nil {
				return err
			}
			if got.PrimaryColor != colours[i] || got.Revision != 1 {
				t.Errorf("existing tenant reads colour %q at revision %d; want %q at revision 1", got.PrimaryColor, got.Revision, colours[i])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(tenants) {
		t.Errorf("upgrade left %d events; want only the %d original saves", count, len(tenants))
	}
	newTenant := tenancy.Tenant{ID: uuid.New()}
	err = db.Run(tenancy.WithTenant(t.Context(), newTenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var colour string
		// btrim because char(7) is blank-padded: the padding is how the column shows
		// the short value, not a colour of its own, and Service.stored trims it the
		// same way at the door every read of this row comes through.
		if err := tx.DB().Raw("INSERT INTO site_settings (tenant_id, title) VALUES (?, ?) RETURNING btrim(primary_color)", newTenant.ID, "New").Scan(&colour).Error; err != nil {
			return err
		}
		if colour != contracts.DefaultPrimaryColor {
			t.Errorf("new tenant's SQL default is %q; want the contract's %q", colour, contracts.DefaultPrimaryColor)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
