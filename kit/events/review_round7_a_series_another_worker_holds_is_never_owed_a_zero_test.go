package events

// The one premise `b2b1b39` rests its new cadence on, tested with a second worker.
//
// `b2b1b39` moved `settleDrained` off every batch and onto the pass that finds the queue's
// end, and its case (relay_settle_test.go) pins which pass asks. What it does not pin — and
// what the comment on `drainedSeries` and the commit body both lean on — is the half about
// concurrency: "including a row another worker took under FOR UPDATE SKIP LOCKED, whose wait
// is still real", and "a short batch means a short pending set, locked rows included". The
// first of those is the safety property: a pass that reads the queue and does not see the
// rows another worker has locked would announce those series drained, and write a zero that
// says "this tenant is not behind" while the row that would prove otherwise waits inside
// someone else's transaction. That is the one direction this number may never lie, and it is
// the direction a cheap edit could break — putting `FOR UPDATE SKIP LOCKED`, a LIMIT or a
// fresher-snapshot hint on `stillWaiting` reads cheaper than the queue's whole pending set,
// which is exactly the cost `b2b1b39` exists to reduce.
//
// relay_drain_test.go proves the three-way choice over sets handed to `drainedSeries`; it
// cannot see whether `stillWaiting` really puts a locked row into `waiting`, because it is
// handed the sets. This case reaches the same choice through one live transaction holding
// rows with the relay's own locking clause, and reads the answer back off the ledger — the
// record of which series this process still owes a zero to — so both branches are visible:
// the drained series must leave the ledger and the held one must not. Nothing here is read
// through an error message or a span, so the case does not depend on what a defect prints.

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestASeriesAnotherWorkerHoldsIsNeverOwedAZero(t *testing.T) {
	_, conn := dbtest.Schema(t)
	drainedT, heldT, shortT := uuid.New(), uuid.New(), uuid.New()
	const (
		drainedName = "review.round7_drained"
		heldName    = "review.round7_held"
		shortName   = "review.round7_short"
	)

	// 106 unpublished rows, written in the order the relay will read them
	// (created_at defaults to clock_timestamp(), so insertion order is queue order):
	// one row of the drained series, then 100 rows of the series the second worker is
	// about to hold, then five rows of the series that will fill the short batch.
	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := PublishFor(ctx, tx, drainedT, drainedName, nil); err != nil {
			return err
		}
		for i := 0; i < batch; i++ {
			if err := PublishFor(ctx, tx, heldT, heldName, nil); err != nil {
				return err
			}
		}
		for i := 0; i < batch/20; i++ {
			if err := PublishFor(ctx, tx, shortT, shortName, nil); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write 1 + %d + %d unpublished rows: %v", batch, batch/20, err)
	}

	// An empty ledger, so every entry below is one a relay pass made and no series
	// another case relayed answers for the read this case is about. The helper carries
	// this file's own name: the files beside it belong to other rounds and to the
	// delivery, and this case must not stop building because one of them changed.
	restore := swapLedgerForReviewRound7(map[lagKey]struct{}{})
	t.Cleanup(restore)

	// Pass one: a full batch — the drained series' row and 99 rows of the series the
	// second worker will hold — so both are in the ledger and the queue still holds a
	// row of the held series.
	if n, err := relayBatch(t.Context(), conn, memory.New()); err != nil || n != batch {
		t.Fatalf("first relay pass: moved %d rows, err %v, want a full batch of %d: without a ledger "+
			"grown by a real pass there is no earlier reading for the correction to be owed", n, err, batch)
	}
	if size := ledgerSizeForReviewRound7(); size != 2 {
		t.Fatalf("the ledger holds %d series after the first pass, want 2 (the drained series and the "+
			"series the second worker is about to hold): the case cannot decide anything until both are in it", size)
	}

	// The second worker. The relay's own SELECT, in its own transaction, held open past
	// this case's assertions and rolled back: it locks what remains of the held series
	// and republishes nothing.
	var wg sync.WaitGroup
	locked, release := make(chan struct{}), make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
			var got []row
			if err := tx.DB().Raw(`SELECT id FROM `+table+`
				WHERE published_at IS NULL AND name = ? ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED`,
				heldName, batch).Scan(&got).Error; err != nil {
				return err
			}
			if len(got) == 0 {
				t.Errorf("the second worker locked no row of %s: the first pass took them all, so this "+
					"case never reached the state it is about", heldName)
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	giveBack := sync.OnceFunc(func() { close(release); wg.Wait() })
	t.Cleanup(giveBack)

	// Pass two, while those locks are held: it skips the locked rows, finds five of the
	// short series, and so is the pass that finds the queue's end and asks it the
	// correction's question.
	n, err := relayBatch(t.Context(), conn, memory.New())
	if err != nil {
		t.Fatalf("relay pass while another worker holds rows: %v: the correction's read runs before the "+
			"publish, so a locked row must not be able to refuse the pass", err)
	}
	if n != batch/20 {
		t.Fatalf("second relay pass moved %d rows, want %d: the pass must come back short to be the one "+
			"that finds the queue's end, and if it did not, it asked no read and decided nothing", n, batch/20)
	}

	heldKey := lagKey{tenant: heldT, event: heldName}
	if !inLedgerForReviewRound7(heldKey) {
		t.Errorf("the correction zeroed %s/%s and dropped it from the ledger while another worker's "+
			"transaction held a row of it: the queue's unpublished rows are there to be read, and a "+
			"zero written over one that is locked says this tenant's series has drained while the row "+
			"waits inside a transaction — the direction this number may never lie",
			heldKey.tenant, heldKey.event)
	}
	if inLedgerForReviewRound7(lagKey{tenant: drainedT, event: drainedName}) {
		t.Errorf("the drained series %s is still in the ledger: no row of it is left in the queue, so this "+
			"pass owed it its zero, and a correction that zeroes nothing keeps reporting a wait that "+
			"the queue no longer holds — the case cannot be satisfied by never writing the zero",
			drainedName)
	}

	// The worker rolls back, its row returns to the queue unpublished, and the drain that
	// finishes the queue settles what is left: the patience above is one pass's worth, not
	// a series stuck at a stale reading for as long as the process lives.
	giveBack()
	if err := Relay(t.Context(), conn, memory.New()); err != nil {
		t.Fatalf("relay the rest of the queue: %v", err)
	}
	if err := Relay(t.Context(), conn, memory.New()); err != nil {
		t.Fatalf("relay the idle queue: %v", err)
	}
	var pending int
	if err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM " + table + " WHERE published_at IS NULL").Scan(&pending).Error
	}); err != nil {
		t.Fatalf("count the queue: %v", err)
	}
	if pending != 0 {
		t.Errorf("%d rows are still unpublished after two drains: nothing here can be judged until the "+
			"queue is empty", pending)
	}
	if left := ledgerSizeForReviewRound7(); left != 0 {
		ids := seriesInLedgerForReviewRound7()
		t.Errorf("%d series (%v) are left in the ledger after the queue drained and its end was reached: "+
			"a drained series is owed one zero and then asked about no more", left, ids)
	}
}

// ledgerSizeForReviewRound7 reads the set the correction is asked about.
func ledgerSizeForReviewRound7() int {
	reportedSeries.mu.Lock()
	defer reportedSeries.mu.Unlock()
	return len(reportedSeries.seen)
}

// inLedgerForReviewRound7 answers whether this process still owes one series its zero.
func inLedgerForReviewRound7(k lagKey) bool {
	reportedSeries.mu.Lock()
	defer reportedSeries.mu.Unlock()
	_, in := reportedSeries.seen[k]
	return in
}

// seriesInLedgerForReviewRound7 names what is left, for the failure message.
func seriesInLedgerForReviewRound7() []string {
	reportedSeries.mu.Lock()
	defer reportedSeries.mu.Unlock()
	out := make([]string, 0, len(reportedSeries.seen))
	for k := range reportedSeries.seen {
		out = append(out, k.tenant.String()+"/"+k.event)
	}
	return out
}

// swapLedgerForReviewRound7 replaces the process's ledger and restores what it held.
func swapLedgerForReviewRound7(next map[lagKey]struct{}) func() {
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
