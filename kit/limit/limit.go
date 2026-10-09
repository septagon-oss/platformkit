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
	"errors"
	"fmt"
	"github.com/septagon-oss/platformkit/kit/appname"
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
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error)

	// Count reports how many events key has in the window that is open now, and
	// how long that window has left. It records nothing, because a limit with
	// three answers rather than two — allow, delay, refuse — has to be read
	// before the attempt it is about, and a read that counted would make every
	// successful sign-in an attempt against the lockout.
	Count(ctx context.Context, key string, window time.Duration) (n int, retryAfter time.Duration, err error)

	// Forget drops a key: the caller has been proved right about whoever they
	// were counting.
	Forget(ctx context.Context, key string) error
}

// table is the one this package owns. See migrations/000021_limits.
const table = "platformkit_limits"

const (
	// budget is the whole wall one attempt may take, and it is the number
	// docs/adr/0010 promises the caller — this much, and then an error it can
	// fail open on. A limiter is on the path of a request that is about to be
	// refused and must never be the thing that holds one open, so raising it to
	// cover a queue is the wrong lever: the queue is the traffic's own length.
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
// Allow never returns it. Its answer to the same fact is the refusal triple —
// ok false, retryAfter the whole window, no error — because what an error means
// is the caller's decision, and every caller in the field fails open on one.
// That is the failure mode this package exists to stop, so it is carried in the
// answer rather than in the error; README.md says why, and says what a caller
// that means to fail open would have to ask for.
var ErrBusy = errors.New("limit: the counter was still busy with this key, and the attempt was not counted")

// Connections is where a limiter finds the pool.
//
// It is a parameter rather than an import for two reasons. A composition builds
// its modules before kit/app opens the pool, so a limiter cannot be handed one
// at construction; and a package that counts rows has no business linking a web
// server to find out where they go — modules/auth's contracts package holds a
// Limiter, and a contracts package is the entity and the interfaces.
//
// What it hands back is the pool itself, not a connection: a count is written in
// a transaction of its own, detached from whatever request asked for it. Read it
// from a held handle (`kit/app` composes `limit.Postgres(a.held.read)`) rather
// than from a request context: `httpx.ConnFrom` answers nothing until the
// middleware that opens the request's transaction has run, and the public write
// limit counts ahead of it, so a composition built that way would find no pool on
// every anonymous write and — the failure mode below says which way that goes —
// admit all of them.
type Connections func(context.Context) (*db.Conn, bool)

// Postgres returns the limiter every replica shares.
func Postgres(conns Connections) Limiter { return postgres{conns: conns} }

// PostgresOf is Postgres for a deployment that names its app: every bucket key
// begins with the slug, so two apps that count the same-named bucket of the same
// tenant id spend one another's allowance. With no slug set the key is the one the
// counters in the field already hold.
func PostgresOf(app appname.Name, conns Connections) Limiter {
	return postgres{conns: conns, app: app}
}

type postgres struct {
	conns Connections
	app   appname.Name
}

func (p postgres) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	// One statement: the row is inserted, or the open window's count is raised,
	// or a window that has closed is started again — and the count that comes
	// back is the one this event landed in. Two statements would be two
	// replicas reading the same number and writing it back.
	const q = `INSERT INTO ` + table + ` (key, window_start, count) VALUES (?, now(), 1)
		ON CONFLICT (key) DO UPDATE SET
			count = CASE WHEN ` + table + `.window_start > now() - ?::interval THEN ` + table + `.count + 1 ELSE 1 END,
			window_start = CASE WHEN ` + table + `.window_start > now() - ?::interval THEN ` + table + `.window_start ELSE now() END
		RETURNING count, extract(epoch FROM (window_start + ?::interval - now()))`
	n, left, err := p.scan(ctx, q, p.scoped(ctx, key), interval(window), interval(window), interval(window))
	if err != nil {
		if errors.Is(err, ErrBusy) {
			// The attempt reached no row, so no window was read and there is
			// nothing honest to report as what is left of one; the whole window is
			// the one figure that cannot understate the wait. The refusal carries no
			// error for the reason ErrBusy's comment gives.
			return false, window, nil
		}
		return false, 0, err
	}
	if n <= limit {
		return true, 0, nil
	}
	return false, left, nil
}

func (p postgres) Count(ctx context.Context, key string, window time.Duration) (int, time.Duration, error) {
	const q = `SELECT count, extract(epoch FROM (window_start + ?::interval - now())) FROM ` + table +
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
func (p postgres) run(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
	conn, ok := p.conns(ctx)
	if !ok {
		return ErrNoConnection
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), budget)
	defer cancel()
	err := db.RunSystem(detached, conn, systemToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		// is_local, so the budget belongs to this transaction and dies with it
		// rather than riding the pooled connection to somebody else's statement.
		if err := tx.DB().Exec("SELECT set_config('lock_timeout', ?, true)",
			strconv.FormatInt(queueBudget.Milliseconds(), 10)).Error; err != nil {
			return err
		}
		return fn(ctx, tx)
	})
	return classified(err)
}

// classified is the one place this package says which world an attempt just
// lived in, so that every method answers from one decision and a conformance
// fake cannot hold a different opinion about it. A wait that spent its budget is
// a refusal — ErrBusy, which Allow carries as ok=false and nothing else — and an
// error the store sent back is an outage, which stays the error ADR 0010 tells
// the caller to fail open on.
func classified(err error) error {
	if err == nil {
		return nil
	}
	if waited(err) {
		return fmt.Errorf("%w: %w", ErrBusy, err)
	}
	return fmt.Errorf("limit: %w", err)
}

// waited reports the two answers that say this attempt never got what it asked
// for while the store is still there: the server stopped the wait for the row's
// lock, which is SQLSTATE 55P03 and the code lock_timeout raises, or this
// attempt's own wall expired, which is what a store that answers nothing — and a
// pool with nothing free, which is the same wait one level up — leaves behind.
// A store that is down is different in kind and in time: it answers, and it
// answers at once, with a refused connection, a closed database, a denied
// permission. Those keep their error, because ADR 0010 is right that the caller
// decides about an outage and that a lockout must not close during one.
func waited(err error) bool {
	if pg, isPostgres := errors.AsType[*pgconn.PgError](err); isPostgres && pg.Code == "55P03" {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
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
