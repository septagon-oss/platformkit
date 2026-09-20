// Package transport describes committed event delivery without a database or broker.
// Publishers and sinks own durable state, tenant authorization and idempotency;
// importing these contracts establishes none of those guarantees by itself.
package transport

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Event is one thing that happened in one tenant.
//
// The JSON tags below are the shape this package published before it adopted
// CloudEvents 1.0 and still decodes; the envelope in cloudevents.go is what
// encoding/json writes now, so these tags describe the old wire form only.
type Event struct {
	// ID is the deduplication key. A handler that has already seen it has
	// already done the work when its sink claims deliveries transactionally.
	ID uuid.UUID `json:"id"`
	// Name is "<module>.<something>", the module's namespace first.
	Name string `json:"name"`
	// TenantID is the tenant the event happened in. The outbox's Consume opens
	// a transaction in it; transports do not establish tenant isolation.
	TenantID uuid.UUID `json:"tenantId"`
	// Payload is whatever the publisher marshalled.
	Payload json.RawMessage `json:"payload"`
	// At records when the state changed; the outbox uses its row's creation time.
	At time.Time `json:"at"`
	// Actor is the user whose request caused this, and the nil UUID when
	// nothing did: a periodic job, the relay, a handler reacting to another
	// event. The publishing owner supplies this trusted fact. The PlatformKit
	// outbox reads it from kit/tenancy's context rather than request input.
	Actor uuid.UUID `json:"actor"`
	// TraceParent and TraceState are the W3C trace context of the work that
	// published this, carried on the wire as the CloudEvents distributed tracing
	// extension attributes. They are the reason a handler's span is a child of
	// the request that caused the event rather than an island in the worker:
	// the outbox row keeps them, so the trace crosses the database and the
	// broker, and kit/events reads them back when it runs a handler. A
	// publisher with no span leaves them empty and the delivery starts a trace
	// of its own.
	TraceParent string `json:"-"`
	TraceState  string `json:"-"`
	// Attempt is which try at this delivery this is, 1 on the first. It is
	// transport state, like an acknowledgement, so it is never on the wire: an
	// adapter counts it if its broker counts it and leaves it 0 otherwise, and
	// the delivery span says nothing about attempts rather than inventing one.
	Attempt int `json:"-"`
}

// eventName is the grammar of an event name: the module's name, a dot, and a
// lower-case path. kit/module checks a manifest's Events with ValidName, so the
// grammar exists once and a name that passes review is a name Publish accepts.
var eventName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ValidName reports whether name is a well-formed event name.
func ValidName(name string) bool { return eventName.MatchString(name) }

// Declared is one event name and the Go type of its payload.
//
// It is a declaration and not a constraint: Publish still takes any value, and
// nothing on the delivery path reads these. What they buy is a document that
// can say what an event contains — the composition description, and the
// AsyncAPI and Backstage documents projected from it — which a bare name
// cannot. A module that declares none is not broken; its events are simply
// documented as carrying whatever the publisher sent.
type Declared struct {
	Name string
	Type reflect.Type
}

// Declare pairs an event name with the type of its payload at the place both
// are already written: Declare[contracts.Assigned](contracts.EventAssigned).
//
// The type has to be the one Publish is handed for that name — nothing here
// can check a call site it cannot see — so a module declares payloads beside
// the events it emits and its contracts test names the pair. The two drift
// apart silently otherwise: an event whose document describes the wrong record
// is worse than one with no document.
func Declare[T any](name string) Declared {
	// The pointer-to-nil idiom rather than reflect.TypeOf(zero T): a nil T of
	// interface type would report nothing, and a payload named by an interface
	// is still named.
	return Declared{Name: name, Type: reflect.TypeOf((*T)(nil)).Elem()}
}

// Transport carries committed events. Publish may return nil only after the
// event is durably accepted by the broker or every local subscription has
// completed handling or terminal recording. An error leaves the outbox pending.
type Transport interface {
	Publish(ctx context.Context, ev Event) error
	// Subscribe delivers every event called name to sink until ctx is done.
	// durable names the subscription. Durable brokers resume its stored state;
	// local providers need a caller-owned replay source after process restart.
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

// SystemNamer is an optional Transport method that names the messaging system it
// delivers through, in the vocabulary the OpenTelemetry messaging conventions
// use: "nats", "memory". It is a question only the adapter can answer — the
// outbox holds a Transport, and which one it was given is the composition's
// decision, not something a Go type or a configuration key can be trusted to
// still agree with at delivery time — and it is optional for the same reason an
// injected transport is: an adapter that does not name itself leaves the
// attribute off the delivery span instead of guessing at it.
type SystemNamer interface {
	MessagingSystem() string
}
