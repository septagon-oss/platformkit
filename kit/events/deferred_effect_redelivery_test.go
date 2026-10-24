package events_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

// A claim marks a delivery handled. An effect that waits for the delivery's own
// commit (events.Defer, because it cannot be a row) is not finished when that claim
// commits, so the claim keeps that fact too — and a redelivery that arrives while
// the first attempt still owes its effect must not acknowledge it.

// deferredEffect is the rendezvous one case shares with the effect it drives: the
// handler says when the effect was reached, and the test says when it may finish.
type deferredEffect struct {
	entered chan struct{}
	release chan error
}

func (e *deferredEffect) action() func(context.Context) error {
	return func(context.Context) error {
		e.entered <- struct{}{}
		return <-e.release
	}
}

func TestARedeliveryCannotAcknowledgeAnUnfinishedDeferredEffect(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := deferredTenant(t, conn)
	event := events.Event{ID: uuid.New(), Name: "cart.order.placed", TenantID: tenant}

	ran := make(chan int, 4)
	effect := &deferredEffect{entered: make(chan struct{}), release: make(chan error, 1)}
	sink := deferredSink(t, conn, event, func(ctx context.Context, _ db.Tx[db.Tenant], _ events.Event) error {
		ran <- 1
		return events.Defer(ctx, effect.action())
	})

	// Attempt one, as far as its commit. The effect is in the air from here, and the
	// rows — the claim and its owed mark — are in too.
	first := make(chan error, 1)
	go func() { first <- sink.Handle(t.Context(), event) }()
	waitEntered(t, effect.entered, "the deferred effect to be reached")

	// A redelivery on the broker's first rung, while that effect is still owed.
	if err := sink.Handle(t.Context(), event); err == nil {
		t.Fatal("redelivery acknowledged success while the first deferred effect was unfinished and no effect had succeeded")
	} else if !errors.Is(err, events.ErrDeliveryOwesEffect) {
		t.Errorf("the redelivery refused with %v, want the delivery's own owed-effect answer", err)
	}

	// The effect succeeds. The delivery that owed it is the only one that can say so.
	effect.release <- nil
	if err := <-first; err != nil {
		t.Fatalf("a delivery whose effect succeeded returned %v", err)
	}
	if n := handled(t, admin, "effects_owed = 0"); n != 1 {
		t.Errorf("%d claims left counting themselves owed nothing, want the one that finished", n)
	}

	// A third arrival finds the mark clear: it runs no handler and acknowledges.
	if err := sink.Handle(t.Context(), event); err != nil {
		t.Errorf("a redelivery of a finished delivery refused with %v, want it skipped and acked", err)
	}
	if n := len(ran); n != 1 {
		t.Errorf("the handler ran %d times, want once for the delivery that did the work", n)
	}
}

func TestADeliveryWhoseDeferredEffectFailedIsHandledAgain(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := deferredTenant(t, conn)
	event := events.Event{ID: uuid.New(), Name: "cart.order.placed", TenantID: tenant}

	ran := 0
	refused := errors.New("the mail transport refused")
	effect := &deferredEffect{entered: make(chan struct{}), release: make(chan error, 1)}
	sink := deferredSink(t, conn, event, func(ctx context.Context, _ db.Tx[db.Tenant], _ events.Event) error {
		ran++
		if ran == 1 {
			return events.Defer(ctx, effect.action())
		}
		// The retry's own effect: the transport took the message, which is all the
		// delivery still owed.
		return events.Defer(ctx, func(context.Context) error { return nil })
	})

	first := make(chan error, 1)
	go func() { first <- sink.Handle(t.Context(), event) }()
	waitEntered(t, effect.entered, "the deferred effect to be reached")
	effect.release <- refused

	if err := <-first; !errors.Is(err, db.ErrEffectNotRun) {
		t.Fatalf("a delivery whose effect failed returned %v, want it said the effect did not run", err)
	}
	if n := handled(t, admin, "true"); n != 0 {
		t.Fatalf("%d claims left behind by a delivery that owes an effect, want none: the next arrival would skip the handler and ack", n)
	}

	// The transport's next delivery is a first delivery again, and this time the
	// effect goes through.
	if err := sink.Handle(t.Context(), event); err != nil {
		t.Fatalf("the redelivery of a released claim: %v", err)
	}
	if ran != 2 {
		t.Errorf("the handler ran %d times, want the released delivery run again from the start", ran)
	}
}

// deferredSink consumes one subscription onto the keeper transport and returns the
// sink it holds, so a case drives deliveries itself rather than through a broker.
func deferredSink(t *testing.T, conn *db.Conn, ev events.Event, h events.Handler) events.Sink {
	t.Helper()
	keeper := &sinkKeeper{sinks: map[string]events.Sink{}}
	sub := events.Subscription{Module: "cart", Name: ev.Name, Handler: h}
	if err := events.Consume(t.Context(), conn, keeper, []events.Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	sink, ok := keeper.sinks[appname.Durable("", sub.Module, ev.Name)]
	if !ok {
		t.Fatalf("no sink was subscribed under the durable of %s", ev.Name)
	}
	return sink
}

// deferredTenant creates one tenant nobody's app holds, which is the deployment an
// app-less subscription delivers for.
func deferredTenant(t *testing.T, conn *db.Conn) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO tenants (id, slug, name, status) VALUES (?, ?, ?, 'active')`,
			id, id.String()[:8], "deferred").Error
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func waitEntered(t *testing.T, entered <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatalf("gave up waiting for %s", what)
	}
}

// handled counts this ledger's claims whose mark answers the predicate.
func handled(t *testing.T, admin *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_handled WHERE " + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
