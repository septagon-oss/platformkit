package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// TestADisabledEndpointHearsNothingMore: "Disable stops delivery now". An
// administrator disables an endpoint while one attempt to it is on the wire;
// that attempt answers 500. The outcome must not queue a retry to an endpoint
// that is disabled, and the next tick must not dial it.
func TestADisabledEndpointHearsNothingMore(t *testing.T) {
	var (
		mu       sync.Mutex
		requests int
		disable  func() error
		failure  error
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		first := requests == 1
		mu.Unlock()
		if first {
			// The administrator's click lands while this request is open.
			if err := disable(); err != nil {
				mu.Lock()
				failure = err
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	e := newEnv(t, true, pool(server.Certificate()), nil)
	admin := uuid.New()
	endpoint, _ := e.register(acme, admin, server.URL, "task.assigned")
	disable = func() error {
		ctx := tenancy.WithActor(tenancy.WithTenant(context.Background(), acme), admin)
		return db.Run(ctx, e.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Disable(ctx, tx, endpoint.ID)
			return err
		})
	}

	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	mu.Lock()
	if failure != nil {
		t.Fatalf("disabling during the send: %v", failure)
	}
	mu.Unlock()

	for _, row := range e.deliveries(acme, endpoint.ID) {
		if row.State == contracts.StateQueued {
			t.Errorf("a disabled endpoint has a queued attempt: %v", state(e.deliveries(acme, endpoint.ID)))
		}
	}

	e.clock.Add(time.Hour)
	e.drain(acme)
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Fatalf("a disabled endpoint was dialled %d times after it was disabled; log %v",
			requests-1, state(e.deliveries(acme, endpoint.ID)))
	}
}
