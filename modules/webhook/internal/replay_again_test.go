package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// TestADeliveredReplayCanBeReplayedAgain: "an administrator can replay a
// delivery". A replay that has already gone out is not "an attempt already
// waiting"; asking again must queue a new attempt and send it, not answer with
// the finished row from last time.
func TestADeliveredReplayCanBeReplayedAgain(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	first := e.deliveries(acme, endpoint.ID)[0]

	replay := func() *contracts.Delivery {
		var row *contracts.Delivery
		e.in(acme, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			row, err = e.svc.Replay(ctx, tx, first.ID)
			return err
		})
		return row
	}

	once := replay()
	e.drain(acme)
	if to.count() != 2 {
		t.Fatalf("the first replay was not sent: %d requests, log %v", to.count(), state(e.deliveries(acme, endpoint.ID)))
	}

	twice := replay()
	if twice.ID == once.ID {
		t.Errorf("the second replay answered with the first replay's row, state %s", twice.State)
	}
	if twice.State != contracts.StateQueued {
		t.Errorf("the second replay answered with a row in state %s", twice.State)
	}
	e.drain(acme)
	if to.count() != 3 {
		t.Fatalf("the second replay was never sent: %d requests, log %v", to.count(), state(e.deliveries(acme, endpoint.ID)))
	}
}
