package events

// Which pass asks the queue the correction's question.
//
// `stillWaiting` deliberately carries no bind parameters — that is what cured the read that
// refused its own transaction — and it pays for that with a scan of the queue's whole
// pending set. The pending set is the backlog while a backlog is draining, and `Relay`
// takes batch after batch of it until one comes back short, so a pass that asked on every
// batch would ask `N/batch` times about rows it is emptying as it goes: the drain's cost
// quadratic in the backlog rather than linear, in the one state the number exists for.
//
// So the question is asked on the pass that finds the queue's end, which is also the pass
// whose answer is worth having: a full batch says only that the queue is not at its end, so
// which of this process's earlier readings have gone stale is not knowable from it. That
// defers a correction, and what it defers is observable in the ledger — the series a full
// batch relayed outright are still owed their zero — which is what this case reads back.
// relay_drain_test.go owns the choice of *which* series a settled pass owes the zero to, and
// review_round6_… owns the size the ledger reaches by ordinary relaying; neither can see
// which pass paid to find out.
//
// The bias this leaves is pinned where it is written rather than here: a tick whose deadline
// passes mid-drain returns without a correction and repeats one stale reading, which is the
// direction this file has always promised — a number a moment too old beats one that calls a
// waiting queue clear.

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

// TestAFullBatchLeavesTheCorrectionToTheQueueEnd: three full batches of one row per series,
// relayed a batch at a time. Two full passes leave the ledger holding both batches — the
// hundred series the first pass relayed outright are still owed a zero, because no read ran
// to discover they were owed one — and the call that reaches the queue's end pays the read
// once and clears all three hundred.
func TestAFullBatchLeavesTheCorrectionToTheQueueEnd(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	const rowsTotal = 3 * batch

	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		for i := 0; i < rowsTotal; i++ {
			if err := PublishFor(ctx, tx, tenant.ID, fmt.Sprintf("relay.settle_%04d", i), nil); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("publish %d rows across %d series: %v", rowsTotal, rowsTotal, err)
	}

	// Start from an empty ledger so every entry below is one this case's passes made, and no
	// series another case relayed answers for the read that this case is about.
	restore := swapLedgerForRelaySettle(map[lagKey]struct{}{})
	t.Cleanup(restore)

	for pass := 1; pass <= 2; pass++ {
		if n, err := relayBatch(t.Context(), conn, memory.New()); err != nil || n != batch {
			t.Fatalf("relay pass %d: moved %d rows, err %v, want a full batch of %d", pass, n, err, batch)
		}
	}
	if grown := ledgerSizeForRelaySettle(); grown != 2*batch {
		t.Errorf("the ledger holds %d series after two full batches relayed %d rows, want %d: a pass that "+
			"found a full batch found no end to the queue, so it cannot know which earlier readings are "+
			"stale, and a pass that asks anyway scans the whole backlog to learn nothing new — %d times over "+
			"one drain of this size, which is the cost the read is kept off",
			grown, 2*batch, 2*batch, rowsTotal/batch)
	}

	// The rest of the backlog, in the call kit/jobs makes every second: two more batches, the
	// second one short, and the correction all three hundred series are owed.
	if err := Relay(t.Context(), conn, memory.New()); err != nil {
		t.Fatalf("relay the rest of the backlog: %v", err)
	}
	var pending int
	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM " + table + " WHERE published_at IS NULL").Scan(&pending).Error
	}); err != nil {
		t.Fatalf("count the queue: %v", err)
	}
	if pending != 0 {
		t.Errorf("%d of %d rows are still unpublished: the drain stopped short of the queue's end, which is "+
			"the pass the correction is owed to", pending, rowsTotal)
	}
	if left := ledgerSizeForRelaySettle(); left != 0 {
		t.Errorf("%d series are left in the ledger once the queue is empty and its end has been reached: "+
			"the pass that finds the end owes every reported series its zero, which is the whole reason the "+
			"correction may wait for it", left)
	}
}

// ledgerSizeForRelaySettle reads the ledger this case is watching, the set the correction's
// read is asked about, and the record of which passes ran it.
func ledgerSizeForRelaySettle() int {
	reportedSeries.mu.Lock()
	defer reportedSeries.mu.Unlock()
	return len(reportedSeries.seen)
}

// swapLedgerForRelaySettle replaces the process's ledger and returns what it held, restored
// after the case, so three hundred series this case relayed cannot answer for another one.
func swapLedgerForRelaySettle(next map[lagKey]struct{}) func() {
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
