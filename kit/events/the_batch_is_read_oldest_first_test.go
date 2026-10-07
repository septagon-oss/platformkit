package events_test

// Review round 4 (T-0110). This case pins the half of `797272b fix(events)` that its own
// test cannot reach.
//
// That commit made `pkit.outbox.lag` record one value per (tenant, event) series, on the
// first row of it, and `kit/events/relay_lag_test.go` pins both halves of that choice —
// how many rows are recorded and which of them — over `rows` that the test itself hands
// over, sorted the way the relay is *assumed* to read them. The assumption lives in SQL:
// "the batch is read ORDER BY created_at, so the first row of a series is the oldest row
// the queue was holding" (kit/events/relay.go). Nothing in this repository puts a case on
// that ORDER BY. `TestEventsFromOneTransactionRelayInTheOrderTheyWerePublished` inserts its
// rows oldest-first, so a batch read with no ORDER BY at all comes back in the order the
// rows were inserted, which is the order it asserts, and the case passes either way.
//
// So this case inserts the queue in the other order. Three rows of one event name, whose
// physical order in the table is newest-first: with the ORDER BY the relay takes them
// oldest-first and the number names the longest wait the queue is holding; without it the
// pass runs newest-first and the same code reports the shortest wait as the queue's lag —
// which is the reading round 3 measured at 10 seconds about a queue five minutes deep. The
// assertion is on what the transport was handed, in the order it was handed it, so it fails
// at the query and not at a comment.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
)

// TestRelayReadsItsBatchOldestFirst: the queue's own order, asserted where only the
// database can answer it.
func TestRelayReadsItsBatchOldestFirst(t *testing.T) {
	admin, conn := dbtest.Schema(t)

	const name = "billing.invoice_issued"
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	// Inserted newest row first, so the table's physical order contradicts the order the
	// queue means. A batch read without ORDER BY created_at returns these in the order they
	// were written — the three waits backwards — and the relay's own comment stops being
	// true of the rows it is holding.
	ages := []string{"10 seconds", "2 minutes", "5 minutes"}
	for i, id := range ids {
		_, err := admin.ExecContext(t.Context(),
			`INSERT INTO platformkit_outbox (id, tenant_id, name, payload, created_at)
			 VALUES ($1, $2, $3, '{}'::jsonb, now() - interval `+"'"+ages[i]+"'"+`)`,
			id, acme.ID, name)
		if err != nil {
			t.Fatalf("insert the row %d of the queue: %v", i, err)
		}
	}

	r := &recorder{}
	if err := events.Relay(t.Context(), conn, r); err != nil {
		t.Fatalf("relay the three-row queue: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != len(ids) {
		t.Fatalf("the transport was handed %d events, want the three rows", len(r.got))
	}
	for i, ev := range r.got {
		want := ids[len(ids)-1-i]
		if ev.ID != want {
			t.Errorf("the relay took row %d of the pass as %s, want %s (the row %s old); a relay pass "+
				"that is not oldest-first both delays the oldest event behind newer ones and leaves "+
				"pkit.outbox.lag holding the shortest wait in the batch, because oldestPerSeries records "+
				"the first row it is given of each series and the ORDER BY is what makes that row the "+
				"oldest one the queue was holding",
				i, ev.ID, want, ages[len(ages)-1-i])
			return
		}
	}
}
