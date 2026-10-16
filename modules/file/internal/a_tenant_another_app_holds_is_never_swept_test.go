package internal_test

// The path rule and the row rule are two rules, and only the second one answers
// for a byte this store wrote into a directory it owns. The sweep's listing names
// a tenant, and a tenant id is the same vocabulary on every app of the server:
// which app holds it is a fact the row states and the directory does not. So a
// blob of another app's tenant that arrived under this app's own segment — a
// request served by the wrong app — is listed here, and the row is what refuses
// its removal. The control in the same run is this app's own tenant's orphan,
// which must go: a refusal that also stopped the sweep would be a sweep that
// quietly never runs.

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

func TestASweepLeavesATenantAnotherAppHoldsEvenUnderItsOwnSegment(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	betaApp := appname.MustParse("beta")
	beta := internal.NewLocalOf(betaApp, dir)
	alphaTenant := tenancy.Tenant{ID: uuid.New(), Slug: "alpha-customer"}
	betaTenant := tenancy.Tenant{ID: uuid.New(), Slug: "beta-customer"}
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO tenants (id, slug, name, app) VALUES
			(?, ?, ?, ''), (?, ?, ?, 'beta')`,
			alphaTenant.ID, alphaTenant.Slug, alphaTenant.Slug,
			betaTenant.ID, betaTenant.Slug, betaTenant.Slug).Error
	}); err != nil {
		t.Fatalf("create the two apps' tenant rows: %v", err)
	}

	// Both bytes are beta's store's own writes, both aged past the hour, neither
	// named by a row: the only difference between them is which app holds the
	// tenant they were written for.
	putOrphan := func(tenant tenancy.Tenant) string {
		t.Helper()
		ctx := tenancy.WithTenant(t.Context(), tenant)
		scope, err := contracts.ScopeOf(ctx)
		if err != nil {
			t.Fatalf("scope for %s: %v", tenant.Slug, err)
		}
		key := contracts.Key(uuid.NewString())
		if err := beta.Put(ctx, scope, key, strings.NewReader(tenant.Slug), -1, contracts.Meta{}); err != nil {
			t.Fatalf("write %s's orphan: %v", tenant.Slug, err)
		}
		at := filepath.Join(dir, appname.StoragePath(betaApp, tenant.ID, uuid.MustParse(key.String())))
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(at, old, old); err != nil {
			t.Fatalf("age %s's orphan: %v", tenant.Slug, err)
		}
		return at
	}
	alphaBlob := putOrphan(alphaTenant)
	betaBlob := putOrphan(betaTenant)

	sweep := internal.NewReconcile(beta, time.Second)
	sweep.Use(dbtest.SystemToken())
	jobs := sweep.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("beta scheduled %d reconciliation jobs, want one", len(jobs))
	}
	if err := jobs[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("beta's reconciliation job: %v", err)
	}
	if _, err := os.Stat(alphaBlob); err != nil {
		t.Errorf("beta's sweep removed an orphan of a tenant another app holds: %v", err)
	}
	if _, err := os.Stat(betaBlob); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("beta's sweep left its own orphan: stat error %v", err)
	}
}
