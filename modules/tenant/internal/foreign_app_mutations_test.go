package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

func TestAnotherAppsTenantCannotBeChangedThroughTheControlPlane(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	owner := internal.NewService(nil, []string{"en", "pt-PT"}, "academy")
	stranger := internal.NewService(nil, []string{"en", "pt-PT"}, "collect")
	// The installation's own tenant, which modules/tenant's Create now asks for
	// before it writes anything: a schema that has never been bootstrapped holds no
	// operator tenant to mirror a create's audit row into. The world gained a tenant;
	// every assertion below is the assertion review 14 wrote.
	installed(t, conn, owner)
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = owner.Create(ctx, tx, contracts.NewTenant{
			Slug: "customer", Name: "Customer", Host: "customer.example.com",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Read through the owner connection so the assertion can see any change,
	// including one the app-scoped service would conceal on a subsequent read.
	snapshot := func(t *testing.T) string {
		t.Helper()
		var state string
		err := admin.QueryRowContext(t.Context(), `SELECT jsonb_build_array(
			(SELECT jsonb_agg(t ORDER BY id) FROM tenants t),
			(SELECT jsonb_agg(h ORDER BY host) FROM tenant_hosts h),
			(SELECT jsonb_agg(l ORDER BY tenant_id, locale) FROM tenant_locales l),
			(SELECT jsonb_agg(o ORDER BY id) FROM platformkit_outbox o))::text`).Scan(&state)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	for _, command := range []struct {
		name string
		run  func(context.Context, db.Tx[db.System], *internal.Service) (*contracts.Tenant, error)
	}{
		{"add-host", func(ctx context.Context, tx db.Tx[db.System], svc *internal.Service) (*contracts.Tenant, error) {
			return svc.AddHost(ctx, tx, tenant.ID, "second.example.com", true)
		}},
		{"set-locale", func(ctx context.Context, tx db.Tx[db.System], svc *internal.Service) (*contracts.Tenant, error) {
			return svc.SetLocale(ctx, tx, tenant.ID, contracts.SetLocale{Default: "pt-PT", Supported: []string{"en"}})
		}},
		{"suspend", func(ctx context.Context, tx db.Tx[db.System], svc *internal.Service) (*contracts.Tenant, error) {
			return svc.Suspend(ctx, tx, tenant.ID)
		}},
	} {
		t.Run(command.name, func(t *testing.T) {
			before := snapshot(t)
			err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
				got, err := command.run(ctx, tx, stranger)
				if !errors.Is(err, crud.ErrNotFound) || got != nil {
					t.Errorf("foreign app mutation returned row %v, error %v; want nil, ErrNotFound", got, err)
				}
				// Commit deliberately: a refusal must not rely on its caller
				// rolling back to prevent domain or outbox writes.
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if snapshot(t) != before {
				t.Error("foreign app mutation changed tenant, hosts, locales or outbox")
			}
			if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
				got, err := command.run(ctx, tx, owner)
				if err == nil && (got == nil || got.ID != tenant.ID) {
					t.Errorf("own app mutation returned %v, want its tenant", got)
				}
				return err
			}); err != nil {
				t.Fatalf("same valid command in the owning app: %v", err)
			}
			if snapshot(t) == before {
				t.Error("own app command made no change; the case must exercise a real mutation")
			}
		})
	}
}
