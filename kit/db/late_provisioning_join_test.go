package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// A context kept from a provisioning callback and joined only after that
// callback returned, while the system transaction lends the next tenant, is born
// expired: it reads and writes nothing, and pointing it at the next tenant is a
// scope mismatch.
func TestAJoinOpenedAfterItsProvisioningCallbackCannotReachTheNextTenant(t *testing.T) {
	admin, app := dbtest.Schema(t)
	createThings(t, t.Context(), admin)
	first, next := newTenant("first"), newTenant("next")
	var kept context.Context
	err := db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, system db.Tx[db.System]) error {
		if err := insert(system.DB(), next.ID, "private"); err != nil {
			return err
		}
		if err := db.InTenant(ctx, system, first, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			kept = context.WithoutCancel(ctx)
			return nil
		}); err != nil {
			return err
		}
		return db.InTenant(ctx, system, next, func(_ context.Context, live db.Tx[db.Tenant]) error {
			joined := false
			if err := db.Run(kept, app, func(ctx context.Context, late db.Tx[db.Tenant]) error {
				joined = true
				if err := ctx.Err(); !errors.Is(err, context.Canceled) {
					t.Errorf("late join's context = %v, want context.Canceled", err)
				}
				var rows int64
				err := late.DB().Raw("SELECT count(*) FROM things").Row().Scan(&rows)
				if !errors.Is(err, context.Canceled) || rows != 0 {
					t.Errorf("late join read = (%d, %v), want (0, context.Canceled)", rows, err)
				}
				if err := insert(late.DB(), next.ID, "late write"); !errors.Is(err, context.Canceled) {
					t.Errorf("late join write = %v, want context.Canceled", err)
				}
				return nil
			}); err != nil {
				return err
			}
			if !joined {
				t.Fatal("the late join did not run")
			}
			retargeted := db.Run(tenancy.WithTenant(kept, next), app, func(context.Context, db.Tx[db.Tenant]) error { return nil })
			if !errors.Is(retargeted, db.ErrScopeMismatch) {
				t.Errorf("late join pointed at the next tenant = %v, want db.ErrScopeMismatch", retargeted)
			}
			if n := count(t, live.DB()); n != 1 {
				t.Errorf("next tenant's live view sees %d rows, want its private row", n)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("system provisioning: %v", err)
	}
	if n := countAs(t, tenancy.WithTenant(t.Context(), next), app); n != 1 {
		t.Errorf("next tenant kept %d rows, want only its private row", n)
	}
}
