package events

// The lag ledger's dangerous size, grown by relaying rather than by a test that assumes it.
//
// Round 5 found that the drained-series read put every series the process had reported a
// wait for into one statement as two bind parameters, and that this repository's own
// Postgres 16 refuses such a statement between 6,000 and 8,000 series (`stack depth limit
// exceeded`, SQLSTATE 54001) — aborting the relay's transaction before the publish, so no
// tenant's queue drained again until the process restarted. `debd70c` cured it by asking the
// queue what it holds instead of naming the series. Round 5's two cases were kept and stay
// sharp: restoring `f05c192`'s read at this head fails them both with SQLSTATE 54001.
//
// What nothing committed proves is the premise, that a ledger of that size is ever there to
// be asked about. Both `f05c192` and `debd70c` say so in their own commit bodies: "not
// verified: no real installation grown to ~7,000 ledger entries by published events; the
// case seeds the ledger". Seeding `reportedSeries` by hand measures the statement's shape,
// which is worth having, but it leaves the size an assumption — and this case had to be
// written twice to find out what the assumption was worth.
//
// It is not worth what round 5's story supposed. A transport that refuses everything does
// NOT grow the ledger: `relayBatch` selects `WHERE published_at IS NULL ORDER BY created_at,
// id LIMIT batch FOR UPDATE SKIP LOCKED`, and a pass whose publish fails rolls the stamp back,
// so the next pass reads the *same* batch again. Measured at this head with a transport that
// refuses every row, eighty-one passes over a backlog of 8,000 series leave the ledger at
// exactly `batch` = 100 entries, because they are the same hundred series every time. So "a
// few minutes of transport trouble across a few hundred tenants" does not fill the ledger;
// the relay is stuck on one batch, and the read it makes is asked about a hundred series.
//
// What does grow it is the ordinary route: a wide backlog draining successfully. `remember`
// fires for every series in every batch, and `drainedSeries` drops a series only once the
// queue holds no row of it — so a series the relay has touched but not emptied stays in the
// ledger, one entry per series, and the ledger's size is the number of series the backlog
// holds. This case publishes two rows per series across more series than one statement could
// name, relays the first row of each, and reads the ledger back. That is the measurement this
// file exists to make: the ledger is the queue's breadth, not the process's memory, and the
// size round 5's server refused at is reached by a backlog that is merely wide.
//
// It pins the other half too. Once the queue is empty, a pass owes each of those series a
// zero and the process a ledger it can hold forever, or every subsequent backlog starts from
// a larger statement than the last one left behind.
//
// Reachability runs through the queue's own `published_at` and the ledger's own size, neither
// of which is the defect's output, so no part of this case is green only while a defect
// stands — and the assertion on the ledger's size is a `Fatalf` that says so out loud if the
// mechanism ever changes, rather than passing over a route it no longer walks.

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	// review6Series is the backlog's breadth. Round 5 measured this server refusing two bind
	// parameters per series somewhere between 6,000 and 8,000 series, so the ledger has to
	// pass the top of that range before it says anything about the cliff. At two rows per
	// series this is a 17,000-row backlog: 350 tenants and twenty event names each holding a
	// couple of events, which is a shared installation's bad afternoon rather than a fixture's
	// invention.
	review6Series = 8500
	// review6RowsPerSeries is what keeps a series in the ledger. With one row per series the
	// relay empties a series in the same pass it remembers it, `drainedSeries` drops it again,
	// and the ledger never leaves `batch` — measured, and the reason this case is two rows
	// deep rather than one and twice as fast.
	review6RowsPerSeries = 2
	// review6Passes is the relay ticks the case runs before reading the ledger: one batch of
	// first rows per pass, which is every series once and so, if the ledger is the queue's
	// breadth, every series the queue holds.
	review6Passes = review6Series / batch
	// review6CliffFloor is the low end of the range round 5 measured, and the number this case
	// demands of a ledger it grew without seeding anything.
	review6CliffFloor = 6000
)

func TestTheLagLedgerGrowsByRelayingRealRowsAndSettlesWhenTheQueueEmpties(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}

	// One transaction, the whole backlog, with every series' first row written before any
	// series' second — which is how a queue that filled up across many tenants reads to a
	// relay working it oldest first.
	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		for r := 0; r < review6RowsPerSeries; r++ {
			for s := 0; s < review6Series; s++ {
				if err := PublishFor(ctx, tx, tenant.ID,
					fmt.Sprintf("review.round6_seeded_%05d", s), nil); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("publish the backlog: %d rows over %d series: %v",
			review6Series*review6RowsPerSeries, review6Series, err)
	}

	// A ledger of this process's earlier readings would make the count below say something
	// about which tests ran first; start from empty so every entry is one the relay made.
	restore := swapLedgerForReviewRound6(map[lagKey]struct{}{})
	t.Cleanup(restore)

	for pass := 1; pass <= review6Passes; pass++ {
		if _, err := relayBatch(t.Context(), conn, memory.New()); err != nil {
			t.Fatalf("relay pass %d over a backlog of %d series: %v: the drained-series read runs "+
				"before the publish, so the ledger this process grew by relaying can stop every "+
				"tenant's queue from draining", pass, review6Series, err)
		}
	}

	grown := ledgerSizeForReviewRound6()
	if grown < review6CliffFloor {
		t.Fatalf("the ledger holds %d series after %d passes over a backlog of %d series with %d rows "+
			"each: the queue holds a row of every one of those series, so a ledger that tracks the "+
			"queue holds %d and a ledger that tracks the batch holds about %d — this case is the "+
			"evidence that the size round 5 refused at is reachable at all, so if the mechanism has "+
			"changed, write that down where the ledger is written instead of letting this pass",
			grown, review6Passes, review6Series, review6RowsPerSeries, review6Series, batch)
	}

	// The queue empties; every reading above is now a stale one. A drained series is owed one
	// zero and then asked about no more, so the ledger cannot grow past the work that grew it.
	if err := Relay(t.Context(), conn, memory.New()); err != nil {
		t.Errorf("Relay over the rest of the backlog: %v", err)
	}
	var unpublished int
	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM " + table + " WHERE published_at IS NULL").Scan(&unpublished).Error
	}); err != nil {
		t.Fatalf("count the queue: %v", err)
	}
	if unpublished != 0 {
		t.Errorf("%d of %d rows are still unpublished after Relay: events these tenants are waiting "+
			"on never reached the transport", unpublished, review6Series*review6RowsPerSeries)
	}
	if left := ledgerSizeForReviewRound6(); left != 0 {
		t.Errorf("%d series are left in the ledger after the queue drained and a pass settled it: a "+
			"drained series is owed one zero and then asked about no more, so the ledger stays the "+
			"size of the queue rather than the size of everything this process has ever relayed", left)
	}
}

// ledgerSizeForReviewRound6 reads what this process has reported a wait for and has not yet
// been able to zero — the set the drained-series read is asked about, and the number whose
// size round 5's HIGH turned on.
func ledgerSizeForReviewRound6() int {
	reportedSeries.mu.Lock()
	defer reportedSeries.mu.Unlock()
	return len(reportedSeries.seen)
}

// swapLedgerForReviewRound6 replaces the ledger with next and returns what it held, restored
// after the case. It carries its own name because the file beside it belongs to another
// review round, and this case must not fail to build because that round changed a helper.
func swapLedgerForReviewRound6(next map[lagKey]struct{}) func() {
	reportedSeries.mu.Lock()
	defer reportedSeries.mu.Unlock()
	was := reportedSeries.seen
	reportedSeries.seen = next
	return func() {
		reportedSeries.mu.Lock()
		defer reportedSeries.mu.Unlock()
		reportedSeries.seen = was
	}
}
