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
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
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
		"an event becomes a row saying what happened, who caused it and when": func(t *testing.T, f Fixture) {
			ev := event("task.task.created", ada, noon)
			f.silent(t, "Record", func() {
				if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
					t.Fatalf("Record: %v", err)
				}
			})
			rows, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if total != 1 || len(rows) != 1 {
				t.Fatalf("the trail holds %d rows, want one", total)
			}
			got := rows[0]
			switch {
			case got.Name != ev.Name:
				t.Errorf("name is %q, want %q", got.Name, ev.Name)
			case got.EventID != ev.ID:
				t.Errorf("eventId is %s, want the event's own id %s", got.EventID, ev.ID)
			case got.Actor == nil || *got.Actor != ada:
				t.Errorf("actor is %v, want %s", got.Actor, ada)
			case !got.OccurredAt.Equal(noon):
				t.Errorf("occurredAt is %s, want when the state changed, %s", got.OccurredAt, noon)
			case compact(t, got.Payload) != compact(t, ev.Payload):
				t.Errorf("payload is %s, want what the module published: %s", got.Payload, ev.Payload)
			}
			back, err := f.Service.Get(f.Ctx, f.Tx, got.ID)
			if err != nil || back.EventID != ev.ID {
				t.Errorf("Get = %v, %v; want the row List returned", back, err)
			}
		},

		"work nobody asked for is recorded as nobody's": func(t *testing.T, f Fixture) {
			if err := f.Service.Record(f.Ctx, f.Tx, event("task.sla_breached", uuid.Nil, noon)); err != nil {
				t.Fatalf("Record: %v", err)
			}
			rows, _, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(rows) != 1 || rows[0].Actor != nil {
				t.Errorf("a job's event is credited to %v; nobody asked for it", rows[0].Actor)
			}
		},

		"the same event twice leaves one row": func(t *testing.T, f Fixture) {
			ev := event("user.invited", ada, noon)
			for range 3 {
				if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
					t.Fatalf("Record: %v", err)
				}
			}
			// The kernel claims each delivery, so this is the second lock, for
			// what the first cannot cover: a replayed outbox row, or a handler
			// that failed after writing.
			if _, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{}); err != nil || total != 1 {
				t.Errorf("three deliveries of one event left %d rows (%v), want one", total, err)
			}
		},

		"a row belongs to the transaction's tenant, not to the envelope": func(t *testing.T, f Fixture) {
			// The envelope names a tenant, and Consume opened this transaction
			// in it, so the two always agree in production. Taking the tenant
			// from the transaction anyway is what every other write here does,
			// and it is the difference between an isolation the database
			// enforces and one a struct field asks for.
			if err := f.Service.Record(f.Ctx, f.Tx, event("tenant.created", ada, noon)); err != nil {
				t.Fatalf("Record: %v", err)
			}
			if _, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{}); err != nil || total != 1 {
				t.Errorf("the row landed somewhere else: %d rows, %v", total, err)
			}
		},

		"the trail is filtered by name, by actor and by time": func(t *testing.T, f Fixture) {
			bob := uuid.New()
			for _, ev := range []events.Event{
				event("task.task.created", ada, noon.Add(-2*time.Hour)),
				event("task.task.created", bob, noon),
				event("user.invited", ada, noon.Add(2*time.Hour)),
			} {
				if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
					t.Fatalf("Record: %v", err)
				}
			}
			// Since is inclusive and Until exclusive, so the row at noon
			// belongs to exactly one of the two and the halves do not overlap.
			for _, c := range []struct {
				what string
				q    contracts.Query
				want int64
			}{
				{"by name", contracts.Query{Name: "task.task.created"}, 2},
				{"by actor", contracts.Query{Actor: ada}, 2},
				{"since noon", contracts.Query{Since: noon}, 2},
				{"until noon", contracts.Query{Until: noon}, 1},
				{"the hour around noon", contracts.Query{Since: noon, Until: noon.Add(time.Hour)}, 1},
			} {
				rows, total, err := f.Service.List(f.Ctx, f.Tx, c.q)
				if err != nil {
					t.Fatalf("List %s: %v", c.what, err)
				}
				if total != c.want {
					t.Errorf("List %s = %d rows, want %d", c.what, total, c.want)
				}
				if int64(len(rows)) != total {
					t.Errorf("List %s returned %d rows for a total of %d", c.what, len(rows), total)
				}
			}
			// Newest first, because a trail is read from the end.
			rows, _, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(rows) != 3 || rows[0].Name != "user.invited" {
				t.Errorf("the trail starts with %v, want the newest row first", rows)
			}
			// And a page is a page.
			if rows, _, err = f.Service.List(f.Ctx, f.Tx, contracts.Query{Limit: 1, Offset: 1}); err != nil ||
				len(rows) != 1 || rows[0].Name != "task.task.created" {
				t.Errorf("the second row of the trail = %v, %v", rows, err)
			}
		},

		"an unknown id is not found": func(t *testing.T, f Fixture) {
			if _, err := f.Service.Get(f.Ctx, f.Tx, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Get of a row nobody has = %v, want ErrNotFound", err)
			}
		},
	}
}
