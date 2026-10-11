package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

// sinkKeeper is a transport that routes nothing: it keeps the sink Consume
// hands it, so a case can deliver straight to it. What reaches a sink through a
// real broker is whatever its filters let through, and during the rollout window
// that includes the old, app-less addresses; this keeper stands for all of them.
type sinkKeeper struct{ sinks map[string]events.Sink }

func (k *sinkKeeper) Publish(context.Context, events.Event) error { return nil }

func (k *sinkKeeper) Subscribe(_ context.Context, durable, _ string, sink events.Sink) error {
	k.sinks[durable] = sink
	return nil
}

// TestADeliveryWhateverItsAddressRunsOnlyItsOwnAppsTenants is the delivery
// boundary with the address taken out of it: the brief requires delivery to
// refuse an event whose tenant is not its app's, before any handler transaction
// opens, old-format messages included. The sink is the last door, so an event
// naming another app's tenant, or a tenant nobody holds, is handed to it
// directly. It must run no handler, write no claim and ask for no redelivery,
// while the app's own tenant's event runs once however often it arrives.
func TestADeliveryWhateverItsAddressRunsOnlyItsOwnAppsTenants(t *testing.T) {
	_, conn := dbtest.Schema(t)

	own, foreign := uuid.New(), uuid.New()
	for _, tenant := range []struct {
		id        uuid.UUID
		slug, app string
	}{{own, "collect-shop", "collect"}, {foreign, "academy-school", "academy"}} {
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(
				`INSERT INTO tenants (id, slug, name, status, app) VALUES (?, ?, ?, 'active', ?)`,
				tenant.id, tenant.slug, tenant.slug, tenant.app).Error
		})
		if err != nil {
			t.Fatalf("place tenant %s in app %s: %v", tenant.slug, tenant.app, err)
		}
	}

	var ran []uuid.UUID
	keeper := &sinkKeeper{sinks: map[string]events.Sink{}}
	subs := []events.Subscription{{
		App: "collect", Module: "cart", Name: "cart.order.placed",
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			ran = append(ran, db.TenantOf(tx).ID)
			return nil
		},
	}}
	if err := events.Consume(t.Context(), conn, keeper, subs); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	durable := appname.Durable("collect", "cart", "cart.order.placed")
	sink, ok := keeper.sinks[durable]
	if !ok {
		t.Fatalf("Consume subscribed no sink under %q; it subscribed %v", durable, keeper.sinks)
	}

	claims := func() int {
		t.Helper()
		n := 0
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw(`SELECT count(*) FROM platformkit_handled WHERE durable = ?`, durable).Row().Scan(&n)
		})
		if err != nil {
			t.Fatalf("count the handled rows of %s: %v", durable, err)
		}
		return n
	}

	for _, refused := range []struct {
		why    string
		tenant uuid.UUID
	}{{"a tenant app academy holds", foreign}, {"a tenant no app holds", uuid.New()}} {
		ev := events.Event{ID: uuid.New(), Name: "cart.order.placed", TenantID: refused.tenant, Payload: []byte(`{}`)}
		if err := sink.Handle(t.Context(), ev); err != nil {
			t.Errorf("delivery naming %s answered %v; a refusal is final, not a redelivery", refused.why, err)
		}
		if len(ran) != 0 {
			t.Fatalf("app collect's handler ran in tenants %v for a delivery naming %s", ran, refused.why)
		}
		if n := claims(); n != 0 {
			t.Errorf("delivery naming %s wrote %d handled rows under %s; a refusal writes nothing", refused.why, n, durable)
		}
	}

	ev := events.Event{ID: uuid.New(), Name: "cart.order.placed", TenantID: own, Payload: []byte(`{}`)}
	for range 2 {
		if err := sink.Handle(t.Context(), ev); err != nil {
			t.Fatalf("delivery of app collect's own tenant's event: %v", err)
		}
	}
	if len(ran) != 1 || ran[0] != own {
		t.Errorf("app collect's own event ran in tenants %v, want once in %s", ran, own)
	}
	if n := claims(); n != 1 {
		t.Errorf("app collect's own event left %d handled rows under %s, want 1", n, durable)
	}
}
