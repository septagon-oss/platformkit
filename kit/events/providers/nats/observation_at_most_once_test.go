package nats_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/events/transport/observetest"
)

func TestASlowObservationNeverReceivesOnePublishTwice(t *testing.T) {
	url := testNATSURL(t)
	app := appname.MustParse("observation")
	tr, closeTransport := observeTransport(t, url, app)
	defer closeTransport()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	tenant, first, second := uuid.New(), uuid.New(), uuid.New()
	entered, release := make(chan struct{}), make(chan struct{})
	announce := sync.OnceFunc(func() { close(entered) })
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	arrived := make(chan uuid.UUID, 16)
	stop, err := tr.Observe(t.Context(), tenant, observetest.EventName, func(ev transport.Event) {
		if ev.ID == first {
			announce()
			<-release
		}
		arrived <- ev.ID
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	// Read this observation's actual broker timer; do not change its delivery policy.
	// A no-ack consumer has no retry window and needs only a short settle period.
	wait := 100 * time.Millisecond
	found := false
	subject := transport.AppSubject(app, tenant, observetest.EventName)
	for consumer := range js.Consumers(sharedStream, nats.Context(t.Context())) {
		if consumer.Config.FilterSubject == subject {
			found = true
			if consumer.Config.AckPolicy != nats.AckNonePolicy {
				wait = consumer.Config.AckWait + time.Second
			}
			t.Logf("observation ack policy = %v, ack wait = %s", consumer.Config.AckPolicy, consumer.Config.AckWait)
		}
	}
	if !found {
		t.Fatal("the observer has no consumer at its exact subject")
	}
	publish := func(id uuid.UUID) {
		t.Helper()
		if err := tr.Publish(t.Context(), transport.Event{
			ID: id, App: app, TenantID: tenant, Name: observetest.EventName,
			At: time.Now().UTC(), Payload: []byte(`{}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	publish(first)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first publication never reached the observation")
	}
	publish(second)
	select {
	case <-time.After(wait):
	case <-t.Context().Done():
		t.Fatal("test ended while waiting for the broker's delivery window")
	}
	unblock()
	counts := map[uuid.UUID]int{}
	quiet := time.NewTimer(time.Second)
	defer quiet.Stop()
	for {
		select {
		case id := <-arrived:
			counts[id]++
			quiet.Reset(time.Second)
		case <-quiet.C:
			if counts[first] != 1 || counts[second] > 1 {
				t.Fatalf("two single publications reached the observer %d and %d times; want the initial publication once and the queued publication at most once", counts[first], counts[second])
			}
			return
		}
	}
}
