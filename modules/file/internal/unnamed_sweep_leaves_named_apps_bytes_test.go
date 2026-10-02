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

// The other direction of a shared volume: the deployment that names no app
// sweeps the un-prefixed position, where a named app's older bytes still sit
// until their move. Those bytes are a tenant of the named app's, and the
// unnamed app's sweep must leave them while it still removes its own orphan.
func TestUnnamedAppSweepLeavesANamedAppsTenantsBytes(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	unnamed := internal.NewLocal(dir)
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

	// Both blobs sit at the un-prefixed position: alpha's because alpha names no
	// app, beta's because beta wrote it before its boot named it and the move
	// into beta's own segment has not run yet.
	putOrphan := func(tenant tenancy.Tenant) string {
		t.Helper()
		ctx := tenancy.WithTenant(t.Context(), tenant)
		scope, err := contracts.ScopeOf(ctx)
		if err != nil {
			t.Fatalf("scope for %s: %v", tenant.Slug, err)
		}
		key := contracts.Key(uuid.NewString())
		if err := unnamed.Put(ctx, scope, key, strings.NewReader(tenant.Slug), -1, contracts.Meta{}); err != nil {
			t.Fatalf("write %s's orphan: %v", tenant.Slug, err)
		}
		at := filepath.Join(dir, appname.StoragePath(appname.Name(""), tenant.ID, uuid.MustParse(key.String())))
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(at, old, old); err != nil {
			t.Fatalf("age %s's orphan: %v", tenant.Slug, err)
		}
		return at
	}
	alphaBlob := putOrphan(alphaTenant)
	betaBlob := putOrphan(betaTenant)

	sweep := internal.NewReconcile(unnamed, time.Second)
	sweep.Use(dbtest.SystemToken())
	jobs := sweep.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("unnamed app scheduled %d reconciliation jobs, want one", len(jobs))
	}
	if err := jobs[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("unnamed app's reconciliation job: %v", err)
	}
	if _, err := os.Stat(alphaBlob); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unnamed app's sweep left its own orphan: stat error %v", err)
	}
	if _, err := os.Stat(betaBlob); err != nil {
		t.Errorf("unnamed app's sweep removed bytes of the named app's tenant: %v", err)
	}
}
