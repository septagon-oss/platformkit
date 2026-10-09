package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestDeletingAnEndpointKeepsTheOutcomeOfItsOpenRequest(t *testing.T) {
	var remove func() error
	deleted := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		deleted <- remove()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	e := newEnv(t, true, pool(server.Certificate()), nil)
	actor := uuid.New()
	endpoint, _ := e.register(acme, actor, server.URL, "task.assigned")
	remove = func() error {
		return e.inErr(acme, actor, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			if err := crud.Delete[*contracts.Endpoint](tx, endpoint.ID, true); err != nil {
				return err
			}
			// The endpoint Spec runs this hook in its delete transaction.
			return e.svc.DropQueued(tx, endpoint.ID, contracts.ReasonEndpointDeleted)
		})
	}
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("the initial request did not reach the receiver")
	}
	assertOutcome := func() {
		t.Helper()
		rows := e.deliveries(acme, endpoint.ID)
		if len(rows) != 1 {
			t.Fatalf("completed request has %d log rows, want one", len(rows))
		}
		if rows[0].State != contracts.StateSent || rows[0].StatusCode != http.StatusNoContent {
			t.Errorf("completed HTTP 204 lost from deleted endpoint's log: state=%s status=%d reason=%q",
				rows[0].State, rows[0].StatusCode, rows[0].Reason)
		}
	}
	assertOutcome()
	e.clock.Add(2 * time.Minute)
	e.drain(acme)
	assertOutcome()
}
