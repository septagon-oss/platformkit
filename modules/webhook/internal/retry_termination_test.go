package internal_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestTheFinalFailedAttemptCommitsItsOutcome(t *testing.T) {
	to := newReceiver(t, http.StatusInternalServerError)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	// Start at the final rung; earlier attempts are immaterial to recording it.
	e.in(acme, uuid.Nil, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE webhook_deliveries SET attempt = ? WHERE endpoint_id = ?", contracts.MaxAttempts, endpoint.ID).Error
	})
	e.drain(acme)
	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 1 || rows[0].State != contracts.StateFailed || rows[0].StatusCode != http.StatusInternalServerError || rows[0].Reason == "" {
		t.Errorf("final failure was not committed: %v", state(rows))
	}
	e.clock.Add(2 * time.Minute)
	e.drain(acme)
	to.mu.Lock()
	defer to.mu.Unlock()
	if to.requests != 1 {
		t.Errorf("exhausted attempt sent %d times; want one recorded terminal failure", to.requests)
	}
}
