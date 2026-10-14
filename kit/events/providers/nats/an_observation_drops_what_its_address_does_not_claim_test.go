package nats_test

import (
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	provider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/events/transport/observetest"
)

// testNATSURL is the broker these cases run against — the one `make up` starts,
// and the one `make down` declares disposable. It is required rather than
// optional for the reason the Makefile states: a suite that quietly skips the
// transport this repository ships proves nothing.
func testNATSURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset: the transport this repository ships is not being tested")
	}
	return url
}

// observeTransport dials one transport as the deployment naming app. What it
// returns is the shipped provider, unchanged: an observation that passes here
// passed against the broker a deployment talks to.
func observeTransport(t *testing.T, url string, app appname.Name) (transport.Transport, func()) {
	t.Helper()
	tr, err := provider.JetStream(app, url)
	if err != nil {
		t.Fatalf("jetstream as app %q: %v", app, err)
	}
	closer, ok := tr.(io.Closer)
	if !ok {
		t.Fatal("the jetstream transport releases nothing")
	}
	return tr, func() { _ = closer.Close() }
}

// TestObservationConformance runs the transport's own observation cases against
// JetStream, twice: once as a deployment that names its app, which publishes and
// watches the scoped address, and once as the deployment of one app, which uses
// the addresses this kernel formed before the app segment. Two transports over one
// broker are two separate connections, which is the only way the case about every
// process being noticed can mean what it says.
//
// Quiet is half a second: the settle window is what a case waits before it
// asserts a notice did not arrive, and on this host the broker is a container, so
// fan-out is prompt but not synchronous.
func TestObservationConformance(t *testing.T) {
	url := testNATSURL(t)
	observetest.Run(t, observetest.Deps{
		Name: "jetstream",
		Transport: func(t *testing.T, app appname.Name) (transport.Transport, func()) {
			return observeTransport(t, url, app)
		},
		Processes: func(t *testing.T, app appname.Name) (transport.Transport, transport.Transport, func()) {
			a, closeA := observeTransport(t, url, app)
			b, closeB := observeTransport(t, url, app)
			return a, b, func() { closeA(); closeB() }
		},
		Quiet: func() time.Duration { return 500 * time.Millisecond },
	})
}

// TestAMessageAtAnAddressItsDocumentDoesNotClaimIsNotDelivered is the negative arm
// the shared harness cannot run: it needs a client that writes where this
// transport would never write.
//
// An observation's filter is an exact subject, so the address is what decides
// whose tenant a delivery is in — but the filter is what the broker routed by, and
// the document inside the message gets a vote of its own unless something
// compares the two. transport.AddressMismatch is that comparison, and each
// forgery below is one way the two copies can disagree: written at the unscoped
// address while claiming an app, written at this app's address claiming a
// different tenant, and written at this app's address by a document naming no app
// at all — the shape a previous build published, which a process that names its
// app reads nowhere.
//
// What is checked is the drop, and the check needs the live wire beside it: a
// watch that delivered nothing would pass three assertions of absence, so the case
// ends by publishing one real event and requiring it.
func TestAMessageAtAnAddressItsDocumentDoesNotClaimIsNotDelivered(t *testing.T) {
	url := testNATSURL(t)
	const name = "task.closed"
	acme := appname.MustParse("acme")
	watched, claimed := uuid.New(), uuid.New()

	tr, closeTransport := observeTransport(t, url, acme)
	defer closeTransport()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect to the test broker: %v", err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("open JetStream: %v", err)
	}

	arrived := make(chan transport.Event, 8)
	stop, err := tr.Observe(t.Context(), watched, name, func(ev transport.Event) { arrived <- ev })
	if err != nil {
		t.Fatalf("observe %s in tenant %s: %v", name, watched, err)
	}
	defer stop()

	for _, forgery := range []struct {
		what    string
		subject string
		ev      transport.Event
	}{
		{"an address naming no app, with a document claiming one",
			transport.Subject(watched, name),
			transport.Event{Name: name, App: acme, TenantID: watched}},
		{"a document claiming another tenant at the watched address",
			transport.AppSubject(acme, watched, name),
			transport.Event{Name: name, App: acme, TenantID: claimed}},
		{"a document naming no app at this app's own address",
			transport.AppSubject(acme, watched, name),
			transport.Event{Name: name, TenantID: watched}},
	} {
		body, err := json.Marshal(forgery.ev)
		if err != nil {
			t.Fatalf("marshal %s: %v", forgery.what, err)
		}
		// Through the stream and not just the server: a message the stream never
		// stored would reach nobody for the wrong reason, and the case would answer
		// the question the transport never had to.
		if _, err := js.Publish(forgery.subject, body); err != nil {
			t.Fatalf("write %s: %v", forgery.what, err)
		}
		select {
		case ev := <-arrived:
			t.Errorf("a watch on tenant %s was handed a message at %s (%s): event %s of tenant %s",
				watched, forgery.subject, forgery.what, ev.Name, ev.TenantID)
		case <-time.After(2 * time.Second):
		}
	}

	// The same watch, the same second, one event this transport published itself.
	genuine := transport.Event{ID: uuid.New(), Name: name, App: acme, TenantID: watched,
		At: time.Now().UTC(), Payload: json.RawMessage(`{"case":"observation"}`)}
	if err := tr.Publish(t.Context(), genuine); err != nil {
		t.Fatalf("publish the real %s: %v", name, err)
	}
	select {
	case ev := <-arrived:
		if ev.ID != genuine.ID {
			t.Errorf("the watch was handed %s of tenant %s, not the %s this transport published", ev.ID, ev.TenantID, genuine.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watch delivered no notice at all: three assertions of absence proved nothing")
	}
}
