package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

func TestTenantLanguageAccessCannotSoftDeleteItsTenant(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = internal.NewService(nil, nil).Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec("UPDATE tenants SET deleted_at = now() WHERE id = ?", tenant.ID).Error
		})
	if err == nil {
		t.Error("tenant-scoped application transaction committed a soft deletion; language access must not authorize deleting the tenant")
	}
	var deleted bool
	if err := owner.QueryRowContext(t.Context(), "SELECT deleted_at IS NOT NULL FROM tenants WHERE id = $1", tenant.ID).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if deleted {
		t.Error("the tenant row is now soft-deleted; a refused non-language write must leave it active")
	}
}
