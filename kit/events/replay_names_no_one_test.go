package events

// A replay names who authorised it — and a missing actor is a refusal, not a NULL.
//
// replay.go names what the brief asked for — "under whose authority, audited" —
// and then enforces only half of it. A reason is required before the
// transaction opens; the actor is read out of the context by the one INSERT
// every event passes through and, when the context names nobody, is stored as
// NULL. So the same call that refuses to act without a sentence is happy to act
// without an actor, and the trail row it writes — the record the delivery
// offers as the audit answer (pillar line 3: actor, tenant, what) — can name
// nobody. ADR 0006 is explicit that "the capability is not the authorization",
// and rule 9 asks a command to recheck the actor inside its own authoritative
// transaction: this command opens that transaction, holds a capability of its
// own, deletes the rows that say a consequential action finished, and asks
// nothing of the person asking.
//
// The fix is one check beside the reason check, in the same place, for the same
// reason. It refuses before the transaction opens, so the claims, the dead
// letter and the stamp are untouched and no record is published — every
// assertion below is the state after a refusal, which is the state before it.

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

func TestAReplayWithNoActorIsRefused(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	transport := memory.New()
	if err := Consume(ctx, conn, transport, []Subscription{{
		Module: "audit", Name: "ledger.invoice_issued",
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

	// The operator's sentence, and no operator.
	if _, err := Replay(ctx, conn, id, "", "the mailer is fixed"); err == nil {
		t.Error("Replay cleared a terminal claim for a context that names no actor")
	} else if errors.Is(err, ErrNothingToReplay) {
		t.Errorf("the refusal is the wrong one: %v", err)
	}

	// A refused mutation writes nothing and emits nothing.
	var claims, dead, records int
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
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE name=$1`, EventReplayed).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if claims != 1 || dead != 1 || pending {
		t.Errorf("the refused replay moved the state: %d claims, %d dead letters, pending=%v", claims, dead, pending)
	}
	if records != 0 {
		t.Errorf("the refused replay emitted %d records", records)
	}

	// And the same call, with the actor the request carried, is allowed: the
	// refusal is about the missing actor and nothing else.
	operator := uuid.New()
	if _, err := Replay(tenancy.WithActor(ctx, operator), conn, id, "", "the mailer is fixed"); err != nil {
		t.Fatalf("Replay with an actor: %v", err)
	}
	var named int
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM platformkit_outbox WHERE name=$1 AND actor=$2`, EventReplayed, operator).Scan(&named); err != nil {
		t.Fatal(err)
	}
	if named != 1 {
		t.Errorf("the accepted replay wrote %d records naming the operator, want 1", named)
	}
}
