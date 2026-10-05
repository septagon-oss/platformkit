package internal_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

// Committing the caller's transaction after a refusal proves that the command
// itself writes nothing; a caller rollback must not conceal an early write.
func TestALifecycleRefusalWithoutItsAppsOperatorCommitsNoChanges(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	academy := internal.NewService(nil, nil, "academy")
	collect := internal.NewService(nil, nil, "collect")
	installedAs(t, conn, academy, "academy-installation", "academy.ops.example.com")
	installedAs(t, conn, collect, "collect-installation", "collect.ops.example.com")
	var before *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		before, err = collect.Create(ctx, tx, contracts.NewTenant{
			Slug: "customer", Name: "Customer", Host: "customer.example.com",
		})
		if err == nil {
			before, err = collect.Get(ctx, tx, before.ID)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Model an unavailable installation while leaving the other app's operator
	// live. The command must inspect the current rows, not a cached boot result.
	if _, err := admin.ExecContext(t.Context(),
		"UPDATE tenants SET deleted_at = now() WHERE app = 'collect' AND operator"); err != nil {
		t.Fatal(err)
	}
	var eventsBefore int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		got, err := collect.Suspend(ctx, tx, before.ID)
		if !errors.Is(err, contracts.ErrNoOperatorTenant) || got != nil {
			t.Errorf("suspend = %v, %v; want nil, ErrNoOperatorTenant", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		after, err := collect.Get(ctx, tx, before.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			t.Errorf("refused suspension changed tenant: before=%+v after=%+v", before, after)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var eventsAfter int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore {
		t.Errorf("refused suspension changed outbox count: before=%d after=%d", eventsBefore, eventsAfter)
	}
	// Restoring this app's own operator makes the same command available again.
	if _, err := admin.ExecContext(t.Context(),
		"UPDATE tenants SET deleted_at = NULL WHERE app = 'collect' AND operator"); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		got, err := collect.Suspend(ctx, tx, before.ID)
		if err == nil && (got == nil || got.Status != contracts.StatusSuspended) {
			t.Errorf("suspend with own operator = %+v, want suspended tenant", got)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
