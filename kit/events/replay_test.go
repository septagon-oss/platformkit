package events

// The operator's verb, end to end: a dead-lettered event is replayed, its claim
// and its dead letter are gone, the row is pending again, the handler that
// could not do the work now does it, and the act itself is in the outbox as an
// event — which is how the audit module comes to record an operator's decision
// in the tenant's trail without kit/events knowing that audit exists.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestReplayRunsADeadLetteredEventAgainAndRecordsTheAct(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	operator := uuid.New()
	name := "ledger.invoice_issued"
	broken := errors.New("the mailer was down")
	brokenNow := true

	transport := memory.New()
	var handled int
	err := Consume(ctx, conn, transport, []Subscription{{
		Module: "audit", Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			if brokenNow {
				return broken
			}
			handled++
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, name, map[string]string{"why": "the invoice went out"})
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

	// The operator's call: with a reason, under their own actor.
	rec, err := Replay(tenancy.WithActor(ctx, operator), conn, id, "", "the mailer is fixed; this invoice still has to go out")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if rec.Name != name || rec.EventID != id || rec.Reason == "" {
		t.Errorf("the receipt says %+v", rec)
	}

	// The claim and the terminal failure are gone, and the row is pending.
	var claims, dead int
	var pending bool
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_handled WHERE event_id=$1`, id).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_dead_letters WHERE event_id=$1`, id).Scan(&dead); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT published_at IS NULL FROM platformkit_outbox WHERE id=$1`, id).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if claims != 0 || dead != 0 || !pending {
		t.Fatalf("after a replay: %d claims, %d dead letters, pending=%v", claims, dead, pending)
	}

	// The relay carries it again, and this time the work is done.
	brokenNow = false
	if err := Relay(ctx, conn, transport); err != nil {
		t.Fatalf("relay after replay: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for handled != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if handled != 1 {
		t.Errorf("the replayed event never reached the handler again: it ran %d times", handled)
	}

	// And the act is an event in the tenant's own outbox, with the operator as
	// its actor: what the audit module records, it records as it records
	// everything else.
	var records int
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM platformkit_outbox WHERE name=$1 AND tenant_id=$2 AND actor=$3`,
		EventReplayed, tenant.ID, operator).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 1 {
		t.Errorf("the replay wrote %d records, want 1", records)
	}
	var payload string
	if err := admin.QueryRowContext(ctx,
		`SELECT payload::text FROM platformkit_outbox WHERE name=$1`, EventReplayed).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	// jsonb reformats, so the record is read as the payload type it is rather
	// than as bytes.
	var recorded ReplayRecord
	if err := json.Unmarshal([]byte(payload), &recorded); err != nil {
		t.Fatalf("the record is not a ReplayRecord: %v (%s)", err, payload)
	}
	if recorded.EventID != id || recorded.Name != name || recorded.Reason != "the mailer is fixed; this invoice still has to go out" {
		t.Errorf("the record says %+v", recorded)
	}
}

// The two refusals. A refused mutation writes nothing and emits nothing: the
// claims, the dead letter and the stamp all still say what they said before.
func TestReplayRefusalsWriteNothing(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	transport := memory.New()
	err := Consume(ctx, conn, transport, []Subscription{{
		Module: "audit", Name: "ledger.invoice_issued",
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			return errors.New("this will never work")
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, "ledger.invoice_issued", nil)
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

	t.Run("no reason", func(t *testing.T) {
		if _, err := Replay(ctx, conn, id, "", ""); err == nil {
			t.Fatal("a replay with no reason was accepted")
		} else if !strings.Contains(err.Error(), "reason") {
			t.Errorf("the error does not say what is missing: %v", err)
		}
	})
	t.Run("no outbox row", func(t *testing.T) {
		if _, err := Replay(ctx, conn, uuid.New(), "", "an id that does not exist"); !errors.Is(err, ErrNothingToReplay) {
			t.Errorf("Replay of an unknown id = %v, want ErrNothingToReplay", err)
		}
	})

	// Nothing moved: the claim, the dead letter and the stamp are as they were.
	var claims, dead int
	var pending bool
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_handled WHERE event_id=$1`, id).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_dead_letters WHERE event_id=$1`, id).Scan(&dead); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(ctx, `SELECT published_at IS NULL FROM platformkit_outbox WHERE id=$1`, id).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if claims != 1 || dead != 1 || pending {
		t.Errorf("a refused replay moved the state: %d claims, %d dead letters, pending=%v", claims, dead, pending)
	}
	var records int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE name=$1`, EventReplayed).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Errorf("a refused replay emitted %d records", records)
	}
}
