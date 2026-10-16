package events

// The other half of the queue's number. `797272b` made a relay pass record the wait of its
// oldest row per (tenant, event) series; this file is about what happens to that series
// afterwards.
//
// A gauge keeps the value it was last given, so a series whose last row went out keeps
// reporting that row's wait until the same tenant publishes that same event name again.
// The relay runs once a second in the worker role and is the thing that can say otherwise,
// by writing a zero for a series it reported and can no longer find a row of — but only
// for a series that has really drained: writing the zero for one whose rows another worker
// is holding would report a backlog clear while its rows are waiting, which is the same
// number lying in the other direction.
//
// The end-to-end reading is pinned by the review's case in kit/events/lagobserved, which
// owns that binary's one meter reader. What these two cases own is the choice behind it —
// which series a pass owes the zero to — because that choice is what decides whether the
// number ever says "not behind at all" and whether it ever says it too early.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func keys(lag ...lagKey) map[lagKey]struct{} {
	m := make(map[lagKey]struct{}, len(lag))
	for _, k := range lag {
		m[k] = struct{}{}
	}
	return m
}

// TestDrainedSeriesOwesTheZeroToNoSeriesStillHeldOrStillWaiting: three series an earlier
// pass reported, of which this pass holds one row of one and the queue still holds a row of
// another. Only the third is owed the correction.
func TestDrainedSeriesOwesTheZeroToNoSeriesStillHeldOrStillWaiting(t *testing.T) {
	acme, globex := uuid.New(), uuid.New()
	relayed := lagKey{tenant: acme, event: "billing.invoice_issued"}
	drained := lagKey{tenant: acme, event: "user.invited"}
	heldElsewhere := lagKey{tenant: globex, event: "billing.invoice_issued"}

	got := drainedSeries(keys(relayed, drained, heldElsewhere), keys(relayed), keys(heldElsewhere))
	if len(got) != 1 || got[0] != drained {
		t.Errorf("drainedSeries chose %v, want only %v: the series in the batch is about to be recorded "+
			"from its own row, and the one the queue still holds a row of is behind by the wait it already "+
			"reports — correcting either is a gauge that says a backlog has drained while it has not",
			got, drained)
	}
	if got := drainedSeries(keys(relayed), keys(relayed), nil); got != nil {
		t.Errorf("a pass that holds the only reported series is owed %v, want nothing", got)
	}
	if got := drainedSeries(nil, nil, nil); got != nil {
		t.Errorf("a queue nothing has ever been reported for is owed %v, want nothing: this is the state a "+
			"queue that has been idle since its last drain sits in, and it is why that state costs no read",
			got)
	}
}

// TestASeriesKeepsItsNumberUntilItsRowsAreGone puts the same choice in front of the
// database: a remembered series whose row is still unpublished is left alone — another
// worker may be publishing it this moment — and is corrected, once, after the row goes.
func TestASeriesKeepsItsNumberUntilItsRowsAreGone(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	// Work this case's ledger alone: the process's ledger also holds series other cases
	// relayed, and this case has no business deciding what they are owed.
	ledger := new(lagLedger)
	guard := reportedSeries
	reportedSeries = ledger
	defer func() { reportedSeries = guard }()

	tenant := uuid.New()
	const name = "billing.invoice_issued"
	series := lagKey{tenant: tenant, event: name}
	_, err := admin.ExecContext(t.Context(),
		`INSERT INTO platformkit_outbox (id, tenant_id, name, payload, created_at)
		 VALUES ($1, $2, $3, '{}'::jsonb, now() - interval '5 minutes')`,
		uuid.New(), tenant, name)
	if err != nil {
		t.Fatalf("insert a row that has been waiting five minutes: %v", err)
	}
	ledger.remember(series)

	settle := func() {
		t.Helper()
		err := db.RunSystem(t.Context(), conn, relayToken,
			func(ctx context.Context, tx db.Tx[db.System]) error { return ledger.settleDrained(ctx, tx, nil) })
		if err != nil {
			t.Fatalf("settle a pass over the queue: %v", err)
		}
	}
	settle()
	if _, still := ledger.seen[series]; !still {
		t.Error("a pass forgot a series whose row the queue still holds: that row is waiting, its wait is " +
			"the number's content, and the pass that wrote no value for it would leave the gauge reporting " +
			"whatever it last said — including zero, if that is what the last correction wrote")
	}

	if _, err := admin.ExecContext(t.Context(),
		"UPDATE platformkit_outbox SET published_at = clock_timestamp() WHERE tenant_id = $1 AND name = $2",
		tenant, name); err != nil {
		t.Fatalf("relay the row: %v", err)
	}
	settle()
	if _, still := ledger.seen[series]; still {
		t.Fatal("the queue holds no row of this series and the ledger still owes it nothing: the row went " +
			"out, so the wait the gauge is holding is history, and only the correction this ledger is kept " +
			"for turns the number back into what the queue says now")
	}
	settle() // A corrected series is not asked about again, which is what an idle queue costs.
}
