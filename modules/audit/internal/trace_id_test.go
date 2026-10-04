package internal_test

// The trail row names the trace that wrote it — the join an operator uses to go from
// a line of audit history to the request in the trace backend, which is why the trail
// keeps a trace at all. The value arrives on the envelope as W3C's traceparent
// (kit/events stores it there from the publishing transaction, migrations/000035, and
// migrations/000037 indexes its second field) and the contract's row hands over the id
// inside that one string as a uuid, so neither a query nor a caller has to parse a
// header to make the join.
//
// What is checked here is therefore both halves of a rule the brief states as one:
// that a traced event's row carries the id, and that the three ways an event can
// arrive untraced leave NULL rather than a zero — "no trace" and "the trace whose id
// is all zeros" are different facts, and a trail full of the latter would make the
// column useless as a join.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// The W3C form is "version-traceid-parentid-flags"; only the middle field matters to
// the column, which is what these four cases vary.
const (
	tracedHex      = "4bf92f3577b34da6a3ce929d0e0e4736"
	zeroTraceHex   = "00000000000000000000000000000000"
	unparsableHex  = "zbf92f3577b34da6a3ce929d0e0e4736"
	parentAndFlags = "-00f067aa0ba902b7-01"
)

// TestTheTrailRowNamesTheTraceThatWroteIt: one event with a real trace context, one
// with none, one whose trace id is all zeros, and one whose trace id is not hex — read
// back through the contract's own read path rather than by re-running the insert.
func TestTheTrailRowNamesTheTraceThatWroteIt(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	traced := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(),
		Payload:     []byte(`{"title":"chiller-2"}`),
		TraceParent: "00-" + tracedHex + parentAndFlags,
		TraceState:  "rojo=00f067aa0ba902b7"}
	untraced := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(), Payload: []byte(`{}`)}
	zeroed := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(),
		Payload: []byte(`{}`), TraceParent: "00-" + zeroTraceHex + parentAndFlags}
	malformed := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(),
		Payload: []byte(`{}`), TraceParent: "00-" + unparsableHex + parentAndFlags}

	all := []events.Event{traced, untraced, zeroed, malformed}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, ev := range all {
			if err := svc.Record(ctx, tx, ev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("record four events: %v", err)
	}

	var rows []*contracts.Event
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var total int64
		var err error
		rows, total, err = svc.List(ctx, tx, contracts.Query{})
		if err == nil && total != int64(len(all)) {
			t.Errorf("the trail holds %d rows, want %d", total, len(all))
		}
		return err
	})
	if err != nil {
		t.Fatalf("list the trail: %v", err)
	}

	byEvent := map[uuid.UUID]*contracts.Event{}
	for _, row := range rows {
		byEvent[row.EventID] = row
	}
	// A row that is not there is its own answer: indexing the map and dereferencing
	// would panic over a broken trace id instead of saying which row went missing.
	rowOf := func(ev events.Event) *contracts.Event {
		t.Helper()
		row, ok := byEvent[ev.ID]
		if !ok {
			t.Fatalf("the trail holds no row for event %s (%s)", ev.Name, ev.ID)
		}
		return row
	}
	got := rowOf(traced).TraceID
	if got == nil {
		t.Fatal("the traced event's row carries no trace id, so the trail cannot be joined to the backend")
	}
	if *got != uuid.MustParse("4bf92f35-77b3-4da6-a3ce-929d0e0e4736") {
		t.Errorf("the row names trace %s, want the publisher's %s", *got, tracedHex)
	}
	for _, ev := range []events.Event{untraced, zeroed, malformed} {
		if id := rowOf(ev).TraceID; id != nil {
			t.Errorf("an event with no traceable context (traceparent %q) was stored with trace id %s, want NULL",
				ev.TraceParent, id)
		}
	}
}
