package events

// A reviewer's pin (review round 6, task T-0109).
//
// b7ad506 taught the NATS provider to read the address a message arrived at and
// to terminate a delivery whose document does not claim it. Round 5's pin
// asserts the half about the handler: it must not run inside the second tenant's
// rows. Nothing asserts the other half — what a refusal *writes*. This package's
// own README, replay.go and rule 9 all say a refusal writes nothing, and the
// branch beside the new one (`sink.Dead` then `Term`, as the undecodable-message
// branch does) is a fix shape that would keep round 5's pin green while writing
// two rows: a `platformkit_handled` claim and a `platformkit_dead_letters` row,
// both stamped with the tenant that only the *body* claimed, filed against the
// tenant whose address the message was thrown at. A refusal that leaves forged,
// cross-tenant evidence in both tenants' tables is not a refusal that wrote
// nothing, and `Replay` would then find a terminal record for work no handler
// ever did.
//
// So the case is paired. The correctly addressed control must leave exactly one
// claim row naming its tenant — that is what makes the forged event's absence
// mean "this refusal wrote nothing" rather than "claims are not written here at
// all", and it is the reachability probe: it never asks the refusal's leave. The
// delivery itself is then read for what it may and may not record: round 5 left
// two correct outcomes open (refuse the mismatch, or run scoped to the address),
// and this pin holds the rule those two share — no row in either table may name a
// tenant the message's address does not, and a message whose handler never ran
// has no terminal record beside it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestADeliveryRefusedForItsAddressRecordsNoTenantItsAddressDoesNotName(t *testing.T) {
	broker, js := jetstreamForTest(t)
	admin, conn := dbtest.Schema(t)

	tenantA, tenantB := uuid.New(), uuid.New()
	_, name := uniqueDurable(t)
	// Consume names its consumer after the subscription, not after uniqueDurable's
	// guess, so the cleanup has to know the real name.
	consumer := Subscription{Module: "drift", Name: name}.durable()
	t.Cleanup(func() {
		nc, err := nats.Connect(os.Getenv("PLATFORMKIT_TEST_NATS_URL"))
		if err != nil {
			return
		}
		defer nc.Close()
		if js, err := nc.JetStream(); err == nil {
			_ = js.DeleteConsumer(stream, consumer)
		}
	})

	seen := make(chan Event, 8)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	if err := Consume(ctx, conn, broker, []Subscription{{
		Module: "drift", Name: name,
		Handler: func(_ context.Context, _ db.Tx[db.Tenant], ev Event) error {
			seen <- ev
			return nil
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	// store publishes one document, self-consistent in every way the envelope
	// checks — subject, source, tenantid and type agree with each other — and
	// reads it back off the broker: both messages below are published at tenant
	// A's address, so the second one is addressed at A while its body names B.
	subjectA := transport.Subject(tenantA, name)
	store := func(claims uuid.UUID) uuid.UUID {
		id := uuid.New()
		body, err := json.Marshal(Event{ID: id, Name: name, TenantID: claims,
			Payload: json.RawMessage(`{}`), At: time.Now().UTC()})
		if err != nil {
			t.Fatalf("marshal the envelope for %s: %v", claims, err)
		}
		ack, err := js.Publish(subjectA, body)
		if err != nil {
			t.Fatalf("publish at %q: %v", subjectA, err)
		}
		stored, err := js.GetLastMsg(stream, subjectA)
		if err != nil {
			t.Fatalf("read back what is stored at %q: %v", subjectA, err)
		}
		if stored.Sequence != ack.Sequence || stored.Subject != subjectA {
			t.Fatalf("the last message at %q is not the one published: seq %d subject %q, want seq %d",
				subjectA, stored.Sequence, stored.Subject, ack.Sequence)
		}
		if got := tenantOfDocument(t, stored.Data); got != claims {
			t.Fatalf("the stored document names tenant %s, not the %s it was made to name", got, claims)
		}
		return id
	}

	controlID := store(tenantA) // addressed at A, stamped as A: this one must be handled
	forgedID := store(tenantB)  // addressed at A, stamped as B: this one must not be handled as B's

	// The probe: the control's delivery, on the same subscription and the same
	// address, proves the consumer is live and that claims are written here.
	var control Event
	select {
	case control = <-seen:
	case <-time.After(10 * time.Second):
		t.Fatalf("the correctly addressed message %s was never handled; with no live delivery nothing below means anything", controlID)
	}
	if control.ID != controlID {
		t.Fatalf("the first delivery was %s, not the control %s", control.ID, controlID)
	}
	// Any further delivery on this subscription is the mismatched message being
	// handled after all. Which tenant that would be is the question the two
	// counts below answer, so it is recorded rather than failed here: a
	// delivery scoped to the address is one of the outcomes round 5 accepted.
	select {
	case late := <-seen:
		t.Logf("the delivery ran the handler for the mismatched message %s (address %s, body %s)", late.ID, tenantA, tenantB)
	case <-time.After(2 * time.Second):
	}

	// Claims are written here at all — the control's own, naming its tenant.
	if n := countClaims(t, admin, handled, controlID); n != 1 {
		t.Fatalf("platformkit_handled holds %d rows for the control %s, want 1: without the control's claim the forged event's absence says nothing", n, controlID)
	}
	if n, tenant := claimTenant(t, admin, controlID); n != 1 || tenant != tenantA {
		t.Fatalf("the control's claim names tenant %s (%d rows), want %s", tenant, n, tenantA)
	}

	// The refusal's own ledger: no claim and no terminal record may name the
	// tenant only the body claimed, and nothing may be recorded as terminally
	// failed when no handler ran.
	if n := countWhere(t, admin, handled, "tenant_id", tenantB, forgedID); n != 0 {
		t.Errorf("a message stored at %q left %d platformkit_handled row(s) for event %s naming tenant %s, which only its body claimed: the refusal wrote evidence into the tenant it refused to run inside (kit/events/providers/nats/jetstream.go's sink records a terminal failure the way the branch beside it does; it must record nothing)",
			subjectA, n, forgedID, tenantB)
	}
	if n := countWhere(t, admin, deadLetters, "tenant_id", tenantB, forgedID); n != 0 {
		t.Errorf("a message stored at %q left %d platformkit_dead_letters row(s) for event %s naming tenant %s: a message no handler ran has no terminal failure to review, and events.Replay would find one to clear",
			subjectA, n, forgedID, tenantB)
	}
	if n := countWhere(t, admin, deadLetters, "tenant_id", tenantA, forgedID); n != 0 {
		t.Errorf("a message stored at %q left %d platformkit_dead_letters row(s) for event %s although its handler never ran: the provider terminated it, which is not a failure of anybody's action",
			subjectA, n, forgedID)
	}
}

// countClaims counts a claim row for one event, however many duries claimed it.
func countClaims(t *testing.T, admin *sql.DB, table string, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+table+" WHERE event_id = $1", id).Scan(&n); err != nil {
		t.Fatalf("count %s rows for %s: %v", table, id, err)
	}
	return n
}

// claimTenant reads back the single claim and whose rows it was written under.
func claimTenant(t *testing.T, admin *sql.DB, id uuid.UUID) (int, uuid.UUID) {
	t.Helper()
	var (
		n   int
		raw string
	)
	// tenant_id is read as text because PostgreSQL has no max(uuid): the aggregate
	// is over the text form of the same bytes, and the answer is parsed back.
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*), coalesce(max(tenant_id::text), $2) FROM "+handled+" WHERE event_id = $1", id, uuid.Nil.String(),
	).Scan(&n, &raw); err != nil {
		t.Fatalf("read the claim for %s: %v", id, err)
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("the claim for %s names tenant %q, which is not a UUID: %v", id, raw, err)
	}
	return n, tenantID
}

// countWhere counts one table's rows for one event that name one tenant.
func countWhere(t *testing.T, admin *sql.DB, table, column string, tenantID, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+table+" WHERE event_id = $1 AND "+column+" = $2", id, tenantID,
	).Scan(&n); err != nil {
		t.Fatalf("count %s rows for %s and tenant %s: %v", table, id, tenantID, err)
	}
	return n
}
