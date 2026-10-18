package internal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// The other half of a delete landing on an open request: the attempt the receiver
// refused is recorded as the answer it gave, and the ladder ends there because
// there is no endpoint left to own a retry. A queued successor would be claimed,
// read, found addressed to nothing, and ended — a row saying less about what
// happened than the row already written, one tick later.
func TestDeletingAnEndpointEndsTheLadderWithTheAnswerItGot(t *testing.T) {
	var remove func() error
	answered := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answered <- remove()
		w.WriteHeader(http.StatusInternalServerError)
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
			return e.svc.DropQueued(tx, endpoint.ID, contracts.ReasonEndpointDeleted)
		})
	}
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 1 {
		t.Fatalf("the deleted endpoint's attempt has %d log rows, want the one answer", len(rows))
	}
	if rows[0].State != contracts.StateFailed || rows[0].StatusCode != http.StatusInternalServerError {
		t.Fatalf("the receiver's 500 is missing from the deleted endpoint's log: state=%s status=%d reason=%q",
			rows[0].State, rows[0].StatusCode, rows[0].Reason)
	}
	if !strings.Contains(rows[0].Reason, contracts.ReasonEndpointDeleted) {
		t.Errorf("the last attempt of a deleted endpoint does not say so: reason=%q", rows[0].Reason)
	}
	e.clock.Add(2 * time.Minute)
	e.drain(acme)
	if after := e.deliveries(acme, endpoint.ID); len(after) != 1 {
		t.Errorf("a deleted endpoint queued %d further attempts, want none: %v", len(after)-1, state(after[1:]))
	}
}
