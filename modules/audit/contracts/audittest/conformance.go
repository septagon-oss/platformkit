// Package audittest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// The suite is the specification of the trail written as executable cases: the
// real service and the fake both run it, so "the fake behaves like the real
// thing" is a test result rather than a hope (AGENTS.md rule 8).
package audittest

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a way to see what was published.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Published is the names of the events published so far, in order. This
	// module publishes none — an audit of audits is a loop — so every case that
	// checks it is checking silence, which is the one thing a return value
	// cannot show.
	Published func() []string
}

// silent runs step and fails if it published anything.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; recording what happened is not itself something that happened", what, after[before:])
	}
}

// Harness builds one Fixture and calls run with it. It is written this way
// round because the real service's fixture is a transaction, and a transaction
// is a scope somebody has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// ada is the person the suite records actions for, and noon is a fixed instant
// the range filters are asked about either side of.
var (
	ada  = uuid.New()
	noon = db.Now().Truncate(time.Hour)
)

// event is one envelope, of the shape kit/events.Relay hands a subscriber.
func event(name string, actor uuid.UUID, at time.Time) events.Event {
	return events.Event{
		ID: uuid.New(), Name: name, TenantID: uuid.New(),
		Payload: json.RawMessage(`{"n":1}`), At: at, Actor: actor,
	}
}

// compact is a payload with its whitespace removed, because a jsonb column
// stores a document rather than the bytes it arrived as: the trail keeps what
// the module published and not the spacing it published it with.
func compact(t *testing.T, payload []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, payload); err != nil {
		t.Fatalf("the payload is not JSON: %s", payload)
	}
	return out.String()
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"an event becomes a row saying what happened, who caused it and when": recordEvent,
		"work nobody asked for is recorded as nobody's":                       recordUnattributedWork,
		"the same event twice leaves one row":                                 recordDuplicateEvent,
		"a row belongs to the transaction's tenant, not to the envelope":      recordTransactionTenant,
		"the trail is filtered by name, by actor and by time":                 filterTrail,
		"the trail is filtered by the row an event is about":                  filterRecordTrail,
		"an unknown id is not found":                                          unknownAuditRow,
	}
}
