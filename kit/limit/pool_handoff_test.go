package limit

// pool_handoff_test.go pins `discarded`, the decision that asks an attempt again
// when the answer says no connection was ever handed to it.
//
// It exists because of the same burst as burst_test.go, seen from the side that
// case cannot hold still. That case measures whether any of 75 queued attempts
// came back answered with an error; on a loaded runner one of them sometimes
// does — `limit: driver: bad connection` — and the caller that fails open on an
// error admits every attempt, which is the failure mode this package exists to
// stop. The shape is not the wall expiring mid-wait, which classified already
// refuses as unserved; it is the pool's driver discarding a connection — one left
// inside the transaction an earlier attempt's expired wall abandoned, one already
// closed, one whose session ping did not come back — three times over, which is
// what database/sql gives up on and hands up as driver.ErrBadConn. The store
// never answered that attempt, so it is not an outage, and ADR 0010's outage is
// what the cure must not have bought: the rows below keep every verdict a store
// actually sent, and the pinning of a down store to its own error is
// classified_internal_test.go's, untouched.

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestADiscardedConnectionIsAskedAgainAndEveryStoreVerdictIsLeftAlone walks the
// predicate over each answer a real pool and a real store send back: the pool's
// own complaint ahead of the wall is asked again, and everything else — the wall
// expired, the row's lock spent, the store refusing the statement — is left for
// classified to answer the way it does today.
func TestADiscardedConnectionIsAskedAgainAndEveryStoreVerdictIsLeftAlone(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	live, cancelLive := context.WithTimeout(t.Context(), time.Hour)
	defer cancelLive()

	for _, tc := range []struct {
		name    string
		err     error
		attempt context.Context
		want    bool
	}{
		{"the pool threw the connection away, bare", driver.ErrBadConn, live, true},
		{"the pool threw the connection away, wrapped", fmt.Errorf("db: %w", driver.ErrBadConn), live, true},
		{"the pool was still hunting when the wall came down", driver.ErrBadConn, expired, false},
		{"the row's lock budget spent", &pgconn.PgError{Code: "55P03", Message: "lock timeout"}, live, false},
		{"the store refused the statement at once", errors.New("permission denied for table platformkit_limits"), live, false},
		{"no answer at all", nil, live, false},
	} {
		if got := discarded(tc.err, tc.attempt); got != tc.want {
			t.Errorf("discarded(%v, wall) = %v, want %v: %s", tc.err, got, tc.want, tc.name)
		}
	}
}
