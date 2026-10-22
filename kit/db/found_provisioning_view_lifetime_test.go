package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTransactionsFoundInAProvisioningContextCannotReachTheNextTenant(t *testing.T) {
	admin, app := dbtest.Schema(t)
	createThings(t, t.Context(), admin)
	first, next := newTenant("first"), newTenant("next")
	var saved context.Context
	var early db.Tx[db.Tenant]
	err := db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, system db.Tx[db.System]) error {
		if err := insert(system.DB(), next.ID, "private"); err != nil {
			return err
		}
		if err := db.InTenant(ctx, system, first, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			saved = context.WithoutCancel(ctx)
			var ok bool
			early, ok = db.TxFrom(saved)
			if !ok {
				return errors.New("no transaction inside the provisioning callback")
			}
			if n := count(t, early.DB()); n != 0 {
				t.Errorf("first tenant read %d rows, want none", n)
			}
			return nil
		}); err != nil {
			return err
		}
		return db.InTenant(ctx, system, next, func(_ context.Context, live db.Tx[db.Tenant]) error {
			late, ok := db.TxFrom(saved)
			if !ok {
				return errors.New("the saved context lost its transaction")
			}
			for name, expired := range map[string]db.Tx[db.Tenant]{"found during callback": early, "found after callback": late} {
				if got := db.TenantOf(expired); got != first {
					t.Errorf("%s: tenant = %v, want %v", name, got, first)
				}
				var rows int64
				err := expired.DB().Raw("SELECT count(*) FROM things").Row().Scan(&rows)
				if !errors.Is(err, context.Canceled) || rows != 0 {
					t.Errorf("%s: read = (%d, %v), want (0, context.Canceled)", name, rows, err)
				}
				if err := insert(expired.DB(), next.ID, "expired write"); !errors.Is(err, context.Canceled) {
					t.Errorf("%s: write = %v, want context.Canceled", name, err)
				}
			}
			if n := count(t, live.DB()); n != 1 {
				t.Errorf("next tenant sees %d rows, want its private row", n)
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
