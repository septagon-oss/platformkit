package limit_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

const window = time.Minute

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme"}

// TestBothLimitersAgree runs one suite against both implementations, which is
// what entitles a test elsewhere to use the memory one: an interface is
// justified by a passing fake, and this is the passing.
func TestBothLimitersAgree(t *testing.T) {
	for implementation, build := range map[string]func(*testing.T) (limit.Limiter, context.Context){
		"memory":   inMemory,
		"postgres": inPostgres,
	} {
		t.Run(implementation, func(t *testing.T) {
			for name, run := range cases {
				t.Run(name, func(t *testing.T) {
					runCase := func(t *testing.T) {
						l, ctx := build(t)
						run(t, l, ctx)
					}
					if implementation == "memory" {
						// Only the in-process limiter shares synctest's clock.
						synctest.Test(t, runCase)
					} else {
						runCase(t)
					}
				})
			}
		})
	}
}

var cases = map[string]func(*testing.T, limit.Limiter, context.Context){
	"the limit-th event is allowed and the next is not": func(t *testing.T, l limit.Limiter, ctx context.Context) {
		for i := range 3 {
			ok, retry, err := l.Allow(ctx, "ada", 3, window)
			if err != nil {
				t.Fatalf("Allow %d: %v", i+1, err)
			}
			if !ok {
				t.Fatalf("event %d of 3 was refused", i+1)
			}
			if retry != 0 {
				t.Errorf("an allowed event asked the caller to wait %s", retry)
			}
		}
		ok, retry, err := l.Allow(ctx, "ada", 3, window)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if ok {
			t.Error("the fourth event of a limit of three was allowed")
		}
		if retry <= 0 || retry > window {
			t.Errorf("retryAfter is %s, want what is left of %s", retry, window)
		}
	},

	"a closed window starts again": func(t *testing.T, l limit.Limiter, ctx context.Context) {
		const brief = 300 * time.Millisecond
		for range 2 {
			if _, _, err := l.Allow(ctx, "ada", 1, brief); err != nil {
				t.Fatalf("Allow: %v", err)
			}
		}
		time.Sleep(2 * brief)
		ok, _, err := l.Allow(ctx, "ada", 1, brief)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if !ok {
			t.Error("the window never closed; a limit that only ever accumulates is a lockout")
		}
	},

	"Count records nothing": func(t *testing.T, l limit.Limiter, ctx context.Context) {
		if _, _, err := l.Allow(ctx, "ada", 10, window); err != nil {
			t.Fatalf("Allow: %v", err)
		}
		for range 5 {
			n, _, err := l.Count(ctx, "ada", window)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if n != 1 {
				t.Fatalf("Count = %d after one event and five reads, want 1", n)
			}
		}
		// And a key nobody has counted is zero rather than an error.
		if n, _, err := l.Count(ctx, "nobody", window); err != nil || n != 0 {
			t.Errorf("Count of an unused key = %d, %v; want 0 and no error", n, err)
		}
	},

	"Forget drops the key": func(t *testing.T, l limit.Limiter, ctx context.Context) {
		for range 3 {
			if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
				t.Fatalf("Allow: %v", err)
			}
		}
		if err := l.Forget(ctx, "ada"); err != nil {
			t.Fatalf("Forget: %v", err)
		}
		if n, _, err := l.Count(ctx, "ada", window); err != nil || n != 0 {
			t.Errorf("Count after Forget = %d, %v; want 0", n, err)
		}
	},

	"a burst at one key admits no more than the limit": func(t *testing.T, l limit.Limiter, ctx context.Context) {
		// The arithmetic both implementations owe, whatever the store's own shape:
		// N parallel attempts at one key can never raise the counter past its
		// allowance. What the two do about the attempts they could not count is the
		// difference between them, and the Postgres-only cases below pin that.
		const knocks, allowance = 75, 20
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			admitted int
		)
		start := make(chan struct{})
		for range knocks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ok, _, err := l.Allow(ctx, "ada", allowance, window)
				if err != nil {
					t.Errorf("Allow: %v", err)
					return
				}
				if ok {
					mu.Lock()
					admitted++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		if admitted > allowance {
			t.Errorf("%d of %d parallel attempts were allowed at a limit of %d", admitted, knocks, allowance)
		}
	},

	"two keys and two tenants are four counters": func(t *testing.T, l limit.Limiter, ctx context.Context) {
		globex := tenancy.WithTenant(ctx, tenancy.Tenant{ID: uuid.New(), Slug: "globex"})
		for range 3 {
			if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
				t.Fatalf("Allow: %v", err)
			}
		}
		for _, tt := range []struct {
			what string
			ctx  context.Context
			key  string
		}{
			{"another key in this tenant", ctx, "grace"},
			{"the same key in another tenant", globex, "ada"},
		} {
			if n, _, err := l.Count(tt.ctx, tt.key, window); err != nil || n != 0 {
				t.Errorf("%s = %d, %v; want its own counter", tt.what, n, err)
			}
		}
	},
}

// TestTwoReplicasShareOneLimit is the whole point of the package.
//
// Two pools on one database are what two pods are: the per-process counter this
// replaces gave an attacker the limit multiplied by the replica count, and gave
// it back on every deploy. Here the fourth attempt is refused whichever pool
// makes it.
func TestTwoReplicasShareOneLimit(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	first, second := replica(t, appURL), replica(t, appURL)
	l := limit.Postgres(httpx.ConnFrom)

	for i, ctx := range []context.Context{first, second, first} {
		if ok, _, err := l.Allow(ctx, "ada", 3, window); err != nil || !ok {
			t.Fatalf("attempt %d = %v, %v; want allowed", i+1, ok, err)
		}
	}
	ok, retry, err := l.Allow(second, "ada", 3, window)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Error("the second replica had a limit of its own, which is three limits on three pods")
	}
	if retry <= 0 {
		t.Errorf("retryAfter is %s, want what is left of the window", retry)
	}
	// And what one replica forgets, the other has forgotten too.
	if err := l.Forget(first, "ada"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if ok, _, err := l.Allow(second, "ada", 3, window); err != nil || !ok {
		t.Errorf("after the first replica forgot the key, the second = %v, %v", ok, err)
	}
}

// TestPurgeDropsWindowsThatClosedLongAgo. Without it the table is a row per key
// anybody ever tried, forever, which is an outage with an attacker's name on it.
func TestPurgeDropsWindowsThatClosedLongAgo(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	l := limit.Postgres(httpx.ConnFrom)
	if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if err := limit.Purge(t.Context(), conn); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n := rows(t, admin); n != 1 {
		t.Errorf("Purge deleted an open window: %d rows, want 1", n)
	}
	if _, err := admin.ExecContext(t.Context(),
		"UPDATE platformkit_limits SET window_start = now() - interval '2 days'"); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	if err := limit.Purge(t.Context(), conn); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n := rows(t, admin); n != 0 {
		t.Errorf("Purge left %d rows whose window closed two days ago", n)
	}
}

// TestAContextWithNoConnectionIsAnError, rather than a silent allowance: the
// caller decides what to do about a limiter it cannot reach, and a limiter that
// answered "allowed" without saying so would be a limit nobody could tell had
// stopped working.
func TestAContextWithNoConnectionIsAnError(t *testing.T) {
	l := limit.Postgres(httpx.ConnFrom)
	if _, _, err := l.Allow(t.Context(), "ada", 3, window); !errors.Is(err, limit.ErrNoConnection) {
		t.Errorf("Allow with no connection = %v, want ErrNoConnection", err)
	}
	if _, _, err := l.Count(t.Context(), "ada", window); !errors.Is(err, limit.ErrNoConnection) {
		t.Errorf("Count with no connection = %v, want ErrNoConnection", err)
	}
	if err := l.Forget(t.Context(), "ada"); !errors.Is(err, limit.ErrNoConnection) {
		t.Errorf("Forget with no connection = %v, want ErrNoConnection", err)
	}
}

// TestTheLimiterTellsAQueuedCounterFromADownOne is the cure's other half. The
// burst case shows a queued attempt is never admitted; this one shows an outage
// still is, which is the direction that would otherwise be cured into a lockout
// of the whole installation during a maintenance window (docs/adr/0010, and
// modules/auth's comment about its own sign-in door).
func TestTheLimiterTellsAQueuedCounterFromADownOne(t *testing.T) {
	t.Run("a key queued behind its own row is refused, with no error to fail open on", func(t *testing.T) {
		admin, conn := dbtest.Schema(t)
		ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
		l := limit.Postgres(httpx.ConnFrom)
		if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
			t.Fatalf("Allow: %v", err)
		}
		dbtest.Hold(t, admin, 5*time.Second,
			"UPDATE platformkit_limits SET count = count WHERE key = $1", storedKeys(t, admin, 1)[0])

		ok, retry, err := l.Allow(ctx, "ada", 3, window)
		if err != nil {
			t.Fatalf("a queued attempt is answered rather than errored: %v", err)
		}
		if ok {
			t.Error("a queued attempt was admitted for having waited, which is the failure this package exists to stop")
		}
		if retry != window {
			t.Errorf("retryAfter is %s, want the whole window %s: the row was never read, so nothing knows what is left of it",
				retry, window)
		}
		if got := storedCount(t, admin); got != 1 {
			t.Errorf("the queued attempt recorded itself: the row counts %d, want 1", got)
		}
		// A row lock never stops a read: MVCC answers from the last committed
		// version, so the door that reads before it decides — auth's lockout —
		// keeps reading while a burst queues on the same key.
		if n, _, err := l.Count(ctx, "ada", window); err != nil || n != 1 {
			t.Errorf("Count behind the row's lock = %d, %v; want 1 and no error", n, err)
		}
	})

	t.Run("a store that cannot be reached is an error, and not ErrBusy", func(t *testing.T) {
		adminURL, appURL := dbtest.URLs(t)
		if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		conn, err := db.Open(t.Context(), appURL)
		if err != nil {
			t.Fatalf("open the pool: %v", err)
		}
		ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
		l := limit.Postgres(httpx.ConnFrom)
		if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
			t.Fatalf("Allow while the store is there: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("close the pool: %v", err)
		}
		// Each method answers with the error ADR 0010 tells its caller to fail
		// open on. ErrBusy here would be a limiter that closed the door while its
		// store was down, which is the outage this rule exists to avoid.
		if _, _, err := l.Allow(ctx, "ada", 3, window); err == nil || errors.Is(err, limit.ErrBusy) {
			t.Errorf("Allow with the store gone = %v; want an error that is not ErrBusy", err)
		}
		if _, _, err := l.Count(ctx, "ada", window); err == nil || errors.Is(err, limit.ErrBusy) {
			t.Errorf("Count with the store gone = %v; want an error that is not ErrBusy", err)
		}
		if err := l.Forget(ctx, "ada"); err == nil || errors.Is(err, limit.ErrBusy) {
			t.Errorf("Forget with the store gone = %v; want an error that is not ErrBusy", err)
		}
	})
}

// TestTheLimiterRefusesToReadAWindowItCannotSee. Count and Forget have no ok to
// refuse with, so the same two worlds arrive as ErrBusy rather than as a number
// nobody read: a Count that answered 0 while the store was busy would tell a
// lockout that nobody had signed in, and a Forget that answered nil while its
// DELETE waited would unlock an account that is still locked.
func TestTheLimiterRefusesToReadAWindowItCannotSee(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	l := limit.Postgres(httpx.ConnFrom)
	if _, _, err := l.Allow(ctx, "ada", 3, window); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	// A row lock stops a write and never a read, so the world this case needs is
	// the one that stops both: the table is locked. The budget that answers for
	// the wait is the same one, and the refusal does not depend on the shape of
	// the statement that was waiting.
	release := dbtest.Hold(t, admin, 10*time.Second, "LOCK TABLE platformkit_limits IN ACCESS EXCLUSIVE MODE")
	if n, _, err := l.Count(ctx, "ada", window); !errors.Is(err, limit.ErrBusy) || n != 0 {
		t.Errorf("Count of a window it could not read = %d, %v; want 0 and ErrBusy, never an invented number", n, err)
	}
	if err := l.Forget(ctx, "ada"); !errors.Is(err, limit.ErrBusy) {
		t.Errorf("Forget of a counter it could not take = %v; want ErrBusy", err)
	}
	release()
	// And a refusal to forget is not a forget: the row is where it was.
	if n, _, err := l.Count(ctx, "ada", window); err != nil || n != 1 {
		t.Errorf("after the refused Forget, Count = %d, %v; want the 1 attempt the row was holding", n, err)
	}
}

func inMemory(t *testing.T) (limit.Limiter, context.Context) {
	t.Helper()
	return limit.Memory(), tenancy.WithTenant(t.Context(), acme)
}

func inPostgres(t *testing.T) (limit.Limiter, context.Context) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	return limit.Postgres(httpx.ConnFrom), httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
}

// replica is one pod's connection pool, on a schema somebody else migrated.
func replica(t *testing.T, appURL string) context.Context {
	t.Helper()
	conn, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatalf("open a replica's pool: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
}

func rows(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_limits").Scan(&n); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	return n
}

// storedKeys is the key the limiter really stores, read back rather than
// restated: the scope is this package's own, and a test that spelled it out would
// be a second opinion about it.
func storedKeys(t *testing.T, admin *sql.DB, want int) []string {
	t.Helper()
	rows, err := admin.QueryContext(t.Context(), "SELECT key FROM platformkit_limits ORDER BY key")
	if err != nil {
		t.Fatalf("read the stored keys: %v", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("read a stored key: %v", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the stored keys: %v", err)
	}
	if want > 0 && len(keys) != want {
		t.Fatalf("the table holds %d keys, want %d", len(keys), want)
	}
	return keys
}

// storedCount is what the one row in the table has counted — the counter as it is
// written rather than as the limiter answered it, which is the difference a
// refusal is measured against.
func storedCount(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n, rows int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT coalesce(max(count), 0), count(*) FROM platformkit_limits").Scan(&n, &rows); err != nil {
		t.Fatalf("read the counter: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the table holds %d rows, want the one counter this case counted", rows)
	}
	return n
}
