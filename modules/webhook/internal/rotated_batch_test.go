package internal_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestRotationSignsUnsentBatchMembersWithTheCurrentSecret(t *testing.T) {
	var requests, rejected atomic.Int32
	var secret atomic.Value
	var rotate func() error
	rotated := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if requests.Add(1) == 1 {
			// Rotation commits while the first request is on the wire. Its
			// signature can use the old key; later requests must use the new one.
			rotated <- rotate()
		} else if !verify(t, secret.Load().(string), r.Header.Get("webhook-id"),
			r.Header.Get("webhook-timestamp"), body, r.Header.Get("webhook-signature")) {
			rejected.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	e := newEnv(t, true, pool(server.Certificate()), nil)
	actor := uuid.New()
	endpoint, oldSecret := e.register(acme, actor, server.URL, "task.assigned")
	secret.Store(oldSecret)
	rotate = func() error {
		var issued *contracts.Issued
		err := e.inErr(acme, actor, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			issued, err = e.svc.Rotate(ctx, tx, endpoint.ID)
			return err
		})
		if err == nil {
			secret.Store(issued.Secret)
		}
		return err
	}
	for range 3 {
		e.fanout(acme, event(acme, "task.assigned", e.now))
	}
	e.drain(acme)
	select {
	case err := <-rotated:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("no request reached the receiver to trigger rotation")
	}
	if got := rejected.Load(); got != 0 {
		t.Errorf("receiver rejected %d requests started after rotation committed: signatures do not verify with the current secret; log %v", got, state(e.deliveries(acme, endpoint.ID)))
	}
	for _, row := range e.deliveries(acme, endpoint.ID) {
		if row.State != contracts.StateSent {
			t.Errorf("rotation lost delivery %s: state=%s status=%d", row.ID, row.State, row.StatusCode)
		}
	}
}
