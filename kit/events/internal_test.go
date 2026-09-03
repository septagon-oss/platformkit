package events

// The tests that need to reach inside the package: the delivery ladder, which
// is a package variable so a test can run it in milliseconds rather than the
// fifty seconds the shipped one takes.

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
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
// transport, against the NATS `make up` starts. Without nats.MaxDeliver a bare
// Nak is handed straight back by the server, so one poison event became a
// redelivery storm measured in thousands per second.
func TestJetStreamStopsRedeliveringAPoisonEvent(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the stack with `make up`")
	}
	fast(t)
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
	err = Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			mu.Lock()
			attempts++
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
		t.Errorf("the handler ran %d times, want %d; the cap is nats.MaxDeliver", attempts, maxDeliveries)
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
		Dead:   func(context.Context, Event, error) {},
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
	if info.Config.MaxDeliver != maxDeliveries {
		t.Errorf("max_deliver is %d, want %d", info.Config.MaxDeliver, maxDeliveries)
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
		Dead:   func(context.Context, Event, error) {},
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
	seen := make(chan int, 16)
	for worker := range workers {
		err := transport.Subscribe(t.Context(), durable, name, Sink{
			Handle: func(_ context.Context, _ Event) error { seen <- worker; return nil },
			Dead:   func(context.Context, Event, error) {},
		})
		if err != nil {
			t.Fatalf("worker %d subscribing to durable %s: %v", worker, durable, err)
		}
	}

	// Three events, so "each is handled once" is a claim about a stream of them
	// and not about one that happened to land on the first subscriber.
	const events = 3
	for range events {
		ev := Event{ID: uuid.New(), Name: name, TenantID: uuid.New(), Payload: []byte(`{"amount":42}`)}
		if err := transport.Publish(t.Context(), ev); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	handled := 0
	deadline := time.After(30 * time.Second)
	for handled < events {
		select {
		case <-seen:
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
		Dead:   func(context.Context, Event, error) {},
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
