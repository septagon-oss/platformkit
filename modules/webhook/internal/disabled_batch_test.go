package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
)

func TestDisablingAnEndpointStopsUnsentMembersOfAClaimedBatch(t *testing.T) {
	var requests atomic.Int32
	var disable func() error
	failures := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			// The first response waits until the administrator's disable commits.
			failures <- disable()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	e := newEnv(t, true, pool(server.Certificate()), nil)
	actor := uuid.New()
	endpoint, _ := e.register(acme, actor, server.URL, "task.assigned")
	disable = func() error {
		return e.inErr(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Disable(ctx, tx, endpoint.ID)
			return err
		})
	}
	for range 3 {
		e.fanout(acme, event(acme, "task.assigned", e.now))
	}
	e.drain(acme)
	select {
	case err := <-failures:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("the first delivery did not reach the receiver")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("%d POSTs started after disable committed; log %v", got-1, state(e.deliveries(acme, endpoint.ID)))
	}
}
