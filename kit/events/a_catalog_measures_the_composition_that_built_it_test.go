package events_test

// The manifest that measures a publish is either the process's install or the
// composition's own Catalog, and the second is a fact the caller already had: two
// compositions live in one process, and a publish one of them carries is measured by that
// one whatever the tenant table says about the tenant the row belongs to. Delivery and
// granting are untouched (kit/events/README.md, "Which catalogue measures a publish"), so
// what moves here is which contract refuses.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// invoiceTotal is what app beta declares billing.invoice_issued to carry: minor units.
type invoiceTotal struct {
	Total int64 `json:"total"`
}

// TestACatalogMeasuresItsOwnManifestWhenTheTenantBelongsToAnotherApp is the write door of
// a composition that is not the tenant's app: the payload is measured against what the
// carrying composition declared, and the row is still the tenant's own.
func TestACatalogMeasuresItsOwnManifestWhenTheTenantBelongsToAnotherApp(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	t.Cleanup(func() { events.DeclareAll(nil) })
	tenantID := uuid.New()
	placeTenant(t, conn, tenantID, "invoice-customer", "acme")
	tenant := tenancy.Tenant{ID: tenantID, Slug: "invoice-customer"}

	catalog, err := events.NewCatalog(appname.MustParse("beta"),
		[]events.Declared{events.Declare[invoiceTotal]("billing.invoice_issued")})
	if err != nil {
		t.Fatalf("app beta's composition was refused: %v", err)
	}
	t.Cleanup(catalog.Close)

	publish := func(body any) error {
		ctx := tenancy.WithTenant(t.Context(), tenant)
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return catalog.Publish(ctx, tx, "billing.invoice_issued", body)
		})
	}
	rows := func() int {
		var n int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1 AND name = 'billing.invoice_issued'`,
			tenantID).Scan(&n); err != nil {
			t.Fatalf("count app beta's rows: %v", err)
		}
		return n
	}

	if err := publish(map[string]any{"total": "not minor units"}); err == nil {
		t.Fatal("a payload app beta declared as minor units was committed for a tenant app acme holds")
	} else if !strings.Contains(err.Error(), `app "beta"`) {
		t.Errorf("the refusal names the wrong contract: %v", err)
	}
	if n := rows(); n != 0 {
		t.Fatalf("the refused publish left %d outbox rows, want none", n)
	}
	if err := publish(invoiceTotal{Total: 1200}); err != nil {
		t.Fatalf("the valid publish was refused: %v", err)
	}
	if n := rows(); n != 1 {
		t.Errorf("one valid publish left %d outbox rows, want one", n)
	}
}

// TestAPublishCarriedOnAContextIsMeasuredByTheCompositionThatCarriedIt is the carrier
// rather than the install: the same publish, on a context that carries the composition's
// door and on one that carries nothing, over one tenant whose row names no app at all.
// The carried one is accepted because the composition declared the name; the bare one is
// refused because the tenant's app — nobody — declared nothing. If the first half fails,
// the publisher never reached the write.
func TestAPublishCarriedOnAContextIsMeasuredByTheCompositionThatCarriedIt(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	t.Cleanup(func() { events.DeclareAll(nil) })
	tenantID := uuid.New()
	placeTenant(t, conn, tenantID, "unplaced-customer", "")
	tenant := tenancy.Tenant{ID: tenantID, Slug: "unplaced-customer"}

	catalog, err := events.NewCatalog(appname.MustParse("acme"),
		[]events.Declared{events.Declare[invoiceTotal]("billing.invoice_issued")})
	if err != nil {
		t.Fatalf("app acme's composition was refused: %v", err)
	}
	t.Cleanup(catalog.Close)

	publish := func(withCarrier bool) error {
		ctx := tenancy.WithTenant(t.Context(), tenant)
		if withCarrier {
			ctx = events.WithPublisher(ctx, catalog)
		}
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "billing.invoice_issued", invoiceTotal{Total: 4200})
		})
	}
	if err := publish(true); err != nil {
		t.Fatalf("the publish carried by its own composition was refused: %v", err)
	}
	err = publish(false)
	if err == nil {
		t.Fatal("an uncarried publish was accepted for a tenant no app holds: the install answered, not the carrier")
	}
	if !strings.Contains(err.Error(), "is declared by no module") {
		t.Errorf("the bare publish was refused for the wrong reason: %v", err)
	}
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
		t.Fatalf("count the tenant's rows: %v", err)
	}
	if n != 1 {
		t.Errorf("one accepted and one refused publish left %d rows, want the accepted one only", n)
	}
}
