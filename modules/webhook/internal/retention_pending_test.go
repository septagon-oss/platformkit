package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
	"github.com/septagon-oss/platformkit/modules/webhook/internal"
)

func TestRetentionKeepsPendingAttemptsAndOtherTenantsHistory(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	foreign, _ := e.register(globex, uuid.New(), to.server.URL, "task.assigned")
	for range 3 {
		e.fanout(acme, event(acme, "task.assigned", e.now))
	}
	e.fanout(globex, event(globex, "task.assigned", e.now))
	rows := e.deliveries(acme, endpoint.ID)
	e.in(acme, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE webhook_deliveries SET state = CASE WHEN id = ? THEN 'sent' WHEN id = ? THEN 'in_flight' ELSE 'queued' END, updated_at = now() - interval '100 days'", rows[0].ID, rows[1].ID).Error
	})
	e.in(globex, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE webhook_deliveries SET state = 'sent', updated_at = now() - interval '100 days'").Error
	})
	job := internal.Retention(lister{acme}, 90)
	for range 2 {
		if err := job.Run(t.Context(), e.conn); err != nil {
			t.Fatal(err)
		}
	}
	kept := e.deliveries(acme, endpoint.ID)
	if len(kept) != 2 || kept[0].State != contracts.StateInFlight || kept[1].State != contracts.StateQueued {
		t.Errorf("retention changed pending rows: %v", state(kept))
	}
	if got := e.deliveries(globex, foreign.ID); len(got) != 1 {
		t.Errorf("retention removed another tenant's history: %v", state(got))
	}
}
