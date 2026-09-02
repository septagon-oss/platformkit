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
	// failAfter refuses everything past the nth event, which is what a broker
	// that goes away mid-batch looks like from here.
	failAfter int
}

func (r *recorder) Publish(_ context.Context, ev events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	if r.failAfter > 0 && len(r.got) >= r.failAfter {
		return errors.New("the broker went away")
	}
	r.got = append(r.got, ev)
	return nil
}

func (r *recorder) Subscribe(context.Context, string, string, events.Sink) error { return nil }

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

// TestAHandlerRunsOnceHoweverOftenTheEventIsDelivered is the exactly-once
// claim. Delivery is at-least-once by construction (docs/adr/0004), so Consume
// claims each event for each subscription in platformkit_handled inside the
// handler's own transaction: the second delivery finds the claim taken and the
// handler is never entered.
func TestAHandlerRunsOnceHoweverOftenTheEventIsDelivered(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	runs := make(chan uuid.UUID, 8)
	transport := events.Memory()
	// Two subscriptions to one event, because the claim is per subscription:
	// two modules interested in one thing are two pieces of work, and each has
	// to do its own.
	var subs []events.Subscription
	for _, name := range []string{"ledger", "mailer"} {
		subs = append(subs, events.Subscription{
			Module: name, Name: "billing.invoice_issued",
			Handler: func(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
				runs <- ev.ID
				return nil
			},
		})
	}
	if err := events.Consume(ctx, conn, transport, subs); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	publish(t, conn, acme, "billing.invoice_issued", map[string]any{"amount": 1})
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}
	// Two handlers, one event: two runs and then silence.
	var first []uuid.UUID
	for range 2 {
		select {
		case id := <-runs:
			first = append(first, id)
		case <-time.After(10 * time.Second):
			t.Fatalf("the handlers ran %d times, want 2", len(first))
		}
	}
	if first[0] != first[1] {
		t.Fatalf("the two subscriptions saw %s and %s", first[0], first[1])
	}

	// The same event again, exactly as a redelivery arrives: the relay has
	// stamped the row, so this is the transport doing what at-least-once means.
	replay := events.Event{ID: first[0], Name: "billing.invoice_issued", TenantID: acme.ID, Payload: []byte(`{"amount":1}`)}
	for range 3 {
		if err := transport.Publish(t.Context(), replay); err != nil {
			t.Fatalf("redeliver: %v", err)
		}
	}
	select {
	case id := <-runs:
		t.Fatalf("a handler ran again for %s", id)
	case <-time.After(2 * time.Second):
	}

	// And the ledger says why: one mark per subscription, both for this event.
	var marks []string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT durable FROM platformkit_handled WHERE event_id = ? ORDER BY durable", first[0]).
			Scan(&marks).Error
	})
	if err != nil {
		t.Fatalf("read the handled ledger: %v", err)
	}
	if got := strings.Join(marks, " "); got != "ledger-billing-invoice_issued mailer-billing-invoice_issued" {
		t.Errorf("the ledger holds %q, want one mark per subscription", got)
	}
}

// TestAFailedHandlerDoesNotKeepItsClaim: the claim and the handler's writes are
// one transaction, so a handler that fails rolls its mark back with its work
// and the redelivery runs it for real. Without that the first failure would
// swallow the event.
func TestAFailedHandlerDoesNotKeepItsClaim(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	var mu sync.Mutex
	attempts := 0
	done := make(chan struct{}, 4)
	transport := events.Memory()
	err := events.Consume(ctx, conn, transport, []events.Subscription{{
		Module: "ledger", Name: "billing.invoice_issued",
		Handler: func(_ context.Context, _ db.Tx[db.Tenant], _ events.Event) error {
			mu.Lock()
			attempts++
			first := attempts == 1
			mu.Unlock()
			if first {
				return errors.New("the ledger was busy")
			}
			done <- struct{}{}
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
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the event was never handled successfully")
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 {
		t.Errorf("the handler ran %d times, want a failure and then a success", attempts)
	}
}

// TestPurgeForgetsTheMarksItNoLongerNeeds: a mark exists to recognise a
// redelivery of its own event, so it outlives the outbox row by nothing.
func TestPurgeForgetsTheMarksItNoLongerNeeds(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	insert := func(age time.Duration) {
		t.Helper()
		_, err := admin.ExecContext(t.Context(),
			`INSERT INTO platformkit_handled (event_id, durable, tenant_id, handled_at) VALUES ($1, 'ledger-x', $2, $3)`,
			uuid.New(), acme.ID, time.Now().Add(-age))
		if err != nil {
			t.Fatalf("insert a mark: %v", err)
		}
	}
	insert(8 * 24 * time.Hour)
	insert(time.Hour)

	if err := events.Purge(t.Context(), conn); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	var left int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_handled`).Scan(&left); err != nil {
		t.Fatalf("count the marks: %v", err)
	}
	if left != 1 {
		t.Errorf("%d marks survived the purge, want the recent one", left)
	}
}

// TestConcurrentRelaysEachTakeTheirOwnRows: the relay holds no lock, so the
// claim that stops two workers publishing one row twice is FOR UPDATE SKIP
// LOCKED and nothing else. Two passes at once, forty rows, forty deliveries.
func TestConcurrentRelaysEachTakeTheirOwnRows(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for range 40 {
		publish(t, conn, acme, "billing.invoice_issued", nil)
	}

	r := &recorder{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := events.Relay(t.Context(), conn, r); err != nil {
				t.Errorf("relay: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := len(r.names()); got != 40 {
		t.Errorf("two concurrent relays published %d rows, want 40 with no duplicates", got)
	}
	if unpublished, published := pending(t, conn); unpublished != 0 || published != 40 {
		t.Errorf("after the relays: %d unpublished, %d published", unpublished, published)
	}
}

// TestOneRelayPassDrainsTheQueue: a pass loops until it sees a short batch, so
// a backlog of more than one batch does not take a tick per batch to clear.
func TestOneRelayPassDrainsTheQueue(t *testing.T) {
	_, conn := dbtest.Schema(t)
	const rows = 150 // more than one batch of 100
	for range rows {
		publish(t, conn, acme, "billing.invoice_issued", nil)
	}
	r := &recorder{}
	if err := events.Relay(t.Context(), conn, r); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if got := len(r.names()); got != rows {
		t.Errorf("one pass published %d of %d rows", got, rows)
	}
}

// TestEventsFromOneTransactionRelayInTheOrderTheyWerePublished. created_at used
// to default to now(), which is the transaction's start time, so every event a
// transaction published carried one timestamp and ORDER BY created_at, id fell
// through to a random uuid. clock_timestamp() is what makes the order real.
func TestEventsFromOneTransactionRelayInTheOrderTheyWerePublished(t *testing.T) {
	_, conn := dbtest.Schema(t)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		for i := range 6 {
			if err := events.Publish(tx, "billing.invoice_issued", map[string]any{"n": i}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	r := &recorder{}
	if err := events.Relay(t.Context(), conn, r); err != nil {
		t.Fatalf("relay: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var got []string
	for _, ev := range r.got {
		got = append(got, string(ev.Payload))
	}
	// jsonb, so Postgres has reprinted the payload; the order is what is under test.
	want := `{"n": 0} {"n": 1} {"n": 2} {"n": 3} {"n": 4} {"n": 5}`
	if strings.Join(got, " ") != want {
		t.Errorf("the relay published %q, want %q", strings.Join(got, " "), want)
	}
}

// TestAPublishThatFailsPartWayLeavesTheWholeBatchUnstamped: the stamp is one
// statement for the batch and it runs after every publish, so a transport that
// takes three of five events and then refuses leaves all five to go again. That
// is the at-least-once bargain, and it is the reason handlers deduplicate.
func TestAPublishThatFailsPartWayLeavesTheWholeBatchUnstamped(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for range 5 {
		publish(t, conn, acme, "billing.invoice_issued", nil)
	}
	r := &recorder{failAfter: 3}
	if err := events.Relay(t.Context(), conn, r); err == nil {
		t.Fatal("Relay reported success while the transport refused part way")
	}
	if got := len(r.names()); got != 3 {
		t.Errorf("the transport took %d events, want the 3 before it refused", got)
	}
	if unpublished, published := pending(t, conn); unpublished != 5 || published != 0 {
		t.Errorf("after the failed relay: %d unpublished, %d published; want 5 and 0", unpublished, published)
	}
}
