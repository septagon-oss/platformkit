package file_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/site"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestTheReleaseSweepKeepsTheSiteLogo pins the release rule's boundary: the
// brief releases "a file whose last use ended more than 24 h ago", and the
// site's logo is a measured image that never has a use — site_settings names it
// by a bare uuid (modules/site/contracts/site.go, LogoFileID), files no ledger
// row and places no hold. Whatever starts an image's grace period, a file the
// tenant's site still names must outlive the sweep: releasing it deletes the
// logo the public frame renders a day after it was configured.
func TestTheReleaseSweepKeepsTheSiteLogo(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations, site.Migrations)
	store := file.Local(t.TempDir())
	// The shipped providers, composed the way the reference app composes them,
	// so any port that connects the site's settings to files is in play.
	inputs := pkit.NewModule("inputs", func(w *pkit.Wiring) (module.Module, error) {
		who := caller{hosts: map[string]tenancy.Tenant{host: acme}}
		pkit.Put[httpx.TenantLoader](w, who)
		pkit.Put[httpx.Authorizer](w, who)
		pkit.Put[pkit.Authenticator](w, func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		})
		pkit.Put[contracts.Storage](w, store)
		pkit.Put[jobs.TenantLister](w, widthsOnly{walking: []tenancy.Tenant{acme}})
		return module.Module{Name: "inputs"}, nil
	}, pkit.Provides[httpx.TenantLoader](), pkit.Provides[httpx.Authorizer](),
		pkit.Provides[pkit.Authenticator](), pkit.Provides[contracts.Storage](), pkit.Provides[jobs.TenantLister]())
	plan, err := pkit.NewApp("branded").Use(inputs, file.Module, site.Module).Plan(pkit.Deployment{
		Environment: pkit.Development, Config: config.Config{Files: config.Files{MaxBytes: 16 << 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	files, ok := pkit.Value[contracts.Service](plan)
	if !ok {
		t.Fatal("file provider omitted its service")
	}
	sites, ok := pkit.Value[sitecontracts.Service](plan)
	if !ok {
		t.Fatal("site provider omitted its service")
	}
	admin := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())
	var logo *contracts.File
	if err := db.Run(admin, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		picture := wideFrame(t)
		var err error
		logo, err = files.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil }, contracts.Upload{
			Name: "logo.jpg", ContentType: "image/jpeg", Image: true, Visibility: contracts.VisibilityPublic,
			Body: bytes.NewReader(picture), Declared: int64(len(picture)),
		})
		if err != nil {
			return err
		}
		settings, err := sites.Settings(ctx, tx)
		if err != nil {
			return err
		}
		settings.LogoFileID = &logo.ID
		_, err = sites.Save(ctx, tx, settings)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The probe is the fixed behaviour's own output: the settings name the logo.
	if err := db.Run(admin, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		settings, err := sites.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if settings.LogoFileID == nil || *settings.LogoFileID != logo.ID {
			t.Fatalf("saved logo = %v, want %s", settings.LogoFileID, logo.ID)
		}
		// Elapsed time is seeded on the persisted clock, as the retention
		// fixtures do; NULL stays NULL, so the seed asserts nothing about
		// whether a grace period was started at all.
		return tx.DB().Exec("UPDATE files SET unused_since = unused_since - interval '25 hours' WHERE id = ?",
			logo.ID).Error
	}); err != nil {
		t.Fatal(err)
	}
	for _, mounted := range plan.Modules() {
		for _, job := range mounted.Jobs {
			if job.Name != "file-unused" {
				continue
			}
			if err := job.Run(t.Context(), conn); err != nil {
				t.Fatalf("release sweep: %v", err)
			}
		}
	}
	if err := db.Run(admin, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := crud.Get[*contracts.File](tx, logo.ID); err != nil {
			t.Errorf("the logo the site names, a day after it was configured: %v, want kept", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
