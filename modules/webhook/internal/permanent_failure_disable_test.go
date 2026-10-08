package internal_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestThreeDaysOfPermanentFailuresDisableTheEndpoint(t *testing.T) {
	to := newReceiver(t, http.StatusNotFound)
	told := &notices{}
	e := newEnv(t, true, pool(to.server.Certificate()), told)
	admin := uuid.New()
	endpoint, _ := e.register(acme, admin, to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.clock.Now()))
	e.drain(acme)
	e.clock.Add(contracts.FaultWindow + time.Minute)
	// A new event proves that the endpoint has still not recovered. The old
	// event remains terminal: a permanent failure must not itself be retried.
	e.fanout(acme, event(acme, "task.assigned", e.clock.Now()))
	e.drain(acme)
	if got := e.endpointStatus(endpoint.ID); got != contracts.StatusDisabled {
		t.Errorf("endpoint answering 404 across three days is %s; log %v", got, state(e.deliveries(acme, endpoint.ID)))
	}
	if !slices.Contains(e.outboxNames(acme), contracts.EventEndpointDisabled) {
		t.Error("no endpoint-disabled event committed")
	}
	if !slices.Equal(told.told, []uuid.UUID{admin}) {
		t.Errorf("administrator notices = %v, want [%s]", told.told, admin)
	}
}
