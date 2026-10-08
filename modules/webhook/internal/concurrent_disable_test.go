package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// TestTwoDisablesOfOneEndpointPublishOnce: two administrators disable the same
// endpoint at once. The row lock settles them in order; the second finds the
// endpoint already disabled and publishes nothing.
func TestTwoDisablesOfOneEndpointPublishOnce(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			errs <- e.inErr(acme, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := e.svc.Disable(ctx, tx, endpoint.ID)
				return err
			})
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("a concurrent disable failed: %v", err)
		}
	}
	disabled := 0
	for _, name := range e.outboxNames(acme) {
		if name == contracts.EventEndpointDisabled {
			disabled++
		}
	}
	if disabled != 1 {
		t.Fatalf("two disables published %d disablements: %v", disabled, e.outboxNames(acme))
	}
}
