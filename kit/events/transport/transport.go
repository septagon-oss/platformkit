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
