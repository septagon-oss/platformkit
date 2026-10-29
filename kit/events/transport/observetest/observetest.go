// Package observetest runs the cases every PlatformKit events transport must answer for an
// ephemeral observation. It is the shape both providers are measured against; a provider
// that cannot answer one of them does not serve live connections.
// The six cases are the contract of transport.Transport.Observe turned into executable
// checks: one tenant and one event, each publish handed at most once, nothing started or
// resumed, and withdrawal on demand. None needs a tenant registry, a database or an
// installed application, because a transport routes by an address and never asks whether
// the tenant exists — which is what lets the same six run against an in-process transport
// and against a broker.
// A provider's own test file is one call to Run; what a provider cannot express at this
// generality stays in the provider's package.
package observetest

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

// EventName is the event every case publishes. It names no module on purpose: a
// durable consumer filters an event name across every tenant, and the stream
// retains what it was fed, so a conformance suite that published a real event
// would add its own traffic to whatever the module owning that name is testing
// against the same broker. This name belongs to the harness and to nothing else,
// and a provider's own case that needs to write one of these events by hand uses
// the same constant rather than a spelling of its own.
const EventName = "observetest.watched"

// arrival is how long a case waits for a notice it expects before it calls the
// transport broken. It is generous because a broker on another host answers in
// milliseconds but not in microseconds; a case that waits the whole window has a
// transport that never delivered.
const arrival = 10 * time.Second

// Deps is one provider's way of building an observation environment. Every
// member is a function, because the broker provider needs a server, a slug and a
// connection per case and the in-process provider needs none of them.
type Deps struct {
	// Name names the provider in a failure.
	Name string
	// Transport returns a transport for a deployment naming app — the zero
	// appname.Name is the deployment of one app, which keeps the addresses this
	// kernel formed before the app segment — and the function that releases
	// everything it took. A case publishes and observes through this one value,
	// so what reaches a watch is only ever this transport's own address.
	Transport func(t *testing.T, app appname.Name) (transport.Transport, func())
	// Processes returns the two transports of two connections to one broker: the
	// pair the case about every process being noticed runs, and it is a function
	// because a broker answers two connections and an in-process transport answers
	// one value. A provider whose single value already is the whole broker hands
	// that value twice — the two watchers are then two readers of one bus rather
	// than two buses that never spoke. What the case checks either way is that a
	// publish is copied to every watch rather than taken by one of them.
	Processes func(t *testing.T, app appname.Name) (a, b transport.Transport, closer func())
	// Quiet is how long a case waits before it asserts a notice did NOT arrive.
	// It is the provider's own fact — a broker's fan-out latency is not something
	// a case may guess — and every case that asserts absence waits it.
	Quiet func() time.Duration
	// App is the slug the named deployment arm runs as, and the fixture namespace
	// of the provider that owns it: on a shared broker the cases publish there, so
	// the provider picks a space nothing else subscribes in and clears it the way
	// its own fixture does. The in-process provider writes no address and reads
	// this only as the app stamped into the event's own claim.
	App appname.Name
}

// Run runs every case as a subtest of t, once for a deployment that names its app
// and once for one that does not: those are two different addresses, and a
// transport that observes only one of them observes half the space. A provider's
// test file is one call to this.
func Run(t *testing.T, d Deps) {
	t.Helper()
	if d.Name == "" || d.Transport == nil || d.Processes == nil || d.Quiet == nil {
		t.Fatalf("%s: observetest.Run needs Name, Transport, Processes and Quiet", d.Name)
	}
	if !d.App.Named() {
		t.Fatalf("%s: observetest.Run needs the app slug its named arm deploys as", d.Name)
	}
	for _, app := range []appname.Name{appname.Name(""), d.App} {
		label := "an app-less deployment"
		if app.Named() {
			label = "app " + app.String()
		}
		t.Run(label, func(t *testing.T) {
			for name, run := range map[string]func(*testing.T, Deps, appname.Name){
				"AnObservationNeverReceivesAnotherTenantsEvent":       receivesOnlyItsOwnTenant,
				"TwoObservationsOnOneSubjectBothSeeOnePublish":        bothWatchesSeeOnePublish,
				"StopEndsObservationDelivery":                         stopEndsDelivery,
				"AnObservationStartedAfterAPublishReceivesNothing":    noHistory,
				"EveryConnectionToOneBrokerReceivesEveryPublishOnce":  everyProcessIsNoticed,
				"AnObservationReceivesWhatItsOwnTransportPublishesAt": publishesAndObservesOneAddress,
			} {
				t.Run(name, func(t *testing.T) { run(t, d, app) })
			}
		})
	}
}

// provider builds one case's transport and arranges for its release to run after
// every watch the case started has been withdrawn, so a case never tears the
// broker connection down from under an observation that is still holding it.
func provider(t *testing.T, d Deps, app appname.Name) transport.Transport {
	t.Helper()
	tr, release := d.Transport(t, app)
	t.Cleanup(release)
	return tr
}

// pair builds the two transports of the two-process case, released the same way.
func pair(t *testing.T, d Deps, app appname.Name) (transport.Transport, transport.Transport) {
	t.Helper()
	a, b, release := d.Processes(t, app)
	t.Cleanup(release)
	return a, b
}

// watch is one observation's counter and its inbox. The counter counts every
// call, including a notice the inbox had no room for: "at most once" is a fact
// about calls, not about channels.
type watch struct {
	calls  atomic.Int64
	events chan transport.Event
}

func newWatch() *watch { return &watch{events: make(chan transport.Event, 16)} }

func (w *watch) fn(ev transport.Event) {
	w.calls.Add(1)
	select {
	case w.events <- ev:
	default:
	}
}

// got waits for one notice, fails when none arrives, and hands back what came.
func (w *watch) got(t *testing.T, d Deps, what string) transport.Event {
	t.Helper()
	select {
	case ev := <-w.events:
		return ev
	case <-time.After(arrival):
		t.Fatalf("%s: %s delivered no notice within %s (calls so far: %d)", d.Name, what, arrival, w.calls.Load())
		return transport.Event{}
	}
}

// settles waits out the provider's settle window and fails unless exactly want
// calls have landed and no notice is still waiting to be read. Absence is only
// evidence once the window has passed, which is why the window is the provider's
// and not a guess made at the call site.
func (w *watch) settles(t *testing.T, d Deps, want int64, what string) {
	t.Helper()
	time.Sleep(d.Quiet())
	select {
	case ev := <-w.events:
		t.Errorf("%s: %s delivered %s in tenant %s, which it was not owed", d.Name, what, ev.Name, ev.TenantID)
	default:
	}
	if n := w.calls.Load(); n != want {
		t.Errorf("%s: %s ran the observation %d time(s); %d was owed", d.Name, what, n, want)
	}
}

// observe starts one watch and withdraws it when the case ends.
func observe(t *testing.T, tr transport.Transport, tenant uuid.UUID, name string) *watch {
	t.Helper()
	w := newWatch()
	stop, err := tr.Observe(t.Context(), tenant, name, w.fn)
	if err != nil {
		t.Fatalf("observe %s in tenant %s: %v", name, tenant, err)
	}
	t.Cleanup(stop)
	return w
}

// publish stamps one event the way the outbox's relay stamps it — its own id, the
// app the publisher is, the tenant it happened in, the instant it happened — and
// publishes it through tr, which is the transport's own chance to refuse an app
// that is not its own.
func publish(t *testing.T, tr transport.Transport, app appname.Name, tenant uuid.UUID) transport.Event {
	t.Helper()
	ev := transport.Event{
		ID: uuid.New(), Name: EventName, App: app, TenantID: tenant,
		At: time.Now().UTC(), Payload: json.RawMessage(`{"case":"observation"}`),
	}
	if err := tr.Publish(t.Context(), ev); err != nil {
		t.Fatalf("publish %s in tenant %s: %v", EventName, tenant, err)
	}
	return ev
}

// receivesOnlyItsOwnTenant is the tenant boundary of an address: a watch answers
// one tenant's one event, and no publish made in another tenant can reach it,
// because the tenant is a segment of the address rather than a field of the body.
func receivesOnlyItsOwnTenant(t *testing.T, d Deps, app appname.Name) {
	tr := provider(t, d, app)
	a, b := uuid.New(), uuid.New()

	inA, inB := observe(t, tr, a, EventName), observe(t, tr, b, EventName)
	publish(t, tr, app, b)
	inA.settles(t, d, 0, "a watch on the other tenant")
	inB.got(t, d, "a watch on the publishing tenant")

	publish(t, tr, app, a)
	if ev := inA.got(t, d, "a watch on the publishing tenant"); ev.TenantID != a {
		t.Errorf("%s: a watch addressed to %s delivered a notice from tenant %s", d.Name, a, ev.TenantID)
	}
	// b's watch was handed its own publish and none of a's. The counter is what
	// makes "and none of a's" a checked fact rather than a sentence.
	inB.settles(t, d, 1, "the first tenant's watch, after a publish in the other")
}

// bothWatchesSeeOnePublish is the fan-out a live connection depends on: two
// watches on one address are two notices, and neither takes the publish from the
// other.
func bothWatchesSeeOnePublish(t *testing.T, d Deps, app appname.Name) {
	tr := provider(t, d, app)
	tenant := uuid.New()
	first, second := observe(t, tr, tenant, EventName), observe(t, tr, tenant, EventName)
	ev := publish(t, tr, app, tenant)

	for i, w := range []*watch{first, second} {
		if got := w.got(t, d, "one of two watches on one address"); got.ID != ev.ID {
			t.Errorf("%s: watch %d delivered %s; this transport published %s", d.Name, i+1, got.ID, ev.ID)
		}
		w.settles(t, d, 1, "one of two watches on one address")
	}
}

// stopEndsDelivery is the withdrawal a connection takes when the browser goes
// away: nothing is delivered after it, and calling it twice is a caller closing a
// watch and then the connection that carried it.
func stopEndsDelivery(t *testing.T, d Deps, app appname.Name) {
	tr := provider(t, d, app)
	tenant := uuid.New()
	w := newWatch()
	stop, err := tr.Observe(t.Context(), tenant, EventName, w.fn)
	if err != nil {
		t.Fatalf("observe %s in tenant %s: %v", EventName, tenant, err)
	}
	publish(t, tr, app, tenant)
	w.got(t, d, "a watch that has not been stopped")

	stop()
	stop()
	publish(t, tr, app, tenant)
	w.settles(t, d, 1, "a watch after its stop")
}

// noHistory is why a client refetches before it trusts what it has been shown: an
// observation started after a publish cannot see it, so a connection made while
// the world was changing is told to resync rather than to catch up.
func noHistory(t *testing.T, d Deps, app appname.Name) {
	tr := provider(t, d, app)
	tenant := uuid.New()
	publish(t, tr, app, tenant)

	observe(t, tr, tenant, EventName).settles(t, d, 0, "a watch started after the publish")
}

// everyProcessIsNoticed is the deployment shape: two connections to one broker,
// only one of which publishes, each handed the publish once. A transport that
// delivered to one connection would leave every other replica serving streams
// that never move. The in-process provider instantiates it as two watchers on one
// bus, which is the same assertion one level down: a notice is copied to every
// watch, never taken by whichever watch asked first.
func everyProcessIsNoticed(t *testing.T, d Deps, app appname.Name) {
	a, b := pair(t, d, app)
	tenant := uuid.New()
	wa, wb := observe(t, a, tenant, EventName), observe(t, b, tenant, EventName)
	ev := publish(t, a, app, tenant)

	for i, w := range []*watch{wa, wb} {
		if got := w.got(t, d, "one of two transports over one broker"); got.ID != ev.ID {
			t.Errorf("%s: transport %d delivered %s; transport 1 published %s", d.Name, i+1, got.ID, ev.ID)
		}
		w.settles(t, d, 1, "one of two transports over one broker")
	}
}

// publishesAndObservesOneAddress is the pair the address scheme rests on: the
// subject this transport publishes at is the subject its own observation is
// filtered to. A deployment naming an app publishes and watches the scoped
// address; a deployment naming nothing publishes and watches the unscoped one.
// Either way one value decides both halves, and a transport that wrote one and
// read the other answers this case with silence.
func publishesAndObservesOneAddress(t *testing.T, d Deps, app appname.Name) {
	tr := provider(t, d, app)
	tenant := uuid.New()
	w := observe(t, tr, tenant, EventName)
	ev := publish(t, tr, app, tenant)

	if got := w.got(t, d, "a watch on the address this transport publishes at"); got.ID != ev.ID {
		t.Errorf("%s: delivered %s of tenant %s; published %s of tenant %s", d.Name, got.ID, got.TenantID, ev.ID, tenant)
	}
	w.settles(t, d, 1, "a watch on the address this transport publishes at")
}
