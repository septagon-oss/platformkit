package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestChangingDestinationStopsUnsentRequestsToThePreviousAddress(t *testing.T) {
	var oldRequests, newRequests atomic.Int32
	var target func() error
	changed := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			if oldRequests.Add(1) == 1 {
				// Commit the new destination before the first response releases
				// the sender to start the remaining members of its batch.
				changed <- target()
			}
		case "/new":
			newRequests.Add(1)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	e := newEnv(t, true, pool(server.Certificate()), nil)
	actor := uuid.New()
	endpoint, _ := e.register(acme, actor, server.URL+"/old", "task.assigned")
	target = func() error {
		return e.inErr(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := e.svc.Target(ctx, tx, endpoint.ID, server.URL+"/new", contracts.EventNames{"task.assigned"})
			return err
		})
	}
	for range 3 {
		e.fanout(acme, event(acme, "task.assigned", e.now))
	}
	e.drain(acme)
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("no first request reached the registered destination")
	}
	if got := oldRequests.Load(); got != 1 {
		t.Errorf("old destination received %d requests after Target committed, want 0; new destination received %d", got-1, newRequests.Load())
	}
}
