package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestANestedTenantViewEndsWithItsProvisioningCallback(t *testing.T) {
	admin, app := dbtest.Schema(t)
	createThings(t, t.Context(), admin)
	first, other := newTenant("first"), newTenant("other")
	var kept db.Tx[db.Tenant]
	joined := false
	err := db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, system db.Tx[db.System]) error {
		if err := insert(system.DB(), other.ID, "private"); err != nil {
			return err
		}
		if err := db.InTenant(ctx, system, first, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			return db.Run(context.WithoutCancel(ctx), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				kept, joined = tx, true
				if n := count(t, tx.DB()); n != 0 {
					t.Errorf("tenant view sees %d rows during provisioning, want 0", n)
				}
				return nil
			})
		}); err != nil {
			return err
		}
		if !joined {
			t.Fatal("the nested tenant operation did not run")
		}
		var foreign int64
		if err := kept.DB().Raw("SELECT count(*) FROM things WHERE tenant_id <> ?", first.ID).Row().Scan(&foreign); err == nil && foreign != 0 {
			t.Errorf("expired nested tenant view read %d foreign rows", foreign)
		}
		if err := insert(kept.DB(), other.ID, "foreign write"); err == nil {
			t.Error("expired nested tenant view wrote a foreign tenant row")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("system provisioning: %v", err)
	}
	if n := countAs(t, tenancy.WithTenant(t.Context(), other), app); n != 1 {
		t.Errorf("other tenant kept %d rows, want only its original row", n)
	}
}
