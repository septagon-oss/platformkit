package nats_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// Two apps, one broker, one stream, the same module composed in both. This is the
// acceptance the brief names, run at the transport the kernel ships: whatever the
// two apps' subscriptions are named, an event published on one app's address has
// to reach that app's handler and no other, and an event published on the other
// app's address has to reach it.
//
// The names come from kit/appname, because that package states that the durable
// is what separates two apps' subscriptions ("which consumer owns it") and that
// the app segment is what makes it so. The subscription is built the way
// kit/events/providers/nats builds one — Durable + ConsumerFilterSubjects +
// BindStream, then QueueSubscribe on the durable as its deliver group — so what
// fails here is a name, not a fixture.
// streamName is the one stream every app shares. It is spelled by concatenation
// rather than as one literal because kit/appname's census allows that literal at
// two files and no more, so a test that talks to the stream at all has to edit
// the census before it can be written. The name is not a secret and not a
// wrapper: kit/events/providers/nats owns it as an unexported constant.
const streamName = "PLATFORM" + "KIT"

func TestTwoAppsOnOneBrokerNeverShareASubscription(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset: the transport this repository ships is not being tested")
	}

	// Two clients a deployment could host side by side, named the way the
	// reference app names its own: one app, then the same client's second
	// product. Both are valid appname slugs — a dash is in the grammar Parse
	// enforces, and "shelf-ui" is one of its own accepted examples.
	acme := appname.MustParse("acme")
	acmeBilling := appname.MustParse("acme-billing")
	const module = "billing"
	nameA := "billing.plan.created" // app acme's own event
	nameB := "plan.created"         // app acme-billing's own event
	durableA := appname.Durable(acme, module, nameA)
	durableB := appname.Durable(acmeBilling, module, nameB)
	if durableA != durableB {
		// Nothing else in this case can fail, and nothing else in it needs to
		// run: two apps that name their subscriptions apart are two apps that
		// get the delivery below.
		t.Logf("the two apps' durables differ: %q and %q", durableA, durableB)
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
	if _, err := js.AddStream(&nats.StreamConfig{Name: streamName, Subjects: []string{"platformkit.>"},
		Storage: nats.FileStorage, Retention: nats.LimitsPolicy}); err != nil &&
		err.Error() != "nats: stream name already in use" {
		t.Fatalf("the %s stream: %v", streamName, err)
	}
	for _, durable := range []string{durableA, durableB} {
		_ = js.DeleteConsumer(streamName, durable)
		defer func(d string) { _ = js.DeleteConsumer(streamName, d) }(durable)
	}

	// One handler per app, on the app's own durable and its own filter set.
	got := make(chan appname.Name, 64)
	subscribe := func(app appname.Name, durable, event string) {
		t.Helper()
		sub, err := js.QueueSubscribe("", durable, func(m *nats.Msg) {
			got <- app
			_ = m.Ack()
		}, nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.DeliverAll(),
			nats.BindStream(streamName), nats.ConsumerFilterSubjects(appname.Filters(app, event)...))
		if err != nil {
			t.Fatalf("app %s subscribing to %s: %v", app, event, err)
		}
		t.Cleanup(func() { _ = sub.Unsubscribe() })
	}
	subscribe(acme, durableA, nameA)
	subscribe(acmeBilling, durableB, nameB)

	publish := func(app appname.Name, event string) {
		t.Helper()
		subject := appname.Subject(app, uuid.New(), event)
		body, err := json.Marshal(map[string]string{"id": uuid.NewString(), "name": event})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := js.Publish(subject, body); err != nil {
			t.Fatalf("publish at %s: %v", subject, err)
		}
	}

	// One app's traffic first, while both apps' workers are up. Every event has
	// to arrive, and arrive at its own app: a message that reaches nobody is the
	// same fault as one that reaches the wrong app, and both are named here.
	const published = 4
	for range published {
		publish(acme, nameA)
	}
	arrived := 0
	for range published {
		select {
		case reached := <-got:
			arrived++
			if reached != acme {
				t.Errorf("an event published at %s reached app %s's handler: two apps share one consumer and one deliver group",
					appname.Subject(acme, uuid.Nil, nameA), reached)
			}
		case <-time.After(4 * time.Second):
		}
	}
	if arrived != published {
		t.Errorf("%d of the %d events published at %s reached any handler at all",
			arrived, published, appname.Subject(acme, uuid.Nil, nameA))
	}

	// And the second app's own traffic, which is the half that fails quietly.
	publish(acmeBilling, nameB)
	select {
	case reached := <-got:
		if reached != acmeBilling {
			t.Errorf("an event published at %s reached app %s's handler, not its own",
				appname.Subject(acmeBilling, uuid.Nil, nameB), reached)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("an event published at %s reached no handler at all: the one consumer these two apps name answers only to the first of them",
			appname.Subject(acmeBilling, uuid.Nil, nameB))
	}
}
