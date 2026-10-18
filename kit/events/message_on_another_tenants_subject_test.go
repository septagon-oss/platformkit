package events

// A message on another tenant's subject is refused at the door.
//
// The subject is where this delivery put the tenant: "platformkit.<tenant>.
// <module>.<event> so a durable can be per tenant" (decision 0053 §1), and
// kit/events/transport/subject.go says the broker's publish subject, the
// envelope's `subject` and a consumer's filter all come from the same function.
// The pin asserts the half of that promise nothing here exercises: a delivery
// runs its handler in the tenant its *address* names, not in the tenant the
// document claims.
//
// The forgery is one a process holding broker credentials can perform — the
// stream accepts anything under platformkit.>, and JetStream checks a message
// against the stream's subject space, never against the document inside it.
// Whether the kernel's own subscriber then notices is the difference between an
// address that routes and an address that is decoration; the README's
// "independent sinks must provide their own tenant checks" is a sentence about
// foreign readers, not about Consume.
//
// Read it as a fixture for the tenant boundary on the wire, not as a claim
// about untrusted networks.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAMessageStoredOnOneTenantsAddressIsNotDeliveredInsideAnotherTenantsTransaction
// puts a self-consistent envelope for tenant B on the wire under tenant A's
// subject, which is what a foreign publisher writes when it addresses a
// message at A and stamps it as B's. The correct outcome is one of two: the
// delivery refuses the mismatch, or it delivers with the transaction scoped to
// the tenant the subject names. What must never happen is the handler running
// inside B's rows because the document said so.
func TestAMessageStoredOnOneTenantsAddressIsNotDeliveredInsideAnotherTenantsTransaction(t *testing.T) {
	broker, js := jetstreamForTest(t)
	admin, conn := dbtest.Schema(t)

	tenantA := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	tenantB := tenancy.Tenant{ID: uuid.New(), Slug: "othercorp"}

	_, name := uniqueDurable(t)
	// The consumer Consume makes is named after the subscription, not after
	// uniqueDurable's guess, so the cleanup has to know the real name.
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

	// One row that belongs to tenant B and to nobody else. The handler's own
	// read of it is what says which tenant its transaction ran under: read
	// through RLS it is visible exactly when the transaction is B's.
	marker := "other-tenant-" + uuid.NewString()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO platformkit_outbox (tenant_id, name, payload) VALUES ($1, $2, '{}'::jsonb)`,
		tenantB.ID, marker); err != nil {
		t.Fatalf("seed tenant B's row: %v", err)
	}

	type delivery struct {
		tenant   uuid.UUID
		reachedB bool
	}
	seen := make(chan delivery, 4)

	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	if err := Consume(ctx, conn, broker, []Subscription{{
		Module: "drift", Name: name,
		Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev Event) error {
			var n int64
			// The read is the assertion: a handler scoped to A cannot see it,
			// and one scoped to B can.
			if err := tx.DB().Raw(
				`SELECT count(*) FROM platformkit_outbox WHERE name = ?`, marker,
			).Row().Scan(&n); err != nil {
				return err
			}
			seen <- delivery{tenant: db.TenantOf(tx).ID, reachedB: n > 0}
			return nil
		},
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	// The envelope, built by the kernel's own marshal so it is a valid one:
	// subject, source and tenantid agree with each other. Only the address it
	// is stored under belongs to another tenant.
	forged := transport.Event{
		ID: uuid.New(), Name: name, TenantID: tenantB.ID,
		Payload: json.RawMessage(`{"claims":"othercorp"}`), At: time.Now().UTC(),
	}
	body, err := json.Marshal(forged)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	subjectA := transport.Subject(tenantA.ID, name)
	ack, err := js.Publish(subjectA, body)
	if err != nil {
		t.Fatalf("publish on %q: %v", subjectA, err)
	}
	// The reachability probe, and it does not ask the defect's leave: the
	// broker's own store acknowledgement says the message is on A's address,
	// and the read-back says the stored bytes name B. If the delivery below
	// refuses the pair, this line is still true.
	stored, err := js.GetLastMsg(stream, subjectA)
	if err != nil {
		t.Fatalf("read back what was stored on %q: %v", subjectA, err)
	}
	if stored.Sequence != ack.Sequence || stored.Subject != subjectA {
		t.Fatalf("the last message on %q is not the one published: seq %d subject %q, want seq %d",
			subjectA, stored.Sequence, stored.Subject, ack.Sequence)
	}
	if docTenant := tenantOfDocument(t, stored.Data); docTenant != tenantB.ID {
		t.Fatalf("the message stored on A's address names tenant %s, not B's %s", docTenant, tenantB.ID)
	}

	select {
	case got := <-seen:
		if got.tenant == tenantB.ID || got.reachedB {
			t.Fatalf("a message stored on %s's address %q ran its handler inside %s's rows: the envelope claimed tenant %s\n"+
				"the handler's transaction is scoped by the tenant the document claims (kit/events/events.go's Consume), and nothing compares the broker's subject with the envelope's (kit/events/providers/nats/jetstream.go)",
				tenantA.Slug, subjectA, tenantB.Slug, tenantB.ID)
		}
		// Delivered, but inside the tenant its address names: acceptable.
	case <-time.After(3 * time.Second):
		// Never delivered at all: the delivery refused the mismatch, or
		// terminated it. Either is the correct outcome for a message
		// addressed at one tenant and stamped as another's.
	}
}

// tenantOfDocument reads the tenant the stored bytes claim, through the same
// decoder a subscriber uses.
func tenantOfDocument(t *testing.T, body []byte) uuid.UUID {
	t.Helper()
	var ev transport.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		t.Fatalf("the stored bytes do not decode: %v", err)
	}
	return ev.TenantID
}
