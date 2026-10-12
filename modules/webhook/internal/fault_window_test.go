package internal_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// notices is the Alerts port as a recorder, failing when told to.
type notices struct {
	mu   sync.Mutex
	told []uuid.UUID
	fail error
}

func (n *notices) EndpointDisabled(_ context.Context, _ db.Tx[db.Tenant], e *contracts.Endpoint) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.fail != nil {
		return n.fail
	}
	n.told = append(n.told, e.CreatedBy)
	return nil
}

func (e *env) endpointStatus(endpoint uuid.UUID) string {
	e.t.Helper()
	var status string
	e.in(acme, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT status FROM webhook_endpoints WHERE id = ?", endpoint).Row().Scan(&status)
	})
	return status
}

// TestAnEndpointFailingForThreeDaysIsDisabledAndItsAdministratorTold: the
// failure that crosses the three-day line disables the endpoint, drops what was
// queued, publishes the disablement and tells the person who registered it.
func TestAnEndpointFailingForThreeDaysIsDisabledAndItsAdministratorTold(t *testing.T) {
	to := newReceiver(t, http.StatusInternalServerError)
	told := &notices{}
	e := newEnv(t, true, pool(to.server.Certificate()), told)
	admin := uuid.New()
	endpoint, _ := e.register(acme, admin, to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)

	e.clock.Add(contracts.FaultWindow + time.Hour)
	e.drain(acme)

	if got := e.endpointStatus(endpoint.ID); got != contracts.StatusDisabled {
		t.Fatalf("an endpoint failing for three days is %s; log %v", got, state(e.deliveries(acme, endpoint.ID)))
	}
	for _, row := range e.deliveries(acme, endpoint.ID) {
		if row.State == contracts.StateQueued || row.State == contracts.StateInFlight {
			t.Errorf("the disabled endpoint keeps a pending attempt: %v", state(e.deliveries(acme, endpoint.ID)))
		}
	}
	if !slices.Equal(told.told, []uuid.UUID{admin}) {
		t.Errorf("the notice went to %v, want the registering administrator %s", told.told, admin)
	}
	if !slices.Contains(e.outboxNames(acme), contracts.EventEndpointDisabled) {
		t.Errorf("the disablement was not published: %v", e.outboxNames(acme))
	}
}
