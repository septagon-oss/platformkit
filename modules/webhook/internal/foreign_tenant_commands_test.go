package internal_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// TestAnotherTenantsCommandsFindNothing: every command a tenant can send names
// an id, and an id of another tenant's endpoint or delivery is not found — the
// row is unchanged, nothing is queued and nothing is published in either tenant.
func TestAnotherTenantsCommandsFindNothing(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	delivery := e.deliveries(acme, endpoint.ID)[0]
	acmeBefore, globexBefore := e.outboxNames(acme), e.outboxNames(globex)

	commands := map[string]func(ctx context.Context, tx db.Tx[db.Tenant]) error{
		"disable": func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Disable(ctx, tx, endpoint.ID)
			return err
		},
		"target": func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Target(ctx, tx, endpoint.ID, to.server.URL, contracts.EventNames{"task.resolved"})
			return err
		},
		"rotate": func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Rotate(ctx, tx, endpoint.ID)
			return err
		},
		"replay": func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Replay(ctx, tx, delivery.ID)
			return err
		},
	}
	for name, command := range commands {
		if err := e.inErr(globex, uuid.New(), command); !isNotFound(err) {
			t.Errorf("globex's %s of acme's row answered %v, want not found", name, err)
		}
	}

	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 1 || rows[0].State != contracts.StateQueued {
		t.Errorf("acme's log changed under globex's commands: %v", state(rows))
	}
	var status string
	var events []string
	e.in(acme, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT status FROM webhook_endpoints WHERE id = ?", endpoint.ID).Row().Scan(&status)
	})
	e.in(acme, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT unnest(events) FROM webhook_endpoints WHERE id = ?", endpoint.ID).Scan(&events).Error
	})
	if status != contracts.StatusEnabled || !slices.Equal(events, []string{"task.assigned"}) {
		t.Errorf("acme's endpoint reads %s %v after globex's commands", status, events)
	}
	if got := e.outboxNames(acme); !slices.Equal(got, acmeBefore) {
		t.Errorf("acme's outbox moved: %v against %v", got, acmeBefore)
	}
	if got := e.outboxNames(globex); !slices.Equal(got, globexBefore) {
		t.Errorf("globex's refused commands published %v", got)
	}
}

// TestAReplayedOutboxEventQueuesOnce: the outbox handler run twice with one event
// id leaves one initial attempt per endpoint.
func TestAReplayedOutboxEventQueuesOnce(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	ev := event(acme, "task.assigned", e.now)
	e.fanout(acme, ev)
	e.fanout(acme, ev)
	if rows := e.deliveries(acme, endpoint.ID); len(rows) != 1 {
		t.Fatalf("one event id queued %v", state(rows))
	}
	e.drain(acme)
	e.fanout(acme, ev)
	e.drain(acme)
	if to.count() != 1 {
		t.Fatalf("a replayed event id after delivery was sent %d times", to.count())
	}
}
