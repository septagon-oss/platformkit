package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

// TestALifecycleVerbIsMirroredIntoItsOwnAppsOperator: two apps of one database
// each hold an operator tenant (migrations/000043 makes that one per app). A
// verb of the second app writes the operator's copy into the second app's
// operator, never into the first's, whichever operator the table returns first.
func TestALifecycleVerbIsMirroredIntoItsOwnAppsOperator(t *testing.T) {
	_, conn := dbtest.Schema(t)
	academy := internal.NewService(nil, []string{"en"}, "academy")
	collect := internal.NewService(nil, []string{"en"}, "collect")
	bootstrap := func(svc *internal.Service, slug string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			op, err := internal.Bootstrap(ctx, tx, svc, contracts.NewTenant{
				Slug: slug, Name: slug, Host: slug + ".ops.example.com",
			})
			if err == nil {
				id = op.ID
			}
			return err
		}); err != nil {
			t.Fatalf("bootstrap %s: %v", slug, err)
		}
		return id
	}
	// The first app's operator is installed first, so it is the row an
	// app-blind read would most likely answer.
	academyOperator := bootstrap(academy, "academy-installation")
	collectOperator := bootstrap(collect, "collect-installation")

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		customer, err := collect.Create(ctx, tx, contracts.NewTenant{
			Slug: "customer", Name: "Customer", Host: "customer.example.com",
		})
		if err != nil {
			return err
		}
		if _, err := collect.Suspend(ctx, tx, customer.ID); err != nil {
			return err
		}
		var mirrors []uuid.UUID
		if err := tx.DB().Table("platformkit_outbox").
			Where("name = ?", contracts.EventLifecycleRecorded).Pluck("tenant_id", &mirrors).Error; err != nil {
			return err
		}
		if len(mirrors) != 2 {
			t.Fatalf("operator copies = %d, want 2 (create and suspend)", len(mirrors))
		}
		for _, id := range mirrors {
			if id == academyOperator {
				t.Errorf("collect's verb was mirrored into academy's operator %s", id)
			} else if id != collectOperator {
				t.Errorf("collect's verb was mirrored into %s, want collect's operator %s", id, collectOperator)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestAnAppWithNoOperatorOfItsOwnIsRefusedEvenWhenAnotherAppHasOne: the
// refusal TestAnInstallationWithNoOperatorTenantWritesNothing pins holds per
// app — another app's operator is not this app's installation.
func TestAnAppWithNoOperatorOfItsOwnIsRefusedEvenWhenAnotherAppHasOne(t *testing.T) {
	_, conn := dbtest.Schema(t)
	academy := internal.NewService(nil, []string{"en"}, "academy")
	collect := internal.NewService(nil, []string{"en"}, "collect")
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := internal.Bootstrap(ctx, tx, academy, contracts.NewTenant{
			Slug: "academy-installation", Name: "Academy", Host: "academy.ops.example.com",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		got, err := collect.Create(ctx, tx, contracts.NewTenant{
			Slug: "customer", Name: "Customer", Host: "customer.example.com",
		})
		if !errors.Is(err, contracts.ErrNoOperatorTenant) || got != nil {
			t.Errorf("create in an app with no operator returned %v, %v; want nil, ErrNoOperatorTenant", got, err)
		}
		var n int64
		if err := tx.DB().Table("platformkit_outbox").
			Where("name = ?", contracts.EventLifecycleRecorded).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("%d operator copies written into another app's trail", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
