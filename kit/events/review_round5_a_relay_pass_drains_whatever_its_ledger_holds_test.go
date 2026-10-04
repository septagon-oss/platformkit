package events

// A relay pass must drain the queue whatever its lag ledger holds.
//
// `f05c192` added `lagLedger`: the relay remembers the (tenant, event) series it has
// reported a wait for and, on a later pass, asks the queue in one statement which of them
// still hold an unpublished row (`stillWaiting`), so a series that has drained stops
// reporting the wait of its last row. The ledger is unbounded, and that one statement
// names every entry of it as two bind parameters. A Postgres statement carries at most
// 65,535 parameters, so a ledger of more than 32,767 series produces a statement the
// server refuses — and `relayBatch` calls `settleDrained` *before* it publishes, so the
// refusal aborts the whole batch: nothing is published, nothing is stamped, the ledger is
// not shortened by a failing pass, and the next pass asks the same question and is refused
// again. The queue stops draining for every tenant, in the one state the number exists
// for — a backlog across many tenants and many event names.
//
// The state is not exotic for the runtime this repository is written for: the ledger holds
// one entry per (tenant, event) series with a row in the queue, so 999 tenants and forty
// event names fill it on the way past, which is one transport blip of a few minutes at a
// modest traffic rate. Seeded here directly because the case is the size of the ledger, not
// the 32,768 rows it would take to grow one.
//
// What the case asserts is the correct behaviour, which is reachable two ways: the pass
// returns no error and publishes its batch, or the correction gives up on itself — reads a
// bounded chunk, or drops the series it cannot ask about — and lets the batch go out.
// Either way the row is relayed and stamped and the worker keeps working.

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestARelayPassDrainsAQueueItsLedgerIsLargerThanOneStatement: one row waiting, and a
// ledger of 40,000 series — larger than one statement can name. The row goes out.
func TestARelayPassDrainsAQueueItsLedgerIsLargerThanOneStatement(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}

	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, "billing.invoice_issued", nil)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The ledger as a backlog across a shared installation leaves it: series this process
	// reported a wait for and has not yet been able to zero.
	restore := swapLedger(map[lagKey]struct{}{})
	t.Cleanup(restore)
	for i := 0; i < 40000; i++ {
		reportedSeries.remember(lagKey{tenant: uuid.New(), event: "review.round5_seeded." + strconv.Itoa(i)})
	}

	transport := memory.New()
	// Two passes, because a failing correction must not be a permanent one: settleDrained
	// returns before it drops anything from the ledger, so the entry that broke this pass is
	// the first thing the next pass names again. The queue has to drain by the second tick.
	for pass := 1; pass <= 2; pass++ {
		if err := Relay(t.Context(), conn, transport); err != nil {
			t.Errorf("Relay pass %d over a queue of one row and a ledger of 40000 series: %v: the "+
				"drained-series read runs before the publish, so a read this pass does not need stops "+
				"every tenant's queue from draining until the process restarts", pass, err)
		}
	}
	var unpublished int
	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM " + table + " WHERE published_at IS NULL").Scan(&unpublished).Error
	}); err != nil {
		t.Fatalf("count the queue: %v", err)
	}
	if unpublished != 0 {
		t.Errorf("%d row(s) still unpublished after Relay: the batch was aborted by the drained-series read, "+
			"so events this tenant is waiting on never reached the transport", unpublished)
	}
}

// TestStillWaitingAnswersALedgerLargerThanOneStatementCanName: the read on its own, so a
// failure names the statement rather than the pass around it. Measured against this
// repository's own Postgres 16 with the bind parameters the read actually sends, the
// statement is refused by `ERROR: stack depth limit exceeded (SQLSTATE 54001)` somewhere
// between 6,000 and 8,000 series — the 65,535-parameter ceiling is the looser of the two.
func TestStillWaitingAnswersALedgerLargerThanOneStatementCanName(t *testing.T) {
	_, conn := dbtest.Schema(t)
	reported := make(map[lagKey]struct{}, 40000)
	for i := 0; i < 40000; i++ {
		reported[lagKey{tenant: uuid.New(), event: "review.round5_seeded." + strconv.Itoa(i)}] = struct{}{}
	}
	err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		waiting, err := stillWaiting(ctx, tx, reported)
		if err != nil {
			return err
		}
		if len(waiting) != 0 {
			t.Errorf("stillWaiting found %d of the seeded series in a queue that holds none", len(waiting))
		}
		return nil
	})
	if err != nil {
		t.Errorf("stillWaiting over 40000 series: %v: the read names every series as two bind parameters, and "+
			"a Postgres statement carries at most 65535 of them, so the ledger's growth is a statement the "+
			"server refuses", err)
	}
}

// swapLedger replaces the process's ledger and returns what it held, so the cases above
// cannot leave 40,000 phantom series behind for the rest of the package's tests.
func swapLedger(next map[lagKey]struct{}) func() {
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
