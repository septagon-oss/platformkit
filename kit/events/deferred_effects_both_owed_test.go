package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

// A delivery that defers two effects still owes the second while only the first has
// run: a redelivery arriving between them must not acknowledge.
func TestADeliveryWithTwoDeferredEffectsIsOwedUntilBothRan(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := deferredTenant(t, conn)
	event := events.Event{ID: uuid.New(), Name: "cart.order.placed", TenantID: tenant}

	slow := &deferredEffect{entered: make(chan struct{}), release: make(chan error, 1)}
	sink := deferredSink(t, conn, event, func(ctx context.Context, _ db.Tx[db.Tenant], _ events.Event) error {
		if err := events.Defer(ctx, func(context.Context) error { return nil }); err != nil {
			return err
		}
		return events.Defer(ctx, slow.action())
	})

	first := make(chan error, 1)
	go func() { first <- sink.Handle(t.Context(), event) }()
	waitEntered(t, slow.entered, "the second deferred effect to be reached")

	err := sink.Handle(t.Context(), event)
	if err == nil || !errors.Is(err, events.ErrDeliveryOwesEffect) {
		t.Errorf("a redelivery while the second effect was unfinished returned %v, want the owed-effect refusal", err)
	}
	slow.release <- nil
	if err := <-first; err != nil {
		t.Fatalf("first delivery: %v", err)
	}
}
