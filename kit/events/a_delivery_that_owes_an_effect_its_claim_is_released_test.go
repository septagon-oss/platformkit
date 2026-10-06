package events

// A delivery's claim marks the delivery, not the commit. The case that separates the
// two is a handler that deferred an effect past its own commit — db.AfterCommit, which
// is how modules/auth hands a one-time link to a mail server without letting a rollback
// leave it in an inbox that opens nothing — and then could not carry that effect out.
// Its rows are committed, so nothing rolls the claim back; had the claim stood, every
// redelivery would have found it taken, skipped the handler, acked, and the row would
// have been stamped over an effect nobody ever ran. The claim is released instead.
//
// Both halves are under test here with the ladder shortened to milliseconds: a delivery
// that owes an effect is handed back and finishes, and one that never finishes ends as a
// dead letter an operator can read.

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	deliverypolicy "github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// oneDeferredEffect publishes one event to one subscription whose handler commits its
// claim and registers one deferred effect, and reports what the effect returned each
// time it ran. The tenant needs no row: an unnamed app holds what no app claims
// (holdsTenant), and the delivery's transaction is scoped by the id alone.
func oneDeferredEffect(t *testing.T, effect func(attempt int) error) (*sql.DB, *db.Conn, Transport, *atomic.Int64) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	t.Cleanup(stop)
	transport := memory.New()
	runs := new(atomic.Int64)
	err := Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: "billing.invoice_issued",
		Handler: func(ctx context.Context, _ db.Tx[db.Tenant], _ Event) error {
			n := runs.Add(1)
			return db.AfterCommit(ctx, func(context.Context) error { return effect(int(n)) })
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, "billing.invoice_issued", nil)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return admin, conn, transport, runs
}

func counts(t *testing.T, admin *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestADeliveryWhoseDeferredEffectDidNotRunIsHandledAgain: the transport's retry runs
// the handler again — not only the effect, which is what a released claim buys — and the
// delivery ends holding one claim and its row stamped.
func TestADeliveryWhoseDeferredEffectDidNotRunIsHandledAgain(t *testing.T) {
	fast(t)
	var effects atomic.Int64
	admin, conn, transport, runs := oneDeferredEffect(t, func(attempt int) error {
		effects.Add(1)
		if attempt == 1 {
			return errors.New("the transport was not listening")
		}
		return nil
	})
	if err := Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	if got := runs.Load(); got != 2 {
		t.Errorf("the handler ran %d times, want the refused delivery run again from the start", got)
	}
	if got := effects.Load(); got != 2 {
		t.Errorf("the effect ran %d times, want once refused and once delivered", got)
	}
	if got, want := counts(t, admin, `SELECT count(*) FROM platformkit_handled`), 1; got != want {
		t.Errorf("%d claims held for one event, want %d: the released claim rewritten once", got, want)
	}
	if got := counts(t, admin, `SELECT count(*) FROM platformkit_outbox WHERE published_at IS NOT NULL`); got != 1 {
		t.Errorf("%d outbox rows stamped by a delivery that finished, want 1", got)
	}
	if got := counts(t, admin, `SELECT count(*) FROM platformkit_dead_letters`); got != 0 {
		t.Errorf("%d dead letters for a delivery that recovered, want 0", got)
	}
}

// TestADeliveryThatNeverCarriedItsEffectOutIsDeadLettered is the other half: an effect
// that never succeeds must end somewhere a person can read. With the claim standing it
// was silent — deadLetter writes its row under the claim it inserts, the committed claim
// was already in the way, so the failure was swallowed, the row stamped and the effect
// lost with nothing left to review.
func TestADeliveryThatNeverCarriedItsEffectOutIsDeadLettered(t *testing.T) {
	fast(t)
	admin, conn, transport, runs := oneDeferredEffect(t, func(int) error {
		return errors.New("the transport is down for good")
	})
	if err := Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	if got := runs.Load(); got != deliverypolicy.MaxDeliveries {
		t.Errorf("the handler ran %d times, want the whole ladder of %d", got, deliverypolicy.MaxDeliveries)
	}
	if got := counts(t, admin, `SELECT count(*) FROM platformkit_dead_letters`); got != 1 {
		t.Errorf("%d dead letters for a delivery that never carried its effect, want the failure recorded", got)
	}
	if got := counts(t, admin, `SELECT count(*) FROM platformkit_handled`); got != 1 {
		t.Errorf("%d claims held with the dead letter, want the terminal claim it is written under", got)
	}
}
