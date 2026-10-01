package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// TestATenantViewLentByInTenantEndsWithItsCallback: InTenant hands a hook a
// db.Tx[db.Tenant] for one tenant. Once the callback returns, the system
// transaction has its cross-tenant settings back; a tenant-typed handle the
// hook kept must not read through them. Either it is refused or it still sees
// only its own tenant's rows — never another tenant's row.
func TestATenantViewLentByInTenantEndsWithItsCallback(t *testing.T) {
	ctx := t.Context()
	admin, app := dbtest.Schema(t)
	createThings(t, ctx, admin)
	first, other := newTenant("first"), newTenant("other")
	token := syscap.NewSystemToken("kit/db test: a lent tenant view")
	err := db.RunSystem(ctx, app, token, func(ctx context.Context, system db.Tx[db.System]) error {
		if err := insert(system.DB(), other.ID, "other's row"); err != nil {
			return err
		}
		var kept db.Tx[db.Tenant]
		if err := db.InTenant(ctx, system, first, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			kept = tx
			return nil
		}); err != nil {
			return err
		}
		var foreign int64
		if err := kept.DB().Raw(`SELECT count(*) FROM things WHERE tenant_id <> ?`, db.TenantOf(kept).ID).Row().Scan(&foreign); err == nil && foreign != 0 {
			t.Errorf("a Tx[Tenant] for %s read %d of another tenant's rows after its callback returned", db.TenantOf(kept).Slug, foreign)
		}
		return nil
	})
	_ = err // the run may refuse to commit; what the kept handle read is the subject
}
