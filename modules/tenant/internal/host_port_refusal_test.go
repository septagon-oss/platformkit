package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// Mailed links combine a tenant's hostname with the installation's served port.
// A host row must not smuggle its own port into that composition.
func TestAHostWithAPortIsRefusedWithoutChangingTheTenantsAddress(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil, "")
	installed(t, conn, svc)
	var customer *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		customer, err = svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var state string
		if err := admin.QueryRowContext(t.Context(), `SELECT json_build_object(
			'hosts', (SELECT json_agg(h ORDER BY host) FROM tenant_hosts h),
			'events', (SELECT json_agg(e ORDER BY id) FROM platformkit_outbox e))::text`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := snapshot()
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		got, err := svc.AddHost(ctx, tx, customer.ID, "new.acme.example.com:8443", true)
		if !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("host with port = %v, want invalid", err)
		}
		if got != nil {
			t.Error("refused host returned a tenant row")
		}
		// Commit even after refusal: validation must precede every write.
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Error("refused host changed committed host or outbox rows")
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := svc.AddHost(ctx, tx, customer.ID, "new.acme.example.com", true); err != nil {
			return err
		}
		resolved, err := svc.ByHost(ctx, tx, "new.acme.example.com")
		if err == nil && resolved.ID != customer.ID {
			t.Errorf("bare hostname resolves to %s, want %s", resolved.ID, customer.ID)
		}
		return err
	}); err != nil {
		t.Fatalf("the same hostname without a port must be usable: %v", err)
	}
}
