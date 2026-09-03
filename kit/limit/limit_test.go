package limit_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
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
	for name, build := range map[string]func(*testing.T) (limit.Limiter, context.Context){
		"memory":   inMemory,
		"postgres": inPostgres,
	} {
		t.Run(name, func(t *testing.T) {
			for name, run := range cases {
				t.Run(name, func(t *testing.T) {
					l, ctx := build(t)
					run(t, l, ctx)
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
	if err := db.Migrate(t.Context(), adminURL, migrations.FS); err != nil {
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
