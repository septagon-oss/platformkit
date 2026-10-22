package db_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Joins racing callback exit must expire before another tenant borrows the
// connection, including joins made with cancellation removed from the caller.
func TestProvisioningJoinsCannotReachTheNextTenantsRows(t *testing.T) {
	admin, app := dbtest.Schema(t)
	createThings(t, t.Context(), admin)
	first, next := newTenant("first"), newTenant("next")
	const callers = 16
	handles := make([]db.Tx[db.Tenant], callers)
	joined := make([]error, callers)
	var workers sync.WaitGroup
	err := db.RunSystem(t.Context(), app, dbtest.SystemToken(), func(ctx context.Context, system db.Tx[db.System]) error {
		if err := insert(system.DB(), next.ID, "private"); err != nil {
			return err
		}
		if err := db.InTenant(ctx, system, first, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
			start := make(chan struct{})
			for i := range callers {
				workers.Go(func() {
					<-start
					joined[i] = db.Run(context.WithoutCancel(ctx), app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
						handles[i] = tx
						return nil
					})
				})
			}
			close(start)
			return nil
		}); err != nil {
			return err
		}
		workers.Wait()
		return db.InTenant(ctx, system, next, func(_ context.Context, live db.Tx[db.Tenant]) error {
			if n := count(t, live.DB()); n != 1 {
				t.Fatalf("next tenant sees %d rows, want its private row", n)
			}
			for i, expired := range handles {
				if joined[i] != nil {
					t.Fatalf("join %d: %v", i, joined[i])
				}
				if err := expired.DB().Statement.Context.Err(); !errors.Is(err, context.Canceled) {
					t.Errorf("join %d context after callback = %v, want context.Canceled", i, err)
				}
				var rows int64
				err := expired.DB().Raw("SELECT count(*) FROM things").Row().Scan(&rows)
				if !errors.Is(err, context.Canceled) || rows != 0 {
					t.Errorf("join %d read = (%d, %v), want (0, context.Canceled)", i, rows, err)
				}
				if err := insert(expired.DB(), next.ID, "expired write"); !errors.Is(err, context.Canceled) {
					t.Errorf("join %d write = %v, want context.Canceled", i, err)
				}
			}
			return nil
		})
	})
	workers.Wait()
	if err != nil {
		t.Fatalf("system provisioning: %v", err)
	}
	if n := countAs(t, tenancy.WithTenant(t.Context(), next), app); n != 1 {
		t.Errorf("next tenant kept %d rows, want only its private row", n)
	}
}
