package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

func TestMovingADeliveryLedgerDoesNotAcknowledgeAnOwedEffect(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := deferredTenant(t, conn)
	event := events.Event{ID: uuid.New(), Name: "cart.order.placed", TenantID: tenant}
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	sink := deferredSink(t, conn, event, func(ctx context.Context, _ db.Tx[db.Tenant], _ events.Event) error {
		return events.Defer(ctx, func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})
	var firstErr error
	done := make(chan struct{})
	go func() {
		firstErr = sink.Handle(t.Context(), event)
		close(done)
	}()
	defer func() { unblock(); <-done }()
	waitEntered(t, entered, "the effect after its claim committed")

	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("UPDATE tenants SET app = 'collect' WHERE id = ?", tenant).Error
	}); err != nil {
		t.Fatal(err)
	}
	_, moveErr := events.MoveLedger(t.Context(), conn, "collect", "place the deployment")
	if moveErr != nil && !errors.Is(moveErr, events.ErrLedgerMoveContended) {
		t.Fatal(moveErr)
	}
	keeper := &sinkKeeper{sinks: map[string]events.Sink{}}
	sub := events.Subscription{App: "collect", Module: "cart", Name: event.Name,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error {
			t.Error("the scoped delivery reran a handler whose effect was already owned")
			return errors.New("the original handler already owns the effect")
		},
	}
	if err := events.Consume(t.Context(), conn, keeper, []events.Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	scoped := keeper.sinks[appname.Durable(sub.App, sub.Module, sub.Name)]
	event.App = sub.App
	if err := scoped.Handle(t.Context(), event); !errors.Is(err, events.ErrDeliveryOwesEffect) {
		t.Errorf("redelivery after ledger move = %v, want refusal while the effect is unfinished", err)
	}

	unblock()
	<-done
	if firstErr != nil {
		t.Fatalf("the original effect completed: %v", firstErr)
	}
	if moveErr != nil {
		if _, err := events.MoveLedger(t.Context(), conn, sub.App, "the deferred effect finished"); err != nil {
			t.Fatal(err)
		}
	}
	if err := scoped.Handle(t.Context(), event); err != nil {
		t.Errorf("redelivery after the effect finished = %v, want success without running it again", err)
	}
}
