// subject.go is where an event's address is written down once. The envelope's
// CloudEvents `subject`, the broker's publish subject and a consumer's filter
// all come from the functions here, so a bridge that reads the address out of a
// document can subscribe to it and get the same messages. Nothing in this file
// reads an address back into an event name: nothing in the program needs to —
// the relay takes the name from the outbox row it is carrying, and a consumer's
// filter is derived from the name it subscribed with, never from a subject.
// What an address is *checked against* is AddressMismatch, the one place the
// two copies of an event's address — the one the broker routed and the one the
// document carries — are compared.
package transport

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// SubjectPrefix is the one namespace every PlatformKit event is published
// under. A deployment that wants its own prefix on the broker gets it by
// renaming the stream, not by editing this: the envelope says which program
// produced it, and every consumer's filter is derived from here.
const SubjectPrefix = appname.Prefix

// Subject is the address of one event:
//
//	platformkit.<tenant>.<module>.<event>
//
// The tenant segment is the point of the whole scheme (decision 0053 §1): a
// durable can be per tenant, one tenant's poison backlog cannot hold another's
// delivery behind it, and a bridge can route a customer's events by address
// rather than by opening the payload. The module and event halves are the
// event's Name, which is why Subject takes a name and not a second grammar.
func Subject(tenantID uuid.UUID, name string) string {
	return appname.PreviousSubject(tenantID, name)
}

// AppSubject is the address of one event of one app:
//
//	platformkit.<app>.<tenant>.<module>.<event>
//
// It is the address a process that names its app publishes at, and the reason the
// app token sits before the tenant is the two wildcards: a subscription filters
// its own app exactly and every tenant of it loosely, and no NATS filter can say
// "this tenant, any app" — which is right, because a subscription always belongs
// to one app. A process with no slug set publishes Subject instead, the address
// this kernel formed before decision 0074, which Filters keeps reading.
func AppSubject(app appname.Name, tenantID uuid.UUID, name string) string {
	return appname.Subject(app, tenantID, name)
}

// Filter is the wildcard a subscription to one event name uses: every tenant's
// delivery of that event, and no other event.
//
// It is a wildcard rather than one consumer per tenant because a consumer per
// tenant per event is a consumer count that grows with the customer base and
// drains nothing else: the outbox claim already makes one tenant's handler run
// once. What the segment buys is the *option* — an operator who needs a
// tenant's own backlog filters Subject(tenant, name), an exact subject rather
// than this wildcard, and the address space has held the tenant since this
// function landed.
func Filter(name string) string {
	return appname.PreviousFilter(name)
}

// AppFilter is one app's wildcard for one event name: that event, in every tenant
// of that app, and no other app's. With no slug set it is Filter.
func AppFilter(app appname.Name, name string) string {
	return appname.Filter(app, name)
}

// Filters is every address a subscription to one event name has to answer, in
// the order a reader checks them: this build's, then the previous build's while
// a publisher on that build is still writing events this worker reads.
//
// The second is `platformkit.<module>.<event>` — the address before the subject
// carried a tenant segment. Nothing in this repository writes to it: the
// envelope never produces the old shape (see cloudevents.go); a publisher still
// running the previous build does. A NATS `*` matches exactly one token, so that
// message matches no consumer filtering `platformkit.*.<module>.<event>`, and
// there is no wildcard that means "this token, or none" and a stream subject
// transform cannot be added to a stream that already exists: a rolling window
// needs two filters and not a cleverer one.
//
// The window is the length of the rollout and no longer: the day no process
// running the previous build publishes, this list shrinks back to Filter alone
// and the consumer is remade without the second filter. Until then it is the
// only reason a pre-envelope message reaches a handler at all — the decoder
// that reads the old shape is unreachable without it, and an event the relay
// stamped published while nothing consumed it is the loss this whole change set
// exists to prevent, in a new place.
func Filters(name string) []string {
	return appname.Filters(appname.Name(""), name)
}

// AppFilters is Filters for a process that names its app: its own scoped address
// first, then both older shapes, so a rolling upgrade reads what a previous build
// published and no app reads another's scoped traffic — the scoped address carries
// the app token and the older filters carry one more or one fewer token than it,
// so no previous-shape filter matches a scoped subject.
func AppFilters(app appname.Name, name string) []string {
	return appname.Filters(app, name)
}

// legacyAddress is the address the build before the tenant segment published
// at: `platformkit.<module>.<event>`, three tokens. Nothing here writes it (see
// cloudevents.go: the envelope never produces the old shape); a publisher still
// running that build does, for the length of the window Filters describes.
//
// What matters about it is what it does not carry: it names no tenant. That is
// why it is a second address an event can legitimately arrive at rather than a
// forgery of the first — there is no tenant in it to disagree with the document.
func legacyAddress(name string) string { return appname.OldestSubject(name) }

// AddressMismatch compares the address a message arrived on with the event the
// document inside it claims to be, and names the disagreement when the two are
// not the same event in the same tenant. It returns nil when a delivery may run.
//
// Why a delivery needs this at all: a consumer's filter fixes the module and
// event halves of the subject, because it spells them out and a NATS `*` matches
// exactly one token, but its tenant half is a wildcard by design (Filter says
// why). So the tenant is the one fact about a delivered message the routing does
// not pin, and the envelope's own check that `subject` is Subject of `tenantid`
// and `type` proves only that the document agrees with itself — which is exactly
// what a self-consistent forgery satisfies. Without this comparison, a message
// stored on tenant A's address and stamped as tenant B's would open its
// transaction inside B's rows because its body said so: a tenant boundary held up
// by convention, with a broker credential as its key. With it, the address the
// broker actually routed by is a fact the delivery reads, and the two tenants
// have to be one tenant.
//
// Both of Filters' addresses pass. The one that names a tenant has to name this
// event's; the previous build's names none, so it contradicts nothing — refusing
// it would terminate the rollout window's events, which is the loss this file's
// own second filter exists to prevent.
func AddressMismatch(subject string, ev Event) error {
	switch subject {
	case Subject(ev.TenantID, ev.Name), legacyAddress(ev.Name):
		return nil
	}
	return fmt.Errorf("events: message stored at %q is not %s in tenant %s, whose address is %q",
		subject, ev.Name, ev.TenantID, Subject(ev.TenantID, ev.Name))
}
