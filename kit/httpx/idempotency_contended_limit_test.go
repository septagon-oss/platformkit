package httpx_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestStoredPostgresRefusalWaitExcludesTimeWaitingForTheCounterLock(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    conn, logs: &lines{},
	}
	counter := limit.Postgres(httpx.ConnFrom)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), f.tenant), conn)
	const window = 5 * time.Second
	const name = "contended-entries"
	if allowed, _, _, err := counter.Reserve(ctx, name, 1, window); err != nil || !allowed {
		t.Fatalf("seed counter: allowed=%v error=%v", allowed, err)
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: f, Conn: conn,
		Authorize: f, Authenticate: f.authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom), Log: slog.New(slog.DiscardHandler),
	})
	runs := 0
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "create-contended-entry", Method: http.MethodPost, Path: "/contended-entries",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		runs++
		allowed, remaining, left, err := counter.Reserve(ctx, name, 1, window)
		if err != nil {
			return nil, err
		}
		httpx.SayCounted(ctx, httpx.Counted{
			Name: name, Quota: 1, Window: window, Remaining: remaining, Left: left, Refused: !allowed,
		})
		return nil, problem.New(http.StatusTooManyRequests, "entry limit exhausted")
	})
	locker, err := admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	var lockerPID, count int
	if err := locker.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&lockerPID); err != nil {
		t.Fatal(err)
	}
	key := f.tenant.ID.String() + "/" + name
	if err := locker.QueryRowContext(t.Context(), "SELECT count FROM platformkit_limits WHERE key = $1 FOR UPDATE", key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/public/probe/contended-entries"
	// The row is parked on while it still has just over two seconds of window left, and
	// the wait under its lock is two fifths of a second. Both figures belong to the
	// counter, not to this case: kit/limit ends a wait for one key's row at its own queue
	// budget — a second — and answers that wait as a refusal that recorded nothing, so the
	// only wait this case can measure at all is a fraction of a second, and the wait it
	// states is stated in whole seconds. Reading the row until it is near its end is what
	// puts the two on the same side of a second boundary: the answer owes the row's
	// remaining less the wait, and a second boundary is where those two readings part.
	// The room left under the bound is the room for the wait, its scheduling and a host
	// doing other things, and a window that closed mid-attempt would reset the count and
	// read as a missing reservation rather than as what it was.
	if left := awaitWindowUnder(t, admin, key, window, 2200*time.Millisecond); left < 0 {
		t.Fatalf("the counter's row never came within 2.2s of its window's end")
	}
	answers := make(chan *httptest.ResponseRecorder, 1)
	go func() { answers <- keyedPost(t, router, http.MethodPost, path, "contended-refusal", "{}") }()
	// Confirm a real counter statement is blocked before spending time under its lock.
	waitCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		var blocked bool
		err := admin.QueryRowContext(waitCtx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))
			AND query LIKE '%INSERT INTO platformkit_limits%')`, lockerPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("wait for contending counter: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	if err := locker.Rollback(); err != nil {
		t.Fatal(err)
	}
	var first *httptest.ResponseRecorder
	select {
	case first = <-answers:
	case <-time.After(10 * time.Second):
		t.Fatal("counter did not finish after its lock was released")
	}
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first answer = %d %s", first.Code, first.Body.String())
	}
	var maximumWait int
	if err := admin.QueryRowContext(t.Context(), `SELECT count,
		GREATEST(0, ceil(extract(epoch FROM (window_start + interval '5 seconds' - clock_timestamp()))))::int
		FROM platformkit_limits WHERE key = $1`, key).Scan(&count, &maximumWait); err != nil {
		t.Fatal(err)
	}
	replay := keyedPost(t, router, http.MethodPost, path, "contended-refusal", "{}")
	if replay.Code != http.StatusTooManyRequests || replay.Body.String() != first.Body.String() || runs != 1 || count != 2 {
		t.Fatalf("replay=%d body=%q executions=%d counter=%d; want same refusal without another execution or reservation", replay.Code, replay.Body.String(), runs, count)
	}
	if got := replay.Header().Get(httpx.RateLimitPolicyHeader); got != `"contended-entries";q=1;w=5` {
		t.Errorf("replayed policy = %q; want the deciding policy", got)
	}
	wait, err := strconv.Atoi(replay.Header().Get(httpx.RetryAfterHeader))
	if err != nil || wait < 1 || wait > max(maximumWait, 1) {
		t.Errorf("replayed Retry-After = %q, Postgres counter has at most %d seconds left; waiting for its lock must not extend the stored window", replay.Header().Get(httpx.RetryAfterHeader), maximumWait)
	}
	var remaining, reset int
	if _, err := fmt.Sscanf(replay.Header().Get(httpx.RateLimitHeader), `"contended-entries";r=%d;t=%d`, &remaining, &reset); err != nil || remaining != 0 || reset < 0 || reset > maximumWait {
		t.Errorf("replayed RateLimit = %q; deciding counter has at most %d seconds left", replay.Header().Get(httpx.RateLimitHeader), maximumWait)
	}
}

// awaitWindowUnder reads a counter row's own remaining window until it falls at or
// below want, and answers how much was left when it did. A case that has to observe a
// countdown close to its end reads the row rather than sleeping at the wall: when the
// window ends is the row's fact, and a wall-clock estimate of it is off by however long
// the fixture around the seed took. The window is stated in whole seconds, which is how
// kit/limit's own column holds it.
func awaitWindowUnder(t *testing.T, admin *sql.DB, key string, window, want time.Duration) time.Duration {
	t.Helper()
	q := fmt.Sprintf(`SELECT GREATEST(0, extract(epoch FROM (window_start + interval '%d seconds' - now())))
		FROM platformkit_limits WHERE key = $1`, int64(window.Seconds()))
	for {
		var left float64
		if err := admin.QueryRowContext(t.Context(), q, key).Scan(&left); err != nil {
			t.Fatalf("read the counter's own countdown: %v", err)
		}
		if remaining := time.Duration(left * float64(time.Second)); remaining <= want {
			return remaining
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-t.Context().Done():
			return -1
		}
	}
}
