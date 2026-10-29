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

// Filters is every address a subscription to one event name has to answer, in the order a
// reader checks them: this build's, then the previous build's while a publisher on that
// build is still writing events this worker reads.
//
// The second is `platformkit.<module>.<event>`, the address before the subject carried a
// tenant segment; nothing in this repository writes it, a publisher still running the
// previous build does. A NATS `*` matches exactly one token, so that message matches no
// consumer filtering the new shape, and no wildcard means "this token, or none": a rolling
// window needs two filters, not a cleverer one.
//
// The window is the length of the rollout and no longer.
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

// AddressMismatch compares the address a message arrived on with the app that reads it, the
// app the document inside it names, and the event the document claims to be, and names the
// disagreement when they are not one delivery. It returns nil when a delivery may run, and
// the document's own app is checked first, before the address, so the log says which
// boundary disagreed.
//
// The tenant is the one fact about a delivered message the routing does not pin: a
// consumer's filter spells out the module and event and leaves the tenant half a wildcard
// by design, and the envelope's own subject check proves only that the document agrees with
// itself — which a self-consistent forgery satisfies. Which of Filters' addresses pass
// depends on whether the reading app names itself; README.md, "Why a delivery checks the
// address it arrived on".
func AddressMismatch(app appname.Name, subject string, ev Event) error {
	// A Name that was set but is not a slug has no token: the address built from
	// it holds an empty segment, and both sides of a comparison built the same way
	// would agree. Refuse on the name instead — a process that names itself with
	// something a subject token cannot hold receives nothing.
	if app.Named() && !app.Valid() {
		return fmt.Errorf("events: app %q is not an app name, so no address can be shown to be its %s in tenant %s", string(app), ev.Name, ev.TenantID)
	}
	// The document's claim, before the address's. An app that names itself refuses a
	// document that names another, and an app that names nothing refuses any document
	// that names one: it has no app to agree with, exactly as it has none for an
	// address carrying an app token. A document that names nothing is the shape a
	// previous build published — accepted, and decided by the address alone, as it is
	// today, which is the rollout rather than a hole in it.
	if doc := ev.App; doc.Named() {
		if !doc.Valid() {
			return fmt.Errorf("events: message names app %q, which is not an app name, so its %s in tenant %s cannot be shown to be anyone's", string(doc), ev.Name, ev.TenantID)
		}
		if doc != app {
			return fmt.Errorf("events: message names app %s, which is not the app reading it (%s), whatever its address %q is", doc, app, subject)
		}
	} else if app.Named() {
		// An app that names itself reads only messages that name it — both copies. The refusal
		// rests on a condition: a deployment that scoped its subjects before an envelope could
		// carry the `app` member holds unstamped messages at its own scoped address, and the
		// moment it sets a slug they are terminated with their outbox rows pending. The fix is to
		// drain the stream or drop the slug, not to widen the check: the claim ledger at that
		// address is what stands between a redelivery and a handler run twice.
		return fmt.Errorf("events: message stored at %q names no app, and app %s reads only messages that name it as their %s in tenant %s", subject, app, ev.Name, ev.TenantID)
	}
	if subject == appname.Subject(app, ev.TenantID, ev.Name) {
		return nil
	}
	if !app.Named() && subject == legacyAddress(ev.Name) {
		return nil
	}
	return fmt.Errorf("events: message stored at %q is not app %s's %s in tenant %s, whose address is %q",
		subject, app, ev.Name, ev.TenantID, appname.Subject(app, ev.TenantID, ev.Name))
}
