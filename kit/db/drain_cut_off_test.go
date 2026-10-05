package db

// drain_cut_off_test.go pins the one sentence a cut-off drain adds to its own report. It is
// read here, of the two facts a cut leaves, rather than through db.Backfill because *which* of
// them a cut leaves is a race: measured on one host, 6 calls in 120 named a driver leftover and
// the other 114 named their deadline unaided. A case that had to lose that race to say anything
// would be the timing failure the drain's own watchdog case exists to prevent
// (window_shape_readings_test.go), so the race is not what this file waits for.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

// TestACutOffDrainNamesTheCutAndWhatItFound walks the leftovers a cut was measured leaving,
// with the sentence wrapped around one of them: the reason the run ended and what its session
// was holding are both in the report, because the first is what a caller of a bounded tick reads
// and the second is what an operator of a broken one would need.
func TestACutOffDrainNamesTheCutAndWhatItFound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, left := range []error{
		sql.ErrTxDone,
		driver.ErrBadConn,
		errors.New("write failed: write tcp [::1]:45836->[::1]:61130: i/o timeout"),
		errors.New("measuring the next batch of probe: " + driver.ErrBadConn.Error()),
	} {
		got := drainCutOff(ctx, left)
		if !errors.Is(got, context.Canceled) {
			t.Errorf("a drain cut off mid-batch reported %v; the reason its context ended is the half its caller reads", got)
		}
		if !errors.Is(got, left) {
			t.Errorf("a drain cut off mid-batch lost what its session was holding: %v", got)
		}
	}
}

// TestDrainCutOffAddsToAReportAndNeverReplacesOne is why the rule prefixes rather than returns:
// the drain's own decisions — the bound it stopped at, a cursor another runner took ahead — are
// the report an operator acts on, and a run whose context happened to end on the way there is
// still the run that made them.
func TestDrainCutOffAddsToAReportAndNeverReplacesOne(t *testing.T) {
	running, stop := context.WithCancel(context.Background())
	defer stop()
	for _, decided := range []error{nil, ErrBackfillBudget, errors.New("another runner took the backfill of user/7 ahead of this one")} {
		if got := drainCutOff(running, decided); got != decided {
			t.Errorf("a run inside its own time reported %v instead of %v", got, decided)
		}
	}
	cut, cancel := context.WithCancel(context.Background())
	cancel()
	if got := drainCutOff(cut, ErrBackfillBudget); !errors.Is(got, ErrBackfillBudget) || !errors.Is(got, context.Canceled) {
		t.Errorf("a bound the drain reached reported %v: the bound stands and the cut is added to it, neither replaces the other", got)
	}
}
