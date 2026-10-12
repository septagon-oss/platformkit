package internal_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// TestAFailingNoticeDoesNotResendOutsideTheLadder: when the notice cannot be
// delivered, the attempt that crossed the line still has an outcome. It is not
// left in flight for the lease to send again a minute later, which would dial a
// dead receiver every lease for as long as the notifier is down.
func TestAFailingNoticeDoesNotResendOutsideTheLadder(t *testing.T) {
	to := newReceiver(t, http.StatusInternalServerError)
	told := &notices{fail: errors.New("the notifier is down")}
	e := newEnv(t, true, pool(to.server.Certificate()), told)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)

	e.clock.Add(contracts.FaultWindow + time.Hour)
	e.drain(acme)
	crossed := to.count()

	// The drain's lease is one minute; the ladder's next rung after attempt 2 is
	// five. Two minutes later nothing is due.
	e.clock.Add(2 * time.Minute)
	e.drain(acme)
	if to.count() != crossed {
		t.Fatalf("the receiver was dialled %d more times inside one rung; log %v",
			to.count()-crossed, state(e.deliveries(acme, endpoint.ID)))
	}
	for _, row := range e.deliveries(acme, endpoint.ID) {
		if row.State == contracts.StateInFlight {
			t.Errorf("an attempt that was answered has no outcome: %v", state(e.deliveries(acme, endpoint.ID)))
		}
	}
}
