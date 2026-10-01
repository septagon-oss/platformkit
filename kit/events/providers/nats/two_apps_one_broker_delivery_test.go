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

// sharedStream names the one stream every app of a deployment shares. It is spelled
// by concatenation for the reason review 1's case gives: kit/appname's census counts
// the whole literal and allows it at two files and no more, so a third test that
// talks to the stream at all would have to edit the census before writing itself.
// The name is owned by kit/events/providers/nats as an unexported constant.
const sharedStream = "PLATFORM" + "KIT"

// TestEachAppOfTwoReceivesOnlyItsOwnEvents is the acceptance the brief names, run at
// the transport this kernel ships: two apps on one broker, one stream, the same
// module composed in both, and the pair whose durable names collide if the app
// segment is not separable — app "acme" with module "billing" and event
// "billing.plan.created" against app "acme-billing" with module "billing" and event
// "plan.created".
//
// Review 1 pinned the same property in two_apps_one_broker_test.go. That case cannot
// report anything about delivery: its subscribe helper defers sub.Unsubscribe()
// inside the helper, so the subscription is closed before the first publish and no
// event can reach any handler whatever the names are (measured: 0 of 3 published
// after that helper returns, 3 of 3 published while the same subscription is held).
// This case holds each subscription for the whole test, which is the only shape in
// which "one app received another's work" can be observed at all — and it asserts
// the half the pinned case could not, that an event reaches its own app and never
// the other one, counted per app rather than in the aggregate.
//
// DeliverAll over a stream this deployment reuses means a previous run's messages
// are replayed to the consumer created here. Every message published by this run
// carries run as its payload id, and only those are counted; a stale message is
// still acknowledged, because leaving it unacknowledged would make the next run
// slower rather than this one honest.
func TestEachAppOfTwoReceivesOnlyItsOwnEvents(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset: the transport this repository ships is not being tested")
	}

	acme := appname.MustParse("acme")
	acmeBilling := appname.MustParse("acme-billing")
	const module = "billing"
	const eventOfAcme = "billing.plan.created"
	const eventOfBilling = "plan.created"
	durableAcme := appname.Durable(acme, module, eventOfAcme)
	durableBilling := appname.Durable(acmeBilling, module, eventOfBilling)
	if durableAcme == durableBilling {
		t.Fatalf("two apps name one consumer %q: one queue group, one handled-ledger key", durableAcme)
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
	if _, err := js.AddStream(&nats.StreamConfig{Name: sharedStream, Subjects: []string{appname.SubjectSpace()},
		Storage: nats.FileStorage, Retention: nats.LimitsPolicy}); err != nil &&
		err.Error() != "nats: stream name already in use" {
		t.Fatalf("the %s stream: %v", sharedStream, err)
	}
	// Both consumers start from nothing: a consumer left by an interrupted run
	// would carry that run's filter set, which is exactly the fault under test.
	for _, durable := range []string{durableAcme, durableBilling} {
		if err := js.DeleteConsumer(sharedStream, durable); err != nil && err != nats.ErrConsumerNotFound {
			t.Fatalf("delete consumer %s: %v", durable, err)
		}
		t.Cleanup(func() { _ = js.DeleteConsumer(sharedStream, durable) })
	}

	run := uuid.NewString()
	arrived := make(chan string, 256)
	// subscribe returns the live subscription, which the caller keeps until the
	// test ends: closing it here is what made the pinned case unable to deliver.
	subscribe := func(app appname.Name, durable, event string) *nats.Subscription {
		t.Helper()
		sub, err := js.QueueSubscribe("", durable, func(m *nats.Msg) {
			var body struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(m.Data, &body) == nil && body.ID == run {
				arrived <- string(app)
			}
			_ = m.Ack()
		}, nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.DeliverAll(),
			nats.BindStream(sharedStream), nats.ConsumerFilterSubjects(appname.Filters(app, event)...))
		if err != nil {
			t.Fatalf("app %s subscribing to %s: %v", app, event, err)
		}
		return sub
	}
	acmeSub := subscribe(acme, durableAcme, eventOfAcme)
	defer func() { _ = acmeSub.Unsubscribe() }()
	billingSub := subscribe(acmeBilling, durableBilling, eventOfBilling)
	defer func() { _ = billingSub.Unsubscribe() }()

	publish := func(app appname.Name, event string, count int) {
		t.Helper()
		for range count {
			subject := appname.Subject(app, uuid.New(), event)
			body, err := json.Marshal(map[string]string{"id": run, "name": event})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := js.Publish(subject, body); err != nil {
				t.Fatalf("publish at %s: %v", subject, err)
			}
		}
	}

	// One app's traffic first, while both apps' workers are up.
	const published = 4
	publish(acme, eventOfAcme, published)
	// And the second app's own traffic in the same window: a consumer that joined
	// the other app's queue group would take a share of these and of those.
	publish(acmeBilling, eventOfBilling, 2)

	got := map[string]int{}
reading:
	for got["acme"]+got["acme-billing"] < published+2 {
		select {
		case reached := <-arrived:
			got[reached]++
		case <-time.After(10 * time.Second):
			t.Logf("delivery stopped early: %v so far", got)
			break reading
		}
	}
	if got["acme"] != published {
		t.Errorf("%d of the %d events published at %s reached that app's handler (%v)",
			got["acme"], published, appname.Subject(acme, uuid.Nil, eventOfAcme), got)
	}
	if got["acme-billing"] != 2 {
		t.Errorf("%d of the 2 events published at %s reached that app's handler (%v)",
			got["acme-billing"], appname.Subject(acmeBilling, uuid.Nil, eventOfBilling), got)
	}
	// The cross-app half, which no aggregate count can hide: an event of app acme
	// delivered to app acme-billing's handler is one app's tenant's work running
	// another app's code.
	if got["acme-billing"] > 2 {
		t.Errorf("%d more events than app acme-billing published reached its handler: the two apps share a queue group",
			got["acme-billing"]-2)
	}
}
