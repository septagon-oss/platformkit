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
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

func TestASecondTenantCannotReadOrReplaceTheFirstSitesColour(t *testing.T) {
	_, conn := dbtest.Schema(t, site.Migrations)
	svc := internal.NewService()
	first := tenancy.Tenant{ID: uuid.New(), Slug: "first", Name: "First"}
	second := tenancy.Tenant{ID: uuid.New(), Slug: "second", Name: "Second"}
	var saved *contracts.SiteSettings
	run := func(tenant tenancy.Tenant, action func(context.Context, db.Tx[db.Tenant]) error) {
		t.Helper()
		if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, action); err != nil {
			t.Fatal(err)
		}
	}
	run(first, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		saved, err = svc.Save(ctx, tx, &contracts.SiteSettings{Title: "First", PrimaryColor: "#123456"})
		return err
	})
	run(second, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if got.Title != "" || got.PrimaryColor != contracts.DefaultPrimaryColor {
			t.Errorf("second tenant read first tenant's settings: %+v", got)
		}
		result := tx.DB().Model(&contracts.SiteSettings{}).Where("id = ?", saved.ID).Update("primary_color", "#ffffff")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 0 {
			t.Errorf("second tenant changed %d rows by first tenant's ID", result.RowsAffected)
		}
		in := &contracts.SiteSettings{Base: saved.Base, Title: "Second"}
		out, err := svc.Save(ctx, tx, in)
		if err == nil && out.ID == saved.ID {
			t.Error("save retained another tenant's settings ID")
		}
		return err
	})
	run(first, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Settings(ctx, tx)
		if err == nil && (got.PrimaryColor != "#123456" || got.Title != "First") {
			t.Errorf("first tenant's settings changed: %+v", got)
		}
		return err
	})
}
