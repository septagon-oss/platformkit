package limit

// pool_storm_asks_again_internal_test.go pins what an attempt does when the
// pool answers it with the bare bad-connection sentinel while its wall still
// stands.
//
// The burst case met this shape twice in one round of twenty runs on a runner
// at load average 97: one attempt in seventy-five came back `limit: driver:
// bad connection`, and the temporary probe showed why the classifier could
// not tell it apart from an outage — the sentinel arrived two and a half
// milliseconds *before* the attempt's 2 s wall, with nothing else in its
// chain:
//
//	err="driver: bad connection" deadline=11:11:38.8444 now=11:11:38.8420
//	passed=false ctxErr=<nil> chain=[*errors.errorString(driver: bad connection)]
//
// The merged cure read the wall from its deadline and so catches the attempt
// that answers behind the wall; this is the same pool storm answering a
// breath early. It cannot be summoned on demand from a real Postgres — it is
// what the pool does when its own retries are exhausted and no core is free —
// so the seam below answers for the pool, and these cases invent nothing
// beside what the probe recorded.

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
)

// stormLimiter is the limiter whose pool answers `answer` at call number
// call, counting the opens itself. The connection it is handed is the nil one
// the seam never dereferences.
func stormLimiter(answer func(call int, attempt context.Context) error) postgres {
	calls := 0
	return postgres{
		conns: func(context.Context) (*db.Conn, bool) { return nil, true },
		attempt: func(ctx context.Context, _ *db.Conn, _ func(context.Context, db.Tx[db.System]) error) error {
			calls++
			return answer(calls, ctx)
		},
	}
}

func nothing(context.Context, db.Tx[db.System]) error { return nil }

// TestAPoolStormIsAskedAgainWhileTheAttemptWallStillStands is the cure itself:
// the sentinel inside a standing wall opens the counter transaction again,
// and the attempt ends on the second answer rather than on the error every
// fail-open caller admits.
func TestAPoolStormIsAskedAgainWhileTheAttemptWallStillStands(t *testing.T) {
	calls := 0
	l := postgres{
		conns: func(context.Context) (*db.Conn, bool) { return nil, true },
		attempt: func(ctx context.Context, _ *db.Conn, _ func(context.Context, db.Tx[db.System]) error) error {
			calls++
			if calls == 1 {
				return driver.ErrBadConn
			}
			return nil
		},
	}
	start := time.Now()
	if err := l.run(t.Context(), nothing); err != nil {
		t.Fatalf("an attempt asked again after the pool's ask = %v; want the second answer to settle it", err)
	}
	if calls != 2 {
		t.Errorf("the counter transaction opened %d times, want 2: the pool asked once, the attempt asked once, and then the attempt was over", calls)
	}
	if elapsed := time.Since(start); elapsed >= budget {
		t.Errorf("the retried attempt took %s, want well inside the %s wall it was given", elapsed, budget)
	}
}

// TestAPoolStormThatOutlivesTheWallEndsAsABusyAttempt is the same pool answered
// until the wall itself ends the attempt: the storm is then the wall's verdict
// — ErrBusy, the refusal — and not the outage a fail-open caller admits. This
// is the shape the burst case asserts zero of, one step away from the
// classification the wall deadline already gave.
func TestAPoolStormThatOutlivesTheWallEndsAsABusyAttempt(t *testing.T) {
	calls := 0
	l := stormLimiter(func(_ int, _ context.Context) error {
		calls++
		// Even a storm costs a round trip; a fake that answers instantly would
		// measure the scheduler, not the wall.
		time.Sleep(10 * time.Millisecond)
		return driver.ErrBadConn
	})
	err := l.run(t.Context(), nothing)
	if !errors.Is(err, ErrBusy) {
		t.Errorf("a pool storm outliving the attempt's wall = %v; want ErrBusy: the wall ended this attempt, and an error is what a caller that fails open admits", err)
	}
	if calls < 2 {
		t.Errorf("the storm was asked %d times, want at least twice: the ask is what keeps it off the fail-open path", calls)
	}
}

// TestABadConnectionCarriedByAWrapperIsNotAskedAgain is the boundary the
// merged wall pins draw: an error that reaches the sentinel through somebody's
// wrapper is kit/db answering from inside a transaction it opened — the
// store's own verdict — and travels as the outage even with the wall standing.
// Only the bare sentinel, which is what the sql layer itself answers when no
// connection ever took the statement, is the pool asking.
func TestABadConnectionCarriedByAWrapperIsNotAskedAgain(t *testing.T) {
	wrapped := fmt.Errorf("db: begin: %w", driver.ErrBadConn)
	calls := 0
	l := postgres{
		conns: func(context.Context) (*db.Conn, bool) { return nil, true },
		attempt: func(ctx context.Context, _ *db.Conn, _ func(context.Context, db.Tx[db.System]) error) error {
			calls++
			return wrapped
		},
	}
	err := l.run(t.Context(), nothing)
	if !errors.Is(err, wrapped) || errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection carried by a wrapper = %v; want the store's own error to travel, not the pool's ask", err)
	}
	if calls != 1 {
		t.Errorf("the wrapped bad connection was asked %d times, want 1", calls)
	}
}

// TestAStoreThatRefusesTheConnectIsNotAskedAgain keeps the other pin alive at
// the loop rather than only at the classifier: a store answering its own way —
// not through the driver's retry sentinel — is answered once, at once, and the
// error travels. ADR 0010's caller decides about that outage; the retry below
// must not turn one into a two-second stall, let alone a refusal.
func TestAStoreThatRefusesTheConnectIsNotAskedAgain(t *testing.T) {
	down := errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")
	calls := 0
	l := postgres{
		conns: func(context.Context) (*db.Conn, bool) { return nil, true },
		attempt: func(ctx context.Context, _ *db.Conn, _ func(context.Context, db.Tx[db.System]) error) error {
			calls++
			return down
		},
	}
	err := l.run(t.Context(), nothing)
	if !errors.Is(err, down) {
		t.Errorf("a store refusing the connect = %v; want its own error to travel, wrapped in limit's name for it", err)
	}
	if errors.Is(err, ErrBusy) {
		t.Errorf("a store refusing the connect = %v; want the outage kept, not a refusal closing a lockout across it", err)
	}
	if calls != 1 {
		t.Errorf("the outage was asked %d times, want 1: a store's own answer is not the pool asking", calls)
	}
}
