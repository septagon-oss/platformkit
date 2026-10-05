// Package transport describes committed event delivery without a database or broker.
// Publishers and sinks own durable state, tenant authorization and idempotency;
// importing these contracts establishes none of those guarantees by itself.
package transport

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// Event is one thing that happened in one tenant.
//
// It carries no JSON tags on purpose: the wire form is CloudEvents 1.0 and
// lives in cloudevents.go, where the envelope's members and their names are
// written once. The tags this struct used to carry were a second, private wire
// format nobody outside this repository could read without importing it.
type Event struct {
	// ID is the deduplication key. A handler that has already seen it has
	// already done the work when its sink claims deliveries transactionally.
	ID uuid.UUID
	// Name is "<module>.<something>", the module's namespace first. It is the
	// envelope's `type`; the address it travels on is Subject of this name and
	// TenantID, so the tenant is in the address and not only in the body.
	Name string
	// App is the slug of the app whose process published this, and the empty Name
	// the deployment of one app. It is the envelope's `app` extension, and the
	// address the message travels on is built from the same value — the reason both
	// exist is that an address can only say what a publisher claimed, so a delivery
	// that reads the address alone is holding a document that agrees with itself,
	// which is exactly what a self-consistent forgery satisfies. AddressMismatch is
	// where the two copies are compared; the publisher stamps this half, and refuses
	// to publish an event that names another app from a process that is not it.
	App appname.Name
	// TenantID is the tenant the event happened in. The outbox's Consume opens
	// a transaction in it; transports do not establish tenant isolation. The
	// envelope requires it, where CloudEvents leaves an extension optional:
	// an event with no tenant has no transaction to deliver it in.
	TenantID uuid.UUID
	// Payload is whatever the publisher marshalled. It is the envelope's data.
	Payload json.RawMessage
	// At records when the state changed; the outbox uses its row's creation time.
	At time.Time
	// Actor is the user whose request caused this, and the nil UUID when
	// nothing did: a periodic job, the relay, a handler reacting to another
	// event. The publishing owner supplies this trusted fact. The PlatformKit
	// outbox reads it from kit/tenancy's context rather than request input.
	Actor uuid.UUID
	// TraceParent and TraceState are the W3C Distributed Tracing context of
	// the request that caused the event, carried from the publisher through the
	// outbox row to the delivery. Empty is the normal case: a periodic job, the
	// relay itself and a handler reacting to another event have no request to
	// inherit. kit/trace owns the format; nothing here parses it.
	TraceParent string
	TraceState  string
	// Baggage is the W3C correlation member of the same propagation set, and it
	// is the request id the router wrote on the publisher's context. The trace
	// members above say which span this event happened under; this says which
	// request — the one string an operator can quote from a response header, a
	// log line and a customer's ticket. It is spelled as W3C spells it and absent
	// when the publisher had nothing to leave, exactly as the pair above: the
	// envelope carries it as a PlatformKit extension attribute beside the two the
	// distributed tracing extension names, and it reaches the delivery's span and
	// every span the handler opens below it. See migrations/000041.
	Baggage string
	// RequestID is the id the call was answered with (X-Request-ID), and ClientIP
	// is the peer address of the connection it arrived on — never a header a
	// client could write. They arrive with the event because they are readable
	// only while the request is open: the publisher copies them from kit/request
	// into its outbox row, and the relay reads them back onto the delivery. Empty
	// is the normal case, and it means the same thing an empty TraceParent
	// means: no request caused this, so there is no call to name.
	//
	// RequestID holds the same fact as Baggage in the other form: Baggage is the
	// trace's correlation member spelled as W3C spells one (`pkit.request_id=<id>`),
	// put back on the handler's context so its spans name the request; RequestID
	// is the bare id, which is what the audit trail answers "which call" with,
	// without parsing a header. One write, two readers.
	RequestID string
	ClientIP  string
}

// eventName is the grammar of an event name: the module's name, a dot, and a
// lower-case path. kit/module checks a manifest's Events with ValidName, so the
// grammar exists once and a name that passes review is a name Publish accepts.
var eventName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ValidName reports whether name is a well-formed event name.
func ValidName(name string) bool { return eventName.MatchString(name) }

// Transport carries committed events. Publish may return nil only after the
// event is durably accepted by the broker or every local subscription has
// completed handling or terminal recording. An error leaves the outbox pending.
type Transport interface {
	Publish(ctx context.Context, ev Event) error
	// Subscribe delivers every event called name to sink until ctx is done.
	// durable names the subscription. Durable brokers resume its stored state;
	// local providers need a caller-owned replay source after process restart.
	//
	// A transport that routes by an address the event does not travel inside —
	// a subject, a topic, a queue name — checks that address against the
	// document before its sink runs and terminates a mismatch
	// (AddressMismatch). Its filter fixes the event's name and cannot fix its
	// tenant, so a transport that skipped the check would open the handler's
	// transaction in whatever tenant the message's body claimed.
	Subscribe(ctx context.Context, durable, name string, sink Sink) error
}

// Sink handles a delivery or records its terminal failure. Dead must return
// persistence failures: a transport must retain recovery work until it succeeds.
// A transactional sink finishes the subscription's claim in the same transaction
// as its work, so acknowledgment loss does not repeat committed database handling.
// External effects still require provider idempotency when a transaction fails.
type Sink struct {
	Handle func(ctx context.Context, ev Event) error
	Dead   func(ctx context.Context, ev Event, cause error) error
}
