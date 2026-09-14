package events

// The tests that need to reach inside the package: the delivery ladder, which
// is a package variable so a test can run it in milliseconds rather than the
// fifty seconds the shipped one takes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// fast shortens the redelivery ladder for one test. The shape is what is under
// test — five attempts, then dead — not the wall-clock waits.
func fast(t *testing.T) {
	t.Helper()
	was := backoff
	backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { backoff = was })
}

// TestAPoisonEventIsDeadLetteredAndStopsComingBack. Before the cap, a handler
// that could never succeed was retried forever, which spent the subscription
// and — through the memory transport's bounded queue — the relay behind it.
func TestAPoisonEventIsDeadLetteredAndStopsComingBack(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	var mu sync.Mutex
	attempts := 0
	poison := errors.New("this will never work")
	transport := Memory()
	err := Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: "billing.invoice_issued",
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			mu.Lock()
			attempts++
			mu.Unlock()
			return poison
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, "billing.invoice_issued", nil)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	// The dead letter is the signal that the transport gave up.
	var (
		got  string
		name string
	)
	deadline := time.Now().Add(20 * time.Second)
	for {
		row := admin.QueryRowContext(t.Context(), `SELECT error, name FROM platformkit_dead_letters`)
		if err := row.Scan(&got, &name); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the poison event was never dead-lettered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got != poison.Error() || name != "billing.invoice_issued" {
		t.Errorf("the dead letter says %q for %q", got, name)
	}

	// And it stays given up on: no further attempts after the cap.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if attempts != maxDeliveries {
		t.Errorf("the handler ran %d times, want %d and then a dead letter", attempts, maxDeliveries)
	}
}

// TestJetStreamStopsRedeliveringAPoisonEvent is the same policy on the other
// transport, against the NATS `make up` starts. The handler cap and consumer
// backoff bound work while terminal recording remains recoverable.
func TestJetStreamStopsRedeliveringAPoisonEvent(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the stack with `make up`")
	}
	fast(t)
	// Unequal rungs catch a second retry delay layered onto the broker timer.
	backoff = []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond, time.Second}
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	// This run's own subject and consumer: the stream is shared with every
	// other run.
	name := "test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + ".happened"
	transport, err := JetStream(url)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	t.Cleanup(func() { _ = transport.(interface{ Close() error }).Close() })

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	var mu sync.Mutex
	attempts := 0
	var deliveries []time.Time
	err = Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			mu.Lock()
			attempts++
			deliveries = append(deliveries, time.Now())
			mu.Unlock()
			return errors.New("this will never work")
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, name, nil)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	var got int
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_dead_letters`).Scan(&got); err != nil {
			t.Fatalf("count the dead letters: %v", err)
		}
		if got == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("JetStream never gave up: %d dead letters after %d attempts", got, attempts)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != maxDeliveries {
		t.Errorf("the handler ran %d times, want %d; terminal recovery must not rerun the handler", attempts, maxDeliveries)
	}
	for i := 1; i < len(deliveries); i++ {
		gap, want := deliveries[i].Sub(deliveries[i-1]), backoff[min(i, len(backoff))-1]
		t.Logf("delivery %d gap=%s configured=%s", i+1, gap, want)
		if gap < want/2 || gap > want+250*time.Millisecond {
			t.Errorf("delivery %d gap=%s, want one %s backoff plus scheduling tolerance", i+1, gap, want)
		}
	}

}

// TestADriftedConsumerIsReconciled, against the NATS `make up` starts.
//
// A durable consumer outlives the process that created it, so its stored
// settings are a second copy of three constants in this package. nats.go
// refuses a subscription whose stored configuration is not the one it asks for,
// so a consumer left by an older build — or by this one, before AckWait and the
// ladder's first rung became one value — is a worker that cannot boot at all:
// the web role stays up and every worker crashloops, which is the rollout
// looking half healthy in exactly the way ADR 0005 wanted to avoid.
func TestADriftedConsumerIsReconciled(t *testing.T) {
	transport, js := jetstreamForTest(t)
	durable, name := uniqueDurable(t)

	// A consumer as an older build left it: a longer acknowledgement deadline,
	// a lower delivery cap, and no backoff ladder at all.
	stale := &nats.ConsumerConfig{
		Durable: durable, DeliverSubject: nats.NewInbox(), FilterSubject: subject + name,
		AckPolicy: nats.AckExplicitPolicy, DeliverPolicy: nats.DeliverAllPolicy,
		AckWait: 90 * time.Second, MaxDeliver: 3,
	}
	if _, err := js.AddConsumer(stream, stale); err != nil {
		t.Fatalf("create the drifted consumer: %v", err)
	}

	seen := make(chan Event, 1)
	if err := transport.Subscribe(t.Context(), durable, name, Sink{
		Handle: func(_ context.Context, ev Event) error { seen <- ev; return nil },
		Dead:   func(context.Context, Event, error) error { return nil },
	}); err != nil {
		t.Fatalf("subscribe to a drifted consumer: %v", err)
	}

	info, err := js.ConsumerInfo(stream, durable)
	if err != nil {
		t.Fatalf("read the consumer back: %v", err)
	}
	if info.Config.AckWait != ackWait() {
		t.Errorf("ack_wait is %v, want %v", info.Config.AckWait, ackWait())
	}
	if info.Config.MaxDeliver != -1 {
		t.Errorf("max_deliver is %d, want %d", info.Config.MaxDeliver, -1)
	}
	if !slices.Equal(info.Config.BackOff, backoff) {
		t.Errorf("backoff is %v, want %v", info.Config.BackOff, backoff)
	}

	// And the subscription is a subscription: the handler receives.
	deliver(t, transport, seen, name)
}

// TestAConsumerNATSCannotUpdateIsRecreated. Some settings are not an update:
// what a consumer filters, how it acknowledges, where it starts, and whether it
// is pushed at all. A pull consumer under this durable name is the shape a
// subscription cannot bind to however patiently it asks, so it is deleted and
// made again — safe because this transport asks for DeliverAll and every
// delivery is claimed in platformkit_handled before a handler runs.
func TestAConsumerNATSCannotUpdateIsRecreated(t *testing.T) {
	transport, js := jetstreamForTest(t)
	durable, name := uniqueDurable(t)

	// No DeliverSubject: a pull consumer, which a push subscription cannot bind
	// to at all.
	if _, err := js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable: durable, FilterSubject: subject + name,
		AckPolicy: nats.AckExplicitPolicy, DeliverPolicy: nats.DeliverAllPolicy,
	}); err != nil {
		t.Fatalf("create the pull consumer: %v", err)
	}

	seen := make(chan Event, 1)
	if err := transport.Subscribe(t.Context(), durable, name, Sink{
		Handle: func(_ context.Context, ev Event) error { seen <- ev; return nil },
		Dead:   func(context.Context, Event, error) error { return nil },
	}); err != nil {
		t.Fatalf("subscribe over a pull consumer: %v", err)
	}
	info, err := js.ConsumerInfo(stream, durable)
	if err != nil {
		t.Fatalf("read the consumer back: %v", err)
	}
	if info.Config.DeliverSubject == "" {
		t.Error("the consumer is still a pull consumer")
	}
	deliver(t, transport, seen, name)
}

// jetstreamForTest is the transport under test and a second connection to look
// at what it did to the server.
func jetstreamForTest(t *testing.T) (Transport, nats.JetStreamContext) {
	t.Helper()
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the stack with `make up`")
	}
	transport, err := JetStream(url)
	if err != nil {
		t.Fatalf("JetStream(%s): %v", url, err)
	}
	t.Cleanup(func() { _ = transport.(interface{ Close() error }).Close() })

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	return transport, js
}

// uniqueDurable names a consumer and an event nothing else in the shared stream
// uses. The name is short: NATS refuses a durable with a dot in it.
func uniqueDurable(t *testing.T) (durable, name string) {
	t.Helper()
	id := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	durable, name = "drift_"+id, "drift_"+id+".happened"
	t.Cleanup(func() {
		nc, err := nats.Connect(os.Getenv("PLATFORMKIT_TEST_NATS_URL"))
		if err != nil {
			return
		}
		defer nc.Close()
		if js, err := nc.JetStream(); err == nil {
			_ = js.DeleteConsumer(stream, durable)
		}
	})
	return durable, name
}

// deliver publishes one event and waits for the sink to see it, which is the
// half of "reconciled" that a consumer's settings do not prove.
func deliver(t *testing.T, transport Transport, seen <-chan Event, name string) {
	t.Helper()
	ev := Event{ID: uuid.New(), Name: name, TenantID: uuid.New(), Payload: []byte(`{"amount":42}`)}
	if err := transport.Publish(t.Context(), ev); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case got := <-seen:
		if got.ID != ev.ID {
			t.Errorf("the handler saw %s, want %s", got.ID, ev.ID)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the reconciled subscription delivered nothing")
	}
}

// TestTwoWorkersShareOneDurable is the review's finding about scaling out: the
// boot log recommends running more than one worker, and a second one could not
// start. A push consumer with no deliver group belongs to one subscriber, and
// the second subscription is refused —
//
//	nats: consumer is already bound to a subscription
//
// — so the second replica crashlooped on the first subscription it made. The
// deliver group is what a durable's subscribers join, and this is the claim it
// makes: two of them bind without error, and one event is handled once between
// them rather than once each.
func TestTwoWorkersShareOneDurable(t *testing.T) {
	transport, _ := jetstreamForTest(t)
	durable, name := uniqueDurable(t)

	// Two subscriptions on one durable, which is two worker replicas: they run
	// in one process here because what is under test is what NATS does with the
	// second bind, and that is the same question either way.
	const workers = 2
	seen := make(chan uuid.UUID, 16)
	for worker := range workers {
		err := transport.Subscribe(t.Context(), durable, name, Sink{
			Handle: func(_ context.Context, ev Event) error { seen <- ev.ID; return nil },
			Dead:   func(context.Context, Event, error) error { return nil },
		})
		if err != nil {
			t.Fatalf("worker %d subscribing to durable %s: %v", worker, durable, err)
		}
	}

	// Three events, so "each is handled once" is a claim about a stream of them
	// and not about one that happened to land on the first subscriber.
	const events = 3
	wanted := make(map[uuid.UUID]bool, events)
	for range events {
		ev := Event{ID: uuid.New(), Name: name, TenantID: uuid.New(), Payload: []byte(`{"amount":42}`)}
		wanted[ev.ID] = false
		if err := transport.Publish(t.Context(), ev); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	handled := 0
	deadline := time.After(30 * time.Second)
	for handled < events {
		select {
		case id := <-seen:
			if delivered, ok := wanted[id]; !ok || delivered {
				t.Fatalf("unexpected or repeated event %s", id)
			}
			wanted[id] = true
			handled++
		case <-deadline:
			t.Fatalf("%d of %d events were handled", handled, events)
		}
	}
	// And no more: a deliver group is one delivery shared, not one per member.
	select {
	case <-seen:
		t.Error("an event was handled twice; the two subscribers are a deliver group and not two consumers")
	case <-time.After(2 * time.Second):
	}
}

// TestADriftedStreamIsReconciled. The stream is created on first use and then
// outlives every process that connects, so its settings are a second copy of
// wantedStream that nothing kept in step — subjects and retention were never
// looked at again after the first boot. They are now, on every subscribe.
func TestADriftedStreamIsReconciled(t *testing.T) {
	transport, js := jetstreamForTest(t)
	durable, name := uniqueDurable(t)

	// The stream as an older build left it: a narrower subject space and a
	// different age. Both are settings NATS can change on a live stream.
	drifted := wantedStream()
	drifted.Subjects = []string{subject + "narrower.>"}
	drifted.MaxAge = keep / 2
	if _, err := js.UpdateStream(drifted); err != nil {
		t.Fatalf("drift the stream: %v", err)
	}

	seen := make(chan Event, 1)
	err := transport.Subscribe(t.Context(), durable, name, Sink{
		Handle: func(_ context.Context, ev Event) error { seen <- ev; return nil },
		Dead:   func(context.Context, Event, error) error { return nil },
	})
	if err != nil {
		t.Fatalf("subscribe against a drifted stream: %v", err)
	}
	info, err := js.StreamInfo(stream)
	if err != nil {
		t.Fatalf("read the stream back: %v", err)
	}
	want := wantedStream()
	if !slices.Equal(info.Config.Subjects, want.Subjects) {
		t.Errorf("the subjects are %v, want %v", info.Config.Subjects, want.Subjects)
	}
	if info.Config.MaxAge != want.MaxAge {
		t.Errorf("max_age is %s, want %s", info.Config.MaxAge, want.MaxAge)
	}
	// And the subject space it was narrowed away from carries an event again.
	deliver(t, transport, seen, name)
}

// A relay deadline does not acknowledge a handler that is still running. Its
// transaction can finish after the pass ends, or roll back when the worker dies.
func TestMemoryKeepsUnfinishedDeliveryInTheOutbox(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%t", restart), func(t *testing.T) {
			admin, conn := dbtest.Schema(t)
			worker, stop := context.WithCancel(t.Context())
			defer stop()
			tenant := tenancy.Tenant{ID: uuid.New()}
			name := "source.changed"
			entered, release := make(chan struct{}), make(chan struct{})
			transport := Memory()
			handler := func(ctx context.Context, tx db.Tx[db.Tenant], _ Event) error {
				if err := Publish(ctx, tx, "effect.completed", nil); err != nil {
					return err
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if err := Consume(worker, conn, transport, []Subscription{{Module: "effect", Name: name, Handler: handler}}); err != nil {
				t.Fatal(err)
			}
			if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return Publish(ctx, tx, name, nil)
			}); err != nil {
				t.Fatal(err)
			}
			pass, cancel := context.WithCancel(t.Context())
			result := make(chan error, 1)
			go func() { result <- Relay(pass, conn, transport) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not start")
			}
			cancel()
			if err := <-result; err == nil {
				t.Error("relay acknowledged an unfinished handler")
			}
			var pending bool
			if err := admin.QueryRowContext(t.Context(), "SELECT published_at IS NULL FROM platformkit_outbox WHERE name=$1", name).Scan(&pending); err != nil || !pending {
				t.Errorf("unfinished event pending=%t: %v", pending, err)
			}
			if restart {
				stop()
				transport = Memory()
				handler = func(ctx context.Context, tx db.Tx[db.Tenant], _ Event) error {
					return Publish(ctx, tx, "effect.completed", nil)
				}
				if err := Consume(t.Context(), conn, transport, []Subscription{{Module: "effect", Name: name, Handler: handler}}); err != nil {
					t.Fatal(err)
				}
			} else {
				close(release)
			}
			finish, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := Relay(finish, conn, transport); err != nil {
				t.Fatal(err)
			}
			var completed int
			if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name='effect.completed'").Scan(&completed); err != nil || completed != 1 {
				t.Fatalf("committed child events=%d, want 1: %v", completed, err)
			}
		})
	}
}

func TestTerminalRecordingCommitsItsClaimAtomically(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ev := Event{ID: uuid.New(), TenantID: uuid.New(), Name: "source.changed"}
	sub := Subscription{Module: "effect", Name: ev.Name}
	durable := sub.durable()
	cause := errors.New("handler failed")
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_dead_letters ADD CONSTRAINT unavailable CHECK (false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	if err := deadLetter(t.Context(), conn, ev, durable, cause); err == nil {
		t.Fatal("terminal recording hid the database failure")
	}
	var claims int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_handled").Scan(&claims); err != nil || claims != 0 {
		t.Fatalf("failed recording kept %d claims: %v", claims, err)
	}
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_dead_letters DROP CONSTRAINT unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := deadLetter(t.Context(), conn, ev, durable, cause); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_handled").Scan(&claims); err != nil || claims != 1 {
		t.Errorf("successful terminal recording kept %d claims, want 1: %v", claims, err)
	}
	// A lost acknowledgment of successful work must not turn it into failure.
	completed := Event{ID: uuid.New(), TenantID: ev.TenantID, Name: ev.Name}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: ev.TenantID}), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		_, err := claim(tx, completed.ID, durable)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := deadLetter(t.Context(), conn, completed, durable, cause); err != nil {
		t.Fatal(err)
	}
	var failures int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_dead_letters").Scan(&failures); err != nil || failures != 1 {
		t.Errorf("recorded failures=%d, want only the exhausted event: %v", failures, err)
	}
	// Retain terminal claims even after the ordinary history window expires.
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_handled SET handled_at = now() - interval '8 days'"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(t.Context(), "INSERT INTO platformkit_outbox (id,tenant_id,name,payload) VALUES ($1,$2,$3,'null')", completed.ID, completed.TenantID, completed.Name); err != nil {
		t.Fatal(err)
	}
	if err := Purge(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_handled WHERE event_id=$1", ev.ID).Scan(&claims); err != nil || claims != 1 {
		t.Errorf("purge removed the terminal claim: remaining=%d error=%v", claims, err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_handled WHERE event_id=$1", completed.ID).Scan(&claims); err != nil || claims != 1 {
		t.Errorf("purge removed the claim of an unpublished outbox row: remaining=%d error=%v", claims, err)
	}
	// Re-enter the real consumer after terminal acknowledgment loss. Its handler
	// must not run again, including after purge.
	var runs atomic.Int64
	transport := Memory()
	sub.Handler = func(context.Context, db.Tx[db.Tenant], Event) error { runs.Add(1); return nil }
	if err := Consume(t.Context(), conn, transport, []Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	if err := transport.Publish(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	if got := runs.Load(); got != 0 {
		t.Errorf("terminal redelivery ran the handler %d times", got)
	}
	// Upgrade compatibility: previous releases persisted the failure without a
	// completion claim. Its handler must remain stopped when the new code starts.
	legacy := uuid.New()
	if _, err := admin.ExecContext(t.Context(), "INSERT INTO platformkit_dead_letters (event_id,durable,tenant_id,name,error) VALUES ($1,$2,$3,$4,'old failure')", legacy, durable, ev.TenantID, ev.Name); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: ev.TenantID}), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		first, err := claim(tx, legacy, durable)
		if first {
			t.Error("legacy terminal event was claimed for handling again")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

}

func TestJetStreamRetriesTerminalRecordingAfterRestart(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), `CREATE SEQUENCE terminal_attempts;
  ALTER TABLE platformkit_dead_letters ADD CONSTRAINT unavailable CHECK (nextval('terminal_attempts') < 0) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	transport, js := jetstreamForTest(t)
	_, name := uniqueDurable(t)
	worker, stop := context.WithCancel(t.Context())
	defer stop()
	var attempts atomic.Int64
	subscription := Subscription{Module: "effect", Name: name, Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
		attempts.Add(1)
		return errors.New("permanent provider refusal")
	}}
	t.Cleanup(func() { _ = js.DeleteConsumer(stream, subscription.durable()) })
	if err := Consume(worker, conn, transport, []Subscription{subscription}); err != nil {
		t.Fatal(err)
	}
	ev := Event{ID: uuid.New(), Name: name, TenantID: uuid.New(), Payload: []byte(`null`)}
	if err := transport.Publish(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		info, err := js.ConsumerInfo(stream, subscription.durable())
		if err != nil {
			t.Fatal(err)
		}
		var tried bool
		if err := admin.QueryRowContext(t.Context(), "SELECT is_called FROM terminal_attempts").Scan(&tried); err != nil {
			t.Fatal(err)
		}
		if tried && info.Delivered.Consumer >= uint64(maxDeliveries+1) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal recording cannot retry: deliveries=%d pending=%d", info.Delivered.Consumer, info.NumAckPending)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	_ = transport.(interface{ Close() error }).Close()
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_dead_letters DROP CONSTRAINT unavailable"); err != nil {
		t.Fatal(err)
	}
	transport, _ = jetstreamForTest(t)
	if err := Consume(t.Context(), conn, transport, []Subscription{subscription}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		var failures int
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_dead_letters").Scan(&failures); err != nil {
			t.Fatal(err)
		}
		info, err := js.ConsumerInfo(stream, subscription.durable())
		if err != nil {
			t.Fatal(err)
		}
		if failures == 1 && info.NumAckPending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart did not finish terminal recording: failures=%d pending=%d", failures, info.NumAckPending)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := attempts.Load(); got > int64(maxDeliveries) {
		t.Fatalf("terminal recovery replayed provider handler: attempts=%d, cap=%d", got, maxDeliveries)
	}
}

func TestMemoryRetriesTerminalRecordingWithoutReplayingTheHandler(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), `CREATE SEQUENCE terminal_attempts;
		ALTER TABLE platformkit_dead_letters ADD CONSTRAINT unavailable CHECK (nextval('terminal_attempts') < 0) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	transport := Memory()
	var attempts atomic.Int64
	sub := Subscription{Module: "effect", Name: "source.changed", Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
		attempts.Add(1)
		return errors.New("permanent provider refusal")
	}}
	if err := Consume(t.Context(), conn, transport, []Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()}), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, sub.Name, nil)
	}); err != nil {
		t.Fatal(err)
	}
	pass, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- Relay(pass, conn, transport) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var tried bool
		if err := admin.QueryRowContext(t.Context(), "SELECT is_called FROM terminal_attempts").Scan(&tried); err != nil {
			t.Fatal(err)
		}
		if tried {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal persistence was not attempted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-result; err == nil {
		t.Error("relay acknowledged a failed terminal write")
	}
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_dead_letters DROP CONSTRAINT unavailable"); err != nil {
		t.Fatal(err)
	}
	finish, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := Relay(finish, conn, transport); err != nil {
		t.Fatal(err)
	}
	var failures int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_dead_letters").Scan(&failures); err != nil || failures != 1 {
		t.Fatalf("terminal recovery recorded %d failures: %v", failures, err)
	}
	if got := attempts.Load(); got != int64(maxDeliveries) {
		t.Fatalf("handler attempts=%d, want %d; retries must only persist the terminal outcome", got, maxDeliveries)
	}
}
