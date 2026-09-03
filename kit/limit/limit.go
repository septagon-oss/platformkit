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
	"time"

	"github.com/google/uuid"

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
	// budget bounds every statement here. A limiter is on the path of a
	// request that is about to be refused and must never be the thing that
	// holds one open: a database that has stopped answering costs the caller
	// this much and then an error it can fail open on.
	budget = 2 * time.Second

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

// Connections is where a limiter finds the pool. httpx.ConnFrom is the one the
// application passes: the kernel puts the connection on every request's
// context, and a job is handed one it can put there itself.
//
// It is a parameter rather than an import for two reasons. A composition builds
// its modules before kit/app opens the pool, so a limiter cannot be handed a
// connection at construction; and a package that counts rows has no business
// linking a web server to find out where they go — modules/auth's contracts
// package holds a Limiter, and a contracts package is the entity and the
// interfaces (ARCHITECTURE, idea 3).
type Connections func(context.Context) (*db.Conn, bool)

// Postgres returns the limiter every replica shares.
func Postgres(conns Connections) Limiter { return postgres{conns: conns} }

type postgres struct{ conns Connections }

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
	n, left, err := p.scan(ctx, q, scoped(ctx, key), interval(window), interval(window), interval(window))
	if err != nil {
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
	return p.scan(ctx, q, interval(window), scoped(ctx, key), interval(window))
}

func (p postgres) Forget(ctx context.Context, key string) error {
	return p.run(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("DELETE FROM "+table+" WHERE key = ?", scoped(ctx, key)).Error
	})
}

// Purge deletes the rows whose window closed a day ago. It is the caller's
// hourly job that runs it, because the table is written by whoever uses the
// limiter and by nothing else; the moment a second module adopts this, the
// purge belongs beside the outbox's in kit/app.
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
		return 0, 0, fmt.Errorf("limit: %w", err)
	}
	if left < 0 {
		left = 0
	}
	return n, time.Duration(left * float64(time.Second)), nil
}

// run opens the counter's own transaction. Detached, so the count survives the
// rollback of the request that made it; WithoutCancel, so a caller who hung up
// is still counted; and bounded, because neither of those may turn a database
// that has stopped answering into a request that never ends.
func (p postgres) run(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
	conn, ok := p.conns(ctx)
	if !ok {
		return ErrNoConnection
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), budget)
	defer cancel()
	return db.RunSystem(detached, conn, systemToken, fn)
}

// scoped is the key as it is stored: the tenant of the context, then the
// caller's own key. The tenant id is a fixed thirty-six characters, so no
// caller's key can forge another tenant's prefix, and a context with no tenant
// counts under the nil UUID — one bucket for the whole installation, rather
// than a bucket shared with whichever customer happened to resolve.
func scoped(ctx context.Context, key string) string {
	var id uuid.UUID
	if t, ok := tenancy.FromContext(ctx); ok {
		id = t.ID
	}
	return id.String() + "/" + key
}

// interval is a window as Postgres reads one. Milliseconds, so a test may use a
// window shorter than a second.
func interval(window time.Duration) string {
	return fmt.Sprintf("%d milliseconds", window.Milliseconds())
}
