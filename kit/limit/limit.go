// Package limit is one rate limit for every replica.
//
// A counter in a process's memory is three limits when three pods are running,
// and none after a deploy: an attacker gets the limit multiplied by the replica
// count and gets it back whenever anything restarts. That is why the auth
// module's lockout said so in its own comment and left the real thing for
// later. This is the real thing — one row per key per window in Postgres, one
// statement to record an event, one to read a count — so the answer does not
// depend on which pod the request landed on.
//
// It is a fixed window rather than a token bucket: a limit stated the way a
// person understands it ("ten in a quarter of an hour"), one INSERT ... ON
// CONFLICT to record it, and the worst the edge between two windows gives an
// attacker is twice the limit for one instant.
package limit

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/septagon-oss/platformkit/kit/appname"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Limiter counts events under a key and answers whether one more is within a
// limit.
//
// A key is whatever the caller counts by — an address, an account, the two
// together — and it is scoped to the tenant of the context before it is stored,
// so two customers never share a counter and no caller has to remember to say
// which tenant it is counting in.
type Limiter interface {
	// Allow records one event under key and reports whether the window is
	// still within limit: the limit-th event is allowed and the one after it is
	// not. retryAfter is what is left of the window, and it is zero when the
	// answer is yes.
	//
	// An error means the counter could not be reached, and only that: an attempt
	// that was not recorded because the counter was still busy with its key is
	// answered as a refusal — ok false, retryAfter the window, no error — so that
	// a caller which fails open on an error cannot let a busy counter admit the
	// traffic it exists to refuse. kit/limit/README.md owns that failure mode.
	//
	// This is the answer for a caller that only gates. A caller that writes the
	// limit onto its response reads Reserve instead, which records the same event
	// in the same statement and keeps the numbers along with the verdict.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error)

	// Reserve records one event under key and reports, from the one statement that
	// recorded it, the three numbers that statement knows: whether the window is
	// still within limit (the limit-th event is allowed, the one after it is not),
	// how much of limit is left, and how long the window has left.
	//
	// One statement and one reading, because the three answers are one fact about
	// one row: a caller that asks twice gets two windows, the event it recorded and
	// the count read a moment later are not the same attempt, and a server that
	// states a quota it did not decide states a number nobody should believe.
	//
	// remaining is clamped at zero and retryAfter is the rest of the window either
	// way — the allowed caller is the one that can still back off. It counts down
	// from the instant the statement wrote the row, which on a contended key is the
	// end of the wait rather than its start.
	Reserve(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, remaining int, retryAfter time.Duration, err error)

	// Count reports how many events key has in the window that is open now, and
	// how long that window has left. It records nothing, because a limit with
	// three answers rather than two — allow, delay, refuse — has to be read
	// before the attempt it is about, and a read that counted would make every
	// successful sign-in an attempt against the lockout. It is not the way a
	// response states itself: that is Reserve, which reads the row it wrote.
	Count(ctx context.Context, key string, window time.Duration) (n int, retryAfter time.Duration, err error)

	// Forget drops a key: the caller has been proved right about whoever they
	// were counting.
	Forget(ctx context.Context, key string) error
}

// table is the one this package owns. See migrations/000021_limits.
const table = "platformkit_limits"

const (
	// budget is the whole wall one attempt may take, and it is the number
	// docs/adr/0010 promises the caller — this much, and then an answer: an
	// error to fail open on where the store replied, a refusal where it did not.
	// A limiter is on the path of a request about to be refused and must never
	// hold one open; the queue is the traffic's own length, not a budget to buy.
	budget = 2 * time.Second

	// queueBudget is the half of that wall given to waiting for this key's row
	// lock. It is written into the counter's own transaction as lock_timeout, so
	// the server ends the wait rather than a stopwatch and says which world it
	// ended it in — SQLSTATE 55P03, the same code kit/db's migration runner reads
	// to tell a contended lock from a failed migration. The wall's other second
	// pays for everything else it waits for: a connection out of the pool, BEGIN,
	// COMMIT. It is checked rather than commented by
	// TestTheLimitersQueueBudgetFitsInsideItsWall, and README.md names both.
	queueBudget = 1 * time.Second

	// keep is how long a row outlives its window before Purge deletes it. A
	// day is far longer than any window a caller here uses and short enough
	// that the table is bounded by one day of distinct keys.
	keep = 24 * time.Hour

	// MaxWindow is the longest window a counter here keeps whole: Purge deletes
	// a row once its window opened keep ago, so a caller's window longer than
	// this is ended by the sweep rather than by the clock.
	MaxWindow = keep

	// MaxQuota is the largest limit a counter here can refuse past. The count
	// is a Postgres integer that Reserve stops at its maximum rather than
	// overflowing, so every event after a limit this size still counts above
	// it; a larger limit is one the counter can never refuse.
	MaxQuota = math.MaxInt32 - 1
)

// systemToken is the capability the counters need. They belong to no tenant —
// the tenant is the first field of every key — so they are written in a
// cross-tenant transaction, from a detached context: a login that is about to
// be refused rolls its own transaction back, and a failure that rolled back
// with it would be a limiter that never counts the attempts it exists to count.
var systemToken = syscap.NewSystemToken("rate limit counters")

// ErrNoConnection is what every method answers when Connections finds none. It is an error rather than a silent allowance because the caller
// decides: auth fails open and logs, which is right for a lockout and would be
// wrong for a paywall.
var ErrNoConnection = errors.New("limit: no database connection on this context")

// ErrBusy is what Count and Forget answer when the attempt spent its budget
// rather than being refused by a store that replied: the row's lock was not
// ours within queueBudget, or the wall expired before anything was answered at
// all. Nothing was read and nothing was written, and a count invented here
// would be a number nobody took.
//
// Allow and Reserve never return it. Their answer to the same fact is the
// refusal — ok false, retryAfter the whole window, no error — because what an
// error means is the caller's decision, and every caller of these two in the
// field fails open on one. That is the failure mode this package exists to stop,
// so it is carried in the answer rather than in the error; README.md says why,
// and says what a caller that means to fail open would have to ask for.
var ErrBusy = errors.New("limit: the counter was still busy with this key, and the attempt was not counted")

// Connections is where a limiter finds the pool.
//
// A parameter rather than an import: a composition builds its modules before
// kit/app opens the pool, so nothing is available at construction, and a package
// that counts rows has no business linking a web server to find out where they go
// — modules/auth's contracts package holds a Limiter, and a contracts package is
// the entity and the interfaces.
//
// What comes back is the pool, not a connection: a count is written in a
// transaction of its own. Read it from a held handle (`kit/app` composes
// `limit.PostgresFor("", a.held.read)`), never from a request context —
// `httpx.ConnFrom` answers nothing until the request's transaction exists and the
// public write limit counts ahead of it, so built that way every anonymous write
// would find no pool and be admitted.
type Connections func(context.Context) (*db.Conn, bool)

// Postgres returns the shared limiter with unnamed application buckets.
// Connections are acquired on operations, never during construction.
//
// Deprecated: Use PostgresFor with the zero appname.Name and the same Connections.
func Postgres(conns Connections) Limiter { return PostgresFor("", conns) }

// PostgresOf returns the shared limiter under app, acquiring conns on operations.
//
// Deprecated: Use PostgresFor with the same app and Connections.
func PostgresOf(app appname.Name, conns Connections) Limiter {
	return PostgresFor(app, conns)
}

// PostgresFor returns the limiter every replica shares, with app naming its
// buckets. The zero name preserves unnamed buckets. Names are formed by
// appname.RateLimitKey without additional validation. Construction opens nothing;
// operations ask conns for a caller-owned pool and report ErrNoConnection if absent.
func PostgresFor(app appname.Name, conns Connections) Limiter {
	return postgres{conns: conns, app: app, attempt: queueRun}
}

type postgres struct {
	conns Connections
	app   appname.Name
	// attempt opens one try of this attempt's counter transaction and answers
	// for what happened in it. queueRun is the attempt every deployment writes
	// and the answer of the zero value; the field is the seam an internal test
	// answers the pool through, because the pool's bare bad-connection sentinel
	// is not a shape a real Postgres can be made to hold on demand (see
	// askedAgain, and the burst case's CI note beside it).
	attempt counterRun
}

// counterRun is queueRun's shape: open one try, answer for it.
type counterRun = func(ctx context.Context, conn *db.Conn, fn func(context.Context, db.Tx[db.System]) error) error

// queueRun is the real counterRun: the counter's own cross-tenant transaction,
// with the queue's wait written into it.
func queueRun(ctx context.Context, conn *db.Conn, fn func(context.Context, db.Tx[db.System]) error) error {
	return db.RunSystem(ctx, conn, systemToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		// is_local, so the budget belongs to this transaction and dies with it
		// rather than riding the pooled connection to somebody else's statement.
		if err := tx.DB().Exec("SELECT set_config('lock_timeout', ?, true)",
			strconv.FormatInt(queueBudget.Milliseconds(), 10)).Error; err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

func (p postgres) Reserve(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	// One statement: the row is inserted, an open window's count is raised, or a
	// closed window starts again — and the count that comes back is the one this
	// event landed in. Two statements would be two replicas reading one number and
	// writing it back, and two answers about one attempt.
	//
	// Which window the event landed in is asked of the transaction's own instant, so
	// the two CASEs cannot disagree and the row is anchored there. The countdown is
	// asked of clock_timestamp() instead: another writer can hold this row's lock and
	// this statement waits for it, so the transaction's instant is earlier than the
	// event by that wait and a countdown taken from it would tell every caller of the
	// key to sit out longer than the row holds.
	//
	// A refused event is counted too, so the count stops at MaxQuota + 1 rather than
	// overflowing into an error a caller fails open on.
	const q = `INSERT INTO ` + table + ` (key, window_start, count) VALUES (?, now(), 1)
		ON CONFLICT (key) DO UPDATE SET
			count = CASE WHEN ` + table + `.window_start > now() - ?::interval THEN LEAST(` + table + `.count, ?) + 1 ELSE 1 END,
			window_start = CASE WHEN ` + table + `.window_start > now() - ?::interval THEN ` + table + `.window_start ELSE now() END
		RETURNING count, extract(epoch FROM (window_start + ?::interval - clock_timestamp()))`
	n, left, err := p.scan(ctx, q, p.scoped(ctx, key), interval(window), MaxQuota, interval(window), interval(window))
	if err != nil {
		if errors.Is(err, ErrBusy) {
			// The attempt reached no row, so no window was read and there is nothing
			// honest to report as what is left of one; the whole window is the one
			// figure that cannot understate it, and remaining is zero because this
			// attempt counted nothing. Refused rather than errored for ErrBusy's own
			// reason, which is Allow's reason in a four-value shape.
			return false, 0, window, nil
		}
		return false, 0, 0, err
	}
	allowed, remaining, left := decided(limit, n, left)
	return allowed, remaining, left, nil
}

// Allow is Reserve without the numbers, for a caller that only gates: the same
// statement, the same row, the same verdict, and no wait promised to a caller
// that was allowed. A second way to count one key would be a second answer about
// one attempt, so this reads Reserve and keeps the pair it owes.
func (p postgres) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	ok, _, after, err := p.Reserve(ctx, key, limit, window)
	if err != nil {
		return false, 0, err
	}
	if ok {
		return true, 0, nil
	}
	return false, after, nil
}

// decided is the arithmetic of one reading, shared by both stores so the counter
// in a map and the counter in a row cannot disagree about what the same count
// under the same quota means. `left` is the window's own clock: it may have closed
// while the statement ran, or while it waited for another writer of the key, and a
// caller is never told a wait a closed window owes them.
func decided(quota, count int, left time.Duration) (allowed bool, remaining int, retryAfter time.Duration) {
	return count <= quota, max(quota-count, 0), max(left, 0)
}

func (p postgres) Count(ctx context.Context, key string, window time.Duration) (int, time.Duration, error) {
	// The same rule as Reserve's: the window's ending is a fact of the row, and the
	// countdown is that fact read at the instant the read happened.
	const q = `SELECT count, extract(epoch FROM (window_start + ?::interval - clock_timestamp())) FROM ` + table +
		` WHERE key = ? AND window_start > now() - ?::interval`
	return p.scan(ctx, q, interval(window), p.scoped(ctx, key), interval(window))
}

func (p postgres) Forget(ctx context.Context, key string) error {
	return p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("DELETE FROM "+table+" WHERE key = ?", p.scoped(ctx, key)).Error
	})
}

// Purge deletes the rows whose window closed a day ago.
//
// It is a scheduled job and never a side effect of Allow: a counter is read on
// the path of a request that is about to be refused, and a refusal that also
// deleted somebody else's row would be a limiter paying for the traffic it is
// refusing. The table is written by whoever holds a limiter and by nothing else,
// so the job belongs to the composition that holds them all — kit/app schedules
// it beside the outbox purge, which is where it moved when the kernel's own
// public write limit became the second writer (docs/adr/0010). A module's sweep
// is the wrong home for it: the rows of a composition that does not compose that
// module would never go.
func Purge(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, systemToken, func(_ context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec("DELETE FROM "+table+" WHERE window_start < now() - ?::interval", interval(keep)).Error; err != nil {
			return fmt.Errorf("limit: purge: %w", err)
		}
		return nil
	})
}

// scan runs one statement answering with a count and what is left of the
// window. No row is not an error: it is a key nobody has counted yet.
func (p postgres) scan(ctx context.Context, query string, args ...any) (int, time.Duration, error) {
	var (
		n    int
		left float64
	)
	err := p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		rows, err := tx.DB().Raw(query, args...).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		return rows.Scan(&n, &left)
	})
	if err != nil {
		return 0, 0, err
	}
	if left < 0 {
		left = 0
	}
	return n, time.Duration(left * float64(time.Second)), nil
}

// run opens the counter's own transaction, and answers for what happened in it.
// Detached, so the count survives the rollback of the request that made it;
// WithoutCancel, so a caller who hung up is still counted; and bounded, because
// neither of those may turn a database that has stopped answering into a request
// that never ends. The two waits inside that wall are told apart here, which is
// the whole of this package's cure: see classified.
//
// One answer is asked again before it is classified at all: the bare
// bad-connection sentinel arriving while the wall still stands (see
// askedAgain). That retry cannot double-count, and the wall bounds it — the
// loop is out of wall time the moment the wall is behind the attempt. The ask
// is withheld from an attempt that ever stood inside its transaction, which is
// the one state in which a counter could already have been spent.
func (p postgres) run(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
	conn, ok := p.conns(ctx)
	if !ok {
		return ErrNoConnection
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), budget)
	defer cancel()
	attempt := p.attempt
	if attempt == nil {
		// The zero postgres is a limiter: one assembled without naming its
		// attempt — as every internal case that goes to a real store builds
		// one — gets the attempt every deployment writes.
		attempt = queueRun
	}
	for {
		// entered says whether this attempt ever stood inside a transaction.
		// Nothing else in the answer can say it: kit/db hands back the callback's
		// error unwrapped, so a statement that failed after the transaction opened
		// can look exactly like the pool's ask.
		entered := false
		err := attempt(detached, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			entered = true
			return fn(ctx, tx)
		})
		if entered || !askedAgain(err, detached) {
			return classified(err, detached)
		}
		// driver.ErrBadConn is the driver's promise that nothing reached the
		// server — it lets that sentinel escape only once its own retries and a
		// fresh connection have come back the same way — so this attempt is no
		// more counted than it was a breath ago, and the wait for a usable
		// connection is the same wait this wall already buys. Asking again is
		// what keeps a pool storm off the fail-open path: classified reads a
		// bad connection inside a standing wall as an outage, and every caller
		// in the field admits an outage.
	}
}

// askedAgain reports the one answer that is nobody's verdict: the bare
// driver.ErrBadConn — that error and nothing wrapping it — arriving with the
// attempt's wall still standing. The bare form is sql's own giving-up signal, its
// words for "no connection ever took this statement; a fresh one might". An error
// that reaches the sentinel through somebody's wrapper is kit/db answering from
// inside a transaction it opened, and that is the store's verdict: it keeps its
// error while the wall stands.
func askedAgain(err error, attempt context.Context) bool {
	return errors.Is(err, driver.ErrBadConn) && errors.Unwrap(err) == nil && !wallPassed(attempt)
}

// classified is the one place this package says which world an attempt just
// lived in, so that every method answers from one decision and a conformance
// fake cannot hold a different opinion about it. A wait that spent its budget is
// a refusal — ErrBusy, which Allow carries as ok=false and nothing else — and an
// error the store sent back is an outage, which stays the error ADR 0010 tells
// the caller to fail open on. The attempt's own context is part of that reading:
// an error that names no server-side verdict, arriving the instant the attempt's
// wall expired, is the wall refusing and not the store answering badly.
func classified(err error, attempt context.Context) error {
	if err == nil {
		return nil
	}
	if waited(err, attempt) {
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return fmt.Errorf("limit: %w", err)
}

// classified reports the answers that mean this attempt never got what it asked
// for while the store was there: 55P03 lock_timeout, or the attempt's own wall —
// the pool's driver answers a connection wait with a bare driver.ErrBadConn and
// drops the deadline with it, so the wall answers, not the text.
//
// A store that is down answers at once and in kind, and keeps its error while the
// wall stands: docs/adr/0010 puts the outage decision with the caller, and a
// lockout must not be able to close during an outage.
func waited(err error, attempt context.Context) bool {
	if pg, isPostgres := errors.AsType[*pgconn.PgError](err); isPostgres && pg.Code == "55P03" {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return errors.Is(err, driver.ErrBadConn) && wallPassed(attempt)
}

// wallPassed reports whether an attempt's own wall is behind it — the fact
// waited needs, of which context.Err() is only the notification, and the
// notification is late by construction. WithTimeout cancels from a time.AfterFunc
// callback that runs in a goroutine of its own, so on a runner with no free core
// the deadline has passed, the waiting has been broken off, and Err() still
// reports nothing. Read the notification alone and the wall is mistaken for a
// down store, which answers with the error every fail-open caller admits: the one
// answer this package exists to withhold. An attempt with no wall of its own has
// no deadline to read and is judged on the notification alone, as before.
func wallPassed(attempt context.Context) bool {
	if deadline, ok := attempt.Deadline(); ok && !time.Now().Before(deadline) {
		return true
	}
	return errors.Is(attempt.Err(), context.DeadlineExceeded)
}

// scoped is the key as it is stored: the tenant of the context, then the
// caller's own key. The tenant id is a fixed thirty-six characters, so no
// caller's key can forge another tenant's prefix, and a context with no tenant
// counts under the nil UUID — one bucket for the whole installation, rather
// than a bucket shared with whichever customer happened to resolve.
// scoped is the key with no app in it: the deployment of one app, and the memory
// limiter, which counts in one process and so counts one app's buckets.
func scoped(ctx context.Context, key string) string {
	var id uuid.UUID
	if t, ok := tenancy.FromContext(ctx); ok {
		id = t.ID
	}
	return appname.RateLimitKey(appname.Name(""), id, key)
}

// scoped is the key as it is stored: the app, then the tenant of the context, then
// the caller's own key. Both fixed-length identifiers come before the caller's
// text, so no caller's key can forge another app's or tenant's prefix.
func (p postgres) scoped(ctx context.Context, key string) string {
	var id uuid.UUID
	if t, ok := tenancy.FromContext(ctx); ok {
		id = t.ID
	}
	return appname.RateLimitKey(p.app, id, key)
}

// interval is a window as Postgres reads one. Milliseconds, so a test may use a
// window shorter than a second.
func interval(window time.Duration) string {
	return fmt.Sprintf("%d milliseconds", window.Milliseconds())
}
