package internal_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestNamedAppSweepLeavesUnnamedAppsOrphanOnSharedVolume(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	unnamed := internal.NewLocal(dir)
	namedApp := appname.MustParse("beta")
	named := internal.NewLocalOf(namedApp, dir)
	alphaTenant := tenancy.Tenant{ID: uuid.New(), Slug: "alpha-customer"}
	betaTenant := tenancy.Tenant{ID: uuid.New(), Slug: "beta-customer"}

	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO tenants (id, slug, name, app) VALUES
			(?, ?, ?, ''), (?, ?, ?, 'beta')`,
			alphaTenant.ID, alphaTenant.Slug, alphaTenant.Slug,
			betaTenant.ID, betaTenant.Slug, betaTenant.Slug).Error
	})
	if err != nil {
		t.Fatalf("create the two apps' tenant rows: %v", err)
	}

	putOrphan := func(store *internal.Local, app appname.Name, tenant tenancy.Tenant) string {
		t.Helper()
		ctx := tenancy.WithTenant(t.Context(), tenant)
		scope, err := contracts.ScopeOf(ctx)
		if err != nil {
			t.Fatalf("scope for %s: %v", tenant.Slug, err)
		}
		key := contracts.Key(uuid.NewString())
		if err := store.Put(ctx, scope, key, strings.NewReader(tenant.Slug), -1, contracts.Meta{}); err != nil {
			t.Fatalf("write %s's orphan: %v", tenant.Slug, err)
		}
		at := filepath.Join(dir, appname.StoragePath(app, tenant.ID, uuid.MustParse(key.String())))
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(at, old, old); err != nil {
			t.Fatalf("age %s's orphan: %v", tenant.Slug, err)
		}
		return at
	}
	alphaBlob := putOrphan(unnamed, appname.Name(""), alphaTenant)
	betaBlob := putOrphan(named, namedApp, betaTenant)

	// Neither blob has a file row. The named app's job may clean up its own
	// orphan, but the other tenant's app column must keep alpha's bytes intact.
	sweep := internal.NewReconcile(named, time.Second)
	sweep.Use(dbtest.SystemToken())
	jobs := sweep.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("named app scheduled %d reconciliation jobs, want one", len(jobs))
	}
	if err := jobs[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("named app's reconciliation job: %v", err)
	}
	if _, err := os.Stat(alphaBlob); err != nil {
		t.Errorf("named app's sweep removed the unnamed app's orphan: %v", err)
	}
	if _, err := os.Stat(betaBlob); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("named app's sweep left its own orphan: stat error %v", err)
	}
}
