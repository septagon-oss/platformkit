package events_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// recorder is a transport that keeps what it was handed. It is what the relay
// tests assert against: the outbox's contract is "each row leaves once", and
// this is where that is visible.
type recorder struct {
	mu   sync.Mutex
	got  []events.Event
	fail error
}

func (r *recorder) Publish(_ context.Context, ev events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, ev)
	return nil
}

func (r *recorder) Subscribe(context.Context, string, string, func(context.Context, events.Event) error) error {
	return nil
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.got))
	for _, ev := range r.got {
		out = append(out, ev.Name)
	}
	return out
}

// publish writes one event in its own committed transaction.
func publish(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, name string, payload any) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(tx, name, payload)
	})
	if err != nil {
		t.Fatalf("publish %s: %v", name, err)
	}
}

func pending(t *testing.T, conn *db.Conn) (unpublished, published int) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FILTER (WHERE published_at IS NULL),
			count(*) FILTER (WHERE published_at IS NOT NULL) FROM platformkit_outbox`).
			Row().Scan(&unpublished, &published)
	})
	if err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	return unpublished, published
}

// TestPublishIsPartOfTheTransaction is the whole point of an outbox: the event
// and the state change share a fate.
func TestPublishIsPartOfTheTransaction(t *testing.T) {
	_, conn := dbtest.Schema(t)

	sentinel := errors.New("the handler failed after publishing")
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := events.Publish(tx, "billing.invoice_issued", map[string]any{"amount": 1}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Run = %v, want the handler's own error", err)
	}
	if unpublished, published := pending(t, conn); unpublished+published != 0 {
		t.Errorf("a rolled-back transaction left %d unpublished and %d published rows", unpublished, published)
	}

	publish(t, conn, acme, "billing.invoice_issued", map[string]any{"amount": 2})
	if unpublished, _ := pending(t, conn); unpublished != 1 {
		t.Errorf("a committed transaction left %d rows, want 1", unpublished)
	}
}

// TestPublishRefusesAnUnnamespacedEvent, because the module prefix is what makes
// an event traceable to the thing that emitted it.
func TestPublishRefusesAnUnnamespacedEvent(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for _, name := range []string{"", "invoice", "Billing.Issued", "billing.", "billing..x"} {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(tx, name, nil)
		})
		if err == nil {
			t.Errorf("Publish accepted %q", name)
		}
	}
}

// TestRelayPublishesEachRowOnceAndStampsIt: two passes, one delivery.
func TestRelayPublishesEachRowOnceAndStampsIt(t *testing.T) {
	_, conn := dbtest.Schema(t)
	publish(t, conn, acme, "billing.invoice_issued", map[string]any{"n": 1})
	publish(t, conn, acme, "billing.invoice_paid", map[string]any{"n": 2})

	r := &recorder{}
	for pass := range 2 {
		if err := events.Relay(t.Context(), conn, r); err != nil {
			t.Fatalf("relay pass %d: %v", pass, err)
		}
	}
	if got := strings.Join(r.names(), " "); got != "billing.invoice_issued billing.invoice_paid" {
		t.Errorf("the relay published %q; each row leaves once, oldest first", got)
	}
	if unpublished, published := pending(t, conn); unpublished != 0 || published != 2 {
		t.Errorf("after the relay: %d unpublished, %d published; want 0 and 2", unpublished, published)
	}

	// The relay crosses tenants, so a second tenant's row goes out too.
	other := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	publish(t, conn, other, "billing.invoice_issued", nil)
	if err := events.Relay(t.Context(), conn, r); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if len(r.names()) != 3 {
		t.Errorf("the relay published %v; it reads every tenant's rows", r.names())
	}
}

// TestAFailedPublishLeavesTheRowForNextTime: at-least-once means the stamp only
// lands when the transport took it.
func TestAFailedPublishLeavesTheRowForNextTime(t *testing.T) {
	_, conn := dbtest.Schema(t)
	publish(t, conn, acme, "billing.invoice_issued", nil)

	r := &recorder{fail: errors.New("the broker is down")}
	if err := events.Relay(t.Context(), conn, r); err == nil {
		t.Fatal("Relay reported success while the transport refused")
	}
	if unpublished, published := pending(t, conn); unpublished != 1 || published != 0 {
		t.Fatalf("after a failed relay: %d unpublished, %d published; want 1 and 0", unpublished, published)
	}

	r.fail = nil
	if err := events.Relay(t.Context(), conn, r); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if unpublished, published := pending(t, conn); unpublished != 0 || published != 1 {
		t.Errorf("after the retry: %d unpublished, %d published; want 0 and 1", unpublished, published)
	}
}

// TestPurgeKeepsWhatHasNotGoneOut: history is disposable, a queue is not.
func TestPurgeKeepsWhatHasNotGoneOut(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	insert := func(name string, publishedAt any) {
		t.Helper()
		_, err := admin.ExecContext(t.Context(),
			`INSERT INTO platformkit_outbox (tenant_id, name, payload, created_at, published_at)
			 VALUES ($1, $2, '{}'::jsonb, now() - interval '30 days', $3)`,
			acme.ID, name, publishedAt)
		if err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}
	insert("billing.old_and_published", time.Now().Add(-8*24*time.Hour))
	insert("billing.recently_published", time.Now())
	insert("billing.old_and_stuck", nil)

	if err := events.Purge(t.Context(), conn); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	var left []string
	rows, err := admin.QueryContext(t.Context(), `SELECT name FROM platformkit_outbox ORDER BY name`)
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		left = append(left, name)
	}
	if got := strings.Join(left, " "); got != "billing.old_and_stuck billing.recently_published" {
		t.Errorf("the purge left %q", got)
	}
}

// TestConsumeRunsInTheEventTenant: a handler is given the same kind of
// transaction a request handler is given, scoped to the tenant the event
// happened in — which is what lets it read and write that tenant's rows without
// knowing anything about tenancy.
func TestConsumeRunsInTheEventTenant(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	seen := make(chan uuid.UUID, 4)
	transport := events.Memory()
	subs := []events.Subscription{{
		Module: "ledger", Name: "billing.invoice_issued",
		Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			// The transaction is real: it can publish an event of its own.
			if err := events.Publish(tx, "ledger.entry_written", ev.ID); err != nil {
				return err
			}
			seen <- db.TenantOf(tx).ID
			return nil
		},
	}}
	if err := events.Consume(ctx, conn, transport, subs); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	publish(t, conn, acme, "billing.invoice_issued", nil)
	publish(t, conn, globex, "billing.invoice_issued", nil)
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	got := map[uuid.UUID]bool{}
	for range 2 {
		select {
		case id := <-seen:
			got[id] = true
		case <-time.After(10 * time.Second):
			t.Fatalf("the handler ran %d times, want 2", len(got))
		}
	}
	if !got[acme.ID] || !got[globex.ID] {
		t.Errorf("the handler saw %v, want one transaction per tenant", got)
	}
}

// TestAFailedHandlerSeesTheEventAgain: an error is a negative acknowledgement,
// and redelivery is what makes idempotency a requirement rather than a nicety.
func TestAFailedHandlerSeesTheEventAgain(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	deliveries := make(chan uuid.UUID, 8)
	var attempts int
	var mu sync.Mutex
	transport := events.Memory()
	err := events.Consume(ctx, conn, transport, []events.Subscription{{
		Module: "ledger", Name: "billing.invoice_issued",
		Handler: func(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			mu.Lock()
			attempts++
			first := attempts == 1
			mu.Unlock()
			deliveries <- ev.ID
			if first {
				return errors.New("the ledger was busy")
			}
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publish(t, conn, acme, "billing.invoice_issued", nil)
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	var ids []uuid.UUID
	for range 2 {
		select {
		case id := <-deliveries:
			ids = append(ids, id)
		case <-time.After(10 * time.Second):
			t.Fatalf("the event was delivered %d times, want 2", len(ids))
		}
	}
	if ids[0] != ids[1] {
		t.Errorf("redelivery carried %s then %s; the id is the deduplication key and has to be stable", ids[0], ids[1])
	}
}

// TestJetStreamCarriesAnEventBetweenProcesses is the other transport, against
// the NATS `make up` starts. It fails rather than skips when the environment
// names none: a suite that quietly skips the transport it ships proves nothing.
func TestJetStreamCarriesAnEventBetweenProcesses(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the stack with `make up`")
	}
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	// The stream is shared with every other run, so the event name and the
	// consumer are this run's alone.
	name := "test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + ".happened"
	transport, err := events.JetStream(url)
	if err != nil {
		t.Fatalf("JetStream(%s): %v", url, err)
	}
	if closer, ok := transport.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	} else {
		t.Fatal("the JetStream transport does not close its connection")
	}

	seen := make(chan events.Event, 2)
	err = events.Consume(ctx, conn, transport, []events.Subscription{{
		Module: "ledger", Name: name,
		Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			if db.TenantOf(tx).ID != ev.TenantID {
				return errors.New("the handler ran in the wrong tenant")
			}
			seen <- ev
			return nil
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publish(t, conn, acme, name, map[string]any{"amount": 42})
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	select {
	case ev := <-seen:
		if ev.Name != name || ev.TenantID != acme.ID || !strings.Contains(string(ev.Payload), "42") {
			t.Errorf("the round trip changed the event: %+v", ev)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("nothing came back from JetStream")
	}
	if unpublished, published := pending(t, conn); unpublished != 0 || published != 1 {
		t.Errorf("after the relay: %d unpublished, %d published", unpublished, published)
	}
}
