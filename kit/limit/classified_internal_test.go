package limit

// classified_internal_test.go pins the one decision every method answers from,
// shape by shape. The burst case shows what the decision is about — attempts
// queued behind one row, whose tail came back answered with driver.ErrBadConn,
// and a caller that fails open on an error admits every one of them — but a
// burst cannot be a pin: whether the queue forms depends on the runner's load,
// which is why that acceptance case comes and goes. What can be pinned is the
// classifier itself, and each shape below is one a real pool has answered with:
// the wait behind the row names its world (55P03), the wall names its own
// (DeadlineExceeded) — and the wall, expiring while the pool was still hunting
// for a connection, comes back as the driver's bare bad-connection error with
// the deadline dropped. Whose verdict that is, the attempt's own context
// answers, and only a store that answered while the wall still stands gets to
// stay an outage.

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestTheClassifierRefusesEveryShapeOfSpentBudgetAndKeepsEveryVerdictTheStoreGave
// is the whole of classified as a table: three waits refused, two outages kept,
// one completed attempt untouched. The outage rows are not decoration — ADR
// 0010's fail-open during a real store outage is what the burst cure must not
// have bought, and a driver error at a live wall is exactly that outage answering
// at once.
func TestTheClassifierRefusesEveryShapeOfSpentBudgetAndKeepsEveryVerdictTheStoreGave(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	live, cancelLive := context.WithTimeout(t.Context(), time.Hour)
	defer cancelLive()

	for _, tc := range []struct {
		name     string
		err      error
		attempt  context.Context
		wantBusy bool
	}{
		{"the row's lock budget spent", &pgconn.PgError{Code: "55P03", Message: "lock timeout"}, live, true},
		{"the wall expired, named", context.DeadlineExceeded, expired, true},
		{"the wall expired, swallowed by the pool's driver", fmt.Errorf("db: begin: %w", driver.ErrBadConn), expired, true},
		{"a connection went bad while the store still stood", fmt.Errorf("db: begin: %w", driver.ErrBadConn), live, false},
		{"the store refused the statement at once", errors.New("permission denied for table platformkit_limits"), live, false},
	} {
		got := classified(tc.err, tc.attempt)
		if busy := errors.Is(got, ErrBusy); busy != tc.wantBusy {
			if busy {
				t.Errorf("%s: classified answered %v, a refusal; want the outage kept as an outage", tc.name, got)
			} else {
				t.Errorf("%s: classified answered %v, an outage; want the refusal ErrBusy — an error is "+
					"what a caller that fails open admits the attempt on", tc.name, got)
			}
		}
	}

	// An attempt that finished is finished, even if its wall expired on the way
	// out of the pool: erasing its answer here would refuse a counted attempt.
	if err := classified(nil, expired); err != nil {
		t.Errorf("classified(nil, expiredWall) = %v, want nil: a completed attempt stays completed", err)
	}
}
