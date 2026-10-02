package events_test

// One process holds one shape per event name, and it holds it for as long as a
// composition that declared the name is live. Two compositions can be live at once
// — one application under two roles, or two applications on two databases — and
// whichever of them booted second may neither replace a shape the first is answering
// publishes under nor take it away on the way out. DeclareMore is the door both
// halves answer at: it installs beside what stands, refuses a name the process
// already means something else by, and returns the release of what it added.
//
// The comparison is the projection rather than the Go type: two modules with their
// own struct for one document declare one event, and refusing them would refuse the
// process for a coincidence of naming. A declared type and no declared type are
// never one shape.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type postedNumber struct {
	Number int64 `json:"number"`
}

// postedSameShape is another module's own struct for the same document: one event,
// because the projection of the two is one schema.
type postedSameShape struct {
	Number int64 `json:"number"`
}

// postedText spells the same name with a payload that is not the same document.
type postedText struct {
	Number string `json:"number"`
}

func TestDeclarationsOfTwoLiveCompositions(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	t.Cleanup(func() { events.DeclareAll(nil) })
	publish := func(body any) error {
		return db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "ledger.posted", body)
		})
	}
	posted := map[string]any{"number": "not a number"}
	countRows := func() int {
		var n int
		if err := admin.QueryRowContext(t.Context(),
			"SELECT count(*) FROM platformkit_outbox WHERE name = 'ledger.posted'").Scan(&n); err != nil {
			t.Fatalf("count the posted events: %v", err)
		}
		return n
	}

	first, err := events.DeclareMore([]events.Declared{events.Declare[postedNumber]("ledger.posted")})
	if err != nil {
		t.Fatalf("the first composition was refused: %v", err)
	}
	if first == nil {
		t.Fatal("DeclareMore returned no release for a composition that installed declarations")
	}
	t.Cleanup(first)
	if err := publish(posted); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Fatalf("the first declaration installed nothing: %v", err)
	}

	// A second composition may declare the same name with the same shape — the
	// kernel's own events are declared by every composition in the process, twice
	// over in a rolling restart — and its release must not take the name away from
	// the composition that declared it first.
	second, err := events.DeclareMore([]events.Declared{events.Declare[postedSameShape]("ledger.posted")})
	if err != nil {
		t.Fatalf("a second composition declaring one shape was refused: %v", err)
	}
	if second == nil {
		t.Fatal("DeclareMore returned no release for a composition that named a standing event")
	}
	second()
	if err := publish(posted); err == nil || !strings.Contains(err.Error(), "the payload is not what the module declared") {
		t.Errorf("releasing the second composition took the shape the first one is answering under away: %v", err)
	}

	// The one disagreement is refused, and refused having installed nothing: the
	// standing shape still decides what the outbox takes, which is what an answer
	// that wrote no part of a new contract has to look like.
	if err := events.CheckDeclared([]events.Declared{events.Declare[postedText]("ledger.posted")}); err == nil ||
		!strings.Contains(err.Error(), "ledger.posted") {
		t.Errorf("a second spelling of a standing event name was not refused by name: %v", err)
	}
	clash, err := events.DeclareMore([]events.Declared{events.Declare[postedText]("ledger.posted")})
	if err == nil {
		if clash != nil {
			defer clash()
		}
		t.Fatal("a composition declaring another shape for a standing event name started")
	}
	if !strings.Contains(err.Error(), "ledger.posted") || !strings.Contains(err.Error(), "integer") {
		t.Errorf("the refusal did not name the event and the shape it refuses: %v", err)
	}
	if err := publish(map[string]any{"number": int64(7)}); err != nil {
		t.Errorf("the refused composition changed what the standing shape accepts: %v", err)
	}
	if got := countRows(); got != 1 {
		t.Errorf("the accepted publish wrote %d rows, want 1", got)
	}

	// The last grip is what ends the shape: with nothing live declaring the name,
	// the process checks nothing under it again, and a composition is free to mean
	// something else by it.
	first()
	if err := publish(posted); err != nil {
		t.Errorf("the shape of a name no live composition declared still refused a payload: %v", err)
	}
	if got := countRows(); got != 2 {
		t.Errorf("the unchecked publish wrote %d rows on top of the one from the accepted call, want 2", got)
	}
	if err := events.CheckDeclared([]events.Declared{events.Declare[postedText]("ledger.posted")}); err != nil {
		t.Errorf("a name nothing declares was refused to a new composition: %v", err)
	}
}
