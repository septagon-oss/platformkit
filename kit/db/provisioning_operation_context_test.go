package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestProvisioningOperationsKeepTheirCallerContextAndCallbackLifetime(t *testing.T) {
	_, app := dbtest.Schema(t)
	var saved context.Context
	err := db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, system db.Tx[db.System]) error {
		if err := db.InTenant(ctx, system, newTenant("first"), func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			saved = context.WithoutCancel(ctx)
			caller, cancel := context.WithTimeout(context.WithValue(saved, valueKey{}, "caller"), time.Minute)
			defer cancel()
			if err := db.Run(caller, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				statement := tx.DB().Statement.Context
				if got := statement.Value(valueKey{}); got != "caller" {
					t.Errorf("statement value = %v, want caller", got)
				}
				want, _ := caller.Deadline()
				if got, ok := statement.Deadline(); !ok || !got.Equal(want) {
					t.Errorf("statement deadline = %v (%v), want %v", got, ok, want)
				}
				cancel()
				if err := tx.DB().Exec("SELECT 1").Error; !errors.Is(err, context.Canceled) {
					t.Errorf("cancelled caller's statement = %v, want context.Canceled", err)
				}
				return nil
			}); err != nil {
				return err
			}
			var kept db.Tx[db.Tenant]
			if err := db.Run(saved, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				kept = tx
				return tx.DB().Exec("SELECT 1").Error
			}); err != nil {
				return err
			}
			// A nested Run ending does not shorten the provisioning callback's lifetime.
			return kept.DB().Exec("SELECT 1").Error
		}); err != nil {
			return err
		}
		err := db.Run(saved, app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec("SELECT 1").Error
		})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("joining an expired provisioning view = %v, want context.Canceled", err)
		}
		return system.DB().Exec("SELECT 1").Error
	})
	if err != nil {
		t.Fatalf("system provisioning: %v", err)
	}
}
