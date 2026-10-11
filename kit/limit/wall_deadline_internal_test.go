package limit

// wall_deadline_internal_test.go pins which reading of the attempt's wall
// classified trusts.
//
// The burst case is what this is about. On a loaded runner one attempt in
// seventy-five came back `limit: driver: bad connection` and the zero-error
// assertion named it — one admitted submission, which is the failure this
// package exists to stop. The shape that produced it cannot be summoned on
// demand: it needs a machine with no free core at the instant the attempt's wall
// expires. Measured, it is this — the pool answered the wait with its bare
// bad-connection sentinel 2.0007 s into an attempt whose wall is 2 s, and the
// attempt's own `Err()` still reported nothing:
//
//	entered=false elapsed=2.00073655s detachedErr=<nil> chain=[*errors.errorString(driver: bad connection)]
//	pool{open=16 inuse=16 idle=0 waits=67 waitdur=1m34.099150205s maxOpen=16}
//
// `entered=false` is the transaction's own callback: the attempt never reached a
// statement, because all sixteen connections were in use and sixty-seven attempts
// were waiting for one. So the wait was abandoned at the wall, and the only thing
// that could still tell the wall from a down store was the cancellation — which
// `WithTimeout` delivers from a `time.AfterFunc` callback, in a goroutine of its
// own, on a machine that had no core to run it on.
//
// What a test can hold is the state that callback reaches and leaves faster than
// a suite can catch: a deadline behind the attempt, `Err()` still nil, `Done()`
// still open. The fake below is that state, and it invents nothing beside it —
// every other context in this file is a real one, and the pair of cases is what
// keeps the cure narrow: a spent wall refused, a standing wall still an outage.

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"
)

// behindWall is an attempt whose deadline has passed and whose cancellation has
// not arrived. Only Deadline answers; Err and Done are what the runtime has them
// say until the AfterFunc goroutine is scheduled.
type behindWall struct{ context.Context }

func (behindWall) Deadline() (time.Time, bool) { return time.Now().Add(-time.Microsecond), true }
func (behindWall) Done() <-chan struct{}       { return nil }
func (behindWall) Err() error                  { return nil }

// TestTheWallIsReadFromItsDeadlineAndNotOnlyFromTheCancellationThatAnnouncesIt is
// the race in both directions. Read from the cancellation alone — which is how
// the classifier answered until the burst refused it — the first case below is an
// outage, and a caller that fails open on an error admits the attempt.
func TestTheWallIsReadFromItsDeadlineAndNotOnlyFromTheCancellationThatAnnouncesIt(t *testing.T) {
	badConn := fmt.Errorf("db: begin: %w", driver.ErrBadConn)

	if err := classified(badConn, behindWall{t.Context()}); !errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection with the attempt's deadline behind it and its cancellation not yet visible = %v; "+
			"want ErrBusy: the wall ended this attempt, and an error is what a caller that fails open admits", err)
	}

	// The same sentinel with a wall still standing is the store answering badly,
	// and stays the error ADR 0010 hands to the caller. Refusing this one instead
	// would close a lockout for the length of an outage.
	live, cancelLive := context.WithTimeout(t.Context(), time.Hour)
	defer cancelLive()
	if err := classified(badConn, live); errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection while the attempt's wall still stands = %v; want the outage kept as an error, "+
			"not a refusal: only the deadline moved, and the store answered inside the wall", err)
	}

	// An attempt with no wall of its own has no deadline to read and is judged on
	// the notification alone: a cancellation is all there is to read there, and a
	// live context that never cancelled is not a spent budget.
	if err := classified(badConn, context.Background()); errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection on an attempt with no deadline and no cancellation = %v; want the outage kept", err)
	}
}
