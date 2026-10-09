package nats

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// TestADurableConsumerClosedWithItsSubscriptionDeliversNothing is the measurement
// behind TestTwoAppsOnOneBrokerNeverShareASubscription, review 1's pin, which was
// red because its subscribe helper carried `defer sub.Unsubscribe()` in its own body:
// both subscriptions were closed before its first publish, and it then waited on
// handlers with nothing standing behind them. Root adopted that file with the one
// line removed (decision 0008); this case stays because what nats.go does on that
// Unsubscribe is a fact about the transport that nothing else in the tree states, and
// because it is the case that says why a fixture of this package must not leave a
// consumer — or a backlog — for the next one. Measured here in the shipped shape — the same `wanted` consumer, the same `group(durable)`
// deliver group, the same filter set transport.AppFilters hands appname.Filters:
//
//   - with the subscription closed where it was made, no event published at the
//     app's own address reaches its handler, and no consumer is left on the stream at
//     all, so no later process is owed a delivery either;
//   - the events are in the stream, at the sequences the publish returned, which is
//     what makes this a fixture rather than a lost publication;
//   - and the same durable with the same filter set, subscribed and *held*, takes all
//     three under DeliverAll — the half that says the durable and the address do
//     reach these messages, and that what the pinned case reports is its own fixture.
//
// Only this run's events are counted, and that is not caution: the stream is one and
// this deployment reuses it, so DeliverAll replays what an earlier run left at the
// same address, and one of those can still be in flight when the close lands. The
// run id in the payload is what separates a message this case caused from one it
// inherited. The event name is this case's own, so its messages match no other
// test's filter, and the durable is cleared first and last.
func TestADurableConsumerClosedWithItsSubscriptionDeliversNothing(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset: the transport this repository ships is not being tested")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.StreamInfo(stream); errors.Is(err, nats.ErrStreamNotFound) {
		if _, err := js.AddStream(wantedStream()); err != nil {
			t.Fatalf("the %s stream: %v", stream, err)
		}
	} else if err != nil {
		t.Fatalf("read the %s stream: %v", stream, err)
	}

	app := appname.MustParse("acme")
	const module, event = "billing", "billing.invoice_closed"
	durable := appname.Durable(app, module, event)
	if err := js.DeleteConsumer(stream, durable); err != nil && !errors.Is(err, nats.ErrConsumerNotFound) {
		t.Fatalf("clear consumer %s: %v", durable, err)
	}
	t.Cleanup(func() { _ = js.DeleteConsumer(stream, durable) })

	run := uuid.NewString()
	arrived := make(chan struct{}, 16)
	// handler counts this run's events and acknowledges everything, including what
	// an earlier run left behind: leaving those unacknowledged would only make the
	// next run slower.
	handler := func(m *nats.Msg) {
		var body struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(m.Data, &body) == nil && body.ID == run {
			arrived <- struct{}{}
		}
		_ = m.Ack()
	}
	// closedInItsBody is review 1's helper: the defer belongs to the helper and not
	// to the test, which is the whole difference between this case and the one below.
	closedInItsBody := func() {
		t.Helper()
		sub, err := js.QueueSubscribe("", group(durable), handler, wanted(app, durable, event)...)
		if err != nil {
			t.Fatalf("subscribing %s: %v", durable, err)
		}
		defer func() { _ = sub.Unsubscribe() }()
	}
	closedInItsBody()

	// The consumer goes with the subscription. That removal is the server's, so it
	// is waited for rather than assumed: a delivery already in flight has to be
	// allowed to drain before "nothing is coming" means anything.
	removed := false
	for range 40 {
		if _, err := js.ConsumerInfo(stream, durable); errors.Is(err, nats.ErrConsumerNotFound) {
			removed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !removed {
		t.Error("a closed subscription left its durable consumer on the stream: something could still be delivered later, which is not the fact this case is about")
	}

	sequences := publishRun(t, js, app, event, run)

	select {
	case <-arrived:
		t.Fatal("an event of this run reached a handler whose subscription was closed where it was made")
	case <-time.After(time.Second):
	}
	for _, seq := range sequences {
		if _, err := js.GetMsg(stream, seq); err != nil {
			t.Errorf("the event at stream sequence %d is not in the stream: %v — nothing was delivered, but nothing was lost either, and this case has to say which", seq, err)
		}
	}

	// The counterfactual, differing above in one shape: the subscription is
	// returned and held, so DeliverAll replays the events sitting at those
	// sequences and this run's three reach the handler.
	held, err := js.QueueSubscribe("", group(durable), handler, wanted(app, durable, event)...)
	if err != nil {
		t.Fatalf("subscribing %s and holding it: %v", durable, err)
	}
	defer func() { _ = held.Unsubscribe() }()
	for i := range sequences {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("the %d of %d events published at %s reached a held subscription on %s with its own filter set: the durable and the address do not reach them",
				i, len(sequences), appname.Subject(app, uuid.Nil, event), durable)
		}
	}
}

// publishRun writes three events of one app and returns the stream sequences they
// landed at, which is how the case tells a message that was never written from one
// that was written and never delivered.
func publishRun(t *testing.T, js nats.JetStreamContext, app appname.Name, event, run string) []uint64 {
	t.Helper()
	var sequences []uint64
	for range 3 {
		body, err := json.Marshal(map[string]string{"id": run})
		if err != nil {
			t.Fatal(err)
		}
		ack, err := js.Publish(appname.Subject(app, uuid.New(), event), body)
		if err != nil {
			t.Fatalf("publish at %s: %v", appname.Subject(app, uuid.Nil, event), err)
		}
		sequences = append(sequences, ack.Sequence)
	}
	return sequences
}
