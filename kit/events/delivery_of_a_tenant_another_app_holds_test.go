package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestDeliveryRefusesAnEventWhoseTenantAnotherAppHolds is the boundary the brief
// names for delivery: "refuses an event whose tenant is not its app's, before any
// handler transaction opens". The tenant control plane now answers inside one app
// (migrations/000041_tenant_app, modules/tenant), so the fact the check needs —
// which app holds this tenant — is a column, and the durable is a consumer name
// the delivery already knows.
//
// Why the address check is not this check: transport.AddressMismatch compares the
// address the broker routed by, so it refuses the message whose *routing* belongs
// to another app. A message that arrives at app acme's own address naming a tenant
// app acme-billing holds passes it — the address says exactly what it can say. What
// it then does is open the handler's transaction as that tenant, under that
// tenant's row-level security, and run app acme's handler code over app
// acme-billing's rows. That is the owner's sentence — one app getting another
// app's tenants' events — reached from the side a filter cannot see.
//
// The refused delivery writes nothing: no handler call, no row in the handled
// ledger. A claim written for a delivery that was refused would make the event
// un-replayable for the app that owns it.
func TestDeliveryRefusesAnEventWhoseTenantAnotherAppHolds(t *testing.T) {
	_, conn := dbtest.Schema(t)

	acmeCustomer := uuid.New()
	billingCustomer := uuid.New()
	hold := func(id uuid.UUID, slug, app string) {
		t.Helper()
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(
				`INSERT INTO tenants (id, slug, name, status, app) VALUES (?, ?, ?, 'active', ?)`,
				id, slug, slug, app).Error
		})
		if err != nil {
			t.Fatalf("place tenant %s in app %s: %v", slug, app, err)
		}
	}
	hold(acmeCustomer, "acme-customer", "acme")
	hold(billingCustomer, "billing-customer", "acme-billing")

	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	// App acme's subscription to the module both apps compose.
	ran := make(chan uuid.UUID, 4)
	transport := memory.New()
	subs := []events.Subscription{{
		App: "acme", Module: "billing", Name: "billing.plan.created",
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			ran <- db.TenantOf(tx).ID
			return nil
		},
	}}
	if err := events.Consume(ctx, conn, transport, subs); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	// The event is app acme-billing's own: its tenant wrote it, and its own relay
	// pass (the claim that filters on tenants.app) publishes it.
	publish(t, conn, tenancy.Tenant{ID: billingCustomer, Slug: "billing-customer"}, "billing.plan.created", nil)
	if err := events.RelayApp(t.Context(), conn, transport, "acme-billing"); err != nil {
		t.Fatalf("relay for app acme-billing: %v", err)
	}

	select {
	case tenant := <-ran:
		t.Errorf("app acme's handler ran in tenant %s, which belongs to app acme-billing: delivery opened a handler transaction for a tenant this app does not hold", tenant)
	case <-time.After(3 * time.Second):
	}

	// And the refusal left nothing behind: a claim row here would mark another
	// app's event handled for this durable, and its own app's replay would then
	// find it already done.
	durable := appname.Durable("acme", "billing", "billing.plan.created")
	claims := 0
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_handled WHERE durable = ?`, durable).
			Row().Scan(&claims)
	})
	if err != nil {
		t.Fatalf("count the handled rows for %s: %v", durable, err)
	}
	if claims != 0 {
		t.Errorf("the refused delivery wrote %d handled rows under %s; a refusal writes nothing", claims, durable)
	}

	// The same delivery for the app that does hold the tenant still runs, so the
	// check above is a boundary and not a block: app acme-billing's own consumer
	// handles its own tenant's event in its own transaction.
	own := make(chan uuid.UUID, 4)
	billingSubs := []events.Subscription{{
		App: "acme-billing", Module: "billing", Name: "billing.plan.created",
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			own <- db.TenantOf(tx).ID
			return nil
		},
	}}
	if err := events.Consume(ctx, conn, transport, billingSubs); err != nil {
		t.Fatalf("Consume for app acme-billing: %v", err)
	}
	publish(t, conn, tenancy.Tenant{ID: billingCustomer, Slug: "billing-customer"}, "billing.plan.created", nil)
	if err := events.RelayApp(t.Context(), conn, transport, "acme-billing"); err != nil {
		t.Fatalf("relay of the second event: %v", err)
	}
	select {
	case tenant := <-own:
		if tenant != billingCustomer {
			t.Errorf("app acme-billing's handler ran in tenant %s, want its own tenant %s", tenant, billingCustomer)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("app acme-billing's own handler never ran for its own tenant's event")
	}
}
