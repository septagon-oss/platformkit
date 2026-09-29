package events

// REVIEW round 1 (T-0109), finding 6.
//
// The brief opened with "dead letters lose their evidence". This delivery clears
// the `platformkit_handled` half of that with `Replay` and says so; the payload
// half is left on the old clock and nothing names it. `Purge` — untouched by
// this change — deletes every outbox row older than the seven-day keep window
// whose `published_at` is set. A dead-lettered event *was* relayed, so its
// `published_at` is set, so after a week the one copy of its payload is gone
// while `platformkit_dead_letters` and its claim remain, "until explicit
// operator review" (Purge's own comment, ADR 0004). From then on `Replay` answers
// `ErrNothingToReplay` — a refusal that is correct on its own terms and means the
// operator is looking at a terminal failure they can never run again, with no
// record of what it carried.
//
// A dead letter is the last account of a delivery, and rule 8 refuses the write
// that takes the last one away: the row has to be exempt while a dead letter (or
// any claim) still refers to it. Make `Purge` that one clause narrower, or state
// in ADR 0004 and the README that a replay has a seven-day fuse, and this passes.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestADeadLetterKeepsThePayloadItDescribes(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	transport := memory.New()
	if err := Consume(ctx, conn, transport, []Subscription{{
		Module: "review1", Name: "ledger.invoice_issued",
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			return errors.New("the mailer is down")
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, "ledger.invoice_issued", map[string]string{"why": "the invoice went out"})
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(ctx, conn, transport); err != nil {
		t.Fatalf("relay: %v", err)
	}
	var id uuid.UUID
	if err := admin.QueryRowContext(ctx, `SELECT event_id FROM platformkit_dead_letters LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("the event was never dead-lettered: %v", err)
	}

	// A week of queue latency: the row is relayed, so it is purgeable by the
	// rule as written, and the dead letter beside it is not.
	if _, err := admin.ExecContext(ctx,
		`UPDATE platformkit_outbox SET published_at = now() - interval '8 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := Purge(ctx, conn); err != nil {
		t.Fatalf("purge: %v", err)
	}

	var dead int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_dead_letters WHERE event_id=$1`, id).Scan(&dead); err != nil {
		t.Fatal(err)
	}
	if dead != 1 {
		t.Fatalf("the purge took the dead letter too: %d rows left", dead)
	}
	var rows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE id=$1`, id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("the purge deleted the outbox row of an event whose dead letter is still there to review: %d rows left, 1 dead letter", rows)
	}

	// The operator who finds that dead letter a week later gets a refusal that
	// blames the missing row instead of the purge that removed it.
	if _, err := Replay(ctx, conn, id, "", "the mailer is fixed"); errors.Is(err, ErrNothingToReplay) {
		t.Errorf("the replay is unreachable because the payload was purged: %v", err)
	} else if err != nil {
		t.Errorf("Replay: %v", err)
	}
}
